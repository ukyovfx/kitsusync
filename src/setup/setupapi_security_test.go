package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

const validZouStatus = `{"name":"Zou","database-up":true,"event-stream-up":true,"job-queue-up":true,"key-value-store-up":true,"version":"0.11.3"}`

func tryKitsuLoginHost(t *testing.T, serverURL string) string {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	return "http://kitsu.test:" + u.Port()
}

func pinTryKitsuLoginToLoopback(t *testing.T, lookup func(context.Context, string) ([]netip.Addr, error)) {
	t.Helper()
	old := kitsuLookupNetIP
	kitsuLookupNetIP = lookup
	t.Cleanup(func() { kitsuLookupNetIP = old })
}

func TestTryKitsuLoginRejects307WithoutCredentialReplay(t *testing.T) {
	testTryKitsuLoginRedirectDoesNotReplay(t, http.StatusTemporaryRedirect)
}

func TestTryKitsuLoginRejects308WithoutCredentialReplay(t *testing.T) {
	testTryKitsuLoginRedirectDoesNotReplay(t, http.StatusPermanentRedirect)
}

func testTryKitsuLoginRedirectDoesNotReplay(t *testing.T, status int) {
	var authCalls, captureCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(validZouStatus))
		case "/api/auth/login":
			authCalls++
			w.Header().Set("Location", server.URL+"/capture")
			w.WriteHeader(status)
		case "/capture":
			captureCalls++
		}
	}))
	defer server.Close()
	pinTryKitsuLoginToLoopback(t, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})

	ok, class := tryKitsuLogin(tryKitsuLoginHost(t, server.URL), "user@example.com", "not-returned")
	if ok || class != "auth_redirect_blocked" {
		t.Fatalf("result = ok:%v class:%q", ok, class)
	}
	if authCalls != 1 || captureCalls != 0 {
		t.Fatalf("auth=%d capture=%d", authCalls, captureCalls)
	}
}

func TestTryKitsuLoginBlocksDNSChangeBeforeCredentialDelivery(t *testing.T) {
	var authCalls, lookups int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(validZouStatus))
		case "/api/auth/login":
			authCalls++
		}
	}))
	defer server.Close()
	pinTryKitsuLoginToLoopback(t, func(context.Context, string) ([]netip.Addr, error) {
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.2")}, nil
	})

	ok, class := tryKitsuLogin(tryKitsuLoginHost(t, server.URL), "user@example.com", "not-returned")
	if ok || class != "dns_scope_changed" {
		t.Fatalf("result = ok:%v class:%q", ok, class)
	}
	if authCalls != 0 {
		t.Fatalf("credentials reached auth endpoint %d times", authCalls)
	}
}

func TestTryKitsuLoginSucceedsForVerifiedTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(validZouStatus))
		case "/api/auth/login":
			if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
				t.Fatal("missing JSON content type")
			}
			_, _ = w.Write([]byte(`{"access_token":"test-token","user":{"role":"manager"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	pinTryKitsuLoginToLoopback(t, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})

	ok, class := tryKitsuLogin(tryKitsuLoginHost(t, server.URL), "user@example.com", "not-returned")
	if !ok || class != "" {
		t.Fatalf("result = ok:%v class:%q", ok, class)
	}
}
