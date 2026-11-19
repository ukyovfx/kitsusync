package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/model"

	"gorm.io/gorm"
)

func commentNotifyHandler(db *gorm.DB, confDefault bool) http.HandlerFunc {
	return ForumSettingsHandler(db, confDefault, func() (string, string) { return "bot-token", "g-fallback" })
}

func commentNotifyGet(t *testing.T, db *gorm.DB, confDefault bool) string {
	t.Helper()
	w := httptest.NewRecorder()
	commentNotifyHandler(db, confDefault)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/forum?project=p1&lang=en", nil))
	return w.Body.String()
}

// The checkbox page shows the stored value, and conf.toml only seeds it
// while nothing is stored.
func TestForumCommentNotifyCheckboxShowsTheStoredValue(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	checked := `name="comment_notify_assignee" value="on" checked>`
	unchecked := `name="comment_notify_assignee" value="on">`
	if body := commentNotifyGet(t, db, true); !strings.Contains(body, checked) {
		t.Fatal("nothing stored must show the conf.toml default true")
	}
	if body := commentNotifyGet(t, db, false); !strings.Contains(body, unchecked) || strings.Contains(body, checked) {
		t.Fatal("nothing stored must not show checked with the default false")
	}
	model.SetSetting(db, CommentNotifyAssigneeSettingKey, "false")
	if body := commentNotifyGet(t, db, true); strings.Contains(body, checked) || !strings.Contains(body, unchecked) {
		t.Fatal("a stored false must win over the conf.toml default")
	}
	model.SetSetting(db, CommentNotifyAssigneeSettingKey, "true")
	if body := commentNotifyGet(t, db, false); !strings.Contains(body, checked) {
		t.Fatal("a stored true must win over the conf.toml default")
	}
}

// A save round-trips: checked stores true, unchecked stores an explicit false.
func TestForumCommentNotifySaveRoundTrip(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	on := completeForumForm()
	on.Set("comment_notify_assignee", "on")
	if w := forumPost(t, db, on); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", w.Code)
	}
	if v := model.GetSetting(db, CommentNotifyAssigneeSettingKey); v != "true" {
		t.Fatalf("stored = %q, want true", v)
	}
	if w := forumPost(t, db, completeForumForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", w.Code)
	}
	if v := model.GetSetting(db, CommentNotifyAssigneeSettingKey); v != "false" {
		t.Fatalf("stored = %q, want an explicit false", v)
	}
}
