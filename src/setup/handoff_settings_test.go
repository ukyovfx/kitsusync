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

// The wiring editors (2026-11-23, placement corrected 2026-11-24) wire
// task types to task types. The studio-common table lives on the
// connection settings page; the Production page carries only its
// overrides and the no-assignee choice. The product matrix, presets,
// timing select and product registration inputs are gone.

// studioRoutesForm is one submit of the studio wiring form on the
// connection settings page.
func studioRoutesForm() url.Values {
	form := url.Values{}
	form["st_notify_tt-lay"] = []string{"tt-anim", "tt-light"}
	form["st_none_tt-comp"] = []string{"1"}
	return form
}

func saveStudioRoutesRequest(t *testing.T, db *gorm.DB, form url.Values) error {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/bot/admin/bot?edit=1&lang=en", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return saveStudioRoutes(db, r)
}

func studioRoutesSection(t *testing.T, db *gorm.DB) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/bot?edit=1&lang=en", nil)
	return renderStudioRoutesSection(db, "en", r)
}

// productionForm is one submit of the Production handoff page: overrides
// and the no-assignee choice, no studio fields.
func productionForm() url.Values {
	return url.Values{"project": {"p1"}, "no_assignee": {"admin"}}
}

func TestHandoffPageShowsRoutesNotMatrix(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	w := handoffGet(t, db)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Overrides for this Production", `name="ov_mode_tt-lay"`, `name="no_assignee"`, `/bot/admin/bot?edit=1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
	// The studio editor moved to the connection settings page: its inputs
	// no longer render here.
	for _, gone := range []string{`name="st_notify_tt-lay"`, `name="st_none_tt-comp"`, `name="row_task"`, `name="product_name"`, `name="notify_timing"`, `data-handoff-cell`} {
		if strings.Contains(body, gone) {
			t.Fatalf("page still carries the abolished control %q", gone)
		}
	}
	// Nothing stored by a bare render.
	if _, ok, err := model.LoadProductionHandoffSettings(db, "p1"); err != nil || ok {
		t.Fatalf("an unconfigured Production must have no handoff settings: ok=%v err=%v", ok, err)
	}
}

func TestStudioRoutesSectionRendersOnConnectionSettings(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	body := studioRoutesSection(t, db)
	for _, want := range []string{"Handoff wiring (studio default)", `name="action" value="save_studio_routes"`, `name="st_notify_tt-lay"`, `name="st_none_tt-comp"`, `class="route-row"`, `data-route-row="tt-lay"`, "Add a source", `data-route-source-select`, `data-route-source-add`} {
		if !strings.Contains(body, want) {
			t.Fatalf("studio section is missing %q", want)
		}
	}
	// A Task Type is never offered as its own target.
	if strings.Contains(body, `name="st_notify_tt-lay" value="tt-lay"`) {
		t.Fatal("the studio editor offers Layout as its own target")
	}
	if strings.Contains(body, `name="st_notify_tt-comp" value="tt-comp"`) {
		t.Fatal("the studio editor offers Compositing as its own target")
	}
}

func TestHandoffStudioRouteSaveStoresSelection(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if err := saveStudioRoutesRequest(t, db, studioRoutesForm()); err != nil {
		t.Fatalf("save studio routes: %v", err)
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
	// The reloaded section shows the saved wiring checked.
	body := studioRoutesSection(t, db)
	if !strings.Contains(body, `name="st_notify_tt-lay" value="tt-anim" checked`) {
		t.Fatal("saved wiring did not reload as checked")
	}
	if !strings.Contains(body, `name="st_none_tt-comp" value="1" checked`) {
		t.Fatal("saved no-handoff mark did not reload as checked")
	}
	// A submit of the Production page must not touch the studio routes.
	if w := handoffPost(t, db, productionForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("production save status = %d, body: %s", w.Code, w.Body.String())
	}
	routes, err = model.LoadStudioTaskRoutes(db)
	if err != nil || len(routes) != 2 {
		t.Fatalf("studio routes after a production save = %d rows, err=%v", len(routes), err)
	}
}

func TestHandoffProductionOverrideModes(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	// Studio seed: Layout -> Animation, Lighting (edited on the connection
	// settings page now, so seed it through the model).
	if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{FromTaskTypeID: "tt-lay", FromTaskTypeName: "Layout", NotifyTaskTypeIDs: "tt-anim,tt-light"}); err != nil {
		t.Fatal(err)
	}
	// none: this Production opts Layout out entirely.
	form := productionForm()
	form["ov_mode_tt-lay"] = []string{"none"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	route, _, found, err := model.EffectiveTaskRoute(db, "p1", "tt-lay")
	if err != nil || !found || len(route) != 0 {
		t.Fatalf("effective route under none override = %v found=%v err=%v", route, found, err)
	}
	// custom: only Compositing receives from Layout here.
	form = productionForm()
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
	form = productionForm()
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
	form := productionForm()
	form["ov_mode_tt-lay"] = []string{"custom"}
	w := handoffPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "受け渡し先が選ばれていません") {
		t.Fatalf("rejected save does not explain the empty override: %s", w.Body.String())
	}
	// An unknown receiving Task Type in the studio form is a save error too.
	studioForm := studioRoutesForm()
	studioForm["st_notify_tt-lay"] = []string{"tt-unknown"}
	err := saveStudioRoutesRequest(t, db, studioForm)
	if err == nil || !strings.Contains(err.Error(), "がこの製作にありません") {
		t.Fatalf("rejected studio save error = %v", err)
	}
	if routes, _ := model.LoadStudioTaskRoutes(db); len(routes) != 0 {
		t.Fatal("a rejected save must not store routes")
	}
}

func TestHandoffNoAssigneeChoiceIsStored(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	form := productionForm()
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
	form = productionForm()
	form["no_assignee"] = []string{"users"}
	if w := handoffPost(t, db, form); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

// A stored self-route (Layout -> Layout, possible from the older editor)
// is not offered as a checkbox anymore, but a save of the page carries it
// through untouched instead of silently dropping it.
func TestHandoffSelfRouteIsPreservedButNotOffered(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{FromTaskTypeID: "tt-lay", FromTaskTypeName: "Layout", NotifyTaskTypeIDs: "tt-lay,tt-anim"}); err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionTaskRoute(db, model.ProductionTaskRoute{ProductionID: "p1", FromTaskTypeID: "tt-lay", FromTaskTypeName: "Layout", NotifyTaskTypeIDs: "tt-lay,tt-comp"}); err != nil {
		t.Fatal(err)
	}
	section := studioRoutesSection(t, db)
	if strings.Contains(section, `<input type="checkbox" name="st_notify_tt-lay" value="tt-lay"`) {
		t.Fatal("the studio editor offers Layout as its own target")
	}
	if !strings.Contains(section, `<input type="hidden" name="st_notify_tt-lay" value="tt-lay">`) {
		t.Fatal("the stored studio self-route is not carried through the form")
	}
	body := handoffGet(t, db).Body.String()
	if strings.Contains(body, `<input type="checkbox" name="ov_notify_tt-lay" value="tt-lay"`) {
		t.Fatal("the override editor offers Layout as its own target")
	}
	if !strings.Contains(body, `<input type="hidden" name="ov_notify_tt-lay" value="tt-lay">`) {
		t.Fatal("the stored override self-route is not carried through the form")
	}
	// Saving what the form submits keeps the self-routes.
	form := url.Values{}
	form["st_notify_tt-lay"] = []string{"tt-lay", "tt-anim"}
	if err := saveStudioRoutesRequest(t, db, form); err != nil {
		t.Fatalf("save studio routes: %v", err)
	}
	routes, err := model.LoadStudioTaskRoutes(db)
	if err != nil || len(routes) != 1 || routes[0].NotifyTaskTypeIDs != "tt-lay,tt-anim" {
		t.Fatalf("studio routes after save = %+v err=%v", routes, err)
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
