package setup

// Display-path non-blocking guarantee: with Discord rate-limiting the member and
// role reads (botDo sleeps for Retry-After on 429), the team tab, reviewer
// manager, and notification tab must still render from the cached snapshot
// immediately. That is the whole point of the directory cache.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"

	"gorm.io/gorm"
)

// guildDirectoryRateLimitTransport answers every guild read with a 429 whose
// Retry-After is far longer than any render budget.
type guildDirectoryRateLimitTransport struct {
	base http.RoundTripper
	hits *atomic.Int32
}

func (d guildDirectoryRateLimitTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.Contains(r.URL.Path, "/guilds/") {
		return d.base.RoundTrip(r)
	}
	d.hits.Add(1)
	body := `{"message":"You are being rate limited.","retry_after":900}`
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Status:     "429 Too Many Requests",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

// expireGuildDirectory marks the cached snapshot for the guild as stale.
func expireGuildDirectory(t *testing.T, cache *guildDirectoryCache, guildID string) {
	t.Helper()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if entry := cache.entries[guildID]; entry != nil {
		entry.fetchedAt = cache.now().Add(-cache.ttl - time.Minute)
	}
}

func TestProductionDisplayPathsDoNotBlockOnDiscordRateLimit(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{
		KitsuProjectID:    "rate-limit-display",
		Name:              "Rate Limit Display",
		DiscordGuildID:    "123456789012345678",
		DiscordCategoryID: "123456789012345679",
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeDiscordBotTokenKey, "synthetic-discord-token")
	if err := model.SaveProductionForumSettings(db, reviewerBrowserForumSettings(project.KitsuProjectID, project.DiscordGuildID,
		[]model.ProductionForumRoute{
			{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", ForumChannelID: "forum-comp", ForumChannelName: "Compositing"},
			{ProductionID: project.KitsuProjectID, TaskTypeID: "task-anim", TaskTypeName: "Animation", ForumChannelID: "forum-anim", ForumChannelName: "Animation"},
		})); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.UserMap{KitsuID: "artist-person", KitsuName: "Linked Artist", DiscordID: "123456789012345680", DiscordDisplayName: "linked-artist"}).Error; err != nil {
		t.Fatal(err)
	}

	oldTasks, oldTeam, oldTransport := reviewerTaskTypesForProduction, reviewerProductionTeamReader, http.DefaultTransport
	t.Cleanup(func() {
		reviewerTaskTypesForProduction, reviewerProductionTeamReader = oldTasks, oldTeam
		http.DefaultTransport = oldTransport
	})
	reviewerTaskTypesForProduction = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "task-comp", Name: "Compositing", DepartmentID: "dept-comp", DepartmentName: "Comp"}}
	}
	reviewerProductionTeamReader = func(*gorm.DB, string) ([]kitsu.Person, error) {
		return []kitsu.Person{{ID: "artist-person", FullName: "Linked Artist", Role: "artist", Active: true}}, nil
	}

	// Warm the directory from the rate-limited Discord: botDo gives up after
	// three Retry-After waits, so the warm-up is slow but the snapshots are real.
	hits := new(atomic.Int32)
	http.DefaultTransport = guildDirectoryRateLimitTransport{base: oldTransport, hits: hits}
	failed := make(chan error, 2)
	go func() {
		_, _, err := guildMemberListCache.refresh(project.DiscordGuildID, "synthetic-discord-token")
		failed <- err
	}()
	go func() {
		_, _, err := guildRoleListCache.refresh(project.DiscordGuildID, "synthetic-discord-token")
		failed <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-failed; err == nil {
			t.Fatal("the directory warm-up should have failed against a rate-limited Discord")
		}
	}
	// Stand in a snapshot: it is what an earlier successful fetch would hold.
	seedGuildDirectory(t, guildMemberListCache, project.DiscordGuildID,
		`[{"user":{"id":"123456789012345680","username":"linked-artist","global_name":"Artist Nick"},"nick":""}]`)
	seedGuildDirectory(t, guildRoleListCache, project.DiscordGuildID,
		`[{"id":"123456789012345681","name":"Leads","managed":false,"mentionable":true}]`)
	expireGuildDirectory(t, guildMemberListCache, project.DiscordGuildID)
	expireGuildDirectory(t, guildRoleListCache, project.DiscordGuildID)
	hits.Store(0)

	request := func(query string) *http.Request {
		return httptest.NewRequest("GET", "/bot/admin/projects?project="+project.KitsuProjectID+query, nil)
	}
	screens := []struct {
		name   string
		render func() string
		want   []string
	}{
		{
			name: "team tab",
			render: func() string {
				return renderCurrentProductionTeam(db, request("&tab=team&lang=en"), project, "en", "synthetic-discord-token")
			},
			want: []string{"Linked Artist", "Artist Nick"},
		},
		{
			name: "reviewer manager",
			render: func() string {
				return renderCurrentProductionWFARecipients(db, request("&tab=team&lang=en"), project, "en", "synthetic-discord-token")
			},
			want: []string{"Compositing", "@Leads"},
		},
		{
			name: "notification tab",
			render: func() string {
				return renderSelectedProductionNotifications(db, request("&tab=notifications&lang=en"), project, "en", "ok", "Healthy", "", "synthetic-discord-token")
			},
			want: []string{"WFA recipients", "Compositing"},
		},
	}
	for _, screen := range screens {
		started := time.Now()
		body := screen.render()
		elapsed := time.Since(started)
		if elapsed >= 2*time.Second {
			t.Errorf("%s rendered in %s while Discord was rate limiting; budget is 2s", screen.name, elapsed)
		}
		for _, want := range screen.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q after %s: %s", screen.name, want, elapsed, body)
			}
		}
		t.Logf("%s rendered in %s", screen.name, elapsed)
	}
	// The renders must not have waited for Discord themselves; any request they
	// started runs in the background and is still sleeping in botDo.
	if hits.Load() == 0 {
		t.Fatal("no background refresh was started, so the fixture did not exercise the stale path")
	}
}

func seedGuildDirectory(t *testing.T, cache *guildDirectoryCache, guildID, body string) {
	t.Helper()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.entries[guildID] = &guildDirectoryCacheEntry{body: []byte(body), fetchedAt: cache.now()}
}
