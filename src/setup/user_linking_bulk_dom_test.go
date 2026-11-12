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
	if !strings.Contains(body, "window.confirm(unlink.dataset.confirm)") || !strings.Contains(body, "field.name='unlink_user_id'") {
		t.Fatal("unlink submits without asking for confirmation")
	}
	if !strings.Contains(body, `name="unlink_user_id"`) {
		t.Fatal("unlink button lost its identity for the no-JavaScript path")
	}
}
