package setup

import (
	"testing"

	"app/src/model"
)

func TestSummarizeForumResourcesCountsMissingForumsAndTags(t *testing.T) {
	report := ProjectDiscordResourceHealth{ForumConfigured: true}
	settings := model.ProductionForumSettings{
		Routes: []model.ProductionForumRoute{
			{TaskTypeID: "tt1", ForumChannelID: "forum-1"},
			{TaskTypeID: "tt2", ForumChannelID: "forum-1"},
			{TaskTypeID: "tt3", ForumChannelID: "forum-gone"},
		},
		Tags: []model.ProductionForumTag{
			{ForumChannelID: "forum-1", Role: model.ForumRoleWFA, TagID: "tag-wfa"},
			{ForumChannelID: "forum-1", Role: model.ForumRoleDone, TagID: "tag-removed"},
			{ForumChannelID: "forum-gone", Role: model.ForumRoleWFA, TagID: "tag-gone"},
		},
	}
	forums := []forumChannelInfo{
		{ID: "forum-1", Tags: []forumTagInfo{{ID: "tag-wfa", Name: "WFA"}}},
	}

	summarizeForumResources(&report, settings, forums)

	if report.RoutedForumCount != 2 || report.MissingForumCount != 1 {
		t.Fatalf("routed forums miscounted: %#v", report)
	}
	// The deleted forum's tag is not counted again; the removed tag on the live
	// forum is.
	if report.MissingTags != 1 {
		t.Fatalf("expected one missing tag, got %d", report.MissingTags)
	}

	report.GuildValid, report.ManageChannels = true, true
	if report.Healthy() || !report.HasStaleResources() {
		t.Fatalf("a missing forum must block health: %#v", report)
	}
}

func TestSummarizeForumResourcesHealthyWhenEverythingExists(t *testing.T) {
	report := ProjectDiscordResourceHealth{GuildValid: true, ManageChannels: true, ForumConfigured: true}
	settings := model.ProductionForumSettings{
		Routes: []model.ProductionForumRoute{{TaskTypeID: "tt1", ForumChannelID: "forum-1"}},
		Tags:   []model.ProductionForumTag{{ForumChannelID: "forum-1", Role: model.ForumRoleWFA, TagID: "tag-wfa"}},
	}
	forums := []forumChannelInfo{
		{ID: "forum-1", Tags: []forumTagInfo{{ID: "tag-wfa", Name: "WFA"}}},
	}

	summarizeForumResources(&report, settings, forums)

	if !report.Healthy() || report.HasStaleResources() {
		t.Fatalf("complete forum setup should be healthy: %#v", report)
	}
}

func TestHasStaleResourcesWithoutForumConfiguration(t *testing.T) {
	report := ProjectDiscordResourceHealth{GuildValid: true, ManageChannels: true}
	if report.Healthy() {
		t.Fatal("a Production without saved forum settings must not be healthy")
	}
	if !report.HasStaleResources() {
		t.Fatal("a Production without saved forum settings must be reported")
	}
}
