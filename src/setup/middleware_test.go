package setup

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func resetSessions() {
	sessionMu.Lock()
	sessions = map[string]sessionData{}
	sessionStoreDB = nil
	sessionMu.Unlock()
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
	req := httptest.NewRequest(http.MethodPost, "/bot/logout", nil)
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

func TestLogoutRejectsStateChangingGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bot/logout", nil)
	rr := httptest.NewRecorder()
	LogoutHandler()(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected GET logout to be rejected, got %d", rr.Code)
	}
}

func TestCSRFProtectionAllowsSameOriginPOST(t *testing.T) {
	resetSessions()
	token := newSessionToken("manager@example.com", "", "manager", "/bot/admin")
	req := httptest.NewRequest(http.MethodPost, "http://admin.example/bot/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	req.Header.Set("Origin", "http://admin.example")
	rr := httptest.NewRecorder()
	called := false
	CSRFProtection(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !called {
		t.Fatalf("same-origin POST must be allowed, status=%d called=%v", rr.Code, called)
	}
}

func TestCSRFProtectionRejectsCrossSiteFetchMetadata(t *testing.T) {
	resetSessions()
	token := newSessionToken("manager@example.com", "", "manager", "/bot/admin")
	req := httptest.NewRequest(http.MethodPost, "http://admin.example/bot/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "http://admin.example")
	rr := httptest.NewRecorder()
	called := false
	CSRFProtection(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || called {
		t.Fatalf("cross-site Fetch Metadata must be rejected, status=%d called=%v", rr.Code, called)
	}
}

func TestCSRFProtectionOriginAndRefererValidation(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		referer    string
		forwarded  string
		wantStatus int
	}{
		{name: "wrong origin", origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "correct origin", origin: "https://admin.example", forwarded: "https", wantStatus: http.StatusOK},
		{name: "same origin referer fallback", referer: "http://admin.example/bot/admin", wantStatus: http.StatusOK},
		{name: "wrong referer fallback", referer: "http://evil.example/bot/admin", wantStatus: http.StatusForbidden},
		{name: "missing browser origin headers", wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetSessions()
			token := newSessionToken("manager@example.com", "", "manager", "/bot/admin")
			req := httptest.NewRequest(http.MethodPost, "http://admin.example/bot/admin", nil)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwarded)
			}
			rr := httptest.NewRecorder()
			called := false
			CSRFProtection(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rr, req)
			if rr.Code != tt.wantStatus || called != (tt.wantStatus == http.StatusOK) {
				t.Fatalf("status=%d called=%v, want status=%d", rr.Code, called, tt.wantStatus)
			}
		})
	}
}

func TestCSRFProtectionLeavesGETAndUnauthenticatedRequestsUnchanged(t *testing.T) {
	resetSessions()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "http://admin.example/bot/admin", nil)
		rr := httptest.NewRecorder()
		called := false
		CSRFProtection(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rr, req)
		if !called || rr.Code != http.StatusOK {
			t.Fatalf("%s should remain unchanged, status=%d called=%v", method, rr.Code, called)
		}
	}
	// An unauthenticated mutation is passed through so RequireSession can
	// preserve its existing redirect behavior.
	req := httptest.NewRequest(http.MethodPost, "http://admin.example/bot/admin", nil)
	rr := httptest.NewRecorder()
	called := false
	CSRFProtection(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(rr, req)
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("unauthenticated request should remain available to auth middleware")
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

func TestPersistentSessionSurvivesProcessCacheResetWithoutPersistingKitsuToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&model.AdminSession{}); err != nil {
		t.Fatal(err)
	}
	ConfigureSessionStore(db)
	t.Cleanup(resetSessions)

	token := newSessionToken("manager@example.com", "short-lived-kitsu-token", "manager", "/bot/admin")
	sessionMu.Lock()
	sessions = map[string]sessionData{}
	sessionMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/bot/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	if !validSession(token) {
		t.Fatal("expected persisted session to remain valid after process cache reset")
	}
	session, ok := currentSessionData(req)
	if !ok || session.Email != "manager@example.com" || session.Role != "manager" {
		t.Fatalf("expected persisted identity to hydrate, got %+v, ok=%v", session, ok)
	}
	if session.KitsuToken != "" {
		t.Fatal("persistent session must not hydrate a Kitsu token")
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/bot/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	LogoutHandler()(httptest.NewRecorder(), logoutReq)
	if validSession(token) {
		t.Fatal("logout must invalidate the persisted session")
	}
}

func TestClassifySessionPersistenceError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"missing table", errors.New("no such table: admin_sessions"), "schema_table_missing"},
		{"incompatible schema", errors.New("table admin_sessions has no column named token_hash"), "schema_incompatible"},
		{"readonly", errors.New("attempt to write a readonly database"), "database_readonly"},
		{"busy", errors.New("database is locked"), "database_busy"},
		{"constraint", errors.New("UNIQUE constraint failed: admin_sessions.token_hash"), "constraint_failed"},
		{"other", errors.New("driver failure"), "persistence_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySessionPersistenceError(tc.err); got != tc.want {
				t.Fatalf("classification = %q, want %q", got, tc.want)
			}
		})
	}
}

func zouFixture(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"Zou","database-up":true,"event-stream-up":true,"job-queue-up":true,"key-value-store-up":true,"version":"0.11.3"}`))
			return
		}
		next(w, r)
	}
}

func TestLoginReportsAuthenticatedButSessionPersistenceFailure(t *testing.T) {
	resetSessions()
	db := newSetupStateTestDB(t) // Deliberately has no admin_sessions table.
	ConfigureSessionStore(db)
	t.Cleanup(resetSessions)

	kitsu := httptest.NewServer(zouFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			t.Fatalf("unexpected Kitsu path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"browser-session-token","user":{"role":"admin"}}`)
	})))
	defer kitsu.Close()

	form := url.Values{"email": {"admin@example.com"}, "password": {"not-returned"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/login?lang=en", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	LoginHandler(kitsu.URL)(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("login status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Kitsu authentication succeeded, but KitsuSync could not save the admin session.") {
		t.Fatalf("missing safe post-auth persistence message: %s", body)
	}
	for _, secret := range []string{"not-returned", "browser-session-token"} {
		if strings.Contains(body, secret) {
			t.Fatalf("login response exposed secret-like test value %q", secret)
		}
	}
}

func TestLoginHandlerUsesConfiguredHostAndAcceptsAdmin(t *testing.T) {
	resetSessions()
	kitsu := httptest.NewServer(zouFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"browser-session-token","user":{"role":"admin"}}`)
	})))
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
	kitsu := httptest.NewServer(zouFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"browser-session-token","user":{"role":"user"}}`)
	})))
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
			kitsu := httptest.NewServer(zouFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"access_token":"browser-session-token","user":{"role":%q}}`, role)
			})))
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
	kitsu := httptest.NewServer(zouFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})))
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
	if strings.TrimRight(persisted, "/") != strings.TrimRight(kitsu.URL, "/") {
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
			kitsu := httptest.NewServer(zouFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"access_token":"browser-session-token","user":{"role":%s}}`, roleJSON)
			})))
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
