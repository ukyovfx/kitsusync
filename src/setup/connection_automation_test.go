package setup

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"

	"app/src/api/kitsu"
	"app/src/model"
)

func stubTaskTypeNames(t *testing.T, names ...string) {
	t.Helper()
	types := make([]kitsu.TaskType, 0, len(names))
	for _, name := range names {
		types = append(types, kitsu.TaskType{ID: "tt-" + strings.ToLower(name), Name: name})
	}
	stubHandoffTaskTypes(t, types)
}

// connectNewProduction runs the wizard's execute step for a Production.
func connectNewProduction(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	plan := firstTimeConnectionPlan{Project: KitsuProject{ID: id, Name: "Production " + id}, GuildID: "g1", NotificationLanguage: "ja"}
	r := httptest.NewRequest("POST", "/bot/admin/setup", nil)
	return executeFirstTimeConnectionLocked(plan, "bot-token", "ja", r, db)
}

func autoCreateAuditRows(t *testing.T, db *gorm.DB, id string) []model.AuditLog {
	t.Helper()
	var rows []model.AuditLog
	if err := db.Where("project_id = ? AND task_id = ?", id, "forum-autocreate").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestConnectingNewProductionCreatesForumsAndTagsWithoutButton(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	stubTaskTypeNames(t, "Animation", "Compositing")
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)

	connectNewProduction(t, db, "p2")

	if strings.Join(created, ",") != "Animation,Compositing" {
		t.Fatalf("created forums = %v", created)
	}
	settings, ok, err := model.LoadProductionForumSettings(db, "p2")
	if err != nil || !ok {
		t.Fatalf("forum settings: ok=%v err=%v", ok, err)
	}
	if len(settings.Routes) != 2 || settings.Routes[0].ForumChannelID != "new-Animation" {
		t.Fatalf("routes = %+v", settings.Routes)
	}
	if len(settings.Tags) != 8 {
		t.Fatalf("tags = %d, want 4 roles x 2 forums", len(settings.Tags))
	}
	if len(settings.StatusRoles) == 0 {
		t.Fatal("status roles were not filled from the template")
	}
	if rows := autoCreateAuditRows(t, db, "p2"); len(rows) != 1 || !rows[0].Success {
		t.Fatalf("audit = %+v", rows)
	}

	// Pressing the repair button afterwards, and connecting again, add nothing.
	if _, err := createForumsForTaskTypes(db, "p2", "bot-token", "g1", model.ProductionForumSettings{}); err != nil {
		t.Fatal(err)
	}
	connectNewProduction(t, db, "p2")
	if len(created) != 2 {
		t.Fatalf("forums duplicated: %v", created)
	}
}

func TestConnectingNewProductionSurvivesDiscordFailureAndMarksNeedsCheck(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	stubTaskTypeNames(t, "Animation")
	old := forumCreateChannel
	t.Cleanup(func() { forumCreateChannel = old })
	forumCreateChannel = func(string, string, string, []forumAutoTag, string) (forumChannelInfo, error) {
		return forumChannelInfo{}, errors.New("discord down")
	}
	db := newForumPageDB(t)

	page := connectNewProduction(t, db, "p2")

	if model.FindProjectByKitsuID(db, "p2") == nil {
		t.Fatal("the connection must survive a failed forum creation")
	}
	if !strings.Contains(page, tr("ja", "wizard.complete_title")) {
		t.Fatalf("wizard did not complete: %s", page)
	}
	if model.GetSetting(db, forumAutoCreateCheckKey("p2")) == "" {
		t.Fatal("needs-check mark missing")
	}
	if rows := autoCreateAuditRows(t, db, "p2"); len(rows) != 1 || rows[0].Success || rows[0].ErrorMessage == "" {
		t.Fatalf("audit = %+v", rows)
	}

	// The health page lists it as needing attention, with the reason.
	w := httptest.NewRecorder()
	HealthHandler(db)(w, httptest.NewRequest("GET", "/bot/admin/health?legacy=1", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Production p2") || !strings.Contains(body, "discord down") {
		t.Fatalf("health page does not show the needs-check reason: %s", body)
	}

	// The repair button succeeds once Discord is back and clears the mark.
	var made []string
	stubForumCreate(t, &made)
	if _, err := createForumsForTaskTypes(db, "p2", "bot-token", "g1", model.ProductionForumSettings{}); err != nil {
		t.Fatal(err)
	}
	if model.GetSetting(db, forumAutoCreateCheckKey("p2")) != "" {
		t.Fatal("needs-check mark not cleared after a successful repair")
	}
}

func TestConnectedProductionIsNotAutoCreatedAgain(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	stubTaskTypeNames(t, "Animation", "Compositing")
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t) // p1 is already connected

	connectNewProduction(t, db, "p1") // repair mode

	if len(created) != 0 {
		t.Fatalf("forums created for an already connected Production: %v", created)
	}
	if _, ok, _ := model.LoadProductionHandoffSettings(db, "p1"); ok {
		t.Fatal("handoff template applied to an existing Production")
	}
	if len(autoCreateAuditRows(t, db, "p1")) != 0 {
		t.Fatal("automation ran for an already connected Production")
	}
}

func TestNewProductionStartsWithHandoffTemplate(t *testing.T) {
	stubForumLookups(t, nil)
	stubNoExistingForums(t)
	stubTaskTypeNames(t, "Layout", "Animation", "Lighting", "Compositing", "Rendering")
	var created []string
	stubForumCreate(t, &created)
	db := newForumPageDB(t)

	connectNewProduction(t, db, "p2")

	settings, ok, err := model.LoadProductionHandoffSettings(db, "p2")
	if err != nil || !ok {
		t.Fatalf("handoff settings: ok=%v err=%v", ok, err)
	}
	rows := map[string]model.ProductionHandoffRoute{}
	for _, route := range settings.Routes {
		rows[route.TaskTypeName+"|"+route.ProductType] = route
	}
	// Rows whose source Task Type is missing (素材取り込み, Modeling, ...) are skipped.
	if _, found := rows["Modeling|model"]; found {
		t.Fatal("a row for a missing Task Type was seeded")
	}
	want := map[string][2]string{
		"Layout|camera":         {model.HandoffFlowHand, "Animation,Lighting,Compositing"},
		"Layout|layout":         {model.HandoffFlowHand, "Animation,Lighting"},
		"Compositing|matchmove": {model.HandoffFlowHand, "Layout,Animation"},
		"Compositing|render":    {model.HandoffFlowSubmit, ""},
		"Compositing|workfile":  {model.HandoffFlowShelve, ""},
		"Rendering|renderPass":  {model.HandoffFlowHand, "Compositing"},
		"Lighting|lighting":     {model.HandoffFlowHand, "Rendering"},
		"Animation|pointcache":  {model.HandoffFlowHand, "Lighting,Rendering"}, // FX is not in this Production
	}
	for key, expect := range want {
		route, found := rows[key]
		if !found {
			t.Fatalf("row %s missing; have %v", key, rows)
		}
		if route.Flow != expect[0] || route.NotifyTaskTypeName != expect[1] {
			t.Fatalf("row %s = %s / %q, want %s / %q", key, route.Flow, route.NotifyTaskTypeName, expect[0], expect[1])
		}
	}
	if len(settings.Routes) != len(want) {
		t.Fatalf("routes = %d, want %d", len(settings.Routes), len(want))
	}
	products := map[string]bool{}
	for _, product := range settings.ProductTypes {
		products[product.Name] = true
		if product.Unclassified {
			t.Fatalf("template product %s is marked unclassified", product.Name)
		}
	}
	for _, name := range []string{"camera", "layout", "matchmove", "render", "workfile", "renderPass", "lighting", "pointcache"} {
		if !products[name] {
			t.Fatalf("product type %s not registered: %v", name, products)
		}
	}
}

func TestHandoffTemplateLeavesExistingSettingsAlone(t *testing.T) {
	stubTaskTypeNames(t, "Layout", "Animation")
	db := newForumPageDB(t)
	existing := model.ProductionHandoffSettings{
		Config:       model.ProductionHandoffConfig{ProductionID: "p1", NotifyTiming: model.HandoffTimingDigest, NoAssignee: model.HandoffFallbackDisabled},
		ProductTypes: []model.ProductionHandoffProductType{{Name: "mine"}},
	}
	if err := model.SaveProductionHandoffSettings(db, existing); err != nil {
		t.Fatal(err)
	}
	if err := seedHandoffTemplate(db, "p1"); err != nil {
		t.Fatal(err)
	}
	got, _, _ := model.LoadProductionHandoffSettings(db, "p1")
	if len(got.Routes) != 0 || len(got.ProductTypes) != 1 || got.Config.NotifyTiming != model.HandoffTimingDigest {
		t.Fatalf("existing settings were changed: %+v", got)
	}
}

// An unclassified product type shows as such on the mapping page, starts
// shelved, and keeps its mark through a save that does not classify it.
func TestHandoffPageShowsUnclassifiedAndKeepsMarkUntilClassified(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config:       model.ProductionHandoffConfig{ProductionID: "p1", NotifyTiming: model.HandoffTimingMixed, NoAssignee: model.HandoffFallbackAdmin},
		ProductTypes: []model.ProductionHandoffProductType{{Name: "vdb", Unclassified: true}},
	}); err != nil {
		t.Fatal(err)
	}
	body := handoffGet(t, db).Body.String()
	if !strings.Contains(body, "Unclassified") || !strings.Contains(body, `name="product_unclassified" value="vdb"`) {
		t.Fatalf("unclassified product not shown: %s", body)
	}

	// Saving the page as it stands (all rows still shelved) keeps the mark.
	form := url.Values{"project": {"p1"}, "notify_timing": {model.HandoffTimingMixed}, "no_assignee": {model.HandoffFallbackAdmin}, "product_name": {"vdb"}, "product_unclassified": {"vdb"}}
	form["row_task"], form["row_task_name"], form["row_product"], form["row_flow"] = []string{"tt-lay"}, []string{"Layout"}, []string{"vdb"}, []string{model.HandoffFlowShelve}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d: %s", w.Code, w.Body.String())
	}
	saved, _, _ := model.LoadProductionHandoffSettings(db, "p1")
	if len(saved.ProductTypes) != 1 || !saved.ProductTypes[0].Unclassified {
		t.Fatalf("mark lost on an unrelated save: %+v", saved.ProductTypes)
	}

	// Choosing a flow for one of its rows classifies it.
	form["row_flow"] = []string{model.HandoffFlowHand}
	form["row_notify_tt-lay|vdb"] = []string{"tt-anim"}
	if w := handoffPost(t, db, form); w.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d: %s", w.Code, w.Body.String())
	}
	saved, _, _ = model.LoadProductionHandoffSettings(db, "p1")
	if saved.ProductTypes[0].Unclassified {
		t.Fatal("a classified product type is still marked unclassified")
	}
}
