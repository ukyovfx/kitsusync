package setup

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	plan := BuildTaskTypeChannelPlan(projectID, guildID, taskTypes, existingChannelsForPlanWithLegacy(channels, model.ListProductionChannelMappings(db, projectID), model.ListProjectWebhooks(db, projectID)))
	if plan.Fingerprint() != strings.TrimSpace(r.FormValue("plan_fingerprint")) {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.stale"), redirect)
		return true
	}
	if !plan.Valid() {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.invalid"), redirect)
		return true
	}
	operationID := fmt.Sprintf("channel-plan-%d", time.Now().UnixNano())
	rows := make([]model.ProductionChannelMapping, 0, len(plan.Entries))
	created := 0
	for _, entry := range plan.Entries {
		channelID := strings.TrimSpace(entry.ExistingID)
		if entry.Action == "create" {
			var createErr error
			channelID, createErr = CreateTextChannel(guildID, strings.TrimSpace(project.DiscordCategoryID), entry.ChannelName, botToken)
			if createErr != nil {
				markPendingChannelPlanFailure(db, rows, operationID, createErr.Error())
				renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.partial", created), redirect)
				return true
			}
			created++
		}
		if channelID == "" {
			markPendingChannelPlanFailure(db, rows, operationID, "Discord channel ID was empty")
			renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.partial", created), redirect)
			return true
		}
		webhookURL, webhookID, destinationErr := ensureProductionChannelWebhook(db, projectID, channelID, entry.ChannelName, botToken)
		_ = webhookURL
		if destinationErr != nil || webhookID == 0 {
			markPendingChannelPlanFailure(db, rows, operationID, destinationErrString(destinationErr))
			renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.partial", created), redirect)
			return true
		}
		row := model.ProductionChannelMapping{ProductionID: projectID, GuildID: guildID, TaskTypeID: entry.TaskTypeID, TaskTypeName: entry.TaskTypeName, ChannelID: channelID, ChannelName: entry.ChannelName, OperationID: operationID, Active: false, State: model.ChannelMappingStatePending, MigrationState: model.ChannelMappingStatePending}
		if err := model.SavePendingProductionChannelMapping(db, row); err != nil {
			markPendingChannelPlanFailure(db, rows, operationID, err.Error())
			renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.partial", created), redirect)
			return true
		}
		row.Active = true
		row.State = model.ChannelMappingStateCurrent
		row.MigrationState = model.ChannelMappingStateCurrent
		rows = append(rows, row)
	}
	verified, verifyErr := ListGuildChannels(guildID, botToken)
	if verifyErr != nil || !mappingRowsMatchChannels(rows, verified) {
		markPendingChannelPlanFailure(db, rows, operationID, "Discord returned an incomplete verification result")
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.verify_failed"), redirect)
		return true
	}
	if err := model.ActivateProductionRoutingFromMappings(db, projectID, guildID, rows); err != nil {
		markPendingChannelPlanFailure(db, rows, operationID, err.Error())
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.routing_persist_failed"), redirect)
		return true
	}
	if r.URL.Path == "/bot/setup" {
		redirect = withLang("/bot/setup?wizard=complete&project="+url.QueryEscape(projectID), r)
	}
	renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.completed", created, len(rows)-created), redirect)
	return true
}

func destinationErrString(err error) string {
	if err == nil {
		return "notification destination was not created"
	}
	return err.Error()
}

func markPendingChannelPlanFailure(db *gorm.DB, rows []model.ProductionChannelMapping, operationID, reason string) {
	for _, row := range rows {
		var persisted model.ProductionChannelMapping
		if err := db.Where("production_id = ? AND task_type_id = ?", row.ProductionID, row.TaskTypeID).First(&persisted).Error; err == nil {
			row.ID = persisted.ID
			row.CreatedAt = persisted.CreatedAt
		}
		row.Active = false
		row.State = model.ChannelMappingStatePartial
		row.MigrationState = model.ChannelMappingStateReviewRequired
		row.OperationID = operationID
		row.LastError = reason
		_ = db.Save(&row).Error
	}
}

func ensureProductionChannelWebhook(db *gorm.DB, productionID, channelID, channelName, botToken string) (string, uint, error) {
	var selected *model.ProjectWebhook
	for _, candidate := range model.ListProjectWebhooks(db, productionID) {
		if strings.TrimSpace(candidate.DiscordChannelID) != strings.TrimSpace(channelID) {
			continue
		}
		if strings.TrimSpace(candidate.WebhookURL) == "" {
			return "", 0, fmt.Errorf("existing notification destination is incomplete")
		}
		if selected != nil && strings.TrimSpace(selected.WebhookURL) != strings.TrimSpace(candidate.WebhookURL) {
			return "", 0, fmt.Errorf("notification destination ownership is ambiguous")
		}
		copy := candidate
		selected = &copy
	}
	if selected != nil {
		return selected.WebhookURL, selected.ID, nil
	}
	webhookURL, err := CreateWebhook(channelID, channelName, botToken)
	if err != nil {
		return "", 0, err
	}
	if err := model.CreateProjectWebhook(db, productionID, channelName, "", webhookURL, channelID); err != nil {
		return "", 0, err
	}
	created := model.ListProjectWebhooks(db, productionID)
	for i := len(created) - 1; i >= 0; i-- {
		if created[i].DiscordChannelID == channelID && created[i].WebhookURL == webhookURL {
			return webhookURL, created[i].ID, nil
		}
	}
	return "", 0, fmt.Errorf("saved notification destination could not be found")
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
