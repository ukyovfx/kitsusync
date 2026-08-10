package kitsu

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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

	got := GetProjectTeam("production-1")
	if len(got) != 1 || got[0].ID != "person-1" || got[0].FullName != "Artist A" || got[0].IsBot {
		t.Fatalf("unexpected project team response: %+v", got)
	}
}
