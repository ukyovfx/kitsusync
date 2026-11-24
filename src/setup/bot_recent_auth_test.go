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

// Opening the settings page and ordinary edits must not ask for re-login.
func TestConnectionsViewAndOrdinaryEditsDoNotRequireRecentAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		form   url.Values
	}{
		{name: "view connections edit", method: http.MethodGet, path: "/bot/admin/bot?edit=1&lang=en"},
		{name: "view legacy connections edit", method: http.MethodGet, path: "/admin/bot?edit=1&legacy=1&lang=en"},
		{name: "save Kitsu host without token", method: http.MethodPost, path: "/bot/admin/bot", form: url.Values{"action": {"save_kitsu"}, "kitsu_hostname": {"kitsu.invalid"}}},
		{name: "save Discord guild without token", method: http.MethodPost, path: "/bot/admin/bot", form: url.Values{"action": {"save_discord"}, "guild_id": {"999999999999999999"}}},
		{name: "save external Kitsu URL", method: http.MethodPost, path: "/bot/admin/bot", form: url.Values{"action": {"save_external_kitsu_url"}, "kitsu_external_url": {""}}},
		{name: "legacy edit without password", method: http.MethodPost, path: "/admin/bot?legacy=1", form: url.Values{"guild_id": {"999999999999999999"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetSessions()
			t.Cleanup(resetSessions)
			token := newSessionToken("manager@example.com", "", "manager", "/bot/admin")
			var req *http.Request
			if tc.form == nil {
				req = httptest.NewRequest(tc.method, tc.path, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			rr := httptest.NewRecorder()

			BotHandler(newSetupStateTestDB(t), nil)(rr, req)

			if location := rr.Header().Get("Location"); strings.Contains(location, "/bot/login") {
				t.Fatalf("non-credential request demanded re-authentication: status=%d location=%q", rr.Code, location)
			}
		})
	}
}

// Changing or removing a credential remains a sensitive operation.
func TestCredentialChangesRequireRecentAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		form url.Values
	}{
		{name: "canonical Kitsu token", path: "/bot/admin/bot", form: url.Values{"action": {"save_kitsu"}, "kitsu_bot_token": {"new-token"}}},
		{name: "canonical Discord token", path: "/bot/admin/bot", form: url.Values{"action": {"save_discord"}, "bot_token": {"new-token"}}},
		{name: "root alias Discord token", path: "/admin/bot", form: url.Values{"action": {"save_discord"}, "bot_token": {"new-token"}}},
		{name: "legacy Kitsu password", path: "/admin/bot?legacy=1", form: url.Values{"kitsu_runtime_password": {"new-password"}}},
		{name: "legacy Discord token", path: "/admin/bot?legacy=1", form: url.Values{"bot_token": {"new-token"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetSessions()
			t.Cleanup(resetSessions)
			token := newSessionToken("manager@example.com", "", "manager", "/bot/admin")
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.form.Encode()))
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

func TestCredentialChangeAllowsValidRecentAuthentication(t *testing.T) {
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

func TestCredentialChangeRejectsExpiredRecentAuthentication(t *testing.T) {
	resetSessions()
	t.Cleanup(resetSessions)
	token := newSessionToken("manager@example.com", "", "manager", "/bot/admin/bot?edit=1")
	sessionMu.Lock()
	session := sessions[token]
	session.BotEditUntil = time.Now().Add(-time.Second)
	sessions[token] = session
	sessionMu.Unlock()

	form := url.Values{"action": {"save_discord"}, "bot_token": {"new-token"}}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?edit=1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	BotHandler(newSetupStateTestDB(t), nil)(rr, req)

	if rr.Code != http.StatusSeeOther || !strings.Contains(rr.Header().Get("Location"), "/bot/login") {
		t.Fatalf("expired recent authentication was not rejected: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
}

func TestNormalLoginGrantsRecentAuthenticationForConnectionsEdit(t *testing.T) {
	resetSessions()
	t.Cleanup(resetSessions)
	kitsu := newLoginPersistenceKitsuServer(t)
	defer kitsu.Close()

	login := httptest.NewRecorder()
	LoginHandler(kitsu.URL)(login, loginRequest(url.Values{"email": {"manager@example.com"}, "password": {"not-returned"}}))
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) == 0 {
		t.Fatalf("normal authorized login failed: status=%d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	sessionMu.Lock()
	createdSession, exists := sessions[cookie.Value]
	sessionMu.Unlock()
	if !validSession(cookie.Value) || !exists || !time.Now().Before(createdSession.BotEditUntil) || time.Until(createdSession.BotEditUntil) > 10*time.Minute {
		t.Fatal("newly authenticated admin session did not receive the 10-minute recent-auth window")
	}

	db := newSetupStateTestDB(t)
	edit := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=en", nil)
	edit.AddCookie(cookie)
	response := httptest.NewRecorder()
	BotHandler(db, nil)(response, edit)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Connections") && !strings.Contains(response.Body.String(), "Kitsu") {
		t.Fatalf("normal login still demands a second step-up for Connections edit: status=%d", response.Code)
	}

	// Even after the recent-auth window expires, viewing and ordinary edits stay open.
	sessionMu.Lock()
	session := sessions[cookie.Value]
	session.BotEditUntil = time.Now().Add(-time.Second)
	sessions[cookie.Value] = session
	sessionMu.Unlock()
	response = httptest.NewRecorder()
	BotHandler(db, nil)(response, edit)
	if response.Code != http.StatusOK {
		t.Fatalf("expired recent-auth blocked the Connections view: status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
}
