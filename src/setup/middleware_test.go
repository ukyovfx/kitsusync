package setup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func resetSessions() {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	sessions = map[string]sessionData{}
}

func TestRequireSessionRedirectsWithoutCookie(t *testing.T) {
	resetSessions()

	req := httptest.NewRequest(http.MethodGet, "/bot/admin?tab=health", nil)
	rr := httptest.NewRecorder()

	RequireSession(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run without a session")
	})(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "/bot/login?lang=ja&next=%2Fbot%2Fadmin%3Ftab%3Dhealth") {
		t.Fatalf("unexpected redirect location: %s", location)
	}
}

func TestRequireSessionAllowsValidCookie(t *testing.T) {
	resetSessions()

	token := newSessionToken("manager@example.com", "jwt-token", "manager", "/bot/admin")
	req := httptest.NewRequest(http.MethodGet, "/bot/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	called := false
	RequireSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})(rr, req)

	if !called {
		t.Fatal("expected next handler to run for a valid session")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected next handler status, got %d", rr.Code)
	}
}

func TestLogoutClearsSessionCookie(t *testing.T) {
	resetSessions()

	token := newSessionToken("manager@example.com", "jwt-token", "manager", "/bot/admin")
	req := httptest.NewRequest(http.MethodGet, "/bot/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	LogoutHandler()(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status, got %d", rr.Code)
	}
	if validSession(token) {
		t.Fatal("expected session to be destroyed on logout")
	}
	cookieHeader := rr.Header().Get("Set-Cookie")
	if !strings.Contains(cookieHeader, sessionCookieName+"=") || !strings.Contains(cookieHeader, "Max-Age=0") {
		t.Fatalf("expected clearing cookie, got %s", cookieHeader)
	}
}

func TestSessionCookieUsesSecureForForwardedHTTPS(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bot/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	cookie := sessionCookie(req, "token", int((15 * time.Minute).Seconds()))
	if !cookie.Secure {
		t.Fatal("expected secure cookie for forwarded https requests")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected lax same-site cookie, got %v", cookie.SameSite)
	}
}

func TestLoginHandlerUsesConfiguredHostAndAcceptsAdmin(t *testing.T) {
	resetSessions()
	kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"browser-session-token","user":{"role":"admin"}}`)
	}))
	defer kitsu.Close()

	form := url.Values{"hostname": {"https://unexpected.example.invalid"}, "email": {"admin@example.com"}, "password": {"not-returned"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	LoginHandler(kitsu.URL)(rr, req)

	if rr.Code != http.StatusSeeOther || !strings.HasPrefix(rr.Header().Get("Location"), "/bot/admin") {
		t.Fatalf("expected admin redirect, got status=%d location=%s", rr.Code, rr.Header().Get("Location"))
	}
	if strings.Contains(rr.Body.String(), "not-returned") || strings.Contains(rr.Body.String(), "browser-session-token") {
		t.Fatal("login response exposed a credential")
	}
}

func TestLoginHandlerFirstRunRejectsNonAdmin(t *testing.T) {
	resetSessions()
	kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"browser-session-token","user":{"role":"user"}}`)
	}))
	defer kitsu.Close()

	called := false
	form := url.Values{"hostname": {kitsu.URL}, "email": {"user@example.com"}, "password": {"not-returned"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	LoginHandler(kitsu.URL)(rr, req)

	if rr.Code != http.StatusUnauthorized || called {
		t.Fatalf("non-admin must be rejected, status=%d callback=%v", rr.Code, called)
	}
}

func TestLoginHandlerAcceptsStudioManagerAndHigherRoles(t *testing.T) {
	for _, role := range []string{"manager", " ADMIN "} {
		t.Run(strings.TrimSpace(role), func(t *testing.T) {
			resetSessions()
			kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"access_token":"browser-session-token","user":{"role":%q}}`, role)
			}))
			defer kitsu.Close()

			form := url.Values{"email": {"manager@example.com"}, "password": {"not-returned"}}
			req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			LoginHandler(kitsu.URL)(rr, req)
			if rr.Code != http.StatusSeeOther {
				t.Fatalf("expected %s role to be accepted, got %d", role, rr.Code)
			}
		})
	}
}

func TestLoginHandlerWithDiscoveryAcceptsValidatedManualHostAndPersistsIt(t *testing.T) {
	resetSessions()
	kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			if r.URL.Path == "/api/" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"access_token":"browser-session-token","user":{"role":"manager"}}`)
	}))
	defer kitsu.Close()
	var persisted string
	form := url.Values{"hostname": {kitsu.URL}, "email": {"manager@example.com"}, "password": {"not-returned"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	LoginHandlerWithDiscovery(func() (string, string) { return "", "" }, func(host string) { persisted = host })(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected manual endpoint login to succeed, got %d: %s", rr.Code, rr.Body.String())
	}
	if persisted != kitsu.URL+"/" {
		t.Fatalf("expected validated manual endpoint to persist, got %q", persisted)
	}
}

func TestLoginHandlerWithDiscoveryRejectsInvalidOrPlaceholderManualHost(t *testing.T) {
	for _, hostname := range []string{"http://YOUR_KITSU_HOST/", "not a URL"} {
		t.Run(hostname, func(t *testing.T) {
			resetSessions()
			form := url.Values{"hostname": {hostname}, "email": {"manager@example.com"}, "password": {"not-returned"}}
			req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			LoginHandlerWithDiscovery(func() (string, string) { return "", "" }, nil)(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected invalid manual endpoint to fail closed, got %d", rr.Code)
			}
		})
	}
}

func TestLoginPageShowsManualHostOnlyForFreshInit(t *testing.T) {
	fresh := loginPageHTML("en", "", "", true, nil)
	if !strings.Contains(fresh, `name="hostname"`) {
		t.Fatal("expected fresh-init login page to offer Kitsu base URL")
	}
	configured := loginPageHTML("en", "", "", false, nil)
	if strings.Contains(configured, `name="hostname"`) {
		t.Fatal("configured login page must not offer endpoint override")
	}
}

func TestLoginHandlerWithDiscoveryHidesManualHostWhenEndpointIsAvailable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bot/login", nil)
	rr := httptest.NewRecorder()
	LoginHandlerWithDiscovery(func() (string, string) { return "http://verified-kitsu/", "persisted" }, nil)(rr, req)
	if strings.Contains(rr.Body.String(), `name="hostname"`) {
		t.Fatal("verified discovered endpoint should not require manual URL input")
	}
}

func TestLoginHandlerRejectsMissingOrBelowManagerRole(t *testing.T) {
	for _, roleJSON := range []string{`"supervisor"`, `"user"`, `""`, `null`} {
		t.Run(strings.Trim(roleJSON, `"`), func(t *testing.T) {
			resetSessions()
			kitsu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"access_token":"browser-session-token","user":{"role":%s}}`, roleJSON)
			}))
			defer kitsu.Close()

			form := url.Values{"email": {"user@example.com"}, "password": {"not-returned"}}
			req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			LoginHandler(kitsu.URL)(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("expected role %s to be rejected, got %d", roleJSON, rr.Code)
			}
		})
	}
}
