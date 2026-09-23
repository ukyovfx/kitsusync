package setup

import (
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

	"app/src/model"
	"app/src/utils/request"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	premergeKitsuToken   = "synthetic-kitsu-browser-token"
	premergeDiscordToken = "synthetic-discord-browser-token"
	premergeSessionEmail = "acceptance-manager@example.invalid"
)

// TestPremergeBrowserAcceptance runs the actual setup handlers behind their
// normal session and CSRF middleware, then drives them with Chromium. The
// temporary manager session is created through the package's SQLite-backed
// session API; no production auth material or product auth bypass is used.
func TestPremergeBrowserAcceptance(t *testing.T) {
	if os.Getenv("KITSUSYNC_PREMERGE_BROWSER") != "1" {
		t.Skip("acceptance-only authenticated browser test")
	}
	workDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal("resolve repository root")
	}
	tmp := t.TempDir()
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(tmp, "runtime-secret.key"))
	t.Setenv("KitsuJWTToken", "")
	t.Setenv("KITSU_API_BASE_URL", "")
	t.Setenv("KITSU_HOSTNAME", "")
	t.Setenv("DISCORD_BOT_TOKEN", "")
	db, err := gorm.Open(sqlite.Open(filepath.Join(tmp, "acceptance.sqlite")), &gorm.Config{})
	if err != nil {
		t.Fatal("open disposable SQLite database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("get disposable SQLite handle")
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Setting{}, &model.AdminSession{}, &model.UserMap{}, &model.Project{}, &model.ProjectWebhook{}, &model.ProjectSetting{}); err != nil {
		t.Fatal("migrate disposable SQLite schema")
	}
	ConfigureSessionStore(db)
	t.Cleanup(func() {
		ConfigureSessionStore(nil)
		sessionMu.Lock()
		sessions = map[string]sessionData{}
		sessionMu.Unlock()
		_ = sqlDB.Close()
	})

	var scenario atomic.Value
	scenario.Store("ready")
	kitsuFixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/data/") && r.Header.Get("Authorization") != "Bearer "+premergeKitsuToken {
			http.Error(w, "synthetic credential did not reach Kitsu fixture", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/status":
			_, _ = fmt.Fprint(w, `{"name":"Zou","database-up":true,"event-stream-up":true,"key-value-store-up":true,"version":"1.0.67"}`)
		case "/api/auth/authenticated":
			_, _ = fmt.Fprint(w, `{"authenticated":true,"user":{"id":"synthetic-kitsu-bot","full_name":"Synthetic Kitsu Bot","is_bot":true,"active":true}}`)
		case "/api/data/projects/":
			if scenario.Load().(string) == "kitsu_failure" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if scenario.Load().(string) == "kitsu_empty" {
				_, _ = fmt.Fprint(w, `[]`)
				return
			}
			_, _ = fmt.Fprint(w, `[{"id":"synthetic-production-1","name":"Synthetic Production"}]`)
		case "/api/data/projects/synthetic-production-1/task-types":
			_, _ = fmt.Fprint(w, `[{"id":"synthetic-task-type-1","name":"Synthetic Comp","active":true}]`)
		case "/api/data/persons/":
			switch scenario.Load().(string) {
			case "kitsu_failure":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = fmt.Fprint(w, `{"detail":"synthetic unauthorized fixture"}`)
			case "kitsu_empty":
				_, _ = fmt.Fprint(w, `[]`)
			default:
				_, _ = fmt.Fprint(w, `[{"id":"synthetic-person-1","full_name":"Synthetic Human","email":"human@example.invalid","active":true},{"id":"synthetic-kitsu-bot","full_name":"Synthetic Bot Identity","email":"bot@example.invalid","active":true,"is_bot":true},{"id":"synthetic-xss-person","full_name":"\"><img src=x onerror=alert(1)>","email":"xss@example.invalid","active":true}]`)
			}
		case "/api/data/task-status/", "/api/data/entities/", "/api/data/entity-types/", "/api/data/task-types/", "/api/data/tasks", "/api/data/comments", "/api/data/projects/synthetic-production-1":
			_, _ = fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer kitsuFixture.Close()
	model.SetSetting(db, "kitsu.hostname", kitsuFixture.URL)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, kitsuFixture.URL)
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: kitsuFixture.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal("pin synthetic Kitsu fixture to loopback")
	}
	if err := setRuntimeKitsuToken(db, premergeKitsuToken); err != nil {
		t.Fatal("persist synthetic Kitsu token")
	}
	if err := setRuntimeDiscordBotToken(db, premergeDiscordToken); err != nil {
		t.Fatal("persist synthetic Discord token")
	}
	_ = os.Unsetenv("KitsuJWTToken")
	_ = os.Unsetenv("DISCORD_BOT_TOKEN")
	if _, exists := os.LookupEnv("KitsuJWTToken"); exists {
		t.Fatal("legacy KitsuJWTToken environment variable must be absent")
	}
	if got := model.GetSetting(db, RuntimeKitsuTokenSettingKey); got == "" {
		t.Fatal("synthetic persisted Kitsu credential missing")
	}

	unexpected := make(chan string, 32)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = premergeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "discord.com" {
			select {
			case unexpected <- r.Method + " " + r.URL.Scheme + "://" + r.URL.Host + r.URL.Path:
			default:
			}
			return nil, fmt.Errorf("unexpected outbound destination")
		}
		if r.URL.Path == "/api/v10/users/@me/guilds" && r.Method == http.MethodGet {
			if scenario.Load().(string) == "discord_failure" {
				return fixtureResponse(r, http.StatusServiceUnavailable, `{"message":"synthetic fixture failure"}`), nil
			}
			body := `[{"id":"11111111111111111","name":"Synthetic Guild A"}]`
			switch scenario.Load().(string) {
			case "zero_guilds":
				body = `[]`
			case "multiple_guilds":
				body = `[{"id":"11111111111111111","name":"Synthetic Guild A"},{"id":"22222222222222222","name":"Synthetic Guild B"}]`
			}
			return fixtureResponse(r, http.StatusOK, body), nil
		}
		if strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/members") && r.Method == http.MethodGet {
			if scenario.Load().(string) == "discord_failure" {
				return fixtureResponse(r, http.StatusServiceUnavailable, `{"message":"synthetic fixture failure"}`), nil
			}
			body := `[{"user":{"id":"33333333333333333","username":"synthetic-human","global_name":"Synthetic Discord Human"}},{"user":{"id":"44444444444444444","username":"synthetic-discord-bot","bot":true,"global_name":"Synthetic Discord Bot"}}]`
			if scenario.Load().(string) == "zero_humans" {
				body = `[{"user":{"id":"44444444444444444","username":"synthetic-discord-bot","bot":true,"global_name":"Synthetic Discord Bot"}}]`
			}
			return fixtureResponse(r, http.StatusOK, body), nil
		}
		if strings.HasPrefix(r.URL.Path, "/api/v10/guilds/") && strings.HasSuffix(r.URL.Path, "/channels") && r.Method == http.MethodGet {
			return fixtureResponse(r, http.StatusOK, `[]`), nil
		}
		select {
		case unexpected <- r.Method + " https://" + r.URL.Host + r.URL.Path:
		default:
		}
		return nil, fmt.Errorf("unexpected Discord route")
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("bind loopback application")
	}
	addr := listener.Addr().String()
	appURL := "http://" + addr
	ready := func() bool { return true }
	mux := http.NewServeMux()
	mux.Handle("/bot/admin/bot", CSRFProtection(RequireSession(BotHandlerWithRuntime(db, nil, ready))))
	mux.Handle("/bot/admin/users", CSRFProtection(RequireSession(UsersHandler(db, ""))))
	mux.Handle("/bot/setup", CSRFProtection(RequireSession(Handler("", "", "", db, ready, nil))))
	mux.HandleFunc("/__fixture", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("scenario")
		switch mode {
		case "missing_kitsu", "missing_discord", "ready", "kitsu_failure", "kitsu_empty", "discord_failure", "zero_guilds", "multiple_guilds", "zero_humans":
		default:
			http.Error(w, "unknown fixture scenario", http.StatusBadRequest)
			return
		}
		if mode != "missing_kitsu" && model.GetSetting(db, RuntimeKitsuTokenSettingKey) == "" {
			if err := setRuntimeKitsuToken(db, premergeKitsuToken); err != nil {
				http.Error(w, "synthetic Kitsu fixture reset failed", http.StatusInternalServerError)
				return
			}
		}
		if mode != "missing_discord" && model.GetSetting(db, RuntimeDiscordBotTokenSettingKey) == "" {
			if err := setRuntimeDiscordBotToken(db, premergeDiscordToken); err != nil {
				http.Error(w, "synthetic Discord fixture reset failed", http.StatusInternalServerError)
				return
			}
			_ = os.Unsetenv("DISCORD_BOT_TOKEN")
		}
		if mode == "missing_kitsu" {
			if err := model.DeleteSettingWithError(db, RuntimeKitsuTokenSettingKey); err != nil {
				http.Error(w, "synthetic Kitsu fixture reset failed", http.StatusInternalServerError)
				return
			}
		}
		if mode == "missing_discord" {
			if err := model.DeleteSettingWithError(db, RuntimeDiscordBotTokenSettingKey); err != nil {
				http.Error(w, "synthetic Discord fixture reset failed", http.StatusInternalServerError)
				return
			}
		}
		scenario.Store(mode)
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	selfCheck := &http.Client{Transport: previousTransport, Timeout: 3 * time.Second}
	response, err := selfCheck.Get(appURL + "/__fixture?scenario=ready")
	if err != nil {
		t.Fatal("loopback app self-check")
	}
	_ = response.Body.Close()

	session, err := newSessionTokenChecked(premergeSessionEmail, "synthetic-manager-session-token", "manager", "/bot/admin/bot?edit=1")
	if err != nil {
		t.Fatal("create temporary SQLite-backed manager session")
	}
	outputDir := strings.TrimSpace(os.Getenv("KITSUSYNC_ACCEPTANCE_OUTPUT"))
	if outputDir == "" {
		outputDir = filepath.Join(workDir, "tools", "premerge-acceptance", "output")
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		t.Fatal("prepare evidence output")
	}
	if err := os.WriteFile(filepath.Join(outputDir, "harness-authentication.txt"), []byte("auth=temporary SQLite-backed manager session from existing package session API\nruntime_kitsu_credential=persisted encrypted synthetic token\nlegacy_KitsuJWTToken_environment=absent\nproduction_credentials=unused\n"), 0600); err != nil {
		t.Fatal("write non-secret authentication evidence")
	}
	cmd := exec.Command("node", filepath.Join(workDir, "tools", "premerge-acceptance", "browser.cjs"))
	cmd.Env = append(os.Environ(), "KITSUSYNC_ACCEPTANCE_URL="+appURL, "KITSUSYNC_ACCEPTANCE_COOKIE="+session,
		"KITSUSYNC_ACCEPTANCE_OUTPUT="+outputDir, "KITSUSYNC_SYNTH_KITSU="+premergeKitsuToken, "KITSUSYNC_SYNTH_DISCORD="+premergeDiscordToken)
	combined, err := cmd.CombinedOutput()
	safeOutput := string(combined)
	for _, sensitive := range []string{session, premergeKitsuToken, premergeDiscordToken, "synthetic-manager-session-token"} {
		safeOutput = strings.ReplaceAll(safeOutput, sensitive, "[REDACTED]")
	}
	if err != nil {
		t.Fatalf("Chromium acceptance failed (output redacted): %s", safeOutput)
	}
	if len(unexpected) != 0 {
		t.Fatalf("fixture blocked unexpected outbound request: %s", <-unexpected)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "unexpected-outbound.txt"), []byte("none\n"), 0600); err != nil {
		t.Fatal("write safe network evidence")
	}
}

type premergeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f premergeRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
