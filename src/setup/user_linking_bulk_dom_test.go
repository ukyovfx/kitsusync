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

	if !strings.Contains(body, `<th>Action</th><th>Status</th>`) {
		t.Fatal("expected Action header before Status header")
	}
	actionsStart := strings.Index(body, `data-label="Actions"><div class="user-link-actions">`)
	statusStart := strings.Index(body, `data-label="Status">`)
	if actionsStart < 0 || statusStart < 0 || statusStart <= actionsStart {
		t.Fatalf("expected Actions cell before Status cell, got actions=%d status=%d", actionsStart, statusStart)
	}
	actionCell := body[actionsStart:statusStart]
	if strings.Count(actionCell, "<select") != 1 || strings.Contains(actionCell, `name="unlink_user_id"`) {
		t.Fatalf("expected action cell to contain exactly one select and no unlink button, got %s", actionCell)
	}
	statusEnd := strings.Index(body[statusStart:], `</td>`)
	if statusEnd < 0 {
		t.Fatal("missing closing td for Status cell")
	}
	statusCell := body[statusStart : statusStart+statusEnd]
	if !strings.Contains(statusCell, `class="user-link-status-inline"`) || !strings.Contains(statusCell, `name="unlink_user_id"`) {
		t.Fatalf("expected unlink button inside status cell alongside status badge, got %s", statusCell)
	}
}
