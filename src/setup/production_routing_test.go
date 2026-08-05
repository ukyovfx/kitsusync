package setup

import (
	"app/src/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProductionRoutingCompatibilityHandlerRedirectsToConnectedProductions(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bot/admin/production-routing?project=p1&lang=en", nil)
	res := httptest.NewRecorder()
	ProductionRoutingCompatibilityHandler().ServeHTTP(res, req)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("expected compatibility redirect, got %d", res.Code)
	}
	if got := res.Header().Get("Location"); !strings.Contains(got, "/bot/admin/projects") || !strings.Contains(got, "project=p1") {
		t.Fatalf("unexpected compatibility location: %s", got)
	}
}

func TestProductionRoutingHandlerDoesNotRenderWebhookURL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:production-routing-ui?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "Alerts", "", "https://example.invalid/secret-value", "11111111111111111"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/bot/admin/production-routing?project=p1&lang=en", nil)
	res := httptest.NewRecorder()
	ProductionRoutingHandler(db)(res, req)
	if strings.Contains(res.Body.String(), "secret-value") {
		t.Fatal("webhook URL was rendered")
	}
	if !strings.Contains(res.Body.String(), "Production Notification Routing") {
		t.Fatal("routing page was not rendered")
	}
}

func TestDryRunActionIsLocalAndRedactsDestinationURL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:production-routing-dry-run?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}, &model.NotificationRoutingDiagnosis{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "p1", "Alerts", "", "https://example.invalid/secret-value", "11111111111111111"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, []model.ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	form := strings.NewReader("action=dry_run&production_id=p1&dry_run_task_type_id=tt1")
	req := httptest.NewRequest(http.MethodPost, "/bot/admin/production-routing?lang=en", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	ProductionRoutingHandler(db)(res, req)
	body := res.Body.String()
	if !strings.Contains(body, "Check without sending completed") || strings.Contains(body, "skip reason") || strings.Contains(body, "Production ID: p1") {
		t.Fatal("dry-run result was not rendered")
	}
	if strings.Contains(body, "secret-value") {
		t.Fatal("dry-run rendered webhook URL")
	}
}
