package kitsu

import (
	"app/src/utils/request"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"testing"
)

func configureTestOrigin(t *testing.T, raw string) {
	t.Helper()
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: raw, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
}

func TestKitsuBaseUsesExplicitAPIOverrideWithoutDuplicateAPIPath(t *testing.T) {
	t.Setenv("KITSU_API_BASE_URL", "https://api.example.test/studio/api/")
	t.Setenv("KITSU_HOSTNAME", "https://display.example.test")
	if got := kitsuBase(); got != "https://api.example.test/studio/" {
		t.Fatalf("kitsuBase() = %q, want explicit API parent", got)
	}
}

func TestExplicitAPIOverrideLeavesDisplayURLUnchanged(t *testing.T) {
	t.Setenv("KITSU_API_BASE_URL", "https://private.example.test/api")
	t.Setenv("KITSU_HOSTNAME", "https://public.example.test/studio")
	if got := os.Getenv("KITSU_HOSTNAME"); got != "https://public.example.test/studio" {
		t.Fatalf("display URL changed to %q", got)
	}
	if got := kitsuBase(); got != "https://private.example.test/" {
		t.Fatalf("runtime API base = %q", got)
	}
}

func TestConfiguredAPIOverrideUsesVerifiedOriginForCredentialRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/tasks" {
			t.Fatalf("unexpected override path: %s", r.URL.Path)
		}
		if r.Host == "127.0.0.1" || r.Header.Get("Authorization") != "Bearer override-canary-token" {
			t.Fatalf("override request lost verified host or bearer: host=%q auth=%q", r.Host, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	t.Setenv("KITSU_API_BASE_URL", server.URL+"/api/")
	t.Setenv("KITSU_HOSTNAME", "https://display-only.example.test")
	t.Setenv("KitsuJWTToken", "override-canary-token")
	configureTestOrigin(t, server.URL)

	got, err := GetTasksWithError()
	if err != nil {
		t.Fatalf("configured API override request failed: %v", err)
	}
	if len(got.Each) != 0 {
		t.Fatalf("expected empty task response, got %+v", got.Each)
	}
}

func TestGetProjectTaskTypesUsesProductionScopedEndpointAndPreservesContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/projects/production-1/task-types" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"tt-asset-concept","name":"Concept","short_name":"concept","for_entity":"Asset","department_id":"dept-concept","department_name":"Concept","active":true},{"id":"tt-shot-storyboard","name":"Storyboard","short_name":"storyboard","for_entity":"Shot","department_id":"dept-animation","archived":false}]`))
	}))
	defer server.Close()
	t.Setenv("KITSU_HOSTNAME", server.URL+"/")
	t.Setenv("KitsuJWTToken", "test-token")
	configureTestOrigin(t, server.URL)

	got := GetProjectTaskTypes("production-1").Each
	if len(got) != 2 {
		t.Fatalf("got %d Task Types, want 2", len(got))
	}
	if got[0].ForEntity != "Asset" || got[0].DepartmentID != "dept-concept" || got[0].DepartmentName != "Concept" || !got[0].Active {
		t.Fatalf("first Task Type context was not preserved: %+v", got[0])
	}
	if got[1].ForEntity != "Shot" || got[1].Archived {
		t.Fatalf("second Task Type context was not preserved: %+v", got[1])
	}
}

func TestGetProjectTeamUsesProductionScopedReadEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/projects/production-1/team" || r.Method != http.MethodGet {
			t.Fatalf("unexpected project team request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"person-1","full_name":"Artist A","active":true,"archived":false,"is_bot":false}]`))
	}))
	defer server.Close()
	t.Setenv("KITSU_HOSTNAME", server.URL+"/")
	t.Setenv("KitsuJWTToken", "test-token")
	configureTestOrigin(t, server.URL)

	got := GetProjectTeam("production-1")
	if len(got) != 1 || got[0].ID != "person-1" || got[0].FullName != "Artist A" || got[0].IsBot {
		t.Fatalf("unexpected project team response: %+v", got)
	}
}

func TestGetTasksWithErrorReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	t.Setenv("KITSU_HOSTNAME", server.URL+"/")
	t.Setenv("KitsuJWTToken", "test-token")
	configureTestOrigin(t, server.URL)

	got, err := GetTasksWithError()
	if err == nil {
		t.Fatal("expected Kitsu request failure")
	}
	if len(got.Each) != 0 {
		t.Fatalf("failed request must not produce tasks: %+v", got.Each)
	}
}

func TestGetTasksWithErrorAcceptsLegitimateEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	t.Setenv("KITSU_HOSTNAME", server.URL+"/")
	t.Setenv("KitsuJWTToken", "test-token")
	configureTestOrigin(t, server.URL)

	got, err := GetTasksWithError()
	if err != nil {
		t.Fatalf("legitimate empty response returned an error: %v", err)
	}
	if len(got.Each) != 0 {
		t.Fatalf("expected no tasks, got %+v", got.Each)
	}
}
