package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

func TestBatchC_ForumPermissionFailuresSurfaceOnHealthAndClear(t *testing.T) {
	db := newIAViewDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "p-perm", Name: "Perm Prod", DiscordGuildID: "123456789012345678"}).Error; err != nil {
		t.Fatal(err)
	}
	oldGetter := forumPermissionGetter
	t.Cleanup(func() { forumPermissionGetter = oldGetter })

	// Missing Send Messages (1<<11) and Manage Threads (1<<34).
	forumPermissionGetter = func(string, string, string) (int64, error) {
		return (1 << 10) | (1 << 14) | (1 << 16) | (1 << 38), nil
	}
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p-perm", GuildID: "123456789012345678"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p-perm", TaskTypeID: "tt-1", TaskTypeName: "Anim", ForumChannelID: "223456789012345678"}},
	}
	problems := RefreshForumPermissionProblems(db, "p-perm", "Perm Prod", settings, "bot-token")
	if len(problems) == 0 {
		t.Fatal("expected missing forum permissions to be reported")
	}

	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=en", nil), db, time.Minute)
	body := w.Body.String()
	if !strings.Contains(body, "Current problem") || !strings.Contains(body, "Missing forum permissions for Perm Prod (Anim)") || !strings.Contains(body, "Send Messages") {
		t.Fatalf("health page did not surface forum permission problem: %s", body)
	}

	// Now grant all required permissions and re-check.
	forumPermissionGetter = func(string, string, string) (int64, error) {
		return (1 << 10) | (1 << 11) | (1 << 14) | (1 << 16) | (1 << 34) | (1 << 38), nil
	}
	if got := RefreshForumPermissionProblems(db, "p-perm", "Perm Prod", settings, "bot-token"); len(got) != 0 {
		t.Fatalf("expected no problems after permissions granted, got %v", got)
	}
	if remaining := LoadAllForumPermissionProblems(db); len(remaining) != 0 {
		t.Fatalf("expected stored problems to clear, got %+v", remaining)
	}
}

func TestBatchC_CommentsReadDegradedStateReported(t *testing.T) {
	server := httptest.NewServer(zouStatus(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			_, _ = w.Write([]byte(`{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true}`))
			return
		}
		if r.URL.Path == "/api/data/comments" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})))
	defer server.Close()

	res := ValidateKitsuBotToken(nil, server.URL, "test-bot-token", true)
	if !res.Compatible() || !res.HasCommentsFailure() || res.CommentsFailure.ErrorClass != "http_403" {
		t.Fatalf("expected compatible+degraded comments state, got %+v", res)
	}

	db := newIAViewDB(t)
	RecordKitsuCommentsVerificationState(db, res)
	if got := model.GetSetting(db, RuntimeKitsuCommentsErrorSettingKey); got != "http_403" {
		t.Fatalf("expected stored comments error class http_403, got %q", got)
	}
	msg := botTokenCommentsDegradedMessage("en", model.GetSetting(db, RuntimeKitsuCommentsErrorSettingKey))
	if !strings.Contains(msg, "http_403") {
		t.Fatalf("expected classified message with http_403, got %q", msg)
	}
}

func TestBatchC_LoginCSRFEnforcesOriginAndReferer(t *testing.T) {
	handler := CSRFProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/login", "/bot/login"} {
		reqNoOrigin := httptest.NewRequest(http.MethodPost, "http://sync.example.com"+path, strings.NewReader("email=a&password=b"))
		reqNoOrigin.Host = "sync.example.com"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, reqNoOrigin)
		if rr.Code != http.StatusOK {
			t.Fatalf("POST %s without Origin/Referer = %d, want 200", path, rr.Code)
		}

		reqBadOrigin := httptest.NewRequest(http.MethodPost, "http://sync.example.com"+path, strings.NewReader("email=a&password=b"))
		reqBadOrigin.Host = "sync.example.com"
		reqBadOrigin.Header.Set("Origin", "http://evil.example.com")
		rrBad := httptest.NewRecorder()
		handler.ServeHTTP(rrBad, reqBadOrigin)
		if rrBad.Code != http.StatusForbidden {
			t.Fatalf("POST %s with cross-origin = %d, want 403", path, rrBad.Code)
		}

		reqCrossSite := httptest.NewRequest(http.MethodPost, "http://sync.example.com"+path, strings.NewReader("email=a&password=b"))
		reqCrossSite.Host = "sync.example.com"
		reqCrossSite.Header.Set("Sec-Fetch-Site", "cross-site")
		rrCrossSite := httptest.NewRecorder()
		handler.ServeHTTP(rrCrossSite, reqCrossSite)
		if rrCrossSite.Code != http.StatusForbidden {
			t.Fatalf("POST %s with Sec-Fetch-Site: cross-site = %d, want 403", path, rrCrossSite.Code)
		}

		reqOK := httptest.NewRequest(http.MethodPost, "http://sync.example.com"+path, strings.NewReader("email=a&password=b"))
		reqOK.Host = "sync.example.com"
		reqOK.Header.Set("Origin", "http://sync.example.com")
		rrOK := httptest.NewRecorder()
		handler.ServeHTTP(rrOK, reqOK)
		if rrOK.Code != http.StatusOK {
			t.Fatalf("POST %s with same-origin = %d, want 200", path, rrOK.Code)
		}
	}
}

func TestBatchC_WiringKitsuDependencyAndApplicationState(t *testing.T) {
	db := newIAViewDB(t)
	if err := db.Create(&model.Project{KitsuProjectID: "p1", Name: "Show Alpha", DiscordGuildID: "123456789012345678"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Project{KitsuProjectID: "p2", Name: "Show Beta", DiscordGuildID: "123456789012345678"}).Error; err != nil {
		t.Fatal(err)
	}
	_ = model.SaveStudioTaskRoute(db, model.StudioTaskRoute{FromTaskTypeID: "tt-1", NotifyTaskTypeIDs: "tt-2"})
	_ = model.SaveProductionTaskRoute(db, model.ProductionTaskRoute{ProductionID: "p2", FromTaskTypeID: "tt-1", NotifyTaskTypeIDs: model.TaskRouteNone})

	oldLister := handoffTaskTypeLister
	t.Cleanup(func() { handoffTaskTypeLister = oldLister })
	handoffTaskTypeLister = func(*gorm.DB, string) []kitsu.TaskType {
		return []kitsu.TaskType{{ID: "tt-1", Name: "Layout"}, {ID: "tt-2", Name: "Anim"}}
	}

	out := renderStudioRoutesSection(db, "en", httptest.NewRequest(http.MethodGet, "/bot/admin/bot/wiring?lang=en", nil))
	for _, want := range []string{"Status Automations", "lower priority numbers", "Studio default wiring: configured (1 task types)", "Per-production overrides in 1 of 2 Productions: Show Beta"} {
		if !strings.Contains(out, want) {
			t.Fatalf("wiring section missing %q in:\n%s", want, out)
		}
	}
}

func TestBatchC_WFARecipientBasisDisplayed(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "p-wfa", Name: "WFA Show", DiscordGuildID: "123456789012345678"}
	out := renderCurrentIARoutingEditorWithStatus(db, httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=p-wfa&tab=notifications&lang=en", nil), p, "en", "", productionNotificationReviewerView{}, "ok", "Connected")
	if !strings.Contains(out, "Kitsu position is Supervisor (position-based; roles and departments are not used)") {
		t.Fatalf("WFA recipient basis missing from notifications editor:\n%s", out)
	}
}

func TestBatchC_HealthBaselineAndConfValuesVisible(t *testing.T) {
	db := newIAViewDB(t)
	SetEffectiveRuntimeConfig(EffectiveRuntimeConfig{IgnoreMessagesDaysOld: 14, TplPreset: "studio-compact"})
	t.Cleanup(func() { SetEffectiveRuntimeConfig(EffectiveRuntimeConfig{}) })

	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=en", nil), db, time.Minute)
	body := w.Body.String()
	for _, want := range []string{
		"First-run baseline: records current task state only",
		"ignoreMessagesDaysOld) = 14 days",
		"tplPreset) = studio-compact",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("health page missing %q in:\n%s", want, body)
		}
	}
}

func TestBatchC_DiscordGuildPickerAndInviteURL(t *testing.T) {
	db := newIAViewDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, t.TempDir()+"/runtime-secret.key")
	if err := setRuntimeDiscordBotToken(db, "bot-token"); err != nil {
		t.Fatal(err)
	}

	oldLister := botGuildLister
	t.Cleanup(func() { botGuildLister = oldLister })

	// 1) Single guild -> <select> with prefilled selected option.
	botGuildLister = func(string) ([]DiscordGuild, error) {
		return []DiscordGuild{{ID: "123456789012345678", Name: "Studio Guild"}}, nil
	}
	field := renderDiscordGuildField("en", db, true, true)
	if !strings.Contains(field, `<select id="discord-guild-id" name="discord_guild_id">`) || !strings.Contains(field, `<option value="123456789012345678" selected>Studio Guild (123456789012345678)</option>`) {
		t.Fatalf("expected prefilled guild select, got:\n%s", field)
	}

	// 2) Guild list fetch failure -> fallback text input.
	botGuildLister = func(string) ([]DiscordGuild, error) {
		return nil, errors.New("discord unavailable")
	}
	fallback := renderDiscordGuildField("en", db, true, true)
	if !strings.Contains(fallback, `<input id="discord-guild-id" type="text" name="discord_guild_id"`) {
		t.Fatalf("expected fallback text input on guild list failure, got:\n%s", fallback)
	}

	// 3) Invite URL with permissions = 292057861136 and 7 permissions listed.
	invite := renderDiscordInviteBlock("en", "987654321098765432")
	for _, want := range []string{
		"https://discord.com/oauth2/authorize?client_id=987654321098765432&amp;permissions=292057861136&amp;scope=bot",
		"Manage Channels, View Channel, Send Messages, Send Messages in Threads, Manage Threads, Embed Links, Read Message History",
	} {
		if !strings.Contains(invite, want) {
			t.Fatalf("invite block missing %q in:\n%s", want, invite)
		}
	}
}

func TestBatchC_PublicRouteStateAndBackupUnit(t *testing.T) {
	db := newIAViewDB(t)
	_ = model.SetSettingWithError(db, AppOperationPatternSettingKey, OperationPatternExternal)
	_ = model.SetSettingWithError(db, DiscordApplicationPublicKeySettingKey, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	_ = model.SetSettingWithError(db, AppExternalURLSettingKey, "https://configured.example.com")
	_ = model.SetSettingWithError(db, AppObservedPublicURLSettingKey, "https://observed.example.com")

	// Connection page shows configured URL, observed URL, and mismatch.
	statusRows := renderDiscordButtonReceiverStatusRows("en", db)
	if !strings.Contains(statusRows, "Configured URL: https://configured.example.com") || !strings.Contains(statusRows, "Observed URL: https://observed.example.com (mismatch)") {
		t.Fatalf("receiver status rows missing public route state:\n%s", statusRows)
	}

	// Health page surfaces URL mismatch as Current problem.
	w := httptest.NewRecorder()
	renderIAHealth(w, httptest.NewRequest(http.MethodGet, "/bot/admin/health?lang=en", nil), db, time.Minute)
	if !strings.Contains(w.Body.String(), "Configured and observed public HTTPS URLs disagree") {
		t.Fatalf("health page did not surface public URL mismatch:\n%s", w.Body.String())
	}

	// Connections page shows backup unit line.
	connBody := renderConnectionsDisplayBodyWithBotIdentity("en", httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang=en", nil), db, "", "ok", "Connected", "https://kitsu.example.com", "", true, false, "", "")
	if !strings.Contains(connBody, "SQLite database file and runtime-secret.key together") {
		t.Fatalf("connections page missing backup unit line:\n%s", connBody)
	}
}
