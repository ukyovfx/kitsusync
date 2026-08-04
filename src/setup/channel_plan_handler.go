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
		renderTaskTypeChannelPlanResult(w, r, lang, "Confirmation was not recorded. No Discord write occurred.", redirect)
		return true
	}
	project := model.FindProjectByKitsuID(db, projectID)
	if project == nil {
		renderTaskTypeChannelPlanResult(w, r, lang, "The selected Production is no longer connected. No Discord write occurred.", redirect)
		return true
	}
	taskTypes := kitsu.GetTaskTypes().Each
	channels, err := ListGuildChannels(guildID, botToken)
	if err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, "The selected Guild could not be revalidated. No Discord write occurred.", redirect)
		return true
	}
	plan := BuildTaskTypeChannelPlan(projectID, guildID, taskTypes, existingChannelsForPlan(channels, model.ListProductionChannelMappings(db, projectID)))
	if plan.Fingerprint() != strings.TrimSpace(r.FormValue("plan_fingerprint")) {
		renderTaskTypeChannelPlanResult(w, r, lang, "The plan is stale because Production, Task Types, or Guild channels changed. Review the new plan before confirming again. No Discord write occurred.", redirect)
		return true
	}
	if !plan.Valid() {
		renderTaskTypeChannelPlanResult(w, r, lang, "The plan is blocked by a naming, ownership, or stale-reference issue. No Discord write occurred.", redirect)
		return true
	}
	rows, created, err := applyTaskTypeChannelPlan(plan, func(guild, name string) (string, error) {
		return CreateTextChannel(guild, strings.TrimSpace(project.DiscordCategoryID), name, botToken)
	})
	if err != nil {
		// Successful rows are intentionally not activated or persisted as current on partial failure.
		renderTaskTypeChannelPlanResult(w, r, lang, fmt.Sprintf("Channel creation stopped after %d write(s). Routing remains inactive; review Discord and retry the refreshed plan.", created), redirect)
		return true
	}
	verified, verifyErr := ListGuildChannels(guildID, botToken)
	if verifyErr != nil || !mappingRowsMatchChannels(rows, verified) {
		renderTaskTypeChannelPlanResult(w, r, lang, "Discord did not return a complete verified result. Routing remains inactive and no mappings were saved.", redirect)
		return true
	}
	if err := model.SaveProductionChannelMappings(db, projectID, guildID, rows); err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, "The verified Discord result could not be persisted. Routing remains inactive.", redirect)
		return true
	}
	if err := model.UpdateProjectGuildID(db, projectID, guildID); err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, "Mappings were verified, but the Production Guild assignment could not be saved. Review the result before retrying.", redirect)
		return true
	}
	renderTaskTypeChannelPlanResult(w, r, lang, fmt.Sprintf("Channel plan completed: %d missing text channel(s) created and %d exact channel(s) reused. Routing mappings are active.", created, len(rows)-created), redirect)
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
	body := `<div class="section-stack"><section class="section-card glass" role="status"><h2>Task Type channel plan</h2><p>` + html.EscapeString(message) + `</p><a class="btn" href="` + html.EscapeString(redirect) + `">Review Connected Productions</a></section></div>`
	fmt.Fprint(w, adminPage(lang, "Task Type channel plan", r, body))
}
