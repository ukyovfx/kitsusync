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
	for _, expected := range []string{"Setup required", "Disconnected", "Paused", "runtime_setup_from_session"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("setup-required page missing %q", expected)
		}
	}
}

func TestAdminSessionCanConfigureDedicatedRuntimeWithoutReusingBrowserToken(t *testing.T) {
	resetSessions()
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv(RuntimeKitsuPasswordEnv, "")
	kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer browser-session-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/data/persons":
			fmt.Fprint(w, `[]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/data/persons":
			fmt.Fprint(w, `{"id":"runtime-person","email":"kitsusync-bot@local.invalid","is_bot":true}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer kitsu.Close()

	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", kitsu.URL+"/")
	sessionToken := newSessionToken("admin@example.com", "browser-session-token", "admin", "/bot/setup")
	form := url.Values{"action": {"runtime_setup_from_session"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/setup?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})
	rr := httptest.NewRecorder()
	configured := false
	Handler(kitsu.URL+"/", "", "", db, func() bool { return configured }, func() { configured = true })(rr, req)

	if rr.Code != http.StatusOK || !configured {
		t.Fatalf("runtime setup did not complete, status=%d configured=%v", rr.Code, configured)
	}
	if got := model.GetSetting(db, RuntimeKitsuEmailSettingKey); got != "kitsusync-bot@local.invalid" {
		t.Fatalf("runtime email = %q", got)
	}
	if encrypted := model.GetSetting(db, RuntimeKitsuPasswordSettingKey); !strings.HasPrefix(encrypted, "v1:") {
		t.Fatal("runtime password was not stored as encrypted data")
	}
	if StoredRuntimeKitsuPassword(db) == "" {
		t.Fatal("encrypted runtime password could not be reloaded")
	}
	if strings.Contains(rr.Body.String(), "browser-session-token") {
		t.Fatal("browser session token was exposed in the response")
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
