package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openAdminAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Setting{},
		&model.AuditLog{},
		&model.Project{},
		&model.UserMap{},
		&model.StudioStatusPolicy{},
		&model.StudioTaskRoute{},
		&model.AdminSession{},
	); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestWriteAdminMutationAuditUsesSessionEmailOrUnknown(t *testing.T) {
	db := openAdminAuditTestDB(t)
	resetSessions()
	t.Cleanup(resetSessions)

	token := newSessionToken("admin@example.com", "", "manager", "/bot/admin")
	authedReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", nil)
	authedReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	writeAdminMutationAudit(db, authedReq, "save_kitsu", "", "", "", "https://kitsu.example", true, "")
	unauthedReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", nil)
	writeAdminMutationAudit(db, unauthedReq, "save_discord", "", "", "", "discord_bot_token", true, "")

	logs := model.ListAuditLogs(db, 10)
	if len(logs) != 2 {
		t.Fatalf("expected 2 audit logs, got %d", len(logs))
	}
	var foundAuthed, foundUnauthed bool
	for _, entry := range logs {
		switch entry.NewStatus {
		case "save_kitsu":
			foundAuthed = true
			if entry.ActorKind != model.AuditActorHuman || entry.ActorName != "admin@example.com" {
				t.Fatalf("authed audit actor = (%q, %q), want (human, admin@example.com)", entry.ActorKind, entry.ActorName)
			}
		case "save_discord":
			foundUnauthed = true
			if entry.ActorKind != model.AuditActorUnknown || entry.ActorName != "" {
				t.Fatalf("unauthed audit actor = (%q, %q), want (unknown, \"\")", entry.ActorKind, entry.ActorName)
			}
		}
	}
	if !foundAuthed || !foundUnauthed {
		t.Fatalf("missing expected audit entries: authed=%v unauthed=%v", foundAuthed, foundUnauthed)
	}
}

func TestAdminMutationsEmitAuditRowsWithSessionEmail(t *testing.T) {
	db := openAdminAuditTestDB(t)
	resetSessions()
	t.Cleanup(resetSessions)

	token := newSessionToken("lead@example.com", "", "manager", "/bot/admin")

	if err := db.Create(&model.UserMap{KitsuID: "person-1", KitsuName: "Taro", KitsuEmail: "taro@example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	var user model.UserMap
	if err := db.First(&user).Error; err != nil {
		t.Fatal(err)
	}

	usersHandler := UsersHandler(db, "https://kitsu.example")
	form := url.Values{
		"action":     {"remove_global_link"},
		"user_id":    {"1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	usersHandler.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("UsersHandler status = %d, want 303", rr.Code)
	}

	botHandler := BotHandlerWithRuntime(db, nil, nil)
	statusForm := url.Values{"action": {"save_status_roles"}}
	statusReq := httptest.NewRequest(http.MethodPost, "/bot/admin/bot", strings.NewReader(statusForm.Encode()))
	statusReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	statusReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	statusRR := httptest.NewRecorder()
	botHandler.ServeHTTP(statusRR, statusReq)
	if statusRR.Code != http.StatusSeeOther {
		t.Fatalf("save_status_roles status = %d, want 303", statusRR.Code)
	}

	logs := model.ListAuditLogs(db, 10)
	if len(logs) != 2 {
		t.Fatalf("expected 2 audit rows, got %d", len(logs))
	}
	for _, entry := range logs {
		if entry.ActorKind != model.AuditActorHuman || entry.ActorName != "lead@example.com" {
			t.Fatalf("audit row %q has actor (%q, %q), want (human, lead@example.com)", entry.NewStatus, entry.ActorKind, entry.ActorName)
		}
	}
}
