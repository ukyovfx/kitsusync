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
	for _, want := range []string{"Select a Kitsu Production", "Select a Discord server", "Review channels to create or reuse", "Confirm the exact plan before execution"} {
		if !strings.Contains(setup, want) {
			t.Fatalf("new connection flow missing %q", want)
		}
	}
	if strings.Contains(setup, "Guild ID") || strings.Contains(setup, "Project Routing") {
		t.Fatal("new connection flow exposes implementation terms")
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
