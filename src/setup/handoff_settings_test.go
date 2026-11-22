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

func TestHandoffPageArrangesRowsWithoutProductTypes(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	w := handoffGet(t, db)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Register at least one product type") {
		t.Fatal("a Production without product types must say the rows need a product type")
	}
	if strings.Contains(body, `name="row_task"`) {
		t.Fatal("no mapping rows may be generated without a product type")
	}
	if !strings.Contains(body, `<option value="mixed" selected>`) {
		t.Fatal("an untouched Production must start on the mixed timing")
	}
	// Nothing stored: the handoff path stays inactive.
	if _, ok, err := model.LoadProductionHandoffSettings(db, "p1"); err != nil || ok {
		t.Fatalf("an unconfigured Production must have no handoff settings: ok=%v err=%v", ok, err)
	}
}

func TestHandoffPageAppliesPresetsToMatchingRowsOnly(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	w := handoffPost(t, db, url.Values{
		"project":       {"p1"},
		"product_name":  {"layout", "camera"},
		"notify_timing": {"digest"},
		"no_assignee":   {"admin"},
	})
	// The first save only stores the product types; the page then renders rows.
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	// Rows come from the stored product types, with the preset applied on render.
	w = handoffGet(t, db)
	body := w.Body.String()
	if !strings.Contains(body, `<option value="hand" selected>`) {
		t.Fatal("Layout × layout must default to the handoff flow from the preset")
	}
	// Animation × layout is not in the preset table and must stay shelved.
	for _, cell := range []string{"tt-anim|layout", "tt-anim|camera", "tt-light|layout"} {
		if !strings.Contains(body, `data-handoff-cell="`+cell+`"`) {
			t.Fatalf("cell %q is missing from the generated matrix", cell)
		}
	}
	if strings.Count(body, `<option value="shelve" selected>`) < 6 {
		t.Fatalf("non-preset rows must default to shelving, got: %s", body)
	}
}

// Rendering hands off renderPass to Compositing; Compositing's own render
// submission stays the only "render" preset row (owner decision 2026-11-19).
func TestHandoffPresetRenderingUsesRenderPass(t *testing.T) {
	stubHandoffTaskTypes(t, []kitsu.TaskType{
		{ID: "tt-comp", Name: "Compositing"},
		{ID: "tt-render", Name: "Rendering"},
	})
	db := newForumPageDB(t)
	w := handoffPost(t, db, url.Values{
		"project":       {"p1"},
		"product_name":  {"renderPass", "render"},
		"notify_timing": {"digest"},
		"no_assignee":   {"admin"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	body := handoffGet(t, db).Body.String()
	for cell, wants := range map[string][]string{
		`data-handoff-cell="tt-render|renderPass"`: {
			`<option value="hand" selected>`,
			`name="row_notify_tt-render|renderPass" value="tt-comp" checked`,
		},
		`data-handoff-cell="tt-comp|render"`:   {`<option value="submit" selected>`},
		`data-handoff-cell="tt-render|render"`: {`<option value="shelve" selected>`},
	} {
		start := strings.Index(body, cell)
		if start < 0 {
			t.Fatalf("cell %q is missing from the matrix: %s", cell, body)
		}
		row := body[start:]
		if end := strings.Index(row, "</td>"); end > 0 {
			row = row[:end]
		}
		for _, want := range wants {
			if !strings.Contains(row, want) {
				t.Fatalf("cell %q is missing %q, got: %s", cell, want, row)
			}
		}
	}
}

func TestHandoffSaveStoresSelectionAndReloadsIt(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	form := url.Values{
		"project":                   {"p1"},
		"product_name":              {"layout", "camera"},
		"notify_timing":             {"immediate"},
		"no_assignee":               {"users"},
		"fallback_user":             {"u-42"},
		"row_task":                  {"tt-lay", "tt-lay", "tt-anim", "tt-comp"},
		"row_task_name":             {"Layout", "Layout", "Animation", "Compositing"},
		"row_product":               {"layout", "camera", "layout", "camera"},
		"row_flow":                  {"hand", "hand", "shelve", "submit"},
		"row_notify_tt-lay|layout":  {"tt-anim", "tt-light"},
		"row_notify_tt-lay|camera":  {"tt-anim"},
		"row_notify_tt-comp|camera": {},
		"row_notify_tt-anim|layout": {},
	}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	loaded, ok, err := model.LoadProductionHandoffSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if loaded.Config.NotifyTiming != model.HandoffTimingImmediate || loaded.Config.NoAssignee != model.HandoffFallbackUsers || loaded.Config.FallbackUserID != "u-42" {
		t.Fatalf("notification settings not stored: %+v", loaded.Config)
	}
	if len(loaded.ProductTypes) != 2 || len(loaded.Routes) != 4 {
		t.Fatalf("unexpected stored shape: %+v", loaded)
	}
	byKey := map[string]model.ProductionHandoffRoute{}
	for _, route := range loaded.Routes {
		byKey[route.TaskTypeID+"|"+route.ProductType] = route
	}
	layer := byKey["tt-lay|layout"]
	if layer.Flow != model.HandoffFlowHand || layer.NotifyTaskTypeIDs != "tt-anim,tt-light" || layer.NotifyTaskTypeName != "Animation,Lighting" {
		t.Fatalf("Layout × layout stored as %+v", layer)
	}
	if got := byKey["tt-comp|camera"]; got.Flow != model.HandoffFlowSubmit || got.NotifyTaskTypeIDs != "" {
		t.Fatalf("Compositing × camera stored as %+v", got)
	}

	// Reload the page: every selection comes back checked.
	body := handoffGet(t, db).Body.String()
	for _, want := range []string{
		`value="tt-anim" checked`,
		`value="tt-light" checked`,
		`<option value="immediate" selected>`,
		`<option value="users" selected>`,
		`value="u-42"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("reloaded page is missing %q", want)
		}
	}
}

func TestHandoffSaveRejectsWrongInput(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	form := url.Values{
		"project":       {"p1"},
		"product_name":  {"layout"},
		"notify_timing": {"weekly"},
		"no_assignee":   {"users"},
		"row_task":      {"tt-lay", "tt-lay"},
		"row_task_name": {"Layout", "Layout"},
		"row_product":   {"layout", "model"},
		"row_flow":      {"hand", "shelve"},
	}
	w := handoffPost(t, db, form)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"Choose a notification timing",
		"needs that user&#39;s ID",
		"model",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rejected save does not explain %q", want)
		}
	}
	if !strings.Contains(body, "not registered") {
		t.Fatal("a row using an unregistered product type must be explained")
	}
	if _, ok, _ := model.LoadProductionHandoffSettings(db, "p1"); ok {
		t.Fatal("a rejected save must not store settings")
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
	w := handoffPost(t, db, url.Values{"project": {"p1"}, "product_name": {"layout"}, "notify_timing": {"digest"}, "no_assignee": {"admin"}})
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

func TestHandoffVisualChipsAndDotsRendered(t *testing.T) {
	stubHandoffTaskTypes(t, []kitsu.TaskType{
		{ID: "tt-lay", Name: "Layout"},
		{ID: "tt-anim", Name: "Animation"},
		{ID: "tt-comp", Name: "Compositing"},
		{ID: "tt-light", Name: "Lighting"},
	})
	db := newForumPageDB(t)
	// Save product types to activate the matrix.
	if err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID: "p1",
			NotifyTiming: model.HandoffTimingMixed,
			NoAssignee:   model.HandoffFallbackAdmin,
		},
		ProductTypes: []model.ProductionHandoffProductType{
			{Name: "camera"},
			{Name: "render"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	body := handoffGet(t, db).Body.String()

	// 1. Product type chip layout and add chip exist.
	if !strings.Contains(body, `class="handoff-products-wrap"`) {
		t.Fatal("product types must be wrapped in handoff-products-wrap")
	}
	if !strings.Contains(body, `class="handoff-product-chip"`) {
		t.Fatal("each product type must be a chip")
	}
	if !strings.Contains(body, `class="handoff-product-chip handoff-product-chip-add"`) {
		t.Fatal("an add product type chip with dashed border must exist")
	}

	// 2. Layout × camera has preset "Hand off" to Animation, Lighting, Compositing.
	cellLayCam := body[strings.Index(body, `data-handoff-cell="tt-lay|camera"`):]
	cellLayCam = cellLayCam[:strings.Index(cellLayCam, "</td>")]
	if !strings.Contains(cellLayCam, `class="handoff-chip handoff-chip-hand"`) {
		t.Fatalf("Layout × camera must have hand chip, got: %s", cellLayCam)
	}
	for _, want := range []string{`<span class="handoff-chip-flow">Hand off</span>`, `→ Animation, Compositing, Lighting`} {
		if !strings.Contains(cellLayCam, want) {
			t.Fatalf("Layout × camera handoff summary must contain %q, got: %s", want, cellLayCam)
		}
	}
	if !strings.Contains(cellLayCam, `class="handoff-edit"`) {
		t.Fatalf("Layout × camera must have handoff-edit container, got: %s", cellLayCam)
	}

	// 3. Compositing × render has preset "Submit".
	cellCompRend := body[strings.Index(body, `data-handoff-cell="tt-comp|render"`):]
	cellCompRend = cellCompRend[:strings.Index(cellCompRend, "</td>")]
	if !strings.Contains(cellCompRend, `class="handoff-chip handoff-chip-submit"`) {
		t.Fatalf("Compositing × render must have submit chip, got: %s", cellCompRend)
	}
	if !strings.Contains(cellCompRend, `Submit`) {
		t.Fatalf("Compositing × render must display Submit label, got: %s", cellCompRend)
	}

	// 4. Animation × camera has no preset, so it defaults to shelving with a dot.
	cellAnimCam := body[strings.Index(body, `data-handoff-cell="tt-anim|camera"`):]
	cellAnimCam = cellAnimCam[:strings.Index(cellAnimCam, "</td>")]
	if !strings.Contains(cellAnimCam, `class="handoff-dot"`) {
		t.Fatalf("Animation × camera must have a dot for shelve, got: %s", cellAnimCam)
	}
	if !strings.Contains(cellAnimCam, `title="Shelve">·</div>`) {
		t.Fatalf("Animation × camera dot must show · with Shelve title, got: %s", cellAnimCam)
	}
}

func TestHandoffNoChangeSavePreservesDatabaseSettings(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)

	// Seed configured handoff settings with diverse flows and notify sets.
	initial := model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID:   "p1",
			NotifyTiming:   model.HandoffTimingImmediate,
			NoAssignee:     model.HandoffFallbackUsers,
			FallbackUserID: "user-42",
		},
		ProductTypes: []model.ProductionHandoffProductType{
			{Name: "camera"},
			{Name: "plate"},
		},
		Routes: []model.ProductionHandoffRoute{
			{
				TaskTypeID:         "tt-lay",
				TaskTypeName:       "Layout",
				ProductType:        "camera",
				Flow:               model.HandoffFlowHand,
				NotifyTaskTypeIDs:  "tt-anim,tt-comp",
				NotifyTaskTypeName: "Animation,Compositing",
			},
			{
				TaskTypeID:   "tt-comp",
				TaskTypeName: "Compositing",
				ProductType:  "camera",
				Flow:         model.HandoffFlowSubmit,
			},
			{
				TaskTypeID:   "tt-anim",
				TaskTypeName: "Animation",
				ProductType:  "plate",
				Flow:         model.HandoffFlowShelve,
			},
		},
	}
	if err := model.SaveProductionHandoffSettings(db, initial); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	before, ok, err := model.LoadProductionHandoffSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load before failed: ok=%v err=%v", ok, err)
	}

	// Render the page as rendered to the operator.
	getResp := handoffGet(t, db)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get page failed: code=%d", getResp.Code)
	}
	body := getResp.Body.String()
	if !strings.Contains(body, `name="fallback_user" value="user-42"`) {
		t.Fatalf("expected fallback_user in body: %s", body)
	}

	// Extract all inputs as rendered in the form DOM and post them without modification.
	form := url.Values{
		"project":       {"p1"},
		"notify_timing": {before.Config.NotifyTiming},
		"no_assignee":   {before.Config.NoAssignee},
		"fallback_user": {before.Config.FallbackUserID},
	}
	for _, pt := range before.ProductTypes {
		form.Add("product_name", pt.Name)
	}
	// Verify that the DOM contains the inputs for all routes and post them.
	for _, route := range before.Routes {
		form.Add("row_task", route.TaskTypeID)
		form.Add("row_task_name", route.TaskTypeName)
		form.Add("row_product", route.ProductType)
		form.Add("row_flow", route.Flow)
		for _, id := range model.SplitHandoffNotifyIDs(route.NotifyTaskTypeIDs) {
			form.Add("row_notify_"+route.TaskTypeID+"|"+route.ProductType, id)
		}
	}

	// Submit identical form (no-change save).
	w := handoffPost(t, db, form)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("post failed: code=%d body=%s", w.Code, w.Body.String())
	}

	// Load settings from DB after save and verify exact equality.
	after, ok, err := model.LoadProductionHandoffSettings(db, "p1")
	if err != nil || !ok {
		t.Fatalf("load after failed: ok=%v err=%v", ok, err)
	}

	if before.Config.NotifyTiming != after.Config.NotifyTiming ||
		before.Config.NoAssignee != after.Config.NoAssignee ||
		before.Config.FallbackUserID != after.Config.FallbackUserID {
		t.Fatalf("config changed: before=%+v, after=%+v", before.Config, after.Config)
	}
	if len(before.ProductTypes) != len(after.ProductTypes) {
		t.Fatalf("product types length changed: before=%d after=%d", len(before.ProductTypes), len(after.ProductTypes))
	}
	for i := range before.ProductTypes {
		if before.ProductTypes[i].Name != after.ProductTypes[i].Name {
			t.Fatalf("product type[%d] changed: %s vs %s", i, before.ProductTypes[i].Name, after.ProductTypes[i].Name)
		}
	}
	if len(before.Routes) != len(after.Routes) {
		t.Fatalf("routes length changed: before=%d after=%d", len(before.Routes), len(after.Routes))
	}
	byKeyBefore := map[string]model.ProductionHandoffRoute{}
	for _, r := range before.Routes {
		byKeyBefore[r.TaskTypeID+"|"+r.ProductType] = r
	}
	for _, afterRoute := range after.Routes {
		key := afterRoute.TaskTypeID + "|" + afterRoute.ProductType
		beforeRoute, exists := byKeyBefore[key]
		if !exists {
			t.Fatalf("route %s added in after", key)
		}
		if beforeRoute.Flow != afterRoute.Flow {
			t.Fatalf("route %s flow changed: %s vs %s", key, beforeRoute.Flow, afterRoute.Flow)
		}
		if beforeRoute.NotifyTaskTypeIDs != afterRoute.NotifyTaskTypeIDs {
			t.Fatalf("route %s notify ids changed: %s vs %s", key, beforeRoute.NotifyTaskTypeIDs, afterRoute.NotifyTaskTypeIDs)
		}
	}
}
