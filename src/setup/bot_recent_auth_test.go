package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSensitiveBotMutationsRequireRecentAuthenticationWithoutEditQuery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		action string
	}{
		{name: "canonical Kitsu save", path: "/bot/admin/bot", action: "save_kitsu"},
		{name: "canonical Discord save", path: "/bot/admin/bot", action: "save_discord"},
		{name: "root alias Kitsu save", path: "/admin/bot", action: "save_kitsu"},
		{name: "root alias Discord save", path: "/admin/bot", action: "save_discord"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetSessions()
			t.Cleanup(resetSessions)
			token := newSessionToken("manager@example.com", "", "manager", "/bot/admin")
			form := url.Values{"action": {tc.action}}
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			rr := httptest.NewRecorder()

			BotHandler(newSetupStateTestDB(t), nil)(rr, req)

			if rr.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want recent-auth redirect", rr.Code)
			}
			location := rr.Header().Get("Location")
			if !strings.Contains(location, "/bot/login") || !strings.Contains(location, "next=") {
				t.Fatalf("unexpected recent-auth redirect: %q", location)
			}
		})
	}
}

func TestSensitiveBotMutationAllowsValidRecentAuthentication(t *testing.T) {
	t.Setenv(RuntimeSecretKeyFileEnv, filepath.Join(t.TempDir(), "runtime-secret.key"))
	db := newSetupStateTestDB(t)
	oldValidator := validateDiscordBotTokenForSave
	validateDiscordBotTokenForSave = func(string) error { return nil }
	defer func() { validateDiscordBotTokenForSave = oldValidator }()

	form := url.Values{"action": {"save_discord"}, "bot_token": {"new-token-for-test"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecentBotEditSession(t, req)
	rr := httptest.NewRecorder()

	BotHandler(db, nil)(rr, req)

	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "discord_saved") {
		t.Fatalf("valid recent authentication did not reach the save: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
}

func TestSensitiveBotMutationRejectsExpiredRecentAuthentication(t *testing.T) {
	resetSessions()
	t.Cleanup(resetSessions)
	token := newSessionToken("manager@example.com", "", "manager", "/bot/admin/bot?edit=1")
	sessionMu.Lock()
	session := sessions[token]
	session.BotEditUntil = time.Now().Add(-time.Second)
	sessions[token] = session
	sessionMu.Unlock()

	form := url.Values{"action": {"save_discord"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?edit=1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	BotHandler(newSetupStateTestDB(t), nil)(rr, req)

	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/bot/login") {
		t.Fatalf("expired recent authentication was not rejected: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
}

func TestLegacyBotMutationRequiresRecentAuthentication(t *testing.T) {
	resetSessions()
	t.Cleanup(resetSessions)
	token := newSessionToken("manager@example.com", "", "manager", "/bot/admin")
	form := url.Values{"guild_id": {"999999999999999999"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/bot?legacy=1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	BotHandler(newSetupStateTestDB(t), nil)(rr, req)

	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/bot/login") {
		t.Fatalf("legacy mutation bypassed recent authentication: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
}
