package setup

import (
	"app/src/model"
	"testing"
)

func TestSummarizeProjectDiscordResourcesDetectsStaleAndDuplicateChannels(t *testing.T) {
	project := model.Project{KitsuProjectID: "p1", DiscordGuildID: "g1", DiscordCategoryID: "cat1"}
	webhooks := []model.ProjectWebhook{
		{DiscordChannelID: "live1"},
		{DiscordChannelID: "stale1"},
		{DiscordChannelID: "live1"},
	}
	channels := []DiscordGuildChannel{
		{ID: "cat1", Type: 4},
		{ID: "live1", Type: 0, ParentID: "cat1"},
	}
	report := summarizeProjectDiscordResources(project, webhooks, channels)
	if !report.CategoryPresent || report.SavedChannelCount != 3 || report.LiveSavedChannelCount != 2 || report.StaleChannelCount != 1 || report.DuplicateSavedChannelCount != 1 {
		t.Fatalf("unexpected resource report: %#v", report)
	}
	if report.Healthy() || !report.HasStaleResources() {
		t.Fatalf("stale resources must block health: %#v", report)
	}
}

func TestSummarizeProjectDiscordResourcesRequiresManagedCategoryParent(t *testing.T) {
	project := model.Project{KitsuProjectID: "p1", DiscordCategoryID: "cat1"}
	webhooks := []model.ProjectWebhook{{DiscordChannelID: "channel1"}}
	channels := []DiscordGuildChannel{
		{ID: "cat1", Type: 4},
		{ID: "channel1", Type: 0, ParentID: "other-category"},
	}
	report := summarizeProjectDiscordResources(project, webhooks, channels)
	if report.LiveSavedChannelCount != 0 || report.StaleChannelCount != 1 {
		t.Fatalf("channel outside managed category was not stale: %#v", report)
	}
}

func TestMergeDiscordStatusPreservesResourceHealth(t *testing.T) {
	report := ProjectDiscordResourceHealth{CategoryPresent: true, SavedChannelCount: 1, LiveSavedChannelCount: 1}
	merged := mergeDiscordStatus(report, DiscordStatusInfo{GuildValid: true, Permissions: DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}})
	if !merged.Healthy() {
		t.Fatalf("complete Discord status and live resources should be healthy: %#v", merged)
	}
}
