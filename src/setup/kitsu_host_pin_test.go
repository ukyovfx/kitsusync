package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"app/src/model"
)

func newKitsuSaveStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"Zou","database-up":true,"event-stream-up":true,"job-queue-up":true,"key-value-store-up":true,"version":"0.11.3"}`))
		case "/api/auth/authenticated":
			_, _ = w.Write([]byte(`{"id":"bot-id","full_name":"KitsuSync Bot","is_bot":true}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func postSaveKitsu(t *testing.T, db *gorm.DB, host, lang string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"action": {"save_kitsu"}, "kitsu_hostname": {host}, "kitsu_bot_token": {"test-bot-token"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang="+lang, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecentBotEditSession(t, req)
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	return rr
}

func resetAppManagedKitsuHostname(t *testing.T) {
	t.Helper()
	appManagedKitsuHostname.mu.Lock()
	appManagedKitsuHostname.value = ""
	appManagedKitsuHostname.mu.Unlock()
}

func TestPinnedKitsuHostRejectsAdminSaveKitsu(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	resetAppManagedKitsuHostname(t)
	pinned := newKitsuSaveStubServer(t)
	other := newKitsuSaveStubServer(t)
	t.Setenv("KITSU_HOSTNAME", pinned.URL)
	db := newSetupStateTestDB(t)

	rr := postSaveKitsu(t, db, other.URL, "en")
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "KITSU_HOSTNAME") || !strings.Contains(body, "pinned by the operator") {
		t.Fatalf("expected an English reason, got %q", body)
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != "" {
		t.Fatalf("kitsu.hostname must not be persisted, got %q", got)
	}
	if StoredRuntimeKitsuToken(db) != "" {
		t.Fatal("token must not be stored on a rejected save")
	}
	if osKitsuHostname() != pinned.URL {
		t.Fatalf("KITSU_HOSTNAME was overridden: %q", osKitsuHostname())
	}

	jaRR := postSaveKitsu(t, db, other.URL, "ja")
	if jaRR.Code != http.StatusConflict || !strings.Contains(jaRR.Body.String(), "運用者") {
		t.Fatalf("expected a Japanese reason, got %d", jaRR.Code)
	}
}

func TestPinnedKitsuHostAllowsResubmittingSameHost(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	resetAppManagedKitsuHostname(t)
	pinned := newKitsuSaveStubServer(t)
	t.Setenv("KITSU_HOSTNAME", pinned.URL)
	db := newSetupStateTestDB(t)

	rr := postSaveKitsu(t, db, pinned.URL, "en")
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect for the pinned host itself, got %d", rr.Code)
	}
	if _, ok := operatorPinnedKitsuHost(); !ok {
		t.Fatal("endpoint must stay operator-pinned after saving the same host")
	}
}

func TestPinnedKitsuHostRejectsLegacyAdminSave(t *testing.T) {
	resetAppManagedKitsuHostname(t)
	pinned := newKitsuSaveStubServer(t)
	other := newKitsuSaveStubServer(t)
	t.Setenv("KITSU_HOSTNAME", pinned.URL)
	db := newSetupStateTestDB(t)

	form := url.Values{"kitsu_hostname": {other.URL}, "kitsu_runtime_email": {"m@example.test"}, "kitsu_runtime_password": {"not-a-real-secret"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?legacy=1&lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecentBotEditSession(t, req)
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "KITSU_HOSTNAME") {
		t.Fatalf("expected a pinned-endpoint rejection, got %d", rr.Code)
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != "" {
		t.Fatalf("kitsu.hostname must not be persisted, got %q", got)
	}
	if osKitsuHostname() != pinned.URL {
		t.Fatal("KITSU_HOSTNAME was overridden")
	}
}

func TestSetupFormDoesNotPersistKitsuHostname(t *testing.T) {
	resetAppManagedKitsuHostname(t)
	pinned := newKitsuSaveStubServer(t)
	other := newKitsuSaveStubServer(t)
	t.Setenv("KITSU_HOSTNAME", pinned.URL)
	db := newSetupStateTestDB(t)

	form := url.Values{"action": {"bot_setup"}, "kitsu_hostname": {other.URL}, "kitsu_runtime_email": {"m@example.test"}, "kitsu_runtime_password": {"not-a-real-secret"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	Handler("", "", "", db, func() bool { return true }, nil)(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected the legacy form to redirect, got %d", rr.Code)
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != "" {
		t.Fatalf("kitsu.hostname must not be persisted from /setup, got %q", got)
	}
	if osKitsuHostname() != pinned.URL {
		t.Fatal("KITSU_HOSTNAME was overridden")
	}
}

func TestUnpinnedKitsuHostStillSavesFromAdmin(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv("KITSU_HOSTNAME", "")
	resetAppManagedKitsuHostname(t)
	first := newKitsuSaveStubServer(t)
	second := newKitsuSaveStubServer(t)
	db := newSetupStateTestDB(t)

	if rr := postSaveKitsu(t, db, first.URL, "en"); rr.Code != http.StatusSeeOther {
		t.Fatalf("expected the first save to succeed, got %d", rr.Code)
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != first.URL+"/" {
		t.Fatalf("expected the host to be saved, got %q", got)
	}
	// The app exports KITSU_HOSTNAME itself after a save; that must not lock
	// later saves from the screen.
	if osKitsuHostname() == "" {
		t.Fatal("expected the saved host to be exported for the running process")
	}
	if rr := postSaveKitsu(t, db, second.URL, "en"); rr.Code != http.StatusSeeOther {
		t.Fatalf("expected a later save to succeed, got %d", rr.Code)
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != second.URL+"/" {
		t.Fatalf("expected the second host to be saved, got %q", got)
	}
}

func TestPinnedKitsuHostDoesNotFallBackToSavedHostWhenDown(t *testing.T) {
	resetAppManagedKitsuHostname(t)
	saved := httptest.NewServer(kitsuProbeHandler())
	defer saved.Close()
	down := httptest.NewServer(kitsuProbeHandler())
	downURL := down.URL
	down.Close()
	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", saved.URL+"/")

	t.Setenv("KITSU_HOSTNAME", downURL)
	if result := DiscoverKitsuHost(db); result.RuntimeHost != "" {
		t.Fatalf("pinned host is down; must not switch to the saved host: %+v", result)
	}

	// An invalid/placeholder value is not a pin, so the saved host still applies.
	t.Setenv("KITSU_HOSTNAME", "http://YOUR_KITSU_HOST/")
	if result := DiscoverKitsuHost(db); result.Source != "persisted" || result.RuntimeHost != saved.URL+"/" {
		t.Fatalf("placeholder must not pin the endpoint: %+v", result)
	}
}

func TestRuntimeExportDoesNotCreateFalsePin(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv("KITSU_HOSTNAME", "")
	resetAppManagedKitsuHostname(t)
	first := newKitsuSaveStubServer(t)
	second := newKitsuSaveStubServer(t)
	db := newSetupStateTestDB(t)

	if rr := postSaveKitsu(t, db, first.URL, "en"); rr.Code != http.StatusSeeOther {
		t.Fatalf("expected the first save to succeed, got %d", rr.Code)
	}
	// Runtime authentication re-exports the host without the trailing slash.
	ExportRuntimeKitsuHostname(first.URL)
	if _, pinned := operatorPinnedKitsuHost(); pinned {
		t.Fatal("an app-exported host must not count as operator-pinned")
	}
	if rr := postSaveKitsu(t, db, second.URL, "en"); rr.Code != http.StatusSeeOther {
		t.Fatalf("expected a later save to succeed, got %d", rr.Code)
	}

	// A genuine operator pin must survive a runtime export of the same host.
	resetAppManagedKitsuHostname(t)
	t.Setenv("KITSU_HOSTNAME", first.URL)
	ExportRuntimeKitsuHostname(first.URL + "/")
	if _, pinned := operatorPinnedKitsuHost(); !pinned {
		t.Fatal("a runtime export must not hide a genuine operator pin")
	}
}

func TestPinnedKitsuHostRejectsAPIOverrideToOtherServer(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	resetAppManagedKitsuHostname(t)
	pinned := newKitsuSaveStubServer(t)
	other := newKitsuSaveStubServer(t)
	t.Setenv("KITSU_HOSTNAME", pinned.URL)
	db := newSetupStateTestDB(t)

	form := url.Values{"action": {"save_kitsu"}, "kitsu_hostname": {pinned.URL}, "kitsu_api_base_url": {other.URL}, "kitsu_bot_token": {"test-bot-token"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecentBotEditSession(t, req)
	rr := httptest.NewRecorder()
	BotHandler(db, nil)(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected the API override to be rejected, got %d", rr.Code)
	}
	if StoredRuntimeKitsuToken(db) != "" || model.GetSetting(db, KitsuAPIBaseURLSettingKey) != "" {
		t.Fatal("nothing may be persisted on a rejected save")
	}

	if kitsuAPIOverrideBlocked(pinned.URL) {
		t.Fatal("an override equal to the pinned host must be allowed")
	}
	if kitsuAPIOverrideBlocked("") {
		t.Fatal("no override must be allowed")
	}
}
