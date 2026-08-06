package setup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		if r.URL.Path != "/api/auth/login" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct{ Email, Password string }
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Email != "existing@example.test" || body.Password != "stored-password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"test"}`))
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	if err := setRuntimeKitsuPassword(db, "stored-password"); err != nil {
		t.Fatalf("failed to create stored test credential: %v", err)
	}
	form := url.Values{"action": {"save_kitsu"}, "kitsu_hostname": {server.URL}, "kitsu_runtime_email": {"existing@example.test"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "kitsu_saved") {
		t.Fatalf("expected Kitsu save redirect, got %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if StoredRuntimeKitsuPassword(db) != "stored-password" {
		t.Fatal("blank Kitsu password did not retain the stored password")
	}
}

func TestConnectionsEditFormSeparatesKitsuAndDiscordFields(t *testing.T) {
	db := newSetupStateTestDB(t)
	req := httptest.NewRequest("GET", "/bot/admin/bot?edit=1&lang=en", nil)
	body := renderConnectionsEditForm("en", req, db, "Review connections separately.", "bad", "Action required", "http://host.docker.internal:8080", "")

	if !strings.Contains(body, `name="kitsu_hostname"`) {
		t.Fatal("expected a named Kitsu hostname field")
	}
	if !strings.Contains(body, `name="kitsu_runtime_email"`) || !strings.Contains(body, `name="kitsu_runtime_password"`) {
		t.Fatal("expected Kitsu integration credentials in the Kitsu section")
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
	if strings.Count(body, `name="kitsu_runtime_password"`) != 1 || strings.Count(body, `name="bot_token"`) != 1 {
		t.Fatal("expected each secret field to appear once")
	}
	if strings.Contains(body, `name="kitsu_runtime_password" value=`) || strings.Contains(body, `name="bot_token" value=`) {
		t.Fatal("did not expect stored secrets in rendered fields")
	}
	if strings.Count(body, `class="connection-save-form"`) != 2 {
		t.Fatal("expected exactly two save-form boundaries")
	}
	if strings.Contains(body, "Kitsu Runtime") || strings.Contains(body, "Runtime email") || strings.Contains(body, "Runtime password") {
		t.Fatal("did not expect internal Runtime terminology in the edit form")
	}
	if strings.Index(body, `name="kitsu_runtime_email"`) > strings.Index(body, `name="bot_token"`) {
		t.Fatal("expected Kitsu fields before the Discord section")
	}
	if !strings.Contains(body, "Enter a new token only when needed.") || !strings.Contains(body, "The saved token is not displayed. Changes take effect after saving.") {
		t.Fatal("expected compact token guidance")
	}
	if strings.Contains(body, `<button type="submit" class="btn">Save</button>`) {
		t.Fatal("did not expect a generic Save button")
	}
}

func TestConnectionsDisplayUsesSharedFieldRows(t *testing.T) {
	db := newSetupStateTestDB(t)
	req := httptest.NewRequest("GET", "/bot/admin/bot?lang=ja", nil)
	body := renderConnectionsDisplayBody("ja", req, db, "Kitsu接続を設定してください", "bad", "対応が必要", "http://127.0.0.1:8080", "")

	if got := strings.Count(body, `class="connection-field-row"`); got != 3 {
		t.Fatalf("expected three shared connection field rows, got %d", got)
	}
	if !strings.Contains(body, `class="connection-field-list"`) || !strings.Contains(body, `class="status-pill bad"`) {
		t.Fatal("expected shared field-list and semantic status badge")
	}
	if strings.Contains(body, "host.docker.internal") || strings.Contains(body, "&lt;span") || strings.Contains(body, "<span class=\"status-pill bad\">対応が必要</span>") {
		t.Fatal("did not expect internal endpoint or literal status markup")
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
