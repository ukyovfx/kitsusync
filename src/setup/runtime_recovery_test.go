package setup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"app/src/model"
)

func TestRecoverRuntimeCredentialsAuthenticatesAndPersists(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Email != "kitsusync-bot@google.com" || body.Password != "new-password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"not-used"}`))
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	if err := RecoverRuntimeCredentials(db, server.URL, "kitsusync-bot@google.com", "new-password"); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if model.GetSetting(db, RuntimeKitsuEmailSettingKey) != "kitsusync-bot@google.com" || StoredRuntimeKitsuPassword(db) != "new-password" {
		t.Fatal("recovered credentials were not persisted and decryptable")
	}
}

func TestRecoverRuntimeCredentialsStopsOnAuthenticationFailure(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	if err := RecoverRuntimeCredentials(db, server.URL, "kitsusync-bot@google.com", "new-password"); err == nil {
		t.Fatal("expected authentication failure")
	}
	if model.GetSetting(db, RuntimeKitsuEmailSettingKey) != "" || model.GetSetting(db, RuntimeKitsuPasswordSettingKey) != "" {
		t.Fatal("credentials were persisted after authentication failure")
	}
}

func TestRecoverRuntimeCredentialsStopsOnPersistenceFailure(t *testing.T) {
	keyDir := t.TempDir()
	t.Setenv(RuntimeSecretKeyFileEnv, keyDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	if err := RecoverRuntimeCredentials(db, server.URL, "kitsusync-bot@google.com", "new-password"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if model.GetSetting(db, RuntimeKitsuEmailSettingKey) != "" || model.GetSetting(db, RuntimeKitsuPasswordSettingKey) != "" {
		t.Fatal("credentials persisted after failure")
	}
}

func TestRecoverRuntimeTokenValidatesAndPersists(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/authenticated" || r.Header.Get("Authorization") != "Bearer replacement-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	if err := RecoverRuntimeToken(db, server.URL, "temp@example.com", "replacement-token"); err != nil {
		t.Fatalf("token recovery failed: %v", err)
	}
	if StoredRuntimeKitsuToken(db) != "replacement-token" {
		t.Fatal("runtime token was not persisted")
	}
}
