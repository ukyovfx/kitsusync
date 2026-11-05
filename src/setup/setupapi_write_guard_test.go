package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"app/src/model"
)

func TestTestKitsuSuccessDoesNotChangeSavedHostname(t *testing.T) {
	db := newSetupStateTestDB(t)
	t.Setenv(RuntimeKitsuEmailEnv, "")
	t.Setenv(RuntimeKitsuPasswordEnv, "")
	const saved = "http://saved-kitsu.test:8080"
	model.SetSetting(db, "kitsu.hostname", saved)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(validZouStatus))
		case "/api/auth/login":
			_, _ = w.Write([]byte(`{"access_token":"runtime-token","user":{"role":"admin"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	pinTryKitsuLoginToLoopback(t, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})

	body := `{"hostname":"` + tryKitsuLoginHost(t, server.URL) + `","email":"admin@example.test","password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/test-kitsu", strings.NewReader(body))
	rr := httptest.NewRecorder()
	TestKitsuHandler(db)(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"authenticated":true`) {
		t.Fatalf("test-kitsu should succeed: %d %s", rr.Code, rr.Body.String())
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != saved {
		t.Fatalf("kitsu.hostname changed by test-kitsu: %q", got)
	}
}

func TestSetupJSONWriteHandlersRejectGETWithBody(t *testing.T) {
	db := newSetupStateTestDB(t)
	model.SetSetting(db, "kitsu.hostname", "http://saved-kitsu.test:8080")
	handlers := map[string]http.HandlerFunc{
		"test-kitsu":    TestKitsuHandler(db),
		"test-discord":  TestDiscordHandler(db),
		"apply-project": ApplyProjectHandler(db, nil),
		"notification":  TestNotificationHandler(db, nil),
		"users":         SaveUserMappingHandler(db),
		"checkers":      SaveCheckerMappingHandler(db),
	}
	body := `{"hostname":"http://attacker.test","email":"a@b.test","password":"pw","project_id":"p1"}`
	for name, h := range handlers {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			req := httptest.NewRequest(method, "/api/setup/"+name, strings.NewReader(body))
			rr := httptest.NewRecorder()
			h(rr, req)
			if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("%s %s: want 405 with Allow: POST, got %d %q", name, method, rr.Code, rr.Header().Get("Allow"))
			}
		}
	}
	if got := model.GetSetting(db, "kitsu.hostname"); got != "http://saved-kitsu.test:8080" {
		t.Fatalf("kitsu.hostname changed: %q", got)
	}
}

func TestValidationOnlyRejectsSetupWritesIncludingTestKitsu(t *testing.T) {
	t.Setenv(validationOnlyEnv, "1")
	called := false
	next := func(http.ResponseWriter, *http.Request) { called = true }
	h := RejectValidationMutation(requirePOST(next))
	for _, target := range []string{"test-kitsu", "test-discord", "apply-project", "test-notification", "mapping/users", "mapping/checkers"} {
		called = false
		req := httptest.NewRequest(http.MethodPost, "/api/setup/"+target, strings.NewReader(`{}`))
		rr := httptest.NewRecorder()
		h(rr, req)
		if rr.Code != http.StatusForbidden || called {
			t.Fatalf("%s: validation-only POST must be rejected, got %d called=%v", target, rr.Code, called)
		}
	}
	// A GET reaches the POST guard and is refused there, not served.
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/api/setup/test-kitsu", strings.NewReader(`{}`)))
	if rr.Code != http.StatusMethodNotAllowed || called {
		t.Fatalf("validation-only GET: want 405, got %d", rr.Code)
	}
}

func TestPublicKitsuHostnameIgnoresForwardedHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://kitsu.example.test/", nil)
	req.Header.Set("X-Forwarded-Host", "attacker.test")
	got := publicKitsuHostnameFromRequest(req, "")
	if strings.Contains(got, "attacker.test") || !strings.Contains(got, "kitsu.example.test") {
		t.Fatalf("X-Forwarded-Host must not be used: %q", got)
	}
}
