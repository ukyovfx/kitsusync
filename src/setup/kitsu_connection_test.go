package setup

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app/src/model"
)

func TestBotHandlerRejectsRuntimeCredentialsWithoutEndpoint(t *testing.T) {
	db := newSetupStateTestDB(t)
	form := url.Values{"kitsu_runtime_email": {"manager@example.test"}, "kitsu_runtime_password": {"not-a-real-secret"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected missing endpoint to be rejected, got %d", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "status=0") || strings.Contains(body, "category=network") || strings.Contains(body, "host.docker.internal") {
		t.Fatal("did not expect raw network diagnostics in the response")
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != "" {
		t.Fatalf("did not expect endpoint persistence on rejected input: %q", got)
	}
}

func TestConnectionsSaveDiscordDoesNotValidateKitsu(t *testing.T) {
	db := newSetupStateTestDB(t)
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "existing@example.test")
	oldValidator := validateDiscordBotTokenForSave
	validateDiscordBotTokenForSave = func(string) error { return nil }
	defer func() { validateDiscordBotTokenForSave = oldValidator }()

	form := url.Values{"action": {"save_discord"}, "bot_token": {"new-token-for-test"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "discord_saved") {
		t.Fatalf("expected Discord-only save redirect, got %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if model.GetSetting(db, RuntimeKitsuEmailSettingKey) != "existing@example.test" {
		t.Fatal("Discord-only save changed Kitsu settings")
	}
}

func TestConnectionsSaveDiscordEmptyIsDiscordSpecific(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "")
	db := newSetupStateTestDB(t)
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "existing@example.test")
	form := url.Values{"action": {"save_discord"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Discord Bot Token") {
		t.Fatalf("expected Discord-specific validation error, got %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "Kitsu連携アカウント") || strings.Contains(rr.Body.String(), "Kitsu password") {
		t.Fatal("did not expect Kitsu validation during Discord-only save")
	}
}

func TestConnectionsSaveKitsuRetainsStoredPassword(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	form := url.Values{"action": {"save_kitsu"}, "kitsu_hostname": {server.URL}, "kitsu_bot_token": {"test-bot-token"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "kitsu_bot_saved") {
		t.Fatalf("expected Kitsu save redirect, got %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if StoredRuntimeKitsuToken(db) == "" {
		t.Fatal("validated Kitsu Bot token was not stored")
	}
}

func TestConnectionsRecheckKitsuUsesStoredTokenWithoutRenderingIt(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	if err := setRuntimeKitsuToken(db, "stored-token-for-recheck"); err != nil {
		t.Fatalf("store Kitsu test token: %v", err)
	}
	form := url.Values{"action": {"save_kitsu"}, "kitsu_hostname": {server.URL}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "kitsu_bot_saved") {
		t.Fatalf("expected stored-token recheck redirect, got %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if !strings.Contains(rr.Header().Get("Location"), "/bot/admin/bot") {
		t.Fatal("recheck did not return to connections")
	}
}

func TestConnectionsEditFormSeparatesKitsuAndDiscordFields(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "")
	db := newSetupStateTestDB(t)
	req := httptest.NewRequest("GET", "/bot/admin/bot?edit=1&lang=en", nil)
	body := renderConnectionsEditForm("en", req, db, "Review connections separately.", "bad", "Action required", "http://host.docker.internal:8080", "")

	if !strings.Contains(body, `name="kitsu_hostname"`) {
		t.Fatal("expected a named Kitsu hostname field")
	}
	if strings.Contains(body, `name="kitsu_runtime_email"`) || strings.Contains(body, `name="kitsu_runtime_password"`) {
		t.Fatal("did not expect human Kitsu credential controls in the current Connections form")
	}
	if !strings.Contains(body, `name="kitsu_bot_token"`) {
		t.Fatal("expected the Kitsu Bot token field")
	}
	if !strings.Contains(body, `name="bot_token"`) {
		t.Fatal("expected the Discord Bot token field")
	}
	if got := strings.Count(body, `<form method="POST" class="connection-save-form">`); got != 2 {
		t.Fatalf("expected two independent connection forms, got %d", got)
	}
	if !strings.Contains(body, `name="action" value="save_kitsu"`) || !strings.Contains(body, `name="action" value="save_discord"`) {
		t.Fatal("expected explicit independent save actions")
	}
	if !strings.Contains(body, `value="Kitsu connection saved`) && strings.Contains(body, `>Save<`) {
		t.Fatal("did not expect a generic global Save action")
	}
	if strings.Count(body, `name="kitsu_bot_token"`) != 1 || strings.Count(body, `name="bot_token"`) != 1 {
		t.Fatal("expected each current secret field to appear once")
	}
	if strings.Contains(body, `name="kitsu_bot_token" value=`) || strings.Contains(body, `name="bot_token" value=`) {
		t.Fatal("did not expect stored secrets in rendered fields")
	}
	if strings.Count(body, `class="connection-save-form"`) != 2 {
		t.Fatal("expected exactly two save-form boundaries")
	}
	if strings.Contains(body, "Kitsu Runtime") || strings.Contains(body, "Runtime email") || strings.Contains(body, "Runtime password") || strings.Contains(body, "Legacy fallback") {
		t.Fatal("did not expect internal Runtime terminology in the edit form")
	}
	if strings.Index(body, `name="kitsu_bot_token"`) > strings.Index(body, `name="bot_token"`) {
		t.Fatal("expected Kitsu Bot fields before the Discord section")
	}
	if strings.Contains(body, "Saved tokens are never displayed.") || strings.Contains(body, `placeholder="••••••••••••••••••••"`) {
		t.Fatal("configured token fields must not use a mask-looking placeholder")
	}
	if strings.Contains(body, "Saved") || strings.Contains(body, "Change token") || strings.Contains(body, `hidden style="display:none"`) {
		t.Fatal("unset token fields should remain editable without saved-secret controls")
	}
	if !strings.Contains(body, "Kitsu Bot API token") || !strings.Contains(body, "Discord Bot Token") {
		t.Fatal("expected distinct token labels")
	}
	if strings.Contains(body, `<button type="submit" class="btn">Save</button>`) {
		t.Fatal("did not expect a generic Save button")
	}
	// The summary intentionally contains helper text only; service headers own the prominent state.
	if strings.Contains(body, `<div class="connections-edit-summary"><p class="hint">Review connections separately.</p><span class="status-pill`) {
		t.Fatal("did not expect a page-level status pill in edit mode")
	}
	if got := strings.Count(body, `class="status-pill `); got != 2 {
		t.Fatalf("expected one service status pill per card, got %d", got)
	}
	if strings.Contains(body, `class="connection-state-row"`) || strings.Contains(body, `>Token<`) || strings.Contains(body, `>Connection<`) {
		t.Fatal("edit form should not duplicate token and connection state rows")
	}
}

func TestConnectionsEditFormShowsSavedSecretsSeparately(t *testing.T) {
	db := newSetupStateTestDB(t)
	keyPath := filepath.Join(t.TempDir(), "runtime-secret.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x42}, 32), 0600); err != nil {
		t.Fatalf("write test secret key: %v", err)
	}
	t.Setenv(RuntimeSecretKeyFileEnv, keyPath)
	if err := setRuntimeKitsuToken(db, "kitsu-test-token"); err != nil {
		t.Fatalf("store Kitsu test token: %v", err)
	}
	setRuntimeDiscordBotToken(db, "discord-test-token")

	body := renderConnectionsEditFormWithIdentityRows("en", httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=en", nil), db, "", "warn", "Needs review", "https://kitsu.example.test", false, false, "Test Bot")
	for _, want := range []string{"Needs review", "Recheck connection", "Change token", `hidden style="display:none"`, `name="action" value="save_kitsu"`, `name="action" value="save_discord"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("saved-secret form missing %q", want)
		}
	}
	for _, secret := range []string{"kitsu-test-token", "discord-test-token", "••••"} {
		if strings.Contains(body, secret) {
			t.Fatalf("saved secret or mask leaked into form: %q", secret)
		}
	}
}

func TestConnectionsDisplayUsesSharedFieldRows(t *testing.T) {
	db := newSetupStateTestDB(t)
	req := httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil)
	body := renderConnectionsDisplayBodyWithHealthRaw("ja", req, db, "Kitsu接続を設定してください", "bad", "対応が必要", "http://127.0.0.1:8080", "", false, false)

	if got := strings.Count(body, `class="connection-field-row"`); got != 1 {
		t.Fatalf("expected one Kitsu host field row in the compact summary, got %d", got)
	}
	if !strings.Contains(body, `class="connection-field-list"`) || !strings.Contains(body, `class="status-pill warning"`) {
		t.Fatal("expected shared field-list and semantic status badge")
	}
	if strings.Contains(body, "host.docker.internal") || strings.Contains(body, "&lt;span") || strings.Contains(body, "<span class=\"status-pill bad\">対応が必要</span>") {
		t.Fatal("did not expect internal endpoint or literal status markup")
	}
	if strings.Contains(body, "<dt>Token</dt>") || strings.Contains(body, "<dt>Connection</dt>") || strings.Contains(body, "<dt>トークン</dt>") || strings.Contains(body, "<dt>接続</dt>") {
		t.Fatal("normal Connections view should not duplicate token and connection state rows")
	}
}

func TestConnectionsPageStatusUsesOnlyConnectionHealth(t *testing.T) {
	configured := SharedBotRuntimeReadiness{KitsuConfigured: true, DiscordConfigured: true, ProductionConnected: false, OverallReady: false}
	for _, lang := range []string{"ja", "en"} {
		status := connectionPageStatus(lang, configured, true, true)
		if status.Class != "ok" || status.Label != map[string]string{"ja": "接続済み", "en": "Connected"}[lang] {
			t.Fatalf("%s healthy connections status = %+v", lang, status)
		}
		missing := connectionPageStatus(lang, SharedBotRuntimeReadiness{}, true, true)
		if missing.Class != "warning" || missing.Label != map[string]string{"ja": "未設定", "en": "Not configured"}[lang] {
			t.Fatalf("%s missing connections status = %+v", lang, missing)
		}
		failed := connectionPageStatus(lang, configured, false, true)
		if failed.Class != "warn" || failed.Label != map[string]string{"ja": "要確認", "en": "Needs review"}[lang] {
			t.Fatalf("%s failed connections status = %+v", lang, failed)
		}
		pills := renderConnectionHealthPills(lang, true, true, "configured-secret")
		if !strings.Contains(pills, `class="status-pill ok"`) || !strings.Contains(pills, map[string]string{"ja": "保存済み", "en": "Saved"}[lang]) || strings.Contains(pills, "Configured") {
			t.Fatalf("%s connection health and hidden metadata are not separated: %s", lang, pills)
		}
	}
}

func TestConnectionsUsesCanonicalRuntimeHealthForKitsu(t *testing.T) {
	db := newSetupStateTestDB(t)
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	t.Setenv("KITSU_HOSTNAME", server.URL)
	model.SetSetting(db, "kitsu.hostname", server.URL)
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "runtime@example.test")
	if err := setRuntimeKitsuToken(db, "configured-token"); err != nil {
		t.Fatalf("store test token: %v", err)
	}

	req := httptest.NewRequest("GET", "/bot/admin/bot?lang=en", nil)
	rr := httptest.NewRecorder()
	renderConnectionsPageSafeWithRuntime(rr, req, db, func() bool { return true })
	body := rr.Body.String()
	if !strings.Contains(body, "Kitsu connection") || !strings.Contains(body, `class="status-pill ok"`) || !strings.Contains(body, "Connected") {
		t.Fatalf("expected healthy Kitsu status from runtime readiness: %s", body)
	}
	if strings.Contains(body, "configured-token") {
		t.Fatal("Kitsu token leaked into the connections summary")
	}
}

func TestRuntimeKitsuEndpointSeparatesInternalAndDisplayAddresses(t *testing.T) {
	db := newSetupStateTestDB(t)
	t.Setenv("KITSUSYNC_LOCAL_PROFILE", "1")
	t.Setenv("KITSU_HOSTNAME", "http://host.docker.internal:8080")

	if got := strings.TrimRight(effectiveRuntimeKitsuEndpoint(db), "/"); got != "http://host.docker.internal:8080" {
		t.Fatalf("unexpected effective runtime endpoint: %q", got)
	}
	if got, err := runtimeEndpointFromDisplay(db, "http://127.0.0.1:8080"); err != nil || strings.TrimRight(got, "/") != "http://host.docker.internal:8080" {
		t.Fatalf("expected local display endpoint to resolve internally, got %q, %v", got, err)
	}
	if got, err := runtimeEndpointFromDisplay(db, "https://kitsu.example.test"); err != nil || strings.TrimRight(got, "/") != "https://kitsu.example.test" {
		t.Fatalf("expected remote endpoint to remain unchanged, got %q, %v", got, err)
	}
	if _, err := validateKitsuEndpoint("not a URL"); err == nil {
		t.Fatal("expected malformed endpoint to be rejected")
	}
}

func TestRuntimeKitsuEndpointResolvesSafeDisplayWithoutLocalProfile(t *testing.T) {
	db := newSetupStateTestDB(t)
	t.Setenv("KITSUSYNC_LOCAL_PROFILE", "")
	model.SetSetting(db, "kitsu.hostname", "http://host.docker.internal:8080")

	got, err := runtimeEndpointFromDisplay(db, "http://127.0.0.1:8080")
	if err != nil || strings.TrimRight(got, "/") != "http://host.docker.internal:8080" {
		t.Fatalf("expected saved runtime endpoint to resolve from safe display value, got %q, %v", got, err)
	}
}
