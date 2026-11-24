package setup

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBotDoDoesNotFollowRedirect(t *testing.T) {
	var captureCalls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Location", server.URL+"/capture")
			w.WriteHeader(http.StatusFound)
		case "/capture":
			captureCalls++
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, status, err := botDo(http.MethodGet, server.URL+"/start", nil, "test-bot-token")
	if err != nil {
		t.Fatalf("botDo returned error on 302: %v", err)
	}
	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}
	if captureCalls != 0 {
		t.Fatalf("redirect target was requested %d times, want 0", captureCalls)
	}
}

func TestBotDoCapsResponseBodyAt4MiB(t *testing.T) {
	const limit = 4 << 20
	payload := bytes.Repeat([]byte("a"), limit+1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	body, status, err := botDo(http.MethodGet, server.URL, nil, "test-bot-token")
	if err != nil {
		t.Fatalf("botDo returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if len(body) != limit {
		t.Fatalf("len(body) = %d, want %d", len(body), limit)
	}
}
