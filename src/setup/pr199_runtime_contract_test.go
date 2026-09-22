package setup

import (
	"app/src/utils/request"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
)

func TestPR199RuntimeKitsuDataSourceFallsBackToEnvWithoutPersistedToken(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	t.Setenv("KitsuJWTToken", "legacy-env-token")
	t.Setenv("KITSU_API_BASE_URL", "https://kitsu.example.test/api")
	db := newRuntimeCredentialTestDB(t)

	base, token, ok := runtimeKitsuDataSource(db)
	if !ok || base != "https://kitsu.example.test/api" || token != "legacy-env-token" {
		t.Fatalf("base=%q token=%q ok=%t", base, token, ok)
	}
}

func TestPR199KitsuLookupDistinguishes401FromEmptySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/data/persons/":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("sensitive-response-body"))
		case "/api/data/projects/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: server.URL, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}

	people, err := ListKitsuPersonsWithCredentials(server.URL, "runtime-secret-token")
	if people != nil || err == nil {
		t.Fatalf("people=%v err=%v", people, err)
	}
	var lookupErr *KitsuLookupError
	if !errors.As(err, &lookupErr) || lookupErr.Class != "http_4xx" || lookupErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("lookup error=%+v", err)
	}
	if text := err.Error(); strings.Contains(text, "runtime-secret-token") || strings.Contains(text, "sensitive-response-body") || strings.Contains(text, server.URL) {
		t.Fatalf("safe lookup error leaked sensitive detail: %q", text)
	}

	projects, err := ListKitsuProjectsWithCredentials(server.URL, "runtime-secret-token")
	if err != nil || len(projects) != 0 {
		t.Fatalf("projects=%v err=%v", projects, err)
	}
}

func TestPR199KitsuLookupClassifiesInvalidAndRetried5xx(t *testing.T) {
	invalid := classifyKitsuLookupError("persons", errors.New("decode response: invalid character"))
	var invalidErr *KitsuLookupError
	if !errors.As(invalid, &invalidErr) || invalidErr.Class != "invalid_response" || invalidErr.StatusCode != 0 {
		t.Fatalf("invalid response classification=%+v", invalid)
	}

	exhausted := classifyKitsuLookupError("projects", errors.New("request failed after 3 attempts: HTTP status 502"))
	var exhaustedErr *KitsuLookupError
	if !errors.As(exhausted, &exhaustedErr) || exhaustedErr.Class != "http_5xx" || exhaustedErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("5xx classification=%+v", exhausted)
	}
}
