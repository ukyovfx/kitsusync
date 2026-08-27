package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBotRootRedirectsUnauthenticatedToLogin(t *testing.T) {
	resetSessions()
	r := httptest.NewRequest(http.MethodGet, "/bot/?lang=en", nil)
	rr := httptest.NewRecorder()
	BotRootHandler(func() bool { return false })(rr, r)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusSeeOther)
	}
	location := rr.Header().Get("Location")
	if !strings.HasPrefix(location, "/bot/login?") || !strings.Contains(location, "lang=en") || !strings.Contains(location, "next=%2Fbot%2F%3Flang%3Den") {
		t.Fatalf("location = %q, want login redirect preserving language and next", location)
	}
}

func TestBotRootRedirectsAuthenticatedSetupRequiredToSetup(t *testing.T) {
	resetSessions()
	token := newSessionToken("manager@example.com", "jwt-token", "manager", "/bot/")
	r := httptest.NewRequest(http.MethodGet, "/bot/?lang=ja", nil)
	r.AddCookie(sessionCookie(r, token, 900))
	rr := httptest.NewRecorder()
	BotRootHandler(func() bool { return false })(rr, r)

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/bot/setup?lang=ja" {
		t.Fatalf("status/location = %d/%q, want setup redirect", rr.Code, rr.Header().Get("Location"))
	}
}

func TestBotRootRedirectsAuthenticatedReadyToAdmin(t *testing.T) {
	resetSessions()
	token := newSessionToken("manager@example.com", "jwt-token", "manager", "/bot/")
	r := httptest.NewRequest(http.MethodGet, "/bot/?lang=en", nil)
	r.AddCookie(sessionCookie(r, token, 900))
	rr := httptest.NewRecorder()
	BotRootHandler(func() bool { return true })(rr, r)

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/bot/admin?lang=en" {
		t.Fatalf("status/location = %d/%q, want admin redirect", rr.Code, rr.Header().Get("Location"))
	}
}

func TestBotRootDoesNotRedirectItsDestinations(t *testing.T) {
	resetSessions()
	for _, path := range []string{"/bot/setup", "/bot/admin"} {
		r := httptest.NewRequest(http.MethodGet, path+"?lang=ja", nil)
		rr := httptest.NewRecorder()
		BotRootHandler(func() bool { return false })(rr, r)
		if rr.Code != http.StatusNotFound {
			t.Errorf("path %q status = %d, want %d", path, rr.Code, http.StatusNotFound)
		}
	}
}
