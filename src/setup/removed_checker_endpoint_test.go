package setup

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRemovedCheckerMappingEndpointIsUnreachable pins the removal of the
// lane-era checker setting API (owner decision 2026-11-19). main() no longer
// registers POST /api/setup/mapping/checkers for either prefix: the /bot
// prefix falls through to BotRootHandler's NotFound branch, and the root
// prefix has no matching pattern in the server mux, so net/http answers 404.
func TestRemovedCheckerMappingEndpointIsUnreachable(t *testing.T) {
	mux := http.NewServeMux()
	// Mirrors main(): the /bot prefix registers only BotRootHandler; nothing
	// registers the removed checker mapping route on either prefix.
	mux.HandleFunc("/bot/", BotRootHandler(func() bool { return true }))
	server := httptest.NewServer(mux)
	defer server.Close()

	for _, path := range []string{
		"/bot/api/setup/mapping/checkers",
		"/api/setup/mapping/checkers",
	} {
		resp, err := http.Post(server.URL+path, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
			t.Fatalf("%s: want 404, got %d (%s)", path, resp.StatusCode, body)
		}
	}
}
