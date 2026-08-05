package setup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"app/src/model"
)

func TestSetupRequiredPageIsAvailableWithoutRuntimeCredentials(t *testing.T) {
	db := newTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/bot/setup?lang=en", nil)
	rr := httptest.NewRecorder()
	Handler("", "", "", db, func() bool { return false }, nil)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	for _, expected := range []string{"Setup required", "Disconnected", "Paused", "kitsu_runtime_password"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("setup-required page missing %q", expected)
		}
	}
}

func TestLegacySessionRuntimeSetupRedirectsToKitsuSettings(t *testing.T) {
	db := newSetupStateTestDB(t)
	form := url.Values{"action": {"runtime_setup_from_session"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	Handler("http://kitsu.invalid/", "", "", db, func() bool { return false }, nil)(rr, req)

	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/bot/admin/bot?edit=1") {
		t.Fatalf("legacy runtime setup did not redirect to Kitsu settings: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
	if model.GetSetting(db, RuntimeKitsuPasswordSettingKey) != "" {
		t.Fatal("legacy session setup persisted a runtime credential")
	}
}

func TestRuntimeReadyRequiredFailsClosed(t *testing.T) {
	called := false
	handler := RuntimeReadyRequired(func() bool { return false }, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	req := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?lang=en", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("expected fail-closed response, status=%d called=%v", rr.Code, called)
	}
}

func TestKitsuSetupValidatesAndStoresRuntimeCredentialsWithoutDiscord(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv(RuntimeKitsuPasswordEnv, "")
	kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/auth/login" {
			t.Fatalf("unexpected Kitsu request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"runtime-jwt"}`)
	}))
	defer kitsu.Close()

	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", kitsu.URL+"/")
	configured := false
	form := url.Values{
		"action":                 {"bot_setup"},
		"kitsu_runtime_email":    {"runtime@example.test"},
		"kitsu_runtime_password": {"runtime-password"},
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	Handler(kitsu.URL+"/", "", "", db, func() bool { return configured }, func() { configured = true })(rr, req)

	if rr.Code != http.StatusOK || !configured {
		t.Fatalf("Kitsu setup did not complete, status=%d configured=%v", rr.Code, configured)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Kitsu connection configured") || strings.Contains(body, "Bot Setup Failed") || strings.Contains(body, "runtime-password") {
		t.Fatalf("Kitsu setup rendered the wrong result or exposed a secret: %s", body)
	}
	if StoredRuntimeKitsuPassword(db) == "" || model.GetSetting(db, RuntimeKitsuPasswordSettingKey) == "runtime-password" {
		t.Fatal("runtime password was not stored only as encrypted data")
	}
}

func TestKitsuSetupFailureDoesNotRenderBotFailureOrPersistCredential(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv(RuntimeKitsuPasswordEnv, "")
	kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer kitsu.Close()

	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", kitsu.URL+"/")
	form := url.Values{
		"action":                 {"bot_setup"},
		"kitsu_runtime_email":    {"runtime@example.test"},
		"kitsu_runtime_password": {"wrong-password"},
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	Handler(kitsu.URL+"/", "", "", db, func() bool { return false }, nil)(rr, req)

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Kitsu connection could not be configured") || strings.Contains(rr.Body.String(), "Bot Setup Failed") {
		t.Fatalf("unexpected Kitsu failure result: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if model.GetSetting(db, RuntimeKitsuPasswordSettingKey) != "" {
		t.Fatal("invalid Kitsu credentials were persisted")
	}
}
