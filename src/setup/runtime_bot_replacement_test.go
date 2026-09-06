package setup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRuntimeBotReplacementUsesPublicPersonAPI(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"Zou","database-up":true,"event-stream-up":true,"job-queue-up":true,"key-value-store-up":true,"version":"0.11.3"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"admin-token"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/data/persons":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/authenticated":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/data/persons":
			var body map[string]interface{}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["is_bot"] != true || body["email"] != "temp@example.com" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"id":"replacement-id","email":"temp@example.com","is_bot":true,"active":true,"archived":false,"role":"admin","access_token":"replacement-token"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/data/persons/replacement-id":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	id, err := PrepareRuntimeBotReplacement(db, server.URL, "admin@example.com", "admin-password", "temp@example.com", "bot-password")
	if err != nil || id != "replacement-id" {
		t.Fatalf("prepare failed: id=%q err=%v", id, err)
	}
	if StoredRuntimeKitsuToken(db) != "replacement-token" {
		t.Fatal("replacement token was not persisted")
	}
	if strings.Contains(id, "password") {
		t.Fatal("secret leaked in replacement result")
	}
}
