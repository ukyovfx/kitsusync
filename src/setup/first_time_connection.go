package setup

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

type firstTimeDiscordCheck struct {
	BotValid       bool
	GuildValid     bool
	GuildName      string
	ManageChannels bool
	ManageWebhooks bool
	Reason         string
}

type firstTimePermissionError struct {
	Missing []string
}

func (e firstTimePermissionError) Error() string {
	return "required Discord permissions are missing"
}

func firstTimePermissionLabel(lang, permission string) string {
	if lang == "ja" {
		return map[string]string{
			"manage_channels": "チャンネルの管理",
			"manage_webhooks": "Webhookの管理",
		}[permission]
	}
	return map[string]string{
		"manage_channels": "Manage Channels",
		"manage_webhooks": "Manage Webhooks",
	}[permission]
}

func firstTimeConnectionErrorMessage(lang string, err error) string {
	var permissionErr firstTimePermissionError
	if errors.As(err, &permissionErr) {
		missing := make([]string, 0, len(permissionErr.Missing))
		for _, permission := range permissionErr.Missing {
			missing = append(missing, firstTimePermissionLabel(lang, permission))
		}
		if lang == "ja" {
			return "Discord Botの権限が不足しています\n\n不足している権限:\n- " + strings.Join(missing, "\n- ") + "\n\nDiscordサーバーでBotの権限を変更してから、もう一度実行してください。"
		}
		return "The Discord Bot is missing required permissions.\n\nMissing permissions:\n- " + strings.Join(missing, "\n- ") + "\n\nUpdate the Bot permissions in the Discord server, then try again."
	}
	return err.Error()
}

type firstTimeConnectionPlan struct {
	Project      KitsuProject
	GuildID      string
	GuildName    string
	TaskTypes    []kitsu.TaskType
	Plan         TaskTypeChannelPlan
	GuildChannel []DiscordGuildChannel
}

type firstTimeOwnedResources struct {
	ProductionID    string
	OperationID     string
	CreatedProject  bool
	CategoryID      string
	CreatedCategory bool
	ChannelIDs      []string
}

type firstTimeConnectionOps struct {
	Projects       func(string) []KitsuProject
	TaskTypes      func(string) []kitsu.TaskType
	DiscordCheck   func(string, string) firstTimeDiscordCheck
	ListChannels   func(string, string) ([]DiscordGuildChannel, error)
	CreateCategory func(string, string, string) (string, error)
	CreateChannel  func(string, string, string, string) (string, error)
	CreateWebhook  func(string, string, string) (string, error)
	SetPosition    func(string, int, string) error
	DeleteChannel  func(string, string) error
}

var defaultFirstTimeConnectionOps = firstTimeConnectionOps{
	Projects:  ListKitsuProjects,
	TaskTypes: routingTaskTypesForProduction,
	DiscordCheck: func(token, guild string) firstTimeDiscordCheck {
		status := checkDiscordStatus(token, guild)
		reason := ""
		if status.Error != nil {
			reason = *status.Error
		}
		return firstTimeDiscordCheck{BotValid: status.BotValid, GuildValid: status.GuildValid, GuildName: status.GuildName, ManageChannels: status.Permissions.ManageChannels, ManageWebhooks: status.Permissions.ManageWebhooks, Reason: reason}
	},
	ListChannels:   ListGuildChannels,
	CreateCategory: CreateCategory,
	CreateChannel:  CreateTextChannel,
	CreateWebhook:  CreateWebhook,
	SetPosition:    SetGuildChannelPosition,
	DeleteChannel:  DeleteChannel,
}

var firstTimeOps = defaultFirstTimeConnectionOps

var firstTimeActivateRouting = model.ActivateProductionRoutingFromMappings

var firstTimeExecutionMu sync.Mutex

func validateFirstTimeConnectionRequest(r *http.Request, kitsuHost, botToken string, db *gorm.DB) (firstTimeConnectionPlan, error) {
	ops := firstTimeOps
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	guildID := strings.TrimSpace(r.FormValue("guild_id"))
	if projectID == "" || guildID == "" || strings.TrimSpace(r.FormValue("confirm_plan")) != "yes" {
		return firstTimeConnectionPlan{}, fmt.Errorf("Production and Discord server confirmation are required")
	}
	existingProject := model.FindProjectByKitsuID(db, projectID)
	if existingProject == nil && model.HasProductionOperationalState(db, projectID) {
		return firstTimeConnectionPlan{}, fmt.Errorf("Production has incomplete setup state that requires cleanup before retry")
	}
	if existingProject != nil && !isIncompleteFirstTimeProject(db, projectID) {
		return firstTimeConnectionPlan{}, fmt.Errorf("Production is already connected")
	}
	var project KitsuProject
	for _, candidate := range ops.Projects(kitsuHost) {
		if strings.TrimSpace(candidate.ID) == projectID {
			project = candidate
			break
		}
	}
	if project.ID == "" {
		return firstTimeConnectionPlan{}, fmt.Errorf("The selected Production could not be verified")
	}
	if strings.TrimSpace(botToken) == "" {
		return firstTimeConnectionPlan{}, fmt.Errorf("Discord Bot is not configured")
	}
	status := ops.DiscordCheck(botToken, guildID)
	if !status.BotValid || !status.GuildValid || !status.ManageChannels || !status.ManageWebhooks {
		if status.BotValid && status.GuildValid {
			missing := make([]string, 0, 2)
			if !status.ManageChannels {
				missing = append(missing, "manage_channels")
			}
			if !status.ManageWebhooks {
				missing = append(missing, "manage_webhooks")
			}
			if len(missing) > 0 {
				return firstTimeConnectionPlan{}, firstTimePermissionError{Missing: missing}
			}
		}
		if status.Reason != "" {
			return firstTimeConnectionPlan{}, fmt.Errorf("Discord validation failed: %s", status.Reason)
		}
		return firstTimeConnectionPlan{}, fmt.Errorf("Required Discord permissions are not available")
	}
	channels, err := ops.ListChannels(guildID, botToken)
	if err != nil {
		return firstTimeConnectionPlan{}, fmt.Errorf("Discord channel read failed")
	}
	taskTypes := ops.TaskTypes(projectID)
	if len(taskTypes) == 0 {
		return firstTimeConnectionPlan{}, fmt.Errorf("No valid Task Types were found for this Production")
	}
	if err := validateSubmittedTaskTypeIDs(r, taskTypes); err != nil {
		return firstTimeConnectionPlan{}, err
	}
	active, overrides := taskTypePlanRequest(r, taskTypes)
	existing := map[string]string{}
	if existingProject != nil {
		existing = managedExistingChannelsForFirstTime(channels, model.ListProductionChannelMappings(db, projectID), model.ListProjectWebhooks(db, projectID))
	}
	plan := BuildTaskTypeChannelPlanWithOverrides(projectID, guildID, active, existing, overrides)
	if plan.Fingerprint() != strings.TrimSpace(r.FormValue("plan_fingerprint")) {
		return firstTimeConnectionPlan{}, fmt.Errorf("The reviewed plan is stale")
	}
	if !plan.Valid() {
		return firstTimeConnectionPlan{}, fmt.Errorf("The reviewed plan is invalid")
	}
	for _, entry := range plan.Entries {
		if entry.Action == "reuse" && existingProject == nil {
			return firstTimeConnectionPlan{}, fmt.Errorf("An existing Discord channel has no provable KitsuSync ownership")
		}
	}
	return firstTimeConnectionPlan{Project: project, GuildID: guildID, GuildName: status.GuildName, TaskTypes: taskTypes, Plan: plan, GuildChannel: channels}, nil
}

func isIncompleteFirstTimeProject(db *gorm.DB, projectID string) bool {
	project := model.FindProjectByKitsuID(db, projectID)
	if project == nil || strings.TrimSpace(project.DiscordCategoryID) == "" {
		return false
	}
	config := model.FindProductionNotificationConfig(db, projectID)
	return config == nil || !config.Enabled || len(model.ListProductionChannelMappings(db, projectID)) == 0
}

func managedExistingChannelsForFirstTime(channels []DiscordGuildChannel, mappings []model.ProductionChannelMapping, legacy []model.ProjectWebhook) map[string]string {
	owned := map[string]bool{}
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.ChannelID) != "" {
			owned[strings.TrimSpace(mapping.ChannelID)] = true
		}
	}
	for _, webhook := range legacy {
		if strings.TrimSpace(webhook.DiscordChannelID) != "" {
			owned[strings.TrimSpace(webhook.DiscordChannelID)] = true
		}
	}
	result := map[string]string{}
	for _, channel := range channels {
		if channel.Type == 0 && owned[strings.TrimSpace(channel.ID)] {
			result[NormalizeTaskTypeChannelName(channel.Name)] = strings.TrimSpace(channel.ID)
		}
	}
	return result
}

func validateSubmittedTaskTypeIDs(r *http.Request, taskTypes []kitsu.TaskType) error {
	valid := map[string]bool{}
	for _, taskType := range taskTypes {
		valid[strings.TrimSpace(taskType.ID)] = true
	}
	seen := map[string]bool{}
	for _, raw := range r.Form["included_task_type_id"] {
		id := strings.TrimSpace(raw)
		if id == "" || !valid[id] || seen[id] {
			return fmt.Errorf("Submitted Task Type selection is invalid")
		}
		seen[id] = true
	}
	return nil
}

func renderFirstTimeExecutionPage(lang string, r *http.Request, plan firstTimeConnectionPlan) string {
	return adminPage(lang, tr(lang, "wizard.execute_title"), r, renderWizardFrame(lang, 6, renderWizardExecutionCard(lang, r, plan)))
}

func firstTimeGuildName(lang, guildName string) string {
	if strings.TrimSpace(guildName) != "" {
		return guildName
	}
	return tr(lang, "wizard.server_summary")
}

func renderWizardExecutionCard(lang string, r *http.Request, plan firstTimeConnectionPlan) string {
	var hidden strings.Builder
	for _, entry := range plan.Plan.Entries {
		hidden.WriteString(`<input type="hidden" name="included_task_type_id" value="` + html.EscapeString(entry.TaskTypeID) + `"><input type="hidden" name="channel_name_` + html.EscapeString(entry.TaskTypeID) + `" value="` + html.EscapeString(entry.ChannelName) + `"><input type="hidden" name="channel_order_` + html.EscapeString(entry.TaskTypeID) + `" value="` + fmt.Sprint(entry.Order) + `">`)
	}
	form := `<form id="wizard-execution-form" method="POST" action="` + html.EscapeString(withLang("/bot/setup", r)) + `"><input type="hidden" name="action" value="execute_production_connection"><input type="hidden" name="confirm_plan" value="yes"><input type="hidden" name="project_id" value="` + html.EscapeString(plan.Project.ID) + `"><input type="hidden" name="guild_id" value="` + html.EscapeString(plan.GuildID) + `"><input type="hidden" name="plan_fingerprint" value="` + html.EscapeString(plan.Plan.Fingerprint()) + `">` + hidden.String() + `<noscript><div class="button-row"><a class="btn-ghost" href="` + html.EscapeString(setupWizardURL(r, 5, plan.Project.ID, plan.GuildID, true)) + `">` + html.EscapeString(tr(lang, "wizard.back_to_review")) + `</a><button class="btn" type="submit">` + html.EscapeString(tr(lang, "wizard.execute")) + `</button></div></noscript></form>`
	targets := `<dl class="wizard-connection-summary"><div><dt>` + html.EscapeString(tr(lang, "wizard.production_summary")) + `</dt><dd>` + html.EscapeString(plan.Project.Name) + `</dd></div><div><dt>` + html.EscapeString(tr(lang, "wizard.server_summary")) + `</dt><dd>` + html.EscapeString(firstTimeGuildName(lang, plan.GuildName)) + `</dd></div><div><dt>` + html.EscapeString(tr(lang, "wizard.area_summary")) + `</dt><dd>` + html.EscapeString(KitsuSyncCategoryName(plan.Project.Name)) + `</dd></div></dl>`
	return `<section class="section-card glass" aria-labelledby="wizard-execute-title" role="status" aria-live="polite"><h2 id="wizard-execute-title">` + html.EscapeString(tr(lang, "wizard.execute_title")) + `</h2><p class="hint">` + html.EscapeString(tr(lang, "wizard.execute_hint")) + `</p>` + targets + `<p class="field-help">` + html.EscapeString(tr(lang, "wizard.execute_started")) + `</p>` + form + `<script>(function(){var form=document.getElementById('wizard-execution-form');if(form){form.submit();}})();</script></section>`
}

func renderFirstTimeExecutionError(lang string, r *http.Request, message string) string {
	messageHTML := strings.ReplaceAll(html.EscapeString(message), "\n", "<br>")
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	guildID := strings.TrimSpace(r.FormValue("guild_id"))
	backURL := withLang("/bot/setup", r)
	if projectID != "" && guildID != "" {
		backURL = setupWizardURL(r, 5, projectID, guildID, true)
	}
	body := `<section class="section-card glass" role="alert" aria-live="assertive"><h2>` + html.EscapeString(tr(lang, "wizard.execute_title")) + `</h2><p class="state-explanation">` + messageHTML + `</p><div class="button-row"><a class="btn-ghost" href="` + html.EscapeString(backURL) + `">` + html.EscapeString(tr(lang, "wizard.back_to_review")) + `</a></div></section>`
	return adminPage(lang, tr(lang, "wizard.execute_title"), r, renderWizardFrame(lang, 6, body))
}

func markFirstTimeSetupReviewRequired(db *gorm.DB, productionID, operationID string) {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return
	}
	query := db.Model(&model.ProductionChannelMapping{}).Where("production_id = ?", productionID)
	if strings.TrimSpace(operationID) != "" {
		query = query.Where("operation_id = ?", operationID)
	}
	_ = query.Updates(map[string]interface{}{
		"active":          false,
		"state":           model.ChannelMappingStateReviewRequired,
		"migration_state": model.ChannelMappingStateReviewRequired,
		"last_error":      "first-time setup cleanup requires review",
	}).Error
	_ = db.Model(&model.ProductionNotificationConfig{}).
		Where("production_id = ?", productionID).
		Update("enabled", false).Error
}

func rollbackFirstTimeConnection(db *gorm.DB, botToken string, ops firstTimeConnectionOps, owned firstTimeOwnedResources) error {
	cleanupFailed := false
	for i := len(owned.ChannelIDs) - 1; i >= 0; i-- {
		if err := ops.DeleteChannel(owned.ChannelIDs[i], botToken); err != nil && !isDiscordNotFoundDeleteError(err) {
			cleanupFailed = true
		}
	}
	if owned.CreatedCategory && strings.TrimSpace(owned.CategoryID) != "" {
		if err := ops.DeleteChannel(owned.CategoryID, botToken); err != nil && !isDiscordNotFoundDeleteError(err) {
			cleanupFailed = true
		}
	}
	if cleanupFailed {
		markFirstTimeSetupReviewRequired(db, owned.ProductionID, owned.OperationID)
		return errors.New("first-time setup cleanup requires review")
	}
	if !owned.CreatedProject {
		markFirstTimeSetupReviewRequired(db, owned.ProductionID, owned.OperationID)
		return nil
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := model.DeleteProductionOperationalState(tx, owned.ProductionID); err != nil {
			return err
		}
		if err := tx.Where("kitsu_project_id = ?", owned.ProductionID).Delete(&model.ProjectWebhook{}).Error; err != nil {
			return err
		}
		return tx.Where("kitsu_project_id = ?", owned.ProductionID).Delete(&model.Project{}).Error
	})
	if err != nil {
		markFirstTimeSetupReviewRequired(db, owned.ProductionID, owned.OperationID)
	}
	return err
}

func executeFirstTimeConnection(plan firstTimeConnectionPlan, botToken, lang string, r *http.Request, db *gorm.DB) (result string) {
	ops := firstTimeOps
	project := model.FindProjectByKitsuID(db, plan.Project.ID)
	createdProject := project == nil
	categoryID := ""
	operationID := fmt.Sprintf("first-time-%s", plan.Project.ID)
	owned := firstTimeOwnedResources{ProductionID: plan.Project.ID, OperationID: operationID, CreatedProject: createdProject, CreatedCategory: createdProject}
	completed := false
	defer func() {
		if completed {
			return
		}
		if err := rollbackFirstTimeConnection(db, botToken, ops, owned); err != nil {
			result += " " + err.Error()
		}
	}()
	if project != nil {
		categoryID = strings.TrimSpace(project.DiscordCategoryID)
	} else {
		var err error
		categoryID, err = ops.CreateCategory(plan.GuildID, KitsuSyncCategoryName(plan.Project.Name), botToken)
		if err != nil {
			return renderFirstTimeExecutionError(lang, r, "Discordカテゴリを作成できませんでした。変更は保存されていません。")
		}
		owned.CategoryID = categoryID
		if err := model.CreateProject(db, plan.Project.ID, plan.Project.Name, "kitsu", plan.GuildID, categoryID, lang); err != nil {
			return renderFirstTimeExecutionError(lang, r, "Production接続を保存できませんでした。作成したDiscordカテゴリは削除しました。")
		}
	}
	rows := make([]model.ProductionChannelMapping, 0, len(plan.Plan.Entries))
	owned.CategoryID = categoryID
	for _, entry := range plan.Plan.Entries {
		channelID := strings.TrimSpace(entry.ExistingID)
		if entry.Action == "create" {
			var err error
			channelID, err = ops.CreateChannel(plan.GuildID, categoryID, entry.ChannelName, botToken)
			if err != nil {
				markPendingChannelPlanFailure(db, rows, operationID, err.Error())
				return renderFirstTimeExecutionError(lang, r, "一部のチャンネルを作成できませんでした。保存済みの状態を確認してから再試行してください。")
			}
			owned.ChannelIDs = append(owned.ChannelIDs, channelID)
		}
		row := model.ProductionChannelMapping{ProductionID: plan.Project.ID, GuildID: plan.GuildID, TaskTypeID: entry.TaskTypeID, TaskTypeName: entry.TaskTypeName, ChannelID: channelID, ChannelName: entry.ChannelName, Active: true, State: model.ChannelMappingStateCurrent, MigrationState: model.ChannelMappingStateCurrent, OperationID: operationID}
		if err := model.SavePendingProductionChannelMapping(db, row); err != nil {
			markPendingChannelPlanFailure(db, rows, operationID, err.Error())
			return renderFirstTimeExecutionError(lang, r, "チャンネル設定の保存に失敗しました。保存済みの状態を確認してから再試行してください。")
		}
		rows = append(rows, row)
		webhookURL := ""
		hasExistingWebhook := false
		for _, webhook := range model.ListProjectWebhooks(db, plan.Project.ID) {
			if strings.TrimSpace(webhook.DiscordChannelID) == channelID && strings.TrimSpace(webhook.WebhookURL) != "" {
				webhookURL = strings.TrimSpace(webhook.WebhookURL)
				hasExistingWebhook = true
				break
			}
		}
		if !hasExistingWebhook {
			var err error
			webhookURL, err = ops.CreateWebhook(channelID, entry.ChannelName, botToken)
			if err != nil {
				markPendingChannelPlanFailure(db, rows, operationID, err.Error())
				return renderFirstTimeExecutionError(lang, r, "通知先を作成できませんでした。保存済みの状態を確認してから再試行してください。")
			}
			if err := model.CreateProjectWebhook(db, plan.Project.ID, entry.ChannelName, "", webhookURL, channelID); err != nil {
				markPendingChannelPlanFailure(db, rows, operationID, err.Error())
				return renderFirstTimeExecutionError(lang, r, "通知先の保存に失敗しました。保存済みの状態を確認してから再試行してください。")
			}
		}
		if err := ops.SetPosition(channelID, entry.Order, botToken); err != nil {
			markPendingChannelPlanFailure(db, rows, operationID, err.Error())
			return renderFirstTimeExecutionError(lang, r, "チャンネル順を反映できませんでした。保存済みの状態を確認してから再試行してください。")
		}
	}
	if err := firstTimeActivateRouting(db, plan.Project.ID, plan.GuildID, rows); err != nil {
		markPendingChannelPlanFailure(db, rows, operationID, err.Error())
		return renderFirstTimeExecutionError(lang, r, "接続を完了できませんでした。保存済みの状態を確認してから再試行してください。")
	}
	completed = true
	return adminPage(lang, tr(lang, "wizard.complete_title"), r, renderWizardFrame(lang, 7, renderWizardComplete(lang, r, db, plan)))
}

func handleFirstTimeConnectionAction(w http.ResponseWriter, r *http.Request, lang, kitsuHost, botToken string, db *gorm.DB) bool {
	if r.Method != http.MethodPost {
		return false
	}
	action := strings.TrimSpace(r.FormValue("action"))
	if action != "prepare_production_connection" && action != "execute_production_connection" {
		return false
	}
	plan, err := validateFirstTimeConnectionRequest(r, kitsuHost, botToken, db)
	if err != nil {
		fmt.Fprint(w, renderFirstTimeExecutionError(lang, r, firstTimeConnectionErrorMessage(lang, err)))
		return true
	}
	if action == "prepare_production_connection" {
		fmt.Fprint(w, renderFirstTimeExecutionPage(lang, r, plan))
		return true
	}
	firstTimeExecutionMu.Lock()
	defer firstTimeExecutionMu.Unlock()
	fmt.Fprint(w, executeFirstTimeConnection(plan, botToken, lang, r, db))
	return true
}
