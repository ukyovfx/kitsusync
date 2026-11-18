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
	for _, row := range []string{"Animation × layout", "Animation × camera", "Lighting × layout"} {
		if !strings.Contains(body, row) {
			t.Fatalf("row %q is missing from the generated table", row)
		}
	}
	if strings.Count(body, `<option value="shelve" selected>`) < 6 {
		t.Fatalf("non-preset rows must default to shelving, got: %s", body)
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
