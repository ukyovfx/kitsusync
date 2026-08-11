package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// These views are presentation-only. Existing POST handlers remain the only
// mutation path for setup, routing, confirmation and destructive operations.
func iaNav(lang string, r *http.Request) string {
	items := []struct{ key, path string }{
		{"ia.productions", "/bot/admin/projects"},
		{"ia.user_mapping", "/bot/admin/users"},
		{"connections.title", "/bot/admin/bot"}, {"ia.system_status", "/bot/admin/health"},
		{"ia.audit_log", "/bot/admin/audit"},
	}
	var b strings.Builder
	for _, item := range items {
		b.WriteString(`<a class="nav-chip" href="` + esc(withLang(item.path, r)) + `">` + esc(tr(lang, item.key)) + `</a>`)
	}
	return b.String()
}

func hasValidationOnlyProject(db *gorm.DB) bool {
	for _, project := range model.ListProjects(db) {
		if project.ValidationOnly {
			return true
		}
	}
	return false
}

// availableProjects merges live Kitsu read data with local connection state.
// It never creates or updates database rows. Live-only records are marked as
// in-memory previews so normal pages can explain that they are not connected.
func availableProjects(db *gorm.DB) []model.Project {
	local := model.ListProjects(db)
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) == "" {
		return local
	}
	live := ListKitsuProjects("")
	if len(live) == 0 {
		return local
	}
	localByID := make(map[string]model.Project, len(local))
	for _, project := range local {
		localByID[strings.TrimSpace(project.KitsuProjectID)] = project
	}
	merged := make([]model.Project, 0, len(live)+len(local))
	for _, liveProject := range live {
		id := strings.TrimSpace(liveProject.ID)
		if project, ok := localByID[id]; ok {
			merged = append(merged, project)
			delete(localByID, id)
			continue
		}
		preview := model.Project{KitsuProjectID: id, Name: strings.TrimSpace(liveProject.Name), ProjectType: "live", ReadOnlyPreview: true}
		data := model.ValidationKitsuData{}
		for _, taskType := range kitsu.GetProjectTaskTypes(id).Each {
			if taskType.Archived || taskType.IsArchived {
				continue
			}
			if strings.TrimSpace(taskType.ID) != "" && strings.TrimSpace(taskType.Name) != "" {
				data.TaskTypes = append(data.TaskTypes, model.ValidationTaskType{ID: strings.TrimSpace(taskType.ID), Name: strings.TrimSpace(taskType.Name)})
			}
		}
		if encoded, err := json.Marshal(data); err == nil {
			preview.ValidationDataJSON = string(encoded)
		}
		merged = append(merged, preview)
	}
	for _, project := range localByID {
		merged = append(merged, project)
	}
	sort.Slice(merged, func(i, j int) bool { return strings.ToLower(merged[i].Name) < strings.ToLower(merged[j].Name) })
	return merged
}

func liveProjectPreview(db *gorm.DB, projectID string) *model.Project {
	for _, project := range availableProjects(db) {
		if project.ReadOnlyPreview && strings.TrimSpace(project.KitsuProjectID) == strings.TrimSpace(projectID) {
			return &project
		}
	}
	return nil
}

// statusSummaryRow is the shared normal-user status presentation. The action
// is supplied as already-rendered HTML so links remain escaped at the call
// site and never become part of the badge itself.
func ifNonEmpty(value, wrapped string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return wrapped
}

func iaStatus(db *gorm.DB, project model.Project, lang string) (string, string, string) {
	if project.ReadOnlyPreview && lang == "en" {
		return "warning", "Disconnected", "This Production is not connected to KitsuSync yet. Notifications are unavailable."
	}
	if project.ReadOnlyPreview {
		return "warning", t(lang, "未接続", "Not connected"), t(lang, "このProductionはまだKitsuSyncに接続されていません。通知は利用できません。", "This Production is not connected to KitsuSync yet. Notifications are unavailable.")
	}
	if project.ValidationOnly {
		return "warning", t(lang, "検証専用", "Validation only"), t(lang, "実データの表示確認用です。Discordサーバーは未接続で、通知は利用できません。", "Read-only Kitsu data for validation. No Discord server is connected and notifications are unavailable.")
	}
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
		return "ok", t(lang, "接続済", "Connected"), t(lang, "KitsuとDiscordの接続を利用できます。", "Kitsu and Discord are ready to use.")
	}
	if !r.KitsuConfigured {
		return "bad", t(lang, "対応が必要", "Action required"), t(lang, "Kitsu接続を設定してください。", "Complete Kitsu connection setup.")
	}
	if !r.DiscordConfigured {
		return "bad", t(lang, "対応が必要", "Action required"), t(lang, "Bot接続を設定してください。", "Complete Bot Connection setup.")
	}
	return "warn", tr(lang, "status.action_required"), t(lang, "Productionの接続を設定すると通知を利用できます。", "Connect at least one Production before notifications can be used.")
}

type readinessView struct {
	Class             string
	Label             string
	Hint              string
	ActionURL         string
	ActionLabel       string
	Notification      string
	NotificationClass string
}

func readinessViewFor(lang string, r *http.Request, readiness SharedBotRuntimeReadiness) readinessView {
	view := readinessView{Class: "blocked", NotificationClass: "blocked"}
	switch readiness.State {
	case ReadinessSetupRequired:
		view.Class = "danger"
		view.Label = t(lang, "未設定", "Not set")
		view.Hint = t(lang, "Kitsu接続を設定してください。", "Configure the Kitsu connection before continuing.")
		view.ActionURL = withLang("/bot/admin/bot", r)
		view.ActionLabel = tr(lang, "wizard.open_kitsu")
	case ReadinessBotSetupRequired:
		view.Class = "danger"
		view.Label = t(lang, "未設定", "Not set")
		view.Hint = t(lang, "Discord Botを設定してください。", "Configure the Discord Bot before continuing.")
		view.ActionURL = withLang("/bot/admin/bot", r)
		view.ActionLabel = t(lang, "接続設定", "Connection settings")
	case ReadinessProductionRequired:
		view.Label = t(lang, "接続待ち", "Waiting for connection")
		view.Hint = t(lang, "接続済みプロダクションがありません。", "No connected Productions yet.")
		view.ActionURL = withLang("/bot/setup", r)
		view.ActionLabel = tr(lang, "ia.new_connection")
	case ReadinessRoutingRequired:
		view.Label = t(lang, "対応が必要です", "Action required")
		view.Hint = t(lang, "通知先設定を確認してください。", "Review the notification destination settings.")
		view.ActionURL = withLang("/bot/admin/projects", r)
		view.ActionLabel = tr(lang, "ia.production_list")
	case ReadinessReady:
		view.Class = "success"
		view.Label = t(lang, "利用可能", "Available")
		view.Hint = t(lang, "通知を利用できます。", "Notifications are available.")
	}
	if readiness.OverallReady {
		view.Notification = t(lang, "利用可能", "Available")
		view.NotificationClass = "success"
	} else if readiness.State == ReadinessRoutingRequired {
		view.Notification = t(lang, "対応が必要です", "Action required")
	} else {
		view.Notification = t(lang, "利用不可", "Unavailable")
	}
	return view
}

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
	action := ""
	if strings.TrimSpace(actionHTML) != "" {
		action = `<div class="status-row-action">` + actionHTML + `</div>`
	} else {
		action = `<div class="status-row-action" aria-hidden="true"></div>`
	}
	explanationCell := `<dd class="status-row-explanation">` + esc(explanation) + `</dd>`
	if strings.TrimSpace(explanation) == "" {
		explanationCell = `<dd class="status-row-explanation" aria-hidden="true"></dd>`
	}
	return `<div class="status-row"><dt class="status-row-label">` + esc(label) + `</dt><dd class="status-row-value"><span class="status-badge status-badge-` + esc(class) + `" role="status">` + esc(value) + `</span></dd>` + explanationCell + action + `</div>`
}

func productionSummaryCard(label, value, class string) string {
	return `<div class="production-summary-card"><span class="production-summary-label">` + esc(label) + `</span><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(value) + `</span></div>`
}

func dashboardStatusRow(label, class, value string) string {
	return `<div class="dashboard-status-row"><span class="dashboard-status-label">` + esc(label) + `</span><span class="status-badge status-badge-` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(value) + `</span></div>`
}

func dashboardProblemAction(r *http.Request, p model.Project, lang, hint string) (string, string) {
	path := "/bot/admin/projects?project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications"
	label := t(lang, "通知設定を確認", "Review notification settings")
	lower := strings.ToLower(hint)
	if strings.Contains(lower, "participant") || strings.Contains(lower, "reviewer") || strings.Contains(lower, "checker") {
		path = "/bot/admin/projects?project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=users"
		label = t(lang, "ユーザー設定を確認", "Review user settings")
	}
	return withLang(path, r), label
}

func renderIADashboard(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	projects := availableProjects(db)
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
		result := t(lang, "成功", "Success")
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
		activityRows.WriteString(`<li class="muted">` + esc(tr(lang, "dashboard.no_recent_activity")) + `</li>`)
	}
	readinessClass, readinessLabel, readinessHint := iaReadiness(db, lang)
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	readinessView := readinessViewFor(lang, r, readiness)
	readinessClass = readinessView.Class
	readinessLabel = readinessView.Label
	readinessHint = readinessView.Hint
	nextActionURL := withLang("/bot/setup", r)
	nextActionLabel := tr(lang, "ia.new_connection")
	if readiness.KitsuConfigured && !readiness.DiscordConfigured {
		nextActionURL = withLang("/bot/admin/bot", r)
		nextActionLabel = t(lang, "Bot接続を設定", "Set up Bot Connection")
		nextActionLabel = tr(lang, "connections.title")
	} else if readiness.KitsuConfigured && readiness.DiscordConfigured && !readiness.OverallReady {
		nextActionURL = withLang("/bot/admin/projects", r)
		nextActionLabel = tr(lang, "ia.production_list")
	}
	nextActionURL = readinessView.ActionURL
	nextActionLabel = readinessView.ActionLabel
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
	dashboardMenu := renderDashboardMenuRefined(lang, r, db, projects, attentionCount, readiness)
	body := `<div class="section-stack">` +
		`<section class="dashboard-intro"><div><h1>` + esc(tr(lang, "ia.dashboard")) + `</h1><p class="hint">` + esc(t(lang, "KitsuSyncの接続状態と、対応が必要な項目を確認できます。", "Review KitsuSync connection state and items that need attention.")) + `</p></div><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin", r)) + `">` + esc(t(lang, "状態を更新", "Refresh status")) + `</a></div></section>` +
		`<section class="dashboard-summary-grid" aria-label="` + esc(t(lang, "概要", "Summary")) + `"><div class="metric-card"><div class="metric-label">` + esc(t(lang, "接続済みProduction", "Connected Productions")) + `</div><div class="metric-value">` + fmt.Sprint(len(projects)) + `</div><p class="field-help">` + esc(t(lang, "現在確認できるProduction", "Productions currently visible")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "対応が必要", "Needs attention")) + `</div><div class="metric-value">` + fmt.Sprint(attentionCount) + `</div><p class="field-help">` + esc(t(lang, "安全に通知できる状態か確認が必要です。", "Review before notifications can be safely delivered.")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "直近24時間の通知失敗", "Notification failures, last 24 hours")) + `</div><div class="metric-value">` + fmt.Sprint(failureCount) + `</div><p class="field-help">` + esc(t(lang, "記録された失敗イベント", "Recorded failure events")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "システム状態", "System status")) + `</div><div class="metric-value"><span class="status-pill ` + readinessClass + `">` + esc(readinessLabel) + `</span></div><p class="field-help" role="status">` + esc(readinessHint) + `</p></div></section>` +
		`<section class="section-card glass dashboard-queue" aria-labelledby="dashboard-attention"><div class="page-heading"><div><h2 id="dashboard-attention">` + esc(t(lang, "対応が必要なProduction", "Productions needing attention")) + `</h2><p class="hint">` + esc(t(lang, "通知が安全に利用できない理由と、次の操作を示します。", "Each row explains why notifications are unavailable and what to do next.")) + `</p></div><span class="status-pill ` + map[bool]string{true: "bad", false: "ok"}[attentionCount > 0] + `">` + fmt.Sprint(attentionCount) + `</span></div><ul class="list-tight">` + attentionRows.String() + `</ul></section>` +
		dashboardMenu +
		`<div class="dashboard-lower-grid"><section class="section-card glass" aria-labelledby="dashboard-activity"><h2 id="dashboard-activity">` + esc(tr(lang, "ia.activity")) + `</h2><div class="activity-columns" aria-hidden="true"><span>` + esc(t(lang, "日時", "Date and time")) + `</span><span>` + esc(t(lang, "操作", "Action")) + `</span><span>` + esc(t(lang, "Production", "Production")) + `</span><span>` + esc(t(lang, "結果", "Result")) + `</span></div><ul class="activity-list" role="log">` + activityRows.String() + `</ul></section><div class="dashboard-side-stack"><section class="section-card glass" aria-labelledby="dashboard-system"><h2 id="dashboard-system">` + esc(t(lang, "通知システム", "Notification system")) + `</h2><div class="dashboard-status-list">` + dashboardStatusRow(t(lang, "Kitsu接続", "Kitsu connection"), map[bool]string{true: "success", false: "danger"}[readiness.KitsuConfigured], statusText(lang, readiness.KitsuConfigured)) + dashboardStatusRow(t(lang, "Discord Bot", "Discord Bot"), map[bool]string{true: "success", false: "blocked"}[readiness.DiscordConfigured], botState) + dashboardStatusRow(t(lang, "通知状態", "Notification state"), map[bool]string{true: "success", false: "blocked"}[readiness.OverallReady], statusTextOverall(lang, readiness.OverallReady)) + `</div><p class="field-help" role="status">` + esc(statusExplanation) + `</p>` + statusAction + `</section><section class="section-card glass dashboard-quick" aria-labelledby="dashboard-quick"><h2 id="dashboard-quick">` + esc(t(lang, "クイック操作", "Quick actions")) + `</h2><div class="button-row">` + ifNonEmpty(quickActions, quickActions) + `</div></section></div></div>`
	body = replaceDashboardConnectedCount(body, len(projects), connectedProductionCount(projects))
	body = removeDashboardSubtitle(body)
	body = applyDashboardMetricSemantics(body, attentionCount, failureCount, readinessClass)
	body = strings.ReplaceAll(body, `class="section-card glass dashboard-quick"`, `class="section-card glass dashboard-quick hidden"`)
	if start := strings.Index(body, `<div class="dashboard-lower-grid">`); start >= 0 {
		body = body[:start] + `</div>`
	}
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func replaceSystemStatusRefreshScript(body string) string {
	start := strings.Index(body, `<script data-system-status-refresh>`)
	if start < 0 {
		return body
	}
	relEnd := strings.Index(body[start:], `</script>`)
	if relEnd < 0 {
		return body
	}
	end := start + relEnd + len(`</script>`)
	return body[:start] + systemStatusRefreshScriptReadable() + body[end:]
}

func systemStatusRefreshScript() string {
	return `<script data-system-status-refresh>(function(){var interval=5000,busy=false,timer,select=document.querySelector("[data-system-status-window]"),root=document.querySelector(".system-status-sections"),live=document.querySelector("[data-system-live-label]");if(!select||!root){return}function text(ja,en){return document.documentElement.lang==="ja"?ja:en}function graph(items){if(!items.length){return ""}var width=320,height=104,left=42,right=8,top=8,bottom=82,windowMs=select.value==="5m"?300000:60000,now=Date.now(),max=1,positions=[];items.forEach(function(item){max=Math.max(max,Number(item.duration_ms)||0);var ratio=1-(now-new Date(item.at).getTime())/windowMs;ratio=Math.max(0,Math.min(1,ratio));positions.push(left+ratio*(width-left-right))});var bars="";items.forEach(function(item,i){var bar=8;if(i>0)bar=Math.min(bar,positions[i]-positions[i-1]-2);if(i+1<items.length)bar=Math.min(bar,positions[i+1]-positions[i]-2);bar=Math.max(2,bar);var value=Number(item.duration_ms)||0,h=Math.max(2,(bottom-top)*value/max),x=Math.max(left,Math.min(width-right-bar,positions[i]-bar/2)),cls=item.success?"bar-success":"bar-failure";bars+="<rect class=\""+cls+"\" x=\""+x.toFixed(1)+"\" y=\""+(bottom-h).toFixed(1)+"\" width=\""+bar.toFixed(1)+"\" height=\""+h.toFixed(1)+"\" rx=\"2\"><title>"+value+" ms</title></rect>"});var label=select.value==="5m"?text("直近5分","Last 5 minutes"):text("直近60秒","Last 60 seconds");return "<svg class=\"api-sparkline api-bar-chart\" viewBox=\"0 0 320 104\" role=\"img\" aria-label=\""+items.length+" observations, "+label+"\"><line class=\"chart-axis\" x1=\"42\" y1=\"82\" x2=\"312\" y2=\"82\"></line><line class=\"chart-axis\" x1=\"42\" y1=\"8\" x2=\"42\" y2=\"82\"></line><text class=\"chart-axis-label\" x=\"2\" y=\"12\">"+text("応答時間 (ms)","Response time (ms)")+"</text><text class=\"chart-tick\" x=\"3\" y=\"84\">0</text><text class=\"chart-tick\" x=\"3\" y=\"12\">"+Math.round(max)+"</text>"+bars+"<text class=\"chart-time-label\" x=\"42\" y=\"100\">"+label+"</text><text class=\"chart-time-label\" x=\"270\" y=\"100\">"+text("今","Now")+"</text></svg>"}function setLive(value){if(live){live.textContent=value}}function updateCard(service,items){var card=root.querySelector("[data-telemetry-card=\""+service+"\"]"),status=card&&card.querySelector("[data-telemetry-status]"),details=card&&card.querySelector("[data-telemetry-details]");if(!status||!details){return}if(!items.length){status.className="status-pill neutral";status.textContent=text("未確認","Not checked");details.innerHTML="<div class=\"api-observation-not-checked\">"+text("未確認","Not checked")+"</div>";return}var last=items[items.length-1],value=Number(last.duration_ms)||0,label=select.value==="5m"?text("直近5分","Last 5 minutes"):text("直近60秒","Last 60 seconds");status.className="status-pill "+(last.success?"ok":"bad");status.textContent=last.success?text("正常","Healthy"):text("要確認","Needs review");details.innerHTML="<div class=\"api-observation-latency\"><strong data-telemetry-value>"+value+" ms</strong><span class=\"api-observation-label\">"+text("現在の応答時間","Current response time")+"</span><span class=\"api-observation-meta\" data-telemetry-meta>"+items.length+" / 20 "+text("観測","observations")+" · "+label+" · "+text("最終更新","Last updated")+" "+new Date(last.at).toLocaleTimeString()+"</span></div>"+graph(items)}function refresh(){if(busy){return}busy=true;fetch("/bot/api/setup/observability?window="+encodeURIComponent(select.value),{headers:{"X-Requested-With":"system-status-refresh"},cache:"no-store"}).then(function(response){if(!response.ok){throw new Error("snapshot failed")};return response.json()}).then(function(payload){updateCard("kitsu",payload.observations.kitsu||[]);updateCard("discord",payload.observations.discord||[]);setLive(text("自動更新","Auto-refresh"))}).catch(function(){setLive(text("更新失敗","Refresh unavailable"))}).finally(function(){busy=false})}select.addEventListener("change",refresh);timer=window.setInterval(refresh,interval);window.addEventListener("beforeunload",function(){window.clearInterval(timer)});refresh()})();</script>`
}

func systemStatusRefreshScriptSharedScale() string {
	return `<script data-system-status-refresh>(function(){var interval=5000,busy=false,timer,select=document.querySelector("[data-system-status-window]"),root=document.querySelector(".system-status-sections"),live=document.querySelector("[data-system-live-label]");if(!select||!root){return}function text(ja,en){return document.documentElement.lang==="ja"?ja:en}function scale(value){for(var i=0,values=[10,25,50,100,250,500,1000];i<values.length;i++){if(value<=values[i]){return values[i]}}return Math.ceil(value/100)*100}function graph(items,maxValue){if(!items.length){return ""}var width=320,height=104,left=42,right=8,top=8,bottom=82,windowMs=select.value==="5m"?300000:60000,now=Date.now(),positions=[];items.forEach(function(item){var ratio=1-(now-new Date(item.at).getTime())/windowMs;ratio=Math.max(0,Math.min(1,ratio));positions.push(left+ratio*(width-left-right))});var bars="";items.forEach(function(item,i){var bar=8;if(i>0){bar=Math.min(bar,positions[i]-positions[i-1]-2)}if(i+1<items.length){bar=Math.min(bar,positions[i+1]-positions[i]-2)}bar=Math.max(2,bar);var value=Number(item.duration_ms)||0,h=Math.max(2,(bottom-top)*value/maxValue),x=Math.max(left,Math.min(width-right-bar,positions[i]-bar/2)),cls=item.success?"bar-success":"bar-failure";bars+="<rect class=\""+cls+"\" x=\""+x.toFixed(1)+"\" y=\""+(bottom-h).toFixed(1)+"\" width=\""+bar.toFixed(1)+"\" height=\""+h.toFixed(1)+"\" rx=\"2\"><title>"+value+" ms</title></rect>"});var label=select.value==="5m"?text("直近5分","Last 5 minutes"):text("直近60秒","Last 60 seconds");return "<svg class=\"api-sparkline api-bar-chart\" viewBox=\"0 0 320 104\" role=\"img\" aria-label=\""+items.length+" observations, "+label+"\"><line class=\"chart-axis\" x1=\"42\" y1=\"82\" x2=\"312\" y2=\"82\"></line><line class=\"chart-axis\" x1=\"42\" y1=\"8\" x2=\"42\" y2=\"82\"></line><text class=\"chart-axis-label\" x=\"2\" y=\"12\">"+text("応答時間 (ms)","Response time (ms)")+"</text><text class=\"chart-tick\" x=\"3\" y=\"84\">0</text><text class=\"chart-tick\" x=\"3\" y=\"12\">"+Math.round(maxValue)+"</text>"+bars+"<text class=\"chart-time-label\" x=\"42\" y=\"100\">"+label+"</text><text class=\"chart-time-label\" x=\"270\" y=\"100\">"+text("今","Now")+"</text></svg>"}function setLive(value){if(live){live.textContent=value}}function updateCard(service,items,maxValue){var card=root.querySelector("[data-telemetry-card=\""+service+"\"]"),status=card&&card.querySelector("[data-telemetry-status]"),details=card&&card.querySelector("[data-telemetry-details]");if(!status||!details){return}if(!items.length){status.className="status-pill neutral";status.textContent=text("未確認","Not checked");details.innerHTML="<div class=\"api-observation-not-checked\">"+text("未確認","Not checked")+"</div>";return}var last=items[items.length-1],value=Number(last.duration_ms)||0,label=select.value==="5m"?text("直近5分","Last 5 minutes"):text("直近60秒","Last 60 seconds");status.className="status-pill "+(last.success?"ok":"bad");status.textContent=last.success?text("正常","Healthy"):text("要確認","Needs review");details.innerHTML="<div class=\"api-observation-latency\"><strong data-telemetry-value>"+value+" ms</strong><span class=\"api-observation-label\">"+text("現在の応答時間","Current response time")+"</span><span class=\"api-observation-meta\" data-telemetry-meta>"+items.length+" / 20 "+text("観測","observations")+" · "+label+" · "+text("最終更新","Last updated")+" "+new Date(last.at).toLocaleTimeString()+"</span></div>"+graph(items,maxValue)}function refresh(){if(busy){return}busy=true;fetch("/bot/api/setup/observability?window="+encodeURIComponent(select.value),{headers:{"X-Requested-With":"system-status-refresh"},cache:"no-store"}).then(function(response){if(!response.ok){throw new Error("snapshot failed")}return response.json()}).then(function(payload){var observations=payload.observations||{},maxValue=1;["kitsu","discord"].forEach(function(service){(observations[service]||[]).forEach(function(item){maxValue=Math.max(maxValue,Number(item.duration_ms)||0)})});maxValue=scale(maxValue);updateCard("kitsu",observations.kitsu||[],maxValue);updateCard("discord",observations.discord||[],maxValue);setLive(text("自動更新","Auto-refresh"))}).catch(function(){setLive(text("更新失敗","Refresh unavailable"))}).finally(function(){busy=false})}select.addEventListener("change",refresh);timer=window.setInterval(refresh,interval);window.addEventListener("beforeunload",function(){window.clearInterval(timer)});refresh()})();</script>`
}

func systemStatusRefreshScriptReadable() string {
	script := systemStatusRefreshScriptSharedScale()
	start := strings.Index(script, "function graph(items,maxValue){")
	endRel := strings.Index(script[start:], "function setLive")
	end := start + endRel
	if start < 0 || endRel < 0 {
		return script
	}
	graph := `function graph(items,maxValue){if(!items.length){return ""}var width=320,height=104,left=2,right=2,top=8,bottom=82,windowMs=select.value==="5m"?300000:60000,now=Date.now(),positions=[];items.forEach(function(item){var ratio=1-(now-new Date(item.at).getTime())/windowMs;ratio=Math.max(0,Math.min(1,ratio));positions.push(left+ratio*(width-left-right))});var bars="";items.forEach(function(item,i){var bar=8;if(i>0){bar=Math.min(bar,positions[i]-positions[i-1]-2)}if(i+1<items.length){bar=Math.min(bar,positions[i+1]-positions[i]-2)}bar=Math.max(2,bar);var value=Number(item.duration_ms)||0,h=Math.max(2,(bottom-top)*value/maxValue),x=Math.max(left,Math.min(width-right-bar,positions[i]-bar/2)),cls=item.success?"bar-success":"bar-failure";bars+="<rect class='"+cls+"' x='"+x.toFixed(1)+"' y='"+(bottom-h).toFixed(1)+"' width='"+bar.toFixed(1)+"' height='"+h.toFixed(1)+"' rx='2'><title>"+value+" ms</title></rect>"});var oldLabel=select.value==="5m"?text("5分前","5 min ago"):text("60秒前","60 sec ago"),middleLabel=select.value==="5m"?text("2分30秒前","2 min 30 sec ago"):text("30秒前","30 sec ago"),label=select.value==="5m"?text("直近5分","Last 5 minutes"):text("直近60秒","Last 60 seconds"),midValue=maxValue/2;return "<svg class='api-sparkline api-bar-chart' viewBox='0 0 320 104' role='img' aria-label='"+items.length+" observations, "+label+"'><line class='chart-axis' x1='2' y1='82' x2='318' y2='82'></line><line class='chart-axis' x1='2' y1='8' x2='2' y2='82'></line><line class='chart-guide' x1='2' y1='45' x2='318' y2='45'></line><text class='chart-axis-label' x='2' y='12'>"+text("応答時間 (ms)","Response time (ms)")+"</text><text class='chart-tick' x='3' y='12'>"+Math.round(maxValue)+"</text><text class='chart-tick' x='3' y='48'>"+Math.round(midValue)+"</text><text class='chart-tick' x='3' y='84'>0</text>"+bars+"<text class='chart-time-label' text-anchor='start' x='2' y='100'>"+oldLabel+"</text><text class='chart-time-label' text-anchor='middle' x='160' y='100'>"+middleLabel+"</text><text class='chart-time-label' text-anchor='end' x='318' y='100'>"+text("今","Now")+"</text></svg>"}`
	graph = strings.NewReplacer(
		"left=2,right=2", "left=54,right=2",
		"width=320,height=104", "width=466,height=104",
		"viewBox='0 0 320 104'", "viewBox='0 0 466 104'",
		"x2='318'", "x2='464'",
		"x='160'", "x='233'",
		"x='318'", "x='464'",
		"x1='2' y1='82'", "x1='54' y1='82'",
		"x1='2' y1='8'", "x1='54' y1='8'",
		"x2='2' y2='82'", "x2='54' y2='82'",
		"x1='2' y1='45'", "x1='54' y1='45'",
		"x='2' y='100'", "x='54' y='100'",
		"x='3' y='12'", "x='0' y='12'",
		"x='3' y='48'", "x='0' y='48'",
		"x='3' y='84'", "x='0' y='84'",
		`text("60秒前","60 sec ago")`, `text("60秒","60s")`,
		`text("30秒前","30 sec ago")`, `text("30秒","30s")`,
		`text("5分前","5 min ago")`, `text("5分","5m")`,
		`text("2分30秒前","2 min 30 sec ago")`, `text("2分30秒","2m30s")`,
	).Replace(graph)
	graph = strings.ReplaceAll(graph, `<text class='chart-tick' x='0'`, `<text class='chart-tick' text-anchor='end' x='48'`)
	graph = addDynamicObservationAccessibility(graph)
	graph = addLatencyTickUnits(removeChartAxisTitle(graph))
	script = script[:start] + graph + script[end:]
	script = strings.ReplaceAll(script, "values=[10,25,50,100,250,500,1000]", "values=[10,25,50,100,250,500,1000,2000]")
	script = strings.ReplaceAll(script, `+graph(items,maxValue)}function refresh`, `+graph(items,maxValue);var telemetryMeta=details.querySelector("[data-telemetry-meta]");if(telemetryMeta){telemetryMeta.textContent=text("最終更新","Last updated")+" "+new Date(last.at).toLocaleTimeString()}}function refresh`)
	script = strings.ReplaceAll(script, `+graph(items)}function refresh`, `+graph(items);var telemetryMeta=details.querySelector("[data-telemetry-meta]");if(telemetryMeta){telemetryMeta.textContent=text("最終更新","Last updated")+" "+new Date(last.at).toLocaleTimeString()}}function refresh`)
	script = strings.ReplaceAll(script, `function updateCard`, `var serviceScales={};function serviceScale(name,items){var max=1;items.forEach(function(item){max=Math.max(max,Number(item.duration_ms)||0)});var required=scale(max),state=serviceScales[name]||{ceiling:0,downSince:0},now=Date.now();if(!state.ceiling||required>state.ceiling){state.ceiling=required;state.downSince=0}else if(required<state.ceiling){if(!state.downSince){state.downSince=now}else if(now-state.downSince>=15000){state.ceiling=required;state.downSince=0}}serviceScales[name]=state;return state.ceiling}function updateCard`)
	script = strings.ReplaceAll(script, `updateCard("kitsu",observations.kitsu||[],maxValue);updateCard("discord",observations.discord||[],maxValue);`, `updateCard("kitsu",observations.kitsu||[],serviceScale("kitsu",observations.kitsu||[]));updateCard("discord",observations.discord||[],serviceScale("discord",observations.discord||[]));`)
	return script
}

func addDynamicObservationAccessibility(graph string) string {
	graph = strings.ReplaceAll(graph,
		`bars+="<rect class='"+cls+"'`,
		`var tooltipStatus=item.success?text("正常","Healthy"):text("リクエスト失敗","Request failed"),tooltipDuration=item.success?value+" ms":"",tooltipText=(new Date(item.at).toLocaleTimeString()+" "+tooltipDuration+" "+tooltipStatus).trim();bars+="<rect class='"+cls+"' tabindex='0' role='img' aria-label='"+tooltipText+"'`)
	graph = strings.ReplaceAll(graph,
		`<title>"+value+" ms</title>`,
		`<title>"+tooltipText+"</title>`)
	return graph
}

func applyDashboardMetricSemantics(body string, attentionCount, failureCount int, readinessClass string) string {
	attentionClass := "semantic-good"
	if attentionCount > 0 {
		attentionClass = "semantic-warning"
	}
	failureClass := "semantic-good"
	if failureCount > 0 {
		failureClass = "semantic-danger"
	}
	systemClass := "semantic-warning"
	if readinessClass == "success" || readinessClass == "ok" {
		systemClass = "semantic-good"
	} else if readinessClass == "danger" || readinessClass == "bad" {
		systemClass = "semantic-danger"
	}
	for _, class := range []string{"semantic-neutral", attentionClass, failureClass, systemClass} {
		idx := strings.Index(body, `class="metric-card"`)
		if idx < 0 {
			break
		}
		body = body[:idx] + `class="metric-card ` + class + `"` + body[idx+len(`class="metric-card"`):]
	}
	return body
}

func removeDashboardSubtitle(body string) string {
	start := strings.Index(body, `<section class="dashboard-intro">`)
	if start < 0 {
		return body
	}
	relEnd := strings.Index(body[start:], `</section>`)
	if relEnd < 0 {
		return body
	}
	end := start + relEnd
	section := body[start:end]
	pStart := strings.Index(section, `<p class="hint">`)
	if pStart < 0 {
		return body
	}
	pRelEnd := strings.Index(section[pStart:], `</p>`)
	if pRelEnd < 0 {
		return body
	}
	pEnd := pStart + pRelEnd + len(`</p>`)
	section = section[:pStart] + section[pEnd:]
	return body[:start] + section + body[end:]
}

func renderDashboardMenu(lang string, r *http.Request, db *gorm.DB, productionCount, attentionCount int, readiness SharedBotRuntimeReadiness) string {
	items := []struct {
		key, path, ja, en, statusA, statusB, statusAClass, statusBClass string
	}{
		{"ia.production_list", "/bot/admin/projects", "接続済みプロダクションの状態と設定を確認します。", "Review connected Productions and settings.", fmt.Sprintf("%d件", productionCount), fmt.Sprintf("要対応 %d件", attentionCount), map[bool]string{true: "ok", false: "muted"}[productionCount > 0], map[bool]string{true: "warning", false: "ok"}[attentionCount > 0]},
		{"ia.user_mapping", "/bot/admin/users", "KitsuユーザーとDiscordユーザーの紐づけを管理します。", "Manage Kitsu-to-Discord user links.", fmt.Sprintf("%d件", len(model.ListUserMap(db))), "ローカル設定", map[bool]string{true: "ok", false: "muted"}[len(model.ListUserMap(db)) > 0], "muted"},
		{"connections.title", "/bot/admin/bot", "Kitsu接続とDiscord Bot接続を設定します。", "Configure the Kitsu and Discord Bot connections.", statusText(lang, readiness.KitsuConfigured), statusText(lang, readiness.DiscordConfigured), map[bool]string{true: "ok", false: "warning"}[readiness.KitsuConfigured], map[bool]string{true: "ok", false: "warning"}[readiness.DiscordConfigured]},
		{"ia.system_status", "/bot/admin/health", "接続状態と通知の利用可否を確認します。", "Review connection state and notification availability.", readinessViewFor(lang, r, readiness).Label, statusTextOverall(lang, readiness.OverallReady), map[bool]string{true: "ok", false: "warning"}[readiness.OverallReady], map[bool]string{true: "ok", false: "warning"}[readiness.OverallReady]},
		{"ia.audit_log", "/bot/admin/audit", "操作履歴と通知イベントを確認します。", "Review action history and notification events.", fmt.Sprintf("%d件", len(model.ListAuditLogs(db, 5))), "最近の記録", map[bool]string{true: "ok", false: "muted"}[len(model.ListAuditLogs(db, 5)) > 0], "muted"},
	}
	var cards strings.Builder
	for i, item := range items {
		description := item.ja
		if lang == "en" {
			description = item.en
		}
		cards.WriteString(`<a class="dashboard-menu-card" href="` + esc(withLang(item.path, r)) + `"><span class="dashboard-menu-icon" aria-hidden="true">` + strconv.Itoa(i+1) + `</span><span class="dashboard-menu-copy"><strong>` + esc(tr(lang, item.key)) + `</strong><span class="field-help">` + esc(description) + `</span></span><span class="dashboard-menu-status"><span class="dashboard-status-chip ` + esc(item.statusAClass) + `">` + esc(item.statusA) + `</span><span class="dashboard-status-chip ` + esc(item.statusBClass) + `">` + esc(item.statusB) + `</span></span></a>`)
	}
	return `<div class="dashboard-menu-wrap"><section class="dashboard-cta" aria-labelledby="dashboard-new-connection"><div><span class="dashboard-cta-kicker">` + esc(t(lang, "ia.new_connection", "New Production Connection")) + `</span><h2 id="dashboard-new-connection">` + esc(t(lang, "ia.new_connection", "New Production Connection")) + `</h2><p class="hint">` + esc(t(lang, "新しいプロダクション接続はセットアップから進めます。", "Start a new Production connection from the setup flow.")) + `</p></div><a class="btn dashboard-cta-action" href="` + esc(withLang("/bot/setup", r)) + `">` + esc(t(lang, "新しいプロダクションを接続", "Open setup")) + `</a></section><section class="dashboard-menu" aria-labelledby="dashboard-menu-title"><div class="page-heading"><div><h2 id="dashboard-menu-title">` + esc(t(lang, "管理メニュー", "Management")) + `</h2><p class="hint">` + esc(t(lang, "主要な管理機能へアクセスします。", "Access the main management areas.")) + `</p></div></div><div class="dashboard-menu-grid">` + cards.String() + `</div></section></div>`
}

func renderIAProductionList(w http.ResponseWriter, r *http.Request, db *gorm.DB, fallbackGuildID string) {
	lang := currentLang(r)
	if selectedID := strings.TrimSpace(r.URL.Query().Get("project")); selectedID != "" {
		if p := model.FindProjectByKitsuID(db, selectedID); p != nil {
			renderIASelectedProduction(w, r, db, *p, fallbackGuildID)
			return
		}
		if p := liveProjectPreview(db, selectedID); p != nil {
			renderIASelectedProduction(w, r, db, *p, fallbackGuildID)
			return
		}
	}
	var rows strings.Builder
	for _, p := range availableProjects(db) {
		class, label := productionConnectionStatus(p, lang)
		_, _, hint := iaStatus(db, p, lang)
		rows.WriteString(fmt.Sprintf(`<article class="section-card glass production-list-item"><div><h2>%s</h2><p class="field-help">%s</p></div><div class="production-list-state"><span class="status-pill %s">%s</span><span class="field-help">%s</span></div><a class="btn" href="%s">%s</a></article>`, esc(p.Name), esc(t(lang, "現在の状態", "Current state")), class, esc(label), esc(hint), esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID), r)), esc(t(lang, "Productionを開く", "Open Production"))))
	}
	if rows.Len() == 0 {
		rows.WriteString(emptyState("-", t(lang, "Productionがありません", "No Productions"), t(lang, "新しいProductionを接続してください。", "Connect a new Production.")))
	}
	body := `<div class="section-stack"><section class="section-card glass"><p class="hint">` + esc(t(lang, "Production一覧で状態を確認し、選択したProductionを開きます。設定は選択後に表示されます。", "Use this list to review states and open one Production. Settings appear only after selection.")) + `</p></section>` + rows.String() + `</div>`
	body = strings.Replace(body, rows.String(), simplifyProductionListRows(rows.String()), 1)
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.production_list"), r, body))
}

func simplifyProductionListRows(rows string) string {
	start := strings.Index(rows, `<article class="section-card glass production-list-item">`)
	if start < 0 {
		return rows
	}
	relEnd := strings.Index(rows[start:], `</article>`)
	if relEnd < 0 {
		return rows
	}
	end := start + relEnd + len(`</article>`)
	article := removeElementWithClass(rows[start:end], `<p class="field-help">`, `</p>`)
	article = removeElementWithClass(article, `<span class="field-help">`, `</span>`)
	return rows[:start] + article + simplifyProductionListRows(rows[end:])
}

func removeElementWithClass(body, open, close string) string {
	for {
		start := strings.Index(body, open)
		if start < 0 {
			return body
		}
		relEnd := strings.Index(body[start+len(open):], close)
		if relEnd < 0 {
			return body
		}
		end := start + len(open) + relEnd + len(close)
		body = body[:start] + body[end:]
	}
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
	if p.ReadOnlyPreview {
		fmt.Fprint(w, adminPage(lang, "", r, renderIAUnconnectedProduction(r, p, lang)))
		return
	}
	tab := selectedProductionTab(r.URL.Query().Get("tab"))
	class, label, hint := iaStatus(db, p, lang)
	headerClass, headerLabel := "success", t(lang, "接続済", "Connected")
	if p.ValidationOnly {
		headerClass, headerLabel = "warning", label
	}
	serverName := projectDiscordServerName(db, p, lang)
	tabs := []struct{ id, key string }{{"overview", "ia.overview"}, {"notifications", "ia.notifications"}, {"users", "ia.user_settings"}, {"storage-settings", "ia.storage_settings"}, {"activity", "ia.activity"}, {"troubleshooting", "ia.troubleshooting"}, {"advanced", "ia.advanced_current"}, {"danger-zone", "ia.danger"}}
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
	header := `<div class="production-context"><div class="page-heading"><div><div class="eyebrow">` + esc(tr(lang, "ia.productions")) + `</div><h1>` + esc(p.Name) + `</h1><p class="hint">` + esc(t(lang, "選択中のプロダクション", "Selected Production")) + `</p></div><span class="status-pill ` + esc(headerClass) + `">` + esc(headerLabel) + `</span></div><nav class="section-nav production-tabs" role="tablist" aria-label="` + esc(t(lang, "プロダクションのセクション", "Production sections")) + `">` + tabLinks.String() + `</nav>`
	body := header + `<section id="panel-` + esc(tab) + `" role="tabpanel" aria-labelledby="tab-` + esc(tab) + `" tabindex="0" class="section-stack production-tabpanel">` + renderProductionPanelMarkup(db, r, p, lang, tab, class, label, hint, serverName) + `</section></div>`
	body += `<script>(function(){var list=document.querySelector('[role="tablist"]');if(!list)return;var tabs=Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));list.addEventListener('keydown',function(e){var i=tabs.indexOf(document.activeElement);if(i<0)return;var n=i;if(e.key==='ArrowRight')n=(i+1)%tabs.length;if(e.key==='ArrowLeft')n=(i-1+tabs.length)%tabs.length;if(e.key==='Home')n=0;if(e.key==='End')n=tabs.length-1;if(n!==i){e.preventDefault();tabs[n].focus();tabs[n].click()}})})();</script>`
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func renderIAUnconnectedProduction(r *http.Request, p model.Project, lang string) string {
	configureURL := withLang("/bot/setup?project="+url.QueryEscape(p.KitsuProjectID), r)
	productionListURL := withLang("/bot/admin/projects", r)
	return `<div class="section-stack"><section class="section-card glass unconnected-production" aria-labelledby="unconnected-production-title"><div class="page-heading"><div><div class="eyebrow">` + esc(tr(lang, "ia.productions")) + `</div><h1 id="unconnected-production-title">` + esc(p.Name) + `</h1><p class="hint">` + esc(tr(lang, "production.unconnected.source")) + `</p></div><span class="status-pill blocked" role="status">` + esc(tr(lang, "production.unconnected.status")) + `</span></div><p class="state-explanation" role="status">` + esc(tr(lang, "production.unconnected.explanation")) + `</p><div class="button-row"><a class="btn" href="` + esc(configureURL) + `">` + esc(tr(lang, "production.unconnected.configure")) + `</a><a class="btn-ghost" href="` + esc(productionListURL) + `">` + esc(tr(lang, "production.unconnected.back")) + `</a></div></section></div>`
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
	case "notifications", "users", "user-settings", "storage-settings", "activity", "troubleshooting", "advanced", "danger-zone":
		return raw
	default:
		return "overview"
	}
}

func renderProductionPanelMarkup(db *gorm.DB, r *http.Request, p model.Project, lang, tab, class, label, hint, serverName string) string {
	panel := renderSelectedProductionPanel(db, r, p, lang, tab, class, label, hint, serverName)
	if tab != "overview" {
		if tab == "notifications" && strings.TrimSpace(hint) != "" {
			panel = strings.Replace(panel, esc(hint), "", 1)
		}
		return panel
	}
	panel = strings.Replace(panel, `<dl class="status-list">`, `<div class="production-summary-grid">`, 1)
	panel = strings.Replace(panel, `</dl></section>`, `</div></section>`, 1)
	for i := 0; i < 4; i++ {
		panel = strings.Replace(panel, `<div class="status-row">`, `<div class="status-row production-summary-card">`, 1)
	}
	panel = strings.Replace(panel, `<div class="status-row">`, `<div class="status-row production-current-issues">`, 1)
	for {
		start := strings.Index(panel, `<dd class="status-row-explanation">`)
		if start < 0 {
			break
		}
		end := strings.Index(panel[start:], `</dd>`)
		if end < 0 {
			break
		}
		end += start + len(`</dd>`)
		panel = panel[:start] + `<dd class="status-row-explanation" aria-hidden="true"></dd>` + panel[end:]
	}
	return panel
}

func renderSelectedProductionPanel(db *gorm.DB, r *http.Request, p model.Project, lang, tab, class, label, hint, serverName string) string {
	switch tab {
	case "notifications":
		if p.ReadOnlyPreview {
			return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.notifications")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "notification_state", "Notification state"), "blocked", t(lang, "unavailable", "Unavailable"), t(lang, "このProductionはKitsuSyncに接続されていないため、通知は利用できません。", "Notifications are unavailable because this Production is not connected to KitsuSync."), "") + `</dl><p class="field-help" role="status">` + esc(t(lang, "送信せずに確認する表示です。", "This read-only preview never sends a Discord message.")) + `</p><h3>` + esc(t(lang, "Task Type", "Task Types")) + `</h3><ul class="mapping-list">` + renderValidationTaskTypes(p, lang) + `</ul></section>`
		}
		if p.ValidationOnly || p.ReadOnlyPreview {
			return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.notifications")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "通知状態", "Notification state"), "blocked", t(lang, "利用できません", "Unavailable"), t(lang, "検証専用ProductionではDiscordサーバーが未接続のため、通知は利用できません。", "Notifications are unavailable because this validation-only Production has no Discord server connected."), "") + `</dl><p class="field-help" role="status">` + esc(t(lang, "この表示確認ではDiscordメッセージを送信しません。", "This validation view never sends a Discord message.")) + `</p><h3>` + esc(t(lang, "Task Type", "Task Types")) + `</h3><ul class="mapping-list">` + renderValidationTaskTypes(p, lang) + `</ul></section>`
		}
		return renderSelectedProductionNotifications(db, r, p, lang, class, label, hint)
	case "users", "user-settings":
		return renderCurrentProductionUserSettings(db, r, p, lang)
	case "storage-settings":
		if p.ValidationOnly || p.ReadOnlyPreview {
			return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.storage_settings")) + `</h2><p class="field-help" role="status">` + esc(t(lang, "検証専用Productionではストレージ設定を変更できません。", "Storage settings are read-only for validation-only Productions.")) + `</p></section>`
		}
		return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.storage_settings")) + `</h2><p class="hint">` + esc(t(lang, "このProductionの保存先とリンクを管理します。", "Manage storage destinations and links for this Production.")) + `</p><form method="POST" action="` + esc(withLang("/bot/admin/drive", r)) + `" class="form-stack"><input type="hidden" name="kitsu_project_id" value="` + esc(p.KitsuProjectID) + `"><label for="storage-url">` + esc(t(lang, "保存先リンク", "Storage link")) + `</label><input id="storage-url" type="url" name="storage_url" value="` + esc(p.StorageURL) + `"><div class="button-row"><button class="btn" type="submit">` + esc(t(lang, "保存", "Save")) + `</button></div></form></section>`
	case "activity":
		return renderSelectedProductionActivity(db, p, lang)
	case "troubleshooting":
		return renderCurrentProductionTroubleshooting(db, p, lang)
	case "advanced":
		return renderCurrentProductionDetails(p, lang)
		/*
			validation := ""
			if p.ValidationOnly || p.ReadOnlyPreview {
				validation = `<dt>` + esc(t(lang, "検証モード", "Validation mode")) + `</dt><dd>` + esc(t(lang, "検証専用・変更不可", "Validation only; changes disabled")) + `</dd>`
			}
			return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.advanced_current")) + `</h2><dl class="detail-list">` + validation + `<dt>Production ID</dt><dd><code>` + esc(p.KitsuProjectID) + `</code></dd><dt>Discord server ID</dt><dd><code>` + esc(p.DiscordGuildID) + `</code></dd><dt>Category ID</dt><dd><code>` + esc(p.DiscordCategoryID) + `</code></dd></dl></section>`
		*/
	case "danger-zone":
		return strings.Replace(renderSelectedProductionDanger(r, p, lang), `value="preview_remove_connection_with_discord"`, `value="execute_current_ia_discord_delete"`, 1)
	default:
		productionLabel := func(jp, en string) string {
			if lang == "en" {
				return en
			}
			return jp
		}
		productionStateClass := "success"
		productionStateLabel := productionLabel("接続済", "Connected")
		if p.ValidationOnly {
			productionStateClass = "warning"
			productionStateLabel = productionLabel("検証専用", "Validation only")
		}
		if p.ReadOnlyPreview {
			productionStateClass = "warning"
			productionStateLabel = productionLabel("未接続", "Disconnected")
		}
		notificationClass, notificationLabel, notificationHint := iaStatus(db, p, lang)
		switch notificationClass {
		case "success", "ok":
			notificationLabel = productionLabel("正常", "Healthy")
		case "warning", "danger", "bad":
			notificationLabel = productionLabel("要確認", "Needs review")
		case "blocked":
			notificationLabel = productionLabel("利用不可", "Unavailable")
		default:
			notificationLabel = productionLabel("未設定", "Not configured")
		}
		if p.ReadOnlyPreview {
			notificationClass, notificationLabel, notificationHint = "warning", productionLabel("未接続", "Disconnected"), productionLabel("このプロダクションは未接続です。", "This Production is not connected.")
		}
		participantCount := len(ListKitsuProjectParticipants(p.KitsuProjectID))
		overviewProblem := productionLabel("問題なし", "No current issues")
		overviewProblemClass := "success"
		if len(model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 10)) > 0 {
			overviewProblem, overviewProblemClass = productionLabel("要確認", "Needs review"), "warning"
		}
		return `<section class="section-card glass"><h2>` + esc(productionLabel("概要", "Overview")) + `</h2><dl class="status-list">` + statusSummaryRow(productionLabel("プロダクション状態", "Production state"), productionStateClass, productionStateLabel, "", "") + statusSummaryRow(productionLabel("Discord接続状態", "Discord connection"), map[bool]string{true: "success", false: "warning"}[strings.TrimSpace(p.DiscordGuildID) != ""], map[bool]string{true: productionLabel("接続済", "Connected"), false: productionLabel("未接続", "Disconnected")}[strings.TrimSpace(p.DiscordGuildID) != ""], "", "") + statusSummaryRow(productionLabel("通知ルーティング状態", "Notification routing"), normalizeStatusClass(notificationClass), notificationLabel, notificationHint, "") + statusSummaryRow(productionLabel("ユーザー/参加者", "Users / participants"), "neutral", fmt.Sprintf("%d", participantCount), "", "") + statusSummaryRow(productionLabel("現在の問題", "Current issues"), overviewProblemClass, overviewProblem, "", "") + `</dl></section>`
	}
}

func renderCurrentProductionDetails(p model.Project, lang string) string {
	validation := ""
	if p.ValidationOnly || p.ReadOnlyPreview {
		validation = `<dt>` + esc(t(lang, "検証モード", "Validation mode")) + `</dt><dd>` + esc(t(lang, "検証専用・変更不可", "Validation only; changes disabled")) + `</dd>`
	}
	return `<section class="section-card glass"><h2>` + esc(t(lang, "詳細情報", "Details")) + `</h2><dl class="detail-list">` + validation + `<dt>` + esc(t(lang, "プロダクションID", "Production ID")) + `</dt><dd><code>` + esc(p.KitsuProjectID) + `</code></dd><dt>` + esc(t(lang, "DiscordサーバーID", "Discord server ID")) + `</dt><dd><code>` + esc(p.DiscordGuildID) + `</code></dd><dt>` + esc(t(lang, "カテゴリID", "Category ID")) + `</dt><dd><code>` + esc(p.DiscordCategoryID) + `</code></dd></dl></section>`
}

func renderCurrentProductionUserSettings(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	body := renderSelectedProductionUserSettings(db, r, p, lang)
	if lang == "en" {
		body = strings.ReplaceAll(body, "Participants appear here when they are registered in Kitsu.", "Participants appear here when they are returned by Kitsu. Reviewer / Checker assignment becomes available then.")
		body = strings.ReplaceAll(body, "Assign these roles per Task Type for this Production.", "Reviewer / Checker assignment is unavailable until participants are returned by Kitsu.")
		return body
	}
	// Keep the existing data-path renderer while making the empty state use the
	// current IA vocabulary and explain why role assignment is unavailable.
	body = strings.ReplaceAll(body, "Production参加者", "プロダクション参加者")
	body = strings.ReplaceAll(body, "Kitsu側の参加者が登録されると、ここに表示されます。", "Kitsuから参加者が取得されると、ここに表示されます。Reviewer / Checkerの割り当ても可能になります。")
	body = strings.ReplaceAll(body, "Task TypeごとにProduction単位で設定します。", "Kitsuから参加者が取得されるまで、Reviewer / Checkerは割り当てできません。")
	return body
}

func renderSelectedProductionNotifications(db *gorm.DB, r *http.Request, p model.Project, lang, class, label, hint string) string {
	statusLabel := t(lang, "未設定", "Not configured")
	switch class {
	case "success", "ok":
		statusLabel = t(lang, "正常", "Healthy")
	case "warning", "danger", "bad":
		statusLabel = t(lang, "要確認", "Needs review")
	case "blocked":
		statusLabel = t(lang, "利用不可", "Unavailable")
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.notifications")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "通知状態", "Notification state"), normalizeStatusClass(class), statusLabel, hint, "") + `</dl>` + renderCurrentIARoutingEditor(db, r, p, lang) + renderCurrentIANotificationPreview(db, r, p, lang) + `</section>`
}

func renderSelectedProductionUserSettings(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	global := map[string]model.UserMap{}
	for _, u := range model.ListUserMap(db) {
		global[strings.ToLower(strings.TrimSpace(u.KitsuName))] = u
	}
	var participants, roles strings.Builder
	participantPeople := ListKitsuProjectParticipants(p.KitsuProjectID)
	if len(participantPeople) > 0 {
		globalByEmail := map[string]model.UserMap{}
		for _, user := range global {
			globalByEmail[strings.ToLower(strings.TrimSpace(user.KitsuEmail))] = user
		}
		for _, person := range filterAssignablePersons(participantPeople, botAccountEmail(db)) {
			identity := t(lang, "未対応", "Not mapped")
			if user, ok := globalByEmail[strings.ToLower(strings.TrimSpace(person.Email))]; ok && strings.TrimSpace(user.DiscordID) != "" {
				identity = t(lang, "対応付け済み", "Mapped")
				if strings.TrimSpace(user.DiscordDisplayName) != "" {
					identity = strings.TrimSpace(user.DiscordDisplayName)
				}
			}
			participants.WriteString(`<li><strong>` + esc(person.FullName) + `</strong><span class="status-pill warn">` + esc(identity) + `</span><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users?project="+url.QueryEscape(p.KitsuProjectID), r)) + `">` + esc(t(lang, "ユーザー紐づけ", "User Linking")) + `</a></li>`)
		}
	}
	if len(participantPeople) == 0 {
		for _, u := range model.ListProjectUserMaps(db, p.ID) {
			identity := t(lang, "未対応", "Not mapped")
			action := `<a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(tr(lang, "ia.user_mapping")) + `</a>`
			if g, ok := global[strings.ToLower(strings.TrimSpace(u.KitsuName))]; ok && strings.TrimSpace(g.DiscordID) != "" {
				identity = t(lang, "対応付け済み", "Mapped")
				action = ""
				if strings.TrimSpace(g.DiscordDisplayName) != "" {
					identity = strings.TrimSpace(g.DiscordDisplayName)
				}
			}
			participants.WriteString(`<li><strong>` + esc(u.KitsuName) + `</strong><span class="status-pill ` + map[bool]string{true: "ok", false: "warn"}[identity != t(lang, "未対応", "Not mapped")] + `">` + esc(identity) + `</span>` + action + `</li>`)
		}
	}
	if (p.ValidationOnly || p.ReadOnlyPreview) && participants.Len() == 0 {
		for _, person := range p.ValidationData().Participants {
			participants.WriteString(`<li><strong>` + esc(person.FullName) + `</strong><span class="status-pill warn">` + esc(t(lang, "未設定", "Not linked")) + `</span></li>`)
		}
	}
	if participants.Len() == 0 {
		participants.WriteString(`<li class="empty-state"><strong>` + esc(t(lang, "Production参加者はまだ登録されていません。", "No Production participants are registered yet.")) + `</strong><span class="field-help">` + esc(t(lang, "Kitsu側の参加者が登録されると、ここに表示されます。", "Participants appear here when they are registered in Kitsu.")) + `</span></li>`)
	}
	for _, c := range model.ListProjectCheckerMaps(db, p.ID) {
		roles.WriteString(`<li><strong>` + esc(c.TaskType) + `</strong><span>` + esc(c.KitsuName) + `</span></li>`)
	}
	if roles.Len() == 0 {
		roles.WriteString(`<li class="empty-state"><strong>` + esc(t(lang, "Reviewer / Checkerの割り当てはありません。", "No Reviewer / Checker assignments yet.")) + `</strong><span class="field-help">` + esc(t(lang, "Task TypeごとにProduction単位で設定します。", "Assign these roles per Task Type for this Production.")) + `</span></li>`)
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.user_settings")) + `</h2><div class="settings-block"><h3>` + esc(t(lang, "Production参加者", "Production participants")) + `</h3><ul class="mapping-list">` + participants.String() + `</ul></div><div class="settings-block"><h3>` + esc(t(lang, "Reviewer / Checker", "Reviewer / Checker")) + `</h3><ul class="mapping-list">` + roles.String() + `</ul></div><p class="field-help">` + esc(t(lang, "Discordユーザーの紐づけはグローバルなユーザー紐づけで管理します。", "Discord user linking is managed in global User Linking.")) + `</p><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(tr(lang, "ia.user_mapping")) + `</a></section>`
}

func renderValidationTaskTypes(p model.Project, lang string) string {
	data := p.ValidationData()
	if len(data.TaskTypes) == 0 {
		return `<li class="empty-state"><strong>` + esc(t(lang, "Task Typeは取得できませんでした", "No Task Types were returned")) + `</strong></li>`
	}
	var rows strings.Builder
	for _, taskType := range data.TaskTypes {
		rows.WriteString(`<li><strong>` + esc(taskType.Name) + `</strong><span class="field-help">` + esc(t(lang, "検証専用の表示データ", "Validation-only display data")) + `</span></li>`)
	}
	return rows.String()
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
		rows.WriteString(`<li class="empty-state"><strong>` + esc(t(lang, "アクティビティはありません。", "No activity yet.")) + `</strong></li>`)
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.activity")) + `</h2><ul class="activity-list" role="log">` + rows.String() + `</ul></section>`
}

func renderCurrentProductionTroubleshooting(db *gorm.DB, p model.Project, lang string) string {
	diagnoses := model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 10)
	problemClass, problemLabel := "success", t(lang, "問題なし", "No current problems")
	if len(diagnoses) > 0 {
		problemClass, problemLabel = "warning", t(lang, "要確認", "Needs review")
	}
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	routes := model.ListProductionNotificationRoutes(db, p.KitsuProjectID)
	config := model.FindProductionNotificationConfig(db, p.KitsuProjectID)
	participantCount := len(ListKitsuProjectParticipants(p.KitsuProjectID))
	linkedCount := len(model.ListProjectUserMaps(db, p.ID))
	recentCount, recentFailures := 0, 0
	for _, log := range model.ListAuditLogs(db, 40) {
		if strings.EqualFold(strings.TrimSpace(log.ProjectName), strings.TrimSpace(p.Name)) {
			recentCount++
			if !log.Success {
				recentFailures++
			}
		}
	}
	item := func(label, value, class, explanation string) string {
		return `<div class="production-diagnostic-item"><div><strong>` + esc(label) + `</strong><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(value) + `</span></div><small>` + esc(explanation) + `</small></div>`
	}
	kitsuValue, kitsuClass := t(lang, "未設定", "Not configured"), "warning"
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) != "" {
		kitsuValue, kitsuClass = t(lang, "接続済", "Connected"), "success"
	} else if readiness.KitsuConfigured {
		kitsuValue = t(lang, "要確認", "Needs review")
	}
	discordValue, discordClass := t(lang, "未設定", "Not configured"), "warning"
	if readiness.DiscordConfigured {
		discordValue, discordClass = t(lang, "設定済", "Configured"), "success"
	}
	routingValue, routingClass := t(lang, "未設定", "Not configured"), "warning"
	if config != nil && config.Enabled && len(routes) > 0 {
		routingValue, routingClass = t(lang, "正常", "Healthy"), "success"
	}
	participantValue, participantClass := t(lang, "確認待", "Waiting"), "warning"
	if participantCount > 0 {
		participantValue, participantClass = t(lang, "取得済", "Loaded"), "success"
	}
	linkValue, linkClass := t(lang, "記録なし", "No records"), "neutral"
	if linkedCount > 0 {
		linkValue, linkClass = t(lang, "設定済", "Configured"), "success"
	}
	processingValue, processingClass := t(lang, "記録なし", "Not recorded"), "neutral"
	processingExplanation := t(lang, "このProductionの通知処理記録はありません。", "No notification processing records exist for this Production.")
	if recentCount > 0 {
		processingValue, processingClass = t(lang, "正常", "Healthy"), "success"
		processingExplanation = fmt.Sprintf(t(lang, "直近の記録%d件、失敗%d件。", "%d recent records, %d failures."), recentCount, recentFailures)
		if recentFailures > 0 {
			processingValue, processingClass = t(lang, "要確認", "Needs review"), "warning"
		}
	}
	var diagnosticDetails strings.Builder
	if len(diagnoses) > 0 {
		diagnosticDetails.WriteString(`<details class="advanced-details" open><summary>` + esc(t(lang, "現在の問題の詳細", "Current issue details")) + `</summary><ul class="list-tight">`)
		for _, diagnosis := range diagnoses {
			detail := strings.TrimSpace(diagnosis.Detail)
			if detail == "" {
				detail = strings.TrimSpace(diagnosis.Reason)
			}
			if detail != "" {
				diagnosticDetails.WriteString(`<li>` + esc(detail) + `</li>`)
			}
		}
		diagnosticDetails.WriteString(`</ul></details>`)
	}
	diagnosticDetails.WriteString(`<details class="advanced-details production-diagnostics"><summary>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</summary><div class="production-diagnostic-grid">`)
	diagnosticDetails.WriteString(item(t(lang, "Kitsu接続", "Kitsu connection"), kitsuValue, kitsuClass, t(lang, "現在のランタイム認証状態。", "Current runtime authentication state.")))
	diagnosticDetails.WriteString(item(t(lang, "Discord Bot", "Discord Bot"), discordValue, discordClass, t(lang, "現在のBot設定。", "Current Bot configuration.")))
	diagnosticDetails.WriteString(item(t(lang, "通知ルーティング", "Notification routing"), routingValue, routingClass, fmt.Sprintf(t(lang, "%d件のProductionルートと設定を確認しました。", "%d Production routes and the configuration were inspected."), len(routes))))
	diagnosticDetails.WriteString(item(t(lang, "参加者取得", "Participant retrieval"), participantValue, participantClass, fmt.Sprintf(t(lang, "Kitsu Production team APIから%d人を取得しました。", "The Kitsu Production team API returned %d people."), participantCount)))
	diagnosticDetails.WriteString(item(t(lang, "ユーザー紐づけ", "User linking"), linkValue, linkClass, fmt.Sprintf(t(lang, "このProductionのユーザー割り当て%d件。", "User assignments recorded for this Production: %d."), linkedCount)))
	diagnosticDetails.WriteString(item(t(lang, "最近の通知処理", "Recent notification processing"), processingValue, processingClass, processingExplanation))
	diagnosticDetails.WriteString(`</div></details>`)
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.troubleshooting")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "現在の問題", "Current problem"), problemClass, problemLabel, "", "") + `</dl>` + diagnosticDetails.String() + `</section>`
}

func renderSelectedProductionTroubleshooting(db *gorm.DB, p model.Project, lang string) string {
	diagnoses := model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 10)
	has := len(diagnoses) > 0
	class, value := "success", t(lang, "問題なし", "No current problems")
	if has {
		class, value = "warning", t(lang, "確認が必要", "Needs review")
	}
	var details strings.Builder
	if has {
		details.WriteString(`<ul class="list-tight">`)
		detailCount := 0
		for _, diagnosis := range diagnoses {
			text := strings.TrimSpace(diagnosis.Detail)
			if text == "" {
				text = strings.TrimSpace(diagnosis.Reason)
			}
			if text == "" {
				continue
			}
			detailCount++
			details.WriteString(`<li>` + esc(text) + `</li>`)
		}
		details.WriteString(`</ul>`)
		if detailCount == 0 {
			details.WriteString(`<p class="field-help">` + esc(t(lang, "詳細な診断情報はありません。", "No additional diagnostic detail is available.")) + `</p>`)
		}
	}
	detailSection := ""
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	routes := model.ListProductionNotificationRoutes(db, p.KitsuProjectID)
	config := model.FindProductionNotificationConfig(db, p.KitsuProjectID)
	participantCount := len(ListKitsuProjectParticipants(p.KitsuProjectID))
	linkedCount := len(model.ListProjectUserMaps(db, p.ID))
	diagnosticItem := func(label, value, class, explanation string) string {
		return `<div class="production-diagnostic-item"><div><strong>` + esc(label) + `</strong><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(value) + `</span></div><small>` + esc(explanation) + `</small></div>`
	}
	kitsuStatus := t(lang, "未設定", "Not configured")
	kitsuClass := "warning"
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) != "" {
		kitsuStatus, kitsuClass = t(lang, "接続済", "Connected"), "success"
	} else if readiness.KitsuConfigured {
		kitsuStatus = t(lang, "要確認", "Needs review")
	}
	discordStatus := t(lang, "未設定", "Not configured")
	discordClass := "warning"
	if readiness.DiscordConfigured {
		discordStatus, discordClass = t(lang, "設定済", "Configured"), "success"
	}
	routingStatus := t(lang, "未設定", "Not configured")
	routingClass := "warning"
	if config != nil && config.Enabled && len(routes) > 0 {
		routingStatus, routingClass = t(lang, "正常", "Healthy"), "success"
	}
	participantStatus := t(lang, "確認待", "Waiting")
	participantClass := "warning"
	if participantCount > 0 {
		participantStatus, participantClass = t(lang, "取得済", "Loaded"), "success"
	}
	linkStatus := t(lang, "記録なし", "No records")
	linkClass := "neutral"
	if linkedCount > 0 {
		linkStatus, linkClass = t(lang, "設定済", "Configured"), "success"
	}
	detailSection = `<details class="advanced-details production-diagnostics"><summary>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</summary><div class="production-diagnostic-grid">` +
		diagnosticItem(t(lang, "Kitsu接続", "Kitsu connection"), kitsuStatus, kitsuClass, t(lang, "ランタイム認証状態を確認します。", "Based on current runtime authentication state.")) +
		diagnosticItem(t(lang, "Discord Bot", "Discord Bot"), discordStatus, discordClass, t(lang, "Bot設定の存在を確認します。", "Based on current Bot configuration.")) +
		diagnosticItem(t(lang, "通知ルーティング", "Notification routing"), routingStatus, routingClass, fmt.Sprintf(t(lang, "%d件のProductionルートと設定を確認しました。", "%d Production routes and the configuration were inspected."), len(routes))) +
		diagnosticItem(t(lang, "参加者取得", "Participant retrieval"), participantStatus, participantClass, fmt.Sprintf(t(lang, "Kitsu Production team APIから%d人を取得しました。", "The Kitsu Production team API returned %d people."), participantCount)) +
		diagnosticItem(t(lang, "ユーザー紐づけ", "User linking"), linkStatus, linkClass, fmt.Sprintf(t(lang, "このProductionに記録されたユーザー割り当て: %d件。", "User assignments recorded for this Production: %d."), linkedCount)) +
		`</div></details>` + detailSection
	if has {
		detailSection = `<details class="advanced-details" open><summary>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</summary>` + details.String() + `</details>`
	}
	return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.troubleshooting")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "現在の問題", "Current problem"), class, value, "", "") + `</dl>` + detailSection + `</section>`
}

func renderSelectedProductionDanger(r *http.Request, p model.Project, lang string) string {
	if p.ValidationOnly || p.ReadOnlyPreview {
		return `<details class="advanced-details danger-zone"><summary>` + esc(tr(lang, "ia.danger")) + `</summary><p class="field-help" role="status">` + esc(t(lang, "検証専用Productionでは変更や削除は実行できません。", "Changes and deletion are disabled for validation-only Productions.")) + `</p></details>`
	}
	disconnectPhrase := t(lang, "連携解除", "DISCONNECT")
	deletePhrase := t(lang, "削除", "DELETE")
	return `<details class="advanced-details danger-zone"><summary>` + esc(tr(lang, "ia.danger")) + `</summary><div class="danger-actions"><div class="danger-action-block"><h3>` + esc(tr(lang, "ia.disconnect_production")) + `</h3><p class="hint">` + esc(t(lang, "KitsuSyncの連携だけを解除します。Discord側のリソースは残ります。", "This removes only the KitsuSync connection. Discord resources remain.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "Productionの連携を解除します。Discord側のリソースは残ります。", "This removes the Production connection. Discord resources remain.")) + `" data-require-text="` + esc(disconnectPhrase) + `"><input type="hidden" name="action" value="remove_connection"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-ghost" type="submit">` + esc(tr(lang, "ia.disconnect_production")) + `</button></form></div><div class="danger-action-block"><h3>` + esc(tr(lang, "ia.delete_discord_resources")) + `</h3><p class="hint">` + esc(t(lang, "Discord側のチャンネルとカテゴリを削除します。連携解除とは別の操作です。", "This may delete Discord channels and the category. It is separate from disconnecting the Production.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "Discord側のリソースを削除します。", "This may delete Discord-side resources.")) + `" data-require-text="` + esc(deletePhrase) + `"><input type="hidden" name="action" value="preview_remove_connection_with_discord"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-danger" type="submit">` + esc(tr(lang, "ia.delete_discord_resources")) + `</button></form></div></div></details>`
}

func renderIABot(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	class, _, hint := iaReadiness(db, lang)
	permissionAction := `<a class="btn" href="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `">` + esc(t(lang, "Bot接続を設定", "Connect or reconnect")) + `</a>`
	body := `<div class="section-stack"><section class="section-card glass"><div class="page-heading"><div><h2>` + esc(tr(lang, "connections.title")) + `</h2><p class="hint">` + esc(hint) + `</p></div></div><dl class="status-list">` +
		statusSummaryRow(t(lang, "Bot状態", "Bot state"), normalizeStatusClass(class), cleanStatusLabel(lang, class), hint, permissionAction) +
		statusSummaryRow(t(lang, "必要な権限", "Required permissions"), "neutral", t(lang, "接続設定時に必要", "Required during connection setup"), t(lang, "チャンネル表示、メッセージ送信、接続設定時のチャンネル管理", "View channels, send messages, and manage channels during setup"), "") +
		statusSummaryRow(t(lang, "接続済みサーバー", "Joined servers"), "neutral", t(lang, "Bot接続後に確認できます", "Available after Bot Connection"), "", "") + `</dl></section></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "connections.title"), r, body))
}

type pipelineHealthItem struct {
	label, value, class, explanation, details, detailsLabel, action, actionLabel string
}

func renderIAHealth(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	readinessView := readinessViewFor(lang, r, readiness)
	stats := Stats.Snapshot()
	windowName := telemetryWindowName(strings.TrimSpace(r.URL.Query().Get("window")))
	items := []pipelineHealthItem{
		{label: t(lang, "イベント監視", "Event monitoring"), value: pipelineProcessingValue(lang, stats), class: pipelineProcessingClass(stats), explanation: pipelineProcessingHint(lang, stats), details: pipelineProcessingDetails(lang, stats), detailsLabel: t(lang, "詳細", "Details")},
		{label: t(lang, "通知処理", "Notification processing"), value: pipelineNotificationValue(lang, readiness), class: map[bool]string{true: "success", false: "blocked"}[readiness.OverallReady], explanation: pipelineNotificationHint(lang, readiness), details: pipelineNotificationDetails(lang, stats), detailsLabel: t(lang, "詳細", "Details")},
		{label: t(lang, "内部データ", "Internal data"), value: t(lang, "利用可能", "Available"), class: "success", explanation: t(lang, "ローカル設定と履歴を読み取れます。", "Local configuration and history can be read."), details: pipelineInternalDetails(lang), detailsLabel: t(lang, "詳細", "Details")},
		{label: t(lang, "接続・ルーティング整合性", "Connection / routing integrity"), value: pipelineRoutingValue(lang, readiness), class: map[bool]string{true: "success", false: "warning"}[readiness.RoutingReady], explanation: pipelineRoutingHint(lang, readiness), details: pipelineRoutingDetails(lang, readiness, db), detailsLabel: t(lang, "詳細", "Details")},
	}
	var healthRows strings.Builder
	for _, item := range items {
		healthRows.WriteString(renderPipelineHealthItem(item))
	}
	body := `<div class="section-stack"><section class="section-card glass pipeline-health" aria-labelledby="pipeline-health-title"><div class="page-heading"><div><h2 id="pipeline-health-title">` + esc(t(lang, "通知パイプラインの状態", "Notification pipeline health")) + `</h2><p class="hint">` + esc(t(lang, "通知に関わる各段階の状態を確認できます。取得できないメトリクスは未確認として表示します。", "Review each notification stage. Metrics that are not available are shown as unconfirmed.")) + `</p></div><span class="status-pill ` + esc(readinessView.Class) + `" role="status">` + esc(readinessView.Label) + `</span></div><div class="pipeline-health-grid">` + healthRows.String() + `</div></section><section class="section-card glass" aria-labelledby="system-issues-title"><div class="page-heading"><div><h2 id="system-issues-title">` + esc(t(lang, "最近のシステム問題", "Recent system issues")) + `</h2><p class="hint">` + esc(t(lang, "直近の失敗と復旧記録を表示します。", "Recent failure and recovery records.")) + `</p></div></div>` + recentSystemIssues(lang, db) + `</section></div>`
	body = renderRuntimeObservabilitySummary(lang, stats, readiness, windowName, body)
	body += `<script data-system-status-refresh>(function(){var interval=5000;var busy=false;var timer;var select=document.querySelector("[data-system-status-window]");var root=document.querySelector(".system-status-sections");var live=document.querySelector("[data-system-live-label]");if(!select||!root){return}function text(ja,en){return document.documentElement.lang==="ja"?ja:en}function setLive(value){if(live){live.textContent=value}}function graph(items){if(!items.length){return ""}var width=320,height=104,left=42,right=8,top=8,bottom=82,max=1;items.forEach(function(item){max=Math.max(max,Number(item.duration_ms)||0)});var slot=(width-left-right)/items.length,bar=Math.max(3,slot*.64),bars="";items.forEach(function(item,i){var value=Number(item.duration_ms)||0,h=Math.max(2,(bottom-top)*value/max),x=left+slot*i+(slot-bar)/2,cls=item.success?"bar-success":"bar-failure";bars+="<rect class=\""+cls+"\" x=\""+x.toFixed(1)+"\" y=\""+(bottom-h).toFixed(1)+"\" width=\""+bar.toFixed(1)+"\" height=\""+h.toFixed(1)+"\" rx=\"2\"><title>"+value+" ms</title></rect>"});var label=select.value==="5m"?text("直近5分","Last 5 minutes"):text("直近60秒","Last 60 seconds");return "<svg class=\"api-sparkline api-bar-chart\" viewBox=\"0 0 320 104\" role=\"img\" aria-label=\""+items.length+" observations, "+label+"\"><line class=\"chart-axis\" x1=\"42\" y1=\"82\" x2=\"312\" y2=\"82\"></line><line class=\"chart-axis\" x1=\"42\" y1=\"8\" x2=\"42\" y2=\"82\"></line><text class=\"chart-axis-label\" x=\"2\" y=\"12\">"+text("応答時間 (ms)","Response time (ms)")+"</text><text class=\"chart-tick\" x=\"3\" y=\"84\">0</text><text class=\"chart-tick\" x=\"3\" y=\"12\">"+Math.round(max)+"</text>"+bars+"<text class=\"chart-time-label\" x=\"42\" y=\"100\">"+label+"</text><text class=\"chart-time-label\" x=\"270\" y=\"100\">"+text("今","Now")+"</text></svg>"}function updateCard(service,items){var card=root.querySelector("[data-telemetry-card=\""+service+"\"]");if(!card){return}var status=card.querySelector("[data-telemetry-status]");var details=card.querySelector("[data-telemetry-details]");if(!status||!details){return}if(!items.length){status.className="status-pill neutral";status.textContent=text("未確認","Not checked");details.innerHTML="<div class=\"api-observation-not-checked\">"+text("未確認","Not checked")+"</div>";return}var last=items[items.length-1],value=Number(last.duration_ms)||0,windowLabel=select.value==="5m"?text("直近5分","Last 5 minutes"):text("直近60秒","Last 60 seconds");status.className="status-pill "+(last.success?"ok":"bad");status.textContent=last.success?text("正常","Healthy"):text("要確認","Needs review");details.innerHTML="<div class=\"api-observation-latency\"><strong data-telemetry-value>"+value+" ms</strong><span class=\"api-observation-label\">"+text("現在の応答時間","Current response time")+"</span><span class=\"api-observation-meta\" data-telemetry-meta>"+items.length+" / 20 "+text("観測","observations")+" · "+windowLabel+" · "+text("最終更新","Last updated")+" "+new Date(last.at).toLocaleTimeString()+"</span></div>"+graph(items)}function refresh(){if(busy){return}busy=true;fetch("/bot/api/setup/observability?window="+encodeURIComponent(select.value),{headers:{"X-Requested-With":"system-status-refresh"},cache:"no-store"}).then(function(response){if(!response.ok){throw new Error("snapshot failed")}return response.json()}).then(function(payload){updateCard("kitsu",payload.observations.kitsu||[]);updateCard("discord",payload.observations.discord||[]);setLive(text("自動更新","Auto-refresh"))}).catch(function(){setLive(text("更新失敗","Refresh unavailable"))}).finally(function(){busy=false})}select.addEventListener("change",refresh);timer=window.setInterval(refresh,interval);window.addEventListener("beforeunload",function(){window.clearInterval(timer)});refresh()})();</script>`
	body = replaceSystemStatusRefreshScript(body)
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.system_status"), r, body))
}

func renderRuntimeObservabilitySummary(lang string, stats RuntimeSnapshot, readiness SharedBotRuntimeReadiness, windowName, body string) string {
	statusLabel, statusClass, statusHint := overallRuntimeStatus(lang, readiness, stats)
	overall := `<section class="section-card glass system-overall-summary" aria-labelledby="system-overall-title"><div class="page-heading"><div><h2 id="system-overall-title">` + esc(t(lang, "システム全体", "Overall system health")) + `</h2><p class="hint">` + esc(statusHint) + `</p></div><span class="status-pill ` + esc(statusClass) + `" role="status">` + esc(statusLabel) + `</span></div></section>`
	body = renderRuntimeObservabilitySummaryRaw(lang, stats, windowName, body)
	body = addTelemetryViewerLocalTimes(body, stats, windowName)
	body = replaceElementTextByID(body, "system-observability-title", t(lang, "API応答状態", "API response status"))
	body = replaceElementTextByID(body, "pipeline-health-title", t(lang, "KitsuSync処理状態", "KitsuSync operational status"))
	return `<div class="section-stack system-status-sections">` + overall + body + `</div>`
}

func replaceElementTextByID(body, id, text string) string {
	marker := `id="` + id + `"`
	start := strings.Index(body, marker)
	if start < 0 {
		return body
	}
	relOpen := strings.Index(body[start:], ">")
	if relOpen < 0 {
		return body
	}
	start += relOpen + 1
	end := strings.Index(body[start:], "<")
	if start < 0 || end < 0 {
		return body
	}
	end += start
	return body[:start] + esc(text) + body[end:]
}

func addTelemetryViewerLocalTimes(body string, stats RuntimeSnapshot, windowName string) string {
	for _, service := range []string{"kitsu", "discord"} {
		items := filterAPIObservations(stats.APIObservations[service], time.Now(), telemetryWindowDuration(windowName))
		if len(items) == 0 {
			continue
		}
		marker := `<span class="api-observation-meta" data-telemetry-meta>`
		replacement := `<span class="api-observation-meta" data-telemetry-meta data-telemetry-at="` + esc(items[len(items)-1].At.UTC().Format(time.RFC3339)) + `">`
		body = strings.Replace(body, marker, replacement, 1)
	}
	return body + `<script data-telemetry-local-time>(function(){document.querySelectorAll("[data-telemetry-at]").forEach(function(node){var date=new Date(node.getAttribute("data-telemetry-at"));if(Number.isNaN(date.getTime())){return}var zone=Intl.DateTimeFormat().resolvedOptions().timeZone||"UTC";node.textContent=(document.documentElement.lang==="ja"?"最終更新":"Last updated")+" "+new Intl.DateTimeFormat(undefined,{hour:"2-digit",minute:"2-digit",second:"2-digit"}).format(date);node.title="UTC "+node.getAttribute("data-telemetry-at")+" · "+zone})})();</script>`
}

func renderRuntimeObservabilitySummaryRaw(lang string, stats RuntimeSnapshot, windowName, body string) string {
	sharedMax := sharedAPIObservationScale(stats, windowName)
	windowLabel := t(lang, "直近60秒", "Last 60 seconds")
	if windowName == telemetryWindow5Minutes {
		_ = windowLabel
		windowLabel = t(lang, "直近5分", "Last 5 minutes")
	}
	windowControl := `<label class="telemetry-window-control"><span>` + esc(t(lang, "表示範囲", "Time window")) + `</span><select id="system-status-window" data-system-status-window><option value="60s"` + selectedAttr(windowName == telemetryWindow60Seconds) + `>` + esc(t(lang, "直近60秒", "Last 60 seconds")) + `</option><option value="5m"` + selectedAttr(windowName == telemetryWindow5Minutes) + `>` + esc(t(lang, "直近5分", "Last 5 minutes")) + `</option></select></label>`
	return `<section class="section-card glass system-observability" aria-labelledby="system-observability-title"><div class="page-heading"><div><h2 id="system-observability-title">` + esc(t(lang, "外部APIの健全性", "External API health")) + `</h2><p class="hint">` + esc(t(lang, "実測できた応答時間を時間順に表示します。", "Shows real response times in chronological order.")) + `</p></div><div class="telemetry-window-actions">` + windowControl + `<span class="system-live-indicator" role="status"><i aria-hidden="true"></i><span data-system-live-label>` + esc(t(lang, "自動更新", "Auto-refresh")) + `</span></span></div></div><div class="system-observability-grid"><article class="api-observation-card" data-telemetry-card="kitsu"><div class="api-observation-summary"><h3>Kitsu API</h3><span class="status-pill ` + apiObservationStatusClass(stats, "kitsu") + `" role="status" data-telemetry-status>` + esc(apiObservationStatus(lang, stats, "kitsu")) + `</span></div><div class="api-observation-details" data-telemetry-details="kitsu">` + apiObservationDetails(lang, stats, "kitsu", windowName, sharedMax) + `</div></article><article class="api-observation-card" data-telemetry-card="discord"><div class="api-observation-summary"><h3>Discord API</h3><span class="status-pill ` + apiObservationStatusClass(stats, "discord") + `" role="status" data-telemetry-status>` + esc(apiObservationStatus(lang, stats, "discord")) + `</span></div><div class="api-observation-details" data-telemetry-details="discord">` + apiObservationDetails(lang, stats, "discord", windowName, sharedMax) + `</div></article></div></section>` + body
}

func renderPipelineHealthItem(item pipelineHealthItem) string {
	action := `<span class="pipeline-health-action" aria-hidden="true"></span>`
	if item.action != "" && item.actionLabel != "" {
		action = `<a class="btn-ghost pipeline-health-action" href="` + esc(item.action) + `">` + esc(item.actionLabel) + `</a>`
	}
	details := ""
	if item.details != "" {
		details = `<details class="pipeline-health-details"><summary>` + esc(item.detailsLabel) + `</summary><div>` + item.details + `</div></details>`
	}
	return `<article class="pipeline-health-item"><div class="pipeline-health-heading"><h3>` + esc(item.label) + `</h3><span class="status-badge status-badge-` + esc(normalizeStatusClass(item.class)) + `" role="status">` + esc(item.value) + `</span></div><p class="field-help">` + esc(item.explanation) + `</p>` + details + action + `</article>`
}

func apiObservationDetails(lang string, stats RuntimeSnapshot, service, windowName string, sharedMax ...float64) string {
	items := filterAPIObservations(stats.APIObservations[service], time.Now(), telemetryWindowDuration(windowName))
	if len(items) == 0 {
		return `<div class="api-observation-not-checked">` + esc(t(lang, "未確認", "Not checked")) + `</div>`
	}
	last := items[len(items)-1]
	windowLabel := t(lang, "直近60秒", "Last 60 seconds")
	if windowName == telemetryWindow5Minutes {
		windowLabel = t(lang, "直近5分", "Last 5 minutes")
	}
	value := strconv.FormatInt(last.Duration.Milliseconds(), 10) + " ms"
	normalMeta := fmt.Sprintf("%s %s", t(lang, "最終更新", "Last updated"), last.At.Local().Format("15:04:05"))
	meta := fmt.Sprintf("%d / %d %s · %s · %s %s", len(items), maxAPIObservations, t(lang, "観測", "observations"), windowLabel, t(lang, "最終更新", "Last updated"), last.At.Format("15:04:05"))
	meta = normalMeta
	maxValue := stableObservationScale(service, windowName, items)
	return `<div class="api-observation-latency"><strong data-telemetry-value>` + esc(value) + `</strong><span class="api-observation-label">` + esc(t(lang, "現在の応答時間", "Current response time")) + `</span><span class="api-observation-meta" data-telemetry-meta>` + esc(meta) + `</span></div>` + apiObservationBarGraphWithScale(items, lang, windowName, maxValue)
}

func apiObservationGraph(items []APIObservation) string {
	return apiObservationBarGraph(items, "en", telemetryWindow60Seconds)
}

func apiObservationBarGraph(items []APIObservation, lang, windowName string) string {
	return apiObservationBarGraphWithScale(items, lang, windowName, observationScaleForItems(items))
}

func apiObservationBarGraphWithScale(items []APIObservation, lang, windowName string, maxValue float64) string {
	return rewriteLatencyGraph(apiObservationBarGraphWithScaleRaw(items, lang, windowName, maxValue))
}

func apiObservationBarGraphWithScaleRaw(items []APIObservation, lang, windowName string, maxValue float64) string {
	if len(items) == 0 {
		return ""
	}
	geometry := telemetryChartGeometry()
	width, height := geometry.Width, geometry.Height
	plotLeft, plotRight, plotTop, plotBottom := geometry.PlotLeft, geometry.PlotRight, geometry.PlotTop, geometry.PlotBottom
	window := telemetryWindowDuration(windowName)
	now := time.Now()
	positions := make([]float64, len(items))
	for i, item := range items {
		age := now.Sub(item.At)
		ratio := 1 - age.Seconds()/window.Seconds()
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		positions[i] = plotLeft + ratio*(width-plotLeft-plotRight)
	}
	var bars strings.Builder
	for i, item := range items {
		barWidth := 8.0
		if i > 0 && positions[i]-positions[i-1]-2 < barWidth {
			barWidth = positions[i] - positions[i-1] - 2
		}
		if i+1 < len(items) && positions[i+1]-positions[i]-2 < barWidth {
			barWidth = positions[i+1] - positions[i] - 2
		}
		if barWidth < 2 {
			barWidth = 2
		}
		value := float64(item.Duration.Milliseconds())
		barHeight := (plotBottom - plotTop) * value / maxValue
		if barHeight < 2 {
			barHeight = 2
		}
		class := "bar-success"
		if !item.Success {
			class = "bar-failure"
		}
		x := positions[i] - barWidth/2
		if x < plotLeft {
			x = plotLeft
		}
		if x+barWidth > width-plotRight {
			x = width - plotRight - barWidth
		}
		statusLabel := t(lang, "正常", "Healthy")
		if !item.Success {
			statusLabel = t(lang, "リクエスト失敗", "Request failed")
		}
		durationLabel := ""
		if item.Success {
			durationLabel = fmt.Sprintf("%d ms", item.Duration.Milliseconds())
		}
		tooltip := strings.TrimSpace(fmt.Sprintf("%s %s %s", item.At.Local().Format("15:04:05"), durationLabel, statusLabel))
		bars.WriteString(fmt.Sprintf(`<rect class="%s" tabindex="0" role="img" aria-label="%s" x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="2"><title>%s</title></rect>`, class, esc(tooltip), x, plotBottom-barHeight, barWidth, barHeight, esc(tooltip)))
	}
	windowLabel := t(lang, "60秒", "60s")
	middleLabel := t(lang, "30秒", "30s")
	if windowName == telemetryWindow5Minutes {
		windowLabel = t(lang, "5分", "5m")
		middleLabel = t(lang, "2分30秒", "2m30s")
	}
	middleValue := maxValue / 2
	plotMid := plotLeft + (width-plotLeft-plotRight)/2
	return `<svg class="api-sparkline api-bar-chart" viewBox="0 0 ` + strconv.FormatFloat(width, 'f', 0, 64) + ` ` + strconv.FormatFloat(height, 'f', 0, 64) + `" role="img" aria-label="` + esc(fmt.Sprintf("%d observations, %s", len(items), windowLabel)) + `"><line class="chart-axis" x1="` + strconv.FormatFloat(plotLeft, 'f', 0, 64) + `" y1="82" x2="` + strconv.FormatFloat(width-plotRight, 'f', 0, 64) + `" y2="82"></line><line class="chart-axis" x1="` + strconv.FormatFloat(plotLeft, 'f', 0, 64) + `" y1="8" x2="` + strconv.FormatFloat(plotLeft, 'f', 0, 64) + `" y2="82"></line><line class="chart-guide" x1="` + strconv.FormatFloat(plotLeft, 'f', 0, 64) + `" y1="45" x2="` + strconv.FormatFloat(width-plotRight, 'f', 0, 64) + `" y2="45"></line><text class="chart-axis-label" x="2" y="12">` + esc(t(lang, "応答時間 (ms)", "Response time (ms)")) + `</text><text class="chart-tick" x="3" y="12">` + esc(strconv.FormatFloat(maxValue, 'f', 0, 64)) + `</text><text class="chart-tick" x="3" y="48">` + esc(strconv.FormatFloat(middleValue, 'f', 0, 64)) + `</text><text class="chart-tick" x="3" y="84">0</text>` + bars.String() + `<text class="chart-time-label" text-anchor="start" x="` + strconv.FormatFloat(plotLeft, 'f', 0, 64) + `" y="100">` + esc(windowLabel) + `</text><text class="chart-time-label" text-anchor="middle" x="` + strconv.FormatFloat(plotMid, 'f', 0, 64) + `" y="100">` + esc(middleLabel) + `</text><text class="chart-time-label" text-anchor="end" x="` + strconv.FormatFloat(width-plotRight, 'f', 0, 64) + `" y="100">` + esc(t(lang, "今", "Now")) + `</text></svg>`
}

func rewriteLatencyGraph(graph string) string {
	if graph == "" {
		return graph
	}
	graph = strings.ReplaceAll(graph, `x="3" y="12"`, `x="0" y="12"`)
	graph = strings.ReplaceAll(graph, `x="3" y="48"`, `x="0" y="48"`)
	graph = strings.ReplaceAll(graph, `x="3" y="84"`, `x="0" y="84"`)
	return alignLatencyTickLabels(addLatencyTickUnits(removeChartAxisTitle(graph)))
}

func alignLatencyTickLabels(graph string) string {
	graph = strings.ReplaceAll(graph, `<text class="chart-tick" x="0"`, `<text class="chart-tick" text-anchor="end" x="48"`)
	graph = strings.ReplaceAll(graph, `<text class='chart-tick' x='0'`, `<text class='chart-tick' text-anchor='end' x='48'`)
	return graph
}

func removeChartAxisTitle(graph string) string {
	for {
		start := strings.Index(graph, `<text class="chart-axis-label"`)
		quote := `"`
		if start < 0 {
			start = strings.Index(graph, `<text class='chart-axis-label'`)
			quote = `'`
		}
		_ = quote
		if start < 0 {
			return graph
		}
		relEnd := strings.Index(graph[start:], `</text>`)
		if relEnd < 0 {
			return graph
		}
		end := start + relEnd + len(`</text>`)
		graph = graph[:start] + graph[end:]
	}
}

func addLatencyTickUnits(graph string) string {
	var result strings.Builder
	for len(graph) > 0 {
		start := strings.Index(graph, `<text class="chart-tick"`)
		if start < 0 {
			start = strings.Index(graph, `<text class='chart-tick'`)
		}
		if start < 0 {
			result.WriteString(graph)
			break
		}
		result.WriteString(graph[:start])
		openEnd := strings.Index(graph[start:], ">")
		if openEnd < 0 {
			result.WriteString(graph[start:])
			break
		}
		contentStart := start + openEnd + 1
		relEnd := strings.Index(graph[contentStart:], `</text>`)
		if relEnd < 0 {
			result.WriteString(graph[start:])
			break
		}
		contentEnd := contentStart + relEnd
		result.WriteString(graph[start:contentStart])
		content := graph[contentStart:contentEnd]
		result.WriteString(content)
		if !strings.HasSuffix(content, "ms") {
			result.WriteString("ms")
		}
		result.WriteString(`</text>`)
		graph = graph[contentEnd+len(`</text>`):]
	}
	return result.String()
}

type telemetryChartLayout struct {
	Width, Height, PlotLeft, PlotRight, PlotTop, PlotMiddle, PlotBottom float64
}

func telemetryChartGeometry() telemetryChartLayout {
	return telemetryChartLayout{Width: 466, Height: 104, PlotLeft: 54, PlotRight: 2, PlotTop: 8, PlotMiddle: 45, PlotBottom: 82}
}

func observationScaleForItems(items []APIObservation) float64 {
	maxValue := 1.0
	for _, item := range items {
		if value := float64(item.Duration.Milliseconds()); value > maxValue {
			maxValue = value
		}
	}
	return roundedObservationScale(maxValue)
}

func sharedAPIObservationScale(stats RuntimeSnapshot, windowName string) float64 {
	maxValue := 1.0
	now := time.Now()
	window := telemetryWindowDuration(windowName)
	for _, service := range []string{"kitsu", "discord"} {
		for _, item := range filterAPIObservations(stats.APIObservations[service], now, window) {
			if value := float64(item.Duration.Milliseconds()); value > maxValue {
				maxValue = value
			}
		}
	}
	return roundedObservationScale(maxValue)
}

func roundedObservationScale(maxValue float64) float64 {
	for _, ceiling := range []float64{10, 25, 50, 100, 250, 500, 1000, 2000} {
		if maxValue <= ceiling {
			return ceiling
		}
	}
	return float64((int(maxValue) + 99) / 100 * 100)
}

type observationScaleState struct {
	ceiling   float64
	downSince time.Time
}

var observationScaleStates = struct {
	sync.Mutex
	values map[string]observationScaleState
}{values: make(map[string]observationScaleState)}

func stableObservationScale(service, windowName string, items []APIObservation) float64 {
	key := service + ":" + windowName
	required := observationScaleForItems(items)
	now := time.Now()
	observationScaleStates.Lock()
	defer observationScaleStates.Unlock()
	state := observationScaleStates.values[key]
	if state.ceiling == 0 || required > state.ceiling {
		state.ceiling = required
		state.downSince = time.Time{}
	} else if required < state.ceiling {
		if state.downSince.IsZero() {
			state.downSince = now
		} else if now.Sub(state.downSince) >= 15*time.Second {
			state.ceiling = required
			state.downSince = time.Time{}
		}
	}
	observationScaleStates.values[key] = state
	return state.ceiling
}

func apiObservationBarGraphIndexed(items []APIObservation, lang, windowName string) string {
	if len(items) == 0 {
		return ""
	}
	width, height := 320.0, 104.0
	_ = height
	plotLeft, plotRight, plotTop, plotBottom := 42.0, 8.0, 8.0, 82.0
	maxValue := 1.0
	for _, item := range items {
		if value := float64(item.Duration.Milliseconds()); value > maxValue {
			maxValue = value
		}
	}
	barSlot := (width - plotLeft - plotRight) / float64(len(items))
	barWidth := barSlot * 0.64
	if barWidth < 3 {
		barWidth = 3
	}
	var bars strings.Builder
	for i, item := range items {
		value := float64(item.Duration.Milliseconds())
		x := plotLeft + barSlot*float64(i) + (barSlot-barWidth)/2
		barHeight := (plotBottom - plotTop) * value / maxValue
		if barHeight < 2 {
			barHeight = 2
		}
		class := "bar-success"
		if !item.Success {
			class = "bar-failure"
		}
		bars.WriteString(fmt.Sprintf(`<rect class="%s" x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="2"><title>%d ms</title></rect>`, class, x, plotBottom-barHeight, barWidth, barHeight, item.Duration.Milliseconds()))
	}
	windowLabel := t(lang, "直近60秒", "Last 60 seconds")
	if windowName == telemetryWindow5Minutes {
		windowLabel = t(lang, "直近5分", "Last 5 minutes")
	}
	return `<svg class="api-sparkline api-bar-chart" viewBox="0 0 320 104" role="img" aria-label="` + esc(fmt.Sprintf("%d observations, %s", len(items), windowLabel)) + `"><line class="chart-axis" x1="42" y1="82" x2="312" y2="82"></line><line class="chart-axis" x1="42" y1="8" x2="42" y2="82"></line><text class="chart-axis-label" x="2" y="12">` + esc(t(lang, "応答時間 (ms)", "Response time (ms)")) + `</text><text class="chart-tick" x="3" y="84">0</text><text class="chart-tick" x="3" y="12">` + esc(strconv.FormatInt(int64(maxValue), 10)) + `</text>` + bars.String() + `<text class="chart-time-label" x="42" y="100">` + esc(windowLabel) + `</text><text class="chart-time-label" x="270" y="100">` + esc(t(lang, "今", "Now")) + `</text></svg>`
}

func apiObservationStatusClass(stats RuntimeSnapshot, service string) string {
	items := stats.APIObservations[service]
	if len(items) == 0 {
		return "neutral"
	}
	if items[len(items)-1].Success {
		return "ok"
	}
	return "bad"
}

func apiObservationStatus(lang string, stats RuntimeSnapshot, service string) string {
	items := stats.APIObservations[service]
	if len(items) == 0 {
		return t(lang, "未確認", "Not checked")
	}
	if items[len(items)-1].Success {
		return t(lang, "正常", "Healthy")
	}
	return t(lang, "要確認", "Needs review")
}

func latestAPIObservationFailed(stats RuntimeSnapshot, service string) bool {
	items := stats.APIObservations[service]
	return len(items) > 0 && !items[len(items)-1].Success
}

func overallRuntimeStatus(lang string, readiness SharedBotRuntimeReadiness, stats RuntimeSnapshot) (string, string, string) {
	if !readiness.KitsuConfigured || !readiness.DiscordConfigured {
		return t(lang, "未設定", "Not configured"), "warning", t(lang, "接続設定を確認してください。", "Review the connection settings.")
	}
	if stats.LastPollErr != "" || latestAPIObservationFailed(stats, "kitsu") || latestAPIObservationFailed(stats, "discord") {
		return t(lang, "要確認", "Needs review"), "warning", t(lang, "直近の実測で問題が確認されています。", "A recent observation recorded an issue.")
	}
	if stats.LastPollTime.IsZero() && len(stats.APIObservations) == 0 {
		return t(lang, "未確認", "Not checked"), "neutral", t(lang, "まだ実測値がありません。", "No runtime observations are available yet.")
	}
	if !readiness.KitsuConfigured || !readiness.DiscordConfigured {
		return t(lang, "未設定", "Not configured"), "warning", t(lang, "接続設定を確認してください。", "Review the connection settings.")
	}
	return t(lang, "正常", "Healthy"), "success", t(lang, "直近の実測値に問題はありません。", "Recent runtime observations are healthy.")
}

func pipelineProcessingValue(lang string, stats RuntimeSnapshot) string {
	if !stats.LastPollTime.IsZero() && stats.LastPollErr == "" {
		return t(lang, "稼働中", "Running")
	}
	return t(lang, "未確認", "Unconfirmed")
}

func pipelineProcessingClass(stats RuntimeSnapshot) string {
	if !stats.LastPollTime.IsZero() && stats.LastPollErr == "" {
		return "success"
	}
	return "warning"
}

func pipelineProcessingHint(lang string, stats RuntimeSnapshot) string {
	if stats.LastPollErr != "" {
		return t(lang, "直近の処理で問題が記録されています。", "The most recent processing cycle recorded an issue.")
	}
	if stats.LastPollTime.IsZero() {
		return t(lang, "処理メトリクスはまだ確認できません。", "Processing metrics are not available yet.")
	}
	return t(lang, "直近の処理が正常に記録されています。", "A recent processing cycle was recorded successfully.")
}

func pipelineProcessingDetails(lang string, stats RuntimeSnapshot) string {
	if stats.LastPollTime.IsZero() {
		return `<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "最終観測", "Last observation")) + `</dt><dd>` + esc(t(lang, "未確認", "Not checked")) + `</dd></div></dl>`
	}
	return `<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "最終観測", "Last observation")) + `</dt><dd>` + esc(stats.LastPollTime.Format("2006-01-02 15:04:05")) + `</dd></div><div><dt>` + esc(t(lang, "処理時間", "Processing duration")) + `</dt><dd>` + esc(stats.LastPollDuration.Round(time.Millisecond).String()) + `</dd></div></dl>`
}

func pipelineRoutingValue(lang string, readiness SharedBotRuntimeReadiness) string {
	if !readiness.ProductionConnected && !readiness.RoutingReady {
		return t(lang, "接続待ち", "Waiting")
	}
	if readiness.RoutingReady {
		return t(lang, "設定済み", "Configured")
	}
	return t(lang, "要確認", "Needs review")
}

func pipelineRoutingHint(lang string, readiness SharedBotRuntimeReadiness) string {
	if readiness.RoutingReady {
		return t(lang, "有効な通知先設定を確認済みです。", "A valid enabled route is available.")
	}
	return t(lang, "有効な通知先設定がありません。", "No valid enabled route is available.")
}

func pipelineDiscordValue(lang string, configured bool) string {
	if configured {
		return t(lang, "未検証", "Unverified")
	}
	return t(lang, "未設定", "Not configured")
}

func pipelineDiscordHint(lang string, configured bool) string {
	if configured {
		return t(lang, "Discord APIの検証結果はまだ記録されていません。", "Discord API validation has not been recorded.")
	}
	return t(lang, "Discord Botを設定してください。", "Configure the Discord Bot.")
}

func pipelineNotificationValue(lang string, readiness SharedBotRuntimeReadiness) string {
	if readiness.OverallReady {
		return t(lang, "利用可能", "Available")
	}
	return t(lang, "利用不可", "Unavailable")
}

func pipelineNotificationHint(lang string, readiness SharedBotRuntimeReadiness) string {
	if readiness.OverallReady {
		return t(lang, "通知処理を利用できます。", "Notification processing is available.")
	}
	return t(lang, "設定が完了するまで通知は停止しています。", "Notifications remain blocked until setup is complete.")
}

func pipelineNotificationDetails(lang string, stats RuntimeSnapshot) string {
	if stats.SendSuccessTotal == 0 && stats.SendFailureTotal == 0 {
		return `<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "成功", "Successful")) + `</dt><dd>0</dd></div><div><dt>` + esc(t(lang, "失敗", "Failed")) + `</dt><dd>0</dd></div></dl>`
	}
	return `<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "成功", "Successful")) + `</dt><dd>` + strconv.FormatInt(stats.SendSuccessTotal, 10) + `</dd></div><div><dt>` + esc(t(lang, "失敗", "Failed")) + `</dt><dd>` + strconv.FormatInt(stats.SendFailureTotal, 10) + `</dd></div></dl>`
}

func pipelineInternalDetails(lang string) string {
	return `<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "データソース", "Data source")) + `</dt><dd>` + esc(t(lang, "ローカル設定・履歴", "Local settings and history")) + `</dd></div></dl>`
}

func pipelineRoutingDetails(lang string, readiness SharedBotRuntimeReadiness, db *gorm.DB) string {
	connection := t(lang, "未接続", "Disconnected")
	if readiness.ProductionConnected {
		connection = t(lang, "接続済", "Connected")
	}
	routing := t(lang, "未設定", "Not configured")
	if readiness.RoutingReady {
		routing = t(lang, "設定済み", "Configured")
	}
	projects := model.ListProjects(db)
	connected := connectedProductionCount(projects)
	routeCount := 0
	configCount := 0
	for _, project := range projects {
		routeCount += len(model.ListProductionNotificationRoutes(db, project.KitsuProjectID))
		if model.FindProductionNotificationConfig(db, project.KitsuProjectID) != nil {
			configCount++
		}
	}
	return `<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "Production接続", "Production connection")) + `</dt><dd>` + esc(connection) + `</dd></div><div><dt>` + esc(t(lang, "接続済みProduction", "Connected Productions")) + `</dt><dd>` + strconv.Itoa(connected) + `</dd></div><div><dt>` + esc(t(lang, "設定済みルート", "Configured routes")) + `</dt><dd>` + strconv.Itoa(routeCount) + `</dd></div><div><dt>` + esc(t(lang, "通知設定", "Notification configurations")) + `</dt><dd>` + strconv.Itoa(configCount) + `</dd></div><div><dt>` + esc(t(lang, "ルート設定", "Route configuration")) + `</dt><dd>` + esc(routing) + `</dd></div></dl>`
}

func recentSystemIssues(lang string, db *gorm.DB) string {
	logs := model.ListAuditLogs(db, 20)
	var rows strings.Builder
	count := 0
	for _, log := range logs {
		if log.Success {
			continue
		}
		count++
		rows.WriteString(`<li><strong>` + esc(t(lang, "失敗", "Failure")) + `</strong><span>` + esc(log.ErrorMessage) + `</span></li>`)
		if count >= 5 {
			break
		}
	}
	if count == 0 {
		return `<p class="empty" role="status">` + esc(t(lang, "最近の問題はありません。", "No recent issues.")) + `</p>`
	}
	return `<ul class="list-tight pipeline-issues" role="log">` + rows.String() + `</ul>`
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
		return t(lang, "接続済", "Connected")
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
	for _, log := range logs {
		oldTime := `<td>` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</td>`
		newTime := `<td><time class="audit-time" datetime="` + esc(log.CreatedAt.UTC().Format(time.RFC3339)) + `">` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</time></td>`
		body = strings.Replace(body, oldTime, newTime, 1)
	}
	body += `<script data-audit-local-time>(function(){var zone=Intl.DateTimeFormat().resolvedOptions().timeZone||"UTC";document.querySelectorAll(".audit-time").forEach(function(node){var date=new Date(node.getAttribute("datetime"));if(Number.isNaN(date.getTime())){return}node.textContent=new Intl.DateTimeFormat(undefined,{year:"numeric",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",second:"2-digit"}).format(date)+" "+zone;node.title="UTC "+node.getAttribute("datetime")})})();</script>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.audit_log"), r, body))
}

func renderIAUsers(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	var rows strings.Builder
	for _, u := range model.ListUserMap(db) {
		identity := t(lang, "Discordユーザー（紐づけ済み）", "Discord user linked")
		if strings.TrimSpace(u.DiscordID) == "" {
			identity = t(lang, "未設定", "Not set")
		}
		edit := withLang("/bot/admin/users?legacy=1&edit="+fmt.Sprint(u.ID), r)
		rows.WriteString(`<tr><td>` + esc(u.KitsuName) + `</td><td>` + esc(identity) + `</td><td><a class="btn-ghost" href="` + esc(edit) + `">` + esc(t(lang, "変更", "Change")) + `</a></td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="3" class="muted">` + esc(t(lang, "ユーザー紐づけはありません。", "No user links yet.")) + `</td></tr>`)
	}
	body := `<section class="section-card glass"><p class="hint">` + esc(t(lang, "KitsuユーザーとDiscordユーザーを紐づけます。Reviewer / CheckerはProductionのユーザー設定で管理します。", "Link Kitsu users to Discord users. Reviewer / Checker belongs in the selected Production's user settings.")) + `</p><table><thead><tr><th>Kitsu` + esc(t(lang, "ユーザー", " user")) + `</th><th>Discord` + esc(t(lang, "ユーザー", " user")) + `</th><th>` + esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></section>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.user_mapping"), r, body))
}

type globalDiscordUserOption struct {
	ID   string
	Name string
}

type globalDiscordDirectory struct {
	Guilds        []DiscordGuild
	SelectedGuild DiscordGuild
	Options       []globalDiscordUserOption
}

func loadGlobalDiscordDirectory(botToken, selectedGuildID string) (globalDiscordDirectory, error) {
	directory := globalDiscordDirectory{}
	if strings.TrimSpace(botToken) == "" {
		return directory, &discordMemberListFailure{Kind: discordMemberFailureUnavailable, Technical: "Discord Bot token is not configured"}
	}
	joined, err := ListBotGuilds(botToken)
	if err != nil {
		return directory, err
	}
	for _, guild := range joined {
		if strings.TrimSpace(guild.ID) != "" && strings.TrimSpace(guild.Name) != "" {
			directory.Guilds = append(directory.Guilds, guild)
		}
	}
	sort.Slice(directory.Guilds, func(i, j int) bool {
		return strings.ToLower(directory.Guilds[i].Name) < strings.ToLower(directory.Guilds[j].Name)
	})
	selectedGuildID = strings.TrimSpace(selectedGuildID)
	if selectedGuildID == "" && len(directory.Guilds) == 1 {
		selectedGuildID = strings.TrimSpace(directory.Guilds[0].ID)
	}
	if selectedGuildID == "" {
		return directory, nil
	}
	for _, guild := range directory.Guilds {
		if strings.TrimSpace(guild.ID) == selectedGuildID {
			directory.SelectedGuild = guild
			break
		}
	}
	if directory.SelectedGuild.ID == "" {
		return directory, &discordMemberListFailure{Kind: discordMemberFailureMismatch, Technical: "The selected Discord server is not joined by the Bot"}
	}
	members, err := ListGuildMembers(directory.SelectedGuild.ID, botToken)
	if err != nil {
		return directory, err
	}
	seen := map[string]bool{}
	for _, member := range members {
		name := strings.TrimSpace(member.Nick)
		if name == "" {
			name = strings.TrimSpace(member.User.DisplayName)
		}
		if name == "" {
			name = strings.TrimSpace(member.User.GlobalName)
		}
		if name == "" {
			name = strings.TrimSpace(member.User.Username)
		}
		id := strings.TrimSpace(member.User.ID)
		if id == "" || name == "" || seen[id] {
			continue
		}
		seen[id] = true
		directory.Options = append(directory.Options, globalDiscordUserOption{ID: id, Name: name})
	}
	sort.Slice(directory.Options, func(i, j int) bool {
		return strings.ToLower(directory.Options[i].Name) < strings.ToLower(directory.Options[j].Name)
	})
	return directory, nil
}

// globalDiscordUserOptions is kept for the legacy edit route. The normal
// User Linking surface uses loadGlobalDiscordDirectory directly and never
// derives its server context from a Production.
func globalDiscordUserOptions(db *gorm.DB, botToken string) ([]globalDiscordUserOption, error) {
	for _, project := range model.ListProjects(db) {
		if isSyntheticDiscordID(strings.TrimSpace(project.DiscordGuildID)) {
			return nil, &discordMemberListFailure{Kind: discordMemberFailureFixture, Technical: "The connected server is synthetic fixture data and was not sent to Discord"}
		}
	}
	directory, err := loadGlobalDiscordDirectory(botToken, "")
	return directory.Options, err
}

func globalDiscordMemberLoadMessage(lang string, loadErr error) string {
	kind := discordMemberFailureUnavailable
	detail := "Discord member lookup was unavailable"
	var failure *discordMemberListFailure
	if errors.As(loadErr, &failure) {
		kind = failure.Kind
		detail = failure.Technical
		if failure.Status > 0 {
			detail += fmt.Sprintf(" (HTTP %d)", failure.Status)
		}
		if failure.Code > 0 {
			detail += fmt.Sprintf(" (Discord code %d)", failure.Code)
		}
	}
	title := t(lang, "Discordユーザーを取得できませんでした", "Discord users could not be loaded")
	explanation := t(lang, "Discordユーザー一覧を確認できません。Bot接続とDiscordサーバーの設定を確認してください。", "The Discord user list could not be checked. Verify the Bot connection and Discord server settings.")
	action := `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` + esc(t(lang, "Bot接続を確認", "Check Bot Connection")) + `</a>`
	switch kind {
	case discordMemberFailureFixture:
		explanation = t(lang, "この画面は合成QAデータのため、実Discordユーザーは取得しません。", "This screen uses synthetic QA data, so real Discord users are not requested.")
		action = `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` + esc(t(lang, "Bot接続を確認", "Check Bot Connection")) + `</a>`
	case discordMemberFailureMalformed:
		explanation = t(lang, "Discordユーザー一覧の取得リクエストを確認できません。Discordサーバーの設定を確認して再読み込みしてください。", "Discord rejected the member-list request. Verify the Discord server configuration and reload.")
		action = `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/projects", lang)) + `">` + esc(t(lang, "Discordサーバーの設定を確認", "Check Discord server settings")) + `</a>`
	case discordMemberFailureMismatch:
		explanation = t(lang, "このProductionに設定されたDiscordサーバーへBotが参加していません。", "The Bot has not joined the Discord server connected to this Production.")
		action = `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` + esc(t(lang, "Bot接続を確認", "Check Bot Connection")) + `</a>`
	case discordMemberFailureAccess:
		explanation = t(lang, "BotにDiscordサーバーのメンバーを確認する権限がありません。", "The Bot does not have permission to inspect members in this Discord server.")
		action = `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` + esc(t(lang, "Bot接続を確認", "Check Bot Connection")) + `</a>`
	case discordMemberFailureIntent:
		explanation = t(lang, "Discord Developer PortalでServer Members Intentの設定を確認してください。", "Verify the Server Members Intent in the Discord Developer Portal.")
		action = `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` + esc(t(lang, "Bot接続を確認", "Check Bot Connection")) + `</a>`
	}
	return `<div class="notice notice-warning" role="status"><strong>` + esc(title) + `</strong><p>` + esc(explanation) + `</p><div class="button-row">` + action + `</div><details class="advanced-details"><summary>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</summary><p class="field-help">` + esc(detail) + `</p></details></div>`
}

func renderGlobalUserLinkForm(w http.ResponseWriter, r *http.Request, db *gorm.DB, user *model.UserMap) {
	lang := currentLang(r)
	options, loadErr := globalDiscordUserOptions(db, storedRuntimeDiscordBotToken(db))
	var optionHTML strings.Builder
	for _, option := range options {
		selected := ""
		if option.ID == user.DiscordID {
			selected = " selected"
		}
		optionHTML.WriteString(`<option value="` + esc(option.ID) + `"` + selected + `>` + esc(option.Name) + `</option>`)
	}
	if optionHTML.Len() == 0 {
		optionHTML.WriteString(`<option value="">` + esc(t(lang, "選択できるDiscordユーザーがありません", "No Discord users are available")) + `</option>`)
	}
	disabled := ""
	message := ""
	if len(options) == 0 {
		disabled = " disabled"
		message = `<p class="field-help" role="status">` + esc(t(lang, "Discordユーザーを選択すると保存できます。", "Select a Discord user to enable saving.")) + `</p>`
	}
	if loadErr != nil {
		disabled = " disabled"
		var failure *discordMemberListFailure
		if errors.As(loadErr, &failure) && failure.Kind == discordMemberFailureFixture {
			message = `<div class="notice notice-warning" role="status"><strong>` + esc(t(lang, "Discordユーザーを取得できませんでした", "Discord users could not be loaded")) + `</strong><p>` + esc(t(lang, "この検証用Productionでは実際のDiscordユーザーを取得できません。", "Real Discord users cannot be loaded for this test-fixture Production.")) + `</p><details class="advanced-details"><summary>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</summary><p class="field-help">` + esc(t(lang, "検証用のDiscordサーバーIDは実Discord APIへ送信されません。", "Synthetic fixture server IDs are never sent to the real Discord API.")) + `</p></details></div>`
		} else {
			message = globalDiscordMemberLoadMessage(lang, loadErr)
		}
	}
	body := `<section class="section-stack"><h1>` + esc(t(lang, user.KitsuName+"の対応付けを変更", "Change link for "+user.KitsuName)) + `</h1><section class="section-card glass"><p class="hint">` + esc(t(lang, "Kitsuユーザーに、接続済みDiscordサーバーで利用できるユーザーを選択します。", "Choose a Discord user available in a connected server for this Kitsu user.")) + `</p>` + message + `<form method="POST" class="form-stack"><input type="hidden" name="action" value="save_global_link"><input type="hidden" name="user_id" value="` + fmt.Sprint(user.ID) + `"><p class="field-label">` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `</p><p><strong>` + esc(user.KitsuName) + `</strong></p><label for="global-discord-user">` + esc(t(lang, "Discordユーザー", "Discord user")) + `</label><select id="global-discord-user" name="discord_user_id" required>` + optionHTML.String() + `</select><div class="button-row"><button class="btn" type="submit"` + disabled + `>` + esc(t(lang, "保存", "Save")) + `</button><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(t(lang, "キャンセル", "Cancel")) + `</a></div></form></section></section>`
	if len(options) == 0 {
		body = strings.Replace(body, `</select><div class="button-row">`, `</select><p class="field-help" role="status">`+esc(t(lang, "Discordユーザーを選択すると保存できます。", "Select a Discord user to enable saving."))+`</p><div class="button-row">`, 1)
	}
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func renderGlobalUserMapping(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	var rows strings.Builder
	if len(model.ListUserMap(db)) == 0 && strings.TrimSpace(os.Getenv("KitsuJWTToken")) != "" {
		for _, person := range filterAssignablePersons(ListKitsuPersons(""), botAccountEmail(db)) {
			if strings.TrimSpace(person.FullName) == "" {
				continue
			}
			rows.WriteString(`<tr><td>` + esc(person.FullName) + `</td><td>` + esc(t(lang, "not_set", "Not set")) + `</td><td><span class="status-badge status-badge-blocked" role="status">` + esc(t(lang, "not_set", "Not set")) + `</span></td><td><span class="field-help">` + esc(t(lang, "Kitsuの読み取り専用データです。ローカルの紐づけはありません。", "Read-only live Kitsu data; no local link is configured.")) + `</span></td></tr>`)
		}
	}
	for _, u := range model.ListUserMap(db) {
		state := tr(lang, "status.incomplete")
		class := "blocked"
		identity := t(lang, "未設定", "Not set")
		if strings.TrimSpace(u.DiscordID) != "" {
			state = tr(lang, "wizard.connected")
			class = "success"
			identity = strings.TrimSpace(u.DiscordDisplayName)
			if identity == "" {
				identity = t(lang, "Discordユーザー紐づけ済み", "Discord user linked")
			}
		}
		if isSyntheticDiscordID(u.DiscordID) {
			state = t(lang, "検証用データ", "Fixture data")
			class = "neutral"
			identity = t(lang, "—", "—")
		} else if strings.TrimSpace(u.DiscordID) != "" && strings.TrimSpace(u.DiscordDisplayName) == "" {
			state = t(lang, "表示名未確認", "Display name not verified")
			class = "warning"
			identity = t(lang, "表示名未確認", "Display name not verified")
		}
		if strings.TrimSpace(u.DiscordID) == "" {
			state = t(lang, "未設定", "Not set")
			class = "blocked"
			identity = t(lang, "未設定", "Not set")
		} else if isSyntheticDiscordID(u.DiscordID) {
			state = t(lang, "検証用データ", "Fixture data")
			class = "neutral"
			identity = t(lang, "検証用データ", "Fixture data")
		} else if strings.TrimSpace(u.DiscordDisplayName) == "" {
			state = t(lang, "確認が必要", "Needs verification")
			class = "warning"
			identity = t(lang, "表示名未確認", "Display name not verified")
		} else {
			state = t(lang, "紐づけ済み", "Linked")
			class = "success"
			identity = strings.TrimSpace(u.DiscordDisplayName)
		}
		if isSyntheticDiscordID(u.DiscordID) {
			identity = t(lang, "—", "—")
		}
		if hasValidationOnlyProject(db) {
			rows.WriteString(`<tr><td>` + esc(u.KitsuName) + `</td><td>` + esc(identity) + `</td><td><span class="status-badge status-badge-` + class + `" role="status">` + esc(state) + `</span></td><td><span class="field-help">` + esc(t(lang, "検証専用・変更不可", "Validation only; changes disabled")) + `</span></td></tr>`)
		} else {
			change := withLang("/bot/admin/users?edit="+fmt.Sprint(u.ID), r)
			remove := `<form method="POST" class="inline-form delete-form" data-confirm="` + esc(t(lang, "この紐づけだけを解除します。Discord側のユーザーは変更しません。", "Remove only this identity link. The Discord user will not be changed.")) + `" data-require-text="` + esc(t(lang, "解除", "REMOVE")) + `"><input type="hidden" name="action" value="remove_global_link"><input type="hidden" name="user_id" value="` + fmt.Sprint(u.ID) + `"><button class="btn-danger" type="submit">` + esc(t(lang, "解除", "Remove")) + `</button></form>`
			rows.WriteString(`<tr><td>` + esc(u.KitsuName) + `</td><td>` + esc(identity) + `</td><td><span class="status-badge status-badge-` + class + `" role="status">` + esc(state) + `</span></td><td><div class="inline-actions"><a class="btn-ghost" href="` + esc(change) + `">` + esc(t(lang, "変更", "Change")) + `</a>` + remove + `</div></td></tr>`)
		}
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="4" class="empty-state"><strong>` + esc(t(lang, "ユーザー紐づけはありません。", "No user links yet.")) + `</strong><span class="field-help">` + esc(t(lang, "Kitsuユーザーが利用可能になると、ここからDiscordユーザーを選択できます。", "When Kitsu users are available, choose their Discord identity here.")) + `</span></td></tr>`)
	}
	body := `<section class="section-card glass"><h1>` + esc(tr(lang, "ia.user_mapping")) + `</h1><p class="hint">` + esc(t(lang, "KitsuユーザーとDiscordユーザーを紐づけます。", "Link Kitsu users to Discord users.")) + `</p><div class="table-wrap"><table><thead><tr><th>` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `</th><th>` + esc(t(lang, "Discordユーザー", "Discord user")) + `</th><th>` + esc(t(lang, "状態", "Status")) + `</th><th>` + esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></section>`
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func globalUserLinkingPeople(db *gorm.DB) ([]KitsuPerson, string) {
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) != "" {
		return filterAssignablePersons(ListKitsuPersons(""), botAccountEmail(db)), "live_kitsu_api"
	}
	people := make([]KitsuPerson, 0)
	for _, user := range filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db)) {
		if strings.TrimSpace(user.KitsuName) == "" {
			continue
		}
		people = append(people, KitsuPerson{ID: user.KitsuID, FullName: user.KitsuName, Email: user.KitsuEmail, Active: true})
	}
	sort.Slice(people, func(i, j int) bool { return strings.ToLower(people[i].FullName) < strings.ToLower(people[j].FullName) })
	return people, "local_user_map"
}

func renderGlobalUserLinking(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	people, _ := globalUserLinkingPeople(db)
	selectedGuildID := strings.TrimSpace(r.URL.Query().Get("discord_guild_id"))
	directory, loadErr := loadGlobalDiscordDirectory(storedRuntimeDiscordBotToken(db), selectedGuildID)
	localMaps := model.ListUserMap(db)
	findMap := func(person KitsuPerson) *model.UserMap {
		for i := range localMaps {
			m := &localMaps[i]
			if strings.TrimSpace(person.ID) != "" && strings.TrimSpace(m.KitsuID) == strings.TrimSpace(person.ID) {
				return m
			}
			if strings.TrimSpace(person.Email) != "" && strings.EqualFold(strings.TrimSpace(m.KitsuEmail), strings.TrimSpace(person.Email)) {
				return m
			}
			if strings.EqualFold(strings.TrimSpace(m.KitsuName), strings.TrimSpace(person.FullName)) {
				return m
			}
		}
		return nil
	}
	var guildOptions strings.Builder
	if len(directory.Guilds) == 0 {
		guildOptions.WriteString(`<option value="">` + esc(t(lang, "Discordサーバーが見つかりません", "No joined Discord servers")) + `</option>`)
	} else {
		guildOptions.WriteString(`<option value="">` + esc(t(lang, "Discordサーバーを選択", "Select a Discord server")) + `</option>`)
		for _, guild := range directory.Guilds {
			selected := ""
			if strings.TrimSpace(guild.ID) == strings.TrimSpace(directory.SelectedGuild.ID) {
				selected = " selected"
			}
			guildOptions.WriteString(`<option value="` + esc(guild.ID) + `"` + selected + `>` + esc(guild.Name) + `</option>`)
		}
	}
	serverForm := `<form method="GET" class="form-action-row" aria-label="` + esc(t(lang, "Discordサーバーの選択", "Discord server selection")) + `"><input type="hidden" name="lang" value="` + esc(lang) + `"><label for="global-discord-guild">` + esc(tr(lang, "ia.discord_server")) + `</label><select id="global-discord-guild" name="discord_guild_id" onchange="this.form.submit()">` + guildOptions.String() + `</select><noscript><button class="btn-ghost" type="submit">` + esc(t(lang, "表示", "Show")) + `</button></noscript></form>`
	message := ""
	if loadErr != nil {
		message = globalDiscordMemberLoadMessage(lang, loadErr)
	} else if len(directory.Guilds) > 1 && directory.SelectedGuild.ID == "" {
		message = `<div class="notice notice-info" role="status"><p>` + esc(t(lang, "Discordサーバーを選択すると、メンバーを取得して保存できます。", "Select a Discord server to load members and enable saving.")) + `</p></div>`
	} else if directory.SelectedGuild.ID != "" {
		message = `<p class="field-help" role="status">` + esc(t(lang, "表示中のDiscordサーバー: "+directory.SelectedGuild.Name, "Showing Discord server: "+directory.SelectedGuild.Name)) + `</p>`
	}
	memberOptions := func(current string) string {
		var b strings.Builder
		b.WriteString(`<option value="">` + esc(t(lang, "未設定", "Not set")) + `</option>`)
		for _, option := range directory.Options {
			selected := ""
			if strings.TrimSpace(option.ID) == strings.TrimSpace(current) {
				selected = " selected"
			}
			b.WriteString(`<option value="` + esc(option.ID) + `"` + selected + `>` + esc(option.Name) + `</option>`)
		}
		return b.String()
	}
	canSave := loadErr == nil && directory.SelectedGuild.ID != "" && len(directory.Options) > 0
	var rows strings.Builder
	for _, person := range people {
		if strings.TrimSpace(person.FullName) == "" {
			continue
		}
		mapped := findMap(person)
		identity, state := t(lang, "未設定", "Not set"), t(lang, "未設定", "Not set")
		class := "blocked"
		currentDiscordID := ""
		if mapped != nil {
			currentDiscordID = strings.TrimSpace(mapped.DiscordID)
			if isSyntheticDiscordID(currentDiscordID) {
				identity, state, class = t(lang, "検証用データ", "Fixture data"), t(lang, "検証用データ", "Fixture data"), "neutral"
				currentDiscordID = ""
			} else if currentDiscordID != "" && strings.TrimSpace(mapped.DiscordDisplayName) != "" {
				identity, state, class = strings.TrimSpace(mapped.DiscordDisplayName), t(lang, "紐づけ済み", "Linked"), "success"
			} else if currentDiscordID != "" {
				identity, state, class = t(lang, "表示名未確認", "Display name not verified"), t(lang, "確認が必要", "Needs verification"), "warning"
				currentDiscordID = ""
			}
		}
		kitsuID, kitsuName, kitsuEmail, userID := person.ID, person.FullName, person.Email, ""
		if mapped != nil {
			userID = fmt.Sprint(mapped.ID)
		}
		initialIndex := 0
		if currentDiscordID != "" {
			for i, option := range directory.Options {
				if strings.TrimSpace(option.ID) == currentDiscordID {
					initialIndex = i + 1
					break
				}
			}
		}
		disabled := " disabled"
		actionMessage := ""
		if !canSave {
			actionMessage = `<p class="field-help" role="status">` + esc(t(lang, "Discordユーザーを選択すると保存できます。", "Select a Discord user to enable saving.")) + `</p>`
		}
		form := `<form method="POST" class="inline-form user-link-form"><input type="hidden" name="action" value="save_global_link"><input type="hidden" name="user_id" value="` + esc(userID) + `"><input type="hidden" name="kitsu_id" value="` + esc(kitsuID) + `"><input type="hidden" name="kitsu_name" value="` + esc(kitsuName) + `"><input type="hidden" name="kitsu_email" value="` + esc(kitsuEmail) + `"><input type="hidden" name="discord_guild_id" value="` + esc(directory.SelectedGuild.ID) + `"><select name="discord_user_id" aria-label="` + esc(kitsuName+" - "+t(lang, "Discordユーザー", "Discord user")) + `">` + memberOptions(currentDiscordID) + `</select><button class="btn" type="submit"` + disabled + `>` + esc(t(lang, "保存", "Save")) + `</button>` + actionMessage + `</form>`
		form = strings.Replace(form, `<select name="discord_user_id"`, `<select data-initial-index="`+strconv.Itoa(initialIndex)+`" name="discord_user_id" onchange="this.form.querySelector('button[type=submit]').disabled = this.value === '' || this.selectedIndex === Number(this.dataset.initialIndex)"`, 1)
		if mapped != nil && mapped.ID > 0 {
			form += `<form method="POST" class="inline-form"><input type="hidden" name="action" value="remove_global_link"><input type="hidden" name="user_id" value="` + esc(fmt.Sprint(mapped.ID)) + `"><button class="btn-ghost" type="submit">` + esc(t(lang, "解除", "Unlink")) + `</button></form>`
		}
		rows.WriteString(`<tr class="user-link-grid-row"><td data-label="` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `">` + esc(person.FullName) + `</td><td data-label="` + esc(t(lang, "Discordユーザー", "Discord user")) + `">` + esc(identity) + `</td><td data-label="` + esc(t(lang, "状態", "Status")) + `"><span class="status-badge status-badge-` + class + `" role="status">` + esc(state) + `</span></td><td data-label="` + esc(t(lang, "操作", "Actions")) + `"><div class="user-link-actions">` + form + `</div></td></tr>`)
	}
	if len(people) == 0 {
		rows.WriteString(`<tr><td colspan="4" class="empty-state"><strong>` + esc(t(lang, "Kitsuユーザーが見つかりません", "No Kitsu users were returned")) + `</strong></td></tr>`)
	}
	body := `<section class="section-stack"><h1>` + esc(tr(lang, "ia.user_mapping")) + `</h1><section class="section-card glass">` + serverForm + message + `</section><div class="table-wrap"><table><thead><tr><th>` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `</th><th>` + esc(t(lang, "Discordユーザー", "Discord user")) + `</th><th>` + esc(t(lang, "状態", "Status")) + `</th><th>` + esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></section>`
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func renderIANewConnection(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r != nil {
		lang := currentLang(r)
		if ValidationOnlyModeEnabled() {
			body := `<section class="section-card glass" role="status"><h1>` + esc(tr(lang, "ia.new_connection")) + `</h1><p class="hint">` + esc(t(lang, "この環境は実データの表示確認専用です。Production接続、Discord設定、チャンネル作成は実行できません。", "This environment is for real-data display validation only. Production connection, Discord setup, and channel creation are disabled.")) + `</p></section>`
			fmt.Fprint(w, adminPage(lang, "", r, body))
			return
		}
		kitsuHost := model.GetSetting(db, "kitsu.hostname")
		botToken := storedRuntimeDiscordBotToken(db)
		projects := ListKitsuProjects(kitsuHost)
		requestedProjectID := strings.TrimSpace(r.URL.Query().Get("project"))
		requestedStep := strings.TrimSpace(r.URL.Query().Get("wizard_step"))
		if requestedProjectID != "" && (requestedStep == "3" || requestedStep == "4") {
			selected := wizardProject(projects, requestedProjectID)
			if selected.ID != "" && model.FindProjectByKitsuID(db, selected.ID) == nil {
				updateWizardState(r, func(state *wizardState) {
					state.ProductionID = selected.ID
					if guildID := strings.TrimSpace(r.URL.Query().Get("plan_guild")); guildID != "" {
						state.GuildID = guildID
					}
				})
			}
		}
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
				body += renderExplicitTaskTypeChannelPlan(model.Project{KitsuProjectID: selected.ID, Name: selected.Name}, routingTaskTypesForProduction(selected.ID), botToken, r, lang, db)
			}
		}
		if strings.TrimSpace(botToken) == "" {
			body += `<section class="section-card glass" role="status"><h2>` + esc(t(lang, "対応が必要", "Action required")) + `</h2><p class="hint">` + esc(t(lang, "Discordサーバーを読み込むにはBot接続が必要です。先にBot接続を設定してください。", "Bot Connection is required to read Discord servers. Complete Bot Connection first.")) + `</p><a class="btn" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a></section>`
		}
		body += `</div>`
		fmt.Fprint(w, adminPage(lang, "", r, body))
	}
}

func projectIDFromRequest(r *http.Request) string {
	if projectID := strings.TrimSpace(r.URL.Query().Get("project")); projectID != "" {
		return projectID
	}
	return strings.TrimSpace(wizardStateForRequest(r).ProductionID)
}

func wizardStep(r *http.Request, botToken string, db *gorm.DB, projectID, guildID string) int {
	ready := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), botToken).PrerequisitesReady
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
	if r.URL.Query().Get("action") == "review" {
		return 5
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
	if step > 1 && !readiness.PrerequisitesReady {
		step = 1
	}
	if step >= 3 {
		selected := wizardProject(projects, projectID)
		if selected.ID == "" || model.FindProjectByKitsuID(db, projectID) != nil {
			step = 2
		}
	}
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
	body := `<div class="section-stack"><section class="section-card glass"><p class="hint">` + esc(tr(lang, "wizard.description")) + `</p><div class="setup-steps" aria-label="` + esc(tr(lang, "wizard.progress")) + `">` + steps.String() + `</div></section>`
	switch step {
	case 1:
		body += renderWizardPrerequisitesShared(lang, r, readiness)
	case 2:
		body += renderWizardProductionLocalized(lang, r, db, projects)
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

func renderWizardPrerequisitesShared(lang string, r *http.Request, readiness SharedBotRuntimeReadiness) string {
	view := readinessViewFor(lang, r, readiness)
	status := func(ok bool) string {
		if ok {
			return t(lang, "接続済み", "Connected")
		}
		return t(lang, "未設定", "Not configured")
	}
	productionValue := t(lang, "0件", "0 connected")
	if readiness.ProductionConnected {
		productionValue = t(lang, "接続済み", "Connected")
	}
	body := `<section class="section-card glass" aria-labelledby="wizard-prerequisites-title"><h2 id="wizard-prerequisites-title">` + esc(tr(lang, "wizard.prerequisites_title")) + `</h2><dl class="status-list">` +
		statusSummaryRow(t(lang, "Kitsu接続", "Kitsu connection"), map[bool]string{true: "success", false: "blocked"}[readiness.KitsuConfigured], status(readiness.KitsuConfigured), "", "") +
		statusSummaryRow(t(lang, "Discord Bot", "Discord Bot"), map[bool]string{true: "success", false: "blocked"}[readiness.DiscordConfigured], status(readiness.DiscordConfigured), "", "") +
		statusSummaryRow(t(lang, "Production接続", "Production connections"), map[bool]string{true: "success", false: "blocked"}[readiness.ProductionConnected], productionValue, "", "") +
		statusSummaryRow(t(lang, "通知状態", "Notifications"), view.NotificationClass, view.Notification, view.Hint, "") + `</dl>`
	if !readiness.PrerequisitesReady {
		body += `<p class="state-explanation" role="status" aria-live="polite">` + esc(view.Hint) + `</p><a class="btn" href="` + esc(view.ActionURL) + `">` + esc(view.ActionLabel) + `</a>`
	} else {
		body += `<p class="state-explanation" role="status" aria-live="polite">` + esc(t(lang, "接続の前提条件が整いました。Productionを選択できます。", "Connection prerequisites are ready. Select a Production to continue.")) + `</p><a class="btn" href="` + esc(setupWizardURL(r, 2, "", "", false)) + `">` + esc(t(lang, "次へ", "Next")) + `</a>`
	}
	return body + `</section>`
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

func renderWizardProductionLocalized(lang string, r *http.Request, db *gorm.DB, projects []KitsuProject) string {
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
	errorMessage := ""
	if r.URL.Query().Get("wizard_step") == "3" {
		projectID := strings.TrimSpace(r.URL.Query().Get("project"))
		switch {
		case projectID == "":
			errorMessage = `<p class="state-explanation" role="alert">` + esc(tr(lang, "wizard.error.select_production")) + `</p>`
		case model.FindProjectByKitsuID(db, projectID) != nil:
			errorMessage = `<p class="state-explanation" role="alert">` + esc(tr(lang, "wizard.error.already_connected")) + `</p>`
		default:
			found := false
			for _, p := range projects {
				if p.ID == projectID {
					found = true
					break
				}
			}
			if !found {
				errorMessage = `<p class="state-explanation" role="alert">` + esc(tr(lang, "wizard.error.invalid_production")) + `</p>`
			}
		}
	}
	return `<section class="section-card glass" aria-labelledby="wizard-production-title"><h2 id="wizard-production-title">` + esc(tr(lang, "wizard.production_title")) + `</h2>` + errorMessage + `<form method="GET" class="section-stack"><input type="hidden" name="wizard_step" value="3"><label for="wizard-production">` + esc(tr(lang, "wizard.production_label")) + `</label><select id="wizard-production" name="project" required aria-describedby="wizard-production-help">` + options.String() + `</select><p id="wizard-production-help" class="field-help">` + esc(tr(lang, "wizard.production_help")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 1, "", "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.next")) + `</button></div></form></section>`
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
	errorMessage := ""
	if r.URL.Query().Get("wizard_step") == "3" {
		if strings.TrimSpace(r.URL.Query().Get("project")) == "" {
			errorMessage = `<p class="state-explanation" role="alert">` + esc(t(lang, "Productionを選択してください", "Select a Production")) + `</p>`
		} else if selected := wizardProject(projects, strings.TrimSpace(r.URL.Query().Get("project"))); selected.ID != "" && model.FindProjectByKitsuID(db, selected.ID) != nil {
			errorMessage = `<p class="state-explanation" role="alert">` + esc(t(lang, "このProductionはすでに連携されています", "This Production is already connected")) + `</p>`
		} else {
			errorMessage = `<p class="state-explanation" role="alert">` + esc(t(lang, "選択したProductionを確認できませんでした", "The selected Production could not be verified")) + `</p>`
		}
	}
	return `<section class="section-card glass" aria-labelledby="wizard-production-title"><h2 id="wizard-production-title">` + esc(tr(lang, "wizard.production_title")) + `</h2>` + errorMessage + `<form method="GET" class="section-stack"><input type="hidden" name="wizard_step" value="3"><label for="wizard-production">` + esc(tr(lang, "wizard.production_label")) + `</label><select id="wizard-production" name="project" required aria-describedby="wizard-production-help">` + options.String() + `</select><p id="wizard-production-help" class="field-help">` + esc(tr(lang, "wizard.production_help")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 1, "", "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.next")) + `</button></div></form></section>`
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
	return `<section class="section-card glass" aria-labelledby="wizard-server-title"><h2 id="wizard-server-title">` + esc(tr(lang, "wizard.server_title")) + `</h2><form method="GET" class="section-stack"><input type="hidden" name="wizard_step" value="4"><input type="hidden" name="project" value="` + esc(projectID) + `"><label for="wizard-server">` + esc(tr(lang, "wizard.server_label")) + `</label><select id="wizard-server" name="plan_guild" required>` + options.String() + `</select><p class="field-help">` + esc(tr(lang, "wizard.server_help")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 2, projectID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.next")) + `</button></div></form></section>`
}

func wizardProject(projects []KitsuProject, id string) KitsuProject {
	for _, p := range projects {
		if p.ID == id {
			return p
		}
	}
	return KitsuProject{}
}
func wizardTaskTypes(projectID string) []kitsu.TaskType {
	return routingTaskTypesForProduction(projectID)
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
	allTaskTypes := wizardTaskTypes(project.ID)
	if taskTypePlanRequestInvalid(r, allTaskTypes) {
		return `<section class="section-card glass" role="alert"><h2>` + esc(tr(lang, "wizard.plan_title")) + `</h2><p class="state-explanation">` + esc(tr(lang, "wizard.plan_blocked")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 4, projectID, guildID, false)) + `">` + esc(tr(lang, "wizard.back")) + `</a></div></section>`
	}
	taskTypes, overrides := taskTypePlanRequest(r, allTaskTypes)
	existing := existingChannelsForPlanWithLegacy(channels, model.ListProductionChannelMappings(db, project.ID), model.ListProjectWebhooks(db, project.ID))
	plan := BuildTaskTypeChannelPlanWithOverrides(project.ID, guildID, taskTypes, existing, overrides)
	updateWizardState(r, func(state *wizardState) {
		state.ProductionID = project.ID
		state.GuildID = guildID
		state.PlanFingerprint = plan.Fingerprint()
	})
	if review {
		return renderWizardPlanReview(lang, r, project, guildID, botToken, plan)
	}
	if r != nil {
		return renderWizardPlanPolished(lang, r, project, projectID, guildID, allTaskTypes, plan)
	}
	showStatus := false
	for _, entry := range plan.Entries {
		if entry.Action != "create" {
			showStatus = true
		}
	}
	var rows strings.Builder
	for _, entry := range plan.Entries {
		status := ""
		if showStatus {
			status = `<span class="wizard-plan-status wizard-plan-status-` + esc(entry.Action) + `">` + esc(wizardPlanActionLabel(lang, entry.Action)) + `</span>`
		}
		rows.WriteString(`<tr draggable="true" data-task-type="` + esc(entry.TaskTypeID) + `"><td data-label="` + esc(tr(lang, "wizard.task_type")) + `"><span class="wizard-drag-handle" aria-hidden="true">↕</span><strong>` + esc(entry.DisplayName()) + `</strong><div class="wizard-move-controls"><button type="button" class="wizard-move" data-move="up" aria-label="` + esc(t(lang, "上へ移動", "Move up")) + `">↑</button><button type="button" class="wizard-move" data-move="down" aria-label="` + esc(t(lang, "下へ移動", "Move down")) + `">↓</button></div></td><td data-label="` + esc(tr(lang, "wizard.channel")) + `"><label class="sr-only" for="wizard-channel-` + esc(entry.TaskTypeID) + `">` + esc(entry.DisplayName()) + `</label><span class="wizard-channel-prefix">#</span><input id="wizard-channel-` + esc(entry.TaskTypeID) + `" class="wizard-channel-input" name="channel_name_` + esc(entry.TaskTypeID) + `" value="` + esc(entry.ChannelName) + `" maxlength="100" required>` + status + `<input type="hidden" name="channel_order_` + esc(entry.TaskTypeID) + `" value="` + strconv.Itoa(entry.Order) + `"></td></tr>`)
	}
	body := `<section class="section-card glass wizard-plan-card" aria-labelledby="wizard-plan-title"><div class="page-heading"><div><h2 id="wizard-plan-title">` + esc(tr(lang, "wizard.plan_title")) + `</h2><p class="hint">` + esc(tr(lang, "wizard.plan_hint")) + `</p></div><span class="status-pill ` + map[bool]string{true: "ok", false: "bad"}[plan.Valid()] + `">` + esc(wizardPlanStateLabel(lang, plan.Valid())) + `</span></div><form method="GET" action="` + esc(withLang("/bot/setup", r)) + `"><input type="hidden" name="project" value="` + esc(project.ID) + `"><input type="hidden" name="plan_guild" value="` + esc(guildID) + `"><div class="table-wrap wizard-plan-table"><table><caption class="sr-only">` + esc(tr(lang, "wizard.plan_caption")) + `</caption><thead><tr><th>` + esc(tr(lang, "wizard.task_type")) + `</th><th>` + esc(tr(lang, "wizard.channel")) + `</th></tr></thead><tbody data-wizard-plan-sort>` + rows.String() + `</tbody></table></div>`
	if !plan.Valid() {
		duplicateNotice := ""
		if len(plan.DuplicateNames) > 0 {
			duplicateNotice = `<p class="state-explanation" role="alert">` + esc(tr(lang, "channel_plan.duplicate_name")) + `</p>`
		}
		return body + `</form>` + duplicateNotice + `<p class="state-explanation" role="alert">` + esc(tr(lang, "wizard.plan_blocked")) + `</p>` + renderBlockedWizardPlanNavigation(lang, r, projectID, guildID, false) + `</section>`
	}
	return body + `<p class="field-help" role="status">` + esc(tr(lang, "wizard.no_write")) + `</p><div class="button-row wizard-plan-actions"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 3, projectID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit" name="wizard_step" value="5">` + esc(tr(lang, "wizard.review")) + `</button></div><input type="hidden" name="review" value="1"></form><script>(function(){var body=document.querySelector('[data-wizard-plan-sort]');if(!body)return;var refresh=function(){Array.prototype.forEach.call(body.querySelectorAll('tr'),function(row,index){var field=row.querySelector('input[type="hidden"][name^="channel_order_"]');if(field)field.value=String(index+1);});};var move=function(row,direction){var target=direction==='up'?row.previousElementSibling:row.nextElementSibling;if(!target)return;if(direction==='up')body.insertBefore(row,target);else body.insertBefore(target,row);refresh();row.querySelector('button[data-move="'+(direction==='up'?'up':'down')+'"]')?.focus();};Array.prototype.forEach.call(body.querySelectorAll('tr'),function(row){row.addEventListener('dragstart',function(event){event.dataTransfer.setData('text/plain',row.dataset.taskType);row.classList.add('is-dragging');});row.addEventListener('dragend',function(){row.classList.remove('is-dragging');});row.addEventListener('dragover',function(event){event.preventDefault();});row.addEventListener('drop',function(event){event.preventDefault();var id=event.dataTransfer.getData('text/plain'),dragged=body.querySelector('tr[data-task-type="'+id+'"]');if(dragged&&dragged!==row){var rect=row.getBoundingClientRect();body.insertBefore(dragged,event.clientY<rect.top+rect.height/2?row:row.nextElementSibling);refresh();}});Array.prototype.forEach.call(row.querySelectorAll('button[data-move]'),function(button){button.addEventListener('click',function(){move(row,button.dataset.move);});});});refresh();})();</script></section>`
}

func renderWizardPlanPolished(lang string, r *http.Request, project KitsuProject, projectID, guildID string, allTaskTypes []kitsu.TaskType, plan TaskTypeChannelPlan) string {
	var rows strings.Builder
	included := map[string]bool{}
	for _, entry := range plan.Entries {
		included[entry.TaskTypeID] = true
		rows.WriteString(`<tr draggable="true" tabindex="0" data-task-type="` + esc(entry.TaskTypeID) + `" aria-label="` + esc(trf(lang, "wizard.row_move_hint", entry.DisplayName())) + `"><td data-label="` + esc(tr(lang, "wizard.task_type")) + `"><strong>` + esc(entry.DisplayName()) + `</strong><input type="hidden" name="included_task_type_id" value="` + esc(entry.TaskTypeID) + `"></td><td data-label="` + esc(tr(lang, "wizard.channel")) + `"><label class="sr-only" for="wizard-channel-` + esc(entry.TaskTypeID) + `">` + esc(entry.DisplayName()) + `</label><span class="wizard-channel-prefix">#</span><input id="wizard-channel-` + esc(entry.TaskTypeID) + `" class="wizard-channel-input" name="channel_name_` + esc(entry.TaskTypeID) + `" value="` + esc(entry.ChannelName) + `" maxlength="100" required><input type="hidden" name="channel_order_` + esc(entry.TaskTypeID) + `" value="` + strconv.Itoa(entry.Order) + `"></td><td><button type="submit" name="action" value="exclude" class="wizard-exclude" data-exclude="` + esc(entry.TaskTypeID) + `" aria-label="` + esc(tr(lang, "wizard.exclude")) + `">×</button></td></tr>`)
	}
	var excluded strings.Builder
	for _, taskType := range allTaskTypes {
		id := strings.TrimSpace(taskType.ID)
		if !included[id] {
			excluded.WriteString(`<option value="` + esc(id) + `">` + esc(taskType.Name) + `</option>`)
		}
	}
	statusBadge := ""
	if !plan.Valid() {
		statusBadge = `<span class="status-pill bad">` + esc(wizardPlanStateLabel(lang, false)) + `</span>`
	}
	body := `<section class="section-card glass wizard-plan-card" aria-labelledby="wizard-plan-title"><div class="page-heading"><div><h2 id="wizard-plan-title">` + esc(tr(lang, "wizard.plan_title")) + `</h2><p class="hint">` + esc(tr(lang, "wizard.plan_hint")) + `</p></div>` + statusBadge + `</div><form method="GET" class="wizard-plan-form" action="` + esc(withLang("/bot/setup", r)) + `"><input type="hidden" name="project" value="` + esc(project.ID) + `"><input type="hidden" name="plan_guild" value="` + esc(guildID) + `"><input type="hidden" name="wizard_step" value="4"><input type="hidden" name="exclude_task_type_id" id="wizard-task-type-action"><div class="table-wrap wizard-plan-table"><table><caption class="sr-only">` + esc(tr(lang, "wizard.plan_caption")) + `</caption><thead><tr><th>` + esc(tr(lang, "wizard.task_type")) + `</th><th>` + esc(tr(lang, "wizard.channel")) + `</th><th></th></tr></thead><tbody data-wizard-plan-sort>` + rows.String() + `</tbody></table></div>`
	addDisabled := excluded.Len() == 0
	selectAttrs := ""
	addAttrs := ""
	selectOptions := `<option value="">` + esc(tr(lang, "wizard.select_task_type")) + `</option>` + excluded.String()
	if addDisabled {
		selectAttrs = ` disabled aria-disabled="true"`
		addAttrs = ` disabled aria-disabled="true"`
		selectOptions = `<option value="">` + esc(tr(lang, "wizard.all_task_types_included")) + `</option>`
	}
	selectWrapperClass := "wizard-add-task-type-select"
	if addDisabled {
		selectWrapperClass += " is-disabled"
	}
	body += `<div class="wizard-add-task-type" role="group" aria-labelledby="wizard-add-task-type-label"><label id="wizard-add-task-type-label" for="wizard-add-task-type">` + esc(tr(lang, "wizard.add_task_type")) + `</label><span class="` + selectWrapperClass + `"><select id="wizard-add-task-type" name="task_type_id"` + selectAttrs + `>` + selectOptions + `</select></span><button class="btn-ghost" type="submit" name="action" value="include"` + addAttrs + `>` + esc(tr(lang, "wizard.add")) + `</button></div>`
	body = `<style>.wizard-add-task-type{display:grid;grid-template-columns:auto minmax(220px,1fr) auto;align-items:center;gap:8px;margin-top:12px}.wizard-add-task-type label{width:auto;margin:0}.wizard-add-task-type-select{position:relative;min-width:0}.wizard-add-task-type-select select{appearance:none;-webkit-appearance:none;width:100%;padding-right:36px}.wizard-add-task-type-select::after{content:"";position:absolute;right:14px;top:50%;width:7px;height:7px;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;color:var(--muted);transform:translateY(-65%) rotate(45deg);pointer-events:none}.wizard-add-task-type-select.is-disabled::after{opacity:.55}.wizard-add-task-type button{white-space:nowrap}@media(max-width:640px){.wizard-add-task-type{grid-template-columns:1fr}.wizard-add-task-type label,.wizard-add-task-type-select,.wizard-add-task-type button{width:100%}}</style>` + body
	if !plan.Valid() {
		return body + `</form><p class="state-explanation" role="alert">` + esc(tr(lang, "wizard.plan_blocked")) + `</p>` + renderBlockedWizardPlanNavigation(lang, r, projectID, guildID, false) + `</section>`
	}
	return body + `<div class="button-row wizard-plan-actions"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 3, projectID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit" name="action" value="review">` + esc(tr(lang, "wizard.review")) + `</button></div></form><script>(function(){var body=document.querySelector('[data-wizard-plan-sort]');if(!body)return;var actionField=document.getElementById('wizard-task-type-action');var refresh=function(){Array.prototype.forEach.call(body.querySelectorAll('tr'),function(row,index){var field=row.querySelector('input[type="hidden"][name^="channel_order_"]');if(field)field.value=String(index+1);});};var move=function(row,direction){var target=direction==='up'?row.previousElementSibling:row.nextElementSibling;if(!target)return;if(direction==='up')body.insertBefore(row,target);else body.insertBefore(target,row);refresh();};Array.prototype.forEach.call(body.querySelectorAll('tr'),function(row){row.addEventListener('dragstart',function(event){event.dataTransfer.setData('text/plain',row.dataset.taskType);row.classList.add('is-dragging');});row.addEventListener('dragend',function(){row.classList.remove('is-dragging');});row.addEventListener('dragover',function(event){event.preventDefault();row.classList.add('drag-over');});row.addEventListener('dragleave',function(){row.classList.remove('drag-over');});row.addEventListener('drop',function(event){event.preventDefault();row.classList.remove('drag-over');var id=event.dataTransfer.getData('text/plain'),dragged=body.querySelector('tr[data-task-type="'+id+'"]');if(dragged&&dragged!==row){var rect=row.getBoundingClientRect();body.insertBefore(dragged,event.clientY<rect.top+rect.height/2?row:row.nextElementSibling);refresh();}});row.addEventListener('keydown',function(event){if(!event.altKey||!['ArrowUp','ArrowDown'].includes(event.key))return;event.preventDefault();move(row,event.key==='ArrowUp'?'up':'down');row.focus();});var exclude=row.querySelector('[data-exclude]');if(exclude)exclude.addEventListener('click',function(){if(actionField)actionField.value=exclude.getAttribute('data-exclude');});});refresh();})();</script></section>`
}

func renderWizardPlanReview(lang string, r *http.Request, project KitsuProject, guildID, botToken string, plan TaskTypeChannelPlan) string {
	guildName := t(lang, "選択したDiscordサーバー", "Selected Discord server")
	if guilds, err := ListBotGuilds(botToken); err == nil {
		for _, guild := range guilds {
			if strings.TrimSpace(guild.ID) == strings.TrimSpace(guildID) && strings.TrimSpace(guild.Name) != "" {
				guildName = guild.Name
				break
			}
		}
	}
	var ordered strings.Builder
	showStatus := plan.ReuseCount() > 0 || plan.CreateCount() != len(plan.Entries)
	for _, entry := range plan.Entries {
		line := `<li><code>#` + esc(entry.ChannelName) + `</code>`
		if showStatus {
			line += ` <span class="wizard-plan-status wizard-plan-status-` + esc(entry.Action) + `">` + esc(wizardPlanActionLabel(lang, entry.Action)) + `</span>`
		}
		ordered.WriteString(line + `</li>`)
	}
	var hidden strings.Builder
	for _, entry := range plan.Entries {
		hidden.WriteString(`<input type="hidden" name="channel_name_` + esc(entry.TaskTypeID) + `" value="` + esc(entry.ChannelName) + `"><input type="hidden" name="channel_order_` + esc(entry.TaskTypeID) + `" value="` + strconv.Itoa(entry.Order) + `">`)
	}
	countSummary := trf(lang, "wizard.channels_create", plan.CreateCount())
	if reused := plan.ReuseCount(); reused > 0 {
		countSummary += ` ` + trf(lang, "wizard.channels_reuse", reused)
	}
	body := `<section class="section-card glass wizard-review-card" aria-labelledby="wizard-review-title"><div class="page-heading"><div><h2 id="wizard-review-title">` + esc(tr(lang, "wizard.review_title")) + `</h2><p class="hint">` + esc(tr(lang, "wizard.review_hint")) + `</p></div></div><dl class="wizard-connection-summary"><div><dt>` + esc(tr(lang, "wizard.production_summary")) + `</dt><dd>` + esc(project.Name) + `</dd></div><div><dt>` + esc(tr(lang, "wizard.server_summary")) + `</dt><dd>` + esc(guildName) + `</dd></div><div><dt>` + esc(tr(lang, "wizard.area_summary")) + `</dt><dd>` + esc(KitsuSyncCategoryName(project.Name)) + `</dd></div></dl><p class="wizard-count-summary">` + esc(countSummary) + `</p><h3>` + esc(tr(lang, "wizard.order_title")) + `</h3><ol class="wizard-final-channel-list">` + ordered.String() + `</ol>`
	if !plan.Valid() {
		return body + `<p class="state-explanation" role="alert">` + esc(tr(lang, "wizard.plan_blocked")) + `</p>` + renderBlockedWizardPlanNavigation(lang, r, project.ID, guildID, true) + `</section>`
	}
	return body + `<p class="field-help">` + esc(tr(lang, "wizard.no_write")) + `</p><form method="POST" action="` + esc(withLang("/bot/setup", r)) + `" class="wizard-confirm-form"><input type="hidden" name="action" value="confirm_task_type_channels"><input type="hidden" name="project_id" value="` + esc(project.ID) + `"><input type="hidden" name="guild_id" value="` + esc(guildID) + `"><input type="hidden" name="plan_fingerprint" value="` + esc(plan.Fingerprint()) + `">` + hidden.String() + `<label class="wizard-confirm-control" for="wizard-confirm"><input id="wizard-confirm" type="checkbox" name="confirm_plan" value="yes" required> ` + esc(tr(lang, "wizard.confirm")) + `</label><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 4, project.ID, guildID, false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button id="wizard-execute" class="btn" type="submit" disabled>` + esc(tr(lang, "wizard.execute")) + `</button></div></form><script>(function(){var check=document.getElementById('wizard-confirm'),button=document.getElementById('wizard-execute');if(check&&button){var sync=function(){button.disabled=!check.checked;};check.addEventListener('change',sync);sync();}})();</script></section>`
}

func wizardPlanActionLabel(lang, action string) string {
	return map[string]string{"create": tr(lang, "wizard.create"), "reuse": tr(lang, "wizard.reuse"), "conflict": tr(lang, "wizard.conflict"), "blocked": tr(lang, "wizard.review_required")}[action]
}

func wizardPlanStateLabel(lang string, valid bool) string {
	if valid {
		return tr(lang, "wizard.plan_ready")
	}
	return tr(lang, "wizard.plan_needs_attention")
}

func renderBlockedWizardPlanNavigation(lang string, r *http.Request, projectID, guildID string, review bool) string {
	forwardLabel := tr(lang, "wizard.review")
	backURL := setupWizardURL(r, 3, projectID, "", false)
	if review {
		forwardLabel = tr(lang, "wizard.execute")
		backURL = setupWizardURL(r, 4, projectID, guildID, false)
	}
	return `<div class="button-row"><a class="btn-ghost" href="` + esc(backURL) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="button" disabled aria-disabled="true">` + esc(forwardLabel) + `</button></div>`
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
func renderWizardFrame(lang string, step int, body string) string {
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
		steps.WriteString(`<span class="setup-step ` + state + `"` + map[bool]string{true: ` aria-current="step"`, false: ""}[n == step] + `><span class="step-num">` + strconv.Itoa(n) + `</span><span class="step-label">` + esc(label) + `</span></span>`)
	}
	return `<div class="section-stack"><section class="section-card glass"><div class="setup-steps" aria-label="` + esc(tr(lang, "wizard.progress")) + `">` + steps.String() + `</div></section>` + body + `</div>`
}

func renderWizardComplete(lang string, r *http.Request, db *gorm.DB, target interface{}) string {
	projectID, name := "", ""
	switch value := target.(type) {
	case string:
		projectID = value
		if project := model.FindProjectByKitsuID(db, projectID); project != nil {
			name = project.Name
		}
	case firstTimeConnectionPlan:
		projectID, name = value.Project.ID, value.Project.Name
		name += " " + value.GuildName
	}
	return `<section class="section-card glass" role="status" aria-live="polite"><h2>` + esc(tr(lang, "wizard.complete_title")) + `</h2><p>` + esc(trf(lang, "wizard.complete_message", name)) + `</p><a class="btn" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(projectID), r)) + `">` + esc(tr(lang, "wizard.open_production")) + `</a></section>`
}

func renderIADashboardWithRuntime(w http.ResponseWriter, r *http.Request, db *gorm.DB, runtimeHealthy func() bool) {
	renderIADashboard(w, r, db)
}

func renderDashboardMenuRefined(lang string, r *http.Request, db *gorm.DB, projects []model.Project, attentionCount int, readiness SharedBotRuntimeReadiness) string {
	type card struct{ label, path, description, first, second, firstClass, secondClass string }
	cards := []card{
		{tr(lang, "connections.title"), "/bot/admin/bot", t(lang, "KitsuとDiscordの接続状態を確認します。", "Kitsu and Discord connection health."), dashboardConnectionLabel(lang, "Kitsu", readiness.KitsuConfigured), dashboardConnectionLabel(lang, "Discord", readiness.DiscordConfigured), map[bool]string{true: "ok", false: "warning"}[readiness.KitsuConfigured], map[bool]string{true: "ok", false: "warning"}[readiness.DiscordConfigured]},
		{tr(lang, "ia.production_list"), "/bot/admin/projects", t(lang, "接続済みプロダクションの状態と設定を確認します。", "Connected Production state and settings. Productions needing attention: "+strconv.Itoa(attentionCount)+". Needs attention is shown here when applicable."), strconv.Itoa(connectedProductionCount(projects)), strconv.Itoa(attentionCount), "ok", map[bool]string{true: "warning", false: "ok"}[attentionCount > 0]},
		{tr(lang, "ia.user_mapping"), "/bot/admin/users", t(lang, "人間のKitsuユーザーとDiscordユーザーの紐づけを管理します。", "Human Kitsu-to-Discord links."), t(lang, "設定済", "Configured"), "", "ok", ""},
		{tr(lang, "ia.system_status"), "/bot/admin/health", t(lang, "未設定の場合は接続設定が必要です。システム状態と通知の利用可否を確認します。", "Setup required when a connection is not configured. System health and notification availability."), readinessViewFor(lang, r, readiness).Label, t(lang, "利用不可", "Unavailable"), "warning", "warning"},
		{tr(lang, "ia.audit_log"), "/bot/admin/audit", t(lang, "操作履歴と通知イベントを確認します。", "Action history and notification events."), t(lang, "記録なし", "No records"), "", "muted", ""},
	}
	productionAttention := ""
	if attentionCount > 0 {
		productionAttention = fmt.Sprintf("%s %d", t(lang, "要確認", "Needs review"), attentionCount)
	}
	cards[0].first = statusText(lang, readiness.KitsuConfigured)
	cards[0].second = statusText(lang, readiness.DiscordConfigured)
	cards[1].first = fmt.Sprintf("%s %d", t(lang, "接続済", "Connected"), connectedProductionCount(projects))
	cards[1].second = productionAttention
	var body strings.Builder
	body.WriteString(`<section class="dashboard-cta"><div><h2>` + esc(tr(lang, "ia.new_connection")) + `</h2><p class="hint">` + esc(t(lang, "新しいプロダクションを接続", "Connect a Kitsu Production to a Discord server.")) + `</p></div><a class="btn dashboard-cta-action" href="` + esc(withLang("/bot/setup", r)) + `">` + esc(t(lang, "新しい接続を開始", "Open setup")) + `</a></section><section class="dashboard-menu"><h2>` + esc(t(lang, "管理メニュー", "Management")) + `</h2><div class="dashboard-menu-grid">`)
	for _, item := range cards {
		statusMarkup := `<span class="dashboard-status-chip ` + item.firstClass + `">` + esc(item.first) + `</span>`
		if item.label == tr(lang, "connections.title") {
			statusMarkup = `<span class="dashboard-service-status"><span aria-label="Kitsu ` + esc(item.first) + `"><strong>Kitsu</strong><span class="dashboard-status-chip ` + item.firstClass + `">` + esc(item.first) + `</span></span><span aria-label="Discord ` + esc(item.second) + `"><strong>Discord</strong><span class="dashboard-status-chip ` + item.secondClass + `">` + esc(item.second) + `</span></span></span>`
		}
		cardClass := "dashboard-menu-card"
		body.WriteString(`<a class="` + cardClass + `" href="` + esc(withLang(item.path, r)) + `"><span class="dashboard-menu-copy"><strong>` + esc(item.label) + `</strong><span class="field-help">` + esc(item.description) + `</span></span><span class="dashboard-menu-status">` + statusMarkup)
		if item.second != "" && item.label != tr(lang, "connections.title") {
			body.WriteString(`<span class="dashboard-status-chip ` + item.secondClass + `">` + esc(item.second) + `</span>`)
		}
		body.WriteString(`</span></a>`)
	}
	body.WriteString(`</div></section>`)
	return body.String()
}

func dashboardConnectionLabel(lang, service string, configured bool) string {
	status := t(lang, "未設定", "Not configured")
	if configured {
		status = t(lang, "接続済", "Connected")
	}
	return service + " " + status
}

func connectedProductionCount(projects []model.Project) int {
	count := 0
	for _, project := range projects {
		if !project.ReadOnlyPreview && !project.ValidationOnly {
			count++
		}
	}
	return count
}

func productionConnectionStatus(project model.Project, lang string) (string, string) {
	if project.ReadOnlyPreview || project.ValidationOnly {
		return "warning", t(lang, "未接続", "Disconnected")
	}
	return "ok", t(lang, "接続済", "Connected")
}

func renderDisconnectedProductionCard(lang string, r *http.Request, project model.Project) string {
	return `<article class="section-card glass production-list-item"><div><h2>` + esc(project.Name) + `</h2></div><span class="status-pill warning">` + esc(t(lang, "未接続", "Disconnected")) + `</span><a class="btn" href="` + esc(withLang("/bot/setup?project="+url.QueryEscape(project.KitsuProjectID), r)) + `">` + esc(t(lang, "接続設定", "Configure connection")) + `</a></article>`
}

func replaceDashboardConnectedCount(body string, before, after int) string {
	return strings.Replace(body, `<div class="metric-value">`+strconv.Itoa(before)+`</div>`, `<div class="metric-value">`+strconv.Itoa(after)+`</div>`, 1)
}

func renderIAConnectedProductionDeleteResultRefined(lang string, r *http.Request, project model.Project, result connectedProductionChannelDeleteExecution) string {
	issues := len(result.Failed) + len(result.CleanupWarnings) + len(result.Skipped)
	if result.CategoryError != "" {
		issues++
	}
	if !result.CategoryDeleted {
		issues++
	}
	if result.ConnectionError != "" && !result.ConnectionDeleted {
		issues++
	}
	complete := result.ConnectionDeleted && result.CategoryDeleted && issues == 0
	label, class := t(lang, "削除完了", "Deletion complete"), "ok"
	if !complete {
		label, class = t(lang, "要確認", "Needs review"), "warn"
	}
	var body strings.Builder
	body.WriteString(`<section class="section-card glass delete-result-card" role="status" aria-live="polite"><div class="page-heading"><div><h1>` + esc(label) + `</h1><p class="hint">` + esc(t(lang, "Discord側のリソースとKitsuSyncの連携状態を削除しました。", "The Discord resources and KitsuSync connection state were removed.")) + `</p></div><span class="status-pill ` + class + `">` + esc(label) + `</span></div><dl class="status-list delete-result-summary"><div><dt>Production</dt><dd>` + esc(project.Name) + `</dd></div><div><dt>Discord category</dt><dd>` + strconv.Itoa(map[bool]int{true: 1, false: 0}[result.CategoryDeleted]) + `</dd></div><div><dt>Discord channels</dt><dd>` + strconv.Itoa(len(result.Deleted)) + `</dd></div><div><dt>Webhook</dt><dd>` + strconv.Itoa(result.DeletedWebhookCount) + `</dd></div></dl>`)
	if !complete {
		body.WriteString(`<section class="result-issues"><h2>` + esc(t(lang, "確認事項", "Follow-up items")) + `</h2><ul>`)
		for _, item := range result.Failed {
			body.WriteString(`<li>` + esc(item.Reason) + `</li>`)
		}
		body.WriteString(`</ul></section>`)
	}
	body.WriteString(`<div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(t(lang, "プロダクション一覧へ戻る", "Back to Productions")) + `</a></div></section>`)
	return adminPage(lang, "", r, body.String())
}
