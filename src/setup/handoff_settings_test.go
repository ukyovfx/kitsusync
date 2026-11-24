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
	for _, want := range []string{"Overrides for this Production", `name="ov_mode_tt-lay"`, `name="no_assignee"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
	// The studio editor moved to the connection settings page: its inputs
	// and the studio note no longer render here.
	for _, gone := range []string{`name="st_notify_tt-lay"`, `name="st_none_tt-comp"`, `name="row_task"`, `name="product_name"`, `name="notify_timing"`, `data-handoff-cell`, "Handoff wiring (studio default)", "Open connection settings", `/bot/admin/bot?edit=1`} {
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
	for _, want := range []string{`id="studio-routes"`, `name="action" value="save_studio_routes"`, `name="st_notify_tt-lay"`, `class="route-row"`, `data-route-row="tt-lay"`, `data-route-edit-start`, `Edit wiring`, `data-route-edit-cancel`, `data-route-edit-actions`, `data-route-add-select`, `data-route-add`} {
		if !strings.Contains(body, want) {
			t.Fatalf("studio section is missing %q", want)
		}
	}
	for _, taskType := range handoffTaskTypes() {
		wantItem := `<div class="route-item" data-route-item="` + taskType.ID + `">`
		if !strings.Contains(body, wantItem) {
			t.Fatalf("studio section must render all task types visibly from the start, missing %q", wantItem)
		}
	}
	for _, gone := range []string{`data-route-source-select`, `data-route-source-add`, `data-route-toggle`, `data-route-mode`, `route-seg`, `name="st_none_`, "Add source task type", "Handoff mode"} {
		if strings.Contains(body, gone) {
			t.Fatalf("studio section must omit removed control %q", gone)
		}
	}
	if strings.Contains(body, "<h2>Handoff wiring</h2>") {
		t.Fatalf("studio section must omit duplicate inner heading: %s", body)
	}
	// A Task Type is never offered as its own target.
	if strings.Contains(body, `name="st_notify_tt-lay" value="tt-lay"`) {
		t.Fatal("the studio editor offers Layout as its own target")
	}
	if strings.Contains(body, `name="st_notify_tt-comp" value="tt-comp"`) {
		t.Fatal("the studio editor offers Compositing as its own target")
	}
	// Normal mode renders read-only chips without × buttons.
	if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{FromTaskTypeID: "tt-lay", FromTaskTypeName: "Layout", NotifyTaskTypeIDs: "tt-anim"}); err != nil {
		t.Fatal(err)
	}
	seeded := studioRoutesSection(t, db)
	if !strings.Contains(seeded, `<span class="route-chip route-chip-static">Animation</span>`) {
		t.Fatalf("normal mode must render static target chip without ×: %s", seeded)
	}
}

func TestAdminButtonsHaveNoGlowBoxShadow(t *testing.T) {
	css := adminThemeCSS + shellHeadExtras()
	for _, glow := range []string{
		"0 14px 30px rgba(232,90,26",
		"0 8px 20px rgba(232,90,26",
		"0 10px 24px rgba(232,90,26",
		"0 8px 18px rgba(182,59,45",
	} {
		if strings.Contains(css, glow) {
			t.Fatalf("button glow box-shadow %q must be removed", glow)
		}
	}
}

func TestWiringEditRowsStayCompactWithoutCardBoxes(t *testing.T) {
	css := adminThemeCSS
	for _, forbidden := range []string{
		".route-item.is-editing",
		"padding:8px 0 14px 182px",
		"padding:4px 0 12px 162px",
	} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("wiring edit mode CSS must not style rows as indented cards (%q)", forbidden)
		}
	}
	if !strings.Contains(css, `.route-editor{flex:none;margin-left:auto`) {
		t.Fatal("studio wiring editor add-controls must be pinned right with flex:none;margin-left:auto")
	}
	if !strings.Contains(css, `.handoff-override-editor{flex:none;margin-left:auto`) {
		t.Fatal("handoff override add-controls must be pinned right with flex:none;margin-left:auto")
	}
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	studioBody := studioRoutesSection(t, db)
	if !strings.Contains(studioBody, `class="route-row-body"`) {
		t.Fatal("studio wiring rows must use inline .route-row-body")
	}
	if strings.Contains(studioBody, `route-seg`) || strings.Contains(studioBody, `data-route-mode`) {
		t.Fatal("studio wiring rows must not render per-row state toggle")
	}
	handoffBody := handoffGet(t, db).Body.String()
	if !strings.Contains(handoffBody, `class="route-row-body"`) {
		t.Fatal("handoff override rows must use inline .route-row-body")
	}
	if !strings.Contains(handoffBody, `<select hidden name="ov_mode_tt-lay"`) {
		t.Fatal("handoff override mode select must remain hidden")
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
	if _, ok := bySource["tt-comp"]; ok {
		t.Fatalf("Compositing with no targets must store nothing, got %+v", bySource["tt-comp"])
	}
	// The reloaded section shows the saved wiring checked and unwired rows as No wiring.
	body := studioRoutesSection(t, db)
	if !strings.Contains(body, `name="st_notify_tt-lay" value="tt-anim" checked`) {
		t.Fatal("saved wiring did not reload as checked")
	}
	if !strings.Contains(body, `No wiring`) {
		t.Fatal("unwired row did not render No wiring")
	}
	// Target checkboxes must carry data-route-name so the client-side
	// editor script renders task-type names instead of raw IDs.
	for _, want := range []string{
		`name="st_notify_tt-lay" value="tt-anim" checked data-route-name="Animation"`,
		`name="st_notify_tt-lay" value="tt-comp" data-route-name="Compositing"`,
		`data-route-name="Layout"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("studio editor markup is missing %q", want)
		}
	}
	if !strings.Contains(body, `getAttribute("data-route-name")`) {
		t.Fatal("studio editor script no longer reads data-route-name")
	}
	// A submit of the Production page must not touch the studio routes.
	if w := handoffPost(t, db, productionForm()); w.Code != http.StatusSeeOther {
		t.Fatalf("production save status = %d, body: %s", w.Code, w.Body.String())
	}
	routes, err = model.LoadStudioTaskRoutes(db)
	if err != nil || len(routes) != 1 {
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

func TestHandoffFallbackDefaultsToDisabledAndRendersMemberDropdown(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	project := model.FindProjectByKitsuID(db, "p1")
	project.DiscordGuildID = "11111111111111111"
	if err := db.Save(project).Error; err != nil {
		t.Fatal(err)
	}
	guildMemberListCache.invalidate("11111111111111111")
	oldMembers := reviewerGuildMembersForGuild
	t.Cleanup(func() { reviewerGuildMembersForGuild = oldMembers })
	reviewerGuildMembersForGuild = func(string, string) ([]DiscordGuildMember, error) {
		var m DiscordGuildMember
		m.User.ID = "22222222222222222"
		m.User.Username = "alice"
		return []DiscordGuildMember{m}, nil
	}
	if err := setRuntimeDiscordBotToken(db, "bot-token"); err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID:   "p1",
			NoAssignee:     model.HandoffFallbackUsers,
			FallbackUserID: "99999999999999999",
		},
	}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/bot/admin/handoff?project=p1&lang=ja", nil)
	w := httptest.NewRecorder()
	HandoffSettingsHandler(db)(w, r)
	body := w.Body.String()
	if !strings.Contains(body, `<select id="handoff-fallback-user" name="fallback_user">`) {
		t.Fatalf("expected member dropdown for fallback_user: %s", body)
	}
	if !strings.Contains(body, `<option value="22222222222222222">`) || !strings.Contains(body, `<option value="99999999999999999" selected>99999999999999999</option>`) {
		t.Fatalf("expected guild member and preserved raw fallback ID in dropdown: %s", body)
	}
	if strings.Contains(body, `<input id="handoff-fallback-user"`) {
		t.Fatalf("fallback_user must not be a raw text input")
	}
}

func TestHandoffStoredExplicitNonePreservedAcrossUnrelatedSave(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if err := model.SaveProductionTaskRoute(db, model.ProductionTaskRoute{
		ProductionID:      "p1",
		FromTaskTypeID:    "tt-comp",
		FromTaskTypeName:  "Compositing",
		NotifyTaskTypeIDs: model.TaskRouteNone,
	}); err != nil {
		t.Fatal(err)
	}
	body := handoffGet(t, db).Body.String()
	if !strings.Contains(body, `data-handoff-summary="none">No handoff (inherited:`) {
		t.Fatalf("stored explicit-none override must display as No handoff in normal mode: %s", body)
	}
	if !strings.Contains(body, `select.value="custom"`) || !strings.Contains(body, `select.hidden=true`) {
		t.Fatal("handoffPageScript must keep ov_mode hidden and switch to custom when adding a target")
	}
	// An unrelated save (changing no_assignee while ov_mode_tt-comp remains "none") preserves the override.
	form := productionForm()
	form["ov_mode_tt-comp"] = []string{"none"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	route, _, found, err := model.EffectiveTaskRoute(db, "p1", "tt-comp")
	if err != nil || !found || len(route) != 0 {
		t.Fatalf("stored explicit-none override was not preserved: route=%v found=%v err=%v", route, found, err)
	}
}
