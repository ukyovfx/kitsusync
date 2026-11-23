package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

// stubHandoffTaskTypes replaces the Kitsu Task Type lookup for one test.
func stubHandoffTaskTypes(t *testing.T, taskTypes []kitsu.TaskType) {
	t.Helper()
	old := handoffTaskTypeLister
	t.Cleanup(func() { handoffTaskTypeLister = old })
	handoffTaskTypeLister = func(*gorm.DB, string) []kitsu.TaskType { return taskTypes }
}

func handoffTaskTypes() []kitsu.TaskType {
	return []kitsu.TaskType{
		{ID: "tt-lay", Name: "Layout"},
		{ID: "tt-anim", Name: "Animation"},
		{ID: "tt-comp", Name: "Compositing"},
		{ID: "tt-light", Name: "Lighting"},
	}
}

func handoffHandler(db *gorm.DB) http.HandlerFunc { return HandoffSettingsHandler(db) }

func handoffGet(t *testing.T, db *gorm.DB) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handoffHandler(db)(w, httptest.NewRequest(http.MethodGet, "/bot/admin/handoff?project=p1&lang=en", nil))
	return w
}

func handoffPost(t *testing.T, db *gorm.DB, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/handoff?lang=en", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handoffHandler(db)(w, r)
	return w
}

// The redesigned page (2026-11-23) wires task types to task types: one
// studio-wide table, per-Production overrides, and the no-assignee choice.
// The product matrix, presets, timing select and product registration inputs
// are gone.

func studioForm() url.Values {
	form := url.Values{"project": {"p1"}, "no_assignee": {"admin"}}
	form["st_task"] = []string{"tt-lay", "tt-anim", "tt-comp", "tt-light"}
	form["st_name"] = []string{"Layout", "Animation", "Compositing", "Lighting"}
	form["st_notify_tt-lay"] = []string{"tt-anim", "tt-light"}
	form["st_none_tt-comp"] = []string{"1"}
	return form
}

func TestHandoffPageShowsRoutesNotMatrix(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	w := handoffGet(t, db)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Handoff wiring (studio default)", "Overrides for this Production", `name="st_notify_tt-lay"`, `name="ov_mode_tt-lay"`, `name="no_assignee"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
	for _, gone := range []string{`name="row_task"`, `name="product_name"`, `name="notify_timing"`, `data-handoff-cell`} {
		if strings.Contains(body, gone) {
			t.Fatalf("page still carries the abolished control %q", gone)
		}
	}
	// Nothing stored by a bare render.
	if _, ok, err := model.LoadProductionHandoffSettings(db, "p1"); err != nil || ok {
		t.Fatalf("an unconfigured Production must have no handoff settings: ok=%v err=%v", ok, err)
	}
}

func TestHandoffStudioRouteSaveStoresSelection(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if w := handoffPost(t, db, studioForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	routes, err := model.LoadStudioTaskRoutes(db)
	if err != nil {
		t.Fatal(err)
	}
	bySource := map[string]model.StudioTaskRoute{}
	for _, route := range routes {
		bySource[route.FromTaskTypeID] = route
	}
	if got := bySource["tt-lay"].NotifyTaskTypeIDs; got != "tt-anim,tt-light" {
		t.Fatalf("Layout wiring = %q", got)
	}
	if got := bySource["tt-comp"].NotifyTaskTypeIDs; got != model.TaskRouteNone {
		t.Fatalf("Compositing wiring = %q, want the no-handoff mark", got)
	}
	// The reloaded page shows the saved wiring checked.
	body := handoffGet(t, db).Body.String()
	if !strings.Contains(body, `name="st_notify_tt-lay" value="tt-anim" checked`) {
		t.Fatal("saved wiring did not reload as checked")
	}
	if !strings.Contains(body, `name="st_none_tt-comp" value="1" checked`) {
		t.Fatal("saved no-handoff mark did not reload as checked")
	}
}

func TestHandoffProductionOverrideModes(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if w := handoffPost(t, db, studioForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("seed save: %d", w.Code)
	}
	// none: this Production opts Layout out entirely.
	form := studioForm()
	form["ov_mode_tt-lay"] = []string{"none"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	route, _, found, err := model.EffectiveTaskRoute(db, "p1", "tt-lay")
	if err != nil || !found || len(route) != 0 {
		t.Fatalf("effective route under none override = %v found=%v err=%v", route, found, err)
	}
	// custom: only Compositing receives from Layout here.
	form = studioForm()
	form["ov_mode_tt-lay"] = []string{"custom"}
	form["ov_notify_tt-lay"] = []string{"tt-comp"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	route, _, found, err = model.EffectiveTaskRoute(db, "p1", "tt-lay")
	if err != nil || !found || len(route) != 1 || route[0] != "tt-comp" {
		t.Fatalf("effective route under custom override = %v found=%v err=%v", route, found, err)
	}
	// inherit: the override row goes away and the studio wiring applies again.
	form = studioForm()
	form["ov_mode_tt-lay"] = []string{"inherit"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	route, _, found, err = model.EffectiveTaskRoute(db, "p1", "tt-lay")
	if err != nil || !found || len(route) != 2 {
		t.Fatalf("effective route after inherit = %v found=%v err=%v", route, found, err)
	}
}

func TestHandoffSaveRejectsWrongInput(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	// A custom override without any receiving Task Type is a save error.
	form := studioForm()
	form["ov_mode_tt-lay"] = []string{"custom"}
	w := handoffPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "受け渡し先が選ばれていません") {
		t.Fatalf("rejected save does not explain the empty override: %s", w.Body.String())
	}
	// An unknown receiving Task Type is a save error too.
	form = studioForm()
	form["st_notify_tt-lay"] = []string{"tt-unknown"}
	w = handoffPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "がこの製作にありません") {
		t.Fatalf("rejected save does not explain the unknown Task Type: %s", w.Body.String())
	}
	if routes, _ := model.LoadStudioTaskRoutes(db); len(routes) != 0 {
		t.Fatal("a rejected save must not store routes")
	}
}

func TestHandoffNoAssigneeChoiceIsStored(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	form := studioForm()
	form["no_assignee"] = []string{"users"}
	form["fallback_user"] = []string{"user-9"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	settings, ok, err := model.LoadProductionHandoffSettings(db, "p1")
	if err != nil || !ok || settings.Config.NoAssignee != model.HandoffFallbackUsers || settings.Config.FallbackUserID != "user-9" {
		t.Fatalf("stored no-assignee choice = %+v ok=%v err=%v", settings.Config, ok, err)
	}
	// fallback_user without the user ID is refused.
	form = studioForm()
	form["no_assignee"] = []string{"users"}
	if w := handoffPost(t, db, form); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandoffSaveRefusesValidationOnlyProduction(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	project := model.FindProjectByKitsuID(db, "p1")
	project.ValidationOnly = true
	if err := db.Save(project).Error; err != nil {
		t.Fatal(err)
	}
	w := handoffPost(t, db, url.Values{"project": {"p1"}, "no_assignee": {"admin"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandoffNotificationsTabLinksTheHandoffPage(t *testing.T) {
	db := newForumPageDB(t)
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/projects?project=p1&lang=en", nil)
	// The renderer is passed the tab, so the case body is exercised directly.
	body := renderSelectedProductionPanel(db, r, model.Project{KitsuProjectID: "p1", Name: "Synthetic Production", DiscordGuildID: "g1"}, "en", "notifications", "", "", "")
	if !strings.Contains(body, "/bot/admin/handoff?") || !strings.Contains(body, "project=p1") {
		t.Fatalf("the notifications tab must link to the handoff mapping page: %s", body)
	}
}
