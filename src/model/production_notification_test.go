package model

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProductionNotificationConfigIsolatedByProductionID(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}, &NotificationRoutingDiagnosis{}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProject(db, "p1", "Same Name", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateProject(db, "p2", "Same Name", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateProjectWebhook(db, "p1", "A", "", "https://example.invalid/a", "11111111111111111"); err != nil {
		t.Fatal(err)
	}
	webhook := ListProjectWebhooks(db, "p1")[0]
	routes := []ProductionNotificationRoute{{ProductionID: "p1", TaskTypeID: "tt1", TaskTypeName: "Same Name", DestinationWebhookID: webhook.ID}}
	if issues := ValidateProductionNotificationConfig(db, "p1", routes); len(issues) != 0 {
		t.Fatalf("unexpected validation issues: %v", issues)
	}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: "p1", ProductionName: "Same Name", Enabled: true}, routes); err != nil {
		t.Fatal(err)
	}
	if FindProductionNotificationConfig(db, "p2") != nil {
		t.Fatal("configuration leaked to same-name Production")
	}
}

func TestIncompleteConfigurationDoesNotActivate(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if issues := ValidateProductionNotificationConfig(db, "p1", nil); len(issues) == 0 {
		t.Fatal("expected incomplete configuration to be rejected")
	}
}

func TestPauseAndResumeAndStaleDestination(t *testing.T) {
	db := newRoutingTestDB(t)
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateProjectWebhook(db, "p1", "A", "", "https://example.invalid/a", "11111111111111111"); err != nil {
		t.Fatal(err)
	}
	webhook := ListProjectWebhooks(db, "p1")[0]
	routes := []ProductionNotificationRoute{{ProductionID: "p1", TaskTypeID: "tt1", DestinationWebhookID: webhook.ID}}
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: "p1", Enabled: true}, routes); err != nil {
		t.Fatal(err)
	}
	config := FindProductionNotificationConfig(db, "p1")
	config.Enabled = false
	if err := db.Save(config).Error; err != nil {
		t.Fatal(err)
	}
	if FindProductionNotificationConfig(db, "p1").Enabled {
		t.Fatal("expected pause")
	}
	config.Enabled = true
	if err := db.Save(config).Error; err != nil {
		t.Fatal(err)
	}
	if !FindProductionNotificationConfig(db, "p1").Enabled {
		t.Fatal("expected resume")
	}
	if err := db.Delete(&ProjectWebhook{}, webhook.ID).Error; err != nil {
		t.Fatal(err)
	}
	if issues := ValidateProductionNotificationConfig(db, "p1", routes); len(issues) == 0 {
		t.Fatal("expected stale destination diagnosis")
	}
}

func TestProductionNotificationConfigPersistsAcrossDatabaseRestart(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "routing.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProject(db, "p1", "P", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := CreateProjectWebhook(db, "p1", "A", "", "https://example.invalid/a", "11111111111111111"); err != nil {
		t.Fatal(err)
	}
	webhook := ListProjectWebhooks(db, "p1")[0]
	if err := SaveProductionNotificationConfig(db, &ProductionNotificationConfig{ProductionID: "p1", ProductionName: "P", Enabled: true}, []ProductionNotificationRoute{{TaskTypeID: "tt1", DestinationWebhookID: webhook.ID}}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	restarted, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	config := FindProductionNotificationConfig(restarted, "p1")
	if config == nil || !config.Enabled || len(ListProductionNotificationRoutes(restarted, "p1")) != 1 {
		t.Fatal("routing configuration did not persist across restart")
	}
}
