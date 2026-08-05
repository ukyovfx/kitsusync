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
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionChannelMapping{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}, &model.AuditLog{}, &model.UserMap{}); err != nil {
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
	for _, want := range []string{"Overview", "Notifications", "User settings", "Storage settings", "Activity", "Troubleshooting", "Advanced settings", "Danger Zone", "Notification state and testing"} {
		if !strings.Contains(body, want) {
			t.Fatalf("selected Production view missing %q", want)
		}
	}
	if strings.Contains(body, "<details class=\"accordion\"") {
		t.Fatal("selected Production view still uses the legacy accordion")
	}
	if strings.Contains(body, "name=\"guild_id\"") {
		t.Fatal("selected Production view exposes manual server ID editing")
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
	db.Create(&model.Project{KitsuProjectID: "p", Name: "P", DiscordGuildID: "g", DiscordCategoryID: "c"})
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=p&lang=ja", nil)
	w := httptest.NewRecorder()
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	if !strings.Contains(body, "<details class=\"advanced-details\"") || !strings.Contains(body, "class=\"advanced-details danger-zone\"") {
		t.Fatal("advanced details and Danger Zone are not disclosure sections")
	}
	if strings.Contains(body, "Polling") || strings.Contains(body, "SQLite") || strings.Contains(body, "Stable ID") {
		t.Fatal("technical implementation terms leaked into the normal selected Production view")
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
	p := model.Project{KitsuProjectID: "p", Name: "P", DiscordGuildID: "synthetic-guild", DiscordCategoryID: "synthetic-category"}
	db.Create(&p)
	db.Create(&model.NotificationRoutingDiagnosis{ProductionID: "p", Detail: "Synthetic stale destination"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/bot/admin/projects?project=p&lang=en", nil)
	renderIAProductionList(w, r, db, "")
	body := w.Body.String()
	advanced := strings.Index(body, "<details class=\"advanced-details\">")
	if advanced < 0 || strings.Index(body[:advanced], "synthetic-guild") >= 0 {
		t.Fatal("raw Discord identifier leaked before Advanced settings")
	}
	for _, want := range []string{"Current problem", "Cause", "Next action", "Diagnostic details"} {
		if !strings.Contains(body, want) {
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
