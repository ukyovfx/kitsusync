package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"app/src/model"
)

func TestRecoverRuntimeCredentialsAuthenticatesAndPersists(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"Zou","database-up":true,"event-stream-up":true,"job-queue-up":true,"key-value-store-up":true,"version":"0.11.3"}`))
			return
		}
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

func TestRecoverRuntimeCredentialsRejectsAuthRedirect(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"Zou","database-up":true,"event-stream-up":true,"job-queue-up":true,"key-value-store-up":true,"version":"0.11.3"}`))
			return
		}
		if r.URL.Path == "/api/auth/login" {
			w.Header().Set("Location", server.URL+"/capture")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path == "/capture" {
			t.Fatal("credentials were replayed to redirect target")
		}
	}))
	defer server.Close()
	db := newSetupStateTestDB(t)
	if err := RecoverRuntimeCredentials(db, server.URL, "bot@example.com", "secret"); err == nil {
		t.Fatal("expected redirect to be rejected")
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
		if r.URL.Path == "/api/status" {
			_, _ = w.Write([]byte(validZouStatus))
			return
		}
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

func TestRecoverRuntimeTokenUsesAPIOverride(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	var tokenChecks int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(validZouStatus))
		case "/api/auth/authenticated":
			tokenChecks++
			if r.Header.Get("Authorization") != "Bearer replacement-token" {
				w.WriteHeader(http.StatusUnauthorized)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	db := newSetupStateTestDB(t)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, server.URL+"/api")
	if err := RecoverRuntimeToken(db, "https://public.kitsu.example.test", "temp@example.com", "replacement-token"); err != nil {
		t.Fatalf("token recovery through override failed: %v", err)
	}
	if tokenChecks != 1 || StoredRuntimeKitsuToken(db) != "replacement-token" {
		t.Fatalf("override token checks=%d stored=%q", tokenChecks, StoredRuntimeKitsuToken(db))
	}
}

func TestRecoverRuntimeTokenRejectsAuthRedirects(t *testing.T) {
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
			var captures int
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/status":
					_, _ = w.Write([]byte(validZouStatus))
				case "/api/auth/authenticated":
					w.Header().Set("Location", server.URL+"/capture")
					w.WriteHeader(code)
				case "/capture":
					captures++
				}
			}))
			defer server.Close()
			if err := RecoverRuntimeToken(newSetupStateTestDB(t), server.URL, "temp@example.com", "not-returned"); err == nil {
				t.Fatal("expected redirect rejection")
			}
			if captures != 0 {
				t.Fatal("token was replayed to redirect target")
			}
		})
	}
}

func TestRecoverRuntimeTokenBlocksDNSChangeBeforeCredentialDelivery(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	var authCalls, lookups int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status":
			_, _ = w.Write([]byte(validZouStatus))
		case "/api/auth/authenticated":
			authCalls++
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	old := kitsuLookupNetIP
	kitsuLookupNetIP = func(context.Context, string) ([]netip.Addr, error) {
		lookups++
		if lookups == 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.2")}, nil
	}
	t.Cleanup(func() { kitsuLookupNetIP = old })

	db := newSetupStateTestDB(t)
	model.SetSetting(db, KitsuAPIBaseURLSettingKey, "http://kitsu.test:"+u.Port()+"/api")
	if err := RecoverRuntimeToken(db, "https://public.kitsu.example.test", "temp@example.com", "not-returned"); err == nil {
		t.Fatal("expected DNS target change rejection")
	}
	if authCalls != 0 {
		t.Fatalf("token reached auth endpoint %d times", authCalls)
	}
}

func TestRecoverRuntimeTokenRejectsNonZouStatusWithoutExposingToken(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	const token = "not-returned-token"
	var authCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/authenticated" {
			authCalls++
		}
		_, _ = w.Write([]byte(`{"name":"not-zou"}`))
	}))
	defer server.Close()

	err := RecoverRuntimeToken(newSetupStateTestDB(t), server.URL, "temp@example.com", token)
	if err == nil {
		t.Fatal("expected non-Zou endpoint rejection")
	}
	if strings.Contains(err.Error(), token) || authCalls != 0 {
		t.Fatalf("error/token delivery unsafe: err=%q auth=%d", err, authCalls)
	}
}
