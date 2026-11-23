package setup

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	HealthHandler(db, time.Minute)(w, httptest.NewRequest("GET", "/bot/admin/health?legacy=1", nil))
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

	// The seed writes the studio-wide wiring from the folding table
	// (ui-redesign-core-draft-v2 §3.6), restricted to Task Types that exist,
	// plus the Production's no-assignee default. No per-Production matrix
	// rows or product types are seeded anymore.
	routes, err := model.LoadStudioTaskRoutes(db)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, route := range routes {
		byName[route.FromTaskTypeName] = route.NotifyTaskTypeIDs
	}
	idByName := map[string]string{}
	for _, tt := range handoffTaskTypeLister(db, "p2") {
		idByName[tt.Name] = tt.ID
	}
	wantNames := map[string][]string{
		"Layout":      {"Animation", "Lighting", "Compositing"},
		"Animation":   {"Lighting", "Rendering"}, // FX is not in this Production
		"Lighting":    {"Rendering"},
		"Rendering":   {"Compositing"},
		"Compositing": {"Layout", "Animation"},
	}
	if len(byName) != len(wantNames) {
		t.Fatalf("studio routes = %v, want sources %v", byName, wantNames)
	}
	for source, targets := range wantNames {
		var wantIDs []string
		for _, name := range targets {
			wantIDs = append(wantIDs, idByName[name])
		}
		if got := byName[source]; got != strings.Join(wantIDs, ",") {
			t.Fatalf("studio route %s = %q, want %q", source, got, strings.Join(wantIDs, ","))
		}
	}
	settings, ok, err := model.LoadProductionHandoffSettings(db, "p2")
	if err != nil || !ok {
		t.Fatalf("handoff settings: ok=%v err=%v", ok, err)
	}
	if settings.Config.NoAssignee != model.HandoffFallbackAdmin {
		t.Fatalf("no-assignee default = %q, want admin", settings.Config.NoAssignee)
	}
	if len(settings.Routes) != 0 || len(settings.ProductTypes) != 0 {
		t.Fatalf("per-Production seed rows remain: routes=%d products=%d", len(settings.Routes), len(settings.ProductTypes))
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

// The handoff page renders the studio wiring the seed wrote, with the
// Production inheriting it until an override says otherwise.
func TestHandoffPageShowsSeededStudioRoutes(t *testing.T) {
	stubHandoffTaskTypes(t, handoffTaskTypes())
	db := newForumPageDB(t)
	if err := seedHandoffTemplate(db, "p1"); err != nil {
		t.Fatal(err)
	}
	body := handoffGet(t, db).Body.String()
	// Layout -> Animation, Lighting, Compositing per the folding table.
	if !strings.Contains(body, `name="st_notify_tt-lay" value="tt-anim" checked`) {
		t.Fatalf("seeded studio wiring not shown: %s", body)
	}
	if !strings.Contains(body, "Animation, Lighting, Compositing") {
		t.Fatalf("inherited wiring not shown in the production section: %s", body)
	}
}
