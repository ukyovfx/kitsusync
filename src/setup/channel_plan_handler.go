package setup

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

func handleTaskTypeChannelPlanMutation(w http.ResponseWriter, r *http.Request, lang, botToken string, db *gorm.DB) bool {
	if r.Method != http.MethodPost || strings.TrimSpace(r.FormValue("action")) != "confirm_task_type_channels" {
		return false
	}
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	guildID := strings.TrimSpace(r.FormValue("guild_id"))
	redirect := withLang("/bot/admin/projects?project="+url.QueryEscape(projectID), r)
	if projectID == "" || guildID == "" || strings.TrimSpace(r.FormValue("confirm_plan")) != "yes" {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.no_confirmation"), redirect)
		return true
	}
	project := model.FindProjectByKitsuID(db, projectID)
	if project == nil {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.production_missing"), redirect)
		return true
	}
	taskTypes := kitsu.GetTaskTypes().Each
	channels, err := ListGuildChannels(guildID, botToken)
	if err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.guild_revalidate_failed"), redirect)
		return true
	}
	plan := BuildTaskTypeChannelPlan(projectID, guildID, taskTypes, existingChannelsForPlan(channels, model.ListProductionChannelMappings(db, projectID)))
	if plan.Fingerprint() != strings.TrimSpace(r.FormValue("plan_fingerprint")) {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.stale"), redirect)
		return true
	}
	if !plan.Valid() {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.invalid"), redirect)
		return true
	}
	rows, created, err := applyTaskTypeChannelPlan(plan, func(guild, name string) (string, error) {
		return CreateTextChannel(guild, strings.TrimSpace(project.DiscordCategoryID), name, botToken)
	})
	if err != nil {
		// Successful rows are intentionally not activated or persisted as current on partial failure.
		renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.partial", created), redirect)
		return true
	}
	verified, verifyErr := ListGuildChannels(guildID, botToken)
	if verifyErr != nil || !mappingRowsMatchChannels(rows, verified) {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.verify_failed"), redirect)
		return true
	}
	if err := model.SaveProductionChannelMappings(db, projectID, guildID, rows); err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.persist_failed"), redirect)
		return true
	}
	if err := model.UpdateProjectGuildID(db, projectID, guildID); err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.guild_save_failed"), redirect)
		return true
	}
	renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.completed", created, len(rows)-created), redirect)
	return true
}

func mappingRowsMatchChannels(rows []model.ProductionChannelMapping, channels []DiscordGuildChannel) bool {
	byID := map[string]DiscordGuildChannel{}
	for _, channel := range channels {
		byID[strings.TrimSpace(channel.ID)] = channel
	}
	for _, row := range rows {
		channel, ok := byID[strings.TrimSpace(row.ChannelID)]
		if !ok || channel.Type != 0 || strings.TrimSpace(channel.Name) != strings.TrimSpace(row.ChannelName) {
			return false
		}
	}
	return len(rows) > 0
}

func renderTaskTypeChannelPlanResult(w http.ResponseWriter, r *http.Request, lang, message, redirect string) {
	body := `<div class="section-stack"><section class="section-card glass" role="status"><h2>` + esc(tr(lang, "channel_plan.result_title")) + `</h2><p>` + html.EscapeString(message) + `</p><a class="btn" href="` + html.EscapeString(redirect) + `">` + esc(tr(lang, "channel_plan.review")) + `</a></section></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "channel_plan.result_title"), r, body))
}
