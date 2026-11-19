package setup

import (
	"fmt"
	"net/url"
	"strings"

	"app/src/model"
	"gorm.io/gorm"
	"net/http"
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

func renderProjectDiscordResourceNotice(db *gorm.DB, r *http.Request, project model.Project, lang string) string {
	report := inspectProjectDiscordResources(project, db)
	if report.Error == "" && !report.HasStaleResources() {
		return ""
	}
	detail := report.Error
	if detail == "" {
		switch {
		case !report.ForumConfigured:
			detail = t(lang, "このProductionのフォーラム設定が未保存です。", "This Production has no saved forum settings.")
		case len(report.ForumProblems) > 0:
			detail = fmt.Sprintf(t(lang, "フォーラム設定に%d件の問題があります。", "The forum settings have %d problem(s)."), len(report.ForumProblems))
		default:
			detail = fmt.Sprintf(t(lang, "フォーラム%d件中%d件をDiscord側で確認できません。タグ不一致%d件です。", "%d of %d routed forum(s) were not found in Discord; %d tag mismatch(es)."), report.MissingForumCount, report.RoutedForumCount, report.MissingTags)
		}
	}
	fix := t(lang, "フォーラム設定を開き、対象Guildのフォーラムとタグを確認して保存し直してください。", "Open the forum settings, confirm the routed forums and tags in that guild, and save again.")
	settingsURL := ""
	if strings.TrimSpace(project.KitsuProjectID) != "" {
		settingsURL = withLang("/bot/admin/forum?project="+url.QueryEscape(project.KitsuProjectID), r)
	}
	action := ""
	if settingsURL != "" {
		action = `<a class="btn-ghost" href="` + esc(settingsURL) + `">` + esc(t(lang, "フォーラム設定を開く", "Open forum settings")) + `</a>`
	}
	return `<div class="routing-resource-warning" role="alert" style="padding:12px 14px;border:1px solid rgba(255,200,80,.38);border-radius:14px;background:rgba(255,200,80,.08);margin-bottom:14px"><strong>` + esc(t(lang, "Discordリソースを確認できません", "Discord resources need review")) + `</strong><p style="margin:6px 0">` + esc(detail) + `</p><small>` + esc(fix) + `</small><div style="margin-top:10px">` + action + `</div></div>`
}
