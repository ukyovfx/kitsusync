package basicauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthForJWTTokenDetailedReportsSanitizedHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"password":"must not be exposed"}`))
	}))
	defer server.Close()

	token, diagnostics := AuthForJWTTokenDetailed(server.URL, "admin@example.com", "secret")
	if token != "" || diagnostics.StatusCode != http.StatusBadRequest || diagnostics.Category != "HTTP error" {
		t.Fatalf("unexpected diagnostics: token=%q status=%d category=%q", token, diagnostics.StatusCode, diagnostics.Category)
	}
}

func TestAuthForJWTTokenDetailedAcceptsAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"jwt"}`))
	}))
	defer server.Close()

	token, diagnostics := AuthForJWTTokenDetailed(server.URL, "admin@example.com", "secret")
	if token != "jwt" || diagnostics.StatusCode != http.StatusOK || diagnostics.Category != "success" {
		t.Fatalf("unexpected diagnostics: token=%q status=%d category=%q", token, diagnostics.StatusCode, diagnostics.Category)
	}
}
