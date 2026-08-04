package model

import "testing"

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
