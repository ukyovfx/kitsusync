package setup

import (
	"strings"

	"app/src/model"
	"gorm.io/gorm"
)

// ProjectDiscordResourceHealth is a read-only report on one connected
// Production's forum setup against the live Discord guild. It never reads
// project_webhooks: forum routing is the only delivery path.
type ProjectDiscordResourceHealth struct {
	ProjectID      string
	GuildID        string
	GuildValid     bool
	ManageChannels bool
	// ForumConfigured means the Production has a saved forum configuration row.
	ForumConfigured bool
	// ForumProblems are the saved configuration's own inconsistencies.
	ForumProblems []model.ForumSettingsProblem
	// RoutedForumCount and MissingForumCount count the forums the routes point
	// at that the guild channel list knows / does not know.
	RoutedForumCount  int
	MissingForumCount int
	// MissingTags counts role tags the saved mapping expects that the live
	// forum no longer offers.
	MissingTags int
	// ChannelsStale means the report was summarized from a cached channel list
	// past its TTL whose refresh failed; the display uses it to warn instead
	// of failing the production.
	ChannelsStale       bool
	ChannelsRateLimited bool
	Error               string
}

func (h ProjectDiscordResourceHealth) Healthy() bool {
	return h.GuildValid && h.ManageChannels && h.ForumConfigured &&
		len(h.ForumProblems) == 0 && h.RoutedForumCount > 0 &&
		h.MissingForumCount == 0 && h.MissingTags == 0
}

func (h ProjectDiscordResourceHealth) HasStaleResources() bool {
	return !h.GuildValid || !h.ForumConfigured || len(h.ForumProblems) > 0 ||
		h.RoutedForumCount == 0 || h.MissingForumCount > 0 || h.MissingTags > 0
}

// inspectProjectDiscordResources performs only GET requests. It checks the
// Production's saved forum configuration against the guild's live forums and
// tags, falling back to the Production's saved Discord Guild ID when the forum
// configuration does not name one.
func inspectProjectDiscordResources(project model.Project, db *gorm.DB) ProjectDiscordResourceHealth {
	settings, ok, err := model.LoadProductionForumSettings(db, project.KitsuProjectID)
	report := ProjectDiscordResourceHealth{
		ProjectID:       project.KitsuProjectID,
		GuildID:         strings.TrimSpace(project.DiscordGuildID),
		ForumConfigured: ok && err == nil,
	}
	if report.ForumConfigured {
		report.GuildID = forumGuildID(settings, report.GuildID)
		report.ForumProblems = model.ForumSettingsProblems(settings)
	}
	if report.GuildID == "" {
		report.Error = "project Discord guild is not configured"
		return report
	}
	token := storedRuntimeDiscordBotToken(db)
	if strings.TrimSpace(token) == "" {
		report.Error = "Discord bot token is not configured"
		return report
	}
	status := checkDiscordStatus(token, report.GuildID)
	report.GuildValid = status.GuildValid
	report.ManageChannels = status.Permissions.ManageChannels
	if !status.GuildValid {
		report.Error = "project Discord guild is not accessible"
		return report
	}
	if !report.ForumConfigured {
		return report
	}
	// The channel list comes from the cache, so a rate limit cannot block the
	// page: with cached data the report is summarized and flagged stale, and
	// only a cold cache reports the read failure.
	view := guildChannelListCache.view(report.GuildID, token)
	if !view.hasData() {
		report.Error = "project Discord channels could not be read"
		return report
	}
	forums, decodeErr := decodeGuildForumChannels(view.body)
	if decodeErr != nil {
		report.Error = "project Discord channels could not be read"
		return report
	}
	summarizeForumResources(&report, settings, forums)
	if !view.fresh && view.fetchFailed {
		report.ChannelsStale = true
		report.ChannelsRateLimited = view.rateLimited
	}
	return report
}

func forumGuildID(settings model.ProductionForumSettings, fallback string) string {
	if guildID := strings.TrimSpace(settings.Config.GuildID); guildID != "" {
		return guildID
	}
	return strings.TrimSpace(fallback)
}

// summarizeForumResources counts the routed forums and role tags that exist in
// the live guild. The saved mapping names the expected tags by role, so a forum
// that lost a tag (or was deleted) shows up as a stale resource.
func summarizeForumResources(report *ProjectDiscordResourceHealth, settings model.ProductionForumSettings, forums []forumChannelInfo) {
	byID := make(map[string]forumChannelInfo, len(forums))
	for _, forum := range forums {
		byID[strings.TrimSpace(forum.ID)] = forum
	}
	routed := map[string]struct{}{}
	for _, route := range settings.Routes {
		forumID := strings.TrimSpace(route.ForumChannelID)
		if forumID == "" {
			continue
		}
		if _, seen := routed[forumID]; seen {
			continue
		}
		routed[forumID] = struct{}{}
		report.RoutedForumCount++
		if _, exists := byID[forumID]; !exists {
			report.MissingForumCount++
		}
	}
	for _, tag := range settings.Tags {
		forum, exists := byID[strings.TrimSpace(tag.ForumChannelID)]
		if !exists {
			// A deleted forum is already counted once, not once per tag.
			continue
		}
		if !forumHasTagID(forum.Tags, strings.TrimSpace(tag.TagID)) {
			report.MissingTags++
		}
	}
}

func forumHasTagID(tags []forumTagInfo, tagID string) bool {
	if tagID == "" {
		return false
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag.ID) == tagID {
			return true
		}
	}
	return false
}
