package setup

import (
	discordapi "app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ProductionRoutingHandler is local-only: it never calls a Discord API.
func ProductionRoutingHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lang := currentLang(r)
		message := ""
		selectedID := strings.TrimSpace(r.URL.Query().Get("project"))
		if selectedID == "" {
			selectedID = strings.TrimSpace(r.FormValue("production_id"))
		}
		if r.Method == http.MethodPost && model.IsValidationOnlyProject(db, selectedID) {
			http.Error(w, "validation-only Production is read-only", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			if r.FormValue("action") == "dry_run" {
				message = localizeDryRunMessage(dryRunProductionRoutingAction(db, r, lang), lang)
			} else {
				message = saveProductionRoutingAction(db, r, lang)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, adminPage(lang, tr(lang, "production_routing.title"), r, renderProductionRouting(db, r, selectedID, message)))
	}
}

// Dry-run details are intentionally kept out of the normal user surface. The
// underlying diagnosis remains available through the existing detailed
// diagnostics path, while this message explains the safe next action.
func localizeDryRunMessage(message, lang string) string {
	if strings.Contains(strings.ToLower(message), "dry-run") || strings.Contains(strings.ToLower(message), "skip reason") || strings.Contains(strings.ToLower(message), "channel") {
		return t(lang, "送信せずに確認しました。通知先と実行条件を確認してください。", "Check without sending completed. Review the notification destination and intended action.")
	}
	return message
}

// ProductionRoutingCompatibilityHandler keeps existing bookmarks working
// while Connected Productions remains the single normal management surface.
func ProductionRoutingCompatibilityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := withLang("/bot/admin/projects", r)
		if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" {
			target += "&project=" + url.QueryEscape(project)
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

// WorkflowDiagnosisCompatibilityHandler keeps old links usable while the
// selected Production page owns troubleshooting for normal users.
func WorkflowDiagnosisCompatibilityHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := withLang("/bot/admin/projects", r)
		if project := strings.TrimSpace(r.URL.Query().Get("project")); project != "" {
			target += "&project=" + url.QueryEscape(project)
		}
		http.Redirect(w, r, target+"#troubleshooting", http.StatusSeeOther)
	}
}

func routingTaskTypesForProduction(productionID string) []kitsu.TaskType {
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) == "" || strings.TrimSpace(productionID) == "" {
		return nil
	}
	return filterActiveTaskTypes(kitsu.GetProjectTaskTypes(strings.TrimSpace(productionID)).Each)
}

func filterActiveTaskTypes(taskTypes []kitsu.TaskType) []kitsu.TaskType {
	active := make([]kitsu.TaskType, 0, len(taskTypes))
	for _, taskType := range taskTypes {
		if taskType.Archived || taskType.IsArchived {
			continue
		}
		active = append(active, taskType)
	}
	return active
}

func taskTypeName(taskTypeID string, taskTypes []kitsu.TaskType) string {
	for _, taskType := range taskTypes {
		if taskType.ID == taskTypeID {
			return strings.TrimSpace(taskType.Name)
		}
	}
	return ""
}

func dryRunProductionRoutingAction(db *gorm.DB, r *http.Request, lang string) string {
	productionID := strings.TrimSpace(r.FormValue("production_id"))
	taskTypeID := strings.TrimSpace(r.FormValue("dry_run_task_type_id"))
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil || taskTypeID == "" {
		return tr(lang, "dry_run.select_production_task_type")
	}
	taskTypes := routingTaskTypesForProduction(productionID)
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) != "" && len(taskTypes) == 0 {
		return t(lang, "KitsuからProductionのTask Typeを確認できないため、確認せずに停止しました。", "Dry-run blocked: Production Task Types could not be verified from Kitsu.")
	}
	taskTypeLabel := taskTypeName(taskTypeID, taskTypes)
	if len(taskTypes) > 0 && taskTypeLabel == "" {
		return t(lang, "dry-run をスキップしました: 選択した Task Type は現在の Kitsu metadata では stale です。設定は変更していません。", "Dry-run skipped: the selected Task Type is stale in current Kitsu metadata; configuration was not changed.")
	}
	if taskTypeLabel == "" {
		taskTypeLabel = t(lang, "不明な Task Type", "unknown Task Type")
	}
	config := model.FindProductionNotificationConfig(db, productionID)
	result := fmt.Sprintf(t(lang, "Dry-run | Production ID: %s | Task Type ID: %s | Production: %s | Task Type: %s | matched rule: なし | intended destination: なし | preview: Production=%s; entity/task=未指定; Task Type=%s; status=未指定; user=未指定; link=未指定", "Dry-run | Production ID: %s | Task Type ID: %s | Production: %s | Task Type: %s | matched rule: none | intended destination: none | preview: Production=%s; entity/task=not supplied; Task Type=%s; status=not supplied; user=not supplied; link=not supplied"), productionID, taskTypeID, project.Name, taskTypeLabel, project.Name, taskTypeLabel)
	if config == nil {
		return result + t(lang, " | skip reason: Production は未設定です", " | skip reason: Production is unconfigured")
	}
	if !config.Enabled {
		return result + t(lang, " | skip reason: Production routing は一時停止中です", " | skip reason: Production routing is paused")
	}
	for _, route := range model.ListProductionNotificationRoutes(db, productionID) {
		if route.TaskTypeID != taskTypeID {
			continue
		}
		webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || webhook.KitsuProjectID != productionID || strings.TrimSpace(webhook.WebhookURL) == "" || strings.TrimSpace(webhook.DiscordChannelID) == "" {
			return result + fmt.Sprintf(t(lang, " | matched rule: route-%d | skip reason: 送信先が stale または無効です", " | matched rule: route-%d | skip reason: destination is stale or invalid"), route.ID)
		}
		return fmt.Sprintf(t(lang, "Dry-run | Production ID: %s | Task Type ID: %s | Production: %s | Task Type: %s | matched rule: route-%d | intended destination: 設定済み channel | preview: Production=%s; entity/task=未指定; Task Type=%s; status=未指定; user=未指定; link=未指定 | skip reason: なし", "Dry-run | Production ID: %s | Task Type ID: %s | Production: %s | Task Type: %s | matched rule: route-%d | intended destination: configured channel | preview: Production=%s; entity/task=not supplied; Task Type=%s; status=not supplied; user=not supplied; link=not supplied | skip reason: none"), productionID, taskTypeID, project.Name, taskTypeLabel, route.ID, project.Name, taskTypeLabel)
	}
	return result + t(lang, " | skip reason: Task Type ID route が一致しませんでした", " | skip reason: no Task Type ID route matched")
}

func saveProductionRoutingAction(db *gorm.DB, r *http.Request, lang string) string {
	productionID := strings.TrimSpace(r.FormValue("production_id"))
	project := model.FindProjectByKitsuID(db, productionID)
	if project == nil {
		return t(lang, "選択した Production はローカルに接続されていません。", "The selected Production is not connected locally.")
	}
	action := strings.TrimSpace(r.FormValue("action"))
	config := model.FindProductionNotificationConfig(db, productionID)
	if action == "pause" || action == "resume" {
		if config == nil {
			return t(lang, "この Production に保存済みの routing 設定はありません。", "No saved routing configuration exists for this Production.")
		}
		if action == "resume" {
			routes := model.ListProductionNotificationRoutes(db, productionID)
			if issues := model.ValidateProductionNotificationConfig(db, productionID, routes); len(issues) > 0 {
				return t(lang, "再開できません: ", "Cannot resume: ") + strings.Join(issues, "; ")
			}
		}
		config.Enabled = action == "resume"
		if err := db.Save(config).Error; err != nil {
			return t(lang, "routing 状態を更新できませんでした。", "Could not update routing state.")
		}
		if config.Enabled {
			return t(lang, "routing を再開しました。", "Routing resumed.")
		}
		return t(lang, "routing を一時停止しました。", "Routing paused.")
	}
	if action != "save" {
		return ""
	}
	_ = r.ParseForm()
	taskTypeIDs := r.Form["task_type_id"]
	destinationIDs := r.Form["destination_webhook_id"]
	taskTypeNames := r.Form["task_type_name"]
	knownTaskTypes := routingTaskTypesForProduction(productionID)
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) != "" && len(knownTaskTypes) == 0 {
		return t(lang, "KitsuからProductionのTask Typeを確認できないため、設定を有効化できません。", "Configuration was not activated: Production Task Types could not be verified from Kitsu.")
	}
	knownTaskTypeIDs := make(map[string]struct{}, len(knownTaskTypes))
	for _, taskType := range knownTaskTypes {
		knownTaskTypeIDs[taskType.ID] = struct{}{}
	}
	routes := make([]model.ProductionNotificationRoute, 0, len(taskTypeIDs))
	for i, taskTypeID := range taskTypeIDs {
		taskTypeID = strings.TrimSpace(taskTypeID)
		if taskTypeID == "" && strings.TrimSpace(valueAt(destinationIDs, i)) == "" {
			continue
		}
		destinationID, _ := strconv.ParseUint(strings.TrimSpace(valueAt(destinationIDs, i)), 10, 64)
		displayName := strings.TrimSpace(valueAt(taskTypeNames, i))
		if displayName == "" {
			displayName = taskTypeName(taskTypeID, knownTaskTypes)
		}
		routes = append(routes, model.ProductionNotificationRoute{ProductionID: productionID, TaskTypeID: taskTypeID, TaskTypeName: displayName, DestinationWebhookID: uint(destinationID)})
		if len(knownTaskTypes) > 0 {
			if _, ok := knownTaskTypeIDs[taskTypeID]; !ok {
				return t(lang, "設定は有効化されませんでした: 選択した Task Type は現在の Kitsu metadata では stale です。設定は変更していません。", "Configuration was not activated: selected Task Type is stale in current Kitsu metadata; configuration was not changed.")
			}
		}
	}
	if issues := model.ValidateProductionNotificationConfig(db, productionID, routes); len(issues) > 0 {
		return t(lang, "設定は有効化されませんでした: ", "Configuration was not activated: ") + strings.Join(issues, "; ")
	}
	if err := model.SaveProductionNotificationConfig(db, &model.ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name, Enabled: true}, routes); err != nil {
		return t(lang, "routing 設定を保存できませんでした。", "Could not save routing configuration.")
	}
	return t(lang, "有効な設定を保存し、自動的に有効化しました。", "Valid configuration saved and activated automatically.")
}

func valueAt(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}

func renderProductionRouting(db *gorm.DB, r *http.Request, selectedID, message string) string {
	projects := model.ListProjects(db)
	var selected *model.Project
	for i := range projects {
		if projects[i].KitsuProjectID == selectedID {
			selected = &projects[i]
			break
		}
	}
	taskTypes := routingTaskTypesForProduction(selectedID)
	var b strings.Builder
	b.WriteString(`<div class="section-stack"><div class="section-card glass"><h2>` + esc(tr(currentLang(r), "production_routing.title")) + `</h2><p class="hint">` + esc(tr(currentLang(r), "production_routing.description")) + `</p>`)
	if message != "" {
		b.WriteString(`<div class="notice" role="status" aria-live="polite">` + esc(message) + `</div>`)
	}
	lang := currentLang(r)
	b.WriteString(`<form method="get"><label for="production-select">Production <select id="production-select" name="project" onchange="this.form.submit()"><option value="">` + esc(tr(lang, "production_routing.select_production")) + `</option>`)
	for _, project := range projects {
		selectedAttr := ""
		if project.KitsuProjectID == selectedID {
			selectedAttr = " selected"
		}
		b.WriteString(`<option value="` + esc(project.KitsuProjectID) + `"` + selectedAttr + `>` + esc(project.Name) + `</option>`)
	}
	b.WriteString(`</select></label></form></div>`)
	if selected == nil {
		b.WriteString(`<div class="section-card glass"><p class="hint">` + esc(tr(lang, "production_routing.no_selection")) + `</p></div></div>`)
		return b.String()
	}
	config := model.FindProductionNotificationConfig(db, selected.KitsuProjectID)
	state := "unconfigured"
	if config != nil {
		if config.Enabled {
			state = "enabled"
		} else {
			state = "paused"
		}
	}
	stateLabel := map[string]string{"unconfigured": tr(lang, "production_routing.unconfigured"), "enabled": tr(lang, "production_routing.enabled"), "paused": tr(lang, "production_routing.paused")}[state]
	b.WriteString(`<div class="section-card glass"><h3>` + esc(selected.Name) + `</h3><p>` + esc(tr(lang, "production_routing.status")) + `: <strong class="status-text">` + esc(stateLabel) + `</strong></p><details><summary>` + esc(tr(lang, "production_routing.advanced")) + `</summary><p class="hint">Production ID: <code>` + esc(selected.KitsuProjectID) + `</code></p></details>`)
	b.WriteString(`<form method="post"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="save"><table><thead><tr><th>` + esc(tr(lang, "production_routing.task_type")) + `</th><th>` + esc(tr(lang, "production_routing.display_name")) + `</th><th>` + esc(tr(lang, "production_routing.destination")) + `</th></tr></thead><tbody>`)
	for _, route := range model.ListProductionNotificationRoutes(db, selected.KitsuProjectID) {
		b.WriteString(routingRow(lang, selected.KitsuProjectID, route, db, taskTypes))
	}
	b.WriteString(routingRow(lang, selected.KitsuProjectID, model.ProductionNotificationRoute{}, db, taskTypes))
	b.WriteString(`</tbody></table><p class="hint">` + esc(t(lang, "Task Type の選択肢は現在の Kitsu metadata から取得します。ID は内部で使うため、通常の設定で入力する必要はありません。", "Task Type choices come from current Kitsu metadata. IDs are used internally and are not required for normal setup.")) + `</p><button class="btn" type="submit">` + esc(tr(lang, "production_routing.save")) + `</button></form>`)
	b.WriteString(`<form method="post" style="margin-top:12px"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="dry_run"><label for="dry-run-task-type">` + esc(t(lang, "dry-run 用 Task Type", "Task Type for dry-run")) + ` <select id="dry-run-task-type" name="dry_run_task_type_id"><option value="">` + esc(tr(lang, "production_routing.select_task_type")) + `</option>`)
	for _, taskType := range taskTypes {
		b.WriteString(`<option value="` + esc(taskType.ID) + `">` + esc(taskType.Name) + `</option>`)
	}
	b.WriteString(`</select></label><button class="btn secondary" type="submit">` + esc(tr(lang, "production_routing.dry_run")) + `</button></form>`)
	if config != nil {
		action, label := "pause", tr(lang, "production_routing.pause")
		if !config.Enabled {
			action, label = "resume", tr(lang, "production_routing.resume")
		}
		b.WriteString(`<form method="post" style="margin-top:12px"><input type="hidden" name="production_id" value="` + esc(selected.KitsuProjectID) + `"><input type="hidden" name="action" value="` + action + `"><button class="btn secondary" type="submit">` + label + `</button></form>`)
	}
	b.WriteString(`</div><div class="section-card glass"><h3>` + esc(tr(lang, "production_routing.diagnoses")) + `</h3>`)
	diagnoses := model.ListNotificationRoutingDiagnoses(db, selected.KitsuProjectID, 20)
	if len(diagnoses) == 0 {
		b.WriteString(`<p class="hint">` + esc(tr(lang, "production_routing.no_diagnoses")) + `</p>`)
	} else {
		for _, diagnosis := range diagnoses {
			b.WriteString(`<p><code>` + esc(diagnosis.TaskTypeID) + `</code>: ` + esc(diagnosis.Detail) + `</p>`)
		}
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

func routingRow(lang, projectID string, route model.ProductionNotificationRoute, db *gorm.DB, taskTypes []kitsu.TaskType) string {
	var b strings.Builder
	fieldID := "task-type-new"
	if route.ID != 0 {
		fieldID = "task-type-" + strconv.FormatUint(uint64(route.ID), 10)
	}
	b.WriteString(`<tr><td><label class="sr-only" for="` + fieldID + `">` + esc(tr(lang, "production_routing.task_type")) + `</label><select id="` + fieldID + `" name="task_type_id"><option value="">` + esc(tr(lang, "production_routing.select_task_type")) + `</option>`)
	found := false
	for _, taskType := range taskTypes {
		selected := ""
		if taskType.ID == route.TaskTypeID {
			selected, found = " selected", true
		}
		b.WriteString(`<option value="` + esc(taskType.ID) + `"` + selected + `>` + esc(taskType.Name) + `</option>`)
	}
	if route.TaskTypeID != "" && !found {
		b.WriteString(`<option value="` + esc(route.TaskTypeID) + `" selected>` + esc(t(lang, "stale Task Type reference", "Stale Task Type reference")) + `</option>`)
	}
	b.WriteString(`</select><input type="hidden" name="task_type_name" value="` + esc(route.TaskTypeName) + `"></td><td>` + esc(route.TaskTypeName) + `</td><td><label class="sr-only">` + esc(tr(lang, "production_routing.destination")) + `</label><select name="destination_webhook_id"><option value="">` + esc(t(lang, "送信先を選択", "Select destination")) + `</option>`)
	for _, webhook := range model.ListProjectWebhooks(db, projectID) {
		selected := ""
		if webhook.ID == route.DestinationWebhookID {
			selected = " selected"
		}
		label := webhook.ChannelName
		if label == "" {
			label = "Configured destination"
		}
		b.WriteString(`<option value="` + strconv.FormatUint(uint64(webhook.ID), 10) + `"` + selected + `>` + esc(label) + `</option>`)
	}
	b.WriteString(`</select></td></tr>`)
	return b.String()
}

// renderCurrentIARoutingEditor is the in-context editor for a connected
// Production. The compatibility route remains available for old bookmarks,
// but Current IA users edit the stable ProductionID+TaskTypeID mappings here.
func renderCurrentIARoutingEditor(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	taskTypes := routingTaskTypesForProduction(p.KitsuProjectID)
	routes := model.ListProductionNotificationRoutes(db, p.KitsuProjectID)
	var rows strings.Builder
	for _, route := range routes {
		rows.WriteString(routingRow(lang, p.KitsuProjectID, route, db, taskTypes))
	}
	rows.WriteString(routingRow(lang, p.KitsuProjectID, model.ProductionNotificationRoute{}, db, taskTypes))
	return `<section class="section-card glass"><h3>` + esc(tr(lang, "production_routing.current_editor")) + `</h3><p class="field-help">` + esc(t(lang, "Kitsu Task TypeごとにDiscordチャンネルを1つ設定します。", "Edit one Discord channel for each Kitsu Task Type.")) + `</p><form method="post" action="` + esc(withLang("/bot/admin/production-routing?project="+url.QueryEscape(p.KitsuProjectID), r)) + `"><input type="hidden" name="production_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="action" value="save"><table><thead><tr><th>` + esc(tr(lang, "production_routing.kitsu_task_type")) + `</th><th>` + esc(tr(lang, "production_routing.display_name")) + `</th><th>` + esc(tr(lang, "production_routing.discord_channel")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table><button class="btn" type="submit">` + esc(tr(lang, "production_routing.save")) + `</button></form></section>`
}

func renderCurrentIARoutingSummary(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	var rows strings.Builder
	for _, route := range model.ListProductionNotificationRoutes(db, p.KitsuProjectID) {
		channel := t(lang, "未設定", "Not configured")
		if webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID); webhook != nil && strings.TrimSpace(webhook.ChannelName) != "" {
			channel = "#" + strings.TrimPrefix(strings.TrimSpace(webhook.ChannelName), "#")
		}
		rows.WriteString(`<div class="production-routing-summary-row"><strong>` + esc(route.TaskTypeName) + `</strong><span aria-hidden="true">→</span><span>` + esc(channel) + `</span></div>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<p class="field-help">` + esc(t(lang, "通知ルーティングはまだ設定されていません。", "No notification routing is configured.")) + `</p>`)
	}
	editURL := withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications&edit_routing=1", r)
	return `<section class="section-card glass production-routing-summary"><div class="page-heading"><div><h3>` + esc(t(lang, "通知ルーティング", "Notification routing")) + `</h3><p class="field-help">` + esc(t(lang, "Kitsu Task TypeからDiscord Channelへの読み取り専用マッピング", "Read-only mapping from Kitsu Task Type to Discord Channel.")) + `</p></div><a class="btn-ghost" href="` + esc(editURL) + `">` + esc(t(lang, "編集", "Edit")) + `</a></div><div class="production-routing-summary-head"><strong>` + esc(t(lang, "Kitsu Task Type", "Kitsu Task Type")) + `</strong><strong>` + esc(t(lang, "Discord Channel", "Discord Channel")) + `</strong></div><div class="production-routing-summary-list">` + rows.String() + `</div></section>`
}

func renderCurrentIANotificationPreview(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	taskTypes := routingTaskTypesForProduction(p.KitsuProjectID)
	selectedID := strings.TrimSpace(r.URL.Query().Get("preview_task_type_id"))
	var options strings.Builder
	options.WriteString(`<option value="">` + esc(t(lang, "Task Typeを選択", "Select a Task Type")) + `</option>`)
	for _, taskType := range taskTypes {
		selected := ""
		if taskType.ID == selectedID {
			selected = " selected"
		}
		options.WriteString(`<option value="` + esc(taskType.ID) + `"` + selected + `>` + esc(taskType.Name) + `</option>`)
	}
	result := ""
	if selectedID != "" {
		for _, taskType := range taskTypes {
			if taskType.ID != selectedID {
				continue
			}
			destination := t(lang, "未設定", "Not configured")
			for _, route := range model.ListProductionNotificationRoutes(db, p.KitsuProjectID) {
				if route.TaskTypeID != selectedID {
					continue
				}
				if webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID); webhook != nil && strings.TrimSpace(webhook.ChannelName) != "" {
					destination = webhook.ChannelName
				}
				break
			}
			result = renderDeterministicProductionPreview(p, taskType.Name, destination, lang)
			break
		}
	}
	return `<section class="section-card glass"><h3>` + esc(tr(lang, "production_routing.preview")) + `</h3><p class="field-help">` + esc(t(lang, "Task Typeと送信先、実際の通知内容を確認できます。読み取り専用で、Discordには送信しません。", "Review the Task Type, destination, and rendered notification. This is read-only and never sends to Discord.")) + `</p><form method="get" action="` + esc(withLang("/bot/admin/projects", r)) + `" class="form-action-row"><input type="hidden" name="project" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="tab" value="notifications"><label for="production-notification-preview">` + esc(t(lang, "Kitsu Task Type", "Kitsu Task Type")) + `</label><select id="production-notification-preview" name="preview_task_type_id">` + options.String() + `</select><button class="btn-ghost" type="submit">` + esc(t(lang, "表示", "Show preview")) + `</button></form>` + result + `</section>`
}

func renderDeterministicProductionPreview(p model.Project, taskTypeName, destination, lang string) string {
	notificationLang := discordapi.NotificationLanguage(p.Language)
	payload := discordapi.RenderNotificationPayload(discordapi.Template{
		ProjectName:          p.Name,
		GroupName:            p.Name,
		ParentName:           t(lang, "プレビューイベント", "Preview event"),
		TaskName:             t(lang, "サンプルタスク", "Example task"),
		TaskType:             taskTypeName,
		EntityType:           "Asset",
		CurrentStatus:        "WIP",
		StatusUpper:          "WIP",
		StatusMessage:        t(lang, "作業中です", "Work is in progress"),
		ProcessEmoji:         "•",
		NotificationLanguage: notificationLang,
		ChannelName:          destination,
		AssigneesStr:         t(lang, "未設定", "Not set"),
		Color:                0xE85A1A,
	}, "rich")
	embedTitle, embedDescription, embedFooter := "", "", ""
	if len(payload.Embeds) > 0 {
		embedTitle = payload.Embeds[0].Title
		embedDescription = payload.Embeds[0].Description
		embedFooter = payload.Embeds[0].Footer.Text
	}
	if discordapi.NotificationLanguage(lang) == "en" {
		embedDescription = strings.ReplaceAll(embedDescription, "変更者:", "Changed by:")
	}
	return `<div class="notification-preview-result"><dl class="status-list"><div class="status-row"><dt class="status-row-label">` + esc(t(lang, "Kitsu Task Type", "Kitsu Task Type")) + `</dt><dd class="status-row-value">` + esc(taskTypeName) + `</dd></div><div class="status-row"><dt class="status-row-label">` + esc(t(lang, "Discordチャンネル", "Discord channel")) + `</dt><dd class="status-row-value"><span class="status-badge status-badge-neutral">` + esc(destination) + `</span></dd></div><div class="status-row"><dt class="status-row-label">` + esc(t(lang, "通知言語", "Notification language")) + `</dt><dd class="status-row-value">` + esc(notificationLang) + `</dd></div><div class="status-row"><dt class="status-row-label">` + esc(t(lang, "メンション", "Mentions")) + `</dt><dd class="status-row-value">` + esc(t(lang, "このプレビューではなし", "None in this preview")) + `</dd></div></dl><div class="notification-rendered-preview"><h4>` + esc(t(lang, "レンダリング済みDiscordメッセージ", "Rendered Discord message")) + `</h4><p class="field-help">` + esc(t(lang, "決定的レンダラーの結果です。送信は行いません。", "Deterministic renderer output. Nothing is sent.")) + `</p><dl class="status-list"><div class="status-row"><dt class="status-row-label">Title</dt><dd class="status-row-value">` + esc(embedTitle) + `</dd></div><div class="status-row"><dt class="status-row-label">Description</dt><dd class="status-row-value"><pre class="notification-preview-text">` + esc(embedDescription) + `</pre></dd></div><div class="status-row"><dt class="status-row-label">Footer</dt><dd class="status-row-value">` + esc(embedFooter) + `</dd></div></dl></div></div>`
}

func renderLegacyCurrentIANotificationPreview(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	taskTypes := routingTaskTypesForProduction(p.KitsuProjectID)
	selectedID := strings.TrimSpace(r.URL.Query().Get("preview_task_type_id"))
	var options, result strings.Builder
	options.WriteString(`<option value="">` + esc(t(lang, "Task Typeを選択", "Select a Task Type")) + `</option>`)
	for _, taskType := range taskTypes {
		selected := ""
		if taskType.ID == selectedID {
			selected = " selected"
		}
		options.WriteString(`<option value="` + esc(taskType.ID) + `"` + selected + `>` + esc(taskType.Name) + `</option>`)
	}
	if selectedID != "" {
		for _, taskType := range taskTypes {
			if taskType.ID != selectedID {
				continue
			}
			var route *model.ProductionNotificationRoute
			for _, candidate := range model.ListProductionNotificationRoutes(db, p.KitsuProjectID) {
				if candidate.TaskTypeID == selectedID {
					copyRoute := candidate
					route = &copyRoute
					break
				}
			}
			destination := t(lang, "未設定", "Not configured")
			if route != nil {
				if webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID); webhook != nil && strings.TrimSpace(webhook.ChannelName) != "" {
					destination = webhook.ChannelName
				}
			}
			result.WriteString(`<dl class="status-list"><div class="status-row"><dt class="status-row-label">` + esc(t(lang, "送信先", "Destination")) + `</dt><dd class="status-row-value"><span class="status-badge status-badge-neutral">` + esc(destination) + `</span></dd></div><div class="status-row"><dt class="status-row-label">` + esc(t(lang, "内容", "Message")) + `</dt><dd class="status-row-value">` + esc(fmt.Sprintf(t(lang, "Kitsu Task Type「%s」の通知プレビューです。実際のDiscord送信は行いません。", "Read-only notification preview for Kitsu Task Type %q. No Discord message is sent."), taskType.Name)) + `</dd></div></dl>`)
			break
		}
	}
	return `<section class="section-card glass"><h3>` + esc(tr(lang, "production_routing.preview")) + `</h3><p class="field-help">` + esc(t(lang, "Task Typeと送信先を確認する読み取り専用表示です。", "Read-only view of the selected Task Type and destination.")) + `</p><form method="get" action="` + esc(withLang("/bot/admin/projects", r)) + `" class="form-action-row"><input type="hidden" name="project" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="tab" value="notifications"><label for="production-notification-preview">` + esc(t(lang, "Kitsu Task Type", "Kitsu Task Type")) + `</label><select id="production-notification-preview" name="preview_task_type_id">` + options.String() + `</select><button class="btn-ghost" type="submit">` + esc(t(lang, "表示", "Show preview")) + `</button></form>` + result.String() + `</section>`
}
