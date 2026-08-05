package setup

import (
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newIAViewDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:ia-view-tests?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionChannelMapping{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}, &model.AuditLog{}, &model.UserMap{}, &model.ProjectUserMap{}, &model.ProjectCheckerMap{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestProductionCenteredViewsExposeApprovedSections(t *testing.T) {
	db := newIAViewDB(t)
	project := model.Project{KitsuProjectID: "synthetic-production", Name: "Synthetic Production", DiscordGuildID: "synthetic-server", DiscordCategoryID: "synthetic-category"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=synthetic-production&lang=en", nil)
	w := httptest.NewRecorder()
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	if !strings.Contains(body, `role="tablist"`) || !strings.Contains(body, `aria-selected="true"`) || !strings.Contains(body, `aria-labelledby="tab-overview"`) {
		t.Fatal("selected Production overview tab is not accessible")
	}
	for _, tab := range []string{"overview", "notifications", "user-settings", "storage-settings", "activity", "troubleshooting", "advanced", "danger-zone"} {
		r := httptest.NewRequest("GET", "/bot/admin/projects?project=synthetic-production&tab="+tab+"&lang=en", nil)
		w := httptest.NewRecorder()
		renderIAProductionList(w, r, db, "")
		if !strings.Contains(w.Body.String(), `aria-labelledby="tab-`+tab+`"`) {
			t.Fatalf("selected Production tab missing %q", tab)
		}
	}
	if strings.Contains(body, `id="panel-notifications"`) || strings.Contains(body, `id="panel-danger-zone"`) {
		t.Fatal("inactive Production panels remain rendered")
	}
	if strings.Contains(body, "<details class=\"accordion\"") {
		t.Fatal("selected Production view still uses the legacy accordion")
	}
	if strings.Contains(body, "name=\"guild_id\"") {
		t.Fatal("selected Production view exposes manual server ID editing")
	}
}

func TestGlobalUserMappingHasNoProductionOrRoleControls(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.UserMap{KitsuName: "Synthetic User", DiscordID: "synthetic-discord-id"})
	r := httptest.NewRequest("GET", "/bot/admin/users?lang=en", nil)
	w := httptest.NewRecorder()
	renderGlobalUserMapping(w, r, db)
	body := w.Body.String()
	for _, forbidden := range []string{"Editing production", "Reviewer / Checker task types", "Production selector", "Discord ID"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("global mapping leaked production-scoped control %q", forbidden)
		}
	}
	if !strings.Contains(body, "Synthetic User") || !strings.Contains(body, "Discord user mapped") {
		t.Fatal("global user mapping did not show user-facing identities")
	}
}

func TestPrimaryNavigationAndNewConnectionFlow(t *testing.T) {
	r := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	body := adminPage("en", "Dashboard", r, "")
	for _, want := range []string{"Dashboard", "Productions", "New Production Connection", "User Mapping", "Bot Connection", "System Status", "Audit Log"} {
		if !strings.Contains(body, want) {
			t.Fatalf("primary navigation missing %q", want)
		}
	}
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	renderIANewConnection(w, r, db)
	setup := w.Body.String()
	for _, want := range []string{"Prerequisites", "Production", "Discord server", "Channel plan", "Review", "Execute", "Complete", "Bot Connection is not configured"} {
		if !strings.Contains(setup, want) {
			t.Fatalf("new connection flow missing %q", want)
		}
	}
	if strings.Contains(setup, `name="new-connection-production"`) || strings.Contains(setup, `name="plan_guild"`) {
		t.Fatal("blocked wizard exposed later-step selectors")
	}
	if strings.Contains(setup, "Guild ID") || strings.Contains(setup, "Project Routing") {
		t.Fatal("new connection flow exposes implementation terms")
	}
	forced := httptest.NewRequest("GET", "/bot/setup?lang=en&wizard_step=2", nil)
	forcedWriter := httptest.NewRecorder()
	renderIANewConnection(forcedWriter, forced, db)
	if strings.Contains(forcedWriter.Body.String(), `id="wizard-production"`) {
		t.Fatal("later wizard step was reachable while prerequisites were blocked")
	}
}

func TestNewConnectionWizardUsesSharedJapaneseCatalog(t *testing.T) {
	db := newIAViewDB(t)
	r := httptest.NewRequest("GET", "/bot/setup?lang=ja", nil)
	w := httptest.NewRecorder()
	renderIANewConnection(w, r, db)
	body := w.Body.String()
	for _, want := range []string{"接続の準備状況", "Bot接続を設定", "新しいProductionを接続", "Discordサーバー"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese wizard missing %q", want)
		}
	}
	for _, forbidden := range []string{"Guild", "routing", "runtime", "readiness", "dry-run", "stale"} {
		visibleMarker := ">" + strings.ToLower(forbidden) + "<"
		if strings.Contains(strings.ToLower(body), visibleMarker) {
			t.Fatalf("Japanese wizard leaked internal term %q", forbidden)
		}
	}
}

func TestSharedStatusSummaryRowHasSemanticStructureAndVariants(t *testing.T) {
	body := statusSummaryRow("Discord Bot", "blocked", "Not configured", "Bot Connection is required.", `<a class="btn">Set up</a>`)
	for _, want := range []string{`class="status-row"`, `class="status-row-label"`, `class="status-badge status-badge-blocked"`, `aria-hidden="true"`, `class="status-row-explanation"`, `class="status-row-action"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("shared status row missing %q", want)
		}
	}
	if strings.Contains(body, `status-pill`) {
		t.Fatal("shared status row fell back to the legacy indistinguishable pill")
	}
}

func TestNormalViewsKeepTechnicalDetailsCollapsed(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "technical-details-p", Name: "Technical Details P", DiscordGuildID: "g", DiscordCategoryID: "c"})
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=technical-details-p&lang=ja", nil)
	w := httptest.NewRecorder()
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	if strings.Contains(body, "<details") || strings.Contains(body, ">g<") || strings.Contains(body, ">c<") || strings.Contains(body, "Production ID") {
		t.Fatal("default Overview exposed advanced or destructive details")
	}
	for _, tab := range []string{"advanced", "danger-zone"} {
		r := httptest.NewRequest("GET", "/bot/admin/projects?project=technical-details-p&tab="+tab+"&lang=ja", nil)
		w := httptest.NewRecorder()
		renderIAProductionList(w, r, db, "")
		body := w.Body.String()
		if !strings.Contains(body, "<details") && tab == "danger-zone" {
			t.Fatal("Danger Zone is not a disclosure section")
		}
	}
}

func TestSelectedProductionTabsHaveSingleAccessiblePanel(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "tab-semantics-p", Name: "Tab Semantics P"})
	for _, tab := range []string{"", "notifications", "user-settings", "storage-settings", "activity", "troubleshooting", "advanced", "danger-zone", "invalid"} {
		path := "/bot/admin/projects?project=tab-semantics-p&lang=en"
		if tab != "" {
			path += "&tab=" + tab
		}
		w := httptest.NewRecorder()
		renderIAProductionList(w, httptest.NewRequest("GET", path, nil), db, "")
		body := w.Body.String()
		if strings.Count(body, `role="tabpanel"`) != 1 || strings.Count(body, `role="tablist"`) < 1 {
			t.Fatalf("tab %q did not render one tab panel", tab)
		}
		if tab == "invalid" && !strings.Contains(body, `id="panel-overview"`) {
			t.Fatal("invalid tab did not fall back to Overview")
		}
	}
}

func TestSelectedProductionPolishAndDangerConfirmation(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "polish-p", Name: "Polish Production", DiscordGuildID: "synthetic-guild"})
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?project=polish-p&lang=en", nil), db, "")
	body := w.Body.String()
	if strings.Count(body, "Polish Production") != 1 {
		t.Fatalf("selected Production title rendered %d times", strings.Count(body, "Polish Production"))
	}
	if !strings.Contains(body, `class="section-nav production-tabs`) || !strings.Contains(body, `aria-selected="true"`) {
		t.Fatal("selected tab does not have the shared visual and ARIA state")
	}
	danger := httptest.NewRecorder()
	renderIAProductionList(danger, httptest.NewRequest("GET", "/bot/admin/projects?project=polish-p&tab=danger-zone&lang=en", nil), db, "")
	dangerBody := danger.Body.String()
	for _, want := range []string{`data-require-text="DISCONNECT"`, `data-require-text="DELETE"`, "Discord resources remain.", "separate from disconnecting"} {
		if !strings.Contains(dangerBody, want) {
			t.Fatalf("Danger Zone missing confirmation safeguard %q", want)
		}
	}
	if strings.Contains(dangerBody, `data-require-text="delete"`) {
		t.Fatal("Danger Zone confirmation is not exact and case-sensitive")
	}
}

func TestSelectedProductionJapaneseDangerConfirmation(t *testing.T) {
	db := newIAViewDB(t)
	db.Create(&model.Project{KitsuProjectID: "polish-ja-p", Name: "日本語Production"})
	w := httptest.NewRecorder()
	renderIAProductionList(w, httptest.NewRequest("GET", "/bot/admin/projects?project=polish-ja-p&tab=danger-zone&lang=ja", nil), db, "")
	body := w.Body.String()
	for _, want := range []string{`data-require-text="連携解除"`, `data-require-text="削除"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("Japanese confirmation phrase missing %q", want)
		}
	}
}

func TestDashboardUsesSharedReadinessAndShowsNextAction(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin?lang=en", nil)
	renderIADashboard(w, r, db)
	body := w.Body.String()
	for _, want := range []string{"Action required", "Complete Kitsu connection setup.", "Overall connection status", "Next required actions"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing readiness copy %q", want)
		}
	}
	if strings.Contains(body, "Polling") || strings.Contains(body, "Runtime") {
		t.Fatal("dashboard exposes implementation status")
	}
}

func TestSelectedProductionKeepsIdentifiersAdvancedAndUsesUserCopy(t *testing.T) {
	db := newIAViewDB(t)
	p := model.Project{KitsuProjectID: "selected-advanced-p", Name: "Selected Advanced P", DiscordGuildID: "synthetic-guild", DiscordCategoryID: "synthetic-category"}
	db.Create(&p)
	db.Create(&model.NotificationRoutingDiagnosis{ProductionID: "selected-advanced-p", Detail: "Synthetic stale destination"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=selected-advanced-p&lang=en", nil)
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	if strings.Contains(body, "synthetic-guild") {
		t.Fatal("raw Discord identifier leaked into the normal Overview tab")
	}
	advancedRequest := httptest.NewRequest("GET", "/bot/admin/projects?project=selected-advanced-p&tab=advanced&lang=en", nil)
	advancedWriter := httptest.NewRecorder()
	renderIAProductionList(advancedWriter, advancedRequest, db, "")
	if !strings.Contains(advancedWriter.Body.String(), "synthetic-guild") {
		t.Fatal("Advanced settings did not expose the technical identifier")
	}
	troubleshootingRequest := httptest.NewRequest("GET", "/bot/admin/projects?project=selected-advanced-p&tab=troubleshooting&lang=en", nil)
	troubleshootingWriter := httptest.NewRecorder()
	renderIAProductionList(troubleshootingWriter, troubleshootingRequest, db, "")
	troubleshootingBody := troubleshootingWriter.Body.String()
	for _, want := range []string{"Current problem", "Next action", "Diagnostic details"} {
		if !strings.Contains(troubleshootingBody, want) {
			t.Fatalf("troubleshooting missing %q", want)
		}
	}
}

func TestBotAndSystemStatusUseActualPrerequisiteValues(t *testing.T) {
	db := newIAViewDB(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin/health?lang=en", nil)
	renderIAHealth(w, r, db)
	body := w.Body.String()
	for _, want := range []string{"Not configured", "Bot state", "Notification state", "Overall problem", "Next required action"} {
		if !strings.Contains(body, want) {
			t.Fatalf("system status missing %q", want)
		}
	}
	w = httptest.NewRecorder()
	renderIABot(w, r, db)
	if !strings.Contains(w.Body.String(), "Action required") || !strings.Contains(w.Body.String(), "View channels") {
		t.Fatal("Bot Connection does not expose state and permission list")
	}
}
