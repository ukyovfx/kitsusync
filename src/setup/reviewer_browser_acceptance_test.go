package setup

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/request"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	reviewerBrowserRunEnv   = "KITSUSYNC_RUN_REVIEWER_BROWSER"
	reviewerBrowserToken    = "synthetic-reviewer-runtime-token"
	reviewerBrowserBot      = "synthetic-reviewer-discord-token"
	reviewerBrowserEmail    = "manager@synthetic.invalid"
	reviewerBrowserPass     = "synthetic-browser-password"
	reviewerBrowserGuild    = "11111111111111111"
	reviewerBrowserCategory = "55555555555555550"
	// userLinkingLongNickname reproduces the staging report where a very long
	// Discord nickname in a linked row's select pushed the 解除 button off the line.
	userLinkingLongNickname = "TVアニメ『佐々木さんとピーちゃんSeason2』第1話は1時間"
)

// TestReviewerBrowserAcceptance uses normal KitsuSync login/session routes,
// disposable SQLite state, synthetic Kitsu/Discord services, and real Chromium.
func TestReviewerBrowserAcceptance(t *testing.T) {
	if os.Getenv(reviewerBrowserRunEnv) != "1" {
		t.Skip("dedicated authenticated browser acceptance job")
	}

	workDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal("resolve repository root")
	}
	want := strings.TrimSpace(os.Getenv("KITSUSYNC_REVIEWER_BROWSER_SHA"))
	if want == "" {
		t.Fatal("exact browser candidate SHA is required")
	}
	got, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(got)) != want {
		t.Fatalf("browser candidate mismatch: HEAD=%q expected=%q", strings.TrimSpace(string(got)), want)
	}
	tmp := t.TempDir()
	outputDir := strings.TrimSpace(os.Getenv("KITSUSYNC_REVIEWER_BROWSER_OUTPUT"))
	if outputDir == "" {
		outputDir = filepath.Join(tmp, "evidence")
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		t.Fatal("create private browser evidence directory")
	}
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(tmp, "runtime-secret.key"))
	t.Setenv("KitsuJWTToken", "")
	t.Setenv("KITSU_API_BASE_URL", "")
	t.Setenv("KITSU_HOSTNAME", "")
	t.Setenv("DISCORD_BOT_TOKEN", "")

	db, err := gorm.Open(sqlite.Open(filepath.Join(tmp, "reviewer-browser.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("open disposable acceptance database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("open disposable acceptance database handle")
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&model.Task{}, &model.Project{}, &model.ProjectWebhook{}, &model.ProductionChannelMapping{},
		&model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{},
		&model.UserMap{}, &model.CheckerMap{}, &model.Setting{}, &model.AdminSession{}, &model.AuditLog{},
		&model.ProjectUserMap{}, &model.ProjectCheckerMap{}, &model.ProjectReviewerTarget{}, &model.ProductionNotificationWriteLock{}, &model.NotificationDeliveryEvent{}, &model.ManagedChannelDeletion{}, &model.ProjectSetting{},
		&model.ProductionForumConfig{}, &model.ProductionForumRoute{}, &model.ProductionForumStatusRole{}, &model.ProductionForumTag{},
	); err != nil {
		t.Fatal("migrate disposable acceptance database")
	}
	ConfigureSessionStore(db)
	resetSessions()
	t.Cleanup(func() {
		ConfigureSessionStore(nil)
		resetSessions()
		_ = sqlDB.Close()
	})

	var scenario atomic.Value
	scenario.Store("ready")
	kitsuFixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/data/") && r.Header.Get("Authorization") != "Bearer "+reviewerBrowserToken {
			http.Error(w, "synthetic Kitsu credential rejected", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/", "/api/auth/authenticated":
			// Kitsu host discovery probes these unauthenticated endpoints.
			_, _ = io.WriteString(w, `{"name":"Zou"}`)
		case "/api/status":
			_, _ = io.WriteString(w, `{"name":"Zou","database-up":true,"event-stream-up":true,"key-value-store-up":true,"version":"synthetic"}`)
		case "/api/auth/login":
			var form struct{ Email, Password string }
			if err := json.NewDecoder(r.Body).Decode(&form); err != nil || form.Email != reviewerBrowserEmail || form.Password != reviewerBrowserPass {
				http.Error(w, "synthetic login rejected", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"synthetic-manager-session-token","user":{"role":"manager"}}`)
		case "/api/data/projects/":
			projects := []kitsu.Project{{ID: "reviewer-production", Name: "Synthetic Review Production"}}
			if scenario.Load().(string) == "japanese-wfa-add" {
				projects[0].Name = "aa"
			}
			if mode := scenario.Load().(string); mode == "unconnected-production" || mode == "channels-unconnected" {
				projects = append(projects, kitsu.Project{ID: "reviewer-unconnected", Name: "Synthetic Unconnected Production"})
			}
			_ = json.NewEncoder(w).Encode(projects)
		case "/api/data/persons/":
			persons := reviewerBrowserPeople()
			if scenario.Load().(string) == "japanese-wfa-add" {
				persons = []kitsu.Person{{ID: "person-japanese", FullName: "日本語ユーザー", Email: "japanese@synthetic.invalid", Active: true, Role: "artist"}}
			}
			_ = json.NewEncoder(w).Encode(persons)
		case "/api/data/projects/reviewer-production/team":
			switch scenario.Load().(string) {
			case "japanese-wfa-add":
				_ = json.NewEncoder(w).Encode([]kitsu.Person{{ID: "person-japanese", FullName: "日本語ユーザー", Email: "japanese@synthetic.invalid", Active: true, Role: "artist"}})
			case "sparse-production":
				_ = json.NewEncoder(w).Encode([]kitsu.Person{{ID: "person-artist", FullName: "Synthetic Artist", Email: "artist@synthetic.invalid", Active: true, Role: "artist"}})
			case "team-failure":
				http.Error(w, "synthetic team failure", http.StatusForbidden)
			case "empty-team":
				_, _ = io.WriteString(w, `[]`)
			case "no-matching":
				_ = json.NewEncoder(w).Encode([]kitsu.Person{{ID: "person-artist", FullName: "Synthetic Artist", Email: "artist@synthetic.invalid", Active: true, Role: "artist"}})
			case "no-linked":
				_ = json.NewEncoder(w).Encode([]kitsu.Person{{ID: "person-unlinked", FullName: "Unlinked Supervisor", Email: "unlinked@synthetic.invalid", Active: true, Role: "supervisor"}})
			case "stale-membership":
				_ = json.NewEncoder(w).Encode([]kitsu.Person{{ID: "person-artist", FullName: "Synthetic Artist", Email: "artist@synthetic.invalid", Active: true, Role: "artist"}})
			default:
				_ = json.NewEncoder(w).Encode(reviewerBrowserTeam())
			}
		case "/api/data/projects/reviewer-production/task-types":
			if scenario.Load().(string) == "japanese-wfa-add" {
				_, _ = io.WriteString(w, `[{"id":"task-storyboard","name":"Storyboard","active":true}]`)
			} else {
				_, _ = io.WriteString(w, `[{"id":"task-comp","name":"Compositing","department_id":"dept-comp","department_name":"Compositing","active":true},{"id":"task-animation","name":"Animation","department_id":"dept-animation","department_name":"Animation","active":true},{"id":"task-unassigned","name":"Unassigned","active":true},{"id":"task-concept","name":"Concept","active":true},{"id":"task-modeling","name":"Modeling","active":true}]`)
			}
		case "/api/data/projects/reviewer-unconnected/task-types":
			_, _ = io.WriteString(w, `[{"id":"task-live","name":"Live Task Type","active":true}]`)
		case "/api/data/task-status/":
			_, _ = io.WriteString(w, `[{"id":"status-wfa","name":"Waiting For Approval","short_name":"WFA","is_retake":false,"is_done":false},{"id":"status-retake","name":"Retake","short_name":"Retake","is_retake":true,"is_done":false},{"id":"status-done","name":"Done","short_name":"Done","is_retake":false,"is_done":true},{"id":"status-wip","name":"Work In Progress","short_name":"WIP","is_retake":false,"is_done":false}]`)
		default:
			if strings.HasPrefix(r.URL.Path, "/api/data/persons/") {
				personID := strings.TrimPrefix(r.URL.Path, "/api/data/persons/")
				person, ok := reviewerBrowserPersonDetail(personID)
				if !ok {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(person)
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer kitsuFixture.Close()
	kitsuIP := netip.MustParseAddr("127.0.0.1")
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: kitsuFixture.URL, PinnedIPs: []netip.Addr{kitsuIP}}); err != nil {
		t.Fatal("pin synthetic Kitsu service to loopback")
	}
	model.SetSetting(db, "kitsu.hostname", kitsuFixture.URL)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, kitsuFixture.URL+"/api")
	if err := setRuntimeKitsuToken(db, reviewerBrowserToken); err != nil {
		t.Fatal("store synthetic runtime Kitsu token")
	}
	if err := setRuntimeDiscordBotToken(db, reviewerBrowserBot); err != nil {
		t.Fatal("store synthetic Discord token")
	}
	// The forum page's status lister must read the live Kitsu status list
	// through the runtime credential source the same way the Task Type lister
	// does. Its default reads the cleared legacy KitsuJWTToken environment, so
	// without this override the page silently falls back to saved statuses.
	oldForumStatusLister := forumStatusLister
	t.Cleanup(func() { forumStatusLister = oldForumStatusLister })
	forumStatusLister = func(db *gorm.DB) ([]kitsu.TaskStatus, error) {
		baseURL, token, ok := runtimeKitsuDataSource(db)
		if !ok {
			return nil, fmt.Errorf("Kitsu runtime data source is unavailable")
		}
		statuses := []kitsu.TaskStatus{}
		_, err := request.DoWithError(token, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/data/task-status/", nil, &statuses)
		return statuses, err
	}
	project := model.Project{KitsuProjectID: "reviewer-production", Name: "Synthetic Review Production", DiscordGuildID: reviewerBrowserGuild, DiscordCategoryID: reviewerBrowserCategory, StorageURL: "https://storage.synthetic.invalid"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal("seed disposable Production")
	}
	model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: "TVアニメ『佐々木さんとピーちゃんSeason2』第1話 制作進行記録", EntityName: "Storyboard", TaskType: "Storyboard", Success: true, CreatedAt: time.Now()})
	settings := reviewerBrowserForumSettings(project.KitsuProjectID, project.DiscordGuildID, []model.ProductionForumRoute{
		{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", ForumChannelID: "66666666666666661", ForumChannelName: "compositing"},
		{ProductionID: project.KitsuProjectID, TaskTypeID: "task-animation", TaskTypeName: "Animation", ForumChannelID: "66666666666666662", ForumChannelName: "animation"},
	})
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatal("seed synthetic forum routing")
	}
	model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: project.Name, EntityName: "Storyboard", TaskType: "Storyboard", Success: true, CreatedAt: time.Now()})
	model.WriteAuditLog(db, model.AuditLog{ProjectID: "other-production", ProjectName: project.Name, EntityName: "Must not leak", Success: true, CreatedAt: time.Now()})
	model.RecordNotificationRoutingDiagnosis(db, model.NotificationRoutingDiagnosis{ProductionID: project.KitsuProjectID, Reason: "synthetic route check", Detail: "Synthetic route needs review", CreatedAt: time.Now()})
	seedBrowserMonitoringCycles := func() {
		cycleAt := time.Now().Truncate(time.Millisecond)
		Stats.mu.Lock()
		Stats.apiObservations = make(map[string][]APIObservation)
		Stats.mu.Unlock()
		for _, age := range []time.Duration{65 * time.Second, 45 * time.Second, 25 * time.Second} {
			Stats.RecordAPIObservationBatch(map[string]APIObservation{
				"kitsu":   {At: cycleAt.Add(-age), Duration: 25 * time.Millisecond, Success: true, Classification: "success"},
				"discord": {At: cycleAt.Add(-age), Duration: 35 * time.Millisecond, Success: true, Classification: "success"},
			})
		}
	}
	seedBrowserMonitoringCycles()
	for _, user := range reviewerBrowserUserMaps() {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal("seed disposable global User Linking")
		}
	}
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", model.ReviewerTargetUser, "22222222222222234"); err != nil {
		t.Fatal("seed synthetic additional WFA user")
	}
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", model.ReviewerTargetUser, "22222222222222225"); err != nil {
		t.Fatal("seed synthetic linked User for effective-recipient summary")
	}
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "task-comp", "Compositing", model.ReviewerTargetRole, "33333333333333331"); err != nil {
		t.Fatal("seed synthetic Role for effective-recipient summary")
	}
	if err := model.UpsertProjectReviewerTarget(db, project.ID, "task-animation", "Animation", model.ReviewerTargetRole, "33333333333333331"); err != nil {
		t.Fatal("seed synthetic additional WFA role")
	}

	unexpectedDiscord := make(chan string, 16)
	oldTransport := http.DefaultTransport
	http.DefaultTransport = reviewerBrowserDiscordTransport{
		scenario:      &scenario,
		unexpected:    unexpectedDiscord,
		kitsuHost:     strings.TrimPrefix(kitsuFixture.URL, "http://"),
		expectedGuild: reviewerBrowserGuild,
		baseTransport: oldTransport,
	}
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	scenario.Store("japanese-wfa-add")
	preflight := checkDiscordStatus(reviewerBrowserBot, reviewerBrowserGuild)
	if !preflight.BotValid || !preflight.GuildValid {
		t.Fatalf("Japanese WFA synthetic Bot cannot validate the forum destination: bot=%t guild=%t", preflight.BotValid, preflight.GuildValid)
	}
	scenario.Store("ready")

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("bind isolated acceptance app")
	}
	appURL := "http://" + listener.Addr().String()
	mux := http.NewServeMux()
	mux.Handle("GET /favicon.ico", FaviconHandler())
	mux.Handle("GET /kitsusync.svg", KitsuSyncIconHandler())
	login := LoginRateLimit(http.HandlerFunc(LoginHandlerWithTrustedAuthority(func() KitsuLoginAuthority {
		return KitsuLoginAuthority{RuntimeHost: kitsuFixture.URL, Source: "explicit"}
	}, nil, nil)))
	mux.Handle("/bot/login", login)
	ready := func() bool { return true }
	mux.HandleFunc("/bot/setup", RequireSession(ReadOnlyAuditRoute(ready, func(w http.ResponseWriter, r *http.Request) {
		renderIANewConnection(w, r, db)
	})))
	mux.HandleFunc("/bot/admin", RequireSession(AdminIndexWithRuntime(db, ready)))
	mux.HandleFunc("/bot/admin/users", RequireSession(ReadOnlyAuditRoute(ready, UsersHandler(db, kitsuFixture.URL))))
	mux.Handle("/bot/admin/bot", RequireSession(BotHandlerWithRuntime(db, nil, func() bool { return true })))
	mux.HandleFunc("/bot/admin/projects", RequireSession(ReadOnlyAuditRoute(ready, AdminProjectsHandler(db, reviewerBrowserGuild, reviewerBrowserBot))))
	mux.HandleFunc("/bot/admin/audit", RequireSession(AuditLogHandler(db)))
	mux.HandleFunc("/bot/admin/forum", RequireSession(ForumSettingsHandler(db, func() (string, string) {
		return reviewerBrowserBot, reviewerBrowserGuild
	})))
	mux.HandleFunc("GET /bot/admin/refresh-discord-channels", RequireSession(RefreshDiscordChannelsHandler(func() string {
		return reviewerBrowserBot
	})))
	health := HealthHandler(db)
	mux.HandleFunc("/bot/admin/health", RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The browser suite visits several pages before System Status; reseed
		// nearby shared monitoring cycles at route time so chart assertions
		// measure graph behavior rather than seed age.
		seedBrowserMonitoringCycles()
		health.ServeHTTP(w, r)
	})))
	mux.HandleFunc("/bot/api/setup/observability", RequireSession(TelemetrySnapshotHandler()))
	mux.HandleFunc("/__fixture", func(w http.ResponseWriter, r *http.Request) {
		mode := strings.TrimSpace(r.URL.Query().Get("scenario"))
		switch mode {
		case "ready", "empty-team", "team-failure", "no-matching", "no-linked", "no-roles", "non-mentionable-roles", "bot-can-mention-roles", "roles-forbidden", "roles-rate-limited", "discord-failure", "stale-membership", "user-linking-long-label", "overflow-clip-audit":
			scenario.Store(mode)
			w.WriteHeader(http.StatusNoContent)
		case "overflow-clip-audit":
			// The overflow/clip sweep reseeds the long-URL failure and badges of
			// differing widths so its containment reads the fix, not seed age.
			model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: project.Name, EntityName: "Compositing", TaskType: "Compositing", Success: false, ErrorMessage: "https://kitsu.synthetic.invalid/api/data/projects/50844686-9d53-4e1c-9f47-0f4f3a1b2c3d/task-types/?include=task_status&fields[task-type]=name,for_artist&page[size]=200&page[number]=1&department=Compositing&deadline=2026-11-18", CreatedAt: time.Now()})
			model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: project.Name, EntityName: "Storyboard", TaskType: "Storyboard", Success: true, CreatedAt: time.Now().Add(-time.Minute)})
			model.WriteAuditLog(db, model.AuditLog{ProjectID: project.KitsuProjectID, ProjectName: project.Name, EntityName: "Layout", TaskType: "Layout", Success: true, CreatedAt: time.Now().Add(-2 * time.Minute)})
			scenario.Store("ready")
			w.WriteHeader(http.StatusNoContent)
		case "unconnected-production":
			// The wizard plan step renders only the cached guild channel list,
			// so warm the cache before the click-through reaches step 4.
			scenario.Store(mode)
			if _, _, err := guildChannelListCache.refresh(reviewerBrowserGuild, reviewerBrowserBot); err != nil {
				http.Error(w, "could not prefetch the synthetic guild channel list", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "no-connected-production":
			// Readiness derives ProductionRequired from the absence of connected
			// Production rows, so remove the seeded row to render the unconnected
			// System Status state and restore it with the next fixture call.
			if err := db.Delete(&project).Error; err != nil {
				http.Error(w, "could not remove the seeded Production", http.StatusInternalServerError)
				return
			}
			scenario.Store(mode)
			w.WriteHeader(http.StatusNoContent)
		case "restore-connected-production":
			if err := db.Create(&project).Error; err != nil {
				http.Error(w, "could not restore the seeded Production", http.StatusInternalServerError)
				return
			}
			scenario.Store("ready")
			w.WriteHeader(http.StatusNoContent)
		case "japanese-wfa-add":
			if err := db.Model(&project).Updates(map[string]interface{}{"name": "aa"}).Error; err != nil {
				http.Error(w, "could not rename synthetic Production", http.StatusInternalServerError)
				return
			}
			project.Name = "aa"
			settings := reviewerBrowserForumSettings(project.KitsuProjectID, project.DiscordGuildID, []model.ProductionForumRoute{
				{ProductionID: project.KitsuProjectID, TaskTypeID: "task-storyboard", TaskTypeName: "Storyboard", ForumChannelID: "66666666666666663", ForumChannelName: "storyboard"},
			})
			if err := model.SaveProductionForumSettings(db, settings); err != nil {
				http.Error(w, "could not configure Japanese WFA forum route", http.StatusInternalServerError)
				return
			}
			if err := db.Where("project_id = ?", project.ID).Delete(&model.ProjectReviewerTarget{}).Error; err != nil {
				http.Error(w, "could not reset Japanese WFA targets", http.StatusInternalServerError)
				return
			}
			scenario.Store(mode)
			w.WriteHeader(http.StatusNoContent)
		case "sparse-production":
			settings := reviewerBrowserForumSettings(project.KitsuProjectID, project.DiscordGuildID, []model.ProductionForumRoute{
				{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", ForumChannelID: "66666666666666661", ForumChannelName: "compositing"},
			})
			if model.SaveProductionForumSettings(db, settings) != nil {
				http.Error(w, "could not configure sparse Production fixture", http.StatusInternalServerError)
				return
			}
			if err := db.Where("project_id = ?", project.ID).Delete(&model.ProjectReviewerTarget{}).Error; err != nil {
				http.Error(w, "could not clear sparse Reviewer fixture", http.StatusInternalServerError)
				return
			}
			if err := db.Where("project_id = ?", project.KitsuProjectID).Delete(&model.AuditLog{}).Error; err != nil {
				http.Error(w, "could not clear sparse activity fixture", http.StatusInternalServerError)
				return
			}
			if err := db.Where("production_id = ?", project.KitsuProjectID).Delete(&model.NotificationRoutingDiagnosis{}).Error; err != nil {
				http.Error(w, "could not clear sparse issue fixture", http.StatusInternalServerError)
				return
			}
			scenario.Store(mode)
			w.WriteHeader(http.StatusNoContent)
		case "forum-settings":
			// Start from one routed forum so the page shows a real saved value
			// and the Animation row left unrouted for the browser run to fill
			// in. Every status carries a role and the routed forum carries all
			// four tags, so the page's own Save re-passes validation after the
			// edit. The fixture answers the live Discord channel list from the
			// same forums, so the page offers the unrouted forum's tags too.
			settings := model.ProductionForumSettings{
				Config: model.ProductionForumConfig{ProductionID: project.KitsuProjectID, GuildID: project.DiscordGuildID},
				Routes: []model.ProductionForumRoute{
					{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", ForumChannelID: "66666666666666661", ForumChannelName: "compositing"},
				},
				StatusRoles: []model.ProductionForumStatusRole{
					{ProductionID: project.KitsuProjectID, StatusID: "status-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
					{ProductionID: project.KitsuProjectID, StatusID: "status-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
					{ProductionID: project.KitsuProjectID, StatusID: "status-done", StatusName: "Done", Role: model.ForumRoleDone},
				},
				Tags: []model.ProductionForumTag{
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleWFA, TagID: "66666666666666661-wfa"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleRetake, TagID: "66666666666666661-retake"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleDone, TagID: "66666666666666661-done"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleWIP, TagID: "66666666666666661-wip"},
				},
			}
			if err := model.SaveProductionForumSettings(db, settings); err != nil {
				http.Error(w, "could not configure forum settings fixture", http.StatusInternalServerError)
				return
			}
			scenario.Store("ready")
			// The forum settings page renders only the cached guild channel
			// list, so warm the cache before the page asserts its live forum
			// options.
			if _, _, err := guildChannelListCache.refresh(reviewerBrowserGuild, reviewerBrowserBot); err != nil {
				http.Error(w, "could not prefetch the synthetic guild channel list", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "forum-zero-touch":
			// A Production whose forum setup was never finished: no saved forum
			// settings at all, so the browser run proves one press of the Bot
			// auto-create button finishes the whole setup from template defaults.
			for _, table := range []any{
				&model.ProductionForumConfig{},
				&model.ProductionForumRoute{},
				&model.ProductionForumStatusRole{},
				&model.ProductionForumTag{},
			} {
				if err := db.Where("production_id = ?", project.KitsuProjectID).Delete(table).Error; err != nil {
					http.Error(w, "could not clear the forum zero-touch fixture", http.StatusInternalServerError)
					return
				}
			}
			scenario.Store("ready")
			w.WriteHeader(http.StatusNoContent)
		case "restore-routing":
			// The forum settings page replaces the Production's whole routing, so
			// the browser run puts the seeded mapping back before it re-reads the
			// Notifications tab.
			settings := model.ProductionForumSettings{
				Config: model.ProductionForumConfig{ProductionID: project.KitsuProjectID, GuildID: project.DiscordGuildID},
				Routes: []model.ProductionForumRoute{
					{ProductionID: project.KitsuProjectID, TaskTypeID: "task-comp", TaskTypeName: "Compositing", ForumChannelID: "66666666666666661", ForumChannelName: "compositing"},
					{ProductionID: project.KitsuProjectID, TaskTypeID: "task-animation", TaskTypeName: "Animation", ForumChannelID: "66666666666666662", ForumChannelName: "animation"},
				},
				StatusRoles: []model.ProductionForumStatusRole{
					{ProductionID: project.KitsuProjectID, StatusID: "status-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
					{ProductionID: project.KitsuProjectID, StatusID: "status-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
					{ProductionID: project.KitsuProjectID, StatusID: "status-done", StatusName: "Done", Role: model.ForumRoleDone},
				},
				Tags: []model.ProductionForumTag{
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleWFA, TagID: "66666666666666661-wfa"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleRetake, TagID: "66666666666666661-retake"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleDone, TagID: "66666666666666661-done"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666661", Role: model.ForumRoleWIP, TagID: "66666666666666661-wip"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666662", Role: model.ForumRoleWFA, TagID: "66666666666666662-wfa"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666662", Role: model.ForumRoleRetake, TagID: "66666666666666662-retake"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666662", Role: model.ForumRoleDone, TagID: "66666666666666662-done"},
					{ProductionID: project.KitsuProjectID, ForumChannelID: "66666666666666662", Role: model.ForumRoleWIP, TagID: "66666666666666662-wip"},
				},
			}
			if err := model.SaveProductionForumSettings(db, settings); err != nil {
				http.Error(w, "could not restore the seeded routing", http.StatusInternalServerError)
				return
			}
			scenario.Store("ready")
			w.WriteHeader(http.StatusNoContent)
		case "channels-warm":
			// Hand the following flows a fresh cache and the healthy scenario.
			scenario.Store("ready")
			if _, _, err := guildChannelListCache.refresh(reviewerBrowserGuild, reviewerBrowserBot); err != nil {
				http.Error(w, "could not refresh the synthetic guild channel list", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "channels-rate-limited", "channels-unconnected":
			// Drop the cached list so the next render meets a cold cache while
			// Discord answers 429 for the channel list.
			guildChannelListCache.invalidate(reviewerBrowserGuild)
			scenario.Store(mode)
			w.WriteHeader(http.StatusNoContent)
		case "channels-stale-rate-limited":
			// Warm the cache, record a rate-limited refresh failure against the
			// kept body, then expire the entry: the next render must show the
			// cached channels with the rate-limit stale notice.
			scenario.Store("ready")
			if _, _, err := guildChannelListCache.refresh(reviewerBrowserGuild, reviewerBrowserBot); err != nil {
				http.Error(w, "could not prefetch the synthetic guild channel list", http.StatusInternalServerError)
				return
			}
			scenario.Store("channels-rate-limited")
			guildChannelListCache.refresh(reviewerBrowserGuild, reviewerBrowserBot)
			guildChannelListCache.mu.Lock()
			if entry := guildChannelListCache.entries[reviewerBrowserGuild]; entry != nil {
				entry.fetchedAt = time.Now().Add(-guildChannelCacheTTL - time.Minute)
			}
			guildChannelListCache.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unknown isolated fixture state", http.StatusBadRequest)
		}
	})
	server := &http.Server{Handler: RequestBodyLimit(RequestTrace(CSRFProtection(mux))), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	cmd := exec.Command("node", filepath.Join(workDir, "tools", "reviewer-browser-acceptance", "browser.cjs"))
	cmd.Env = append(os.Environ(),
		"KITSUSYNC_REVIEWER_BROWSER_URL="+appURL,
		"KITSUSYNC_REVIEWER_BROWSER_OUTPUT="+outputDir,
		"KITSUSYNC_REVIEWER_BROWSER_GUILD="+reviewerBrowserGuild,
		"KITSUSYNC_REVIEWER_BROWSER_SHA="+strings.TrimSpace(os.Getenv("KITSUSYNC_REVIEWER_BROWSER_SHA")),
	)
	combined, err := cmd.CombinedOutput()
	safeOutput := string(combined)
	for _, sensitive := range []string{reviewerBrowserToken, reviewerBrowserBot, reviewerBrowserPass, "synthetic-manager-session-token"} {
		safeOutput = strings.ReplaceAll(safeOutput, sensitive, "[redacted]")
	}
	if err != nil {
		if scenario.Load().(string) == "japanese-wfa-add" {
			t.Fatalf("isolated Chromium acceptance failed: %s; Japanese WFA forum route: %s", safeOutput, func() string {
				settings, ok, loadErr := model.LoadProductionForumSettings(db, project.KitsuProjectID)
				if loadErr != nil || !ok {
					return "forum settings unavailable"
				}
				return fmt.Sprintf("%d routes", len(settings.Routes))
			}())
		}
		t.Fatalf("isolated Chromium acceptance failed: %s", safeOutput)
	}
	var japaneseTargets []model.ProjectReviewerTarget
	if err := db.Where("project_id = ? AND task_type_id = ?", project.ID, "task-storyboard").Order("target_kind asc, discord_id asc").Find(&japaneseTargets).Error; err != nil {
		t.Fatal("read back persisted Japanese WFA targets")
	}
	if len(japaneseTargets) != 2 || japaneseTargets[0].TargetKind != model.ReviewerTargetRole || japaneseTargets[0].DiscordID != "33333333333333331" || japaneseTargets[1].TargetKind != model.ReviewerTargetUser || japaneseTargets[1].DiscordID != "22222222222222238" {
		t.Fatalf("Japanese Storyboard recipient flow did not persist the exact User/Role IDs: %#v", japaneseTargets)
	}
	if len(unexpectedDiscord) != 0 {
		t.Fatalf("synthetic Discord fixture blocked an unexpected route: %s", <-unexpectedDiscord)
	}
}

// reviewerBrowserChannelID derives a stable synthetic Discord channel id from
// a name, so a retried create for the same forum reuses the same id.
func reviewerBrowserChannelID(name string) string {
	sum := fnv.New32a()
	sum.Write([]byte(name))
	return "77777777777777" + fmt.Sprintf("%05d", sum.Sum32()%100000)
}

// reviewerBrowserForumSettings builds a complete, validatable forum settings
// fixture (SaveProductionForumSettings rejects incomplete configurations).
func reviewerBrowserForumSettings(projectID, guildID string, routes []model.ProductionForumRoute) model.ProductionForumSettings {
	settings := model.ProductionForumSettings{Config: model.ProductionForumConfig{ProductionID: projectID, GuildID: guildID}, Routes: routes}
	for _, route := range routes {
		for _, role := range []string{model.ForumRoleWFA, model.ForumRoleRetake, model.ForumRoleDone, model.ForumRoleWIP} {
			settings.StatusRoles = append(settings.StatusRoles, model.ProductionForumStatusRole{ProductionID: projectID, StatusID: route.ForumChannelID + "-" + role, StatusName: role, Role: role})
			settings.Tags = append(settings.Tags, model.ProductionForumTag{ProductionID: projectID, ForumChannelID: route.ForumChannelID, Role: role, TagID: route.ForumChannelID + "-" + role})
		}
	}
	return settings
}

func reviewerBrowserTeam() []kitsu.Person {
	return []kitsu.Person{
		{ID: "person-promotion", FullName: "Project Supervisor", Email: "promotion@synthetic.invalid", Active: true, Role: "artist", ProjectRole: "supervisor", Departments: []string{"dept-comp"}},
		{ID: "person-no-department", FullName: "Departmentless Supervisor", Email: "no-dept@synthetic.invalid", Active: true, Role: "supervisor"},
		{ID: "person-wrong-department", FullName: "Wrong Department Supervisor", Email: "wrong-dept@synthetic.invalid", Active: true, Role: "supervisor"},
		{ID: "person-artist", FullName: "Synthetic Artist", Email: "artist@synthetic.invalid", Active: true, Role: "artist"},
		{ID: "person-manager", FullName: "Production Manager", Email: "manager@synthetic.invalid", Active: true, Role: "manager"},
		{ID: "person-global-admin", FullName: "Global Admin", Email: "admin@synthetic.invalid", Active: true, Role: "admin", ProjectRole: "supervisor"},
		{ID: "person-demoted", FullName: "Demoted Supervisor", Email: "demoted@synthetic.invalid", Active: true, Role: "supervisor", ProjectRole: "artist"},
		{ID: "person-project-manager", FullName: "Project Manager Override", Email: "project-manager@synthetic.invalid", Active: true, Role: "supervisor", ProjectRole: "manager"},
		{ID: "person-position-only", FullName: "Position Only", Email: "position@synthetic.invalid", Active: true, Role: "artist", Data: `{"position":"Supervisor"}`},
		{ID: "person-inactive", FullName: "Inactive Supervisor", Email: "inactive@synthetic.invalid", Active: false, Role: "supervisor"},
		{ID: "person-bot", FullName: "Kitsu Bot", Email: "bot@synthetic.invalid", Active: true, IsBot: true, Role: "supervisor"},
		{ID: "person-unlinked", FullName: "Unlinked Supervisor", Email: "unlinked@synthetic.invalid", Active: true, Role: "supervisor"},
		{ID: "person-non-guild", FullName: "Non Guild User", Email: "non-guild@synthetic.invalid", Active: true, Role: "artist"},
		{ID: "person-override", FullName: "Override Candidate", Email: "override@synthetic.invalid", Active: true, Role: "artist"},
		{ID: "person-global-name", FullName: "Global Name Supervisor", Email: "global-name@synthetic.invalid", Active: true, Role: "supervisor"},
		{ID: "person-username", FullName: "Username Supervisor", Email: "username@synthetic.invalid", Active: true, Role: "supervisor"},
	}
}

func reviewerBrowserPeople() []kitsu.Person {
	people := reviewerBrowserTeam()
	people = append(people, kitsu.Person{ID: "person-not-team", FullName: "Not a Team Member", Email: "non-team@synthetic.invalid", Active: true, Role: "supervisor"})
	return people
}

func reviewerBrowserPersonDetail(personID string) (kitsu.Person, bool) {
	if personID == "person-japanese" {
		return kitsu.Person{ID: "person-japanese", FullName: "日本語ユーザー", Email: "japanese@synthetic.invalid", Active: true, Role: "artist"}, true
	}
	departments := map[string][]string{
		"person-promotion":        {"dept-comp"},
		"person-no-department":    {},
		"person-wrong-department": {"dept-animation"},
		"person-artist":           {"dept-comp"},
		"person-manager":          {"dept-comp"},
		"person-global-admin":     {"dept-comp"},
		"person-inactive":         {"dept-comp"},
		"person-bot":              {"dept-comp"},
		"person-unlinked":         {"dept-comp"},
		"person-non-guild":        {"dept-comp"},
		"person-override":         {"dept-comp"},
		"person-global-name":      {"dept-comp"},
		"person-username":         {"dept-comp"},
	}
	for _, person := range reviewerBrowserPeople() {
		if person.ID == personID {
			person.Departments = departments[personID]
			return person, true
		}
	}
	return kitsu.Person{}, false
}

func reviewerBrowserUserMaps() []model.UserMap {
	users := []model.UserMap{
		{KitsuID: "person-promotion", KitsuName: "Project Supervisor", KitsuEmail: "promotion@synthetic.invalid", DiscordID: "22222222222222222", DiscordDisplayName: "Global Supervisor Name"},
		{KitsuID: "person-no-department", KitsuName: "Departmentless Supervisor", KitsuEmail: "no-dept@synthetic.invalid", DiscordID: "22222222222222223", DiscordDisplayName: "Departmentless Global"},
		{KitsuID: "person-wrong-department", KitsuName: "Wrong Department Supervisor", KitsuEmail: "wrong-dept@synthetic.invalid", DiscordID: "22222222222222224", DiscordDisplayName: "Wrong Department Global"},
		{KitsuID: "person-artist", KitsuName: "Synthetic Artist", KitsuEmail: "artist@synthetic.invalid", DiscordID: "22222222222222225", DiscordDisplayName: "Artist Global"},
		{KitsuID: "person-manager", KitsuName: "Production Manager", KitsuEmail: "manager@synthetic.invalid", DiscordID: "22222222222222226", DiscordDisplayName: "Manager Global"},
		{KitsuID: "person-global-admin", KitsuName: "Global Admin", KitsuEmail: "admin@synthetic.invalid", DiscordID: "22222222222222227", DiscordDisplayName: "Admin Global"},
		{KitsuID: "person-demoted", KitsuName: "Demoted Supervisor", KitsuEmail: "demoted@synthetic.invalid", DiscordID: "22222222222222228", DiscordDisplayName: "Demoted Global"},
		{KitsuID: "person-project-manager", KitsuName: "Project Manager Override", KitsuEmail: "project-manager@synthetic.invalid", DiscordID: "22222222222222229", DiscordDisplayName: "Project Manager Global"},
		{KitsuID: "person-position-only", KitsuName: "Position Only", KitsuEmail: "position@synthetic.invalid", DiscordID: "22222222222222230", DiscordDisplayName: "Position Global"},
		{KitsuID: "person-inactive", KitsuName: "Inactive Supervisor", KitsuEmail: "inactive@synthetic.invalid", DiscordID: "22222222222222231", DiscordDisplayName: "Inactive Global"},
		{KitsuID: "person-bot", KitsuName: "Kitsu Bot", KitsuEmail: "bot@synthetic.invalid", DiscordID: "22222222222222232", DiscordDisplayName: "Bot Global"},
		{KitsuID: "person-non-guild", KitsuName: "Non Guild User", KitsuEmail: "non-guild@synthetic.invalid", DiscordID: "22222222222222233", DiscordDisplayName: "Non Guild Global"},
		{KitsuID: "person-not-team", KitsuName: "Not a Team Member", KitsuEmail: "non-team@synthetic.invalid", DiscordID: "22222222222222235", DiscordDisplayName: "Non Team Global"},
		{KitsuID: "person-override", KitsuName: "Override Candidate", KitsuEmail: "override@synthetic.invalid", DiscordID: "22222222222222234", DiscordDisplayName: "Override Global"},
		{KitsuID: "person-global-name", KitsuName: "Global Name Supervisor", KitsuEmail: "global-name@synthetic.invalid", DiscordID: "22222222222222236", DiscordDisplayName: "Global Name Fallback"},
		{KitsuID: "person-username", KitsuName: "Username Supervisor", KitsuEmail: "username@synthetic.invalid", DiscordID: "22222222222222237", DiscordDisplayName: "Username Fallback"},
		{KitsuID: "person-japanese", KitsuName: "日本語ユーザー", KitsuEmail: "japanese@synthetic.invalid", DiscordID: "22222222222222238", DiscordDisplayName: "日本語表示名"},
	}
	return users
}

type reviewerBrowserDiscordTransport struct {
	scenario      *atomic.Value
	unexpected    chan<- string
	kitsuHost     string
	expectedGuild string
	baseTransport http.RoundTripper
}

func (d reviewerBrowserDiscordTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme == "http" && r.URL.Host == d.kitsuHost && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/") {
		return d.baseTransport.RoundTrip(r)
	}
	if r.URL.Host != "discord.com" || r.Header.Get("Authorization") != "Bot "+reviewerBrowserBot {
		select {
		case d.unexpected <- r.Method + " " + r.URL.Host + r.URL.Path:
		default:
		}
		return nil, fmt.Errorf("blocked unexpected synthetic Discord request")
	}
	status, body := http.StatusOK, "[]"
	switch {
	case r.URL.Path == "/api/v10/users/@me":
		body = `{"id":"11111111111111113","username":"Synthetic KitsuSync Bot"}`
	case r.URL.Path == "/api/v10/guilds/"+d.expectedGuild:
		body = `{"id":"` + d.expectedGuild + `","name":"Synthetic Discord"}`
	case r.URL.Path == "/api/v10/users/@me/guilds":
		body = `[{"id":"11111111111111111","name":"Synthetic Discord"},{"id":"11111111111111112","name":"Other Synthetic Discord"}]`
	case r.URL.Path == "/api/v10/guilds/"+d.expectedGuild+"/members/11111111111111113":
		botRoles := `[]`
		if d.scenario.Load().(string) == "japanese-wfa-add" {
			botRoles = ` ["33333333333333334"]`
		}
		body = `{"user":{"id":"11111111111111113"},"roles":` + strings.TrimSpace(botRoles) + `}`
	case strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/members"):
		if d.scenario.Load().(string) == "discord-failure" {
			status, body = http.StatusForbidden, `{"message":"synthetic failure"}`
		} else {
			body = `[{"user":{"id":"22222222222222222","username":"project-supervisor","global_name":"Global Supervisor Name"},"nick":"Guild Nick Supervisor"},{"user":{"id":"22222222222222223","username":"departmentless","global_name":"Departmentless Global"}},{"user":{"id":"22222222222222224","username":"wrong-department","global_name":"Wrong Department Global"}},{"user":{"id":"22222222222222225","username":"artist","global_name":"Artist Global"}},{"user":{"id":"22222222222222226","username":"manager","global_name":"Manager Global"}},{"user":{"id":"22222222222222227","username":"admin","global_name":"Admin Global"}},{"user":{"id":"22222222222222228","username":"demoted","global_name":"Demoted Global"}},{"user":{"id":"22222222222222229","username":"project-manager","global_name":"Project Manager Global"}},{"user":{"id":"22222222222222230","username":"position-only"}},{"user":{"id":"22222222222222231","username":"inactive"}},{"user":{"id":"22222222222222232","username":"kitsu-bot","bot":true}},{"user":{"id":"22222222222222234","username":"override-candidate","global_name":"Override Global"},"nick":"Guild Nick Override"},{"user":{"id":"22222222222222236","username":"global-name-fallback","global_name":"Global Name Fallback"}},{"user":{"id":"22222222222222237","username":"username-fallback"}}]`
			if d.scenario.Load().(string) == "japanese-wfa-add" {
				body = strings.TrimSuffix(body, "]") + `,{"user":{"id":"22222222222222238","username":"nihongo-user","global_name":"日本語表示名"},"nick":"日本語ユーザー"}]`
			}
			if scenario := d.scenario.Load().(string); scenario == "user-linking-long-label" || scenario == "overflow-clip-audit" {
				body = strings.Replace(body, `"nick":"Guild Nick Supervisor"`, `"nick":"`+userLinkingLongNickname+`"`, 1)
			}
		}
	case strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/roles"):
		if r.URL.Path != "/api/v10/guilds/"+d.expectedGuild+"/roles" {
			select {
			case d.unexpected <- r.Method + " " + r.URL.Host + r.URL.Path:
			default:
			}
			return nil, fmt.Errorf("blocked synthetic Discord role lookup outside the configured Production Guild")
		}
		switch d.scenario.Load().(string) {
		case "no-roles":
			body = `[]`
		case "non-mentionable-roles":
			body = `[{"id":"11111111111111111","name":"@everyone","position":0,"mentionable":true,"permissions":"3072"},{"id":"33333333333333332","name":"Private","position":4,"mentionable":false,"permissions":"0"}]`
		case "bot-can-mention-roles":
			body = `[{"id":"11111111111111111","name":"@everyone","position":0,"mentionable":true,"permissions":"134144"},{"id":"33333333333333332","name":"Private","position":4,"mentionable":false,"permissions":"0"}]`
		case "roles-forbidden":
			status, body = http.StatusForbidden, `{"message":"synthetic failure"}`
		case "roles-rate-limited":
			status, body = http.StatusTooManyRequests, `{"message":"synthetic failure"}`
		default:
			body = `[{"id":"11111111111111111","name":"@everyone","position":0,"mentionable":true,"permissions":"3072"},{"id":"33333333333333331","name":"Reviewers","position":5,"mentionable":true,"permissions":"0"},{"id":"33333333333333332","name":"Private","position":4,"mentionable":false,"permissions":"0"},{"id":"33333333333333333","name":"Review Leads","position":6,"mentionable":true,"permissions":"0"}]`
			if d.scenario.Load().(string) == "japanese-wfa-add" {
				body = strings.TrimSuffix(body, `]`) + `,{"id":"33333333333333334","name":"KitsuSync Bot","position":10,"mentionable":false,"permissions":"536874000"}]`
			}
		}
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/channels"):
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || strings.TrimSpace(payload.Name) == "" {
			select {
			case d.unexpected <- r.Method + " " + r.URL.Host + r.URL.Path:
			default:
			}
			return nil, fmt.Errorf("blocked synthetic Discord channel create with an invalid payload")
		}
		// The created forum id is derived from the name, so the same forum name
		// always maps to the same synthetic channel and a retry reuses it.
		id := reviewerBrowserChannelID(payload.Name)
		body = `{"id":"` + id + `","name":"` + payload.Name + `","guild_id":"` + d.expectedGuild + `","type":15,"parent_id":"` + reviewerBrowserCategory + `","available_tags":[{"id":"` + id + `-wfa","name":"WFA"},{"id":"` + id + `-retake","name":"RETAKE"},{"id":"` + id + `-done","name":"DONE"},{"id":"` + id + `-wip","name":"WIP"}]}`
	case strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/channels"):
		// Compositing and animation carry the four standard tags (the names the
		// save derives the role mapping from); storyboard keeps human-readable tag
		// names, so a save routed to it is refused for its missing standard tags.
		body = `[{"id":"66666666666666661","name":"compositing","guild_id":"` + d.expectedGuild + `","type":15,"permission_overwrites":[],"available_tags":[{"id":"66666666666666661-wfa","name":"WFA"},{"id":"66666666666666661-retake","name":"RETAKE"},{"id":"66666666666666661-done","name":"DONE"},{"id":"66666666666666661-wip","name":"WIP"}]},{"id":"66666666666666662","name":"animation","guild_id":"` + d.expectedGuild + `","type":15,"permission_overwrites":[],"available_tags":[{"id":"66666666666666662-wfa","name":"WFA"},{"id":"66666666666666662-retake","name":"RETAKE"},{"id":"66666666666666662-done","name":"DONE"},{"id":"66666666666666662-wip","name":"WIP"}]},{"id":"66666666666666663","name":"review","guild_id":"` + d.expectedGuild + `","type":15,"permission_overwrites":[]},{"id":"66666666666666664","name":"storyboard","guild_id":"` + d.expectedGuild + `","type":15,"permission_overwrites":[],"available_tags":[{"id":"66666666666666664-wfa","name":"Waiting for approval"},{"id":"66666666666666664-retake","name":"Retake"},{"id":"66666666666666664-done","name":"Done"},{"id":"66666666666666664-wip","name":"In progress"}]}]`
		if mode := d.scenario.Load().(string); mode == "channels-rate-limited" || mode == "channels-unconnected" {
			status, body = http.StatusTooManyRequests, `{"message":"synthetic rate limit"}`
		}
	default:
		select {
		case d.unexpected <- r.Method + " " + r.URL.Host + r.URL.Path:
		default:
		}
		return nil, fmt.Errorf("blocked unexpected synthetic Discord request")
	}
	header := http.Header{"Content-Type": []string{"application/json"}}
	if status == http.StatusTooManyRequests {
		header.Set("Retry-After", "0.001")
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
