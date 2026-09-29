package setup

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

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
	redirect := withLang("/bot/admin/projects", r) + "&project=" + url.QueryEscape(productionID) + "&tab=notifications&edit_routing=1"
	project := model.FindProjectByKitsuID(db, productionID)
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

func renderCurrentIARoutingEditorSetupStyleWithData(db *gorm.DB, r *http.Request, p model.Project, lang, readTable string, view productionNotificationReviewerView) string {
	routes := model.ListProductionNotificationRoutes(db, p.KitsuProjectID)
	taskTypes := view.TaskTypes
	webhooks := model.ListProjectWebhooks(db, p.KitsuProjectID)
	used := map[string]bool{}
	for _, route := range routes {
		used[route.TaskTypeID] = true
	}
	optionList := func(selected string, includeUsed bool) string {
		var b strings.Builder
		b.WriteString(`<option value="">` + esc(t(lang, "Task Type\u3092\u9078\u629e", "Select Task Type")) + `</option>`)
		for _, taskType := range taskTypes {
			if !includeUsed && used[taskType.ID] && taskType.ID != selected {
				continue
			}
			mark := ""
			if taskType.ID == selected {
				mark = " selected"
			}
			b.WriteString(`<option value="` + esc(taskType.ID) + `"` + mark + `>` + esc(taskType.Name) + `</option>`)
		}
		return b.String()
	}
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
		return b.String()
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
	for _, id := range userIDs {
		userOptions.WriteString(`<option value="` + esc(id) + `">` + esc(eligibleUsers[id]) + `</option>`)
	}
	mentionableRoles := append([]DiscordGuildRole(nil), view.Roles...)
	sort.Slice(mentionableRoles, func(i, j int) bool { return mentionableRoles[i].Name < mentionableRoles[j].Name })
	for _, role := range mentionableRoles {
		roleOptions.WriteString(`<option value="` + esc(role.ID) + `">@` + esc(role.Name) + `</option>`)
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
		roleAvailability = `<p class="field-help" role="status">` + esc(t(lang, "Discordロールを確認できません。", "Discord roles could not be verified.")) + `</p>`
	} else if len(mentionableRoles) == 0 {
		roleAvailability = `<p class="field-help" role="status">` + esc(t(lang, "追加できるDiscordロールはありません。", "No mentionable Discord roles are available.")) + `</p>`
	}
	formAction := withLang("/bot/admin/projects", r) + "&project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications&edit_routing=1"
	var rows strings.Builder
	var deleteDialogs strings.Builder
	for _, route := range routes {
		removeLabel := t(lang, "\u901a\u77e5\u5bfe\u8c61\u304b\u3089\u5916\u3059", "Remove from notifications")
		deleteButton := ""
		if webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID); webhook != nil && strings.TrimSpace(webhook.DiscordChannelID) != "" && strings.TrimSpace(webhook.ChannelName) != "" {
			name := strings.TrimPrefix(strings.TrimSpace(webhook.ChannelName), "#")
			dialogID := "routing-delete-" + strconv.FormatUint(uint64(webhook.ID), 10)
			deleteButton = `<button type="button" class="routing-menu-delete routing-delete-open" data-delete-dialog="` + esc(dialogID) + `" data-channel-name="` + esc(name) + `">` + esc(t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u524a\u9664", "Delete Discord channel")) + `</button>`
			deleteDialogs.WriteString(`<dialog id="` + esc(dialogID) + `" class="routing-delete-dialog"><div data-routing-delete-form style="display:grid;gap:14px"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="webhook_id" value="` + strconv.FormatUint(uint64(webhook.ID), 10) + `"><input type="hidden" name="action" value="delete_current_routing_channel"><h4>` + esc(t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u524a除", "Delete Discord channel")) + `</h4><p>` + esc(t(lang, "\u3053\u306e\u64cd\u4f5c\u306f\u53d6\u308a\u6d88\u305b\u307e\u305b\u3093\u3002", "This action cannot be undone.")) + `</p><label>` + esc(t(lang, "\u78ba\u8a8d\u306e\u305f\u3081\u30c1\u30e3\u30f3\u30cd\u30eb\u540d\u3092\u5165\u529b\u3057\u3066\u304f\u3060\u3055\u3044\u3002", "Type the exact channel name to confirm.")) + `<input data-delete-confirm><small>#` + esc(name) + `</small></label><div class="button-row"><button type="button" class="btn-ghost routing-delete-cancel">` + esc(t(lang, "\u30ad\u30e3\u30f3\u30bb\u30eb", "Cancel")) + `</button><button type="button" class="btn-danger" data-routing-delete-submit disabled>` + esc(t(lang, "\u524a除", "Delete")) + `</button></div></div></dialog>`)
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
		rows.WriteString(`<tr draggable="true" tabindex="0" data-routing-row data-task-type="` + esc(route.TaskTypeID) + `" data-task-type-name="` + esc(route.TaskTypeName) + `" data-existing-targets="` + esc(string(encodedTargets)) + `" data-original-destination="` + strconv.FormatUint(uint64(route.DestinationWebhookID), 10) + `"><td><button type="button" class="routing-select-task" data-select-task aria-pressed="false"><span class="wizard-drag-handle routing-drag-handle" aria-hidden="true">&#8597;</span><span class="routing-task-type-name">` + esc(route.TaskTypeName) + `</span></button><input type="hidden" name="task_type_id" value="` + esc(route.TaskTypeID) + `"><input type="hidden" name="task_type_name" value="` + esc(route.TaskTypeName) + `"></td><td><span aria-hidden="true">&#8594;</span><label class="sr-only">Discord Channel</label><select class="routing-destination-select" name="destination_webhook_id" aria-label="Discord Channel">` + destinationList(route.DestinationWebhookID) + `</select></td><td><details class="routing-row-menu"><summary aria-label="` + esc(menuLabel) + `">&#8942;</summary><div class="routing-row-menu-panel" role="menu"><button type="button" class="routing-remove" data-routing-remove role="menuitem">` + esc(removeLabel) + `</button><button type="button" class="routing-undo" data-routing-undo hidden>` + esc(t(lang, "元に戻す", "Undo")) + `</button>` + deleteButton + `</div></details></td></tr>`)
	}
	rows.WriteString(`<tr data-routing-new-row hidden><td><label class="sr-only">Kitsu Task Type</label><select name="task_type_id" disabled>` + optionList("", false) + `</select><input type="hidden" name="task_type_name" value="" disabled></td><td><span aria-hidden="true">&#8594;</span><label class="sr-only">Discord Channel</label><select name="destination_webhook_id" disabled>` + destinationList(0) + `</select></td><td></td></tr>`)
	addModal := `<dialog data-wfa-add-modal><h4>` + esc(t(lang, "通知先を追加", "Add recipient")) + `</h4><label>` + esc(t(lang, "Discordユーザー", "Discord user")) + `<select data-wfa-user-id><option value="">` + esc(t(lang, "選択", "Select")) + `</option>` + userOptions.String() + `</select></label>` + userAvailability + `<label>` + esc(t(lang, "Discordロール", "Discord role")) + `<select data-wfa-role-id><option value="">` + esc(t(lang, "選択", "Select")) + `</option>` + roleOptions.String() + `</select></label>` + roleAvailability + `<div class="button-row"><button type="button" class="btn-ghost" data-wfa-add-cancel>` + esc(t(lang, "キャンセル", "Cancel")) + `</button><button type="button" class="btn" data-wfa-add-confirm>` + esc(t(lang, "追加", "Add")) + `</button></div></dialog>`
	return `<section class="section-card glass production-routing-editor" aria-labelledby="production-routing-editor-title"><div class="page-heading"><div><h3 id="production-routing-editor-title">` + esc(t(lang, "\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0", "Notification routing")) + `</h3><p class="field-help">` + esc(t(lang, "\u5909\u66f4\u306fApply\u3059\u308b\u307e\u3067\u53cd\u6620\u3055\u308c\u307e\u305b\u3093\u3002", "Changes stay pending until you apply them.")) + `</p></div><span class="status-pill ` + esc(normalizeStatusClass(iaStatusClass(db, p))) + `">` + esc(iaStatusLabel(db, p, lang)) + `</span></div><form method="post" action="` + esc(formAction) + `" data-current-routing-form data-async-notification-apply data-apply-endpoint="/bot/admin/projects?apply_notifications=1" data-project-id="` + esc(p.KitsuProjectID) + `" data-wfa-label="` + esc(t(lang, "WFA通知先", "WFA recipients")) + `" data-stale-message="` + esc(t(lang, "別の編集が保存されました。入力内容は保持されています。内容を確認してください。", "Another edit was saved. Your pending changes are preserved; review them before continuing.")) + `" data-error-message="` + esc(t(lang, "変更を適用できませんでした。入力内容は保持されています。", "Changes could not be applied. Your pending edits are preserved.")) + `" data-empty-wfa="` + esc(t(lang, "このTask Typeは未ルーティングです。Apply後に自動通知先を再確認できます。", "This Task Type is not routed yet. Automatic recipients can be reviewed after Apply.")) + `" data-remove-label="` + esc(t(lang, "解除", "Remove")) + `" data-success-url="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications", r)) + `"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="expected_revision" value="` + esc(model.ProductionNotificationRevision(db, p.ID, p.KitsuProjectID)) + `"><div class="table-wrap wizard-plan-table"><table><thead><tr><th>Kitsu Task Type</th><th>` + esc(channelLabel) + `</th><th></th></tr></thead><tbody data-current-routing-sort>` + rows.String() + `</tbody></table></div><div class="button-row production-routing-editor-actions"><button type="button" class="btn-ghost" data-routing-add>+ ` + esc(t(lang, "Task Type\u3092追加", "Add Task Type")) + `</button></div><section class="production-wfa-edit-panel" data-wfa-detail-panel aria-live="polite"><h4 data-wfa-title>` + esc(t(lang, "WFA通知先", "WFA recipients")) + `</h4><div data-wfa-automatic><strong>` + esc(t(lang, "自動通知先", "Automatic recipients")) + `</strong><p class="field-help">` + esc(t(lang, "自動通知先は読み取り専用です。", "Automatic recipients are read-only.")) + `</p></div><div data-wfa-additional><strong>` + esc(t(lang, "追加通知先", "Additional recipients")) + `</strong><ul data-wfa-target-list></ul><button type="button" class="btn-ghost" data-wfa-add-target>` + esc(t(lang, "通知先を追加", "Add recipient")) + `</button></div></section><div data-apply-message role="status" aria-live="polite"></div><div class="button-row production-routing-editor-actions"><button type="submit" class="btn" data-apply-submit>` + esc(t(lang, "\u5909\u66f4\u3092\u9069\u7528", "Apply")) + `</button><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications", r)) + `" data-pending-cancel>` + esc(t(lang, "\u30ad\u30e3\u30f3\u30bb\u30eb", "Cancel")) + `</a></div></form>` + addModal + deleteDialogs.String() + `<div hidden data-wfa-source>` + readTable + `</div><p class="field-help routing-destructive-note">` + esc(t(lang, "\u3053\u3053\u3067\u5916\u3059\u306e\u306fKitsuSync\u306e\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0\u3060\u3051\u3067\u3059\u3002Kitsu Task Type\u3084Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u306f\u524a\u9664\u3057\u307e\u305b\u3093\u3002", "Removing a route changes only KitsuSync routing; it never deletes Kitsu Task Types or Discord channels.")) + `</p></section>` + currentRoutingEditorScript() + currentRoutingDeleteScript()
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

func currentRoutingEditorScript() string {
	return `<script>(function(){var form=document.querySelector('[data-current-routing-form]');if(!form)return;var body=form.querySelector('[data-current-routing-sort]'),add=form.querySelector('[data-routing-add]'),template=body.querySelector('[data-routing-new-row]'),panel=form.querySelector('[data-wfa-detail-panel]'),source=form.parentElement.querySelector('[data-wfa-source]'),message=form.querySelector('[data-apply-message]'),apply=form.querySelector('[data-apply-submit]'),modal=form.parentElement.querySelector('[data-wfa-add-modal]'),reviewerChanges={};var rowTemplate=template?template.cloneNode(true):null;function rows(){return Array.prototype.slice.call(body.querySelectorAll('[data-routing-row]'))}function visibleRows(){return rows().filter(function(row){return row.dataset.pendingRemoval!=='true'})}function selected(){return body.querySelector('[data-routing-row].selected')||visibleRows()[0]}function delta(id){return reviewerChanges[id]||(reviewerChanges[id]={add_user_ids:[],remove_user_ids:[],add_role_ids:[],remove_role_ids:[]})}function targets(row){try{return JSON.parse(row.dataset.existingTargets||'[]')}catch(_){return []}}function renderTargets(row){var list=panel.querySelector('[data-wfa-target-list]');if(!list)return;list.textContent='';var id=row.dataset.taskType||'',base=targets(row),changes=delta(id),items=base.filter(function(item){var k=item.kind==='role'?'remove_role_ids':'remove_user_ids';return changes[k].indexOf(item.id)<0});['user','role'].forEach(function(kind){var addKey=kind==='role'?'add_role_ids':'add_user_ids';changes[addKey].forEach(function(targetId){var select=kind==='role'?panel.querySelector('[data-wfa-role-id]'):panel.querySelector('[data-wfa-user-id]'),option=select&&select.querySelector('option[value="'+targetId+'"]');items.push({kind:kind,id:targetId,label:option?option.textContent:(kind==='role'?'@'+targetId:targetId)})})});items.forEach(function(item){var li=document.createElement('li'),span=document.createElement('span'),button=document.createElement('button');li.dataset.kind=item.kind;li.dataset.id=item.id;span.textContent=item.label;button.type='button';button.className='btn-ghost';button.dataset.wfaRemoveTarget='';button.textContent=form.dataset.removeLabel;li.appendChild(span);li.appendChild(button);list.appendChild(li)})}function select(row){if(!row||row.dataset.pendingRemoval==='true')return;rows().forEach(function(item){item.classList.toggle('selected',item===row);var button=item.querySelector('[data-select-task]');if(button)button.setAttribute('aria-pressed',item===row?'true':'false')});var title=panel.querySelector('[data-wfa-title]');if(title)title.textContent=(row.dataset.taskTypeName||'')+' · '+form.dataset.wfaLabel;var match=source&&source.querySelector('[data-task-type-id="'+CSS.escape(row.dataset.taskType||'')+'"]'),summary=match&&match.querySelector('.production-wfa-summary'),automatic=panel.querySelector('[data-wfa-automatic]');if(automatic){if(summary&&summary.children[0])automatic.innerHTML=summary.children[0].innerHTML;else automatic.textContent=form.dataset.emptyWfa}renderTargets(row)}function sync(){var chosen={};visibleRows().forEach(function(row){var s=row.querySelector('select[name="task_type_id"]');if(s&&s.value)chosen[s.value]=(chosen[s.value]||0)+1});Array.prototype.forEach.call(body.querySelectorAll('select[name="task_type_id"] option'),function(option){if(!option.value)return;var row=option.closest('[data-routing-row]');option.disabled=!!chosen[option.value]&&(!row||row.querySelector('select[name="task_type_id"]').value!==option.value)});body.querySelectorAll('[data-routing-remove]').forEach(function(button){button.disabled=visibleRows().length<=1});if(add)add.disabled=!!(template&&!template.hidden)}function getTypeName(select){return select.selectedOptions.length?select.selectedOptions[0].textContent:''}function bindRow(row){row.querySelector('[data-select-task]')?.addEventListener('click',function(){select(row)});row.querySelectorAll('select').forEach(function(control){control.addEventListener('change',function(){if(control.name==='task_type_id'){row.dataset.taskType=control.value;row.dataset.taskTypeName=getTypeName(control);var name=row.querySelector('.routing-task-type-name');if(name)name.textContent=row.dataset.taskTypeName;row.dataset.existingTargets=row.dataset.existingTargets||'[]'}sync();select(row)})});row.querySelector('[data-routing-remove]')?.addEventListener('click',function(){if(visibleRows().length<=1)return;row.dataset.pendingRemoval='true';row.classList.add('pending-removal');row.querySelectorAll('select').forEach(function(control){control.disabled=true});row.querySelector('[data-routing-remove]').hidden=true;row.querySelector('[data-routing-undo]').hidden=false;if(selected()===row)select(visibleRows()[0]);sync()});row.querySelector('[data-routing-undo]')?.addEventListener('click',function(){row.dataset.pendingRemoval='false';row.classList.remove('pending-removal');row.querySelectorAll('select').forEach(function(control){control.disabled=false});row.querySelector('[data-routing-remove]').hidden=false;row.querySelector('[data-routing-undo]').hidden=true;select(row);sync()})}rows().forEach(bindRow);if(template){template.querySelector('select[name="task_type_id"]')?.addEventListener('change',function(){if(!this.value)return;template.hidden=false;template.setAttribute('data-routing-row','');template.dataset.taskType=this.value;template.dataset.taskTypeName=getTypeName(this);template.dataset.existingTargets='[]';template.querySelector('.routing-task-type-name')&&(template.querySelector('.routing-task-type-name').textContent=template.dataset.taskTypeName);template.querySelector('[data-select-task]')?.remove();var selectButton=document.createElement('button');selectButton.type='button';selectButton.dataset.selectTask='';selectButton.className='routing-select-task';selectButton.setAttribute('aria-pressed','true');selectButton.textContent=template.dataset.taskTypeName;template.querySelector('td').prepend(selectButton);template.querySelectorAll('select,input').forEach(function(e){e.disabled=false});template.removeAttribute('data-routing-new-row');bindRow(template);select(template);if(rowTemplate){var clone=rowTemplate.cloneNode(true);clone.hidden=true;clone.removeAttribute('data-routing-row');clone.setAttribute('data-routing-new-row','');clone.querySelectorAll('select,input').forEach(function(e){e.disabled=true});body.appendChild(clone);template=clone;if(add)add.hidden=false;template.querySelector('select[name="task_type_id"]')?.addEventListener('change',arguments.callee)}sync()})}if(add)add.addEventListener('click',function(){if(template&&template.hidden){template.hidden=false;template.querySelectorAll('select,input').forEach(function(e){e.disabled=false});add.hidden=true;template.querySelector('select[name="task_type_id"]')?.focus()}});panel.addEventListener('click',function(event){var remove=event.target.closest('[data-wfa-remove-target]');if(remove){var li=remove.closest('li'),row=selected(),id=row.dataset.taskType,kind=li.dataset.kind,targetID=li.dataset.id,c=delta(id),base=targets(row).some(function(item){return item.kind===kind&&item.id===targetID}),key=kind==='role'?'remove_role_ids':'remove_user_ids',addKey=kind==='role'?'add_role_ids':'add_user_ids';if(base){if(c[key].indexOf(targetID)<0)c[key].push(targetID)}else{c[addKey]=c[addKey].filter(function(value){return value!==targetID})}renderTargets(row)}});panel.querySelector('[data-wfa-add-target]')?.addEventListener('click',function(){if(modal&&typeof modal.showModal==='function')modal.showModal()});modal?.querySelector('[data-wfa-add-cancel]')?.addEventListener('click',function(){modal.close()});modal?.querySelector('[data-wfa-add-confirm]')?.addEventListener('click',function(){var row=selected();if(!row)return;var userSelect=modal.querySelector('[data-wfa-user-id]'),roleSelect=modal.querySelector('[data-wfa-role-id]'),kind=roleSelect.value?'role':'user',targetID=kind==='role'?roleSelect.value:userSelect.value,select=kind==='role'?roleSelect:userSelect;if(!targetID)return;var c=delta(row.dataset.taskType),base=targets(row).some(function(item){return item.kind===kind&&item.id===targetID}),key=kind==='role'?'add_role_ids':'add_user_ids',removeKey=kind==='role'?'remove_role_ids':'remove_user_ids';if(base)c[removeKey]=c[removeKey].filter(function(value){return value!==targetID});else if(c[key].indexOf(targetID)<0)c[key].push(targetID);userSelect.value='';roleSelect.value='';modal.close();renderTargets(row)});form.addEventListener('submit',async function(event){event.preventDefault();message.textContent='';var routes=visibleRows().map(function(row){var type=row.querySelector('select[name="task_type_id"]'),destination=row.querySelector('select[name="destination_webhook_id"]');return{task_type_id:type?type.value:row.dataset.taskType,destination_webhook_id:Number(destination?destination.value:0)}});var live=new Set(routes.map(function(route){return route.task_type_id}));var reviewer_changes=Object.keys(reviewerChanges).filter(function(id){var c=reviewerChanges[id];return live.has(id)&&(c.add_user_ids.length||c.remove_user_ids.length||c.add_role_ids.length||c.remove_role_ids.length)}).map(function(id){var c=reviewerChanges[id];return{task_type_id:id,add_user_ids:c.add_user_ids,remove_user_ids:c.remove_user_ids,add_role_ids:c.add_role_ids,remove_role_ids:c.remove_role_ids}});var payload={project_id:form.dataset.projectId,expected_revision:form.querySelector('[name="expected_revision"]').value,routes:routes,reviewer_changes:reviewer_changes};apply.disabled=true;try{var response=await fetch(form.dataset.applyEndpoint,{method:'POST',headers:{'Content-Type':'application/json','Accept':'application/json'},body:JSON.stringify(payload),credentials:'same-origin'});if(response.status===409){message.textContent=form.dataset.staleMessage;return}if(!response.ok){message.textContent=form.dataset.errorMessage;return}window.location.assign(form.dataset.successUrl)}catch(_){message.textContent=form.dataset.errorMessage}finally{apply.disabled=false}});rows().forEach(function(row){row.addEventListener('dragstart',function(e){e.dataTransfer.setData('text/plain',row.dataset.taskType);row.classList.add('is-dragging')});row.addEventListener('dragend',function(){row.classList.remove('is-dragging')});row.addEventListener('dragover',function(e){e.preventDefault()});row.addEventListener('drop',function(e){e.preventDefault();var id=e.dataTransfer.getData('text/plain'),dragged=body.querySelector('[data-task-type="'+id+'"]');if(dragged&&dragged!==row){body.insertBefore(dragged,row);sync()}})});select(selected());sync()})();</script>`
}
func currentRoutingDeleteScript() string {
	return `<script>(function(){Array.prototype.forEach.call(document.querySelectorAll('.routing-delete-open'),function(open){var dialog=document.getElementById(open.getAttribute('data-delete-dialog')||'');if(!dialog)return;open.addEventListener('click',function(){dialog.showModal()});dialog.querySelector('.routing-delete-cancel')?.addEventListener('click',function(){dialog.close()});var input=dialog.querySelector('[data-delete-confirm]'),submit=dialog.querySelector('[data-routing-delete-submit]'),expected=open.getAttribute('data-channel-name');input?.addEventListener('input',function(){if(submit)submit.disabled=input.value!==expected});submit?.addEventListener('click',function(e){e.preventDefault();var form=document.createElement('form');form.method='post';form.action=window.location.href;Array.prototype.forEach.call(dialog.querySelectorAll('input[type=hidden]'),function(source){var copy=document.createElement('input');copy.type='hidden';copy.name=source.name;copy.value=source.value;form.appendChild(copy)});var confirmation=document.createElement('input');confirmation.type='hidden';confirmation.name='confirm_name';confirmation.value=input?.value||'';form.appendChild(confirmation);document.body.appendChild(form);form.submit()})})})();</script>`
}
