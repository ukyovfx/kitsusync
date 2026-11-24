package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"app/src/model"
)

// The status -> role section lives on the Bot settings page now (redesign
// front half): it renders the saved mapping for a Production, and a save
// through it stores the roles on the Production's forum settings, where the
// forum page's own save must then preserve them.

func TestStatusRolesSectionPreFillsTemplate(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "f-comp"}},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleWFA, TagID: "t1"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleRetake, TagID: "t2"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleDone, TagID: "t3"},
		},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
			{StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=en", nil)
	body := renderStatusRolesSection(db, "en", r)
	if !strings.Contains(body, "Kitsu status → role") {
		t.Fatalf("section missing: %s", body)
	}
	if !strings.Contains(body, `name="srole_project" value="p1"`) {
		t.Fatalf("section does not carry the production: %s", body)
	}
}

func TestSaveStatusRolesStoresAndForumSavePreserves(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "f-comp"}},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleWFA, TagID: "t1"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleRetake, TagID: "t2"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleDone, TagID: "t3"},
		},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
			{StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	form := url.Values{
		"srole_project": {"p1", "p1", "p1", "p1"},
		"srole_id":      {"s-wfa", "s-retake", "s-done", "s-wip"},
		"srole_name":    {"Waiting For Approval", "Retake", "Done", "WIP"},
		"srole_role":    {"wfa", "retake", "done", "wip"},
	}
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?edit=1", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := saveStatusRoles(db, r); err != nil {
		t.Fatalf("saveStatusRoles: %v", err)
	}
	loaded, ok, err := model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok || len(loaded.StatusRoles) != 4 {
		t.Fatalf("stored roles = %+v ok=%v err=%v", loaded.StatusRoles, ok, err)
	}
	// A forum page save afterwards must not wipe the roles edited here.
	w := forumPost(t, db, completeForumForm())
	if w.Code != http.StatusSeeOther {
		t.Fatalf("forum save status = %d, body: %s", w.Code, w.Body.String())
	}
	loaded, ok, err = model.LoadProductionForumSettings(db, "p1")
	if err != nil || !ok || len(loaded.StatusRoles) != 4 {
		t.Fatalf("roles after forum save = %+v ok=%v err=%v", loaded.StatusRoles, ok, err)
	}
}

func TestStatusRolesDisplayOmitsProductionHeadingAndSummaryLine(t *testing.T) {
	stubForumLookups(t, nil)
	db := newForumPageDB(t)
	settings := model.ProductionForumSettings{
		Config: model.ProductionForumConfig{ProductionID: "p1", GuildID: "g1"},
		Routes: []model.ProductionForumRoute{{ProductionID: "p1", TaskTypeID: "tt-comp", TaskTypeName: "Compositing", ForumChannelID: "f-comp"}},
		Tags: []model.ProductionForumTag{
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleWFA, TagID: "t1"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleRetake, TagID: "t2"},
			{ProductionID: "p1", ForumChannelID: "f-comp", Role: model.ForumRoleDone, TagID: "t3"},
		},
		StatusRoles: []model.ProductionForumStatusRole{
			{StatusID: "s-wfa", StatusName: "Waiting For Approval", Role: model.ForumRoleWFA},
			{StatusID: "s-retake", StatusName: "Retake", Role: model.ForumRoleRetake},
			{StatusID: "s-done", StatusName: "Done", Role: model.ForumRoleDone},
		},
	}
	if err := model.SaveProductionForumSettings(db, settings); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?lang=en", nil)
	body := renderStatusRolesDisplay(db, "en", r)
	for _, want := range []string{"Kitsu status → role", `class="role-boxes"`, `class="role-chip role-chip-static"`, "Waiting For Approval"} {
		if !strings.Contains(body, want) {
			t.Fatalf("display section missing %q: %s", want, body)
		}
	}
	for _, gone := range []string{"<h3>", "role-summary-line", "all three roles are assigned", "Alpha"} {
		if strings.Contains(body, gone) {
			t.Fatalf("display section must omit %q: %s", gone, body)
		}
	}
}
