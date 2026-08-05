package setup

import (
	"app/src/model"
	"os"
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

func TestSharedBotRuntimeReadinessIsSharedBySetupAndSettings(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:shared-readiness?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Setting{}, &model.Project{}, &model.ProjectWebhook{}, &model.ProductionNotificationConfig{}, &model.ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	model.SetSetting(db, RuntimeKitsuEmailSettingKey, "manager@example.invalid")
	previous := os.Getenv(RuntimeKitsuPasswordEnv)
	defer os.Setenv(RuntimeKitsuPasswordEnv, previous)
	_ = os.Setenv(RuntimeKitsuPasswordEnv, "configured-in-test")
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", ""); got.OverallReady || got.DiscordConfigured {
		t.Fatalf("token-less shared readiness must be incomplete: %#v", got)
	}
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", "test-token"); got.OverallReady || !got.KitsuConfigured || !got.DiscordConfigured {
		t.Fatalf("connection prerequisites alone must remain blocked: %#v", got)
	}
	if err := model.CreateProject(db, "ready-production", "Ready Production", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := model.CreateProjectWebhook(db, "ready-production", "Alerts", "", "safe-webhook", "channel-1"); err != nil {
		t.Fatal(err)
	}
	webhook := model.ListProjectWebhooks(db, "ready-production")[0]
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: "ready-production", Enabled: true}, []model.ProductionNotificationRoute{{ProductionID: "ready-production", TaskTypeID: "task-type-1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	if got := sharedBotRuntimeReadiness(db, "https://kitsu.invalid", "test-token"); !got.OverallReady {
		t.Fatalf("valid Production routing should complete shared readiness: %#v", got)
	}
}
