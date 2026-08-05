package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// These views are presentation-only. Existing POST handlers remain the only
// mutation path for setup, routing, confirmation and destructive operations.
func iaNav(lang string, r *http.Request) string {
	items := []struct{ key, path string }{
		{"ia.dashboard", "/bot/admin"}, {"ia.productions", "/bot/admin/projects"},
		{"ia.new_connection", "/bot/setup"}, {"ia.user_mapping", "/bot/admin/users"},
		{"ia.bot_connection", "/bot/admin/bot"}, {"ia.system_status", "/bot/admin/health"},
		{"ia.audit_log", "/bot/admin/audit"},
	}
	var b strings.Builder
	for _, item := range items {
		b.WriteString(`<a class="nav-chip" href="` + esc(withLang(item.path, r)) + `">` + esc(tr(lang, item.key)) + `</a>`)
	}
	return b.String()
}

// statusSummaryRow is the shared normal-user status presentation. The action
// is supplied as already-rendered HTML so links remain escaped at the call
// site and never become part of the badge itself.
func statusSummaryRow(label, class, value, explanation, actionHTML string) string {
	if class == "bad" {
		class = "danger"
	}
	if class == "warn" {
		class = "warning"
	}
	if class == "ok" {
		class = "success"
	}
	icon := map[string]string{"success": "✓", "warning": "!", "danger": "×", "blocked": "!", "neutral": "•"}[class]
	if icon == "" {
		icon = "•"
	}
	action := ""
	if strings.TrimSpace(actionHTML) != "" {
		action = `<div class="status-row-action">` + actionHTML + `</div>`
	}
	return `<div class="status-row"><dt class="status-row-label">` + esc(label) + `</dt><dd class="status-row-value"><span class="status-badge status-badge-` + esc(class) + `" role="status"><span aria-hidden="true">` + icon + `</span> ` + esc(value) + `</span>` + ifNonEmpty(explanation, `<span class="status-row-explanation">`+esc(explanation)+`</span>`) + `</dd>` + action + `</div>`
}

func ifNonEmpty(value, wrapped string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return wrapped
}

func iaStatus(db *gorm.DB, project model.Project, lang string) (string, string, string) {
	cfg := model.FindProductionNotificationConfig(db, project.KitsuProjectID)
	if cfg == nil {
		return "bad", t(lang, "未設定", "Incomplete"), t(lang, "通知先が設定されていません。通知先を1つ以上設定してください。", "No notification route is configured. Add at least one valid destination.")
	}
	if !cfg.Enabled {
		return "bad", t(lang, "確認が必要", "Needs review"), t(lang, "以前の停止状態が保存されています。通知を再開せず、通知設定と安全性を確認してください。", "A legacy stopped state is saved. Notifications remain blocked until the configuration is reviewed safely.")
	}
	issues := model.ValidateProductionNotificationConfig(db, project.KitsuProjectID, model.ListProductionNotificationRoutes(db, project.KitsuProjectID))
	if len(issues) > 0 {
		return "bad", t(lang, "更新が必要", "Needs attention"), t(lang, "通知先の確認が必要です。内容を確認してから修正してください。", "Notification destinations need attention. Review the details before changing them.")
	}
	return "ok", t(lang, "有効", "Active"), t(lang, "通知先の設定が有効です。", "Notification destinations are active.")
}

func iaReadiness(db *gorm.DB, lang string) (string, string, string) {
	r := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	if r.OverallReady {
		return "ok", t(lang, "接続済み", "Connected"), t(lang, "KitsuとDiscordの接続を利用できます。", "Kitsu and Discord are ready to use.")
	}
	if !r.KitsuConfigured {
		return "bad", t(lang, "対応が必要", "Action required"), t(lang, "Kitsu接続を設定してください。", "Complete Kitsu connection setup.")
	}
	if !r.DiscordConfigured {
		return "bad", t(lang, "対応が必要", "Action required"), t(lang, "Bot接続を設定してください。", "Complete Bot Connection setup.")
	}
	return "warn", t(lang, "確認が必要", "Needs review"), t(lang, "接続状態を確認してください。", "Review the connection state.")
}

func dashboardStatusRow(label, class, value string) string {
	return `<div class="dashboard-status-row"><span class="dashboard-status-label">` + esc(label) + `</span><span class="status-badge status-badge-` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(value) + `</span></div>`
}

func dashboardProblemAction(r *http.Request, p model.Project, lang, hint string) (string, string) {
	path := "/bot/admin/projects?project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications"
	label := t(lang, "通知設定を確認", "Review notification settings")
	lower := strings.ToLower(hint)
	if strings.Contains(lower, "participant") || strings.Contains(lower, "reviewer") || strings.Contains(lower, "checker") {
		path = "/bot/admin/projects?project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=user-settings"
		label = t(lang, "ユーザー設定を確認", "Review user settings")
	}
	return withLang(path, r), label
}

func renderIADashboard(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	projects := model.ListProjects(db)
	var attentionRows, activityRows strings.Builder
	attentionCount := 0
	for _, p := range projects {
		class, _, hint := iaStatus(db, p, lang)
		tab := "overview"
		if class == "bad" {
			tab = "notifications"
		}
		openURL, actionLabel := dashboardProblemAction(r, p, lang, hint)
		if tab == "overview" {
			openURL = withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=overview", r)
		}
		row := `<li class="dashboard-queue-row"><div><strong>` + esc(p.Name) + `</strong><span class="field-help">` + esc(hint) + `</span></div><span class="status-badge status-badge-` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(cleanStatusLabel(lang, class)) + `</span><a class="btn-ghost" href="` + esc(openURL) + `">` + esc(actionLabel) + `</a></li>`
		if class == "bad" {
			attentionRows.WriteString(row)
			attentionCount++
		}
	}
	if attentionRows.Len() == 0 {
		attentionRows.WriteString(`<li class="muted">` + esc(t(lang, "対応が必要なProductionはありません。", "No Productions need attention.")) + `</li>`)
	}
	failureCount := 0
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, log := range model.ListAuditLogs(db, 5) {
		name := strings.TrimSpace(log.ProjectName)
		if name == "" {
			name = strings.TrimSpace(log.ProjectID)
		}
		result := t(lang, "謌仙粥", "Success")
		resultClass := "success"
		if !log.Success {
			result = t(lang, "要確認", "Needs review")
			resultClass = "warning"
		}
		activityRows.WriteString(`<li class="activity-row dashboard-activity-row"><time class="activity-date" datetime="` + esc(log.CreatedAt.Format(time.RFC3339)) + `">` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</time><strong>` + esc(iaActivityAction(lang, log)) + `</strong><span class="activity-production">` + esc(name) + `</span><span class="status-badge status-badge-` + resultClass + ` activity-result">` + esc(result) + `</span></li>`)
	}
	for _, log := range model.ListAuditLogs(db, 200) {
		if !log.Success && log.CreatedAt.After(cutoff) {
			failureCount++
		}
	}
	if activityRows.Len() == 0 {
		activityRows.WriteString(`<li class="muted">No recent activity.</li>`)
	}
	readinessClass, readinessLabel, readinessHint := iaReadiness(db, lang)
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	nextActionURL := withLang("/bot/setup", r)
	nextActionLabel := tr(lang, "ia.new_connection")
	if readiness.KitsuConfigured && !readiness.DiscordConfigured {
		nextActionURL = withLang("/bot/admin/bot", r)
		nextActionLabel = t(lang, "Bot接続を設定", "Set up Bot Connection")
	} else if readiness.KitsuConfigured && readiness.DiscordConfigured && !readiness.OverallReady {
		nextActionURL = withLang("/bot/admin/projects", r)
		nextActionLabel = tr(lang, "ia.production_list")
	}
	botState := statusText(lang, readiness.DiscordConfigured)
	statusExplanation := readinessHint
	statusAction := ""
	if !readiness.OverallReady {
		statusAction = `<div class="button-row"><a class="btn" href="` + esc(nextActionURL) + `">` + esc(nextActionLabel) + `</a></div>`
	}
	quickActions := ""
	if readiness.OverallReady {
		quickActions = `<a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(tr(lang, "ia.production_list")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(tr(lang, "ia.user_mapping")) + `</a>`
	}
	body := `<div class="section-stack">` +
		`<section class="dashboard-intro"><div><h1>` + esc(tr(lang, "ia.dashboard")) + `</h1><p class="hint">` + esc(t(lang, "KitsuSyncの接続状態と、対応が必要な項目を確認できます。", "Review KitsuSync connection state and items that need attention.")) + `</p></div><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/health", r)) + `">` + esc(t(lang, "状態を更新", "Refresh status")) + `</a></div></section>` +
		`<section class="dashboard-summary-grid" aria-label="` + esc(t(lang, "概要", "Summary")) + `"><div class="metric-card"><div class="metric-label">` + esc(t(lang, "接続済みProduction", "Connected Productions")) + `</div><div class="metric-value">` + fmt.Sprint(len(projects)) + `</div><p class="field-help">` + esc(t(lang, "現在確認できるProduction", "Productions currently visible")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "対応が必要", "Needs attention")) + `</div><div class="metric-value">` + fmt.Sprint(attentionCount) + `</div><p class="field-help">` + esc(t(lang, "安全に通知できる状態か確認が必要です。", "Review before notifications can be safely delivered.")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "直近24時間の通知失敗", "Notification failures, last 24 hours")) + `</div><div class="metric-value">` + fmt.Sprint(failureCount) + `</div><p class="field-help">` + esc(t(lang, "記録された失敗イベント", "Recorded failure events")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "システム状態", "System status")) + `</div><div class="metric-value"><span class="status-pill ` + readinessClass + `">` + esc(readinessLabel) + `</span></div><p class="field-help" role="status">` + esc(readinessHint) + `</p></div></section>` +
		`<section class="section-card glass dashboard-queue" aria-labelledby="dashboard-attention"><div class="page-heading"><div><h2 id="dashboard-attention">` + esc(t(lang, "対応が必要なProduction", "Productions needing attention")) + `</h2><p class="hint">` + esc(t(lang, "通知が安全に利用できない理由と、次の操作を示します。", "Each row explains why notifications are unavailable and what to do next.")) + `</p></div><span class="status-pill ` + map[bool]string{true: "bad", false: "ok"}[attentionCount > 0] + `">` + fmt.Sprint(attentionCount) + `</span></div><ul class="list-tight">` + attentionRows.String() + `</ul></section>` +
		`<div class="dashboard-lower-grid"><section class="section-card glass" aria-labelledby="dashboard-activity"><h2 id="dashboard-activity">` + esc(tr(lang, "ia.activity")) + `</h2><div class="activity-columns" aria-hidden="true"><span>` + esc(t(lang, "日時", "Date and time")) + `</span><span>` + esc(t(lang, "操作", "Action")) + `</span><span>` + esc(t(lang, "Production", "Production")) + `</span><span>` + esc(t(lang, "結果", "Result")) + `</span></div><ul class="activity-list" role="log">` + activityRows.String() + `</ul></section><div class="dashboard-side-stack"><section class="section-card glass" aria-labelledby="dashboard-system"><h2 id="dashboard-system">` + esc(t(lang, "通知システム", "Notification system")) + `</h2><div class="dashboard-status-list">` + dashboardStatusRow(t(lang, "Kitsu接続", "Kitsu connection"), map[bool]string{true: "success", false: "danger"}[readiness.KitsuConfigured], statusText(lang, readiness.KitsuConfigured)) + dashboardStatusRow(t(lang, "Discord Bot", "Discord Bot"), map[bool]string{true: "success", false: "blocked"}[readiness.DiscordConfigured], botState) + dashboardStatusRow(t(lang, "通知状態", "Notification state"), map[bool]string{true: "success", false: "blocked"}[readiness.OverallReady], statusTextOverall(lang, readiness.OverallReady)) + `</div><p class="field-help" role="status">` + esc(statusExplanation) + `</p>` + statusAction + `</section><section class="section-card glass dashboard-quick" aria-labelledby="dashboard-quick"><h2 id="dashboard-quick">` + esc(t(lang, "クイック操作", "Quick actions")) + `</h2><div class="button-row">` + ifNonEmpty(quickActions, quickActions) + `</div></section></div></div>`
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func renderIAProductionList(w http.ResponseWriter, r *http.Request, db *gorm.DB, fallbackGuildID string) {
	lang := currentLang(r)
	if selectedID := strings.TrimSpace(r.URL.Query().Get("project")); selectedID != "" {
		if p := model.FindProjectByKitsuID(db, selectedID); p != nil {
			renderIASelectedProduction(w, r, db, *p, fallbackGuildID)
			return
		}
	}
	var rows strings.Builder
	for _, p := range model.ListProjects(db) {
		class, label, hint := iaStatus(db, p, lang)
		rows.WriteString(fmt.Sprintf(`<article class="section-card glass production-list-item"><div><h2>%s</h2><p class="field-help">%s</p></div><div class="production-list-state"><span class="status-pill %s">%s</span><span class="field-help">%s</span></div><a class="btn" href="%s">%s</a></article>`, esc(p.Name), esc(t(lang, "現在の状態", "Current state")), class, esc(label), esc(hint), esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID), r)), esc(t(lang, "Productionを開く", "Open Production"))))
	}
	if rows.Len() == 0 {
		rows.WriteString(emptyState("-", t(lang, "Productionがありません", "No Productions"), t(lang, "新しいProductionを接続してください。", "Connect a new Production.")))
	}
	body := `<div class="section-stack"><section class="section-card glass"><p class="hint">` + esc(t(lang, "Production一覧で状態を確認し、選択したProductionを開きます。設定は選択後に表示されます。", "Use this list to review states and open one Production. Settings appear only after selection.")) + `</p></section>` + rows.String() + `</div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.production_list"), r, body))
}

func iaActivityAction(lang string, log model.AuditLog) string {
	if log.DiscordMsgID != "" {
		return t(lang, "通知送信", "Notification sent")
	}
	combined := strings.ToLower(log.EntityName + " " + log.OldStatus + " " + log.NewStatus)
	if strings.Contains(combined, "pause") {
		return t(lang, "通知一時停止", "Notifications paused")
	}
	if strings.Contains(combined, "resume") {
		return t(lang, "通知再開", "Notifications resumed")
	}
	if !log.Success {
		return t(lang, "復旧確認", "Recovery review")
	}
	return t(lang, "設定変更", "Configuration changed")
}

func renderIASelectedProduction(w http.ResponseWriter, r *http.Request, db *gorm.DB, p model.Project, fallbackGuildID string) {
	lang := currentLang(r)
	tab := selectedProductionTab(r.URL.Query().Get("tab"))
	class, label, hint := iaStatus(db, p, lang)
	serverName := projectDiscordServerName(db, p, lang)
	tabs := []struct{ id, key string }{{"overview", "ia.overview"}, {"notifications", "ia.notifications"}, {"user-settings", "ia.user_settings"}, {"storage-settings", "ia.storage_settings"}, {"activity", "ia.activity"}, {"troubleshooting", "ia.troubleshooting"}, {"advanced", "ia.advanced"}, {"danger-zone", "ia.danger"}}
	var tabLinks strings.Builder
	for _, item := range tabs {
		selected := item.id == tab
		selectedAttr := "false"
		if selected {
			selectedAttr = "true"
		}
		link := withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab="+url.QueryEscape(item.id), r)
		tabLinks.WriteString(`<a id="tab-` + esc(item.id) + `" role="tab" aria-selected="` + selectedAttr + `" aria-controls="panel-` + esc(item.id) + `" class="section-link` + map[bool]string{true: " active", false: ""}[selected] + `" href="` + esc(link) + `" tabindex="` + map[bool]string{true: "0", false: "-1"}[selected] + `">` + esc(tr(lang, item.key)) + `</a>`)
	}
	header := `<div class="production-context"><div class="page-heading"><div><div class="eyebrow">` + esc(tr(lang, "ia.productions")) + `</div><h1>` + esc(p.Name) + `</h1><p class="hint">` + esc(t(lang, "選択中のProduction", "Selected Production")) + `</p></div><span class="status-pill ` + esc(class) + `">` + esc(label) + `</span></div><nav class="section-nav production-tabs" role="tablist" aria-label="` + esc(t(lang, "Productionのセクション", "Production sections")) + `">` + tabLinks.String() + `</nav>`
	body := header + `<section id="panel-` + esc(tab) + `" role="tabpanel" aria-labelledby="tab-` + esc(tab) + `" tabindex="0" class="section-stack production-tabpanel">` + renderSelectedProductionPanel(db, r, p, lang, tab, class, label, hint, serverName) + `</section></div>`
	body += `<script>(function(){var list=document.querySelector('[role="tablist"]');if(!list)return;var tabs=Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));list.addEventListener('keydown',function(e){var i=tabs.indexOf(document.activeElement);if(i<0)return;var n=i;if(e.key==='ArrowRight')n=(i+1)%tabs.length;if(e.key==='ArrowLeft')n=(i-1+tabs.length)%tabs.length;if(e.key==='Home')n=0;if(e.key==='End')n=tabs.length-1;if(n!==i){e.preventDefault();tabs[n].focus();tabs[n].click()}})})();</script>`
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func projectDiscordServerName(db *gorm.DB, p model.Project, lang string) string {
	if strings.TrimSpace(p.DiscordGuildID) == "" {
		return t(lang, "未接続", "Not connected")
	}
	var setting model.ProjectSetting
	for _, key := range []string{"discord_server_name", "discord.guild_name"} {
		if db != nil && db.Where("project_id = ? AND key = ?", p.ID, key).First(&setting).Error == nil && strings.TrimSpace(setting.Value) != "" {
			return strings.TrimSpace(setting.Value)
		}
	}
	return t(lang, "接続済みDiscordサーバー", "Connected Discord server")
}

func selectedProductionTab(raw string) string {
	switch raw {
	case "notifications", "user-settings", "storage-settings", "activity", "troubleshooting", "advanced", "danger-zone":
		return raw
	default:
		return "overview"
	}
}

func renderSelectedProductionPanel(db *gorm.DB, r *http.Request, p model.Project, lang, tab, class, label, hint, serverName string) string {
	switch tab {
	case "notifications":
		return renderSelectedProductionNotifications(db, r, p, lang, class, label, hint)
	case "user-settings":
		return renderSelectedProductionUserSettings(db, r, p, lang)
	case "storage-settings":
		return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.storage_settings")) + `</h2><p class="hint">` + esc(t(lang, "このProductionの保存先とリンクを管理します。", "Manage storage destinations and links for this Production.")) + `</p><form method="POST" action="` + esc(withLang("/bot/admin/drive", r)) + `" class="form-stack"><input type="hidden" name="kitsu_project_id" value="` + esc(p.KitsuProjectID) + `"><label for="storage-url">` + esc(t(lang, "保存先リンク", "Storage link")) + `</label><input id="storage-url" type="url" name="storage_url" value="` + esc(p.StorageURL) + `"><div class="button-row"><button class="btn" type="submit">` + esc(t(lang, "保存", "Save")) + `</button></div></form></section>`
	case "activity":
		return renderSelectedProductionActivity(db, p, lang)
	case "troubleshooting":
		return renderSelectedProductionTroubleshooting(db, p, lang)
	case "advanced":
		return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.advanced")) + `</h2><dl class="detail-list"><dt>Production ID</dt><dd><code>` + esc(p.KitsuProjectID) + `</code></dd><dt>Discord server ID</dt><dd><code>` + esc(p.DiscordGuildID) + `</code></dd><dt>Category ID</dt><dd><code>` + esc(p.DiscordCategoryID) + `</code></dd></dl></section>`
	case "danger-zone":
		return renderSelectedProductionDanger(r, p, lang)
	default:
		problem := t(lang, "現在の問題はありません", "No current problem")
		nextAction := t(lang, "通常どおり利用できます", "No action required")
		nextActionHTML := ""
		serverActionHTML := ""
		if class != "ok" {
			problem = cleanStatusLabel(lang, class)
			nextAction = hint
			nextActionHTML = `<a class="btn" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications", r)) + `">` + esc(tr(lang, "ia.notifications")) + `</a>`
		}
		if strings.TrimSpace(p.DiscordGuildID) != "" {
			serverChangeURL := withLang("/bot/setup?project="+url.QueryEscape(p.KitsuProjectID)+"&wizard_step=3", r)
			serverActionHTML = `<a class="btn-ghost" href="` + esc(serverChangeURL) + `">` + esc(t(lang, "Discordサーバーを変更する", "Review Discord server change")) + `</a>`
		}
		return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.overview")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "Productionの状態", "Production state"), normalizeStatusClass(class), cleanStatusLabel(lang, class), hint, "") + statusSummaryRow(t(lang, "Discordサーバー", "Discord server"), map[bool]string{true: "success", false: "blocked"}[strings.TrimSpace(p.DiscordGuildID) != ""], serverName, t(lang, "変更は確認画面で確認します。", "Changes are reviewed in the confirmation flow."), serverActionHTML) + statusSummaryRow(t(lang, "現在の問題", "Current problem"), normalizeStatusClass(class), problem, "", "") + statusSummaryRow(t(lang, "次の操作", "Next action"), normalizeStatusClass(class), nextAction, "", nextActionHTML) + `</dl><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=user-settings", r)) + `">` + esc(tr(lang, "ia.user_settings")) + `</a></div></section>`
	}
}

func renderSelectedProductionNotifications(db *gorm.DB, r *http.Request, p model.Project, lang, class, label, hint string) string {
	actionURL := withLang("/bot/admin/production-routing?project="+url.QueryEscape(p.KitsuProjectID), r)
	actionHTML := `<a class="btn" href="` + esc(actionURL) + `">` + esc(t(lang, "通知設定を確認", "Review notification settings")) + `</a>`
	var mappings strings.Builder
	var taskTypeOptions strings.Builder
	for _, taskType := range routingTaskTypes() {
		taskTypeOptions.WriteString(`<option value="` + esc(taskType.ID) + `">` + esc(taskType.Name) + `</option>`)
	}
	for _, m := range model.ListProductionChannelMappings(db, p.KitsuProjectID) {
		mappings.WriteString(`<li><strong>` + esc(m.TaskTypeName) + `</strong><span>` + esc(m.ChannelName) + `</span></li>`)
	}
	if mappings.Len() == 0 {
		mappings.WriteString(`<li class="muted">` + esc(t(lang, "チャンネル設定はありません。", "No channel settings yet.")) + `</li>`)
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.notifications")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "通知状態", "Notification state"), normalizeStatusClass(class), cleanStatusLabel(lang, class), hint, actionHTML) + `</dl><form method="POST" action="` + esc(actionURL) + `" class="section-card glass form-stack"><input type="hidden" name="production_id" value="` + esc(p.KitsuProjectID) + `"><input type="hidden" name="action" value="dry_run"><label for="selected-production-dry-run">` + esc(t(lang, "確認するTask Type", "Task Type to check")) + `</label><div class="form-action-row"><select id="selected-production-dry-run" name="dry_run_task_type_id"><option value="">` + esc(t(lang, "Task Typeを選択", "Select a Task Type")) + `</option>` + taskTypeOptions.String() + `</select><button class="btn" type="submit">` + esc(tr(lang, "ia.check_without_sending")) + `</button></div><p class="field-help">` + esc(t(lang, "送信せずに確認します。Discordメッセージは送信されません。", "This check sends no Discord message.")) + `</p></form><h3>` + esc(t(lang, "Task TypeとDiscordチャンネル", "Task Type to Discord channel settings")) + `</h3><ul class="mapping-list">` + mappings.String() + `</ul></section>`
}

func renderSelectedProductionUserSettings(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	global := map[string]model.UserMap{}
	for _, u := range model.ListUserMap(db) {
		global[strings.ToLower(strings.TrimSpace(u.KitsuName))] = u
	}
	var participants, roles strings.Builder
	for _, u := range model.ListProjectUserMaps(db, p.ID) {
		identity := t(lang, "未対応", "Not mapped")
		action := `<a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(tr(lang, "ia.user_mapping")) + `</a>`
		if g, ok := global[strings.ToLower(strings.TrimSpace(u.KitsuName))]; ok && strings.TrimSpace(g.DiscordID) != "" {
			identity = t(lang, "対応付け済み", "Mapped")
			action = ""
		}
		participants.WriteString(`<li><strong>` + esc(u.KitsuName) + `</strong><span class="status-pill ` + map[bool]string{true: "ok", false: "warn"}[identity != t(lang, "未対応", "Not mapped")] + `">` + esc(identity) + `</span>` + action + `</li>`)
	}
	if participants.Len() == 0 {
		participants.WriteString(`<li class="empty-state"><span class="empty-state-mark" aria-hidden="true">•</span><strong>` + esc(t(lang, "Production参加者はまだ登録されていません。", "No Production participants are registered yet.")) + `</strong><span class="field-help">` + esc(t(lang, "Kitsu側の参加者が登録されると、ここに表示されます。", "Participants appear here when they are registered in Kitsu.")) + `</span></li>`)
	}
	for _, c := range model.ListProjectCheckerMaps(db, p.ID) {
		roles.WriteString(`<li><strong>` + esc(c.TaskType) + `</strong><span>` + esc(c.KitsuName) + `</span></li>`)
	}
	if roles.Len() == 0 {
		roles.WriteString(`<li class="empty-state"><span class="empty-state-mark" aria-hidden="true">•</span><strong>` + esc(t(lang, "Reviewer / Checkerの割り当てはありません。", "No Reviewer / Checker assignments yet.")) + `</strong><span class="field-help">` + esc(t(lang, "Task TypeごとにProduction単位で設定します。", "Assign these roles per Task Type for this Production.")) + `</span></li>`)
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.user_settings")) + `</h2><div class="settings-block"><h3>` + esc(t(lang, "Production参加者", "Production participants")) + `</h3><ul class="mapping-list">` + participants.String() + `</ul></div><div class="settings-block"><h3>` + esc(t(lang, "Reviewer / Checker", "Reviewer / Checker")) + `</h3><ul class="mapping-list">` + roles.String() + `</ul></div><p class="field-help">` + esc(t(lang, "Discordユーザーの対応付けはグローバルなユーザー対応付けで管理します。", "Discord identity correspondence is managed in global User Mapping.")) + `</p><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(tr(lang, "ia.user_mapping")) + `</a></section>`
}

func renderSelectedProductionActivity(db *gorm.DB, p model.Project, lang string) string {
	var rows strings.Builder
	for _, log := range model.ListAuditLogs(db, 40) {
		if log.ProjectName == p.Name {
			result := t(lang, "成功", "Success")
			resultClass := "success"
			if !log.Success {
				result = t(lang, "確認が必要", "Needs review")
				resultClass = "warning"
			}
			rows.WriteString(`<li class="activity-row"><time class="activity-date" datetime="` + esc(log.CreatedAt.Format(time.RFC3339)) + `">` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</time><strong>` + esc(iaActivityAction(lang, log)) + `</strong><span class="status-badge status-badge-` + resultClass + ` activity-result">` + esc(result) + `</span></li>`)
		}
	}
	if rows.Len() == 0 {
		rows.WriteString(`<li class="empty-state"><span class="empty-state-mark" aria-hidden="true">•</span><strong>` + esc(t(lang, "アクティビティはありません。", "No activity yet.")) + `</strong></li>`)
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.activity")) + `</h2><ul class="activity-list" role="log">` + rows.String() + `</ul></section>`
}

func renderSelectedProductionTroubleshooting(db *gorm.DB, p model.Project, lang string) string {
	has := len(model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 10)) > 0
	class, value := "success", t(lang, "問題なし", "No current problems")
	if has {
		class, value = "warning", t(lang, "確認が必要", "Needs review")
	}
	cause := t(lang, "通知設定に問題はありません", "No notification configuration problem was found")
	if has {
		cause = t(lang, "保存された通知先を確認できません", "The saved notification destination could not be verified")
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.troubleshooting")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "現在の問題", "Current problem"), class, value, t(lang, "詳細な診断は下の開示で確認できます。", "Open diagnostic details for technical context."), "") + statusSummaryRow(t(lang, "原因", "Cause"), class, cause, "", "") + statusSummaryRow(t(lang, "次の操作", "Next action"), class, t(lang, "通知設定を確認", "Review notification settings"), "", "") + `</dl><details class="advanced-details"><summary>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</summary><p class="hint">` + esc(t(lang, "技術的な診断情報はここに限定します。", "Technical diagnostic information is limited to this disclosure.")) + `</p></details></section>`
}

func renderSelectedProductionDanger(r *http.Request, p model.Project, lang string) string {
	disconnectPhrase := t(lang, "連携解除", "DISCONNECT")
	deletePhrase := t(lang, "削除", "DELETE")
	return `<details class="advanced-details danger-zone"><summary>` + esc(tr(lang, "ia.danger")) + `</summary><div class="danger-actions"><div class="danger-action-block"><h3>` + esc(tr(lang, "ia.disconnect_production")) + `</h3><p class="hint">` + esc(t(lang, "KitsuSyncの連携だけを解除します。Discord側のリソースは残ります。", "This removes only the KitsuSync connection. Discord resources remain.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "Productionの連携を解除します。Discord側のリソースは残ります。", "This removes the Production connection. Discord resources remain.")) + `" data-require-text="` + esc(disconnectPhrase) + `"><input type="hidden" name="action" value="remove_connection"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-ghost" type="submit">` + esc(tr(lang, "ia.disconnect_production")) + `</button></form></div><div class="danger-action-block"><h3>` + esc(tr(lang, "ia.delete_discord_resources")) + `</h3><p class="hint">` + esc(t(lang, "Discord側のチャンネルとカテゴリを削除します。連携解除とは別の操作です。", "This may delete Discord channels and the category. It is separate from disconnecting the Production.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "Discord側のリソースを削除します。", "This may delete Discord-side resources.")) + `" data-require-text="` + esc(deletePhrase) + `"><input type="hidden" name="action" value="preview_remove_connection_with_discord"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-danger" type="submit">` + esc(tr(lang, "ia.delete_discord_resources")) + `</button></form></div></div></details>`
}

func renderIASelectedProductionLegacy(w http.ResponseWriter, r *http.Request, db *gorm.DB, p model.Project, fallbackGuildID string) {
	lang := currentLang(r)
	class, label, hint := iaStatus(db, p, lang)
	sectionLink := func(id, title string) string {
		return `<a class="section-link" href="#` + id + `">` + esc(title) + `</a>`
	}
	statusSection := renderConnectedProductionNotificationSection(db, p, lang, r, class, label, hint, model.ValidateProductionNotificationConfig(db, p.KitsuProjectID, model.ListProductionNotificationRoutes(db, p.KitsuProjectID)))
	var mappings, activity, diagnoses, rawDiagnoses strings.Builder
	for _, m := range model.ListProductionChannelMappings(db, p.KitsuProjectID) {
		mappings.WriteString(`<li><strong>` + esc(m.TaskTypeName) + `</strong><span>→ ` + esc(m.ChannelName) + `</span></li>`)
	}
	if mappings.Len() == 0 {
		mappings.WriteString(`<li class="muted">` + esc(t(lang, "チャンネル設定はありません。", "No channel settings yet.")) + `</li>`)
	}
	for _, log := range model.ListAuditLogs(db, 40) {
		if log.ProjectName == p.Name {
			activity.WriteString(`<li>` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + ` — ` + esc(iaActivityAction(lang, log)) + `</li>`)
		}
	}
	if activity.Len() == 0 {
		activity.WriteString(`<li class="muted">` + esc(t(lang, "履歴はありません。", "No activity yet.")) + `</li>`)
	}
	for _, d := range model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 10) {
		diagnoses.WriteString(`<li><strong>` + esc(t(lang, "現在の問題", "Current problem")) + `:</strong> ` + esc(t(lang, "通知先の確認が必要です。", "The notification destination needs attention.")) + `</li><li><strong>` + esc(t(lang, "原因", "Cause")) + `:</strong> ` + esc(t(lang, "保存された通知先を確認できません。", "The saved notification destination could not be verified.")) + `</li><li><strong>` + esc(t(lang, "次の操作", "Next action")) + `:</strong> ` + esc(t(lang, "通知先設定を確認してください。", "Review the notification destination settings.")) + `</li>`)
		rawDiagnoses.WriteString(`<li>` + esc(d.Detail) + `</li>`)
	}
	if diagnoses.Len() == 0 {
		diagnoses.WriteString(`<li class="muted">` + esc(t(lang, "現在の問題はありません。", "No current problems.")) + `</li>`)
	}
	advanced := `<details class="advanced-details"><summary>` + esc(tr(lang, "ia.advanced")) + `</summary><dl class="detail-list"><dt>Production ID</dt><dd><code>` + esc(p.KitsuProjectID) + `</code></dd><dt>Discord server ID</dt><dd><code>` + esc(p.DiscordGuildID) + `</code></dd><dt>Category ID</dt><dd><code>` + esc(p.DiscordCategoryID) + `</code></dd></dl>`
	if rawDiagnoses.Len() > 0 {
		advanced += `<h3>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</h3><ul class="list-tight">` + rawDiagnoses.String() + `</ul>`
	}
	advanced += `</details>`
	danger := `<details class="advanced-details danger-zone"><summary>` + esc(tr(lang, "ia.danger")) + `</summary><div class="danger-actions"><div><h3>` + esc(tr(lang, "ia.disconnect_production")) + `</h3><p class="hint">` + esc(t(lang, "Productionの連携だけを解除します。Discord側のリソースは残ります。", "Remove only the KitsuSync connection. Discord resources remain.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "連携解除", "disconnect")) + `"><input type="hidden" name="action" value="remove_connection"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-ghost" type="submit">` + esc(tr(lang, "ia.disconnect_production")) + `</button></form></div><div><h3>` + esc(tr(lang, "ia.delete_discord_resources")) + `</h3><p class="hint">` + esc(t(lang, "Discord側のチャンネルとカテゴリを削除します。影響を確認してから実行してください。", "Delete Discord-side channels and category only after reviewing the exact impact.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "Discord側のリソース削除", "delete Discord resources")) + `"><input type="hidden" name="action" value="preview_remove_connection_with_discord"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-danger" type="submit">` + esc(tr(lang, "ia.delete_discord_resources")) + `</button></form></div></div></details>`
	serverName := t(lang, "接続済みDiscordサーバー", "Connected Discord server")
	if strings.TrimSpace(p.DiscordGuildID) == "" {
		serverName = t(lang, "未接続", "Not connected")
	}
	body := `<div class="production-context"><div class="page-heading"><div><div class="eyebrow">` + esc(tr(lang, "ia.productions")) + `</div><h1>` + esc(p.Name) + `</h1><p class="hint">` + esc(t(lang, "選択中のProduction", "Selected Production")) + `</p></div><span class="status-pill ` + class + `">` + esc(label) + `</span></div><nav class="section-nav" aria-label="` + esc(t(lang, "Productionのセクション", "Production sections")) + `">` + sectionLink("overview", tr(lang, "ia.overview")) + sectionLink("notifications", tr(lang, "ia.notifications")) + sectionLink("user-settings", tr(lang, "ia.user_settings")) + sectionLink("storage-settings", tr(lang, "ia.storage_settings")) + sectionLink("activity", tr(lang, "ia.activity")) + sectionLink("troubleshooting", tr(lang, "ia.troubleshooting")) + sectionLink("advanced", tr(lang, "ia.advanced")) + sectionLink("danger-zone", tr(lang, "ia.danger")) + `</nav>` +
		`<section id="overview" class="section-card glass"><h2>` + esc(tr(lang, "ia.overview")) + `</h2><p class="state-explanation" role="status">` + esc(hint) + `</p><p class="field-help">` + esc(t(lang, "Discordサーバー", "Discord server")) + `: ` + esc(serverName) + `</p></section>` +
		`<section id="notifications" class="section-stack">` + statusSection + `<div class="section-card glass"><h2>` + esc(t(lang, "Task TypeとDiscordチャンネル", "Task Type to Discord channel settings")) + `</h2><ul class="mapping-list">` + mappings.String() + `</ul></div></section>` +
		`<section id="user-settings" class="section-card glass"><h2>` + esc(tr(lang, "ia.user_settings")) + `</h2><p class="hint">` + esc(t(lang, "Reviewer / Checkerの割り当てはこのProductionで管理します。", "Reviewer / Checker assignments belong to this Production.")) + `</p><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users?project="+url.QueryEscape(p.KitsuProjectID), r)) + `">` + esc(t(lang, "ユーザー設定を開く", "Open user settings")) + `</a></section>` +
		`<section id="storage-settings" class="section-card glass"><h2>` + esc(tr(lang, "ia.storage_settings")) + `</h2><p class="hint">` + esc(t(lang, "このProductionの保存先とリンクを管理します。", "Manage storage destinations and links for this Production.")) + `</p><form method="POST" action="` + esc(withLang("/bot/admin/drive", r)) + `"><input type="hidden" name="kitsu_project_id" value="` + esc(p.KitsuProjectID) + `"><label for="storage-url">` + esc(t(lang, "保存先リンク", "Storage link")) + `</label><input id="storage-url" type="url" name="storage_url" value="` + esc(p.StorageURL) + `"><button class="btn" type="submit">` + esc(t(lang, "保存", "Save")) + `</button></form></section>` +
		`<section id="activity" class="section-card glass"><h2>` + esc(tr(lang, "ia.activity")) + `</h2><ul class="list-tight" role="log">` + activity.String() + `</ul></section>` +
		`<section id="troubleshooting" class="section-card glass"><h2>` + esc(tr(lang, "ia.troubleshooting")) + `</h2><ul class="list-tight" role="status">` + diagnoses.String() + `</ul></section>` + advanced + `<div id="danger-zone">` + danger + `</div></div>`
	troubleshootingClass := "success"
	troubleshootingValue := t(lang, "問題なし", "No current problems")
	if diagnoses.Len() > 0 && !strings.Contains(diagnoses.String(), "No current problems") && !strings.Contains(diagnoses.String(), "現在の問題はありません") {
		troubleshootingClass = "warning"
		troubleshootingValue = t(lang, "確認が必要", "Needs review")
	}
	body += `<section class="section-card glass" aria-labelledby="production-status-summary"><h2 id="production-status-summary">` + esc(t(lang, "状態の概要", "Status summary")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "Productionの状態", "Production state"), normalizeStatusClass(class), label, hint, "") + statusSummaryRow(t(lang, "Discordサーバー", "Discord server"), map[bool]string{true: "success", false: "blocked"}[strings.TrimSpace(p.DiscordGuildID) != ""], serverName, "", "") + statusSummaryRow(t(lang, "通知状態", "Notification state"), normalizeStatusClass(class), label, hint, "") + statusSummaryRow(t(lang, "トラブルシューティング", "Troubleshooting"), troubleshootingClass, troubleshootingValue, "", "") + `</dl></section>`
	body += `<section class="section-card glass" aria-labelledby="production-status-summary"><h2 id="production-status-summary">` + esc(t(lang, "状態の概要", "Status summary")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "Productionの状態", "Production state"), normalizeStatusClass(class), cleanStatusLabel(lang, class), hint, "") + statusSummaryRow(t(lang, "Discordサーバー", "Discord server"), map[bool]string{true: "success", false: "blocked"}[strings.TrimSpace(p.DiscordGuildID) != ""], serverName, "", "") + statusSummaryRow(t(lang, "通知状態", "Notification state"), normalizeStatusClass(class), cleanStatusLabel(lang, class), hint, "") + `</dl></section>`
	fmt.Fprint(w, adminPage(lang, p.Name, r, body))
}

func renderIABotLegacy(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	class, state, hint := iaReadiness(db, lang)
	body := `<div class="section-stack"><section class="section-card glass"><div class="page-heading"><div><h2>` + esc(tr(lang, "ia.bot_connection")) + `</h2><p class="hint">` + esc(hint) + `</p></div><span class="status-pill ` + class + `">` + esc(state) + `</span></div><dl class="status-list"><dt>` + esc(t(lang, "Bot状態", "Bot state")) + `</dt><dd>` + esc(state) + `</dd></dl><h3>` + esc(t(lang, "必要な権限", "Required permissions")) + `</h3><ul class="list-tight permission-list"><li>` + esc(t(lang, "チャンネルを表示", "View channels")) + `</li><li>` + esc(t(lang, "メッセージを送信", "Send messages")) + `</li><li>` + esc(t(lang, "チャンネルを管理（接続設定時のみ）", "Manage channels (only during connection setup)")) + `</li></ul><div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `">` + esc(t(lang, "Bot接続を設定", "Connect or reconnect")) + `</a></div></section><section class="section-card glass"><h2>` + esc(t(lang, "接続済みDiscordサーバー", "Joined Discord servers")) + `</h2><p class="hint">` + esc(t(lang, "Bot接続後に確認できます。", "Available after the bot is connected.")) + `</p></section></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.bot_connection"), r, body))
}

func renderIAHealthLegacy(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	status := func(ok bool) string {
		if ok {
			return t(lang, "接続済み", "Connected")
		}
		return t(lang, "未接続", "Disconnected")
	}
	_, notificationState, notificationHint := iaReadiness(db, lang)
	body := `<div class="section-stack"><section class="section-card glass"><h2>` + esc(tr(lang, "ia.system_status")) + `</h2><div class="status-list"><dl><dt>Kitsu` + esc(t(lang, "接続", " connection")) + `</dt><dd>` + esc(status(readiness.KitsuConfigured)) + `</dd><dt>Discord` + esc(t(lang, "接続", " connection")) + `</dt><dd>` + esc(status(readiness.DiscordConfigured)) + `</dd><dt>` + esc(t(lang, "Bot状態", "Bot state")) + `</dt><dd>` + esc(status(readiness.DiscordConfigured)) + `</dd><dt>` + esc(t(lang, "通知状態", "Notification state")) + `</dt><dd>` + esc(notificationState) + `</dd></dl></div><p class="state-explanation" role="status">` + esc(notificationHint) + `</p><p class="hint">` + esc(t(lang, "全体の問題", "Overall problem")) + `: ` + esc(notificationHint) + `</p><p class="hint">` + esc(t(lang, "次に必要な操作", "Next required action")) + `: ` + esc(notificationHint) + `</p><div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(tr(lang, "ia.productions")) + `</a></div></section><details class="advanced-details"><summary>` + esc(tr(lang, "ia.advanced")) + `</summary><p class="hint">` + esc(t(lang, "技術的な診断情報はここに限定します。", "Technical diagnostic information is limited to this disclosure.")) + `</p></details></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.system_status"), r, body))
}

func renderIABot(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	class, _, hint := iaReadiness(db, lang)
	permissionAction := `<a class="btn" href="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `">` + esc(t(lang, "Bot接続を設定", "Connect or reconnect")) + `</a>`
	body := `<div class="section-stack"><section class="section-card glass"><div class="page-heading"><div><h2>` + esc(tr(lang, "ia.bot_connection")) + `</h2><p class="hint">` + esc(hint) + `</p></div></div><dl class="status-list">` +
		statusSummaryRow(t(lang, "Bot状態", "Bot state"), normalizeStatusClass(class), cleanStatusLabel(lang, class), hint, permissionAction) +
		statusSummaryRow(t(lang, "必要な権限", "Required permissions"), "neutral", t(lang, "接続設定時に必要", "Required during connection setup"), t(lang, "チャンネル表示、メッセージ送信、接続設定時のチャンネル管理", "View channels, send messages, and manage channels during setup"), "") +
		statusSummaryRow(t(lang, "接続済みサーバー", "Joined servers"), "neutral", t(lang, "Bot接続後に確認できます", "Available after Bot Connection"), "", "") + `</dl></section></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.bot_connection"), r, body))
}

func renderIAHealth(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	_, notificationState, notificationHint := iaReadiness(db, lang)
	actionURL := "/bot/admin/projects"
	actionLabel := tr(lang, "ia.production_list")
	problem := notificationHint
	if !readiness.KitsuConfigured {
		actionURL = "/bot/setup"
		actionLabel = tr(lang, "ia.new_connection")
		problem = t(lang, "Kitsu接続を設定してください。", "Complete Kitsu connection setup.")
	} else if !readiness.DiscordConfigured {
		actionURL = "/bot/admin/bot"
		actionLabel = tr(lang, "ia.bot_connection")
		problem = t(lang, "Bot接続を設定してください。", "Complete Bot Connection setup.")
	}
	action := `<a class="btn" href="` + esc(withLang(actionURL, r)) + `">` + esc(actionLabel) + `</a>`
	body := `<div class="section-stack"><section class="section-card glass"><h2>` + esc(tr(lang, "ia.system_status")) + `</h2><dl class="status-list">` +
		statusSummaryRow("Kitsu"+t(lang, "接続", " connection"), map[bool]string{true: "success", false: "danger"}[readiness.KitsuConfigured], statusText(lang, readiness.KitsuConfigured), "", "") +
		statusSummaryRow("Discord"+t(lang, "接続", " connection"), map[bool]string{true: "success", false: "blocked"}[readiness.DiscordConfigured], statusText(lang, readiness.DiscordConfigured), "", "") +
		statusSummaryRow(t(lang, "Bot状態", "Bot state"), map[bool]string{true: "success", false: "blocked"}[readiness.DiscordConfigured], statusText(lang, readiness.DiscordConfigured), "", "") +
		statusSummaryRow(t(lang, "通知状態", "Notification state"), notificationClass(notificationState, readiness.OverallReady, lang), notificationState, "", "") +
		statusSummaryRow(t(lang, "全体の問題", "Overall problem"), map[bool]string{true: "success", false: "blocked"}[readiness.OverallReady], statusTextOverall(lang, readiness.OverallReady), problem, action) + `</dl><p class="hint" role="status" aria-live="polite">` + esc(t(lang, "次に必要な操作", "Next required action")) + `: ` + esc(problem) + `</p></section></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.system_status"), r, body))
}

func normalizeStatusClass(class string) string {
	switch class {
	case "ok":
		return "success"
	case "warn":
		return "warning"
	case "bad":
		return "blocked"
	case "success", "warning", "danger", "blocked", "neutral":
		return class
	default:
		return "neutral"
	}
}

func cleanStatusLabel(lang, class string) string {
	switch normalizeStatusClass(class) {
	case "success":
		return tr(lang, "wizard.connected")
	case "warning":
		return tr(lang, "status.needs_review")
	case "danger":
		return tr(lang, "status.needs_review")
	default:
		return tr(lang, "status.action_required")
	}
}

func statusText(lang string, ok bool) string {
	if ok {
		return tr(lang, "wizard.connected")
	}
	return tr(lang, "wizard.not_configured")
}
func statusTextOverall(lang string, ok bool) string {
	if ok {
		return tr(lang, "wizard.available")
	}
	return tr(lang, "wizard.unavailable")
}
func notificationClass(state string, ready bool, lang string) string {
	if strings.Contains(strings.ToLower(state), "pause") || state == t(lang, "一時停止中", "Paused") {
		return "warning"
	}
	if ready {
		return "success"
	}
	return "blocked"
}

func renderIAAudit(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	logs := model.ListAuditLogs(db, 200)
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].CreatedAt.After(logs[j].CreatedAt) })
	var rows strings.Builder
	for _, log := range logs {
		result := t(lang, "成功", "Success")
		if !log.Success {
			result = t(lang, "失敗", "Failed")
		}
		rows.WriteString(`<tr><td>` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</td><td>` + esc(log.ProjectName) + `</td><td>` + esc(iaActivityAction(lang, log)) + `</td><td>` + esc(t(lang, "記録なし", "Not recorded")) + `</td><td><span class="status-pill ` + map[bool]string{true: "ok", false: "bad"}[log.Success] + `">` + esc(result) + `</span></td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="5" class="muted">` + esc(t(lang, "監査ログはありません。", "No audit log entries.")) + `</td></tr>`)
	}
	body := `<section class="section-card glass"><p class="hint">` + esc(t(lang, "設定変更、通知、失敗、復旧の履歴を確認できます。技術的な識別子は詳細表示に限定します。", "Review configuration, notification, failure, and recovery history. Technical identifiers remain in details.")) + `</p><div class="table-wrap"><table><thead><tr><th>` + esc(t(lang, "日時", "Date and time")) + `</th><th>Production</th><th>` + esc(t(lang, "操作内容", "Action")) + `</th><th>` + esc(t(lang, "操作ユーザー", "Acting user")) + `</th><th>` + esc(t(lang, "結果", "Result")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></section>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.audit_log"), r, body))
}

func renderIAUsers(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	var rows strings.Builder
	for _, u := range model.ListUserMap(db) {
		identity := t(lang, "Discordユーザー（対応付け済み）", "Discord user linked")
		if strings.TrimSpace(u.DiscordID) == "" {
			identity = t(lang, "未設定", "Not set")
		}
		edit := withLang("/bot/admin/users?legacy=1&edit="+fmt.Sprint(u.ID), r)
		rows.WriteString(`<tr><td>` + esc(u.KitsuName) + `</td><td>` + esc(identity) + `</td><td><a class="btn-ghost" href="` + esc(edit) + `">` + esc(t(lang, "変更", "Change")) + `</a></td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="3" class="muted">` + esc(t(lang, "ユーザー対応付けはありません。", "No user mappings yet.")) + `</td></tr>`)
	}
	body := `<section class="section-card glass"><p class="hint">` + esc(t(lang, "KitsuユーザーとDiscordユーザーの対応付けだけを管理します。Reviewer / CheckerはProductionのユーザー設定で管理します。", "Manage only Kitsu user to Discord user correspondence. Reviewer / Checker belongs in the selected Production's user settings.")) + `</p><table><thead><tr><th>Kitsu` + esc(t(lang, "ユーザー", " user")) + `</th><th>Discord` + esc(t(lang, "ユーザー", " user")) + `</th><th>` + esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></section>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.user_mapping"), r, body))
}

func renderGlobalUserMapping(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	var rows strings.Builder
	for _, u := range model.ListUserMap(db) {
		state := tr(lang, "status.incomplete")
		class := "blocked"
		identity := t(lang, "未対応", "Not mapped")
		if strings.TrimSpace(u.DiscordID) != "" {
			state = tr(lang, "wizard.connected")
			class = "success"
			identity = strings.TrimSpace(u.DiscordDisplayName)
			if identity == "" {
				identity = t(lang, "Discordユーザー対応付け済み", "Discord user mapped")
			}
		}
		change := withLang("/bot/admin/users?legacy=1&edit="+fmt.Sprint(u.ID), r)
		rows.WriteString(`<tr><td>` + esc(u.KitsuName) + `</td><td>` + esc(identity) + `</td><td><span class="status-badge status-badge-` + class + `"><span aria-hidden="true">•</span> ` + esc(state) + `</span></td><td><a class="btn-ghost" href="` + esc(change) + `">` + esc(t(lang, "変更", "Change")) + `</a></td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="4" class="muted">` + esc(t(lang, "ユーザー対応付けはありません。", "No user mappings yet.")) + `</td></tr>`)
	}
	body := `<section class="section-card glass"><h1>` + esc(tr(lang, "ia.user_mapping")) + `</h1><p class="hint">` + esc(t(lang, "KitsuユーザーとDiscordユーザーの対応付けだけを管理します。Reviewer / Checkerは選択中のProductionで管理します。", "Manage only Kitsu user to Discord user correspondence. Reviewer / Checker is managed inside the selected Production.")) + `</p><div class="table-wrap"><table><thead><tr><th>` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `</th><th>` + esc(t(lang, "Discordユーザー", "Discord user")) + `</th><th>` + esc(t(lang, "状態", "Status")) + `</th><th>` + esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></section>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.user_mapping"), r, body))
}

func renderIANewConnection(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r != nil {
		lang := currentLang(r)
		kitsuHost := model.GetSetting(db, "kitsu.hostname")
		botToken := storedRuntimeDiscordBotToken(db)
		projects := ListKitsuProjects(kitsuHost)
		step := wizardStep(r, botToken, db, projectIDFromRequest(r), strings.TrimSpace(r.URL.Query().Get("plan_guild")))
		if r.URL.Query().Get("wizard") == "complete" && sharedBotRuntimeReadiness(db, kitsuHost, botToken).OverallReady {
			step = 7
		}
		fmt.Fprint(w, adminPage(lang, tr(lang, "ia.new_connection"), r, renderSetupWizard(lang, r, db, projects, botToken, step)))
	} else {
		lang := currentLang(r)
		kitsuHost := model.GetSetting(db, "kitsu.hostname")
		botToken := storedRuntimeDiscordBotToken(db)
		projects := ListKitsuProjects(kitsuHost)
		projectID := strings.TrimSpace(r.URL.Query().Get("project"))
		var projectOptions strings.Builder
		projectOptions.WriteString(`<option value="">` + esc(t(lang, "Productionを選択", "Select a Kitsu Production")) + `</option>`)
		for _, p := range projects {
			selected := ""
			if p.ID == projectID {
				selected = " selected"
			}
			projectOptions.WriteString(`<option value="` + esc(p.ID) + `"` + selected + `>` + esc(p.Name) + `</option>`)
		}
		body := `<div class="section-stack"><section class="section-card glass"><h1>` + esc(tr(lang, "ia.new_connection")) + `</h1><p class="hint">` + esc(t(lang, "ProductionとDiscordサーバーを選び、作成または再利用するチャンネルを確認してから接続します。", "Select a Production and Discord server, review the create/reuse plan, then connect.")) + `</p><ol class="step-list"><li>` + esc(t(lang, "Kitsu Productionを選択", "Select a Kitsu Production")) + `</li><li>` + esc(t(lang, "Discordサーバーを選択", "Select a Discord server")) + `</li><li>` + esc(t(lang, "作成または再利用するチャンネルを確認", "Review channels to create or reuse")) + `</li><li>` + esc(t(lang, "内容を確認してから実行", "Confirm the exact plan before execution")) + `</li></ol><form method="GET" class="section-stack"><label for="new-connection-production">` + esc(t(lang, "Kitsu Production", "Kitsu Production")) + `</label><select id="new-connection-production" name="project">` + projectOptions.String() + `</select><button class="btn" type="submit">` + esc(t(lang, "次へ", "Continue")) + `</button></form></section>`
		if projectID != "" {
			selected := KitsuProject{}
			for _, p := range projects {
				if p.ID == projectID {
					selected = p
					break
				}
			}
			if selected.ID != "" {
				body += renderExplicitTaskTypeChannelPlan(model.Project{KitsuProjectID: selected.ID, Name: selected.Name}, routingTaskTypes(), botToken, r, lang, db)
			}
		}
		if strings.TrimSpace(botToken) == "" {
			body += `<section class="section-card glass" role="status"><h2>` + esc(t(lang, "対応が必要", "Action required")) + `</h2><p class="hint">` + esc(t(lang, "Discordサーバーを読み込むにはBot接続が必要です。先にBot接続を設定してください。", "Bot Connection is required to read Discord servers. Complete Bot Connection first.")) + `</p><a class="btn" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a></section>`
		}
		body += `</div>`
		fmt.Fprint(w, adminPage(lang, tr(lang, "ia.new_connection"), r, body))
	}
}

func projectIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("project"))
}

func wizardStep(r *http.Request, botToken string, db *gorm.DB, projectID, guildID string) int {
	ready := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), botToken).OverallReady
	if !ready {
		return 1
	}
	maxAllowed := 2
	if projectID != "" {
		maxAllowed = 3
		if guildID != "" {
			maxAllowed = 7
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("wizard_step")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= maxAllowed {
			return n
		}
	}
	if projectID == "" {
		return 2
	}
	if guildID == "" {
		return 3
	}
	if r.URL.Query().Get("review") == "1" {
		return 5
	}
	return 4
}

func setupWizardURL(r *http.Request, step int, projectID, guildID string, review bool) string {
	values := url.Values{}
	values.Set("wizard_step", strconv.Itoa(step))
	if projectID != "" {
		values.Set("project", projectID)
	}
	if guildID != "" {
		values.Set("plan_guild", guildID)
	}
	if review {
		values.Set("review", "1")
	}
	return withLang("/bot/setup?"+values.Encode(), r)
}

func renderSetupWizard(lang string, r *http.Request, db *gorm.DB, projects []KitsuProject, botToken string, step int) string {
	projectID := projectIDFromRequest(r)
	guildID := strings.TrimSpace(r.URL.Query().Get("plan_guild"))
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), botToken)
	labels := []string{tr(lang, "wizard.step_prerequisites"), tr(lang, "wizard.step_production"), tr(lang, "wizard.step_server"), tr(lang, "wizard.step_plan"), tr(lang, "wizard.step_review"), tr(lang, "wizard.step_execute"), tr(lang, "wizard.step_complete")}
	var steps strings.Builder
	for i, label := range labels {
		n := i + 1
		state := "pending"
		if n < step {
			state = "done"
		}
		if n == step {
			state = "active"
		}
		if n > step && step == 1 {
			state = "blocked"
		}
		aria := ""
		if n == step {
			aria = ` aria-current="step"`
		}
		steps.WriteString(`<span class="setup-step ` + state + `"` + aria + `><span class="step-num">` + strconv.Itoa(n) + `</span><span class="step-label">` + esc(label) + `</span></span>`)
		if n < len(labels) {
			steps.WriteString(`<span class="step-connector" aria-hidden="true"></span>`)
		}
	}
	body := `<div class="section-stack"><section class="section-card glass"><h1>` + esc(tr(lang, "ia.new_connection")) + `</h1><p class="hint">` + esc(tr(lang, "wizard.description")) + `</p><div class="setup-steps" aria-label="` + esc(tr(lang, "wizard.progress")) + `">` + steps.String() + `</div></section>`
	switch step {
	case 1:
		body += renderWizardPrerequisites(lang, r, readiness)
	case 2:
		body += renderWizardProduction(lang, r, db, projects)
	case 3:
		body += renderWizardServer(lang, r, botToken, projectID)
	case 4, 5:
		body += renderWizardPlan(lang, r, db, botToken, projects, projectID, guildID, step == 5)
	case 6:
		body += `<section class="section-card glass" role="status" aria-live="polite"><h2>` + esc(tr(lang, "wizard.execute_title")) + `</h2><p class="hint">` + esc(tr(lang, "wizard.execute_hint")) + `</p><a class="btn" href="` + esc(setupWizardURL(r, 5, projectID, guildID, true)) + `">` + esc(tr(lang, "wizard.back_to_review")) + `</a></section>`
	case 7:
		body += renderWizardComplete(lang, r, db, projectID)
	}
	return body + `</div>`
}

func renderWizardPrerequisites(lang string, r *http.Request, readiness SharedBotRuntimeReadiness) string {
	status := func(ok bool) string {
		if ok {
			return tr(lang, "wizard.connected")
		}
		return tr(lang, "wizard.not_configured")
	}
	botExplanation := ""
	if !readiness.DiscordConfigured {
		botExplanation = tr(lang, "wizard.bot_required_explanation")
	}
	notificationValue := tr(lang, "wizard.unavailable")
	notificationExplanation := tr(lang, "wizard.unavailable_explanation")
	if readiness.OverallReady {
		notificationValue = tr(lang, "wizard.available")
		notificationExplanation = ""
	}
	body := `<section class="section-card glass" aria-labelledby="wizard-prerequisites-title"><h2 id="wizard-prerequisites-title">` + esc(tr(lang, "wizard.prerequisites_title")) + `</h2><dl class="status-list">` +
		statusSummaryRow("Kitsu"+t(lang, "接続", " connection"), map[bool]string{true: "success", false: "blocked"}[readiness.KitsuConfigured], status(readiness.KitsuConfigured), "", "") +
		statusSummaryRow("Discord Bot", map[bool]string{true: "success", false: "blocked"}[readiness.DiscordConfigured], status(readiness.DiscordConfigured), botExplanation, "") +
		statusSummaryRow(tr(lang, "wizard.notification_state"), map[bool]string{true: "success", false: "blocked"}[readiness.OverallReady], notificationValue, notificationExplanation, "") + `</dl>`
	if !readiness.OverallReady {
		body += `<p class="state-explanation" role="status" aria-live="polite">` + esc(tr(lang, "wizard.blocked_bot")) + `</p><a class="btn" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "wizard.open_bot")) + `</a>`
	} else {
		body += `<p class="state-explanation" role="status" aria-live="polite">` + esc(tr(lang, "wizard.prerequisites_ready")) + `</p><a class="btn" href="` + esc(setupWizardURL(r, 2, "", "", false)) + `">` + esc(tr(lang, "wizard.next")) + `</a>`
	}
	return body + `</section>`
}

func renderWizardProduction(lang string, r *http.Request, db *gorm.DB, projects []KitsuProject) string {
	var options strings.Builder
	options.WriteString(`<option value="">` + esc(tr(lang, "wizard.select_production")) + `</option>`)
	for _, p := range projects {
		connected := model.FindProjectByKitsuID(db, p.ID) != nil
		disabled := ""
		label := p.Name
		if connected {
			disabled = " disabled"
			label += " (" + tr(lang, "wizard.already_connected") + ")"
		}
		options.WriteString(`<option value="` + esc(p.ID) + `"` + disabled + `>` + esc(label) + `</option>`)
	}
	return `<section class="section-card glass" aria-labelledby="wizard-production-title"><h2 id="wizard-production-title">` + esc(tr(lang, "wizard.production_title")) + `</h2><form method="GET" class="section-stack"><input type="hidden" name="wizard_step" value="2"><label for="wizard-production">` + esc(tr(lang, "wizard.production_label")) + `</label><select id="wizard-production" name="project" required aria-describedby="wizard-production-help">` + options.String() + `</select><p id="wizard-production-help" class="field-help">` + esc(tr(lang, "wizard.production_help")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 1, "", "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.next")) + `</button></div></form></section>`
}

func renderWizardServer(lang string, r *http.Request, botToken, projectID string) string {
	var options strings.Builder
	options.WriteString(`<option value="">` + esc(tr(lang, "wizard.select_server")) + `</option>`)
	if strings.TrimSpace(botToken) != "" {
		if guilds, err := ListBotGuilds(botToken); err == nil {
			for _, guild := range guilds {
				options.WriteString(`<option value="` + esc(guild.ID) + `">` + esc(guild.Name) + `</option>`)
			}
		}
	}
	return `<section class="section-card glass" aria-labelledby="wizard-server-title"><h2 id="wizard-server-title">` + esc(tr(lang, "wizard.server_title")) + `</h2><form method="GET" class="section-stack"><input type="hidden" name="wizard_step" value="3"><input type="hidden" name="project" value="` + esc(projectID) + `"><label for="wizard-server">` + esc(tr(lang, "wizard.server_label")) + `</label><select id="wizard-server" name="plan_guild" required>` + options.String() + `</select><p class="field-help">` + esc(tr(lang, "wizard.server_help")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 2, projectID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.next")) + `</button></div></form></section>`
}

func wizardProject(projects []KitsuProject, id string) KitsuProject {
	for _, p := range projects {
		if p.ID == id {
			return p
		}
	}
	return KitsuProject{}
}
func wizardTaskTypes() []kitsu.TaskType {
	if types := routingTaskTypes(); len(types) > 0 {
		return types
	}
	return kitsu.GetTaskTypes().Each
}

func renderWizardPlan(lang string, r *http.Request, db *gorm.DB, botToken string, projects []KitsuProject, projectID, guildID string, review bool) string {
	project := wizardProject(projects, projectID)
	if project.ID == "" || guildID == "" {
		return `<section class="section-card glass" role="alert">` + esc(tr(lang, "wizard.plan_blocked")) + `</section>`
	}
	channels, err := ListGuildChannels(guildID, botToken)
	if err != nil {
		return `<section class="section-card glass" role="alert"><h2>` + esc(tr(lang, "wizard.plan_title")) + `</h2><p>` + esc(tr(lang, "wizard.plan_unavailable")) + `</p></section>`
	}
	plan := BuildTaskTypeChannelPlan(project.ID, guildID, wizardTaskTypes(), existingChannelsForPlanWithLegacy(channels, model.ListProductionChannelMappings(db, project.ID), model.ListProjectWebhooks(db, project.ID)))
	var rows strings.Builder
	for _, entry := range plan.Entries {
		action := map[string]string{"create": tr(lang, "wizard.create"), "reuse": tr(lang, "wizard.reuse"), "conflict": tr(lang, "wizard.conflict"), "blocked": tr(lang, "wizard.review_required")}[entry.Action]
		rows.WriteString(`<tr><td>` + esc(entry.TaskTypeName) + `</td><td><code>` + esc(entry.ChannelName) + `</code></td><td>` + esc(action) + `</td><td>` + esc(wizardPlanDetails(lang, entry.Action)) + `</td></tr>`)
	}
	body := `<section class="section-card glass" aria-labelledby="wizard-plan-title"><h2 id="wizard-plan-title">` + esc(tr(lang, "wizard.plan_title")) + `</h2><p class="hint">` + esc(tr(lang, "wizard.plan_hint")) + `</p><div class="table-wrap"><table><caption class="sr-only">` + esc(tr(lang, "wizard.plan_caption")) + `</caption><thead><tr><th>` + esc(tr(lang, "wizard.task_type")) + `</th><th>` + esc(tr(lang, "wizard.channel")) + `</th><th>` + esc(tr(lang, "wizard.result")) + `</th><th>` + esc(tr(lang, "wizard.details")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div>`
	if !plan.Valid() {
		return body + `<p class="state-explanation" role="alert">` + esc(tr(lang, "wizard.plan_blocked")) + `</p></section>`
	}
	if !review {
		return body + `<p class="field-help" role="status">` + esc(tr(lang, "wizard.no_write")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 3, projectID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><a class="btn" href="` + esc(setupWizardURL(r, 5, projectID, guildID, true)) + `">` + esc(tr(lang, "wizard.review")) + `</a></div></section>`
	}
	return body + `<p class="field-help">` + esc(tr(lang, "wizard.no_write")) + `</p><form method="POST" action="` + esc(withLang("/bot/setup", r)) + `" class="section-stack"><input type="hidden" name="action" value="confirm_task_type_channels"><input type="hidden" name="project_id" value="` + esc(projectID) + `"><input type="hidden" name="guild_id" value="` + esc(guildID) + `"><input type="hidden" name="plan_fingerprint" value="` + esc(plan.Fingerprint()) + `"><label for="wizard-confirm"><input id="wizard-confirm" type="checkbox" name="confirm_plan" value="yes" required> ` + esc(tr(lang, "wizard.confirm")) + `</label><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 4, projectID, guildID, false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.execute")) + `</button></div></form></section>`
}

func wizardPlanDetails(lang, action string) string {
	switch action {
	case "create":
		return tr(lang, "wizard.detail_create")
	case "reuse":
		return tr(lang, "wizard.detail_reuse")
	case "conflict":
		return tr(lang, "wizard.detail_conflict")
	default:
		return tr(lang, "wizard.detail_review")
	}
}
func renderWizardComplete(lang string, r *http.Request, db *gorm.DB, projectID string) string {
	project := model.FindProjectByKitsuID(db, projectID)
	name := ""
	if project != nil {
		name = project.Name
	}
	return `<section class="section-card glass" role="status" aria-live="polite"><h2>` + esc(tr(lang, "wizard.complete_title")) + `</h2><p>` + esc(trf(lang, "wizard.complete_message", name)) + `</p><a class="btn" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(projectID), r)) + `">` + esc(tr(lang, "wizard.open_production")) + `</a></section>`
}
