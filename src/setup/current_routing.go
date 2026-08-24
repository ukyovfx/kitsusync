package setup

import (
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

func handleCurrentIARoutingMutation(w http.ResponseWriter, r *http.Request, lang string, db *gorm.DB) bool {
	if r.Method == http.MethodPost && strings.TrimSpace(r.FormValue("action")) == "delete_current_routing_channel" {
		return handleCurrentIARoutingChannelDelete(w, r, lang, db)
	}
	if r.Method != http.MethodPost || strings.TrimSpace(r.FormValue("action")) != "save_current_production_routing" {
		return false
	}
	productionID := strings.TrimSpace(r.FormValue("project_id"))
	project := model.FindProjectByKitsuID(db, productionID)
	redirect := withLang("/bot/admin/projects", r) + "&project=" + url.QueryEscape(productionID) + "&tab=notifications"
	if project == nil {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	oldRoutes := model.ListProductionNotificationRoutes(db, productionID)
	_ = r.ParseForm()
	taskTypeIDs := r.Form["task_type_id"]
	destinationIDs := r.Form["destination_webhook_id"]
	taskTypeNames := r.Form["task_type_name"]
	known := routingTaskTypesForProduction(productionID)
	knownIDs := make(map[string]struct{}, len(known))
	knownNames := make(map[string]string, len(known))
	for _, taskType := range known {
		knownIDs[taskType.ID] = struct{}{}
		knownNames[taskType.ID] = taskType.Name
	}
	routes := make([]model.ProductionNotificationRoute, 0, len(taskTypeIDs))
	seen := map[string]struct{}{}
	for i, rawID := range taskTypeIDs {
		taskTypeID := strings.TrimSpace(rawID)
		destinationID, err := strconv.ParseUint(strings.TrimSpace(valueAt(destinationIDs, i)), 10, 64)
		if taskTypeID == "" && err != nil {
			continue
		}
		if taskTypeID == "" || err != nil || destinationID == 0 {
			http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
			return true
		}
		if _, exists := seen[taskTypeID]; exists {
			http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
			return true
		}
		seen[taskTypeID] = struct{}{}
		if len(known) > 0 {
			if _, ok := knownIDs[taskTypeID]; !ok {
				http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
				return true
			}
		}
		name := strings.TrimSpace(valueAt(taskTypeNames, i))
		if name == "" {
			name = knownNames[taskTypeID]
		}
		routes = append(routes, model.ProductionNotificationRoute{TaskTypeID: taskTypeID, TaskTypeName: name, DestinationWebhookID: uint(destinationID)})
	}
	if issues := model.ValidateProductionNotificationConfig(db, productionID, routes); len(issues) > 0 {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true}, routes); err != nil {
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	if err := syncCurrentRoutingDiscordOrder(*project, routes, db); err != nil {
		_ = model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true}, oldRoutes)
		http.Redirect(w, r, redirect+"&msg=error", http.StatusSeeOther)
		return true
	}
	http.Redirect(w, r, redirect+"&msg=saved", http.StatusSeeOther)
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

func renderCurrentIARoutingEditorSetupStyle(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	routes := model.ListProductionNotificationRoutes(db, p.KitsuProjectID)
	taskTypes := routingTaskTypesForProduction(p.KitsuProjectID)
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
	formAction := withLang("/bot/admin/projects", r) + "&project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications&edit_routing=1"
	var rows strings.Builder
	for _, route := range routes {
		removeLabel := t(lang, "\u901a\u77e5\u5bfe\u8c61\u304b\u3089\u5916\u3059", "Remove from notifications")
		deleteButton := ""
		if webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID); webhook != nil && strings.TrimSpace(webhook.DiscordChannelID) != "" && strings.TrimSpace(webhook.ChannelName) != "" {
			name := strings.TrimPrefix(strings.TrimSpace(webhook.ChannelName), "#")
			deleteButton = `<button type="button" class="routing-menu-delete routing-delete-open" data-channel-name="` + esc(name) + `">` + esc(t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u524a\u9664", "Delete Discord channel")) + `</button><dialog class="routing-delete-dialog"><div data-routing-delete-form style="display:grid;gap:14px"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="webhook_id" value="` + strconv.FormatUint(uint64(webhook.ID), 10) + `"><input type="hidden" name="action" value="delete_current_routing_channel"><h4>` + esc(t(lang, "Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u524a除", "Delete Discord channel")) + `</h4><p>` + esc(t(lang, "\u3053\u306e\u64cd\u4f5c\u306f\u53d6\u308a\u6d88\u305b\u307e\u305b\u3093\u3002", "This action cannot be undone.")) + `</p><label>` + esc(t(lang, "\u78ba\u8a8d\u306e\u305f\u3081\u30c1\u30e3\u30f3\u30cd\u30eb\u540d\u3092\u5165\u529b\u3057てください\u3002", "Type the exact channel name to confirm.")) + `<input data-delete-confirm><small>#` + esc(name) + `</small></label><div class="button-row"><button type="button" class="btn-ghost routing-delete-cancel">` + esc(t(lang, "\u30ad\u30e3\u30f3\u30bb\u30eb", "Cancel")) + `</button><button type="submit" class="btn-danger" disabled>` + esc(t(lang, "\u524a除", "Delete")) + `</button></div></div></dialog>`
		}
		menuLabel := t(lang, "行の操作", "Route actions")
		rows.WriteString(`<tr draggable="true" tabindex="0" data-routing-row data-task-type="` + esc(route.TaskTypeID) + `"><td><span class="wizard-drag-handle routing-drag-handle" aria-hidden="true">&#8597;</span><span class="routing-task-type-name">` + esc(route.TaskTypeName) + `</span><input type="hidden" name="task_type_id" value="` + esc(route.TaskTypeID) + `"><input type="hidden" name="task_type_name" value="` + esc(route.TaskTypeName) + `"></td><td><span aria-hidden="true">&#8594;</span><label class="sr-only">Discord Channel</label><select class="routing-destination-select" name="destination_webhook_id" aria-label="Discord Channel">` + destinationList(route.DestinationWebhookID) + `</select></td><td><details class="routing-row-menu"><summary aria-label="` + esc(menuLabel) + `">&#8942;</summary><div class="routing-row-menu-panel" role="menu"><button type="button" class="routing-remove" role="menuitem">` + esc(removeLabel) + `</button>` + deleteButton + `</div></details></td></tr>`)
	}
	rows.WriteString(`<tr data-routing-new-row hidden><td><label class="sr-only">Kitsu Task Type</label><select name="task_type_id" disabled>` + optionList("", false) + `</select><input type="hidden" name="task_type_name" value="" disabled></td><td><span aria-hidden="true">&#8594;</span><label class="sr-only">Discord Channel</label><select name="destination_webhook_id" disabled>` + destinationList(0) + `</select></td><td></td></tr>`)
	return `<section class="section-card glass production-routing-editor" aria-labelledby="production-routing-editor-title"><div class="page-heading"><div><h3 id="production-routing-editor-title">` + esc(t(lang, "\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0", "Notification routing")) + `</h3><p class="field-help">` + esc(t(lang, "\u65b0\u898f\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u8a2d\u5b9a\u3068\u540c\u3058\u304f\u3001Task Type\u3054\u3068\u306e\u9001\u4fe1\u5148\u3092\u4e26\u3079\u3066\u7de8\u96c6\u3057\u307e\u3059\u3002\u4fdd\u5b58\u3059\u308b\u307e\u3067\u5909\u66f4\u306f\u53cd\u6620\u3055\u308c\u307e\u305b\u3093\u3002", "Edit one destination per Task Type using the same staged mapping pattern as New Production Setup. Changes are not applied until saved.")) + `</p></div><span class="status-pill ` + esc(normalizeStatusClass(iaStatusClass(db, p))) + `">` + esc(iaStatusLabel(db, p, lang)) + `</span></div><form method="post" action="` + esc(formAction) + `" data-current-routing-form><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="action" value="save_current_production_routing"><div class="table-wrap wizard-plan-table"><table><thead><tr><th>Kitsu Task Type</th><th>Discord Channel</th><th></th></tr></thead><tbody data-current-routing-sort>` + rows.String() + `</tbody></table></div><div class="button-row production-routing-editor-actions"><button type="button" class="btn-ghost" data-routing-add>+ ` + esc(t(lang, "Task Type\u3092追加", "Add Task Type")) + `</button><button type="submit" class="btn">` + esc(t(lang, "\u5909\u66f4\u3092\u9069\u7528", "Apply changes")) + `</button><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications", r)) + `">` + esc(t(lang, "\u30ad\u30e3\u30f3\u30bb\u30eb", "Cancel")) + `</a></div></form><p class="field-help routing-destructive-note">` + esc(t(lang, "\u3053\u3053\u3067\u5916\u3059\u306e\u306fKitsuSync\u306e\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0\u3060\u3051\u3067\u3059\u3002Kitsu Task Type\u3084Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u306f\u524a\u9664\u3057\u307e\u305b\u3093\u3002", "Remove only the KitsuSync notification route here. Kitsu Task Types and Discord channels are not deleted.")) + `</p></section>` + currentRoutingEditorScript() + currentRoutingDeleteScript()
}

func renderCurrentIARoutingSummaryWithStatus(db *gorm.DB, r *http.Request, p model.Project, lang, class, label string) string {
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
	return `<section class="section-card glass production-routing-summary"><div class="page-heading"><div><h3>` + esc(t(lang, "\u901a\u77e5\u30eb\u30fc\u30c6\u30a3\u30f3\u30b0", "Notification routing")) + `</h3><p class="field-help">` + esc(t(lang, "Kitsu Task Type\u304b\u3089Discord Channel\u3078\u306e\u8aad\u307f\u53d6\u308a\u5c02\u7528\u30de\u30c3\u30d4\u30f3\u30b0\u3067\u3059\u3002", "Read-only mapping from Kitsu Task Type to Discord Channel.")) + `</p></div><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(label) + `</span><a class="btn-ghost" href="` + esc(editURL) + `">` + esc(t(lang, "\u7de8\u96c6", "Edit")) + `</a></div><div class="production-routing-summary-head"><strong>Kitsu Task Type</strong><strong>Discord Channel</strong></div><div class="production-routing-summary-list">` + rows.String() + `</div></section>`
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
	return `<script>(function(){var form=document.querySelector('[data-current-routing-form]');if(!form)return;var body=form.querySelector('[data-current-routing-sort]'),add=form.querySelector('[data-routing-add]'),newRow=form.querySelector('[data-routing-new-row]');var sync=function(){var selected={};Array.prototype.forEach.call(body.querySelectorAll('[data-routing-row] select[name="task_type_id"]'),function(s){if(s.value)selected[s.value]=(selected[s.value]||0)+1});Array.prototype.forEach.call(body.querySelectorAll('select[name="task_type_id"] option'),function(o){if(!o.value)return;var row=o.parentElement.closest('[data-routing-row]');o.disabled=!!selected[o.value]&&(!row||row.querySelector('select[name="task_type_id"]').value!==o.value)});};if(add)add.addEventListener('click',function(){if(newRow){newRow.hidden=false;Array.prototype.forEach.call(newRow.querySelectorAll('select,input'),function(e){e.disabled=false});add.hidden=true;newRow.querySelector('select')?.focus()}});Array.prototype.forEach.call(body.querySelectorAll('[data-routing-row]'),function(row){row.addEventListener('dragstart',function(e){e.dataTransfer.setData('text/plain',row.dataset.taskType);row.classList.add('is-dragging')});row.addEventListener('dragend',function(){row.classList.remove('is-dragging')});row.addEventListener('dragover',function(e){e.preventDefault();row.classList.add('drag-over')});row.addEventListener('dragleave',function(){row.classList.remove('drag-over')});row.addEventListener('drop',function(e){e.preventDefault();row.classList.remove('drag-over');var id=e.dataTransfer.getData('text/plain'),dragged=body.querySelector('[data-task-type="'+id+'"]');if(dragged&&dragged!==row){var rect=row.getBoundingClientRect();body.insertBefore(dragged,e.clientY<rect.top+rect.height/2?row:row.nextElementSibling)}});row.addEventListener('keydown',function(e){if(!e.altKey||!['ArrowUp','ArrowDown'].includes(e.key))return;e.preventDefault();var target=e.key==='ArrowUp'?row.previousElementSibling:row.nextElementSibling;if(target&&target.hasAttribute('data-routing-row')){body.insertBefore(row,e.key==='ArrowUp'?target:target.nextElementSibling);row.focus()}});row.querySelector('.routing-remove')?.addEventListener('click',function(){Array.prototype.forEach.call(row.querySelectorAll('select,input'),function(e){e.disabled=true});row.hidden=true});row.querySelector('select[name="task_type_id"]')?.addEventListener('change',sync)});sync()})();</script>`
}

func currentRoutingDeleteScript() string {
	return `<script>(function(){Array.prototype.forEach.call(document.querySelectorAll('.routing-delete-open'),function(open){var dialog=open.parentElement.querySelector('dialog');if(!dialog)return;open.addEventListener('click',function(){dialog.showModal()});dialog.querySelector('.routing-delete-cancel')?.addEventListener('click',function(){dialog.close()});var input=dialog.querySelector('[data-delete-confirm]'),submit=dialog.querySelector('button[type=submit]'),expected=open.getAttribute('data-channel-name');input?.addEventListener('input',function(){if(submit)submit.disabled=input.value!==expected});submit?.addEventListener('click',function(e){e.preventDefault();var form=document.createElement('form');form.method='post';form.action=window.location.href;Array.prototype.forEach.call(dialog.querySelectorAll('input[type=hidden]'),function(source){var copy=document.createElement('input');copy.type='hidden';copy.name=source.name;copy.value=source.value;form.appendChild(copy)});var confirmation=document.createElement('input');confirmation.type='hidden';confirmation.name='confirm_name';confirmation.value=input?.value||'';form.appendChild(confirmation);document.body.appendChild(form);form.submit()})})})();</script>`
}
