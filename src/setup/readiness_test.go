package setup

import (
	"app/src/model"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHasReadyProductionRoutingRequiresEnabledValidRoute(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:readiness-routing?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p1", "Same name", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProject(db, "p2", "Same name", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if hasReadyProductionRouting(db) {
		t.Fatal("connected Productions alone must not be ready")
	}
	if err := model.CreateProjectWebhook(db, "p1", "Alerts", "", "redacted-webhook", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "p1")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p1", Enabled: false}, []model.ProductionNotificationRoute{{ProductionID: "p1", TaskTypeID: "tt1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	if hasReadyProductionRouting(db) {
		t.Fatal("paused routing must not be ready")
	}
	config := model.FindProductionNotificationConfig(db, "p1")
	config.Enabled = true
	if err := db.Save(config).Error; err != nil {
		t.Fatal(err)
	}
	if !hasReadyProductionRouting(db) {
		t.Fatal("valid enabled routing should be ready")
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "p2", Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: "p2", TaskTypeID: "tt1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	if len(model.ValidateProductionNotificationConfig(db, "p2", model.ListProductionNotificationRoutes(db, "p2"))) == 0 {
		t.Fatal("cross-Production destination must be stale")
	}
}
