package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	for _, expected := range []string{"Disconnected", "Paused", "/bot/admin/bot?edit=1"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("setup-required page missing %q", expected)
		}
	}
	if strings.Contains(body, "kitsu_runtime_email") || strings.Contains(body, "kitsu_runtime_password") {
		t.Fatal("setup-required page rendered human Kitsu credential fields")
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
	db := newSetupStateTestDB(t)
	form := url.Values{"action": {"bot_setup"}, "kitsu_runtime_email": {"runtime@example.test"}, "kitsu_runtime_password": {"runtime-password"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	Handler("http://kitsu.invalid/", "", "", db, func() bool { return false }, nil)(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/bot/admin/bot?edit=1") {
		t.Fatalf("legacy human setup was not redirected safely: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
	if model.GetSetting(db, RuntimeKitsuPasswordSettingKey) != "" {
		t.Fatal("legacy human setup persisted a runtime credential")
	}
}

func TestKitsuSetupFailureDoesNotRenderBotFailureOrPersistCredential(t *testing.T) {
	db := newSetupStateTestDB(t)
	form := url.Values{"action": {"bot_setup"}, "kitsu_runtime_email": {"runtime@example.test"}, "kitsu_runtime_password": {"wrong-password"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	Handler("http://kitsu.invalid/", "", "", db, func() bool { return false }, nil)(rr, req)
	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/bot/admin/bot?edit=1") {
		t.Fatalf("legacy human setup was not redirected safely: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
	if model.GetSetting(db, RuntimeKitsuPasswordSettingKey) != "" {
		t.Fatal("invalid Kitsu credentials were persisted")
	}
}
