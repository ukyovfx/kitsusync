package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestActivateProductionRoutingFromMappingsPersistsNewModelAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:activate-routing?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProductionChannelMapping{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProject(db, "p1", "Production", "cg", "g1", "cat1", "en"); err != nil {
		t.Fatal(err)
	}
	if err := CreateProjectWebhook(db, "p1", "shot", "Animation", "https://example.invalid/webhook", "c1"); err != nil {
		t.Fatal(err)
	}
	rows := []ProductionChannelMapping{{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt1", TaskTypeName: "Animation", ChannelID: "c1", ChannelName: "shot", Active: true, MigrationState: "current"}}
	if err := ActivateProductionRoutingFromMappings(db, "p1", "g1", rows); err != nil {
		t.Fatal(err)
	}
	if err := ActivateProductionRoutingFromMappings(db, "p1", "g1", rows); err != nil {
		t.Fatal(err)
	}
	if got := len(ListProductionChannelMappings(db, "p1")); got != 1 {
		t.Fatalf("expected one stable mapping after retry, got %d", got)
	}
	if got := len(ListProductionNotificationRoutes(db, "p1")); got != 1 {
		t.Fatalf("expected one notification route after retry, got %d", got)
	}
	config := FindProductionNotificationConfig(db, "p1")
	if config == nil || !config.Enabled {
		t.Fatal("expected routing config to be active")
	}
	if issues := ValidateProductionNotificationConfig(db, "p1", ListProductionNotificationRoutes(db, "p1")); len(issues) != 0 {
		t.Fatalf("expected active routing to validate, got %#v", issues)
	}
}

func TestActivateProductionRoutingFromMappingsFailsClosedWithoutLegacyDestination(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:activate-routing-blocked?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Project{}, &ProjectWebhook{}, &ProductionChannelMapping{}, &ProductionNotificationConfig{}, &ProductionNotificationRoute{}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProject(db, "p1", "Production", "cg", "g1", "cat1", "en"); err != nil {
		t.Fatal(err)
	}
	rows := []ProductionChannelMapping{{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt1", TaskTypeName: "Animation", ChannelID: "c1", ChannelName: "shot", Active: true, MigrationState: "current"}}
	if err := ActivateProductionRoutingFromMappings(db, "p1", "g1", rows); err == nil {
		t.Fatal("expected activation to fail without a valid destination")
	}
	if FindProductionNotificationConfig(db, "p1") != nil || len(ListProductionNotificationRoutes(db, "p1")) != 0 || len(ListProductionChannelMappings(db, "p1")) != 0 {
		t.Fatal("blocked activation must not persist partial routing state")
	}
}
