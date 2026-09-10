package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
