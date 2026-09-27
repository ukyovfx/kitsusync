package setup

import (
	"encoding/json"
	"fmt"
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
	reviewerBrowserRunEnv = "KITSUSYNC_RUN_REVIEWER_BROWSER"
	reviewerBrowserToken  = "synthetic-reviewer-runtime-token"
	reviewerBrowserBot    = "synthetic-reviewer-discord-token"
	reviewerBrowserEmail  = "manager@synthetic.invalid"
	reviewerBrowserPass   = "synthetic-browser-password"
	reviewerBrowserGuild  = "11111111111111111"
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
		&model.ProjectUserMap{}, &model.ProjectCheckerMap{}, &model.ProjectReviewerTarget{}, &model.ProjectSetting{},
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
			_, _ = io.WriteString(w, `[{"id":"reviewer-production","name":"Synthetic Review Production"}]`)
		case "/api/data/persons/":
			persons := reviewerBrowserPeople()
			_ = json.NewEncoder(w).Encode(persons)
		case "/api/data/projects/reviewer-production/team":
			switch scenario.Load().(string) {
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
			_, _ = io.WriteString(w, `[{"id":"task-comp","name":"Compositing","department_id":"dept-comp","department_name":"Compositing","active":true},{"id":"task-animation","name":"Animation","department_id":"dept-animation","department_name":"Animation","active":true},{"id":"task-unassigned","name":"Unassigned","active":true}]`)
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
	project := model.Project{KitsuProjectID: "reviewer-production", Name: "Synthetic Review Production", DiscordGuildID: reviewerBrowserGuild}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal("seed disposable Production")
	}
	for _, user := range reviewerBrowserUserMaps() {
		if err := db.Create(&user).Error; err != nil {
			t.Fatal("seed disposable global User Linking")
		}
	}

	unexpectedDiscord := make(chan string, 16)
	oldTransport := http.DefaultTransport
	http.DefaultTransport = reviewerBrowserDiscordTransport{scenario: &scenario, unexpected: unexpectedDiscord}
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("bind isolated acceptance app")
	}
	appURL := "http://" + listener.Addr().String()
	mux := http.NewServeMux()
	login := LoginRateLimit(http.HandlerFunc(LoginHandlerWithTrustedAuthority(func() KitsuLoginAuthority {
		return KitsuLoginAuthority{RuntimeHost: kitsuFixture.URL, Source: "explicit"}
	}, nil, nil)))
	mux.Handle("/bot/login", login)
	ready := func() bool { return true }
	mux.HandleFunc("/bot/admin/users", RequireSession(ReadOnlyAuditRoute(ready, UsersHandler(db, kitsuFixture.URL))))
	mux.HandleFunc("/bot/admin/projects", RequireSession(ReadOnlyAuditRoute(ready, AdminProjectsHandler(db, reviewerBrowserGuild, reviewerBrowserBot))))
	mux.HandleFunc("/bot/admin/health", RequireSession(HealthHandler(db)))
	mux.HandleFunc("/__fixture", func(w http.ResponseWriter, r *http.Request) {
		mode := strings.TrimSpace(r.URL.Query().Get("scenario"))
		switch mode {
		case "ready", "empty-team", "team-failure", "no-matching", "no-linked", "no-roles", "discord-failure", "stale-membership":
			scenario.Store(mode)
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
		t.Fatalf("isolated Chromium acceptance failed: %s", safeOutput)
	}
	if len(unexpectedDiscord) != 0 {
		t.Fatalf("synthetic Discord fixture blocked an unexpected route: %s", <-unexpectedDiscord)
	}
}

func reviewerBrowserTeam() []kitsu.Person {
	return []kitsu.Person{
		{ID: "person-promotion", FullName: "Project Supervisor", Email: "promotion@synthetic.invalid", Active: true, Role: "artist", ProjectRole: "supervisor"},
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
	}
	return users
}

type reviewerBrowserDiscordTransport struct {
	scenario   *atomic.Value
	unexpected chan<- string
}

func (d reviewerBrowserDiscordTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "discord.com" || r.Header.Get("Authorization") != "Bot "+reviewerBrowserBot {
		select {
		case d.unexpected <- r.Method + " " + r.URL.Host + r.URL.Path:
		default:
		}
		return nil, fmt.Errorf("blocked unexpected synthetic Discord request")
	}
	status, body := http.StatusOK, "[]"
	switch {
	case r.URL.Path == "/api/v10/users/@me/guilds":
		body = `[{"id":"11111111111111111","name":"Synthetic Discord"}]`
	case strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/members"):
		if d.scenario.Load().(string) == "discord-failure" {
			status, body = http.StatusForbidden, `{"message":"synthetic failure"}`
		} else {
			body = `[{"user":{"id":"22222222222222222","username":"project-supervisor","global_name":"Global Supervisor Name"},"nick":"Guild Nick Supervisor"},{"user":{"id":"22222222222222223","username":"departmentless","global_name":"Departmentless Global"}},{"user":{"id":"22222222222222224","username":"wrong-department","global_name":"Wrong Department Global"}},{"user":{"id":"22222222222222225","username":"artist","global_name":"Artist Global"}},{"user":{"id":"22222222222222226","username":"manager","global_name":"Manager Global"}},{"user":{"id":"22222222222222227","username":"admin","global_name":"Admin Global"}},{"user":{"id":"22222222222222228","username":"demoted","global_name":"Demoted Global"}},{"user":{"id":"22222222222222229","username":"project-manager","global_name":"Project Manager Global"}},{"user":{"id":"22222222222222230","username":"position-only"}},{"user":{"id":"22222222222222231","username":"inactive"}},{"user":{"id":"22222222222222232","username":"kitsu-bot","bot":true}},{"user":{"id":"22222222222222234","username":"override-candidate","global_name":"Override Global"},"nick":"Guild Nick Override"},{"user":{"id":"22222222222222236","username":"global-name-fallback","global_name":"Global Name Fallback"}},{"user":{"id":"22222222222222237","username":"username-fallback"}}]`
		}
	case strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/roles"):
		if d.scenario.Load().(string) == "no-roles" {
			body = `[]`
		} else {
			body = `[{"id":"11111111111111111","name":"@everyone","position":0,"mentionable":true},{"id":"33333333333333331","name":"Reviewers","position":5,"mentionable":true},{"id":"33333333333333332","name":"Private","position":4,"mentionable":false}]`
		}
	default:
		select {
		case d.unexpected <- r.Method + " " + r.URL.Host + r.URL.Path:
		default:
		}
		return nil, fmt.Errorf("blocked unexpected synthetic Discord request")
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
