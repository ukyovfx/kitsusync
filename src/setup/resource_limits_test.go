package setup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestBodyLimitRejectsOversizedUnauthenticatedLogin(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", maxLoginRequestBodyBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/bot/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	RequestBodyLimit(LoginHandler("")).ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestLoginHandlerRejectsOversizedBodyEvenWithoutOuterMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/bot/login", strings.NewReader(strings.Repeat("x", maxLoginRequestBodyBytes+1)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	LoginHandler("").ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRequestTraceRejectsOversizedFormBeforeAuthentication(t *testing.T) {
	nextCalled := false
	requestBody := "field=" + strings.Repeat("x", maxTraceActionBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/health", strings.NewReader(requestBody))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler := RequestBodyLimit(RequestTrace(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
	if nextCalled {
		t.Fatal("oversized traced request reached downstream authentication/handler")
	}
}

func TestRequestBodyLimitAllowsNormalAdminRequest(t *testing.T) {
	nextCalled := false
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/health", strings.NewReader("action=refresh"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler := RequestBodyLimit(RequestTrace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		if err := r.ParseForm(); err != nil || r.FormValue("action") != "refresh" {
			t.Errorf("normal form was not preserved: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent || !nextCalled {
		t.Fatalf("normal request status=%d downstream=%v", rr.Code, nextCalled)
	}
}

func TestLoginRateLimitUsesDirectPeerAndIgnoresForwardedHeader(t *testing.T) {
	const peer = "198.51.100.240"
	key := "direct:" + peer
	loginAttempts.Lock()
	delete(loginAttempts.started, key)
	loginAttempts.Unlock()
	t.Cleanup(func() {
		loginAttempts.Lock()
		delete(loginAttempts.started, key)
		loginAttempts.Unlock()
	})

	called := 0
	handler := LoginRateLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called++
	}))
	getReq := httptest.NewRequest(http.MethodGet, "/bot/login", nil)
	getReq.RemoteAddr = peer + ":1234"
	getRR := httptest.NewRecorder()
	handler.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK || called != 1 {
		t.Fatalf("GET login was rate limited: status=%d calls=%d", getRR.Code, called)
	}
	for i := 0; i < maxLoginAttemptsPerPeer; i++ {
		req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
		req.RemoteAddr = peer + ":1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want %d", i+1, rr.Code, http.StatusOK)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
	req.RemoteAddr = peer + ":1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.8")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt %d status = %d, want %d", maxLoginAttemptsPerPeer+1, rr.Code, http.StatusTooManyRequests)
	}
	if called != maxLoginAttemptsPerPeer+1 {
		t.Fatalf("downstream calls = %d, want %d", called, maxLoginAttemptsPerPeer+1)
	}
}

func TestLoginRateLimitUsesForwardedIdentityOnlyFromLoopbackProxy(t *testing.T) {
	keys := []string{"forwarded:198.51.100.241", "forwarded:198.51.100.242", "direct:198.51.100.243"}
	loginAttempts.Lock()
	for _, key := range keys {
		delete(loginAttempts.started, key)
	}
	loginAttempts.Unlock()
	t.Cleanup(func() {
		loginAttempts.Lock()
		for _, key := range keys {
			delete(loginAttempts.started, key)
		}
		loginAttempts.Unlock()
	})

	called := 0
	handler := LoginRateLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called++ }))
	for i := 0; i < maxLoginAttemptsPerPeer; i++ {
		for _, client := range []string{"198.51.100.241", "198.51.100.242"} {
			req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
			req.RemoteAddr = "127.0.0.1:8090"
			req.Header.Set("X-Real-IP", client)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("proxied client %s attempt %d status=%d", client, i+1, rr.Code)
			}
		}
	}
	for _, client := range []string{"198.51.100.241", "198.51.100.242"} {
		req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
		req.RemoteAddr = "127.0.0.1:8090"
		req.Header.Set("X-Real-IP", client)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("proxied client %s attempt %d status=%d", client, maxLoginAttemptsPerPeer+1, rr.Code)
		}
	}
	spoof := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
	spoof.RemoteAddr = "198.51.100.243:8090"
	spoof.Header.Set("X-Real-IP", "198.51.100.244")
	for i := 0; i < maxLoginAttemptsPerPeer; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, spoof)
		if rr.Code != http.StatusOK {
			t.Fatalf("direct spoof attempt %d status=%d", i+1, rr.Code)
		}
	}
	spoof.Header.Set("X-Forwarded-For", "198.51.100.245")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, spoof)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed forwarded header bypassed direct peer limit: status=%d", rr.Code)
	}
	if called != 2*maxLoginAttemptsPerPeer+maxLoginAttemptsPerPeer {
		t.Fatalf("downstream calls=%d", called)
	}
}

func TestLoginRateLimitDoesNotTrustPrivatePeerForwardedIdentity(t *testing.T) {
	const key = "direct:172.20.0.2"
	loginAttempts.Lock()
	delete(loginAttempts.started, key)
	loginAttempts.Unlock()
	t.Cleanup(func() {
		loginAttempts.Lock()
		delete(loginAttempts.started, key)
		loginAttempts.Unlock()
	})
	handler := LoginRateLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for i := 0; i < maxLoginAttemptsPerPeer; i++ {
		req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
		req.RemoteAddr = "172.20.0.2:8090"
		req.Header.Set("X-Real-IP", "198.51.100.250")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("private peer attempt %d status=%d", i+1, rr.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
	req.RemoteAddr = "172.20.0.2:8090"
	req.Header.Set("X-Forwarded-For", "198.51.100.251")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("private peer spoof bypassed limiter: status=%d", rr.Code)
	}
}

func TestDiagnosticsDoesNotMutateLegacyWritePath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "data", ".diag_write_test")
	const sentinel = "preserve this file"
	if err := os.WriteFile(path, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}

	runDiagnostics("en", "", "", "", "", newSetupStateTestDB(t))

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("legacy path was removed: %v", err)
	}
	if string(got) != sentinel {
		t.Fatalf("legacy path changed to %q", string(got))
	}
}

func TestDiagnosticsDoesNotFollowLegacyWritePathSymlink(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	const sentinel = "target remains unchanged"
	if err := os.WriteFile(target, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "data", ".diag_write_test")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	runDiagnostics("en", "", "", "", "", newSetupStateTestDB(t))

	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("legacy symlink was removed: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Fatalf("symlink target changed to %q", string(got))
	}
}

func TestConcurrentDiagnosticsDoNotShareAWritePath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "data", ".diag_write_test")
	const sentinel = "concurrent sentinel"
	if err := os.WriteFile(path, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}
	db := newSetupStateTestDB(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runDiagnostics("en", "", "", "", "", db)
		}()
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("legacy path was removed during concurrent diagnostics: %v", err)
	}
	if string(got) != sentinel {
		t.Fatalf("legacy path changed during concurrent diagnostics to %q", string(got))
	}
}

func TestLoginRateLimitEvictsStaleKeysAndAdmitsNewClientsAfterCap(t *testing.T) {
	loginAttempts.Lock()
	saved := loginAttempts.started
	loginAttempts.started = make(map[string][]time.Time)
	loginAttempts.Unlock()
	t.Cleanup(func() {
		loginAttempts.Lock()
		loginAttempts.started = saved
		loginAttempts.Unlock()
	})

	handler := LoginRateLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	post := func(addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
		req.RemoteAddr = addr
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}
	// Fill past the cap with unique, still-active sources.
	for i := 0; i < maxRateLimitKeys+100; i++ {
		addr := fmt.Sprintf("10.%d.%d.%d:1000", i>>16&255, i>>8&255, i&255)
		if code := post(addr); code != http.StatusOK {
			t.Fatalf("unique source %d status=%d", i, code)
		}
	}
	loginAttempts.Lock()
	size := len(loginAttempts.started)
	loginAttempts.Unlock()
	if size > maxRateLimitKeys {
		t.Fatalf("limiter grew to %d keys, cap %d", size, maxRateLimitKeys)
	}
	if code := post("198.51.100.77:1000"); code != http.StatusOK {
		t.Fatalf("new client after cap status=%d", code)
	}
	// Expired keys are dropped first.
	loginAttempts.Lock()
	for k := range loginAttempts.started {
		loginAttempts.started[k] = []time.Time{time.Now().Add(-2 * time.Minute)}
	}
	loginAttempts.started["direct:198.51.100.77"] = []time.Time{time.Now()}
	loginAttempts.Unlock()
	if code := post("198.51.100.78:1000"); code != http.StatusOK {
		t.Fatalf("new client after expiry status=%d", code)
	}
	loginAttempts.Lock()
	size = len(loginAttempts.started)
	loginAttempts.Unlock()
	if size != 2 {
		t.Fatalf("expired keys not evicted: %d keys", size)
	}
}

func TestLoginRateLimitIgnoresClientSuppliedForwardedForPrefix(t *testing.T) {
	handler := LoginRateLimit(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	const real = "198.51.100.90"
	t.Cleanup(func() {
		loginAttempts.Lock()
		delete(loginAttempts.started, "forwarded:"+real)
		loginAttempts.Unlock()
	})
	var last int
	for i := 0; i < maxLoginAttemptsPerPeer+1; i++ {
		req := httptest.NewRequest(http.MethodPost, "/bot/login", nil)
		req.RemoteAddr = "127.0.0.1:8090"
		// Proxy appends the real peer; the leading value is attacker-chosen.
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d, %s", i+1, real))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		last = rr.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("rotating XFF prefix bypassed limit: status=%d", last)
	}
}
