package setup

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
	"github.com/gookit/slog"
	"gorm.io/gorm"
)

var currentRoutingDiscordCheck = checkDiscordStatus
var currentRoutingListChannels = ListGuildChannels
var currentRoutingSetPositions = SetGuildChannelPositions
var currentRoutingDeleteChannel = DeleteChannel

func handleCurrentIARoutingMutation(w http.ResponseWriter, r *http.Request, lang string, db *gorm.DB, botTokens ...string) bool {
	if handleProductionNotificationApply(w, r, lang, db, botTokens...) {
		return true
	}
	action := strings.TrimSpace(r.FormValue("action"))
	if r.Method == http.MethodPost && action == "delete_current_routing_channel" {
		return handleCurrentIARoutingChannelDelete(w, r, lang, db)
	}
	if r.Method != http.MethodPost || action != "save_current_production_routing" {
		return false
	}
	http.Error(w, "Use the unified Production Notifications Apply request", http.StatusConflict)
	return true
}

func syncCurrentRoutingDiscordOrder(project model.Project, routes []model.ProductionNotificationRoute, db *gorm.DB) error {
	if len(routes) == 0 {
		return nil
	}
	guildID := strings.TrimSpace(project.DiscordGuildID)
	categoryID := strings.TrimSpace(project.DiscordCategoryID)
	if guildID == "" || categoryID == "" {
		return fmt.Errorf("managed Discord guild/category is not configured")
	}
	botToken := storedRuntimeDiscordBotToken(db)
	status := currentRoutingDiscordCheck(botToken, guildID)
	if !routingDiscordStatusReady(status) {
		slog.Warn("Current routing Discord preflight incomplete", "stage", "initial_preflight", "bot_valid", status.BotValid, "guild_valid", status.GuildValid, "manage_channels", status.Permissions.ManageChannels, "manage_webhooks", status.Permissions.ManageWebhooks)
		time.Sleep(500 * time.Millisecond)
		status = currentRoutingDiscordCheck(botToken, guildID)
	}
	slog.Info("Current routing Discord preflight", "stage", "preflight", "bot_valid", status.BotValid, "guild_valid", status.GuildValid, "manage_channels", status.Permissions.ManageChannels, "manage_webhooks", status.Permissions.ManageWebhooks)
	if !status.BotValid || !status.GuildValid || !status.Permissions.ManageChannels {
		return fmt.Errorf("Discord Bot cannot manage channels for the managed guild")
	}
	channels, err := currentRoutingListChannels(guildID, botToken)
	if err != nil {
		return err
	}
	byID := make(map[string]DiscordGuildChannel, len(channels))
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	owned := make(map[string]DiscordGuildChannel, len(routes))
	for _, route := range routes {
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || strings.TrimSpace(webhook.DiscordChannelID) == "" {
			return fmt.Errorf("routing destination ownership is incomplete")
		}
		channel, ok := byID[strings.TrimSpace(webhook.DiscordChannelID)]
		if !ok || channel.Type != 0 || strings.TrimSpace(channel.ParentID) != categoryID {
			return fmt.Errorf("routing destination is not a verified owned channel")
		}
		if _, duplicate := owned[channel.ID]; duplicate {
			return fmt.Errorf("routing destination ownership is duplicated")
		}
		owned[channel.ID] = channel
	}
	current := make([]DiscordGuildChannel, 0, len(owned))
	categoryTextChannels := make([]DiscordGuildChannel, 0)
	for _, channel := range channels {
		if channel.Type == 0 && strings.TrimSpace(channel.ParentID) == categoryID {
			categoryTextChannels = append(categoryTextChannels, channel)
		}
	}
	for _, channel := range owned {
		current = append(current, channel)
	}
	sort.SliceStable(current, func(i, j int) bool { return current[i].Position < current[j].Position })
	compactPositions := len(categoryTextChannels) == len(owned) && len(owned) > 0
	firstPosition := 0
	if compactPositions {
		firstPosition = current[0].Position
	}
	positions := make([]DiscordChannelPosition, 0, len(routes))
	for i, route := range routes {
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		channel := byID[strings.TrimSpace(webhook.DiscordChannelID)]
		position := current[i].Position
		if compactPositions {
			position = firstPosition + i
		}
		positions = append(positions, DiscordChannelPosition{ID: channel.ID, Position: position})
	}
	return currentRoutingSetPositions(guildID, positions, botToken)
}

func routingDiscordStatusReady(status DiscordStatusInfo) bool {
	return status.BotValid && status.GuildValid && status.Permissions.ManageChannels
}

func handleCurrentIARoutingChannelDelete(w http.ResponseWriter, r *http.Request, lang string, db *gorm.DB) bool {
	productionID := strings.TrimSpace(r.FormValue("project_id"))
	project := model.FindProjectByKitsuID(db, productionID)
	redirectQuery := url.Values{"tab": {"notifications"}, "edit_routing": {"1"}}
	if project != nil {
		redirectQuery.Set("project", project.KitsuProjectID)
	}
	redirect := withLang("/bot/admin/projects?"+redirectQuery.Encode(), r)
	webhookID, err := strconv.ParseUint(strings.TrimSpace(r.FormValue("webhook_id")), 10, 64)
	if project == nil || err != nil || webhookID == 0 {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	webhook := model.FindProjectWebhookByID(db, uint(webhookID))
	if webhook == nil || strings.TrimSpace(r.FormValue("confirm_name")) != strings.TrimPrefix(strings.TrimSpace(webhook.ChannelName), "#") {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	if !canDeleteWebhookWithoutRemovingLastRoute(db, *project, webhook.ID) {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	if err := verifyCurrentRoutingOwnedChannel(*project, *webhook, db); err != nil {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	if err := currentRoutingDeleteChannel(webhook.DiscordChannelID, storedRuntimeDiscordBotToken(db)); err != nil {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	if err := db.Where("destination_webhook_id = ?", webhook.ID).Delete(&model.ProductionNotificationRoute{}).Error; err != nil {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	if err := db.Delete(&model.ProjectWebhook{}, webhook.ID).Error; err != nil {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	http.Redirect(w, r, redirect+"&msg=saved", http.StatusSeeOther)
	return true
}

func canDeleteWebhookWithoutRemovingLastRoute(db *gorm.DB, project model.Project, webhookID uint) bool {
	routes := model.ListProductionNotificationRoutes(db, project.KitsuProjectID)
	if len(routes) == 0 {
		return true
	}
	for _, route := range routes {
		if route.DestinationWebhookID != webhookID {
			return true
		}
	}
	return false
}

func verifyCurrentRoutingOwnedChannel(project model.Project, webhook model.ProjectWebhook, db *gorm.DB) error {
	if strings.TrimSpace(project.DiscordGuildID) == "" || strings.TrimSpace(project.DiscordCategoryID) == "" || strings.TrimSpace(webhook.DiscordChannelID) == "" {
		return fmt.Errorf("managed Discord ownership is incomplete")
	}
	token := storedRuntimeDiscordBotToken(db)
	status := currentRoutingDiscordCheck(token, project.DiscordGuildID)
	if !status.BotValid || !status.GuildValid || !status.Permissions.ManageChannels {
		return fmt.Errorf("Discord Bot cannot delete managed channels")
	}
	channels, err := currentRoutingListChannels(project.DiscordGuildID, token)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		if channel.ID == webhook.DiscordChannelID && channel.Type == 0 && channel.ParentID == project.DiscordCategoryID {
			return nil
		}
	}
	return fmt.Errorf("Discord channel is not a verified child of the managed category")
}

func renderCurrentIARoutingEditorSetupStyle(db *gorm.DB, r *http.Request, p model.Project, lang, readTable string, botTokens ...string) string {
	view := loadProductionNotificationReviewerView(db, p, true, botTokens...)
	return renderCurrentIARoutingEditorSetupStyleWithData(db, r, p, lang, readTable, view)
}

func renderCurrentIARoutingEditorMarkupWithData(db *gorm.DB, r *http.Request, p model.Project, lang, readTable string, view productionNotificationReviewerView) string {
	routes := model.ListProductionNotificationRoutes(db, p.KitsuProjectID)
	taskTypes := view.TaskTypes
	webhooks := model.ListProjectWebhooks(db, p.KitsuProjectID)
	destinationList := func(selected uint) string {
		var b strings.Builder
		b.WriteString(`<option value="">` + esc(t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u9078\u629e", "Select Discord Channel")) + `</option>`)
		for _, webhook := range webhooks {
			mark := ""
			if webhook.ID == selected {
				mark = " selected"
			}
			label := strings.TrimSpace(webhook.ChannelName)
			if label == "" {
				label = t(lang, "\u8a2d\u5b9a\u6e08\u307f\u30c1\u30e3\u30f3\u30cd\u30eb", "Configured channel")
			}
			b.WriteString(`<option value="` + strconv.FormatUint(uint64(webhook.ID), 10) + `"` + mark + `>#` + esc(strings.TrimPrefix(label, "#")) + `</option>`)
		}
		b.WriteString(`<option value="__auto__">` + esc(t(lang, "自動作成", "Automatic")) + `</option><option value="__custom__">` + esc(t(lang, "カスタム...", "Custom...")) + `</option>`)
		return b.String()
	}
	taskTypesByID := make(map[string]kitsu.TaskType, len(taskTypes))
	for _, taskType := range taskTypes {
		taskTypesByID[taskType.ID] = taskType
	}
	channelLabel := t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb", "Discord Channel")
	eligibleUsers := map[string]string{}
	if view.TeamErr == nil && view.GuildErr == nil {
		eligibleUsers = currentProductionLinkedHumanDiscordIDs(view.Team, view.Users, view.GuildUsers)
	}
	roleLabels := map[string]string{}
	for _, role := range view.Roles {
		roleLabels[role.ID] = "@" + role.Name
	}
	var userOptions, roleOptions strings.Builder
	userIDs := make([]string, 0, len(eligibleUsers))
	for id := range eligibleUsers {
		userIDs = append(userIDs, id)
	}
	sort.Strings(userIDs)
	var userListboxOptions strings.Builder
	for _, id := range userIDs {
		userOptions.WriteString(`<option value="` + esc(id) + `">` + esc(eligibleUsers[id]) + `</option>`)
		userListboxOptions.WriteString(`<button type="button" role="option" tabindex="-1" aria-selected="false" data-wfa-option-value="` + esc(id) + `">` + esc(eligibleUsers[id]) + `</button>`)
	}
	mentionableRoles := append([]DiscordGuildRole(nil), view.Roles...)
	sort.Slice(mentionableRoles, func(i, j int) bool { return mentionableRoles[i].Name < mentionableRoles[j].Name })
	var roleListboxOptions strings.Builder
	for _, role := range mentionableRoles {
		roleOptions.WriteString(`<option value="` + esc(role.ID) + `">@` + esc(role.Name) + `</option>`)
		roleListboxOptions.WriteString(`<button type="button" role="option" tabindex="-1" aria-selected="false" data-wfa-option-value="` + esc(role.ID) + `">@` + esc(role.Name) + `</button>`)
	}
	userAvailability, roleAvailability := "", ""
	if view.TeamErr != nil {
		userAvailability = `<p class="field-help" role="status">` + esc(t(lang, "Production Teamを読み込めないため、ユーザーを確認できません。", "Production Team could not be verified, so User targets are unavailable.")) + `</p>`
	} else if view.GuildErr != nil || view.BotToken == "" {
		userAvailability = `<p class="field-help" role="status">` + esc(t(lang, "Discordメンバーを確認できないため、ユーザーを選択できません。", "Discord membership could not be verified, so User targets are unavailable.")) + `</p>`
	} else if len(userIDs) == 0 {
		userAvailability = `<p class="field-help" role="status">` + esc(t(lang, "現在リンク可能なDiscordユーザーはいません。", "No linked Production Team users are currently available.")) + `</p>`
	}
	if !view.RolesReady {
		roleAvailability = `<p class="field-help recipient-empty-state" role="status">` + esc(t(lang, "Discordロールを確認できません。", "Discord roles could not be verified.")) + `</p>`
	} else if len(mentionableRoles) == 0 {
		roleAvailability = `<p class="field-help recipient-empty-state" role="status">` + esc(t(lang, "追加可能なDiscordロールはありません", "No eligible Discord roles available")) + `</p>`
	}
	formAction := withLang("/bot/admin/projects", r) + "&project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications&edit_routing=1"
	var rows strings.Builder
	var deleteDialogs strings.Builder
	for _, route := range routes {
		removeLabel := t(lang, "\u901a\u77e5\u5bfe\u8c61\u304b\u3089\u5916\u3059", "Remove from notifications")
		deleteButton := ""
		if len(routes) > 1 {
			if webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID); webhook != nil && strings.TrimSpace(webhook.DiscordChannelID) != "" && strings.TrimSpace(webhook.ChannelName) != "" {
				name := strings.TrimPrefix(strings.TrimSpace(webhook.ChannelName), "#")
				dialogID := "routing-delete-" + strconv.FormatUint(uint64(webhook.ID), 10)
				deleteButton = `<button type="button" class="routing-menu-delete routing-delete-open" data-delete-dialog="` + esc(dialogID) + `" data-channel-name="` + esc(name) + `">` + esc(t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u524a\u9664", "Delete Discord channel")) + `</button>`
				deleteDialogs.WriteString(`<dialog id="` + esc(dialogID) + `" class="routing-delete-dialog"><div data-routing-delete-form style="display:grid;gap:14px"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="webhook_id" value="` + strconv.FormatUint(uint64(webhook.ID), 10) + `"><input type="hidden" name="action" value="delete_current_routing_channel"><h4>` + esc(t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u524a除", "Delete Discord channel")) + `</h4><p>` + esc(t(lang, "\u3053\u306e\u64cd\u4f5c\u306f\u53d6\u308a\u6d88\u305b\u307e\u305b\u3093\u3002", "This action cannot be undone.")) + `</p><label>` + esc(t(lang, "\u78ba\u8a8d\u306e\u305f\u3081\u30c1\u30e3\u30f3\u30cd\u30eb\u540d\u3092\u5165\u529b\u3057\u3066\u304f\u3060\u3055\u3044\u3002", "Type the exact channel name to confirm.")) + `<input data-delete-confirm><small>#` + esc(name) + `</small></label><div class="button-row"><button type="button" class="btn-ghost routing-delete-cancel">` + esc(t(lang, "\u30ad\u30e3\u30f3\u30bb\u30eb", "Cancel")) + `</button><button type="button" class="btn-danger" data-routing-delete-submit disabled>` + esc(t(lang, "\u524a除", "Delete")) + `</button></div></div></dialog>`)
			}
		}
		menuLabel := t(lang, "行の操作", "Route actions")
		targets, _, _ := model.ListProjectReviewerTargetsForTaskType(db, p.ID, route.TaskTypeID)
		type targetView struct {
			Kind  string `json:"kind"`
			ID    string `json:"id"`
			Label string `json:"label"`
		}
		var targetViews []targetView
		for _, target := range targets {
			label := target.DiscordID
			if target.TargetKind == model.ReviewerTargetUser {
				if name := eligibleUsers[target.DiscordID]; name != "" {
					label = name
				} else {
					label = t(lang, "リンクまたはメンバーを確認できません", "User is no longer verified")
				}
			} else if roleLabels[target.DiscordID] != "" {
				label = roleLabels[target.DiscordID]
			} else {
				label = t(lang, "ロールを確認できません", "Role is no longer verified")
			}
			targetViews = append(targetViews, targetView{target.TargetKind, target.DiscordID, label})
		}
		encodedTargets, _ := json.Marshal(targetViews)
		targetsAttribute := base64.StdEncoding.EncodeToString(encodedTargets)
		defaultChannelName := ""
		if taskType, ok := taskTypesByID[route.TaskTypeID]; ok {
			defaultChannelName = NormalizeTaskTypeChannelName(taskType.Name)
		}
		rows.WriteString(`<tr draggable="true" data-routing-row data-task-type="` + esc(route.TaskTypeID) + `" data-task-type-name="` + esc(route.TaskTypeName) + `" data-default-channel-name="` + esc(defaultChannelName) + `" data-existing-targets="` + targetsAttribute + `" data-original-destination="` + strconv.FormatUint(uint64(route.DestinationWebhookID), 10) + `"><td><button type="button" class="routing-select-task" data-select-task aria-pressed="false" aria-expanded="false"><span class="routing-task-type-name">` + esc(route.TaskTypeName) + `</span><span class="routing-expand-indicator" aria-hidden="true"></span></button></td><td><div class="routing-destination-control"><select class="routing-destination-select" name="destination_mode" data-destination-control aria-label="` + esc(channelLabel) + `">` + destinationList(route.DestinationWebhookID) + `</select><label class="routing-custom-channel" data-custom-channel-field hidden><span class="sr-only">` + esc(t(lang, "チャンネル名", "Channel name")) + `</span><input data-new-channel-name maxlength="100" autocomplete="off" disabled>` + `</label></div></td><td><details class="routing-row-menu"><summary aria-label="` + esc(menuLabel) + `">&#8942;</summary><div class="routing-row-menu-panel" role="menu"><button type="button" class="routing-remove" data-routing-remove role="menuitem">` + esc(removeLabel) + `</button>` + deleteButton + `</div></details></td></tr>`)
	}
	userSelectLabel := t(lang, "選択", "Select")
	userPicker := userAvailability
	if len(userIDs) > 0 {
		userPicker = `<label class="recipient-picker-label">` + esc(t(lang, "Discordユーザー", "Discord user")) + `<span class="recipient-picker" data-recipient-picker><button type="button" class="recipient-combobox" role="combobox" aria-haspopup="listbox" aria-expanded="false" aria-controls="wfa-user-listbox" aria-label="` + esc(t(lang, "Discordユーザー", "Discord user")) + `" data-wfa-user-combobox><span data-recipient-combobox-value>` + esc(userSelectLabel) + `</span><span aria-hidden="true" class="recipient-combobox-indicator">▾</span></button><div id="wfa-user-listbox" class="recipient-listbox" role="listbox" aria-label="` + esc(t(lang, "Discordユーザー", "Discord user")) + `" data-wfa-user-listbox hidden>` + userListboxOptions.String() + `</div></span></label>`
	} else if userPicker == "" {
		userPicker = `<p class="field-help recipient-empty-state" role="status">` + esc(t(lang, "現在追加できるDiscordユーザーはいません", "No eligible Discord users available")) + `</p>`
	}
	rolePicker := roleAvailability
	if view.RolesReady && len(mentionableRoles) > 0 {
		rolePicker = `<label class="recipient-picker-label">` + esc(t(lang, "Discordロール", "Discord role")) + `<span class="recipient-picker" data-recipient-picker><button type="button" class="recipient-combobox" role="combobox" aria-haspopup="listbox" aria-expanded="false" aria-controls="wfa-role-listbox" aria-label="` + esc(t(lang, "Discordロール", "Discord role")) + `" data-wfa-role-combobox><span data-recipient-combobox-value>` + esc(userSelectLabel) + `</span><span aria-hidden="true" class="recipient-combobox-indicator">▾</span></button><div id="wfa-role-listbox" class="recipient-listbox" role="listbox" aria-label="` + esc(t(lang, "Discordロール", "Discord role")) + `" data-wfa-role-listbox hidden>` + roleListboxOptions.String() + `</div></span></label>`
	}
	addModal := `<dialog data-wfa-add-modal data-recipient-mode="user" aria-labelledby="wfa-add-modal-title"><h4 id="wfa-add-modal-title">` + esc(t(lang, "通知先を追加", "Add recipient")) + `</h4><div class="recipient-mode-tabs" role="tablist" aria-label="` + esc(t(lang, "通知先の種類", "Recipient type")) + `"><button type="button" role="tab" id="wfa-user-tab" aria-selected="true" aria-controls="wfa-user-panel" data-recipient-mode="user">` + esc(t(lang, "ユーザー", "Users")) + `</button><button type="button" role="tab" id="wfa-role-tab" aria-selected="false" aria-controls="wfa-role-panel" tabindex="-1" data-recipient-mode="role">` + esc(t(lang, "ロール", "Roles")) + `</button></div><div id="wfa-user-panel" role="tabpanel" aria-labelledby="wfa-user-tab" data-recipient-panel="user">` + userPicker + `<select data-wfa-user-id data-recipient-select="user" hidden aria-hidden="true" tabindex="-1"><option value=""></option>` + userOptions.String() + `</select></div><div id="wfa-role-panel" role="tabpanel" aria-labelledby="wfa-role-tab" data-recipient-panel="role" hidden>` + rolePicker + `<select data-wfa-role-id data-recipient-select="role" hidden aria-hidden="true" tabindex="-1"><option value=""></option>` + roleOptions.String() + `</select></div><div class="button-row"><button type="button" class="btn-ghost" data-wfa-add-cancel>` + esc(t(lang, "キャンセル", "Cancel")) + `</button><button type="button" class="btn" data-wfa-add-confirm>` + esc(t(lang, "追加", "Add")) + `</button></div></dialog>`
	html := `<section class="production-routing-editor" aria-labelledby="production-routing-editor-title"><div class="page-heading"><div><h3 id="production-routing-editor-title">` + esc(t(lang, "\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0", "Notification routing")) + `</h3></div><span class="status-pill ` + esc(normalizeStatusClass(iaStatusClass(db, p))) + `">` + esc(iaStatusLabel(db, p, lang)) + `</span></div><form method="post" action="` + esc(formAction) + `" data-current-routing-form data-async-notification-apply data-apply-endpoint="/bot/admin/projects?apply_notifications=1" data-project-id="` + esc(p.KitsuProjectID) + `" data-remove-confirm="` + esc(t(lang, "このTask Typeの通知ルートをApply時に解除します。続行しますか？", "Remove this Task Type route?")) + `" data-stale-message="` + esc(t(lang, "別の編集が保存されました。入力内容は保持されています。内容を確認してください。", "Another edit was saved. Your pending changes are preserved; review them before continuing.")) + `" data-error-message="` + esc(t(lang, "変更を適用できませんでした。入力内容は保持されています。", "Changes could not be applied. Your pending edits are preserved.")) + `" data-empty-wfa="` + esc(t(lang, "Supervisorなし", "No Supervisor")) + `" data-draft-label="` + esc(t(lang, "新しい通知ルート", "New notification route")) + `" data-remove-label="` + esc(t(lang, "解除", "Remove")) + `" data-no-pending="` + esc(t(lang, "変更はありません", "No pending changes")) + `" data-pending-one="` + esc(t(lang, "件の未適用の変更", "pending change")) + `" data-pending-many="` + esc(t(lang, "件の未適用の変更", "pending changes")) + `" data-success-url="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications", r)) + `"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="expected_revision" value="` + esc(model.ProductionNotificationRevision(db, p.ID, p.KitsuProjectID)) + `"><div class="table-wrap wizard-plan-table"><table><thead><tr><th>Kitsu Task Type</th><th>` + esc(channelLabel) + `</th><th></th></tr></thead><tbody data-current-routing-sort>` + rows.String() + `</tbody></table></div><div class="button-row production-routing-editor-actions"><button type="button" class="btn-ghost" data-routing-add>+ ` + esc(t(lang, "Task Type\u3092追加", "Add Task Type")) + `</button></div><div hidden data-editor-parking><section class="production-wfa-edit-panel" data-wfa-detail-panel aria-live="polite"></section></div><div data-apply-message role="status" aria-live="polite"></div><div class="button-row production-routing-editor-footer"><span data-pending-status role="status">` + esc(t(lang, "変更はありません", "No pending changes")) + `</span><button type="submit" class="btn" data-apply-submit disabled>` + esc(t(lang, "\u5909\u66f4\u3092\u9069\u7528", "Apply")) + `</button><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications", r)) + `" data-pending-cancel>` + esc(t(lang, "\u30ad\u30e3\u30f3\u30bb\u30eb", "Cancel")) + `</a></div></form>` + addModal + deleteDialogs.String() + `<div hidden data-wfa-source>` + readTable + `</div><p class="field-help routing-destructive-note">` + esc(t(lang, "\u3053\u3053\u3067\u5916\u3059\u306e\u306fKitsuSync\u306e\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0\u3060\u3051\u3067\u3059\u3002Kitsu Task Type\u3084Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u306f\u524a\u9664\u3057\u307e\u305b\u3093\u3002", "Removing a route changes only KitsuSync routing; it never deletes Kitsu Task Types or Discord channels.")) + `</p></section>` + currentRoutingEditorScript() + currentRoutingDeleteScript()
	panelStart := strings.Index(html, `<section class="production-wfa-edit-panel" data-wfa-detail-panel`)
	if panelStart >= 0 {
		panelEnd := strings.Index(html[panelStart:], `</section>`)
		if panelEnd >= 0 {
			panelEnd += panelStart + len(`</section>`)
			panel := `<section class="production-wfa-edit-panel" data-wfa-detail-panel aria-live="polite"><div class="production-wfa-edit-heading"><span>` + esc(t(lang, "編集中のTask Type", "Editing Task Type")) + `</span><h4 data-wfa-title>` + esc(t(lang, "Task Typeを選択", "Select a Task Type")) + `</h4></div><div class="production-wfa-channel"><label>` + esc(channelLabel) + `<div data-wfa-channel-control></div></label><label class="production-wfa-new-channel" data-new-channel-field hidden><span>` + esc(t(lang, "チャンネル名", "Channel name")) + `</span><input data-new-channel-name maxlength="100" autocomplete="off" disabled required></label><small class="field-help" data-new-channel-help hidden>` + esc(t(lang, "ApplyするとDiscordチャンネルを作成します。", "The Discord channel will be created when you Apply.")) + `</small></div><section class="production-wfa-recipient-block"><h5>` + esc(t(lang, "WFA通知先", "WFA recipients")) + `</h5><div class="production-wfa-recipient-row" data-wfa-automatic><span class="production-wfa-recipient-kind">` + esc(t(lang, "自動", "Automatic")) + `</span><span data-wfa-automatic-value>` + esc(t(lang, "確認中", "Checking")) + `</span><small class="field-help">` + esc(t(lang, "SupervisorはKitsuのDepartmentから自動設定されます。", "Supervisors are derived from the Kitsu Department.")) + `</small></div><div class="production-wfa-recipient-row" data-wfa-additional><span class="production-wfa-recipient-kind">` + esc(t(lang, "追加", "Additional")) + `</span><ul data-wfa-target-list></ul><button type="button" class="btn-ghost production-wfa-add-target" data-wfa-add-target>` + esc(t(lang, "通知先を追加", "Add recipient")) + `</button></div></section></section>`
			html = html[:panelStart] + panel + html[panelEnd:]
		}
	}
	html = strings.Replace(html, `<div data-apply-message role="status" aria-live="polite"></div><div class="button-row production-routing-editor-actions">`, `<div data-apply-message role="status" aria-live="polite"></div><div class="button-row production-routing-editor-footer">`, 1)
	return html
}

func renderCurrentIARoutingSummaryWithStatus(db *gorm.DB, r *http.Request, p model.Project, lang, class, label string) string {
	channelLabel := t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb", "Discord Channel")
	var rows strings.Builder
	for _, route := range model.ListProductionNotificationRoutes(db, p.KitsuProjectID) {
		channel := t(lang, "\u672a\u8a2d\u5b9a", "Not configured")
		if webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID); webhook != nil && strings.TrimSpace(webhook.ChannelName) != "" {
			channel = "#" + strings.TrimPrefix(strings.TrimSpace(webhook.ChannelName), "#")
		}
		rows.WriteString(`<div class="production-routing-summary-row"><strong>` + esc(route.TaskTypeName) + `</strong><span aria-hidden="true">&#8594;</span><span>` + esc(channel) + `</span></div>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<p class="field-help">` + esc(t(lang, "\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0\u306f\u307e\u3060\u8a2d\u5b9a\u3055\u308c\u3066\u3044\u307e\u305b\u3093\u3002", "No notification routing is configured.")) + `</p>`)
	}
	editURL := withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications&edit_routing=1", r)
	return `<section class="production-settings-section production-routing-summary"><div class="page-heading"><div><h3>` + esc(t(lang, "\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0", "Notification routing")) + `</h3><p class="field-help">` + esc(t(lang, "Kitsu Task Type\u304b\u3089Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3078\u306e\u8aad\u307f\u53d6\u308a\u5c02\u7528\u30de\u30c3\u30d4\u30f3\u30b0\u3067\u3059\u3002", "Read-only mapping from Kitsu Task Type to Discord Channel.")) + `</p></div><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(label) + `</span><a class="btn-ghost" href="` + esc(editURL) + `">` + esc(t(lang, "\u7de8\u96c6", "Edit")) + `</a></div><div class="production-routing-summary-head"><strong>Kitsu Task Type</strong><strong>` + esc(channelLabel) + `</strong></div><div class="production-routing-summary-list">` + rows.String() + `</div></section>`
}

func iaStatusClass(db *gorm.DB, p model.Project) string {
	class, _, _ := iaStatus(db, p, "en")
	return class
}
func iaStatusLabel(db *gorm.DB, p model.Project, lang string) string {
	_, label, _ := iaStatus(db, p, lang)
	return label
}

func currentRoutingDeleteScript() string {
	return `<script>(function(){Array.prototype.forEach.call(document.querySelectorAll('.routing-delete-open'),function(open){var dialog=document.getElementById(open.getAttribute('data-delete-dialog')||'');if(!dialog)return;open.addEventListener('click',function(){dialog.showModal()});dialog.querySelector('.routing-delete-cancel')?.addEventListener('click',function(){dialog.close()});var input=dialog.querySelector('[data-delete-confirm]'),submit=dialog.querySelector('[data-routing-delete-submit]'),expected=open.getAttribute('data-channel-name');input?.addEventListener('input',function(){if(submit)submit.disabled=input.value!==expected});submit?.addEventListener('click',function(e){e.preventDefault();var form=document.createElement('form');form.method='post';form.action=window.location.href;Array.prototype.forEach.call(dialog.querySelectorAll('input[type=hidden]'),function(source){var copy=document.createElement('input');copy.type='hidden';copy.name=source.name;copy.value=source.value;form.appendChild(copy)});var confirmation=document.createElement('input');confirmation.type='hidden';confirmation.name='confirm_name';confirmation.value=input?.value||'';form.appendChild(confirmation);document.body.appendChild(form);form.submit()})})})();</script>`
}

func currentRecipientComboboxScript() string {
	return `<script>
(function(){
  var modal=document.querySelector('[data-wfa-add-modal]');
  if(!modal)return;
  var pickers=Array.prototype.map.call(modal.querySelectorAll('[data-recipient-picker]'),function(picker){
    var combo=picker.querySelector('[role="combobox"]'),list=picker.querySelector('[role="listbox"]'),panel=picker.closest('[data-recipient-panel]'),select=panel&&panel.querySelector('select[data-recipient-select]'),valueNode=combo.querySelector('[data-recipient-combobox-value]'),empty=valueNode.textContent;
    if(!select)return null;
    function options(){return Array.prototype.slice.call(list.querySelectorAll('[role="option"]'))}
    function enabled(){return options().filter(function(option){return option.getAttribute('aria-disabled')!=='true'})}
    function sync(){var selected=select.selectedOptions&&select.selectedOptions[0];valueNode.textContent=selected&&selected.value?selected.textContent:empty;options().forEach(function(option){option.setAttribute('aria-selected',String(!!selected&&selected.value===option.dataset.wfaOptionValue))})}
    function close(){list.hidden=true;combo.setAttribute('aria-expanded','false');list.style.left='';list.style.top='';list.style.width=''}
    function open(last){list.hidden=false;combo.setAttribute('aria-expanded','true');var anchor=combo.getBoundingClientRect(),width=Math.min(anchor.width,innerWidth-16),left=Math.max(8,Math.min(anchor.left,innerWidth-width-8));list.style.left=left+'px';list.style.width=width+'px';var menu=list.getBoundingClientRect(),top=anchor.bottom+4;if(top+menu.height>innerHeight-8)top=anchor.top-menu.height-4;list.style.top=Math.max(8,Math.min(top,innerHeight-menu.height-8))+'px';var choices=enabled(),target=last?choices[choices.length-1]:choices[0];if(target)target.focus()}
    function choose(option){if(!option||option.getAttribute('aria-disabled')==='true')return;select.value=option.dataset.wfaOptionValue;select.dispatchEvent(new Event('change',{bubbles:true}));sync();close();combo.focus()}
    combo.addEventListener('click',function(){if(list.hidden)open(false);else close()});
    combo.addEventListener('keydown',function(event){if(event.key==='ArrowDown'||event.key==='ArrowUp'||event.key==='Enter'||event.key===' '){event.preventDefault();open(event.key==='ArrowUp')}});
    list.addEventListener('click',function(event){var option=event.target.closest('[role="option"]');if(option)choose(option)});
    list.addEventListener('keydown',function(event){var choices=enabled(),index=choices.indexOf(document.activeElement),target=null;if(event.key==='ArrowDown'){event.preventDefault();target=choices[Math.min(index+1,choices.length-1)]}else if(event.key==='ArrowUp'){event.preventDefault();target=choices[Math.max(index-1,0)]}else if(event.key==='Home'){event.preventDefault();target=choices[0]}else if(event.key==='End'){event.preventDefault();target=choices[choices.length-1]}else if(event.key==='Enter'||event.key===' '){event.preventDefault();choose(document.activeElement)}else if(event.key==='Escape'){event.preventDefault();close();combo.focus()}else if(event.key==='Tab'){close()}if(target)target.focus()});
    select.addEventListener('change',sync);
    return{picker:picker,close:close,sync:sync}
  });
  pickers=pickers.filter(Boolean);
  document.addEventListener('pointerdown',function(event){pickers.forEach(function(item){if(!item.picker.contains(event.target))item.close()})});
  modal.addEventListener('cancel',function(){pickers.forEach(function(item){item.close()})});
  modal.addEventListener('close',function(){pickers.forEach(function(item){item.close();item.sync()})});
  window.addEventListener('resize',function(){pickers.forEach(function(item){item.close()})});
  window.addEventListener('scroll',function(){pickers.forEach(function(item){if(!item.picker.querySelector('[role="listbox"]').hidden)item.close()})},true)
})();
</script>`
}
