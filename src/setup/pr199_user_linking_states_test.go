package setup

import (
	"app/src/model"
	"app/src/utils/request"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

type pr199DiscordReply struct {
	status int
	body   string
}

func pr199UserLinkingDB(t *testing.T, personsStatus int, personsBody string) *gorm.DB {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/persons/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(personsStatus)
		_, _ = w.Write([]byte(personsBody))
	}))
	t.Cleanup(server.Close)
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	db := newIAViewDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, server.URL+"/api")
	if err := setRuntimeKitsuToken(db, "runtime-kitsu-token"); err != nil {
		t.Fatal(err)
	}
	setRuntimeDiscordBotToken(db, "runtime-discord-token")
	return db
}

func installPR199DiscordReplies(t *testing.T, replies map[string]pr199DiscordReply) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "discord.com" {
			return old.RoundTrip(req)
		}
		reply, ok := replies[req.URL.Path]
		if !ok {
			reply = pr199DiscordReply{status: http.StatusNotFound, body: `{}`}
		}
		if reply.status == 0 {
			reply.status = http.StatusOK
		}
		return &http.Response{
			StatusCode: reply.status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(reply.body)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
}

func renderPR199UserLinking(t *testing.T, db *gorm.DB, rawURL string) string {
	t.Helper()
	w := httptest.NewRecorder()
	renderGlobalUserLinking(w, httptest.NewRequest(http.MethodGet, rawURL, nil), db)
	return w.Body.String()
}

func TestPR199UserLinkingKitsuFailureIsNotAnEmptyState(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusUnauthorized, `{"error":"denied"}`)
	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if !strings.Contains(body, "Kitsu users could not be checked") || !strings.Contains(body, "Diagnostic details") {
		t.Fatal("Kitsu lookup failure was not rendered as a failure state")
	}
	if strings.Contains(body, "<table") || strings.Contains(body, `id="global-discord-guild"`) {
		t.Fatal("Kitsu lookup failure rendered blocked mapping UI")
	}
}

func TestPR199UserLinkingZeroKitsuUsersIsSuccessfulEmpty(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[]`)
	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if !strings.Contains(body, "No Kitsu users were returned") || strings.Contains(body, "could not be checked") {
		t.Fatal("successful empty Kitsu response was not kept distinct from failure")
	}
	if strings.Contains(body, "<table") || strings.Contains(body, `id="global-discord-guild"`) {
		t.Fatal("zero Kitsu users rendered mapping controls")
	}
}

func TestPR199UserLinkingDiscordFailureIsNotZeroGuilds(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {status: http.StatusUnauthorized, body: `{}`},
	})
	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if !strings.Contains(body, "Discord users could not be loaded") || strings.Contains(body, "No Discord servers are available") {
		t.Fatal("Discord failure was collapsed into a zero-guild state")
	}
	if strings.Contains(body, "<table") {
		t.Fatal("Discord failure rendered the mapping table")
	}
}

func TestPR199UserLinkingZeroGuildsIsSuccessfulEmpty(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {body: `[]`},
	})
	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if !strings.Contains(body, "No Discord servers are available") || strings.Contains(body, "Discord users could not be loaded") {
		t.Fatal("zero-guild response was not rendered as a successful empty state")
	}
	if strings.Contains(body, `id="global-discord-guild"`) || strings.Contains(body, "<table") {
		t.Fatal("zero guilds rendered selector or mapping table")
	}
}

func TestPR199UserLinkingMultipleGuildsWaitsForSelection(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds": {body: `[{"id":"123456789012345678","name":"Studio A"},{"id":"123456789012345679","name":"Studio B"}]`},
	})
	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if !strings.Contains(body, `id="global-discord-guild"`) || !strings.Contains(body, "Select a Discord server to load members and enable saving.") {
		t.Fatal("multi-guild state did not render the selection boundary")
	}
	if strings.Contains(body, "<table") {
		t.Fatal("multi-guild state rendered rows before a server was selected")
	}
}

func TestPR199UserLinkingSelectedGuildWithNoHumansIsEmpty(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds":                  {body: `[{"id":"123456789012345678","name":"Studio"}]`},
		"/api/v10/guilds/123456789012345678/members": {body: `[{"user":{"id":"123456789012345680","username":"bot","global_name":"Bot User","bot":true}}]`},
	})
	body := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en")
	if !strings.Contains(body, "No selectable Discord users") || !strings.Contains(body, `id="global-discord-guild"`) {
		t.Fatal("selected-guild zero-human state did not preserve selector and empty result")
	}
	if strings.Contains(body, "Bot User") || strings.Contains(body, "<table") {
		t.Fatal("selected-guild zero-human state exposed bots or mapping rows")
	}
}
func TestPR199UserLinkingReadyTableExcludesBotsAndHasLanguageParity(t *testing.T) {
	db := pr199UserLinkingDB(t, http.StatusOK, `[{"id":"person-1","full_name":"Person One","email":"one@example.test","active":true}]`)
	guildID := "123456789012345678"
	installPR199DiscordReplies(t, map[string]pr199DiscordReply{
		"/api/v10/users/@me/guilds":                  {body: `[{"id":"123456789012345678","name":"Studio"}]`},
		"/api/v10/guilds/123456789012345678/members": {body: `[{"user":{"id":"123456789012345679","username":"human","global_name":"Human User","bot":false}},{"user":{"id":"123456789012345680","username":"bot","global_name":"Bot User","bot":true}}]`},
	})
	en := renderPR199UserLinking(t, db, "/bot/admin/users?lang=en&discord_guild_id="+guildID)
	ja := renderPR199UserLinking(t, db, "/bot/admin/users?lang=ja&discord_guild_id="+guildID)
	for _, body := range []string{en, ja} {
		if !strings.Contains(body, `class="user-link-grid-row"`) || !strings.Contains(body, `type="submit" disabled`) {
			t.Fatal("ready state did not render the interactive four-column row")
		}
		if !strings.Contains(body, "Person One") || !strings.Contains(body, "Human User") {
			t.Fatal("ready state omitted real directory identities")
		}
		if strings.Contains(body, "Bot User") {
			t.Fatal("Discord bot identity leaked into the human linking selector")
		}
	}
	for _, want := range []string{"Kitsu user", "Discord user", "Status", "Action"} {
		if !strings.Contains(en, want) {
			t.Fatalf("English ready table missing %q", want)
		}
	}
	if strings.Count(en, `class="user-link-grid-row"`) != strings.Count(ja, `class="user-link-grid-row"`) {
		t.Fatal("JP/EN ready table structure diverged")
	}
}
