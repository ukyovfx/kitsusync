package setup

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
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
	revokedSessions = map[string]time.Time{}
	sessionStoreDB = nil
	sessionMu.Unlock()
}

func addRecentBotEditSession(t *testing.T, req *http.Request) string {
	t.Helper()
	resetSessions()
	token := newSessionToken("manager@example.com", "jwt-token", "manager", "/bot/admin/bot?edit=1")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	t.Cleanup(resetSessions)
	return token
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
	logoutRecorder := httptest.NewRecorder()
	LogoutHandler()(logoutRecorder, logoutReq)
	if logoutRecorder.Code != http.StatusSeeOther {
		t.Fatalf("persistent logout status = %d, want %d", logoutRecorder.Code, http.StatusSeeOther)
	}
	if validSession(token) {
		t.Fatal("logout must invalidate the persisted session")
	}
	var sessionCount int64
	if err := db.Model(&model.AdminSession{}).Where("token_hash = ?", sessionTokenHash(token)).Count(&sessionCount).Error; err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 {
		t.Fatalf("persistent logout left %d session rows", sessionCount)
	}
}

func TestPersistentSessionLogoutReportsDeleteFailureAndFailsClosed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.AdminSession{}); err != nil {
		t.Fatal(err)
	}
	ConfigureSessionStore(db)
	t.Cleanup(resetSessions)

	token, err := newSessionTokenChecked("manager@example.com", "", "manager", "/bot/admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA query_only=ON").Error; err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	LogoutHandler()(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("logout status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	if validSession(token) {
		t.Fatal("session remained valid in the process after revocation failure")
	}
	if cookie := rr.Header().Get("Set-Cookie"); !strings.Contains(cookie, "Max-Age=0") {
		t.Fatalf("logout failure did not clear the browser cookie: %q", cookie)
	}
}

func TestPersistentSessionCacheObservesDatabaseRevocation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AdminSession{}); err != nil {
		t.Fatal(err)
	}
	ConfigureSessionStore(db)
	t.Cleanup(resetSessions)

	token, err := newSessionTokenChecked("manager@example.com", "", "manager", "/bot/admin")
	if err != nil {
		t.Fatal(err)
	}
	if !validSession(token) {
		t.Fatal("new persistent session was not valid")
	}
	if err := db.Where("token_hash = ?", sessionTokenHash(token)).Delete(&model.AdminSession{}).Error; err != nil {
		t.Fatal(err)
	}
	if validSession(token) {
		t.Fatal("database-revoked session remained valid from cache")
	}
}

func TestDatabaseRevocationSurvivesSessionStoreTransition(t *testing.T) {
	firstDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "first.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	secondDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "second.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []*gorm.DB{firstDB, secondDB} {
		if err := db.AutoMigrate(&model.AdminSession{}); err != nil {
			t.Fatal(err)
		}
	}
	ConfigureSessionStore(firstDB)
	t.Cleanup(resetSessions)

	token, err := newSessionTokenChecked("manager@example.com", "", "manager", "/bot/admin")
	if err != nil {
		t.Fatal(err)
	}
	var staleRow model.AdminSession
	if err := firstDB.Where("token_hash = ?", sessionTokenHash(token)).First(&staleRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := secondDB.Create(&staleRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := firstDB.Where("token_hash = ?", sessionTokenHash(token)).Delete(&model.AdminSession{}).Error; err != nil {
		t.Fatal(err)
	}
	if validSession(token) {
		t.Fatal("database-revoked session remained valid from cache")
	}

	ConfigureSessionStore(secondDB)
	if validSession(token) {
		t.Fatal("database-revoked session became valid after switching to stale store state")
	}
}

func TestPersistentSessionExpiryOverridesCachedSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AdminSession{}); err != nil {
		t.Fatal(err)
	}
	ConfigureSessionStore(db)
	t.Cleanup(resetSessions)

	token, err := newSessionTokenChecked("manager@example.com", "", "manager", "/bot/admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AdminSession{}).Where("token_hash = ?", sessionTokenHash(token)).Update("expiry", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if validSession(token) {
		t.Fatal("persistently expired session remained valid from cache")
	}
}

func TestPersistentRevocationSurvivesCacheReset(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AdminSession{}); err != nil {
		t.Fatal(err)
	}
	ConfigureSessionStore(db)
	t.Cleanup(resetSessions)

	token, err := newSessionTokenChecked("manager@example.com", "", "manager", "/bot/admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := destroySession(token); err != nil {
		t.Fatal(err)
	}
	sessionMu.Lock()
	sessions = map[string]sessionData{}
	revokedSessions = map[string]time.Time{}
	sessionMu.Unlock()
	if validSession(token) {
		t.Fatal("revoked session became valid after process cache reset")
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

func newCountingLoginKitsuServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	fixture := zouFixture(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"access_token":"browser-session-token","user":{"role":"manager"}}`)
	}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fixture(w, r)
	}))
	return server, &calls
}

func newLoginPersistenceKitsuServer(t *testing.T) *httptest.Server {
	t.Helper()
	server, _ := newCountingLoginKitsuServer(t)
	return server
}

func loginRequest(values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestLoginHandlerConfiguredAuthorityIgnoresCallerSelectedEndpoints(t *testing.T) {
	resetSessions()
	trusted, trustedCalls := newCountingLoginKitsuServer(t)
	attacker, attackerCalls := newCountingLoginKitsuServer(t)
	defer trusted.Close()
	defer attacker.Close()

	form := url.Values{
		"hostname":          {attacker.URL},
		"internal_hostname": {attacker.URL},
		"api_base_url":      {attacker.URL},
		"email":             {"manager@example.com"},
		"password":          {"not-returned"},
	}
	rr := httptest.NewRecorder()
	LoginHandler(trusted.URL)(rr, loginRequest(form))
	if rr.Code != http.StatusSeeOther || len(rr.Result().Cookies()) == 0 {
		t.Fatalf("trusted login status=%d cookies=%d", rr.Code, len(rr.Result().Cookies()))
	}
	if *trustedCalls == 0 || *attackerCalls != 0 {
		t.Fatalf("trusted requests=%d attacker requests=%d", *trustedCalls, *attackerCalls)
	}
}

func TestLoginHandlerSetupRequiredRejectsCallerSelectedFakeZou(t *testing.T) {
	for _, field := range []string{"hostname", "internal_hostname", "api_base_url"} {
		t.Run(field, func(t *testing.T) {
			resetSessions()
			fake, fakeCalls := newCountingLoginKitsuServer(t)
			defer fake.Close()
			persisted := false
			form := url.Values{field: {fake.URL}, "email": {"manager@example.com"}, "password": {"not-returned"}}
			rr := httptest.NewRecorder()
			LoginHandlerWithTrustedAuthority(func() KitsuLoginAuthority {
				return KitsuLoginAuthority{}
			}, nil, func(_, _, _ string) { persisted = true })(rr, loginRequest(form))
			if rr.Code != http.StatusServiceUnavailable || persisted || len(rr.Result().Cookies()) != 0 {
				t.Fatalf("field=%s status=%d persisted=%v cookies=%d", field, rr.Code, persisted, len(rr.Result().Cookies()))
			}
			if *fakeCalls != 0 {
				t.Fatalf("field=%s contacted attacker authority %d times", field, *fakeCalls)
			}
		})
	}
}

func TestLoginHandlerRejectsStatusDiscoveredAuthorityWithoutOperatorTrust(t *testing.T) {
	resetSessions()
	fake, fakeCalls := newCountingLoginKitsuServer(t)
	defer fake.Close()
	rr := httptest.NewRecorder()
	LoginHandlerWithTrustedAuthority(func() KitsuLoginAuthority {
		return KitsuLoginAuthority{RuntimeHost: fake.URL, Source: "local-discovered"}
	}, nil, nil)(rr, loginRequest(url.Values{"email": {"manager@example.com"}, "password": {"not-returned"}}))
	if rr.Code != http.StatusServiceUnavailable || len(rr.Result().Cookies()) != 0 {
		t.Fatalf("status=%d cookies=%d", rr.Code, len(rr.Result().Cookies()))
	}
	if *fakeCalls != 0 {
		t.Fatalf("status discovery contacted untrusted authority %d times", *fakeCalls)
	}
}

func TestLoginHandlerAcceptsOperatorEstablishedPrivateAndTailscaleAuthority(t *testing.T) {
	if got := classifyKitsuIP(netip.MustParseAddr("100.114.77.117")); got != "vpn/shared" {
		t.Fatalf("Tailscale address scope=%q", got)
	}
	resetSessions()
	private := newLoginPersistenceKitsuServer(t)
	defer private.Close()
	rr := httptest.NewRecorder()
	LoginHandlerWithTrustedAuthority(func() KitsuLoginAuthority {
		return KitsuLoginAuthority{RuntimeHost: private.URL, Source: "explicit"}
	}, nil, nil)(rr, loginRequest(url.Values{"email": {"manager@example.com"}, "password": {"not-returned"}}))
	if rr.Code != http.StatusSeeOther || len(rr.Result().Cookies()) == 0 {
		t.Fatalf("private authority status=%d cookies=%d", rr.Code, len(rr.Result().Cookies()))
	}
}

func TestLoginHandlerUsesOperatorEstablishedAPIOverride(t *testing.T) {
	resetSessions()
	trustedAPI, trustedCalls := newCountingLoginKitsuServer(t)
	attacker, attackerCalls := newCountingLoginKitsuServer(t)
	defer trustedAPI.Close()
	defer attacker.Close()
	const display = "https://public.kitsu.example.test/studio"
	var savedDisplay, savedRuntime, savedAPI string
	rr := httptest.NewRecorder()
	LoginHandlerWithTrustedAuthority(func() KitsuLoginAuthority {
		return KitsuLoginAuthority{RuntimeHost: display, APIBaseURL: trustedAPI.URL, Source: "persisted"}
	}, func() string { return display }, func(gotDisplay, gotRuntime, gotAPI string) {
		savedDisplay, savedRuntime, savedAPI = gotDisplay, gotRuntime, gotAPI
	})(rr, loginRequest(url.Values{
		"internal_hostname": {attacker.URL},
		"api_base_url":      {attacker.URL},
		"email":             {"manager@example.com"},
		"password":          {"not-returned"},
	}))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d: %s", rr.Code, rr.Body.String())
	}
	if strings.TrimRight(savedDisplay, "/") != display || savedRuntime != display || savedAPI != trustedAPI.URL {
		t.Fatalf("persisted display=%q runtime=%q api=%q", savedDisplay, savedRuntime, savedAPI)
	}
	if *trustedCalls == 0 || *attackerCalls != 0 {
		t.Fatalf("trusted API requests=%d attacker requests=%d", *trustedCalls, *attackerCalls)
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
			if rr.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected invalid manual endpoint to fail closed, got %d", rr.Code)
			}
		})
	}
}

func TestLoginPageNeverOffersAuthorityOverride(t *testing.T) {
	fresh := loginPageHTML("en", "", "", true, nil)
	configured := loginPageHTML("en", "", "", false, nil)
	for _, body := range []string{fresh, configured} {
		for _, field := range []string{`name="hostname"`, `name="internal_hostname"`, `name="api_base_url"`} {
			if strings.Contains(body, field) {
				t.Fatalf("login page exposed authority field %s", field)
			}
		}
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
