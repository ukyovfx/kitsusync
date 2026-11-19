package setup

import (
	"strings"

	"app/src/model"
	"gorm.io/gorm"
)

// ProjectDiscordResourceHealth is a read-only ownership report for one
// connected Production. A channel ID is stale when it is missing from the
// project guild or is no longer a text child of the managed category.
type ProjectDiscordResourceHealth struct {
	ProjectID                  string
	GuildID                    string
	GuildValid                 bool
	ManageChannels             bool
	ManageWebhooks             bool
	CategoryPresent            bool
	SavedChannelCount          int
	LiveSavedChannelCount      int
	StaleChannelCount          int
	DuplicateSavedChannelCount int
	// ChannelsStale means the report was summarized from a cached channel list
	// past its TTL whose refresh failed; the display uses it to warn instead
	// of failing the production.
	ChannelsStale      bool
	ChannelsRateLimited bool
	Error              string
}

func (h ProjectDiscordResourceHealth) Healthy() bool {
	return h.GuildValid && h.ManageChannels && h.ManageWebhooks &&
		h.CategoryPresent && h.StaleChannelCount == 0 && h.DuplicateSavedChannelCount == 0
}

func (h ProjectDiscordResourceHealth) HasStaleResources() bool {
	return !h.GuildValid || !h.CategoryPresent || h.StaleChannelCount > 0 || h.DuplicateSavedChannelCount > 0
}

func summarizeProjectDiscordResources(project model.Project, webhooks []model.ProjectWebhook, channels []DiscordGuildChannel) ProjectDiscordResourceHealth {
	report := ProjectDiscordResourceHealth{ProjectID: project.KitsuProjectID, GuildID: strings.TrimSpace(project.DiscordGuildID)}
	categoryID := strings.TrimSpace(project.DiscordCategoryID)
	byID := make(map[string]DiscordGuildChannel, len(channels))
	for _, channel := range channels {
		id := strings.TrimSpace(channel.ID)
		if id == "" {
			continue
		}
		byID[id] = channel
		if id == categoryID && channel.Type == 4 {
			report.CategoryPresent = true
		}
	}
	seen := map[string]bool{}
	for _, webhook := range webhooks {
		id := strings.TrimSpace(webhook.DiscordChannelID)
		if id == "" {
			continue
		}
		report.SavedChannelCount++
		if seen[id] {
			report.DuplicateSavedChannelCount++
		} else {
			seen[id] = true
		}
		channel, ok := byID[id]
		if !ok || channel.Type != 0 || strings.TrimSpace(channel.ParentID) != categoryID {
			report.StaleChannelCount++
			continue
		}
		report.LiveSavedChannelCount++
	}
	return report
}

func mergeDiscordStatus(report ProjectDiscordResourceHealth, status DiscordStatusInfo) ProjectDiscordResourceHealth {
	report.GuildValid = status.GuildValid
	report.ManageChannels = status.Permissions.ManageChannels
	report.ManageWebhooks = status.Permissions.ManageWebhooks
	return report
}

// inspectProjectDiscordResources performs only GET requests. It intentionally
// uses the Production's saved guild and never falls back to a global guild.
func inspectProjectDiscordResources(project model.Project, db *gorm.DB) ProjectDiscordResourceHealth {
	report := ProjectDiscordResourceHealth{ProjectID: project.KitsuProjectID, GuildID: strings.TrimSpace(project.DiscordGuildID)}
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
	report.ManageWebhooks = status.Permissions.ManageWebhooks
	if !status.GuildValid {
		report.Error = "project Discord guild is not accessible"
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
	channels, decodeErr := decodeGuildChannelsBody(view.body)
	if decodeErr != nil {
		report.Error = "project Discord channels could not be read"
		return report
	}
	report = mergeDiscordStatus(summarizeProjectDiscordResources(project, model.ListProjectWebhooks(db, project.KitsuProjectID), channels), status)
	if !view.fresh && view.fetchFailed {
		report.ChannelsStale = true
		report.ChannelsRateLimited = view.rateLimited
	}
	return report
}
