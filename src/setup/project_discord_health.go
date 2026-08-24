package setup

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"app/src/model"
	"gorm.io/gorm"
	"net/http"
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
	Error                      string
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
	channels, err := ListGuildChannels(report.GuildID, token)
	if err != nil {
		report.Error = "project Discord channels could not be read"
		return report
	}
	return mergeDiscordStatus(summarizeProjectDiscordResources(project, model.ListProjectWebhooks(db, project.KitsuProjectID), channels), status)
}

func renderProjectDiscordResourceNotice(db *gorm.DB, r *http.Request, project model.Project, lang string) string {
	report := inspectProjectDiscordResources(project, db)
	if report.Error == "" && !report.HasStaleResources() {
		return ""
	}
	detail := report.Error
	if detail == "" {
		detail = fmt.Sprintf(t(lang, "保存channel %d件のうち %d件を確認できません。category=%t、重複=%d件です。", "%d saved channel(s) have not been verified; category=%t, duplicates=%d."), report.SavedChannelCount, report.LiveSavedChannelCount, report.CategoryPresent, report.DuplicateSavedChannelCount)
	}
	fix := t(lang, "既存のDiscord Guildを再検証し、不足channelだけを確認して修復してください。", "Revalidate the linked Discord Guild and explicitly repair only missing channels.")
	repairURL := ""
	if strings.TrimSpace(project.DiscordGuildID) != "" {
		repairURL = withLang("/bot/setup?wizard_step=4&repair=1&project="+url.QueryEscape(project.KitsuProjectID)+"&plan_guild="+url.QueryEscape(project.DiscordGuildID), r)
	}
	action := ""
	if repairURL != "" {
		action = `<a class="btn-ghost" href="` + html.EscapeString(repairURL) + `">` + html.EscapeString(t(lang, "修復計画を確認", "Review repair plan")) + `</a>`
	}
	return `<div class="routing-resource-warning" role="alert" style="padding:12px 14px;border:1px solid rgba(255,200,80,.38);border-radius:14px;background:rgba(255,200,80,.08);margin-bottom:14px"><strong>` + html.EscapeString(t(lang, "Discordリソースを確認できません", "Discord resources need review")) + `</strong><p style="margin:6px 0">` + html.EscapeString(detail) + `</p><small>` + html.EscapeString(fix) + `</small><div style="margin-top:10px">` + action + `</div></div>`
}
