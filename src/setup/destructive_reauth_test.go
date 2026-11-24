package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

func TestDestructiveProjectActionsRedirectWithoutRecentAuth(t *testing.T) {
	actions := []struct {
		action      string
		confirmText string
	}{
		{action: "execute_current_ia_discord_delete", confirmText: "DELETE"},
		{action: "execute_validated_channel_delete", confirmText: "delete"},
		{action: "complete_connection_cleanup", confirmText: "delete"},
		{action: "remove_connection", confirmText: "delete"},
	}

	// Non-recent session (valid session, BotEditUntil zero).
	staleToken, err := createSessionTokenChecked("admin@example.test", "kitsu-token", "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = destroySession(staleToken) })

	for _, tc := range actions {
		t.Run(tc.action, func(t *testing.T) {
			db := newIAViewDB(t)
			project := model.Project{KitsuProjectID: "reauth-test-p", Name: "Reauth Test"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}

			form := url.Values{
				"action":       {tc.action},
				"project_id":   {project.KitsuProjectID},
				"confirm_text": {tc.confirmText},
			}
			req := httptest.NewRequest(http.MethodPost, "/bot/admin/projects?lang=en", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: staleToken})
			rr := httptest.NewRecorder()

			AdminProjectsHandler(db, "", "")(rr, req)

			if rr.Code != http.StatusSeeOther {
				t.Fatalf("%s without recent auth: status = %d, want %d", tc.action, rr.Code, http.StatusSeeOther)
			}
			loc := rr.Header().Get("Location")
			if !strings.HasPrefix(loc, "/bot/login?") {
				t.Fatalf("%s without recent auth: Location = %q, want /bot/login?...", tc.action, loc)
			}
			if model.FindProjectByKitsuID(db, project.KitsuProjectID) == nil {
				t.Fatalf("%s deleted project without recent auth", tc.action)
			}
		})
	}
}
