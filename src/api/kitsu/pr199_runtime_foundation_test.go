package kitsu

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPR199PersonsWithCredentialsUsesProvidedRuntimeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/persons/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer persisted-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"person-1","full_name":"Persisted User"}]`))
	}))
	defer server.Close()

	t.Setenv("KitsuJWTToken", "")
	configureTestOrigin(t, server.URL)

	got, err := GetPersonsWithCredentials(server.URL+"/api", "persisted-token")
	if err != nil || len(got.Each) != 1 || got.Each[0].ID != "person-1" {
		t.Fatalf("persons=%+v err=%v", got.Each, err)
	}
}

func TestPR199ProjectsWithCredentialsPreservesEmptySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/projects/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer persisted-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	t.Setenv("KitsuJWTToken", "")
	configureTestOrigin(t, server.URL)

	got, err := GetProjectsWithCredentials(server.URL, "persisted-token")
	if err != nil || len(got.Each) != 0 {
		t.Fatalf("projects=%+v err=%v", got.Each, err)
	}
}
