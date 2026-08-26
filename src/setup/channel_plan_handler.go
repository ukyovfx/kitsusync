package setup

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	taskTypes := routingTaskTypesForProduction(projectID)
	taskTypes, overrides := taskTypePlanRequest(r, taskTypes)
	channels, err := ListGuildChannels(guildID, botToken)
	if err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.guild_revalidate_failed"), redirect)
		return true
	}
	categoryID := strings.TrimSpace(r.FormValue("category_id"))
	createCategory := categoryID == "__create__"
	requestedCategoryID := categoryID
	if categoryID == "" {
		categoryID = strings.TrimSpace(project.DiscordCategoryID)
	}
	createdCategory := false
	createdChannelIDs := []string{}
	createdWebhookIDs := []uint{}
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, channelID := range createdChannelIDs {
			_ = DeleteChannel(channelID, botToken)
		}
		for _, webhookID := range createdWebhookIDs {
			_ = db.Delete(&model.ProjectWebhook{}, webhookID).Error
		}
		if createdCategory {
			_ = DeleteChannel(categoryID, botToken)
		}
	}()
	if createCategory {
		categoryID, err = CreateCategory(guildID, KitsuSyncCategoryName(project.Name), botToken)
		if err != nil {
			renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.category_create_failed"), redirect)
			return true
		}
		createdCategory = true
	}
	if categoryID == "" || !createCategory && !discordCategoryExists(channels, categoryID) {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.category_required"), redirect)
		return true
	}
	categoryChannels := channelsInCategory(channels, categoryID)
	projectWebhooks := liveProjectWebhooksForCategory(model.ListProjectWebhooks(db, projectID), categoryChannels)
	projectMappings := liveProjectMappingsForCategory(model.ListProductionChannelMappings(db, projectID), categoryChannels)
	existing := existingChannelsForPlanWithLegacy(categoryChannels, projectMappings, projectWebhooks)
	plan := BuildTaskTypeChannelPlanWithOverrides(projectID, guildID, taskTypes, existing, overrides)
	plan.CategoryID = requestedCategoryID
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
			channelID, createErr = CreateTextChannel(guildID, categoryID, entry.ChannelName, botToken)
			if createErr != nil {
				markPendingChannelPlanFailure(db, rows, operationID, createErr.Error())
				renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.partial", created), redirect)
				return true
			}
			createdChannelIDs = append(createdChannelIDs, channelID)
			created++
		}
		if channelID == "" {
			markPendingChannelPlanFailure(db, rows, operationID, "Discord channel ID was empty")
			renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.partial", created), redirect)
			return true
		}
		webhookURL, webhookID, createdWebhook, destinationErr := ensureProductionChannelWebhook(db, projectID, channelID, entry.ChannelName, botToken)
		_ = webhookURL
		if createdWebhook {
			createdWebhookIDs = append(createdWebhookIDs, webhookID)
		}
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
	if err := db.Model(&model.Project{}).Where("kitsu_project_id = ?", projectID).Updates(map[string]interface{}{"discord_guild_id": guildID, "discord_category_id": categoryID}).Error; err != nil {
		renderTaskTypeChannelPlanResult(w, r, lang, tr(lang, "channel_plan.routing_persist_failed"), redirect)
		return true
	}
	_ = createdCategory
	cleanupReplacedProjectWebhooks(db, projectID, rows)
	committed = true
	if r.URL.Path == "/bot/setup" {
		redirect = withLang("/bot/setup?wizard=complete&project="+url.QueryEscape(projectID), r)
	}
	renderTaskTypeChannelPlanResult(w, r, lang, trf(lang, "channel_plan.completed", created, len(rows)-created), redirect)
	return true
}

func discordCategoryExists(channels []DiscordGuildChannel, categoryID string) bool {
	for _, channel := range channels {
		if channel.Type == 4 && strings.TrimSpace(channel.ID) == strings.TrimSpace(categoryID) {
			return true
		}
	}
	return false
}

func channelsInCategory(channels []DiscordGuildChannel, categoryID string) []DiscordGuildChannel {
	if strings.TrimSpace(categoryID) == "__create__" {
		return nil
	}
	result := make([]DiscordGuildChannel, 0, len(channels))
	for _, channel := range channels {
		if channel.Type == 0 && strings.TrimSpace(channel.ParentID) == strings.TrimSpace(categoryID) {
			result = append(result, channel)
		}
	}
	return result
}

func liveProjectWebhooksForCategory(webhooks []model.ProjectWebhook, channels []DiscordGuildChannel) []model.ProjectWebhook {
	ids := map[string]bool{}
	for _, channel := range channels {
		ids[strings.TrimSpace(channel.ID)] = true
	}
	result := make([]model.ProjectWebhook, 0, len(webhooks))
	for _, webhook := range webhooks {
		if ids[strings.TrimSpace(webhook.DiscordChannelID)] {
			result = append(result, webhook)
		}
	}
	return result
}

func liveProjectMappingsForCategory(mappings []model.ProductionChannelMapping, channels []DiscordGuildChannel) []model.ProductionChannelMapping {
	ids := map[string]bool{}
	for _, channel := range channels {
		ids[strings.TrimSpace(channel.ID)] = true
	}
	result := make([]model.ProductionChannelMapping, 0, len(mappings))
	for _, mapping := range mappings {
		if ids[strings.TrimSpace(mapping.ChannelID)] {
			result = append(result, mapping)
		}
	}
	return result
}

func cleanupReplacedProjectWebhooks(db *gorm.DB, projectID string, rows []model.ProductionChannelMapping) {
	keep := map[string]bool{}
	for _, row := range rows {
		keep[strings.TrimSpace(row.ChannelID)] = true
	}
	for _, webhook := range model.ListProjectWebhooks(db, projectID) {
		if !keep[strings.TrimSpace(webhook.DiscordChannelID)] {
			_ = db.Delete(&model.ProjectWebhook{}, webhook.ID).Error
		}
	}
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

func ensureProductionChannelWebhook(db *gorm.DB, productionID, channelID, channelName, botToken string) (string, uint, bool, error) {
	var selected *model.ProjectWebhook
	for _, candidate := range model.ListProjectWebhooks(db, productionID) {
		if strings.TrimSpace(candidate.DiscordChannelID) != strings.TrimSpace(channelID) {
			continue
		}
		if strings.TrimSpace(candidate.WebhookURL) == "" {
			return "", 0, false, fmt.Errorf("existing notification destination is incomplete")
		}
		if selected != nil && strings.TrimSpace(selected.WebhookURL) != strings.TrimSpace(candidate.WebhookURL) {
			return "", 0, false, fmt.Errorf("notification destination ownership is ambiguous")
		}
		copy := candidate
		selected = &copy
	}
	if selected != nil {
		return selected.WebhookURL, selected.ID, false, nil
	}
	webhookURL, err := CreateWebhook(channelID, channelName, botToken)
	if err != nil {
		return "", 0, false, err
	}
	if err := model.CreateProjectWebhook(db, productionID, channelName, "", webhookURL, channelID); err != nil {
		return "", 0, false, err
	}
	created := model.ListProjectWebhooks(db, productionID)
	for i := len(created) - 1; i >= 0; i-- {
		if created[i].DiscordChannelID == channelID && created[i].WebhookURL == webhookURL {
			return webhookURL, created[i].ID, true, nil
		}
	}
	return "", 0, false, fmt.Errorf("saved notification destination could not be found")
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
