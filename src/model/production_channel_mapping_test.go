package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newProductionMappingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestValidateProductionChannelMappingsRejectsCrossGuildAndStaleRows(t *testing.T) {
	issues := ValidateProductionChannelMappings("p1", "g1", []ProductionChannelMapping{
		{ProductionID: "p1", GuildID: "g2", TaskTypeID: "tt1", ChannelID: "c1", Active: true, MigrationState: "current"},
		{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt2", ChannelID: "c2", Active: true, MigrationState: "migration-required"},
	})
	if len(issues) != 2 {
		t.Fatalf("expected cross-guild and migration issues, got %#v", issues)
	}
}

func TestValidateProductionChannelMappingsUsesStableIDsAndRejectsDuplicates(t *testing.T) {
	issues := ValidateProductionChannelMappings("p1", "g1", []ProductionChannelMapping{
		{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt1", TaskTypeName: "Same name", ChannelID: "c1", Active: true, MigrationState: "current"},
		{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt1", TaskTypeName: "Different display name", ChannelID: "c1", Active: true, MigrationState: "current"},
	})
	if len(issues) != 1 {
		t.Fatalf("expected one duplicate issue, got %#v", issues)
	}
}

func TestProductionChannelMappingsPersistByProductionAndTaskTypeID(t *testing.T) {
	db := newProductionMappingTestDB(t)
	if err := db.AutoMigrate(&ProductionChannelMapping{}); err != nil {
		t.Fatal(err)
	}
	rows := []ProductionChannelMapping{{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt1", TaskTypeName: "Same", ChannelID: "c1", ChannelName: "same", Active: true, MigrationState: "current"}}
	if err := SaveProductionChannelMappings(db, "p1", "g1", rows); err != nil {
		t.Fatal(err)
	}
	if got := ListProductionChannelMappings(db, "p2"); len(got) != 0 {
		t.Fatalf("mapping leaked across Productions: %#v", got)
	}
	got := ListProductionChannelMappings(db, "p1")
	if len(got) != 1 || got[0].TaskTypeID != "tt1" || got[0].ChannelID != "c1" {
		t.Fatalf("unexpected persisted mapping: %#v", got)
	}
}

func TestPendingProductionChannelMappingIsInactiveAndRetryable(t *testing.T) {
	db := newProductionMappingTestDB(t)
	if err := db.AutoMigrate(&ProductionChannelMapping{}); err != nil {
		t.Fatal(err)
	}
	row := ProductionChannelMapping{ProductionID: "p1", GuildID: "g1", TaskTypeID: "tt1", ChannelID: "c1", OperationID: "op-1"}
	if err := SavePendingProductionChannelMapping(db, row); err != nil {
		t.Fatal(err)
	}
	var got ProductionChannelMapping
	if err := db.Where("production_id = ? AND task_type_id = ?", "p1", "tt1").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.Active || got.State != ChannelMappingStatePending || got.MigrationState != ChannelMappingStatePending || got.OperationID != "op-1" {
		t.Fatalf("pending mapping was not safely persisted: %#v", got)
	}
}
