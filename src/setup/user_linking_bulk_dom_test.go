package setup

import (
	"strings"
	"testing"

	"app/src/model"
)

// With existing mappings the row action column contains Unlink buttons, so the
// save button must be addressed explicitly instead of "the first submit".
func TestUserLinkingBulkFormTargetsSaveButtonAndConfirmsUnlink(t *testing.T) {
	db := newIAViewDB(t)
	if err := db.Create(&model.UserMap{KitsuID: "person-1", KitsuName: "Kitsu Person", DiscordID: "123456789012345678"}).Error; err != nil {
		t.Fatal(err)
	}
	body := renderUserLinkingIdentity(db, "en", userLinkingIdentityDirectory(globalDiscordUserOption{ID: "123456789012345678", Username: "global_user", Name: "Studio Nick"}))

	if !strings.Contains(body, `id="user-linking-save"`) || !strings.Contains(body, "getElementById('user-linking-save')") {
		t.Fatal("save button is not addressed by id, so an Unlink button would be picked up instead")
	}
	if strings.Contains(body, "querySelector('button[type=submit]')") {
		t.Fatal("bulk form still grabs the first submit button, which is Unlink when a mapping exists")
	}
	// The unlink path must ask for confirmation before submitting.
	if strings.Contains(body, "window.confirm") || !strings.Contains(body, "window.kitsusyncConfirm(unlink.dataset.confirm") || !strings.Contains(body, "field.name='unlink_user_id'") {
		t.Fatal("unlink submits without asking for confirmation")
	}
	if !strings.Contains(body, `name="unlink_user_id"`) {
		t.Fatal("unlink button lost its identity for the no-JavaScript path")
	}

	statusStart := strings.Index(body, `data-label="Status">`)
	actionsStart := strings.Index(body, `<div class="user-link-actions">`)
	if statusStart < 0 || actionsStart < 0 || actionsStart <= statusStart {
		t.Fatalf("expected Status cell before user-link-actions, got status=%d actions=%d", statusStart, actionsStart)
	}
	statusCell := body[statusStart:actionsStart]
	if !strings.Contains(statusCell, `class="user-link-status-inline"`) || !strings.Contains(statusCell, `name="unlink_user_id"`) {
		t.Fatalf("expected unlink button inside status cell alongside status badge, got %s", statusCell)
	}
	actionEnd := strings.Index(body[actionsStart:], `</div>`)
	if actionEnd < 0 {
		t.Fatal("missing closing div for user-link-actions")
	}
	actionCell := body[actionsStart : actionsStart+actionEnd]
	if strings.Count(actionCell, "<select") != 1 || strings.Contains(actionCell, `name="unlink_user_id"`) {
		t.Fatalf("expected action cell to contain exactly one select and no unlink button, got %s", actionCell)
	}
}
