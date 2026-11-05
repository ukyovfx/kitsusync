package request

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newRetryFixture(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	if err := ConfigureVerifiedOrigin(VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
	prev := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = prev })
	return server, hits
}

func TestDoWithErrorRetriesOnly5xxForIdempotentMethods(t *testing.T) {
	cases := []struct {
		method string
		want   int32
	}{
		{http.MethodGet, 3},
		{http.MethodHead, 3},
		{http.MethodPost, 1},
		{http.MethodPut, 1},
		{http.MethodPatch, 1},
		{http.MethodDelete, 1},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			server, hits := newRetryFixture(t, http.StatusInternalServerError)
			var payload interface{}
			if tc.method != http.MethodGet && tc.method != http.MethodHead {
				payload = map[string]string{"k": "v"}
			}
			if _, err := DoWithError("", tc.method, server.URL+"/x", payload, nil); err == nil {
				t.Fatal("expected error for 5xx")
			}
			if got := hits.Load(); got != tc.want {
				t.Fatalf("%s sent %d times, want %d", tc.method, got, tc.want)
			}
		})
	}
}

func TestDoWithErrorDoesNotRetryPostOnNetworkError(t *testing.T) {
	server, hits := newRetryFixture(t, http.StatusOK)
	// Close the connection mid-response so the client sees a network error.
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	})
	if _, err := DoWithError("", http.MethodPost, server.URL+"/x", map[string]string{"k": "v"}, nil); err == nil {
		t.Fatal("expected network error")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("POST sent %d times, want 1", got)
	}
}

func TestDoWithErrorErrorsDoNotLeakQueryString(t *testing.T) {
	server, _ := newRetryFixture(t, http.StatusOK)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	})
	_, err := DoWithError("", http.MethodPost, server.URL+"/x?secret=abc123", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "abc123") {
		t.Fatalf("error leaks query string: %v", err)
	}
	var uerr *url.Error
	if !errors.As(err, &uerr) || uerr.URL == "" {
		t.Fatalf("expected redacted *url.Error, got %v", err)
	}
}

func TestRedactURL(t *testing.T) {
	got := redactURL("https://user:pw@kitsu.example/api/data?token=abc#frag")
	if got != "https://kitsu.example/api/data" {
		t.Fatalf("redactURL = %q", got)
	}
}
