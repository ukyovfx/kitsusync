package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKitsuJSONSanitizesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"password must not appear in UI"}`))
	}))
	defer server.Close()

	err := kitsuJSON("session-token", http.MethodPost, server.URL+"/api/data/persons", map[string]string{"password": "secret"}, nil)
	if err == nil {
		t.Fatal("expected API error")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password must not appear") {
		t.Fatalf("API error leaked response data: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "validation rejected") {
		t.Fatalf("unexpected sanitized API error: %v", err)
	}
}

func TestCreateKitsuBotAccountDoesNotChangeExistingBotPassword(t *testing.T) {
	var changePasswordCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/data/persons":
			_, _ = w.Write([]byte(`[{"id":"bot-id","email":"kitsusync-bot@google.com","full_name":"KitsuSync Bot","is_bot":true}]`))
		case strings.Contains(r.URL.Path, "/change-password"):
			changePasswordCalled = true
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	_, _, err := CreateKitsuBotAccountWithToken(server.URL+"/", "admin-session")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected safe existing-bot error, got %v", err)
	}
	if changePasswordCalled {
		t.Fatal("must not call the bot-incompatible change-password endpoint")
	}
}

func TestReuseRuntimeBotAccountRequiresMatchingBot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/data/persons" {
			_, _ = w.Write([]byte(`[{"id":"person-id","email":"kitsusync-bot@google.com","is_bot":true}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	email, password, err := ReuseRuntimeBotAccountWithToken(server.URL+"/", "admin-session", "kitsusync-bot@google.com", "stored-password")
	if err != nil || email != "kitsusync-bot@google.com" || password != "stored-password" {
		t.Fatalf("expected stored bot credentials to be reused, email=%q password_present=%t err=%v", email, password != "", err)
	}
}
