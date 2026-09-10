package setup

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func writeZouStatus(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"name":"Zou","version":"1","database-up":true,"event-stream-up":true,"job-queue-up":true,"key-value-store-up":true}`))
}

func withPinnedTestHost(t *testing.T, serverURL string, lookup func(context.Context, string) ([]netip.Addr, error)) string {
	t.Helper()
	old := kitsuLookupNetIP
	kitsuLookupNetIP = lookup
	t.Cleanup(func() { kitsuLookupNetIP = old })
	_, port, err := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return "http://kitsu.test:" + port
}

func TestDeletionReauthenticationUsesPinnedNoProxyTransport(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyCalls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			writeZouStatus(w)
		case "/api/auth/login":
			_, _ = w.Write([]byte(`{"access_token":"test-token","user":{"role":"admin"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	host := withPinnedTestHost(t, server.URL, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	if err := authenticateDeleteAdmin(context.Background(), nil, host, "admin@example.com", "secret"); err != nil {
		t.Fatalf("safe deletion reauthentication failed: %v", err)
	}
	if proxyCalls.Load() != 0 {
		t.Fatalf("credential request used environment proxy %d times", proxyCalls.Load())
	}
}

func TestDeletionReauthenticationBlocksDNSChangeBeforeCredentialPost(t *testing.T) {
	var lookups, authCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			writeZouStatus(w)
			return
		}
		if r.URL.Path == "/api/auth/login" {
			authCalls.Add(1)
		}
	}))
	defer server.Close()
	host := withPinnedTestHost(t, server.URL, func(context.Context, string) ([]netip.Addr, error) {
		if lookups.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("198.51.100.10")}, nil
	})
	err := authenticateDeleteAdmin(context.Background(), nil, host, "admin@example.com", "secret")
	if connectionErrorClass(err) != "dns_scope_changed" || authCalls.Load() != 0 {
		t.Fatalf("DNS change error=%v auth_calls=%d", err, authCalls.Load())
	}
}

func TestDiagnosticsBearerCheckBlocksRedirect(t *testing.T) {
	var replayCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			writeZouStatus(w)
		case "/api/auth/authenticated":
			w.Header().Set("Location", server.URL+"/capture")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/capture":
			replayCalls.Add(1)
		}
	}))
	defer server.Close()
	err := verifyDiagnosticsRuntimeToken(context.Background(), nil, server.URL, "runtime-token")
	if connectionErrorClass(err) != "auth_redirect_blocked" || replayCalls.Load() != 0 {
		t.Fatalf("diagnostics redirect error=%v replay_calls=%d", err, replayCalls.Load())
	}
}

func TestCredentialCallerInventoryUsesApprovedTransports(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source tree")
	}
	srcRoot := filepath.Clean(filepath.Join(filepath.Dir(current), ".."))
	approvedBearer := map[string]bool{
		filepath.Clean("setup/kitsu_connection_security.go"): true,
		filepath.Clean("setup/kitsu_bot_validation.go"):      true,
		filepath.Clean("utils/request/request.go"):           true,
	}
	err := filepath.Walk(srcRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{"utils/basicauth", "AuthForJWTToken", "ValidateJWTToken"} {
			if strings.Contains(text, forbidden) {
				return fmt.Errorf("%s contains retired credential client %q", rel, forbidden)
			}
		}
		if strings.Contains(text, `Header.Set("Authorization", "Bearer `) && !approvedBearer[filepath.Clean(rel)] {
			return fmt.Errorf("%s sends a bearer token outside an approved pinned transport", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
