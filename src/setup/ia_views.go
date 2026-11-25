package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"errors"
	"fmt"
	"math"
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
		active := r != nil && r.URL != nil && (r.URL.Path == item.path || (item.path == "/bot/admin/bot" && strings.HasPrefix(r.URL.Path, "/bot/admin/bot/")))
		className := "nav-chip"
		current := ""
		if active {
			className += " active"
			current = ` aria-current="page"`
		}
		b.WriteString(`<a class="` + className + `" href="` + esc(withLang(item.path, r)) + `"` + current + `>` + esc(tr(lang, item.key)) + `</a>`)
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
	projects, _ := availableProjectsWithError(db)
	return projects
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
		return "neutral", "Disconnected", "This Production is not connected to KitsuSync yet. Notifications are unavailable."
	}
	if project.ReadOnlyPreview {
		return "neutral", t(lang, "未接続", "Not connected"), t(lang, "このプロダクションはまだKitsuSyncに接続されていません。通知は利用できません。", "This Production is not connected to KitsuSync yet. Notifications are unavailable.")
	}
	if project.ValidationOnly {
		return "warning", t(lang, "検証専用", "Validation only"), t(lang, "実データの表示確認用です。Discordサーバーは未接続で、通知は利用できません。", "Read-only Kitsu data for validation. No Discord server is connected and notifications are unavailable.")
	}
	settings, hasForum, err := model.LoadProductionForumSettings(db, project.KitsuProjectID)
	if err != nil {
		return "bad", t(lang, "要確認", "Needs review"), t(lang, "フォーラム設定の読み込みに失敗しました。内容を確認してから修正してください。", "Forum settings could not be loaded. Review the details before changing them.")
	}
	if !hasForum || len(settings.Routes) == 0 {
		return "bad", t(lang, "未設定", "Incomplete"), t(lang, "フォーラム設定がありません。フォーラム設定でタスクタイプごとのフォーラムを設定してください。", "No forum settings found. Configure per-task-type forums in the forum settings.")
	}
	return "ok", t(lang, "有効", "Active"), t(lang, "フォーラム通知の設定が有効です。", "Forum notification settings are active.")
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
	return "warn", tr(lang, "status.action_required"), t(lang, "プロダクションの接続を設定すると通知を利用できます。", "Connect at least one Production before notifications can be used.")
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
		view.Label = t(lang, "利用不可", "Unavailable")
		view.Hint = t(lang, "通知可能なプロダクションがありません。", "No Production is currently available for notifications.")
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

func dashboardStatusRow(label, class, value string) string {
	return `<div class="dashboard-status-row"><span class="dashboard-status-label">` + esc(label) + `</span><span class="status-badge status-badge-` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(value) + `</span></div>`
}

func dashboardProblemAction(r *http.Request, p model.Project, lang, hint string) (string, string) {
	path := "/bot/admin/projects?project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications"
	label := t(lang, "通知設定を確認", "Review notification settings")
	lower := strings.ToLower(hint)
	if strings.Contains(lower, "participant") || strings.Contains(lower, "reviewer") || strings.Contains(lower, "checker") {
		path = "/bot/admin/projects?project=" + url.QueryEscape(p.KitsuProjectID) + "&tab=notifications#wfa-recipients"
		label = t(lang, "WFA通知先を確認", "Review WFA recipients")
	}
	return withLang(path, r), label
}

func renderIADashboardWithRuntime(w http.ResponseWriter, r *http.Request, db *gorm.DB, runtimeHealthy func() bool) {
	lang := currentLang(r)
	projects := availableProjects(db)
	productionCounts := productionConnectionCounts(projects)
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
		attentionRows.WriteString(`<li class="muted">` + esc(t(lang, "対応が必要なプロダクションはありません。", "No Productions need attention.")) + `</li>`)
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
	kitsuConnectionStatus, discordConnectionStatus := canonicalConnectionStatuses(lang, db, readiness, runtimeHealthy)
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
	statusExplanation := readinessHint
	statusAction := ""
	if !readiness.OverallReady {
		statusAction = `<div class="button-row"><a class="btn" href="` + esc(nextActionURL) + `">` + esc(nextActionLabel) + `</a></div>`
	}
	quickActions := ""
	if readiness.OverallReady {
		quickActions = `<a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(tr(lang, "ia.production_list")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(tr(lang, "ia.user_mapping")) + `</a>`
	}
	dashboardMenu := renderDashboardMenuRefined(lang, r, db, projects, attentionCount, readiness, runtimeHealthy)
	dashboardAction := ""
	if start := strings.Index(dashboardMenu, `<section class="dashboard-cta"`); start >= 0 {
		if end := strings.Index(dashboardMenu[start:], `</section>`); end >= 0 {
			end += start + len(`</section>`)
			dashboardAction = dashboardMenu[start:end]
			dashboardMenu = dashboardMenu[:start] + dashboardMenu[end:]
		}
	}
	body := `<div class="section-stack dashboard-page">` +
		`<section class="dashboard-intro"><div><h1>` + esc(tr(lang, "ia.dashboard")) + `</h1><p class="hint">` + esc(t(lang, "KitsuSyncの接続状態と、対応が必要な項目を確認できます。", "Review KitsuSync connection state and items that need attention.")) + `</p></div><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin", r)) + `">` + esc(t(lang, "状態を更新", "Refresh status")) + `</a></div></section>` +
		`<section class="dashboard-summary-grid" aria-label="` + esc(t(lang, "概要", "Summary")) + `">` + dashboardProductionMetric(lang, productionCounts) + `<div class="metric-card"><div class="metric-label">` + esc(t(lang, "対応が必要", "Needs attention")) + `</div><div class="metric-value">` + fmt.Sprint(attentionCount) + `</div><p class="field-help">` + esc(t(lang, "安全に通知できる状態かを確認してください。", "Review before notifications can be safely delivered.")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "直近24時間の通知失敗", "Notification failures, last 24 hours")) + `</div><div class="metric-value">` + fmt.Sprint(failureCount) + `</div><p class="field-help">` + esc(t(lang, "記録された失敗イベント", "Recorded failure events")) + `</p></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "通知状態", "Notification status")) + `</div><div class="metric-value"><span class="status-pill ` + readinessClass + `">` + esc(readinessLabel) + `</span></div><p class="field-help" role="status">` + esc(readinessHint) + `</p></div></section>` +
		`<section class="section-card glass dashboard-queue" aria-labelledby="dashboard-attention"><div class="page-heading"><div><h2 id="dashboard-attention">` + esc(t(lang, "対応が必要なプロダクション", "Productions needing attention")) + `</h2></div><span class="status-pill ` + map[bool]string{true: "bad", false: "ok"}[attentionCount > 0] + `">` + fmt.Sprint(attentionCount) + `</span></div><ul class="list-tight">` + attentionRows.String() + `</ul></section>` +
		dashboardAction +
		dashboardMenu +
		`<div class="dashboard-lower-grid"><section class="section-card glass" aria-labelledby="dashboard-activity"><h2 id="dashboard-activity">` + esc(tr(lang, "ia.activity")) + `</h2><div class="activity-columns" aria-hidden="true"><span>` + esc(t(lang, "日時", "Date and time")) + `</span><span>` + esc(t(lang, "操作", "Action")) + `</span><span>` + esc(t(lang, "プロダクション", "Production")) + `</span><span>` + esc(t(lang, "結果", "Result")) + `</span></div><ul class="activity-list" role="log">` + activityRows.String() + `</ul></section><div class="dashboard-side-stack"><section class="section-card glass" aria-labelledby="dashboard-system"><h2 id="dashboard-system">` + esc(t(lang, "通知システム", "Notification system")) + `</h2><div class="dashboard-status-list">` + dashboardStatusRow(t(lang, "Kitsu接続", "Kitsu connection"), kitsuConnectionStatus.Class, kitsuConnectionStatus.Label) + dashboardStatusRow(t(lang, "Discord Bot", "Discord Bot"), discordConnectionStatus.Class, discordConnectionStatus.Label) + dashboardStatusRow(t(lang, "通知状態", "Notification status"), readinessView.NotificationClass, readinessView.Notification) + `</div><p class="field-help" role="status">` + esc(statusExplanation) + `</p>` + statusAction + `</section><section class="section-card glass dashboard-quick" aria-labelledby="dashboard-quick"><h2 id="dashboard-quick">` + esc(t(lang, "クイック操作", "Quick actions")) + `</h2><div class="button-row">` + ifNonEmpty(quickActions, quickActions) + `</div></section></div></div>`
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
	return body[:start] + systemStatusRefreshScriptCanonical() + body[end:]
}

func systemStatusRefreshScriptCanonicalRaw() string {
	return `<script data-system-status-refresh>(function(){
var interval=5000,busy=false,root=document.querySelector(".system-status-sections"),live=document.querySelector("[data-system-live-label]");if(!root)return;
function text(ja,en){return document.documentElement.lang==="ja"?ja:en}
function localTime(value){var date=new Date(value);return isNaN(date.getTime())?"":[date.getHours(),date.getMinutes(),date.getSeconds()].map(function(n){return String(n).padStart(2,"0")}).join(":")}
function tick(value){return Math.max(0,Math.round(value))+"ms"}
function scale(items){var max=1,ceilings=[10,25,50,100,250,500,1000,2000];items.forEach(function(item){var value=Number(item.duration_ms);if(item.success&&isFinite(value)&&value>=0)max=Math.max(max,value)});for(var i=0;i<ceilings.length;i++)if(max<=ceilings[i])return ceilings[i];return Math.ceil(max/100)*100}
function label(item){return (localTime(item.at)+" "+(item.success?Number(item.duration_ms)+" ms "+text("正常","Healthy"):text("リクエスト失敗","Request failed"))).trim()}
function graph(items,asOf){
 var left=48,right=448,top=8,bottom=82,window=60000,start=asOf-window,upper=scale(items),all=[],visible=[],marks=[],runs=[],run=[],previous=null,previousAt=0;
 items.forEach(function(item){var at=Date.parse(item.at);if(isFinite(at)&&at<=asOf)all.push({item:item,at:at})});
 all.sort(function(a,b){return a.at-b.at});
 all.forEach(function(entry){if(entry.at<start)previous=entry;else visible.push(entry)});
 function yFor(entry){var value=Number(entry.item.duration_ms);return bottom-(bottom-top)*Math.min(1,value/upper)}
 if(previous&&visible.length&&previous.item.success&&visible[0].item.success&&visible[0].at-previous.at<=30000){
  var first=visible[0],previousY=yFor(previous),firstY=yFor(first),ratio=(start-previous.at)/(first.at-previous.at);
  run.push({x:left,y:previousY+(firstY-previousY)*ratio,at:start})
 }
 visible.forEach(function(entry){
  var item=entry.item;
  if(!item.success||!isFinite(Number(item.duration_ms))||Number(item.duration_ms)<0){
   if(run.length>1)runs.push(run);run=[];previousAt=entry.at;return
  }
  if(previousAt&&entry.at-previousAt>30000){if(run.length>1)runs.push(run);run=[]}
  var x=left+Math.max(0,Math.min(1,(entry.at-start)/window))*(right-left),y=yFor(entry),point={x:x,y:y,at:entry.at};
  run.push(point);
  marks.push("<circle class=\"telemetry-point success\" cx=\""+x.toFixed(1)+"\" cy=\""+y.toFixed(1)+"\" r=\"3\" data-telemetry-at=\""+item.at+"\" data-telemetry-duration=\""+Number(item.duration_ms)+"\" tabindex=\"0\" role=\"img\" aria-label=\""+label(item)+"\"><title>"+label(item)+"</title></circle>");
  previousAt=entry.at
 });
 if(run.length>1)runs.push(run);
 var paths=runs.map(function(points){return "<path class=\"telemetry-line success\" d=\"M"+points.map(function(p){return p.x.toFixed(1)+","+p.y.toFixed(1)}).join(" L")+"\"></path>"}).join(""),mid=upper/2,labels=[text("60秒","60s"),text("30秒","30s"),text("今","Now")];
 return "<svg class=\"api-sparkline\" viewBox=\"0 0 496 104\" role=\"img\" aria-label=\""+visible.length+" observations\"><line class=\"chart-guide\" x1=\"44\" y1=\"45\" x2=\"452\" y2=\"45\"></line><line class=\"chart-baseline\" x1=\"44\" y1=\"82\" x2=\"452\" y2=\"82\"></line><text class=\"chart-tick\" text-anchor=\"end\" x=\"40\" y=\"12\">"+tick(upper)+"</text><text class=\"chart-tick\" text-anchor=\"end\" x=\"40\" y=\"48\">"+tick(mid)+"</text><text class=\"chart-tick\" text-anchor=\"end\" x=\"40\" y=\"84\">0ms</text>"+paths+marks.join("")+"<text class=\"chart-time-label\" text-anchor=\"start\" x=\"44\" y=\"100\">"+labels[0]+"</text><text class=\"chart-time-label\" text-anchor=\"middle\" x=\"248\" y=\"100\">"+labels[1]+"</text><text class=\"chart-time-label\" text-anchor=\"end\" x=\"452\" y=\"100\">"+labels[2]+"</text></svg>"
}
function updateCard(service,items,asOf){
 var card=root.querySelector("[data-telemetry-card=\""+service+"\"]"),status=card&&card.querySelector("[data-telemetry-status]"),details=card&&card.querySelector("[data-telemetry-details]");if(!status||!details)return;
 if(!items.length){status.className="status-pill neutral";status.textContent=text("未確認","Not checked");details.innerHTML="<div class=\"api-observation-not-checked\">"+text("まだ計測データがありません","No measurements yet")+"</div>";return}
 var last=items[items.length-1],stale=asOf-Date.parse(last.at)>45000,value=last.success?Number(last.duration_ms)+" ms":text("リクエスト失敗","Request failed");
 status.className="status-pill "+(stale?"warning":last.success?"ok":"bad");
 status.textContent=stale?text("古いデータ","Stale"):last.success?text("正常","Healthy"):text("要確認","Needs review");
 details.innerHTML="<div class=\"api-observation-latency\"><div class=\"api-observation-primary\"><strong data-telemetry-value>"+value+"</strong></div><span class=\"api-observation-meta\" data-telemetry-meta>"+text("最終更新","Last updated")+" "+localTime(last.at)+"</span></div>"+graph(items,asOf)
}
function refresh(){
 if(busy)return;busy=true;
 fetch("/bot/api/setup/observability?window=60s",{headers:{"X-Requested-With":"system-status-refresh"},cache:"no-store"}).then(function(response){if(!response.ok)throw new Error("snapshot failed");return response.json()}).then(function(payload){
  var data=payload.observations||{},asOf=Date.parse(payload.generated_at);if(!isFinite(asOf))asOf=Date.now();
  updateCard("kitsu",data.kitsu||[],asOf);updateCard("discord",data.discord||[],asOf);if(live)live.textContent=text("自動更新","Auto-refresh")
 }).catch(function(){if(live)live.textContent=text("更新失敗","Refresh unavailable")}).finally(function(){busy=false})
}
window.setInterval(refresh,interval);refresh()
})();</script>`
}

func systemStatusRefreshScriptCanonical() string {
	return systemStatusRefreshScriptCanonicalRaw()
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

func dashboardAuditSummaryCanonical(lang string, db *gorm.DB) (string, string, string, string) {
	count := model.CountAuditLogs(db)
	healthLabel, healthClass := dashboardAuditHealthLabel(lang, model.RecentAuditHealth(db, time.Now()))
	if count == 0 {
		return t(lang, "記録なし", "No records"), healthLabel, "muted", healthClass
	}
	return fmt.Sprintf(t(lang, "%d件の記録", "%d records"), count), healthLabel, "ok", healthClass
}

func dashboardAuditHealthLabel(lang string, health model.AuditHealth) (string, string) {
	switch health {
	case model.AuditHealthAbnormal:
		return t(lang, "異常", "Abnormal"), "bad"
	case model.AuditHealthNeedsReview:
		return t(lang, "要確認", "Needs review"), "warn"
	case model.AuditHealthNormal:
		return t(lang, "正常", "Normal"), "ok"
	default:
		return t(lang, "記録なし", "No records"), "muted"
	}
}

func canonicalDashboardBadgeClass(class string) string {
	switch class {
	case "warning":
		return "warn"
	case "success":
		return "ok"
	case "danger", "blocked":
		return "bad"
	case "neutral":
		return "muted"
	default:
		return class
	}
}

func renderIAProductionList(w http.ResponseWriter, r *http.Request, db *gorm.DB, fallbackGuildID string, botTokens ...string) {
	lang := currentLang(r)
	botToken := ""
	if len(botTokens) > 0 {
		botToken = botTokens[0]
	}
	if selectedID := strings.TrimSpace(r.URL.Query().Get("project")); selectedID != "" {
		if p := model.FindProjectByKitsuID(db, selectedID); p != nil {
			renderIASelectedProduction(w, r, db, *p, fallbackGuildID, botToken)
			return
		}
		if p := liveProjectPreview(db, selectedID); p != nil {
			renderIASelectedProduction(w, r, db, *p, fallbackGuildID, botToken)
			return
		}
	}
	var rows strings.Builder
	projects := availableProjects(db)
	for _, p := range projects {
		class, label := productionConnectionStatus(p, lang)
		_, _, hint := iaStatus(db, p, lang)
		rows.WriteString(fmt.Sprintf(`<article class="section-card glass production-list-item"><div><h2>%s</h2><p class="field-help">%s</p></div><div class="production-list-state"><span class="status-pill %s">%s</span><span class="field-help">%s</span></div><a class="btn-ghost production-list-link" href="%s"><span class="production-list-open">%s</span></a></article>`, esc(p.Name), esc(t(lang, "現在の状態", "Current state")), class, esc(label), esc(hint), esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID), r)), esc(t(lang, "プロダクションを開く", "Open Production"))))
	}
	if rows.Len() == 0 {
		rows.WriteString(emptyState("-", t(lang, "プロダクションがありません", "No Productions"), t(lang, "新しいプロダクションを接続してください。", "Connect a new Production.")))
	}
	body := `<div class="section-stack"><p class="production-list-intro">` + esc(t(lang, "設定はプロダクションを選択した後に表示します", "Settings appear after you select a Production")) + `</p>` + productionListConnectionSummary(lang, productionConnectionCounts(projects)) + `<div class="production-list" aria-label="` + esc(t(lang, "プロダクション一覧", "Production list")) + `">` + rows.String() + `</div></div>`
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

func auditActorLabel(lang string, log model.AuditLog) string {
	switch log.ActorKind {
	case model.AuditActorHuman:
		if strings.TrimSpace(log.ActorName) != "" {
			return log.ActorName
		}
	case model.AuditActorSystem:
		return t(lang, "システム", "System")
	}
	return t(lang, "不明", "Unknown")
}

func renderIASelectedProduction(w http.ResponseWriter, r *http.Request, db *gorm.DB, p model.Project, fallbackGuildID string, botTokens ...string) {
	lang := currentLang(r)
	if p.ReadOnlyPreview {
		fmt.Fprint(w, adminPage(lang, "", r, renderIAUnconnectedProduction(r, p, lang)))
		return
	}
	requestedTab := strings.TrimSpace(r.URL.Query().Get("tab"))
	tab := selectedProductionTab(requestedTab)
	legacySection := legacyProductionSection(requestedTab)
	class, label, hint := iaStatus(db, p, lang)
	headerClass, headerLabel := "success", t(lang, "接続済み", "Connected")
	if p.ValidationOnly {
		headerClass, headerLabel = "warning", label
	}
	header := renderProductionContext(p, lang, r, tab, headerClass, headerLabel)
	// Storage feedback belongs to the selected-Production page, rather than the
	// Storage panel, so it is announced before the Production context and tabs.
	// The panel itself remains the single renderer for the Storage form.
	feedback := ""
	if tab == "settings" && r.URL.Query().Get("tab") == "storage-settings" && !p.ValidationOnly && !p.ReadOnlyPreview {
		feedback = renderStorageSettingsFeedback(r, lang)
	}
	body := feedback + `<div class="production-context">` + header + `<section id="panel-` + esc(tab) + `" role="tabpanel" aria-labelledby="tab-` + esc(tab) + `" tabindex="0" class="section-stack production-tabpanel">` + renderProductionPanelMarkup(db, r, p, lang, tab, class, label, hint, botTokens...) + `</section></div>`
	if legacySection != "" {
		fallback := ""
		if requestedTab == "activity" {
			fallback = "panel-overview"
		}
		body += `<script>(function(){var target=document.getElementById('` + esc(legacySection) + `')||document.getElementById('` + esc(fallback) + `');if(!target)return;target.scrollIntoView({block:'start'});var summary=target.querySelector('summary');if(summary)summary.focus({preventScroll:true});else target.focus({preventScroll:true});})();</script>`
	}
	body += `<script>(function(){var list=document.querySelector('[role="tablist"]');if(!list)return;var tabs=Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));list.addEventListener('keydown',function(e){var i=tabs.indexOf(document.activeElement);if(i<0)return;var n=i;if(e.key==='ArrowRight')n=(i+1)%tabs.length;if(e.key==='ArrowLeft')n=(i-1+tabs.length)%tabs.length;if(e.key==='Home')n=0;if(e.key==='End')n=tabs.length-1;if(n!==i){e.preventDefault();tabs[n].focus();tabs[n].click()}})})();</script>`
	fmt.Fprint(w, adminPage(lang, "", r, body))
}

func renderProductionContext(p model.Project, lang string, r *http.Request, tab, headerClass, headerLabel string) string {
	tabs := []struct{ id, key, label string }{{"overview", "ia.overview", ""}, {"notifications", "ia.notifications", ""}, {"team", "", "Team"}, {"settings", "", "Settings"}}
	var tabLinks strings.Builder
	for _, item := range tabs {
		selected := item.id == tab
		selectedAttr := "false"
		if selected {
			selectedAttr = "true"
		}
		link := withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab="+url.QueryEscape(item.id), r)
		label := item.label
		if item.key != "" {
			label = tr(lang, item.key)
		}
		if item.id == "team" {
			label = t(lang, "チーム", "Team")
		}
		if item.id == "settings" {
			label = t(lang, "設定", "Settings")
		}
		tabLinks.WriteString(`<a id="tab-` + esc(item.id) + `" role="tab" aria-selected="` + selectedAttr + `" aria-controls="panel-` + esc(item.id) + `" class="section-link` + map[bool]string{true: " active", false: ""}[selected] + `" href="` + esc(link) + `" tabindex="` + map[bool]string{true: "0", false: "-1"}[selected] + `">` + esc(label) + `</a>`)
	}
	return renderProductionIdentity(p, lang, headerClass, headerLabel) + `<nav class="section-nav production-tabs" role="tablist" aria-label="` + esc(t(lang, "プロダクションのセクション", "Production sections")) + `">` + tabLinks.String() + `</nav>`
}

func renderProductionIdentity(p model.Project, lang, headerClass, headerLabel string) string {
	return `<div class="production-identity"><div><div class="eyebrow">` + esc(t(lang, "プロダクション", "Production")) + `</div><h1>` + esc(p.Name) + `</h1></div><span class="status-pill ` + esc(headerClass) + `" role="status">` + esc(headerLabel) + `</span></div>`
}

func renderStorageSettingsFeedback(r *http.Request, lang string) string {
	if r == nil {
		return ""
	}
	switch {
	case r.URL.Query().Get("drive_saved") == "1":
		return `<div class="notice notice-success" role="status">` + esc(t(lang, "保存しました。", "Saved.")) + `</div>`
	case r.URL.Query().Get("drive_error") == "save":
		return `<div class="notice notice-error" role="alert">` + esc(t(lang, "Drive設定を保存できませんでした。プロダクションを確認して、もう一度お試しください。", "Drive settings could not be saved. Verify the Production and try again.")) + `</div>`
	case r.URL.Query().Get("drive_error") == "readback":
		return `<div class="notice notice-error" role="alert">` + esc(t(lang, "Drive設定の保存結果を確認できませんでした。もう一度お試しください。", "Drive settings were not confirmed after saving. Please try again.")) + `</div>`
	default:
		return ""
	}
}

func renderIAUnconnectedProduction(r *http.Request, p model.Project, lang string) string {
	configureURL := withLang("/bot/setup?project="+url.QueryEscape(p.KitsuProjectID), r)
	productionListURL := withLang("/bot/admin/projects", r)
	return `<div class="production-context production-unconfigured">` + renderProductionIdentity(p, lang, "neutral", tr(lang, "production.unconnected.status")) + `<section class="production-settings-section production-unconfigured-state production-detail-surface" aria-labelledby="unconnected-production-title"><div class="production-unconfigured-content"><h2 id="unconnected-production-title">` + esc(tr(lang, "production.unconnected.source")) + `</h2><p class="production-detail-state-row" role="status">` + esc(tr(lang, "production.unconnected.explanation")) + `</p></div><div class="button-row production-unconfigured-actions"><a class="btn" href="` + esc(configureURL) + `">` + esc(tr(lang, "production.unconnected.configure")) + `</a><a class="btn-ghost" href="` + esc(productionListURL) + `">` + esc(tr(lang, "production.unconnected.back")) + `</a></div></section></div>`
}

func selectedProductionTab(raw string) string {
	switch raw {
	case "notifications":
		return "notifications"
	case "team", "users", "user-settings":
		return "team"
	case "reviewers":
		return "notifications"
	case "storage-settings", "troubleshooting", "advanced", "danger-zone", "settings":
		return "settings"
	default:
		return "overview"
	}
}

func legacyProductionSection(raw string) string {
	switch raw {
	case "reviewers":
		return "wfa-recipients"
	case "storage-settings":
		return "storage"
	case "activity":
		return "recent-activity"
	case "troubleshooting":
		return "diagnostics"
	case "advanced":
		return "technical-details"
	case "danger-zone":
		return "danger-zone"
	default:
		return ""
	}
}

func renderProductionPanelMarkup(db *gorm.DB, r *http.Request, p model.Project, lang, tab, class, label, hint string, botTokens ...string) string {
	return renderSelectedProductionPanel(db, r, p, lang, tab, class, label, hint, botTokens...)
}

func renderSelectedProductionPanel(db *gorm.DB, r *http.Request, p model.Project, lang, tab, class, label, hint string, botTokens ...string) string {
	switch tab {
	case "notifications":
		if p.ReadOnlyPreview {
			return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.notifications")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "notification_state", "Notification state"), "blocked", t(lang, "unavailable", "Unavailable"), t(lang, "このプロダクションはKitsuSyncに接続されていないため、通知は利用できません。", "Notifications are unavailable because this Production is not connected to KitsuSync."), "") + `</dl></section>`
		}
		if p.ValidationOnly {
			return `<section class="section-card glass"><h2>` + esc(tr(lang, "ia.notifications")) + `</h2><dl class="status-list">` + statusSummaryRow(t(lang, "通知状態", "Notification state"), "blocked", t(lang, "利用できません", "Unavailable"), t(lang, "検証専用のプロダクションではDiscordサーバーが未接続のため、通知は利用できません。", "Notifications are unavailable because this validation-only Production has no Discord server connected."), "") + `</dl></section>`
		}
		notifications := renderSelectedProductionNotifications(db, r, p, lang, class, label, hint, botTokens...)
		// While the WFA recipients are being edited, only that section is shown.
		if r != nil && r.URL.Query().Get("edit_routing") == "1" {
			return notifications
		}
		return notifications + renderForumSettingsLink(db, r, p, lang) + renderHandoffSettingsLink(r, p, lang)
	case "team":
		return renderCurrentProductionTeam(db, r, p, lang, botTokens...)
	case "settings":
		return renderCurrentProductionSettings(db, r, p, lang)
	default:
		return renderCurrentProductionOverview(db, r, p, lang, class, label, hint)
	}
}

func renderCurrentProductionOverview(db *gorm.DB, r *http.Request, p model.Project, lang, statusClass, statusLabel, statusHint string) string {
	productionStateClass, productionState := "success", t(lang, "接続済み", "Connected")
	if p.ValidationOnly {
		productionStateClass, productionState = "warning", t(lang, "検証専用", "Validation only")
	}
	if p.ReadOnlyPreview {
		productionStateClass, productionState = "neutral", t(lang, "未接続", "Disconnected")
	}
	discordClass, discordState := "neutral", t(lang, "未接続", "Not connected")
	if strings.TrimSpace(p.DiscordGuildID) != "" {
		discordClass, discordState = "success", t(lang, "接続済み", "Connected")
	}
	if statusClass == "ok" {
		statusClass = "success"
	}
	if statusClass == "bad" || statusClass == "danger" {
		statusClass = "danger"
	}
	if strings.TrimSpace(statusLabel) == "" {
		statusLabel = t(lang, "未設定", "Not configured")
	}
	statusRows := statusSummaryRow(t(lang, "プロダクション接続", "Production connection"), productionStateClass, productionState, "", "") +
		statusSummaryRow(t(lang, "Discordリソース", "Discord resources"), discordClass, discordState, "", "") +
		statusSummaryRow(t(lang, "通知ルーティング", "Notification routing"), statusClass, statusLabel, statusHint, "")
	statusSection := `<section class="production-settings-section production-detail-surface production-overview-status"><h2 class="production-detail-section-title">` + esc(t(lang, "状態", "Status")) + `</h2><dl class="status-list production-status-list">` + statusRows + `</dl></section>`
	issues := make([]productionOverviewIssue, 0, 4)
	issueIndex := map[string]int{}
	addIssue := func(cause, tab string) {
		cause = strings.TrimSpace(cause)
		if cause == "" {
			return
		}
		if index, ok := issueIndex[cause]; ok {
			issues[index].count++
			return
		}
		issueIndex[cause] = len(issues)
		issues = append(issues, productionOverviewIssue{cause: cause, count: 1, tab: tab})
	}
	diagnoses := model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 100)
	for _, diagnosis := range diagnoses {
		detail := strings.TrimSpace(diagnosis.Detail)
		if detail == "" {
			detail = strings.TrimSpace(diagnosis.Reason)
		}
		addIssue(detail, "notifications")
	}
	if len(issues) == 0 && statusClass != "ok" && statusClass != "success" {
		cause := strings.TrimSpace(statusHint)
		if cause == "" {
			cause = strings.TrimSpace(statusLabel)
		}
		addIssue(cause, "notifications")
	}
	discordMissing := !p.ValidationOnly && strings.TrimSpace(p.DiscordGuildID) == ""
	if discordMissing {
		addIssue(t(lang, "Discordリソースが接続されていません。", "Discord resources are not connected."), "settings")
	}
	var issueContent strings.Builder
	if len(issues) > 0 {
		visible := len(issues)
		if visible > 3 {
			visible = 3
		}
		issueContent.WriteString(`<ul class="production-issue-list">`)
		for _, issue := range issues[:visible] {
			cause := issue.cause
			if issue.count > 1 {
				cause += t(lang, fmt.Sprintf("（%d件）", issue.count), fmt.Sprintf(" (%d occurrences)", issue.count))
			}
			issueContent.WriteString(`<li class="production-issue-row"><span>` + esc(cause) + `</span><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab="+url.QueryEscape(issue.tab), r)) + `">` + esc(overviewIssueActionLabel(lang, issue.tab)) + `</a></li>`)
		}
		if remaining := len(issues) - visible; remaining > 0 {
			issueContent.WriteString(`<li class="production-issue-more">` + esc(t(lang, fmt.Sprintf("他 %d 件", remaining), fmt.Sprintf("%d other issues", remaining))) + `</li>`)
		}
		issueContent.WriteString(`</ul>`)
	} else {
		issueContent.WriteString(`<div class="production-detail-state-row production-issue-empty" role="status">` + esc(t(lang, "現在の問題はありません。", "No current issues.")) + `</div>`)
	}
	issuesSection := `<section class="production-settings-section production-detail-surface production-current-issues"><h2 class="production-detail-section-title">` + esc(t(lang, "現在の問題", "Current issues")) + `</h2>` + issueContent.String() + `</section>`
	return `<div class="section-stack production-overview">` + statusSection + issuesSection + renderSelectedProductionActivity(db, p, lang, r) + `</div>`
}

type productionOverviewIssue struct {
	cause string
	count int
	tab   string
}

func overviewIssueActionLabel(lang, tab string) string {
	switch tab {
	case "team":
		return t(lang, "チームを確認", "Review Team")
	case "settings":
		return t(lang, "設定を確認", "Review Settings")
	default:
		return t(lang, "通知を確認", "Review notifications")
	}
}

func renderCurrentProductionSettings(db *gorm.DB, r *http.Request, p model.Project, lang string) string {
	requestedTab := ""
	if r != nil {
		requestedTab = r.URL.Query().Get("tab")
	}
	sections := renderCurrentProductionStorage(r, p, lang) +
		renderCurrentProductionDetails(p, lang, requestedTab == "advanced") +
		renderCurrentProductionTroubleshooting(db, p, lang, requestedTab == "troubleshooting") +
		strings.Replace(renderSelectedProductionDanger(r, p, lang, requestedTab == "danger-zone"), `value="preview_remove_connection_with_discord"`, `value="execute_current_ia_discord_delete"`, 1)
	sections = strings.ReplaceAll(sections, `class="production-settings-section production-settings-disclosure `, `class="production-settings-section production-settings-disclosure production-settings-disclosure-row `)
	return `<div class="production-settings-list">` + sections + `</div>`
}

func renderCurrentProductionStorage(r *http.Request, p model.Project, lang string) string {
	storageBody := `<p class="hint">` + esc(t(lang, "このプロダクションの保存先とリンクを管理します。", "Manage storage destinations and links for this Production.")) + `</p>`
	if p.ValidationOnly || p.ReadOnlyPreview {
		storageBody = `<p class="field-help" role="status">` + esc(t(lang, "検証専用のプロダクションではストレージ設定を変更できません。", "Storage settings are read-only for validation-only Productions.")) + `</p>`
	} else {
		storageBody += `<form method="POST" action="` + esc(withLang("/bot/admin/drive", r)) + `" class="form-stack drive-storage-form production-storage-form"><input type="hidden" name="kitsu_project_id" value="` + esc(p.KitsuProjectID) + `"><label class="production-storage-field" for="storage-url">` + esc(t(lang, "保存先リンク", "Storage link")) + `</label><div class="production-storage-control-row"><input id="storage-url" type="url" name="storage_url" value="` + esc(p.StorageURL) + `"><div class="button-row production-storage-actions"><button class="btn" type="submit" data-drive-save disabled>` + esc(t(lang, "保存", "Save")) + `</button></div></div></form><script>(function(){var form=document.querySelector('.drive-storage-form'),input=form&&form.querySelector('#storage-url'),button=form&&form.querySelector('[data-drive-save]');if(!form||!input||!button)return;var original=input.value;var sync=function(){button.disabled=input.value===original};input.addEventListener('input',sync);input.addEventListener('change',sync);sync();form.addEventListener('submit',function(){button.disabled=true;button.setAttribute('aria-busy','true');button.textContent='` + esc(t(lang, "保存中...", "Saving...")) + `';});}());</script>`
	}
	return `<section id="storage" tabindex="-1" class="production-settings-section production-detail-surface"><h2 class="production-detail-section-title">` + esc(t(lang, "ストレージ", "Storage")) + `</h2>` + storageBody + `</section>`
}

func renderCurrentProductionDetails(p model.Project, lang string, expanded bool) string {
	validation := ""
	if p.ValidationOnly || p.ReadOnlyPreview {
		validation = `<dt>` + esc(t(lang, "検証モード", "Validation mode")) + `</dt><dd>` + esc(t(lang, "検証専用・変更不可", "Validation only; changes disabled")) + `</dd>`
	}
	open := ""
	if expanded {
		open = ` open`
	}
	var rows strings.Builder
	rows.WriteString(validation)
	for _, row := range []struct{ label, value string }{
		{t(lang, "プロダクションID", "Production ID"), p.KitsuProjectID},
		{t(lang, "DiscordサーバーID", "Discord server ID"), p.DiscordGuildID},
		{t(lang, "カテゴリID", "Category ID"), p.DiscordCategoryID},
	} {
		if strings.TrimSpace(row.value) == "" {
			rows.WriteString(`<dt>` + esc(row.label) + `</dt><dd class="production-technical-id"><span class="field-help">` + esc(t(lang, "未設定", "Not set")) + `</span></dd>`)
			continue
		}
		copyLabel := t(lang, row.label+"をコピー", "Copy "+row.label)
		copiedLabel := t(lang, "コピーしました", "Copied")
		failedLabel := t(lang, "コピーできませんでした", "Copy unavailable")
		rows.WriteString(`<dt>` + esc(row.label) + `</dt><dd class="production-technical-id"><code>` + esc(row.value) + `</code><button type="button" class="btn-ghost production-copy-id" data-copy-value="` + esc(row.value) + `" data-copy-label="` + esc(copyLabel) + `" data-copied-label="` + esc(copiedLabel) + `" data-copy-failed-label="` + esc(failedLabel) + `" aria-label="` + esc(copyLabel) + `">` + esc(t(lang, "コピー", "Copy")) + `</button></dd>`)
	}
	return `<details id="technical-details" class="production-settings-section production-settings-disclosure advanced-details"` + open + `><summary>` + esc(t(lang, "技術情報", "Technical details")) + `</summary><dl class="detail-list production-technical-details">` + rows.String() + `</dl><script>(function(){var buttons=document.querySelectorAll('.production-copy-id');buttons.forEach(function(button){button.addEventListener('click',async function(){var value=button.getAttribute('data-copy-value')||'';var copied=false;try{if(navigator.clipboard&&navigator.clipboard.writeText){await navigator.clipboard.writeText(value);copied=true}else{var field=document.createElement('textarea');field.value=value;field.setAttribute('readonly','');field.style.position='fixed';field.style.opacity='0';document.body.appendChild(field);field.select();try{copied=document.execCommand('copy')}finally{field.remove()}}}catch(_){}var label=copied?button.getAttribute('data-copied-label'):button.getAttribute('data-copy-failed-label');button.textContent=label;button.setAttribute('aria-label',label)})})})();</script></details>`
}

var reviewerProductionTeamReader = func(db *gorm.DB, projectID string) ([]kitsu.Person, error) {
	baseURL, token, ok := runtimeKitsuDataSource(db)
	if !ok {
		return nil, errors.New("Kitsu runtime connection is not configured")
	}
	return kitsu.GetProjectTeamWithCredentialsAndError(baseURL, token, projectID)
}

func renderCurrentProductionTeam(db *gorm.DB, r *http.Request, p model.Project, lang string, botTokens ...string) string {
	var team []kitsu.Person
	var teamErr error
	if p.ValidationOnly || p.ReadOnlyPreview {
		for _, person := range p.ValidationData().Participants {
			team = append(team, kitsu.Person{ID: person.ID, FullName: person.FullName, Email: person.Email})
		}
	} else {
		team, teamErr = reviewerProductionTeamReader(db, p.KitsuProjectID)
	}
	taskTypes := reviewerTaskTypesForProduction(db, p.KitsuProjectID)
	globalUsers := filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db))
	botToken := ""
	if len(botTokens) > 0 {
		botToken = botTokens[0]
	}
	var guildMembers []DiscordGuildMember
	var teamGuildErr error
	guildReadOK := false
	guildNotice := ""
	guildMembersPending := false
	if botToken != "" && isDiscordSnowflake(p.DiscordGuildID) {
		guildView := guildMemberListCache.view(p.DiscordGuildID, botToken)
		guildNotice = guildDirectoryNotice(lang, guildView)
		var status guildDirectoryStatus
		guildMembers, status, teamGuildErr = cachedGuildMembers(p.DiscordGuildID, botToken)
		guildMembersPending = status == guildDirectoryPending
		guildReadOK = teamGuildErr == nil && !guildMembersPending
	}
	guildMemberNames := make(map[string]string, len(guildMembers))
	guildMemberIDs := make(map[string]bool, len(guildMembers))
	var selectableMembers []DiscordGuildMember
	for _, member := range guildMembers {
		if id := strings.TrimSpace(member.User.ID); isDiscordSnowflake(id) {
			if !member.User.Bot {
				guildMemberIDs[id] = true
				guildMemberNames[id] = discordGuildMemberDisplayName(member)
				selectableMembers = append(selectableMembers, member)
			}
		}
	}
	editable := !p.ValidationOnly && !p.ReadOnlyPreview && teamErr == nil && guildReadOK && len(selectableMembers) > 0
	supervisorSummaries := productionSupervisorTaskTypeSummaries(team, taskTypes)
	departmentNames := productionDepartmentNames(taskTypes)

	userText := func(ja, en string) string { return t(lang, ja, en) }
	memberSelect := func(person kitsu.Person, currentID string) string {
		var b strings.Builder
		b.WriteString(`<input type="hidden" name="team_kitsu_id" value="` + esc(strings.TrimSpace(person.ID)) + `">`)
		b.WriteString(`<select name="team_discord_user_id" aria-label="` + esc(kitsuPersonDisplayName(person)+" - Discord") + `">`)
		b.WriteString(`<option value="">` + esc(userText("未設定（紐づけ解除）", "Not linked")) + `</option>`)
		if currentID != "" && !guildMemberIDs[currentID] {
			b.WriteString(`<option value="` + esc(currentID) + `" selected disabled>` + esc(userText("このサーバーで見つかりません", "Unavailable in this server")) + `</option>`)
		}
		for _, member := range selectableMembers {
			id := strings.TrimSpace(member.User.ID)
			selected := ""
			if id == currentID {
				selected = " selected"
			}
			b.WriteString(`<option value="` + esc(id) + `"` + selected + `>` + esc(discordGuildMemberDisplayName(member)) + `</option>`)
		}
		b.WriteString(`</select>`)
		return b.String()
	}
	var members strings.Builder
	if teamErr != nil {
		members.WriteString(`<tr><td colspan="5"><div class="empty-state" role="status"><strong>` + esc(userText("Kitsuのプロダクションチームを読み込めませんでした", "Could not load the Kitsu Production Team")) + `</strong><span class="field-help">` + esc(userText("Kitsu接続を確認してから再読み込みしてください。", "Check the Kitsu connection and reload this page.")) + `</span><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a></div></div></td></tr>`)
	} else {
		botEmail := botAccountEmail(db)
		for _, person := range team {
			if person.IsBot || strings.TrimSpace(person.ID) == "" || (botEmail != "" && strings.EqualFold(strings.TrimSpace(person.Email), botEmail)) {
				continue
			}
			role := strings.TrimSpace(kitsu.EffectiveProductionRole(person))
			if role == "" {
				role = userText("不明", "Unknown")
			} else {
				role = strings.ToUpper(role[:1]) + role[1:]
			}
			departments := productionPersonDepartmentSummary(person, departmentNames)
			if departments == "" {
				departments = userText("—", "—")
			}
			scope := userText("—", "—")
			if strings.EqualFold(role, "Supervisor") {
				if summary := supervisorSummaries[strings.TrimSpace(person.ID)]; summary != "" {
					scope = summary
				}
			}
			discordCell := `<span class="field-help">—</span>`
			statusCell := `<span class="status-pill warning" role="status">` + esc(userText("要確認", "Needs review")) + `</span>`
			currentDiscordID := ""
			if guildMembersPending {
				discordCell += `<span class="field-help">` + esc(userText("Discord所属を取得中", "Guild membership is loading")) + `</span>`
			} else if !guildReadOK {
				discordCell += `<span class="field-help">` + esc(userText("Discord所属を確認できません", "Guild membership could not be verified")) + `</span> <a class="field-help" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a>`
			}
			user := globalUserForKitsuPerson(globalUsers, person)
			if user != nil && isDiscordSnowflake(user.DiscordID) {
				currentDiscordID = strings.TrimSpace(user.DiscordID)
				if !guildReadOK || !guildMemberIDs[currentDiscordID] {
					discordCell = `<span class="field-help">` + esc(userText("Discord所属が要確認", "Discord membership needs review")) + `</span>`
				} else {
					discordName := guildMemberNames[currentDiscordID]
					if discordName == "" {
						discordName = discordReviewerDisplayName(user.DiscordDisplayName)
					}
					if discordName == "" {
						discordName = userText("リンク済み", "Linked")
					}
					discordCell = `<span class="production-team-discord-name" title="` + esc(discordName) + `">` + esc(discordName) + `</span>`
					statusCell = `<span class="status-pill success" role="status">` + esc(userText("リンク済み", "Linked")) + `</span>`
				}
			}
			if editable {
				discordCell = memberSelect(person, currentDiscordID)
				if currentDiscordID != "" {
					statusCell += ` <button type="submit" name="unlink_team_kitsu_id" value="` + esc(strings.TrimSpace(person.ID)) + `" class="btn-ghost sm">` + esc(userText("解除", "Unlink")) + `</button>`
				}
			}
			scopeMarkup := ""
			if strings.EqualFold(role, "Supervisor") && scope != userText("—", "—") {
				scopeMarkup = `<small class="production-team-supervisor-scope" title="` + esc(scope) + `"><span class="production-team-label">` + esc(userText("Supervisor範囲", "Supervision scope")) + `</span> ` + esc(scope) + `</small>`
			}
			members.WriteString(`<tr class="production-team-row"><th scope="row" class="production-team-cell production-team-member"><strong title="` + esc(kitsuPersonDisplayName(person)) + `">` + esc(kitsuPersonDisplayName(person)) + `</strong></th><td class="production-team-cell" data-label="` + esc(userText("Kitsuロール", "Kitsu role")) + `">` + esc(role) + `</td><td class="production-team-cell" data-label="Department"><span>` + esc(departments) + `</span>` + scopeMarkup + `</td><td class="production-team-cell production-team-discord" data-label="Discord">` + discordCell + `</td><td class="production-team-cell production-team-status" data-label="` + esc(userText("状態", "Status")) + `">` + statusCell + `</td></tr>`)
		}
		if members.Len() == 0 {
			members.WriteString(`<tr><td colspan="5"><div class="empty-state" role="status"><strong>` + esc(userText("Kitsuのプロダクションチームは空です", "The Kitsu Production Team is empty")) + `</strong></div></td></tr>`)
		}
	}
	tableHTML := `<div class="table-wrap production-team-table-wrap"><table class="production-team-table"><colgroup><col class="production-team-col-member"><col class="production-team-col-role"><col class="production-team-col-department"><col class="production-team-col-discord"><col class="production-team-col-status"></colgroup><thead><tr><th scope="col">` + esc(userText("メンバー", "Member")) + `</th><th scope="col">` + esc(userText("Kitsuロール", "Kitsu role")) + `</th><th scope="col">Department</th><th scope="col">Discord</th><th scope="col">` + esc(userText("状態", "Status")) + `</th></tr></thead><tbody>` + members.String() + `</tbody></table></div>`
	if editable && members.Len() > 0 {
		tableHTML = `<form method="POST" action="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=team", r)) + `" data-team-link-form>` +
			`<input type="hidden" name="action" value="save_team_links">` +
			`<input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `">` +
			`<input type="hidden" name="discord_guild_id" value="` + esc(p.DiscordGuildID) + `">` +
			tableHTML +
			`<div class="button-row"><button type="submit" class="btn">` + esc(userText("変更を保存", "Save changes")) + `</button></div></form>`
	}
	return `<section id="production-team" class="production-team-page"><h2>` + esc(userText("チーム", "Team")) + `</h2><p class="field-help">` + esc(userText("メンバーとロールはKitsuから読み取り専用で表示します。Supervisor範囲はDepartmentとProductionのTask Typeから導出され、実際のタスク割り当てを示すものではありません。この場でDiscordユーザーとの紐づけや解除を保存できます。", "Members and roles are read-only from Kitsu. Supervisor scope is derived from Departments and this Production's Task Types; it does not indicate actual task assignments. You can link or unlink Discord users here.")) + `</p>` + guildNoticeMarkup(guildNotice) + tableHTML + `</section>`
}

func productionDepartmentNames(taskTypes []kitsu.TaskType) map[string]string {
	names := make(map[string]string)
	for _, taskType := range taskTypes {
		id, name := strings.TrimSpace(taskType.DepartmentID), strings.TrimSpace(taskType.DepartmentName)
		if id != "" && name != "" && names[id] == "" {
			names[id] = name
		}
	}
	return names
}

func productionPersonDepartmentSummary(person kitsu.Person, departmentNames map[string]string) string {
	seen := make(map[string]struct{})
	var names []string
	for _, id := range person.Departments {
		name := strings.TrimSpace(departmentNames[strings.TrimSpace(id)])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// reviewerGuildMembersForGuild and reviewerDiscordRolesForGuild are the live
// readers. Writes, delivery and the directory cache's fetch go through them, so
// a test that replaces the seam also feeds the cache. They are bound in init so
// the cache and the readers do not form an initialization cycle.
var reviewerGuildMembersForGuild func(string, string) ([]DiscordGuildMember, error)

func productionSupervisorTaskTypeSummaries(team []kitsu.Person, taskTypes []kitsu.TaskType) map[string]string {
	type summary struct {
		department string
		tasks      []string
		seen       map[string]struct{}
	}
	byDepartment := map[string]summary{}
	for _, taskType := range taskTypes {
		departmentID := strings.TrimSpace(taskType.DepartmentID)
		departmentName := strings.TrimSpace(taskType.DepartmentName)
		taskName := strings.TrimSpace(taskType.Name)
		if departmentID == "" || departmentName == "" || taskName == "" {
			continue
		}
		entry := byDepartment[departmentID]
		if entry.department == "" {
			entry.department = departmentName
			entry.seen = map[string]struct{}{}
		}
		if _, ok := entry.seen[taskName]; !ok {
			entry.tasks = append(entry.tasks, taskName)
			entry.seen[taskName] = struct{}{}
		}
		byDepartment[departmentID] = entry
	}
	result := map[string]string{}
	for _, person := range team {
		if kitsu.EffectiveProductionRole(person) != "supervisor" || !person.Active || person.Archived || person.IsBot || strings.TrimSpace(person.ID) == "" {
			continue
		}
		departmentIDs := append([]string(nil), person.Departments...)
		for i := range departmentIDs {
			departmentIDs[i] = strings.TrimSpace(departmentIDs[i])
		}
		sort.Strings(departmentIDs)
		var departments []string
		lastDepartmentID := ""
		for _, departmentID := range departmentIDs {
			if departmentID == "" || departmentID == lastDepartmentID {
				continue
			}
			lastDepartmentID = departmentID
			if entry, ok := byDepartment[departmentID]; ok {
				sort.Strings(entry.tasks)
				department := entry.department
				if len(entry.tasks) > 0 {
					department += ": " + strings.Join(entry.tasks, ", ")
				}
				departments = append(departments, department)
			}
		}
		if len(departments) > 0 {
			result[strings.TrimSpace(person.ID)] = strings.Join(departments, " / ")
		}
	}
	return result
}

func globalUserForKitsuPerson(users []model.UserMap, person kitsu.Person) *model.UserMap {
	if id := strings.TrimSpace(person.ID); id != "" {
		for i := range users {
			if strings.TrimSpace(users[i].KitsuID) == id {
				return &users[i]
			}
		}
	}
	// Email/name fallback is retained only for legacy rows without a stable
	// Kitsu Person ID. A row tied to a different Person must never inherit its
	// Discord identity through reused names or email addresses.
	legacyMatch := func(matches func(model.UserMap) bool) (*model.UserMap, bool) {
		var candidate *model.UserMap
		matched := false
		for i := range users {
			if !matches(users[i]) {
				continue
			}
			matched = true
			if strings.TrimSpace(users[i].KitsuID) != "" {
				// Even if an identityless row also matches, a stable conflicting
				// identity makes this fallback ambiguous and unsafe.
				return nil, true
			}
			if candidate == nil {
				candidate = &users[i]
				continue
			}
			if strings.TrimSpace(candidate.DiscordID) != strings.TrimSpace(users[i].DiscordID) {
				return nil, true
			}
		}
		return candidate, matched
	}
	if email := strings.ToLower(strings.TrimSpace(person.Email)); email != "" {
		if user, matched := legacyMatch(func(user model.UserMap) bool {
			return strings.ToLower(strings.TrimSpace(user.KitsuEmail)) == email
		}); matched {
			return user
		}
	}
	if name := strings.ToLower(kitsuPersonDisplayName(person)); name != "" {
		user, _ := legacyMatch(func(user model.UserMap) bool {
			return strings.ToLower(strings.TrimSpace(user.KitsuName)) == name
		})
		return user
	}
	return nil
}

func kitsuPersonDisplayName(person kitsu.Person) string {
	if name := strings.TrimSpace(person.FullName); name != "" {
		return name
	}
	return strings.TrimSpace(strings.TrimSpace(person.FirstName) + " " + strings.TrimSpace(person.LastName))
}

func discordReviewerDisplayName(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "@") {
		return "@" + value
	}
	return value
}

func discordGuildMemberDisplayName(member DiscordGuildMember) string {
	for _, name := range []string{member.Nick, member.User.GlobalName, member.User.DisplayName, member.User.Username} {
		if name = strings.TrimSpace(name); name != "" {
			return discordReviewerDisplayName(name)
		}
	}
	return ""
}

var reviewerTaskTypesForProduction = setupKitsuTaskTypes
var reviewerDiscordRolesForGuild func(string, string) ([]DiscordGuildRole, error)
var reviewerDepartmentSupervisorsForTeam = kitsu.GetDepartmentSupervisorsForProductionTeamWithCredentials

func currentProductionLinkedHumanDiscordIDs(team []kitsu.Person, globalUsers []model.UserMap, guildMembers []DiscordGuildMember) map[string]string {
	linked := map[string]string{}
	memberNames := make(map[string]string, len(guildMembers))
	for _, member := range guildMembers {
		if !member.User.Bot {
			memberNames[strings.TrimSpace(member.User.ID)] = discordGuildMemberDisplayName(member)
		}
	}
	for _, person := range team {
		if !person.Active || person.Archived || person.IsBot || strings.TrimSpace(person.ID) == "" {
			continue
		}
		if user := globalUserForKitsuPerson(globalUsers, person); user != nil && isDiscordSnowflake(user.DiscordID) {
			id := strings.TrimSpace(user.DiscordID)
			name := memberNames[id]
			if name == "" {
				continue
			}
			linked[id] = name
		}
	}
	return linked
}

func handleCurrentProductionUserMutation(w http.ResponseWriter, r *http.Request, db *gorm.DB, botTokens ...string) bool {
	if r.Method != http.MethodPost {
		return false
	}
	action := strings.TrimSpace(r.FormValue("action"))
	targetAction := action == "add_production_reviewer_target" || action == "remove_production_reviewer_target" || action == "reset_production_reviewers" || action == "remove_legacy_production_reviewer"
	if !targetAction {
		return false
	}
	project := model.FindProjectByKitsuID(db, strings.TrimSpace(r.FormValue("project_id")))
	if project == nil || model.IsValidationOnlyProject(db, project.KitsuProjectID) {
		http.Error(w, "Production is not writable", http.StatusForbidden)
		return true
	}
	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	defer unlockApply()
	project = model.FindProjectByKitsuID(db, project.KitsuProjectID)
	if project == nil || model.IsValidationOnlyProject(db, project.KitsuProjectID) {
		http.Error(w, "Production is not writable", http.StatusForbidden)
		return true
	}
	if targetAction {
		writeReviewerMutationError := func() {
			http.Error(w, "Invalid Production Reviewer target", http.StatusBadRequest)
		}
		taskTypeID := strings.TrimSpace(r.FormValue("task_type_id"))
		var taskType *kitsu.TaskType
		for _, candidate := range reviewerTaskTypesForProduction(db, project.KitsuProjectID) {
			if strings.TrimSpace(candidate.ID) == taskTypeID && taskTypeID != "" {
				copy := candidate
				taskType = &copy
				break
			}
		}
		if taskType == nil {
			writeReviewerMutationError()
			return true
		}
		var mutationErr error
		switch action {
		case "add_production_reviewer_target":
			kind := strings.TrimSpace(r.FormValue("target_kind"))
			id := strings.TrimSpace(r.FormValue("target_id"))
			if !isDiscordSnowflake(id) {
				writeReviewerMutationError()
				return true
			}
			switch kind {
			case model.ReviewerTargetUser:
				team, teamErr := reviewerProductionTeamReader(db, project.KitsuProjectID)
				if teamErr != nil {
					writeReviewerMutationError()
					return true
				}
				botToken := ""
				if len(botTokens) > 0 {
					botToken = botTokens[0]
				}
				// ListGuildMembers refreshes the directory cache, so this write
				// path still reads live members.
				guildMembers, membersErr := reviewerGuildMembersForGuild(project.DiscordGuildID, botToken)
				if membersErr != nil {
					writeReviewerMutationError()
					return true
				}
				globalUsers := filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db))
				if _, linked := currentProductionLinkedHumanDiscordIDs(team, globalUsers, guildMembers)[id]; !linked {
					writeReviewerMutationError()
					return true
				}
			case model.ReviewerTargetRole:
				botToken := ""
				if len(botTokens) > 0 {
					botToken = botTokens[0]
				}
				eligibleRoles, err := ForumReviewerRoles(db, project.KitsuProjectID, taskType.ID, botToken)
				if err != nil {
					writeReviewerMutationError()
					return true
				}
				if _, eligible := eligibleRoles[id]; !eligible {
					writeReviewerMutationError()
					return true
				}
			default:
				writeReviewerMutationError()
				return true
			}
			mutationErr = model.UpsertProjectReviewerTarget(db, project.ID, taskType.ID, taskType.Name, kind, id)
		case "remove_production_reviewer_target":
			targetID := parseUint(r.FormValue("target_id"))
			targets := model.ListProjectReviewerTargets(db, project.ID)
			found := false
			for _, target := range targets {
				if target.ID == targetID && target.TaskTypeID == taskType.ID {
					found = true
					break
				}
			}
			if !found {
				writeReviewerMutationError()
				return true
			}
			mutationErr = model.DeleteProjectReviewerTarget(db, project.ID, targetID)
		case "reset_production_reviewers":
			mutationErr = model.DeleteProjectReviewerTargetsForTaskType(db, project.ID, taskType.ID)
		case "remove_legacy_production_reviewer":
			for _, legacy := range model.ListProjectCheckerMaps(db, project.ID) {
				if legacy.TaskTypeID == taskType.ID || (legacy.TaskTypeID == "" && legacy.TaskType == taskType.Name) {
					mutationErr = db.Delete(&model.ProjectCheckerMap{}, legacy.ID).Error
					break
				}
			}
		}
		if mutationErr != nil {
			http.Error(w, "Production Reviewer target could not be saved", http.StatusInternalServerError)
			return true
		}
		target := withLang("/bot/admin/projects?project="+url.QueryEscape(project.KitsuProjectID)+"&tab=notifications&reviewer_task_type="+url.QueryEscape(taskType.ID)+"&msg=saved#wfa-recipients", r)
		http.Redirect(w, r, target, http.StatusSeeOther)
		return true
	}
	return false
}

func renderSelectedProductionNotifications(db *gorm.DB, r *http.Request, p model.Project, lang, class, label, hint string, botTokens ...string) string {
	statusLabel := t(lang, "未設定", "Not configured")
	switch class {
	case "success", "ok":
		statusLabel = t(lang, "正常", "Healthy")
	case "warning", "danger", "bad":
		statusLabel = t(lang, "要確認", "Needs review")
	case "blocked":
		statusLabel = t(lang, "利用不可", "Unavailable")
	}
	editing := r != nil && r.URL.Query().Get("edit_routing") == "1"
	forumSettings, _, _ := model.LoadProductionForumSettings(db, p.KitsuProjectID)
	routeCount := len(forumSettings.Routes)
	data := loadProductionNotificationReviewerView(db, p, routeCount > 0 || editing, routeCount > 0 || editing, botTokens...)
	if class == "success" || class == "ok" {
		if guildDirectoryDegraded(p.DiscordGuildID, data.BotToken) {
			class, statusLabel = "warning", t(lang, "要確認", "Needs review")
		}
	}
	var routing string
	if editing {
		routing = renderCurrentIARoutingEditorWithStatus(db, r, p, lang, productionDirectoryNoticeMarkup(lang, p, data), data, class, statusLabel)
	} else {
		routing = renderProductionNotificationsReadTableWithData(db, r, p, lang, class, statusLabel, data)
	}
	return `<div class="production-notifications">` + routing + `</div>`
}

type productionNotificationReviewerView struct {
	TaskTypes                  []kitsu.TaskType
	Team                       []kitsu.Person
	TeamErr                    error
	Users                      []model.UserMap
	BotToken                   string
	GuildUsers                 []DiscordGuildMember
	GuildErr                   error
	Roles                      []DiscordGuildRole
	RoleCatalog                []DiscordGuildRole
	RolesErr                   error
	RolesReady                 bool
	RolesPending               bool
	GuildPending               bool
	RoleSendEvidenceByTaskType map[string]reviewerRoleSendEvidence
	RoleSendErrorsByTaskType   map[string]error
	RoleSendersByTaskType      map[string]reviewerRoleSender
	RoleSendEvidenceRequested  bool
	// AutomaticReviewersByTaskType is filled by the loader so rendering never
	// re-queries Kitsu per row.
	AutomaticReviewersByTaskType map[string][]string
	AutomaticStatesByTaskType    map[string]string
	KitsuBase                    string
	KitsuToken                   string
	KitsuReady                   bool
}

func loadProductionNotificationReviewerView(db *gorm.DB, p model.Project, includeAutomatic, includeRoleSendEvidence bool, botTokens ...string) productionNotificationReviewerView {
	view := productionNotificationReviewerView{
		TaskTypes: reviewerTaskTypesForProduction(db, p.KitsuProjectID),
		Users:     filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db)),
	}
	if len(botTokens) > 0 {
		view.BotToken = strings.TrimSpace(botTokens[0])
	}
	if stored := storedRuntimeDiscordBotToken(db); stored != "" {
		view.BotToken = stored
	}
	view.RoleSendEvidenceRequested = false
	if includeAutomatic {
		view.Team, view.TeamErr = reviewerProductionTeamReader(db, p.KitsuProjectID)
		if view.BotToken != "" && isDiscordSnowflake(p.DiscordGuildID) {
			// Display only reads cached snapshots: member and role lists are
			// refreshed in the background, and per-route send access is
			// verified when a target is applied, not while rendering.
			var memberStatus guildDirectoryStatus
			view.GuildUsers, memberStatus, view.GuildErr = cachedGuildMembers(p.DiscordGuildID, view.BotToken)
			view.GuildPending = memberStatus == guildDirectoryPending
			var roleStatus guildDirectoryStatus
			view.Roles, roleStatus, view.RolesErr = cachedGuildRoles(p.DiscordGuildID, view.BotToken)
			view.RolesPending = roleStatus == guildDirectoryPending
			if view.RolesErr == nil && !view.RolesPending {
				view.RoleCatalog = reviewerRoleCatalog(p.DiscordGuildID, view.Roles)
				view.Roles = mentionableReviewerRoles(p.DiscordGuildID, view.RoleCatalog)
				view.RolesReady = true
			}
			forumSettings, _, _ := model.LoadProductionForumSettings(db, p.KitsuProjectID)
			view.RoleSendersByTaskType = make(map[string]reviewerRoleSender, len(forumSettings.Routes))
			for _, route := range forumSettings.Routes {
				view.RoleSendersByTaskType[route.TaskTypeID] = reviewerRoleSender{ChannelID: route.ForumChannelID}
			}
			// Resolve department supervisors from the cached snapshot so the
			// read table is complete on the first render, while the directory
			// is still being fetched in the background.
			if !view.GuildPending && view.TeamErr == nil && view.KitsuReady {
				view.AutomaticReviewersByTaskType = make(map[string][]string, len(view.TaskTypes))
				view.AutomaticStatesByTaskType = make(map[string]string, len(view.TaskTypes))
				for _, taskType := range view.TaskTypes {
					view.AutomaticReviewersByTaskType[taskType.ID], view.AutomaticStatesByTaskType[taskType.ID] = productionAutomaticReviewerRecipients(taskType, view)
				}
			}
		}
		view.KitsuBase, view.KitsuToken, view.KitsuReady = runtimeKitsuDataSource(db)
	}
	return view
}

func renderProductionNotificationsReadTableWithData(db *gorm.DB, r *http.Request, p model.Project, lang, class, statusLabel string, data productionNotificationReviewerView) string {
	label := func(ja, en string) string { return t(lang, ja, en) }
	channelLabel := label("フォーラム", "Forum")
	forumSettings, _, _ := model.LoadProductionForumSettings(db, p.KitsuProjectID)
	routes := forumSettings.Routes
	taskTypes := data.TaskTypes
	taskTypesByID := make(map[string]kitsu.TaskType, len(taskTypes))
	for _, taskType := range taskTypes {
		taskTypesByID[strings.TrimSpace(taskType.ID)] = taskType
	}
	linked := currentProductionLinkedHumanDiscordIDs(data.Team, data.Users, data.GuildUsers)
	roleNames := make(map[string]DiscordGuildRole, len(data.RoleCatalog))
	for _, role := range data.RoleCatalog {
		roleNames[role.ID] = role
	}
	var rows strings.Builder
	for _, route := range routes {
		channel := label("未設定", "Not configured")
		if name := strings.TrimSpace(route.ForumChannelName); name != "" {
			channel = "#" + strings.TrimPrefix(name, "#")
		}
		taskTypeID := strings.TrimSpace(route.TaskTypeID)
		taskType := taskTypesByID[taskTypeID]
		additional := make([]string, 0)
		targets, _, _ := model.ListProjectReviewerTargetsForTaskType(db, p.ID, taskTypeID)
		for _, target := range targets {
			switch target.TargetKind {
			case model.ReviewerTargetUser:
				if name := linked[strings.TrimSpace(target.DiscordID)]; name != "" {
					additional = append(additional, name)
				}
			case model.ReviewerTargetRole:
				// Send access is verified when the target is applied, so the
				// table shows the role from the cached role list.
				if role, ok := roleNames[target.DiscordID]; ok && data.RolesReady {
					additional = append(additional, "@"+role.Name)
				} else {
					additional = append(additional, label("ロール利用不可", "Role unavailable"))
				}
			}
		}
		automatic, automaticState := productionAutomaticReviewerRecipients(taskType, data)
		recipients := append(append([]string(nil), automatic...), additional...)
		if automaticState != "matched" && automaticState != "none" {
			recipients = append(recipients, label("確認不可", "Unavailable"))
		}
		recipients = uniqueSortedRecipientLabels(recipients)
		recipientsMarkup := renderProductionWFATargetChips(recipients, label("通知先なし", "No recipients"), "", false)
		ariaLabel := label("通知先なし", "No recipients")
		if len(recipients) > 0 {
			ariaLabel = strings.Join(recipients, ", ")
		}
		rows.WriteString(`<tr class="production-notification-row" data-task-type-id="` + esc(taskTypeID) + `"><th scope="row">` + esc(route.TaskTypeName) + `</th><td data-label="` + esc(channelLabel) + `">` + esc(channel) + `</td><td data-label="` + esc(label("WFA通知先", "WFA recipients")) + `"><div class="production-wfa-effective-recipients" data-wfa-recipients aria-label="` + esc(ariaLabel) + `">` + recipientsMarkup + `</div></td></tr>`)
	}
	if len(routes) == 0 {
		rows.WriteString(`<tr class="production-notification-empty"><td colspan="3"><div class="production-detail-state-row">` + esc(label("通知ルーティングはまだ設定されていません。", "No notification routing is configured.")) + `</div></td></tr>`)
	}
	pendingAutomatic := renderPendingAutomaticReviewerSource(taskTypes, data, lang)
	directoryNotice := productionDirectoryNoticeMarkup(lang, p, data)
	roleApplyNotice := `<p class="field-help guild-directory-notice">` + esc(label("ロールの送信可否は表示時に確認しません。適用時に確認します。", "Role send access is not verified for display; it is verified when applied.")) + `</p>`
	editAction := ""
	if len(routes) > 0 {
		editURL := withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID)+"&tab=notifications&edit_routing=1", r)
		editAction = `<a class="btn-ghost" href="` + esc(editURL) + `">` + esc(label("編集", "Edit")) + `</a>`
	}
	return `<section id="wfa-recipients" tabindex="-1" class="production-settings-section production-notification-table-section"><div class="page-heading production-notification-heading"><h3 id="production-notifications-title">` + esc(label("WFA通知先", "WFA recipients")) + `</h3><div class="production-notification-actions"><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(statusLabel) + `</span>` + editAction + `</div></div>` + directoryNotice + roleApplyNotice + `<div class="table-wrap production-notification-table production-detail-surface"><table><colgroup><col class="production-notification-col-task"><col class="production-notification-col-channel"><col class="production-notification-col-wfa"></colgroup><thead><tr><th>Kitsu Task Type</th><th>` + esc(channelLabel) + `</th><th>` + esc(label("WFA通知先", "WFA recipients")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div><div hidden data-wfa-pending-source>` + pendingAutomatic + `</div></section>`
}

func renderProductionWFATargetChips(values []string, emptyLabel, unavailableLabel string, automatic bool) string {
	unique := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	if automatic && len(unique) == 1 && (unique[0] == emptyLabel || unique[0] == unavailableLabel) {
		return `<span class="production-wfa-state-chip">` + esc(unique[0]) + `</span>`
	}
	if len(unique) == 0 {
		return `<span class="production-wfa-state-chip">` + esc(emptyLabel) + `</span>`
	}
	const visibleLimit = 2
	var chips strings.Builder
	for index, value := range unique {
		if index == visibleLimit {
			chips.WriteString(`<span class="production-wfa-more-chip">+` + strconv.Itoa(len(unique)-visibleLimit) + `</span>`)
			break
		}
		chips.WriteString(`<span class="production-wfa-target-chip" title="` + esc(value) + `">` + esc(value) + `</span>`)
	}
	return `<span class="production-wfa-chip-row" aria-label="` + esc(strings.Join(unique, ", ")) + `">` + chips.String() + `</span>`
}

func compactAutomaticReviewerLabel(value, lang string) string {
	switch strings.TrimSpace(value) {
	case "No matching Supervisor.", "該当するSupervisorはいません。":
		return t(lang, "Supervisorなし", "No Supervisor")
	case "Discord membership could not be verified.", "Discordメンバーを確認できません。",
		"Discord membership is loading.", "Discordメンバーを取得中です。",
		"Production Team unavailable.", "プロダクションのチームを読み込めません。",
		"Kitsu connection required.", "Kitsu接続が必要です。",
		"Task Type data unavailable.", "Task Type情報を読み込めません。",
		"Supervisor data unavailable.", "Supervisor情報を読み込めません。":
		return t(lang, "確認不可", "Unavailable")
	default:
		return value
	}
}

func productionAutomaticReviewerLabel(taskType kitsu.TaskType, data productionNotificationReviewerView, lang string) string {
	label := func(ja, en string) string { return t(lang, ja, en) }
	names, state := productionAutomaticReviewerRecipients(taskType, data)
	switch state {
	case "discord-pending":
		return label("Discordメンバーを取得中です。", "Discord membership is loading.")
	case "discord-unavailable":
		return label("Discordメンバーを確認できません。", "Discord membership could not be verified.")
	case "team-unavailable":
		return label("プロダクションのチームを読み込めません。", "Production Team unavailable.")
	case "kitsu-required":
		return label("Kitsu接続が必要です。", "Kitsu connection required.")
	case "task-type-unavailable":
		return label("Task Type情報を読み込めません。", "Task Type data unavailable.")
	case "supervisor-unavailable":
		return label("Supervisor情報を読み込めません。", "Supervisor data unavailable.")
	case "none":
		return label("該当するSupervisorはいません。", "No matching Supervisor.")
	default:
		return strings.Join(names, ", ")
	}
}

func productionAutomaticReviewerRecipients(taskType kitsu.TaskType, data productionNotificationReviewerView) ([]string, string) {
	if names, ok := data.AutomaticReviewersByTaskType[taskType.ID]; ok {
		return names, data.AutomaticStatesByTaskType[taskType.ID]
	}
	switch {
	case data.GuildPending:
		return nil, "discord-pending"
	case data.GuildErr != nil || data.BotToken == "":
		return nil, "discord-unavailable"
	case data.TeamErr != nil:
		return nil, "team-unavailable"
	case !data.KitsuReady:
		return nil, "kitsu-required"
	case strings.TrimSpace(taskType.ID) == "":
		return nil, "task-type-unavailable"
	}
	people, err := reviewerDepartmentSupervisorsForTeam(data.KitsuBase, data.KitsuToken, taskType.DepartmentID, data.Team)
	if err != nil {
		return nil, "supervisor-unavailable"
	}
	linked := currentProductionLinkedHumanDiscordIDs(data.Team, data.Users, data.GuildUsers)
	seen := map[string]bool{}
	var names []string
	for _, person := range people {
		user := globalUserForKitsuPerson(data.Users, person)
		if user == nil {
			continue
		}
		name := linked[strings.TrimSpace(user.DiscordID)]
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, "none"
	}
	return names, "matched"
}

func uniqueSortedRecipientLabels(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	sort.Strings(unique)
	return unique
}

func renderPendingAutomaticReviewerSource(taskTypes []kitsu.TaskType, data productionNotificationReviewerView, lang string) string {
	var pending strings.Builder
	for _, taskType := range taskTypes {
		id := strings.TrimSpace(taskType.ID)
		if id == "" {
			continue
		}
		automatic := productionAutomaticReviewerLabel(taskType, data, lang)
		pending.WriteString(`<div data-task-type-id="` + esc(id) + `"><div class="production-wfa-summary"><div class="production-wfa-summary-line"><span class="production-wfa-group" data-wfa-group="automatic"><span class="production-wfa-kind">` + esc(t(lang, "自動", "Automatic")) + `</span><span class="production-wfa-value" data-wfa-automatic-value>` + esc(compactAutomaticReviewerLabel(automatic, lang)) + `</span></span></div></div></div>`)
	}
	return pending.String()
}

func renderSelectedProductionActivity(db *gorm.DB, p model.Project, lang string, r *http.Request) string {
	var rows strings.Builder
	var logs []model.AuditLog
	if db != nil && strings.TrimSpace(p.KitsuProjectID) != "" {
		db.Where("project_id = ?", strings.TrimSpace(p.KitsuProjectID)).Where("(task_id = '' OR task_id IS NULL OR success = ?)", false).Order("created_at desc").Limit(5).Find(&logs)
	}
	if len(logs) == 0 {
		return `<section id="recent-activity" tabindex="-1" class="production-settings-section production-detail-surface production-activity-section"><h2 class="production-detail-section-title">` + esc(t(lang, "最近のアクティビティ", "Recent activity")) + `</h2><div class="production-detail-state-row production-activity-empty" role="status">` + esc(t(lang, "最近のアクティビティはありません。", "No recent activity.")) + `</div></section>`
	}
	for _, log := range logs {
		result := t(lang, "成功", "Success")
		resultClass := "success"
		if !log.Success {
			result = t(lang, "要確認", "Needs review")
			resultClass = "warning"
		}
		context := strings.TrimSpace(log.TaskType)
		if context == "" {
			context = strings.TrimSpace(log.EntityName)
		}
		contextMarkup := ""
		if context != "" {
			contextMarkup = `<span class="activity-context">` + esc(context) + `</span>`
		}
		rows.WriteString(`<li class="activity-row"><time class="activity-date" datetime="` + esc(log.CreatedAt.Format(time.RFC3339)) + `">` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</time><strong>` + esc(iaActivityAction(lang, log)) + `</strong>` + contextMarkup + `<span class="status-badge status-badge-` + resultClass + ` activity-result">` + esc(result) + `</span></li>`)
	}
	return `<section id="recent-activity" tabindex="-1" class="production-settings-section production-detail-surface production-activity-section"><h2 class="production-detail-section-title">` + esc(t(lang, "最近のアクティビティ", "Recent activity")) + `</h2><ul class="activity-list" role="log">` + rows.String() + `</ul><a class="btn-ghost" href="` + esc(withLang("/bot/admin/audit", r)) + `">` + esc(t(lang, "監査ログを表示", "View audit log")) + `</a></section>`
}

func renderCurrentProductionTroubleshooting(db *gorm.DB, p model.Project, lang string, expanded bool) string {
	diagnoses := model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 10)
	problemClass, problemLabel := "success", t(lang, "問題なし", "No current problems")
	if len(diagnoses) > 0 {
		problemClass, problemLabel = "warning", t(lang, "要確認", "Needs review")
	}
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	forumSettings, hasForum, forumErr := model.LoadProductionForumSettings(db, p.KitsuProjectID)
	routeCount := 0
	if forumErr == nil && hasForum {
		routeCount = len(forumSettings.Routes)
	}
	participantCount := len(ListKitsuProjectParticipants(p.KitsuProjectID))
	linkedCount := len(model.ListProjectUserMaps(db, p.ID))
	recentCount, recentFailures := 0, 0
	var recentLogs []model.AuditLog
	if db != nil && strings.TrimSpace(p.KitsuProjectID) != "" {
		db.Where("project_id = ?", strings.TrimSpace(p.KitsuProjectID)).Order("created_at desc").Limit(40).Find(&recentLogs)
	}
	for _, log := range recentLogs {
		recentCount++
		if !log.Success {
			recentFailures++
		}
	}
	item := func(label, value, class, explanation string) string {
		return `<div class="production-diagnostic-item"><strong>` + esc(label) + `</strong><span class="status-pill ` + esc(normalizeStatusClass(class)) + `" role="status">` + esc(value) + `</span><small>` + esc(explanation) + `</small></div>`
	}
	kitsuValue, kitsuClass := t(lang, "未設定", "Not configured"), "warning"
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) != "" {
		kitsuValue, kitsuClass = t(lang, "接続済み", "Connected"), "success"
	} else if readiness.KitsuConfigured {
		kitsuValue = t(lang, "要確認", "Needs review")
	}
	discordValue, discordClass := t(lang, "未設定", "Not configured"), "warning"
	if readiness.DiscordConfigured {
		discordValue, discordClass = t(lang, "設定済", "Configured"), "success"
	}
	routingValue, routingClass := t(lang, "未設定", "Not configured"), "warning"
	if forumErr == nil && hasForum && routeCount > 0 {
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
	processingExplanation := t(lang, "このプロダクションの通知処理記録はありません。", "No notification processing records exist for this Production.")
	if recentCount > 0 {
		processingValue, processingClass = t(lang, "正常", "Healthy"), "success"
		processingExplanation = fmt.Sprintf(t(lang, "直近の記録%d件、失敗%d件。", "%d recent records, %d failures."), recentCount, recentFailures)
		if recentFailures > 0 {
			processingValue, processingClass = t(lang, "要確認", "Needs review"), "warning"
		}
	}
	var diagnosticDetails strings.Builder
	if len(diagnoses) > 0 {
		diagnosticDetails.WriteString(`<details class="advanced-details"><summary>` + esc(t(lang, "現在の問題の詳細", "Current issue details")) + `</summary><ul class="list-tight">`)
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
	diagnosticDetails.WriteString(`<div class="production-diagnostics-list">`)
	diagnosticDetails.WriteString(item(t(lang, "Kitsu接続", "Kitsu connection"), kitsuValue, kitsuClass, t(lang, "現在のランタイム認証状態。", "Current runtime authentication state.")))
	diagnosticDetails.WriteString(item(t(lang, "Discord Bot", "Discord Bot"), discordValue, discordClass, t(lang, "現在のBot設定。", "Current Bot configuration.")))
	diagnosticDetails.WriteString(item(t(lang, "通知ルーティング", "Notification routing"), routingValue, routingClass, fmt.Sprintf(t(lang, "%d件のフォーラム通知設定を確認しました。", "%d forum notification routes were inspected."), routeCount)))
	diagnosticDetails.WriteString(item(t(lang, "参加者取得", "Participant retrieval"), participantValue, participantClass, fmt.Sprintf(t(lang, "KitsuプロダクションのチームAPIから%d人を取得しました。", "The Kitsu Production team API returned %d people."), participantCount)))
	diagnosticDetails.WriteString(item(t(lang, "ユーザー紐づけ", "User linking"), linkValue, linkClass, fmt.Sprintf(t(lang, "このプロダクションのユーザー割り当て%d件。", "User assignments recorded for this Production: %d."), linkedCount)))
	diagnosticDetails.WriteString(item(t(lang, "最近の通知処理", "Recent notification processing"), processingValue, processingClass, processingExplanation))
	diagnosticDetails.WriteString(`</div>`)
	open := ""
	if expanded {
		open = ` open`
	}
	return `<details id="diagnostics" class="production-settings-section production-settings-disclosure advanced-details production-diagnostics"` + open + `><summary>` + esc(t(lang, "診断", "Diagnostics")) + `</summary><div class="production-diagnostics-content"><div class="production-diagnostic-current"><strong>` + esc(t(lang, "現在の問題", "Current issues")) + `</strong><span class="status-pill ` + esc(normalizeStatusClass(problemClass)) + `" role="status">` + esc(problemLabel) + `</span></div>` + diagnosticDetails.String() + `</div></details>`
}

func renderSelectedProductionDanger(r *http.Request, p model.Project, lang string, expanded bool) string {
	open := ""
	if expanded {
		open = ` open`
	}
	if p.ValidationOnly || p.ReadOnlyPreview {
		return `<details id="danger-zone" class="production-settings-section production-settings-disclosure advanced-details danger-zone"` + open + `><summary>` + esc(t(lang, "危険な操作", "Danger Zone")) + `</summary><p class="field-help" role="status">` + esc(t(lang, "検証専用のプロダクションでは変更や削除は実行できません。", "Changes and deletion are disabled for validation-only Productions.")) + `</p></details>`
	}
	disconnectPhrase := t(lang, "連携解除", "DISCONNECT")
	deletePhrase := t(lang, "削除", "DELETE")
	return `<details id="danger-zone" class="production-settings-section production-settings-disclosure advanced-details danger-zone"` + open + `><summary>` + esc(t(lang, "危険な操作", "Danger Zone")) + `</summary><p class="danger-zone-intro">` + esc(t(lang, "危険操作は確認後にのみ実行されます。", "Destructive actions require explicit confirmation.")) + `</p><div class="danger-actions"><div class="danger-action-block"><h3>` + esc(tr(lang, "ia.disconnect_production")) + `</h3><p class="hint">` + esc(t(lang, "KitsuSyncの連携だけを解除します。Discord側のリソースは残ります。", "This removes only the KitsuSync connection. Discord resources remain.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "Productionの連携を解除します。Discord側のリソースは残ります。", "This removes the Production connection. Discord resources remain.")) + `" data-require-text="` + esc(disconnectPhrase) + `"><input type="hidden" name="action" value="remove_connection"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-ghost" type="submit">` + esc(tr(lang, "ia.disconnect_production")) + `</button></form></div><div class="danger-action-block"><h3>` + esc(tr(lang, "ia.delete_discord_resources")) + `</h3><p class="hint">` + esc(t(lang, "Discord側のチャンネルとカテゴリを削除します。連携解除とは別の操作です。", "This may delete Discord channels and the category. It is separate from disconnecting the Production.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "Discord側のリソースを削除します。", "This may delete Discord-side resources.")) + `" data-require-text="` + esc(deletePhrase) + `"><input type="hidden" name="action" value="preview_remove_connection_with_discord"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-danger" type="submit">` + esc(tr(lang, "ia.delete_discord_resources")) + `</button></form></div></div></details>`
}

type pipelineHealthItem struct {
	label, value, class, explanation, details, detailsLabel, action, actionLabel string
}

func renderIAHealth(w http.ResponseWriter, r *http.Request, db *gorm.DB, pollInterval time.Duration) {
	lang := currentLang(r)
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db))
	readinessView := readinessViewFor(lang, r, readiness)
	stats := Stats.Snapshot()
	windowName := telemetryWindow60Seconds
	issues := recentSystemIssues(lang, r, db)
	eventAction, eventActionLabel := "", ""
	if stats.LastPollErr != "" && readiness.PrerequisitesReady {
		eventAction, eventActionLabel = withLang("/bot/admin/audit", r), t(lang, "監査ログを確認", "Review audit log")
	}
	notificationAction, notificationActionLabel := "", ""
	if !readiness.OverallReady {
		view := readinessViewFor(lang, r, readiness)
		notificationAction, notificationActionLabel = view.ActionURL, view.ActionLabel
	}
	routingAction, routingActionLabel := "", ""
	if !readiness.RoutingReady && readiness.ProductionConnected {
		routingAction = systemStatusProjectAction(lang, r, db)
		routingActionLabel = t(lang, "プロダクションを確認", "Review Productions")
	}
	items := []pipelineHealthItem{
		{label: t(lang, "イベント監視", "Event monitoring"), value: pipelineProcessingValue(lang, stats), class: pipelineProcessingClass(stats), explanation: pipelineProcessingHint(lang, stats, readiness), details: pipelineProcessingDetails(lang, stats), detailsLabel: t(lang, "観測診断", "Observation diagnostics"), action: eventAction, actionLabel: eventActionLabel},
		{label: t(lang, "通知処理", "Notification processing"), value: pipelineNotificationValue(lang, readiness), class: map[bool]string{true: "success", false: "blocked"}[readiness.OverallReady], explanation: pipelineNotificationHint(lang, readiness), details: pipelineNotificationDetails(lang, stats, db), detailsLabel: t(lang, "通知診断", "Notification diagnostics"), action: notificationAction, actionLabel: notificationActionLabel},
		{label: t(lang, "内部データ", "Internal data"), value: t(lang, "利用可能", "Available"), class: "success"},
		{label: t(lang, "接続・ルーティング整合性", "Connection / routing integrity"), value: pipelineRoutingValue(lang, readiness), class: map[bool]string{true: "success", false: "warning"}[readiness.RoutingReady], explanation: pipelineRoutingHint(lang, readiness), details: pipelineRoutingDetails(lang, readiness, db), detailsLabel: t(lang, "接続・ルーティング診断", "Connection and routing diagnostics"), action: routingAction, actionLabel: routingActionLabel},
	}
	var healthRows strings.Builder
	for index, item := range items {
		healthRows.WriteString(renderPipelineHealthItem(lang, item, index))
	}
	issuesSection := ""
	if issues != "" {
		issuesSection = `<section class="section-card glass system-issues" aria-labelledby="system-issues-title"><div class="page-heading"><div><h2 id="system-issues-title">` + esc(t(lang, "最近のシステム問題", "Recent system issues")) + `</h2><p class="hint">` + esc(t(lang, "直近の失敗と復旧記録を表示します。", "Recent failure and recovery records.")) + `</p></div></div>` + issues + `</section>`
	}
	livenessSection := renderLivenessSection(lang, stats, pollInterval)
	handoffSection := renderHandoffAttentionSection(lang, db)
	body := `<div class="section-stack"><section class="section-card glass pipeline-health" aria-labelledby="pipeline-health-title"><div class="page-heading"><div><h2 id="pipeline-health-title">` + esc(t(lang, "通知パイプラインの状態", "Notification pipeline health")) + `</h2><p class="hint">` + esc(t(lang, "通知に関わる各段階の状態を確認できます。取得できないメトリクスは未確認として表示します。", "Review each notification stage. Metrics that are not available are shown as unconfirmed.")) + `</p></div><span class="status-pill ` + esc(readinessView.Class) + `" role="status">` + esc(readinessView.Label) + `</span></div><div class="pipeline-health-grid">` + healthRows.String() + `</div></section>` + livenessSection + handoffSection + issuesSection + `</div>`
	body = renderRuntimeObservabilitySummary(lang, stats, readiness, windowName, body)
	if card := currentProblemCard(lang, items); card != "" {
		body = strings.Replace(body, `<div class="section-stack system-status-sections">`, `<div class="section-stack system-status-sections">`+card, 1)
	}
	body += `<script data-system-status-refresh></script>`
	body = replaceSystemStatusRefreshScript(body)
	body = stripSystemStatusRedundantCopy(body)
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.system_status"), r, body))
}

func renderRuntimeObservabilitySummary(lang string, stats RuntimeSnapshot, readiness SharedBotRuntimeReadiness, windowName, body string) string {
	asOf := time.Now()
	windowName = telemetryWindow60Seconds
	body = renderRuntimeObservabilitySummaryRawAt(lang, stats, windowName, body, asOf)
	body = stripSystemStatusRedundantCopy(body)
	body = addTelemetryViewerLocalTimesAt(body, stats, windowName, asOf)
	body = replaceElementTextByID(body, "system-observability-title", t(lang, "API応答状態", "API response status"))
	body = replaceElementTextByID(body, "pipeline-health-title", t(lang, "KitsuSync処理状態", "KitsuSync operational status"))
	return `<div class="section-stack system-status-sections">` + body + `</div>`
}

func stripSystemStatusRedundantCopy(body string) string {
	for _, copy := range []string{
		"実測できた応答時間を時間順に表示します。",
		"Shows real response times in chronological order.",
		"通知に関わる各段階の状態を確認できます。取得できないメトリクスは未確認として表示します。",
		"Review each notification stage. Metrics that are not available are shown as unconfirmed.",
		"直近の失敗と復旧記録を表示します。",
		"Recent failure and recovery records.",
	} {
		body = strings.ReplaceAll(body, `<p class="hint">`+copy+`</p>`, "")
	}
	body = strings.ReplaceAll(body, `items.length+" / 20 "+text("観測","observations")+" · "+label+" · "+text("最終更新","Last updated")+" "`, `label+" · "+text("最終更新","Last updated")+" "`)
	body = strings.ReplaceAll(body, `items.length+" / 20 "+text("観測","observations")+" · "+windowLabel+" · "+text("最終更新","Last updated")+" "`, `windowLabel+" · "+text("最終更新","Last updated")+" "`)
	return removePipelineReadinessPill(body)
}

func removePipelineReadinessPill(body string) string {
	sectionStart := strings.Index(body, `<section class="section-card glass pipeline-health"`)
	gridStart := strings.Index(body, `<div class="pipeline-health-grid">`)
	if sectionStart < 0 || gridStart < 0 || gridStart <= sectionStart {
		return body
	}
	spanStart := strings.Index(body[sectionStart:gridStart], `<span class="status-pill `)
	if spanStart < 0 {
		return body
	}
	spanStart += sectionStart
	spanEnd := strings.Index(body[spanStart:], `</span>`)
	if spanEnd < 0 {
		return body
	}
	spanEnd += spanStart + len(`</span>`)
	return body[:spanStart] + body[spanEnd:]
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

func addTelemetryViewerLocalTimesAt(body string, stats RuntimeSnapshot, windowName string, asOf time.Time) (result string) {
	for _, service := range []string{"kitsu", "discord"} {
		items := filterAPIObservations(stats.APIObservations[service], asOf, time.Minute)
		if len(items) == 0 {
			continue
		}
		marker := `<span class="api-observation-meta" data-telemetry-meta>`
		replacement := `<span class="api-observation-meta" data-telemetry-meta data-telemetry-at="` + esc(items[len(items)-1].At.UTC().Format(time.RFC3339)) + `">`
		body = strings.Replace(body, marker, replacement, 1)
	}
	return body + `<script data-telemetry-local-time>(function(){window.kitsuSyncSystemStatusTime=function(value){var date=new Date(value);if(Number.isNaN(date.getTime())){return ""}return [date.getHours(),date.getMinutes(),date.getSeconds()].map(function(part){return String(part).padStart(2,"0")}).join(":")};var zone=Intl.DateTimeFormat().resolvedOptions().timeZone||"UTC";document.querySelectorAll("[data-telemetry-meta][data-telemetry-at]").forEach(function(node){node.textContent=(document.documentElement.lang==="ja"?"最終更新":"Last updated")+" "+window.kitsuSyncSystemStatusTime(node.getAttribute("data-telemetry-at"));node.title="UTC "+node.getAttribute("data-telemetry-at")+" · "+zone});document.querySelectorAll(".telemetry-point[data-telemetry-at]").forEach(function(node){var duration=Number(node.getAttribute("data-telemetry-duration"))||0,label=(window.kitsuSyncSystemStatusTime(node.getAttribute("data-telemetry-at"))+" "+duration+" ms "+(document.documentElement.lang==="ja"?"正常":"Healthy")).trim();node.setAttribute("aria-label",label);var title=node.querySelector("title");if(title){title.textContent=label}})})();</script>`
}

func renderRuntimeObservabilitySummaryRawAt(lang string, stats RuntimeSnapshot, windowName, body string, asOf time.Time) string {
	windowName = telemetryWindow60Seconds
	return `<section class="section-card glass system-observability" aria-labelledby="system-observability-title"><div class="page-heading"><div><h2 id="system-observability-title">` + esc(t(lang, "外部APIの健全性", "External API health")) + `</h2><p class="hint">` + esc(t(lang, "実測できた応答時間を時間順に表示します。", "Shows real response times in chronological order.")) + `</p></div><div class="telemetry-window-actions"><span class="system-live-indicator" role="status"><i aria-hidden="true"></i><span data-system-live-label>` + esc(t(lang, "自動更新", "Auto-refresh")) + `</span></span></div></div><div class="system-observability-grid"><article class="api-observation-card" data-telemetry-card="kitsu"><div class="api-observation-summary"><h3>Kitsu API</h3><span class="status-pill ` + apiObservationStatusClassAt(stats, "kitsu", asOf) + `" role="status" data-telemetry-status>` + esc(apiObservationStatusAt(lang, stats, "kitsu", asOf)) + `</span></div><div class="api-observation-details" data-telemetry-details="kitsu">` + apiObservationDetailsAt(lang, stats, "kitsu", windowName, asOf) + `</div></article><article class="api-observation-card" data-telemetry-card="discord"><div class="api-observation-summary"><h3>Discord API</h3><span class="status-pill ` + apiObservationStatusClassAt(stats, "discord", asOf) + `" role="status" data-telemetry-status>` + esc(apiObservationStatusAt(lang, stats, "discord", asOf)) + `</span></div><div class="api-observation-details" data-telemetry-details="discord">` + apiObservationDetailsAt(lang, stats, "discord", windowName, asOf) + `</div></article></div></section>` + body
}

// currentProblemCard summarizes the first non-OK pipeline row as one
// compact card at the top of the system status page: what the problem is,
// why (the row's own explanation, when it carries one), and the row's
// next-action link. It derives only from the rows already computed for
// the page; when every row is OK it renders nothing.
func currentProblemCard(lang string, items []pipelineHealthItem) string {
	for _, item := range items {
		if normalizeStatusClass(item.class) == "success" {
			continue
		}
		sub := ""
		if strings.TrimSpace(item.explanation) != "" {
			sub = `<div class="sub">` + esc(item.explanation) + `</div>`
		}
		action := ""
		if item.action != "" && item.actionLabel != "" {
			action = `<a class="linkbtn" href="` + esc(item.action) + `">` + esc(item.actionLabel) + `</a>`
		}
		return `<section class="section-card glass current-problem" aria-labelledby="current-problem-title"><h2 id="current-problem-title">` + esc(t(lang, "現在の問題", "Current problem")) + `</h2><div class="current-problem-row"><span class="dot" aria-hidden="true"></span><div class="body">` + esc(item.label) + `: ` + esc(item.value) + sub + `</div>` + action + `</div></section>`
	}
	return ""
}

func renderPipelineHealthItem(lang string, item pipelineHealthItem, index int) string {
	_ = index
	action := ""
	if item.action != "" && item.actionLabel != "" {
		action = `<a class="btn-ghost pipeline-health-action" href="` + esc(item.action) + `">` + esc(item.actionLabel) + `</a>`
	}
	explanation := ""
	if strings.TrimSpace(item.explanation) != "" {
		explanation = `<p class="field-help">` + esc(item.explanation) + `</p>`
	}
	details := ""
	if strings.TrimSpace(item.details) != "" && strings.TrimSpace(item.detailsLabel) != "" {
		details = `<details class="pipeline-health-diagnostic"><summary>` + esc(item.detailsLabel) + `</summary><div class="pipeline-health-diagnostic-content">` + item.details + `</div></details>`
	}
	return `<article class="pipeline-health-item"><div class="pipeline-health-copy"><h3>` + esc(item.label) + `</h3>` + explanation + details + `</div><div class="pipeline-health-rail">` + action + `<span class="status-badge status-badge-` + esc(normalizeStatusClass(item.class)) + `" role="status">` + esc(item.value) + `</span></div></article>`
}

func apiObservationDetailsAt(lang string, stats RuntimeSnapshot, service, windowName string, asOf time.Time) string {
	windowName = telemetryWindow60Seconds
	items := filterAPIObservations(stats.APIObservations[service], asOf, time.Minute)
	if len(items) == 0 {
		// No measurement history yet: one line says so, instead of an
		// empty graph frame pretending to be a chart.
		return `<div class="api-observation-not-checked">` + esc(t(lang, "まだ計測データがありません", "No measurements yet")) + `</div>`
	}
	last := items[len(items)-1]
	value := t(lang, "リクエスト失敗", "Request failed")
	if last.Success {
		value = strconv.FormatInt(last.Duration.Milliseconds(), 10) + " ms"
	}
	normalMeta := fmt.Sprintf("%s %s", t(lang, "最終更新", "Last updated"), last.At.Local().Format("15:04:05"))
	scale := stableObservationScale(service, windowName, items)
	return `<div class="api-observation-latency"><div class="api-observation-primary"><strong data-telemetry-value>` + esc(value) + `</strong></div><span class="api-observation-meta" data-telemetry-meta>` + esc(normalMeta) + `</span></div>` + apiObservationLineGraphAt(items, lang, asOf, observationYDomain{Lower: 0, Upper: scale})
}

func systemStatusProjectAction(lang string, r *http.Request, db *gorm.DB) string {
	for _, project := range model.ListProjects(db) {
		if len(model.ListNotificationRoutingDiagnoses(db, project.KitsuProjectID, 1)) > 0 {
			return withLang("/bot/admin/projects?project="+url.QueryEscape(project.KitsuProjectID)+"&tab=notifications", r)
		}
	}
	return withLang("/bot/admin/projects", r)
}

func apiObservationLineGraphAt(items []APIObservation, lang string, asOf time.Time, domain observationYDomain) string {
	geometry := telemetryChartGeometry()
	if domain.Upper <= domain.Lower {
		domain = observationYDomain{Lower: 0, Upper: 10}
	}
	dataWidth := geometry.DataRight - geometry.DataLeft
	window := time.Minute
	start := asOf.Add(-window)
	type chartPoint struct {
		x, y float64
		at   time.Time
	}
	var successful []chartPoint
	var points strings.Builder
	inWindowCount := 0
	for _, item := range items {
		if item.At.After(asOf) {
			continue
		}
		x := geometry.DataLeft + math.Max(0, math.Min(1, item.At.Sub(start).Seconds()/window.Seconds()))*dataWidth
		if !item.Success {
			if !item.At.Before(start) {
				inWindowCount++
				successful = append(successful, chartPoint{x: math.NaN(), y: math.NaN(), at: item.At}) // failed observations break the line without a plotted value
			}
			continue
		}
		value := math.Max(0, float64(item.Duration.Milliseconds()))
		y := geometry.PlotBottom - (geometry.PlotBottom-geometry.PlotTop)*math.Min(1, value/domain.Upper)
		if item.At.Before(start) {
			// Keep only the immediately preceding sample for an entering segment.
			successful = append(successful, chartPoint{x: math.NaN(), y: y, at: item.At})
			continue
		}
		inWindowCount++
		label := telemetryObservationLabel(lang, item)
		xText, yText := fmt.Sprintf("%.1f", x), fmt.Sprintf("%.1f", y)
		points.WriteString(`<circle class="telemetry-point success" cx="` + xText + `" cy="` + yText + `" r="3" data-telemetry-at="` + esc(item.At.UTC().Format(time.RFC3339Nano)) + `" data-telemetry-duration="` + strconv.FormatInt(item.Duration.Milliseconds(), 10) + `" data-telemetry-success="true" tabindex="0" role="img" aria-label="` + esc(label) + `"><title>` + esc(label) + `</title></circle>`)
		successful = append(successful, chartPoint{x: x, y: y, at: item.At})
	}
	// A preceding success may continue into the visible window only when the
	// adjacent measurements are close enough to represent one monitoring run.
	for index := 0; index+1 < len(successful); index++ {
		previous, first := successful[index], successful[index+1]
		if math.IsNaN(previous.x) && !math.IsNaN(previous.y) && previous.at.Before(start) && !math.IsNaN(first.x) && !first.at.Before(start) && first.at.Sub(previous.at) <= telemetryLineGapAfter {
			fraction := start.Sub(previous.at).Seconds() / first.at.Sub(previous.at).Seconds()
			boundaryY := previous.y + (first.y-previous.y)*fraction
			successful[index] = chartPoint{x: float64(geometry.DataLeft), y: boundaryY, at: start}
		}
	}
	// Each successful run is a separate path; failed samples never acquire a latency value.
	var line strings.Builder
	var run []chartPoint
	flush := func() {
		if len(run) > 1 {
			line.WriteString(`<path class="telemetry-line success" d="M`)
			for index, point := range run {
				if index > 0 {
					line.WriteString(` L`)
				}
				line.WriteString(fmt.Sprintf("%.1f,%.1f", point.x, point.y))
			}
			line.WriteString(`"></path>`)
		}
		run = nil
	}
	var previousAt time.Time
	for _, point := range successful {
		if math.IsNaN(point.x) {
			flush()
			previousAt = point.at
			continue
		}
		if !previousAt.IsZero() && point.at.Sub(previousAt) > telemetryLineGapAfter {
			flush()
		}
		run = append(run, point)
		previousAt = point.at
	}
	flush()
	labels := []string{"60s", "30s", "Now"}
	if lang == "ja" {
		labels = []string{"60秒", "30秒", "今"}
	}
	middle := domain.Upper / 2
	svg := `<svg class="api-sparkline" viewBox="0 0 496 104" role="img" aria-label="` + esc(fmt.Sprintf("%d observations", inWindowCount)) + `"><line class="chart-guide" x1="44" y1="45" x2="452" y2="45"></line><line class="chart-baseline" x1="44" y1="82" x2="452" y2="82"></line><text class="chart-tick" text-anchor="end" x="40" y="12">` + esc(formatLatencyTick(domain.Upper)) + `</text><text class="chart-tick" text-anchor="end" x="40" y="48">` + esc(formatLatencyTick(middle)) + `</text><text class="chart-tick" text-anchor="end" x="40" y="84">0ms</text>` + line.String() + points.String() + `<text class="chart-time-label" text-anchor="start" x="44" y="100">` + esc(labels[0]) + `</text><text class="chart-time-label" text-anchor="middle" x="248" y="100">` + esc(labels[1]) + `</text><text class="chart-time-label" text-anchor="end" x="452" y="100">` + esc(labels[2]) + `</text></svg>`
	return svg
}

func telemetryObservationLabel(lang string, item APIObservation) string {
	when := item.At.Local().Format("15:04:05")
	if !item.Success {
		return when + " " + t(lang, "リクエスト失敗", "Request failed")
	}
	return when + " " + strconv.FormatInt(item.Duration.Milliseconds(), 10) + " ms " + t(lang, "正常", "Healthy")
}

type telemetryChartLayout struct {
	Width, Height, PlotLeft, PlotRight, DataLeft, DataRight, PlotTop, PlotMiddle, PlotBottom, PlotCenterX float64
}

type observationYDomain struct {
	Lower float64
	Upper float64
}

func formatLatencyTick(value float64) string {
	return strconv.FormatInt(int64(math.Round(math.Max(0, value))), 10) + "ms"
}

func telemetryChartGeometry() telemetryChartLayout {
	return telemetryChartLayout{Width: 496, Height: 104, PlotLeft: 44, PlotRight: 452, DataLeft: 48, DataRight: 448, PlotTop: 8, PlotMiddle: 45, PlotBottom: 82, PlotCenterX: 248}
}

func observationScaleForItems(items []APIObservation) float64 {
	maxValue := 1.0
	for _, item := range items {
		if !item.Success {
			continue
		}
		if value := float64(item.Duration.Milliseconds()); value > maxValue {
			maxValue = value
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

func apiObservationStatusClassAt(stats RuntimeSnapshot, service string, asOf time.Time) string {
	items := stats.APIObservations[service]
	if len(items) == 0 {
		return "neutral"
	}
	if apiObservationIsStale(items, asOf) {
		return "warning"
	}
	if items[len(items)-1].Success {
		return "ok"
	}
	return "bad"
}

func apiObservationStatusAt(lang string, stats RuntimeSnapshot, service string, asOf time.Time) string {
	items := stats.APIObservations[service]
	if len(items) == 0 {
		return t(lang, "未確認", "Not checked")
	}
	if apiObservationIsStale(items, asOf) {
		return t(lang, "古いデータ", "Stale")
	}
	if items[len(items)-1].Success {
		return t(lang, "正常", "Healthy")
	}
	return t(lang, "要確認", "Needs review")
}

func apiObservationIsStale(items []APIObservation, asOf time.Time) bool {
	return len(items) > 0 && asOf.Sub(items[len(items)-1].At) > telemetryStaleAfter
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

func pipelineProcessingHint(lang string, stats RuntimeSnapshot, readiness SharedBotRuntimeReadiness) string {
	if stats.LastPollErr != "" {
		return t(lang, "直近の処理で問題が記録されています。", "The most recent processing cycle recorded an issue.")
	}
	if stats.LastPollTime.IsZero() {
		if !readiness.PrerequisitesReady {
			return readinessViewFor(lang, nil, readiness).Hint
		}
		return t(lang, "初回の観測を待っています。", "Waiting for the first observation.")
	}
	return ""
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
		return ""
	}
	if !readiness.ProductionConnected {
		return t(lang, "通知可能なプロダクションがありません。", "No Production is currently available for notifications.")
	}
	return t(lang, "Productionはありますが、有効な通知先設定がありません。", "A Production exists, but its notification routing is missing or invalid.")
}

func pipelineNotificationValue(lang string, readiness SharedBotRuntimeReadiness) string {
	if readiness.OverallReady {
		return t(lang, "利用可能", "Available")
	}
	return t(lang, "利用不可", "Unavailable")
}

func pipelineNotificationHint(lang string, readiness SharedBotRuntimeReadiness) string {
	if readiness.OverallReady {
		return ""
	}
	return readinessViewFor(lang, nil, readiness).Hint
}

func pipelineNotificationDetails(lang string, stats RuntimeSnapshot, db *gorm.DB) string {
	var rows strings.Builder
	rows.WriteString(`<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "成功", "Successful")) + `</dt><dd>` + strconv.FormatInt(stats.SendSuccessTotal, 10) + `</dd></div><div><dt>` + esc(t(lang, "失敗", "Failed")) + `</dt><dd>` + strconv.FormatInt(stats.SendFailureTotal, 10) + `</dd></div>`)
	deliveries, err := model.ListUnresolvedNotificationDeliveries(db, 50)
	if err != nil {
		rows.WriteString(`<div><dt>` + esc(t(lang, "未解決の通知状態", "Unresolved delivery state")) + `</dt><dd>` + esc(t(lang, "確認不可", "Unavailable")) + `</dd></div>`)
	} else if len(deliveries) == 0 {
		rows.WriteString(`<div><dt>` + esc(t(lang, "未解決の通知状態", "Unresolved delivery state")) + `</dt><dd>0</dd></div>`)
	} else {
		rows.WriteString(`<div><dt>` + esc(t(lang, "未解決の通知状態", "Unresolved delivery state")) + `</dt><dd>` + strconv.Itoa(len(deliveries)) + `</dd></div>`)
		for _, item := range deliveries {
			label := item.Disposition
			if item.Cleanup {
				label = t(lang, "旧メッセージ削除待ち", "Prior-message cleanup pending")
			}
			detail := label
			if item.ConflictCount > 0 {
				detail += " · " + strconv.Itoa(item.ConflictCount) + t(lang, "件", " conflicts")
			}
			if item.FailureClass != "" {
				detail += " · " + item.FailureClass
			}
			rows.WriteString(`<div><dt><code>` + esc(item.EventID) + `</code></dt><dd>` + esc(detail) + `</dd></div>`)
		}
	}
	rows.WriteString(`</dl>`)
	return rows.String()
}

func pipelineRoutingDetails(lang string, readiness SharedBotRuntimeReadiness, db *gorm.DB) string {
	connection := t(lang, "未接続", "Disconnected")
	if readiness.ProductionConnected {
		connection = t(lang, "接続済み", "Connected")
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
		if settings, ok, err := model.LoadProductionForumSettings(db, project.KitsuProjectID); err == nil && ok {
			routeCount += len(settings.Routes)
			configCount++
		}
	}
	return `<dl class="pipeline-detail-list"><div><dt>` + esc(t(lang, "プロダクション接続", "Production connection")) + `</dt><dd>` + esc(connection) + `</dd></div><div><dt>` + esc(t(lang, "接続済みProduction", "Connected Productions")) + `</dt><dd>` + strconv.Itoa(connected) + `</dd></div><div><dt>` + esc(t(lang, "設定済みルート", "Configured routes")) + `</dt><dd>` + strconv.Itoa(routeCount) + `</dd></div><div><dt>` + esc(t(lang, "通知設定", "Notification configurations")) + `</dt><dd>` + strconv.Itoa(configCount) + `</dd></div><div><dt>` + esc(t(lang, "ルート設定", "Route configuration")) + `</dt><dd>` + esc(routing) + `</dd></div></dl>`
}

// recentSystemIssues renders the newest failed audit rows as a small
// table (time / content / state). The state chip is the same status-pill
// the audit log itself uses for a failure, so one word keeps one style
// across the page. Audit rows carry no aggregate count, so the table
// shows one row per failure; a row that names a Production links to the
// audit log filtered to it.
func recentSystemIssues(lang string, r *http.Request, db *gorm.DB) string {
	logs := model.ListAuditLogs(db, 20)
	var rows strings.Builder
	count := 0
	for _, log := range logs {
		if log.Success {
			continue
		}
		count++
		content := esc(log.ErrorMessage)
		if name := strings.TrimSpace(log.ProjectName); name != "" {
			content += ` <a href="` + esc(withLang("/bot/admin/audit?production="+url.QueryEscape(name), r)) + `">` + esc(t(lang, "監査ログで見る", "View in audit log")) + `</a>`
		}
		rows.WriteString(`<tr><td>` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</td><td>` + content + `</td><td><span class="status-pill bad" role="status">` + esc(t(lang, "失敗", "Failed")) + `</span></td></tr>`)
		if count >= 5 {
			break
		}
	}
	if count == 0 {
		return ""
	}
	return `<table class="data-table system-issues-table"><thead><tr><th scope="col">` + esc(t(lang, "時刻", "Time")) + `</th><th scope="col">` + esc(t(lang, "内容", "Content")) + `</th><th scope="col">` + esc(t(lang, "状態", "State")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table>`
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

func renderIAAudit(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	logs := model.ListAuditLogs(db, 200)
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].CreatedAt.After(logs[j].CreatedAt) })
	// Production, user, and result filters: view-only narrowing of the rows below.
	// Unknown values fall back to showing everything.
	displayName := func(log model.AuditLog) string {
		return fallbackText(log.ProjectName, t(lang, "未割り当て", "Unassigned"))
	}
	selected := strings.TrimSpace(r.URL.Query().Get("production"))
	selectedUser := strings.TrimSpace(r.URL.Query().Get("user"))
	selectedResult := strings.TrimSpace(r.URL.Query().Get("result"))
	seenNames := map[string]bool{}
	var productionNames []string
	seenActors := map[string]bool{}
	var actorNames []string
	for _, log := range logs {
		name := displayName(log)
		if !seenNames[name] {
			seenNames[name] = true
			productionNames = append(productionNames, name)
		}
		actor := auditActorLabel(lang, log)
		if !seenActors[actor] {
			seenActors[actor] = true
			actorNames = append(actorNames, actor)
		}
	}
	if selected != "" && !seenNames[selected] {
		selected = ""
	}
	if selectedUser != "" && !seenActors[selectedUser] {
		selectedUser = ""
	}
	if selectedResult != "success" && selectedResult != "failed" {
		selectedResult = ""
	}
	shown := make([]model.AuditLog, 0, len(logs))
	for _, log := range logs {
		if selected != "" && displayName(log) != selected {
			continue
		}
		if selectedUser != "" && auditActorLabel(lang, log) != selectedUser {
			continue
		}
		if selectedResult == "success" && !log.Success {
			continue
		}
		if selectedResult == "failed" && log.Success {
			continue
		}
		shown = append(shown, log)
	}
	var prodOptions strings.Builder
	prodOptions.WriteString(`<option value="">` + esc(t(lang, "全体", "All")) + `</option>`)
	for _, name := range productionNames {
		sel := ""
		if name == selected {
			sel = " selected"
		}
		prodOptions.WriteString(`<option value="` + esc(name) + `"` + sel + `>` + esc(name) + `</option>`)
	}
	var userOptions strings.Builder
	userOptions.WriteString(`<option value="">` + esc(t(lang, "全ユーザー", "All users")) + `</option>`)
	for _, actor := range actorNames {
		sel := ""
		if actor == selectedUser {
			sel = " selected"
		}
		userOptions.WriteString(`<option value="` + esc(actor) + `"` + sel + `>` + esc(actor) + `</option>`)
	}
	resultOptions := `<option value="">` + esc(t(lang, "全ての結果", "All results")) + `</option>` +
		`<option value="success"` + map[bool]string{true: " selected"}[selectedResult == "success"] + `>` + esc(t(lang, "成功", "Success")) + `</option>` +
		`<option value="failed"` + map[bool]string{true: " selected"}[selectedResult == "failed"] + `>` + esc(t(lang, "失敗", "Failed")) + `</option>`
	langInput := ""
	if qLang := strings.TrimSpace(r.URL.Query().Get("lang")); qLang != "" {
		langInput = `<input type="hidden" name="lang" value="` + esc(qLang) + `">`
	}
	filter := ""
	if len(logs) > 0 {
		countLabel := fmt.Sprintf("%d%s", len(shown), t(lang, " 件", " entries"))
		filter = `<form class="audit-filter-form" method="get" action="` + esc(withLang("/bot/admin/audit", r)) + `" aria-label="` + esc(t(lang, "プロダクションで絞り込む", "Filter audit log")) + `">` +
			langInput +
			`<label class="lbl" for="audit-filter-production">` + esc(t(lang, "プロダクション", "Production")) + `</label>` +
			`<select id="audit-filter-production" name="production" onchange="this.form.submit()">` + prodOptions.String() + `</select>` +
			`<div class="right">` +
			`<label class="lbl" for="audit-filter-user">` + esc(t(lang, "ユーザー", "User")) + `</label>` +
			`<select id="audit-filter-user" name="user" onchange="this.form.submit()">` + userOptions.String() + `</select>` +
			`<label class="lbl" for="audit-filter-result">` + esc(t(lang, "状態", "Result")) + `</label>` +
			`<select id="audit-filter-result" name="result" onchange="this.form.submit()">` + resultOptions + `</select>` +
			`<span class="count">` + esc(countLabel) + `</span>` +
			`</div></form>`
	}
	var rows strings.Builder
	for _, log := range shown {
		result := t(lang, "成功", "Success")
		if !log.Success {
			result = t(lang, "失敗", "Failed")
		}
		rows.WriteString(`<tr><td class="mobile-card-lead" data-label="` + esc(t(lang, "日時", "Date and time")) + `">` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</td><td data-label="` + esc(t(lang, "プロダクション", "Production")) + `">` + esc(log.ProjectName) + `</td><td data-label="` + esc(t(lang, "操作内容", "Action")) + `">` + esc(iaActivityAction(lang, log)) + `</td><td data-label="` + esc(t(lang, "操作ユーザー", "Acting user")) + `">` + esc(auditActorLabel(lang, log)) + `</td><td data-label="` + esc(t(lang, "結果", "Result")) + `"><span class="status-pill ` + map[bool]string{true: "ok", false: "bad"}[log.Success] + `">` + esc(result) + `</span></td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="5" class="muted">` + esc(t(lang, "監査ログはありません", "No audit log entries")) + `</td></tr>`)
	}
	body := `<div class="audit-log-content"><p class="hint">` + esc(t(lang, "設定変更、通知、失敗、復旧の履歴を確認できます。技術的な識別子は詳細表示に限定します。", "Review configuration, notification, failure, and recovery history. Technical identifiers remain in details.")) + `</p>` + filter + `<div class="table-wrap audit-log-table"><table class="mobile-card-table"><thead><tr><th>` + esc(t(lang, "日時", "Date and time")) + `</th><th>` + esc(t(lang, "プロダクション", "Production")) + `</th><th>` + esc(t(lang, "操作内容", "Action")) + `</th><th>` + esc(t(lang, "操作ユーザー", "Acting user")) + `</th><th>` + esc(t(lang, "結果", "Result")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></div>`
	for _, log := range shown {
		oldTime := `<td class="mobile-card-lead" data-label="` + esc(t(lang, "日時", "Date and time")) + `">` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</td>`
		newTime := `<td class="mobile-card-lead" data-label="` + esc(t(lang, "日時", "Date and time")) + `"><time class="audit-time" datetime="` + esc(log.CreatedAt.UTC().Format(time.RFC3339)) + `">` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</time></td>`
		body = strings.Replace(body, oldTime, newTime, 1)
	}
	body += `<script data-audit-local-time>(function(){document.querySelectorAll(".audit-time").forEach(function(node){var date=new Date(node.getAttribute("datetime"));if(Number.isNaN(date.getTime())){return}node.textContent=new Intl.DateTimeFormat("` + auditLocale(lang) + `",{year:"numeric",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",second:"2-digit"}).format(date);node.title="UTC "+node.getAttribute("datetime")})})();</script>`
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
		rows.WriteString(`<tr><td colspan="3" class="muted">` + esc(t(lang, "ユーザー紐づけはありません", "No user links yet")) + `</td></tr>`)
	}
	body := `<section class="section-card glass"><p class="hint">` + esc(t(lang, "KitsuユーザーとDiscordユーザーを紐づけます。WFA通知先はProductionの通知タブで設定します。", "Link Kitsu users to Discord users. WFA recipients are configured in each Production's Notifications tab.")) + `</p><table><thead><tr><th>Kitsu` + esc(t(lang, "ユーザー", " user")) + `</th><th>Discord` + esc(t(lang, "ユーザー", " user")) + `</th><th>` + esc(t(lang, "操作", "Action")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></section>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.user_mapping"), r, body))
}

type globalDiscordUserOption struct {
	ID       string
	Name     string
	Username string
}

type globalDiscordDirectory struct {
	Guilds        []DiscordGuild
	SelectedGuild DiscordGuild
	Options       []globalDiscordUserOption
}

func globalDiscordOptionsFromMembers(members []DiscordGuildMember) []globalDiscordUserOption {
	options := make([]globalDiscordUserOption, 0, len(members))
	nameCounts := make(map[string]int)
	usernameCounts := make(map[string]int)
	seenIDs := make(map[string]bool)
	for _, member := range members {
		if member.User.Bot {
			continue
		}
		id := strings.TrimSpace(member.User.ID)
		username := strings.TrimSpace(member.User.Username)
		name := strings.TrimSpace(member.Nick)
		if name == "" {
			name = strings.TrimSpace(member.User.GlobalName)
		}
		if name == "" {
			name = username
		}
		if id == "" || name == "" || seenIDs[id] {
			continue
		}
		seenIDs[id] = true
		options = append(options, globalDiscordUserOption{ID: id, Name: name, Username: username})
		nameCounts[strings.ToLower(name)]++
		if username != "" {
			usernameCounts[strings.ToLower(username)]++
		}
	}
	for i := range options {
		if nameCounts[strings.ToLower(options[i].Name)] < 2 {
			continue
		}
		suffix := ""
		username := strings.TrimSpace(options[i].Username)
		if username != "" && usernameCounts[strings.ToLower(username)] == 1 {
			suffix = "@" + username
		} else {
			// Use the complete immutable account ID when names cannot disambiguate.
			// A short suffix could still collide and leave two options ambiguous.
			suffix = "ID " + options[i].ID
		}
		options[i].Name += " · " + suffix
	}
	sort.Slice(options, func(i, j int) bool {
		if strings.EqualFold(options[i].Name, options[j].Name) {
			return options[i].ID < options[j].ID
		}
		return strings.ToLower(options[i].Name) < strings.ToLower(options[j].Name)
	})
	return options
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
	directory.Options = globalDiscordOptionsFromMembers(members)
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
	explanation := t(lang, "Discordユーザー一覧を確認できません。Bot接続を確認して再試行してください。", "The Discord user list could not be checked. Verify the Bot connection and try again.")
	switch kind {
	case discordMemberFailureFixture:
		explanation = t(lang, "QA用の合成データでは実Discordユーザーを取得しません。", "Real Discord users are not loaded for synthetic QA data.")
	case discordMemberFailureMalformed:
		explanation = t(lang, "Discordがユーザー一覧の要求を受け付けませんでした。", "Discord did not accept the member-list request.")
	case discordMemberFailureMismatch:
		explanation = t(lang, "Botが接続先Discordサーバーに参加していません。", "The Bot has not joined the connected Discord server.")
	case discordMemberFailureAccess:
		explanation = t(lang, "Botにメンバー一覧を読む権限がありません。", "The Bot cannot read the server member list.")
	case discordMemberFailureIntent:
		explanation = t(lang, "Discord Developer PortalのServer Members Intentを確認してください。", "Check Server Members Intent in the Discord Developer Portal.")
	}
	action := `<a class="btn-ghost" href="` + esc(appendLang("/bot/admin/bot", lang)) + `">` + esc(t(lang, "Bot接続を確認", "Check Bot Connection")) + `</a>`
	return `<div class="user-linking-lookup-error notice notice-warning" role="status"><div class="user-linking-lookup-copy"><strong>` + esc(title) + `</strong><p>` + esc(explanation) + `</p></div>` + action + `<details class="advanced-details"><summary>` + esc(t(lang, "診断の詳細", "Diagnostic details")) + `</summary><p class="field-help">` + esc(detail) + `</p></details></div>`
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
	body := `<section class="section-stack"><h1>` + esc(t(lang, user.KitsuName+"の対応付けを変更", "Change link for "+user.KitsuName)) + `</h1><section class="section-card glass"><p class="hint">` + esc(t(lang, "Kitsuユーザーに、接続済みDiscordサーバーで利用できるユーザーを選択します。", "Choose a Discord user available in a connected server for this Kitsu user.")) + `</p>` + message + `<form method="POST" class="form-stack"><input type="hidden" name="action" value="save_global_link"><input type="hidden" name="user_id" value="` + fmt.Sprint(user.ID) + `"><p class="field-label">` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `</p><p><strong>` + esc(user.KitsuName) + `</strong></p><label for="global-discord-user">` + esc(t(lang, "Discordユーザー", "Discord user")) + `</label><select id="global-discord-user" name="discord_user_id" required>` + optionHTML.String() + `</select><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users", r)) + `">` + esc(t(lang, "キャンセル", "Cancel")) + `</a><button class="btn" type="submit"` + disabled + `>` + esc(t(lang, "保存", "Save")) + `</button></div></form></section></section>`
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
		projects, projectsErr := setupKitsuProjects(db)
		requestedProjectID := strings.TrimSpace(r.URL.Query().Get("project"))
		requestedStep := strings.TrimSpace(r.URL.Query().Get("wizard_step"))
		if requestedProjectID != "" && (requestedStep == "3" || requestedStep == "4") {
			selected := wizardProject(projects, requestedProjectID)
			if selected.ID != "" && (model.FindProjectByKitsuID(db, selected.ID) == nil || productionRepairMode(r)) {
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
		if productionRepairMode(r) {
			updateWizardState(r, func(state *wizardState) { state.Repair = true })
		}
		fmt.Fprint(w, adminPage(lang, tr(lang, "ia.new_connection"), r, renderSetupWizard(lang, r, db, projects, projectsErr, botToken, step)))
	} else {
		lang := currentLang(r)
		botToken := storedRuntimeDiscordBotToken(db)
		projects, _ := setupKitsuProjects(db)
		projectID := strings.TrimSpace(r.URL.Query().Get("project"))
		var projectOptions strings.Builder
		projectOptions.WriteString(`<option value="">` + esc(t(lang, "プロダクションを選択", "Select a Kitsu Production")) + `</option>`)
		for _, p := range projects {
			selected := ""
			if p.ID == projectID {
				selected = " selected"
			}
			projectOptions.WriteString(`<option value="` + esc(p.ID) + `"` + selected + `>` + esc(p.Name) + `</option>`)
		}
		body := `<div class="section-stack"><section class="section-card glass"><h1>` + esc(tr(lang, "ia.new_connection")) + `</h1><p class="hint">` + esc(t(lang, "プロダクションとDiscordサーバーを選び、接続内容を確認してから接続します。フォーラムは接続後にフォーラム設定で作成します。", "Select a Production and Discord server, review the connection, then connect. Forums are created afterwards in the forum settings.")) + `</p><ol class="step-list"><li>` + esc(t(lang, "Kitsu Productionを選択", "Select a Kitsu Production")) + `</li><li>` + esc(t(lang, "Discordサーバーを選択", "Select a Discord server")) + `</li><li>` + esc(t(lang, "カテゴリと通知言語を選択", "Choose the category and notification language")) + `</li><li>` + esc(t(lang, "内容を確認してから実行", "Confirm the exact plan before execution")) + `</li></ol><form method="GET" class="section-stack"><input type="hidden" name="lang" value="` + esc(lang) + `">` + repairInput(r) + `<label for="new-connection-production">` + esc(t(lang, "Kitsu Production", "Kitsu Production")) + `</label><select id="new-connection-production" name="project">` + projectOptions.String() + `</select><button class="btn" type="submit">` + esc(t(lang, "次へ", "Continue")) + `</button></form></section>`
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
	if productionRepairMode(r) {
		values.Set("repair", "1")
	}
	return withLang("/bot/setup?"+values.Encode(), r)
}

func productionRepairMode(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.TrimSpace(r.URL.Query().Get("repair")) == "1" {
		return true
	}
	// The repair link only sets repair on its first GET; later wizard steps are
	// plain GET/POST submissions, so the session carries the mode forward.
	return wizardStateForRequest(r).Repair
}

func repairInput(r *http.Request) string {
	if !productionRepairMode(r) {
		return ""
	}
	return `<input type="hidden" name="repair" value="1">`
}

func renderSetupWizard(lang string, r *http.Request, db *gorm.DB, projects []KitsuProject, projectsErr error, botToken string, step int) string {
	projectID := projectIDFromRequest(r)
	guildID := strings.TrimSpace(r.URL.Query().Get("plan_guild"))
	readiness := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), botToken)
	if step > 1 && !readiness.PrerequisitesReady {
		step = 1
	}
	if step >= 3 {
		selected := wizardProject(projects, projectID)
		if selected.ID == "" || model.FindProjectByKitsuID(db, projectID) != nil && !productionRepairMode(r) {
			step = 2
		}
	}
	body := `<div class="section-stack"><section class="section-card glass"><p class="hint">` + esc(tr(lang, "wizard.description")) + `</p>` + renderSetupSteps(lang, step, true) + `</section>`
	switch step {
	case 1:
		body += renderWizardPrerequisitesShared(lang, r, readiness)
	case 2:
		body += renderWizardProductionLocalized(lang, r, db, projects, projectsErr)
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
		statusSummaryRow(t(lang, "プロダクション接続", "Production connections"), map[bool]string{true: "success", false: "neutral"}[readiness.ProductionConnected], productionValue, "", "") +
		statusSummaryRow(t(lang, "通知状態", "Notifications"), view.NotificationClass, view.Notification, "", "") + `</dl>`
	if !readiness.PrerequisitesReady {
		body += `<p class="state-explanation" role="status" aria-live="polite">` + esc(view.Hint) + `</p><a class="btn" href="` + esc(view.ActionURL) + `">` + esc(view.ActionLabel) + `</a>`
	} else {
		body += `<p class="state-explanation" role="status" aria-live="polite">` + esc(t(lang, "接続の前提条件が整いました。プロダクションを選択できます。", "Connection prerequisites are ready. Select a Production to continue.")) + `</p><a class="btn" href="` + esc(setupWizardURL(r, 2, "", "", false)) + `">` + esc(t(lang, "次へ", "Next")) + `</a>`
	}
	return body + `</section>`
}

var wizardGuildLister = ListBotGuilds

func renderWizardProductionLocalized(lang string, r *http.Request, db *gorm.DB, projects []KitsuProject, projectsErr error) string {
	errorMessage := ""
	if projectsErr != nil {
		errorMessage = `<p class="state-explanation" role="alert">` + esc(t(lang, "KitsuからProductionリストを取得できませんでした。Kitsu接続を確認して再試行してください。", "Production list could not be loaded from Kitsu. Check the Kitsu connection and try again.")) + `</p>`
	} else if len(projects) == 0 {
		errorMessage = `<p class="state-explanation" role="status" aria-live="polite">` + esc(t(lang, "KitsuからProductionが返されませんでした。", "Kitsu returned no Productions.")) + `</p>`
	}
	if projectsErr != nil || len(projects) == 0 {
		return `<section class="section-card glass" aria-labelledby="wizard-production-title"><h2 id="wizard-production-title">` + esc(tr(lang, "wizard.production_title")) + `</h2>` + errorMessage + `<div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 1, "", "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><a class="btn" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a></div></section>`
	}
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
	return `<section class="section-card glass" aria-labelledby="wizard-production-title"><h2 id="wizard-production-title">` + esc(tr(lang, "wizard.production_title")) + `</h2>` + errorMessage + `<form method="GET" class="section-stack"><input type="hidden" name="wizard_step" value="3"><input type="hidden" name="lang" value="` + esc(lang) + `">` + repairInput(r) + `<label for="wizard-production">` + esc(tr(lang, "wizard.production_label")) + `</label><select id="wizard-production" name="project" required aria-describedby="wizard-production-help">` + options.String() + `</select><p id="wizard-production-help" class="field-help">` + esc(tr(lang, "wizard.production_help")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 1, "", "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.next")) + `</button></div></form></section>`
}

func renderWizardServer(lang string, r *http.Request, botToken, projectID string) string {
	var guilds []DiscordGuild
	var guildsErr error
	tokenMissing := strings.TrimSpace(botToken) == ""
	if !tokenMissing {
		guilds, guildsErr = wizardGuildLister(botToken)
	}
	if tokenMissing || guildsErr != nil || len(guilds) == 0 {
		var message string
		switch {
		case tokenMissing:
			message = `<p class="state-explanation" role="alert">` + esc(t(lang, "Botトークンが設定されていません。接続設定を確認してください。", "Bot token is not configured. Check the connection settings.")) + `</p>`
		case guildsErr != nil:
			message = `<p class="state-explanation" role="alert">` + esc(t(lang, "Discordサーバーリストを取得できませんでした。Bot接続を確認して再試行してください。", "Discord server list could not be loaded. Check the Bot connection and try again.")) + `</p>`
		default:
			message = `<p class="state-explanation" role="status" aria-live="polite">` + esc(t(lang, "参加中のDiscordサーバーが見つかりませんでした。Botをサーバーに招待してから再試行してください。", "No Discord servers were found. Invite the Bot to a server and try again.")) + `</p>`
		}
		return `<section class="section-card glass" aria-labelledby="wizard-server-title"><h2 id="wizard-server-title">` + esc(tr(lang, "wizard.server_title")) + `</h2>` + message + `<div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 2, projectID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><a class="btn" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a></div></section>`
	}
	var options strings.Builder
	options.WriteString(`<option value="">` + esc(tr(lang, "wizard.select_server")) + `</option>`)
	for _, guild := range guilds {
		options.WriteString(`<option value="` + esc(guild.ID) + `">` + esc(guild.Name) + `</option>`)
	}
	return `<section class="section-card glass" aria-labelledby="wizard-server-title"><h2 id="wizard-server-title">` + esc(tr(lang, "wizard.server_title")) + `</h2><form method="GET" class="section-stack"><input type="hidden" name="wizard_step" value="4"><input type="hidden" name="project" value="` + esc(projectID) + `"><input type="hidden" name="lang" value="` + esc(lang) + `">` + repairInput(r) + `<label for="wizard-server">` + esc(tr(lang, "wizard.server_label")) + `</label><select id="wizard-server" name="plan_guild" required>` + options.String() + `</select><p class="field-help">` + esc(tr(lang, "wizard.server_help")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 2, projectID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit">` + esc(tr(lang, "wizard.next")) + `</button></div></form></section>`
}

func wizardProject(projects []KitsuProject, id string) KitsuProject {
	for _, p := range projects {
		if p.ID == id {
			return p
		}
	}
	return KitsuProject{}
}
func discordCategoryExists(channels []DiscordGuildChannel, categoryID string) bool {
	categoryID = strings.TrimSpace(categoryID)
	if categoryID == "" {
		return false
	}
	for _, channel := range channels {
		if channel.Type == 4 && channel.ID == categoryID {
			return true
		}
	}
	return false
}

// renderWizardPlan is the forum-only plan step: pick the Discord category and
// the notification language. Forum channels are provisioned later from the
// forum settings page, so there is no per-Task-Type channel plan anymore.
func renderWizardPlan(lang string, r *http.Request, db *gorm.DB, botToken string, projects []KitsuProject, projectID, guildID string, review bool) string {
	project := wizardProject(projects, projectID)
	if project.ID == "" || guildID == "" {
		return `<section class="section-card glass" role="alert">` + esc(tr(lang, "wizard.plan_blocked")) + `</section>`
	}
	refetchURL := withLang("/bot/admin/refresh-discord-channels?guild="+url.QueryEscape(guildID)+"&back="+url.QueryEscape(r.URL.RequestURI()), r)
	refetchButton := `<div class="button-row"><a class="btn-ghost" href="` + esc(refetchURL) + `">` + esc(t(lang, "チャンネル情報を再取得", "Refresh channel info")) + `</a></div>`
	view := guildChannelListCache.view(guildID, botToken)
	var channels []DiscordGuildChannel
	if view.hasData() {
		if decoded, decodeErr := decodeGuildChannelsBody(view.body); decodeErr == nil {
			channels = decoded
		}
	}
	if len(channels) == 0 {
		return `<section class="section-card glass" role="alert"><h2>` + esc(tr(lang, "wizard.plan_title")) + `</h2><p>` + esc(tr(lang, "wizard.plan_unavailable")) + `</p>` + refetchButton + `</section>`
	}
	categoryID := strings.TrimSpace(r.URL.Query().Get("plan_category"))
	if categoryID == "" {
		if localProject := model.FindProjectByKitsuID(db, project.ID); localProject != nil {
			categoryID = strings.TrimSpace(localProject.DiscordCategoryID)
		}
		if !discordCategoryExists(channels, categoryID) {
			categoryID = "__create__"
		}
	}
	updateWizardState(r, func(state *wizardState) {
		state.ProductionID = project.ID
		state.GuildID = guildID
	})
	if review {
		return renderWizardPlanReview(lang, r, project, guildID, botToken, categoryID, normalizedProductionNotificationLanguage(r.FormValue("notification_language")))
	}
	categoryOptions := ""
	for _, channel := range channels {
		if channel.Type == 4 && strings.TrimSpace(channel.ID) != "" {
			selected := ""
			if channel.ID == categoryID {
				selected = " selected"
			}
			categoryOptions += `<option value="` + esc(channel.ID) + `"` + selected + `>` + esc(channel.Name) + `</option>`
		}
	}
	if categoryID != "__create__" && categoryOptions == "" {
		categoryOptions = `<option value="` + esc(categoryID) + `" selected>` + esc(KitsuSyncCategoryName(project.Name)) + `</option>`
	}
	categoryOptions += `<option value="__create__"` + map[bool]string{true: " selected", false: ""}[categoryID == "__create__"] + `>` + esc(t(lang, "新しいカテゴリを作成", "Create a new category")) + `</option>`
	notificationLanguage := normalizedProductionNotificationLanguage(r.FormValue("notification_language"))
	staleNotice := ""
	if !view.fresh && view.fetchFailed {
		staleNotice = `<p class="field-help" role="status">` + esc(guildChannelStaleNoticeText(lang, view.rateLimited)) + `</p>` + refetchButton
	}
	languageControl := `<div class="wizard-notification-language"><label for="wizard-notification-language">` + esc(t(lang, "Discord通知の言語", "Discord notification language")) + `</label><select id="wizard-notification-language" name="notification_language"><option value="ja"` + selectedAttr(notificationLanguage == "ja") + `>` + esc(t(lang, "日本語", "Japanese")) + `</option><option value="en"` + selectedAttr(notificationLanguage == "en") + `>English</option></select><p class="field-help">` + esc(t(lang, "このProductionの今後のDiscord通知に使う言語です。管理画面の言語とは別に設定できます。", "Used for future Discord notifications for this Production. It is independent of the admin UI language.")) + `</p></div>`
	return `<section class="section-card glass wizard-plan-card" aria-labelledby="wizard-plan-title"><div class="page-heading"><div><h2 id="wizard-plan-title">` + esc(tr(lang, "wizard.plan_title")) + `</h2><p class="hint">` + esc(tr(lang, "wizard.plan_hint")) + `</p></div></div><form method="GET" class="wizard-plan-form" action="/bot/setup"><input type="hidden" name="project" value="` + esc(project.ID) + `"><input type="hidden" name="plan_guild" value="` + esc(guildID) + `"><input type="hidden" name="lang" value="` + esc(lang) + `">` + repairInput(r) + staleNotice + `<label for="wizard-plan-category">` + esc(t(lang, "Discordカテゴリ", "Discord category")) + `</label><select id="wizard-plan-category" name="plan_category">` + categoryOptions + `</select><p class="field-help">` + esc(t(lang, "既存カテゴリを確認するか、新しい管理対象カテゴリを作成します。", "Choose an existing category or explicitly create a new managed category.")) + `</p>` + languageControl + `<div class="button-row wizard-plan-actions"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 3, project.ID, "", false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button class="btn" type="submit" name="wizard_step" value="5">` + esc(tr(lang, "wizard.review")) + `</button></div></form></section>`
}

func renderWizardPlanReview(lang string, r *http.Request, project KitsuProject, guildID, botToken, categoryID, notificationLanguage string) string {
	guildName := t(lang, "選択したDiscordサーバー", "Selected Discord server")
	if guilds, err := ListBotGuilds(botToken); err == nil {
		for _, guild := range guilds {
			if strings.TrimSpace(guild.ID) == strings.TrimSpace(guildID) && strings.TrimSpace(guild.Name) != "" {
				guildName = guild.Name
				break
			}
		}
	}
	categoryLabel := t(lang, "新しいカテゴリを作成", "Create a new category")
	if categoryID != "__create__" && strings.TrimSpace(categoryID) != "" {
		categoryLabel = t(lang, "既存カテゴリを再利用", "Reuse an existing category")
	}
	body := `<section class="section-card glass wizard-review-card" aria-labelledby="wizard-review-title"><div class="page-heading"><div><h2 id="wizard-review-title">` + esc(tr(lang, "wizard.review_title")) + `</h2><p class="hint">` + esc(tr(lang, "wizard.review_hint")) + `</p></div></div><dl class="wizard-connection-summary"><div><dt>` + esc(tr(lang, "wizard.production_summary")) + `</dt><dd>` + esc(project.Name) + `</dd></div><div><dt>` + esc(tr(lang, "wizard.server_summary")) + `</dt><dd>` + esc(guildName) + `</dd></div><div><dt>` + esc(tr(lang, "wizard.area_summary")) + `</dt><dd>` + esc(KitsuSyncCategoryName(project.Name)) + `</dd></div><div><dt>` + esc(t(lang, "カテゴリの扱い", "Category mode")) + `</dt><dd>` + esc(categoryLabel) + `</dd></div><div><dt>` + esc(t(lang, "Discord通知の言語", "Discord notification language")) + `</dt><dd>` + esc(notificationLanguageLabel(lang, notificationLanguage)) + `</dd></div></dl><p class="field-help">` + esc(tr(lang, "wizard.no_write")) + `</p><form method="POST" action="` + esc(withLang("/bot/setup", r)) + `" class="wizard-confirm-form"><input type="hidden" name="action" value="execute_production_connection"><input type="hidden" name="project_id" value="` + esc(project.ID) + `"><input type="hidden" name="guild_id" value="` + esc(guildID) + `"><input type="hidden" name="category_id" value="` + esc(categoryID) + `"><input type="hidden" name="notification_language" value="` + esc(notificationLanguage) + `">` + repairInput(r) + `<label class="wizard-confirm-control" for="wizard-confirm"><input id="wizard-confirm" type="checkbox" name="confirm_plan" value="yes" required> ` + esc(tr(lang, "wizard.confirm")) + `</label><div class="button-row"><a class="btn-ghost" href="` + esc(setupWizardURL(r, 4, project.ID, guildID, false)) + `">` + esc(tr(lang, "wizard.back")) + `</a><button id="wizard-execute" class="btn" type="submit" disabled>` + esc(tr(lang, "wizard.execute")) + `</button></div></form><script>(function(){var check=document.getElementById('wizard-confirm'),button=document.getElementById('wizard-execute');if(check&&button){var sync=function(){button.disabled=!check.checked;};check.addEventListener('change',sync);sync();}})();</script></section>`
	return body
}

// auditLocale keeps the Audit Log timestamp format in the page language
// instead of the browser locale, so JA and EN pages read consistently.
func auditLocale(lang string) string {
	if lang == "ja" {
		return "ja-JP"
	}
	return "en-US"
}

// notificationLanguageLabel shows the notification language the way step 4
// offers it, instead of a bare code such as "JA".
func notificationLanguageLabel(lang, code string) string {
	if strings.EqualFold(strings.TrimSpace(code), "ja") {
		return t(lang, "日本語", "Japanese")
	}
	return "English"
}

func renderWizardFrame(lang string, step int, body string) string {
	return `<div class="section-stack"><section class="section-card glass">` + renderSetupSteps(lang, step, false) + `</section>` + body + `</div>`
}

func renderSetupSteps(lang string, step int, blockFuture bool) string {
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
		if blockFuture && n > step && step == 1 {
			state = "blocked"
		}
		steps.WriteString(`<span class="setup-step ` + state + `"` + map[bool]string{true: ` aria-current="step"`, false: ""}[n == step] + `><span class="step-num">` + strconv.Itoa(n) + `</span><span class="step-label">` + esc(label) + `</span></span>`)
		if n < len(labels) {
			steps.WriteString(`<span class="step-connector" aria-hidden="true"></span>`)
		}
	}
	return `<div class="setup-steps" aria-label="` + esc(tr(lang, "wizard.progress")) + `">` + steps.String() + `</div>`
}

func renderWizardComplete(lang string, r *http.Request, db *gorm.DB, projectID string) string {
	name := ""
	if project := model.FindProjectByKitsuID(db, projectID); project != nil {
		name = project.Name
	}
	return `<section class="section-card glass" role="status" aria-live="polite"><h2>` + esc(tr(lang, "wizard.complete_title")) + `</h2><p>` + esc(trf(lang, "wizard.complete_message", name)) + `</p><p class="field-help">` + esc(t(lang, "次はフォーラム設定でタスクタイプごとのフォーラムを作成してください。", "Next, create the per-Task-Type forums in the forum settings.")) + `</p><div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/forum?project="+url.QueryEscape(projectID), r)) + `">` + esc(t(lang, "フォーラム設定を開く", "Open forum settings")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects?project="+url.QueryEscape(projectID), r)) + `">` + esc(tr(lang, "wizard.open_production")) + `</a></div></section>`
}

func renderDashboardMenuRefined(lang string, r *http.Request, db *gorm.DB, projects []model.Project, attentionCount int, readiness SharedBotRuntimeReadiness, runtimeHealthy func() bool) string {
	type card struct{ label, path, description, first, second, firstClass, secondClass string }
	productionCounts := productionConnectionCounts(projects)
	systemLabel, systemClass, _ := overallRuntimeStatus(lang, readiness, Stats.Snapshot())
	systemClass = canonicalDashboardBadgeClass(systemClass)
	auditFirst, auditSecond, auditClass, auditSecondClass := dashboardAuditSummaryCanonical(lang, db)
	cards := []card{
		{tr(lang, "connections.title"), "/bot/admin/bot", t(lang, "KitsuとDiscordの接続状態を確認します。", "Kitsu and Discord connection health."), "", "", "", ""},
		{tr(lang, "ia.production_list"), "/bot/admin/projects", t(lang, "接続済みと未接続のプロダクションを確認します。", "Review connected and disconnected Productions."), "", "", "", ""},
		{tr(lang, "ia.user_mapping"), "/bot/admin/users", t(lang, "人間のKitsuユーザーとDiscordユーザーの紐づけを管理します。", "Human Kitsu-to-Discord links."), t(lang, "設定済", "Configured"), "", "ok", ""},
		{tr(lang, "ia.system_status"), "/bot/admin/health", t(lang, "実測されたシステム健全性を確認します。", "Review measured system health."), systemLabel, "", systemClass, ""},
		{tr(lang, "ia.audit_log"), "/bot/admin/audit", t(lang, "操作履歴と通知イベントを確認します。", "Action history and notification events."), t(lang, "記録なし", "No records"), "", "muted", ""},
	}
	cards[4].first, cards[4].second, cards[4].firstClass, cards[4].secondClass = auditFirst, auditSecond, auditClass, auditSecondClass
	kitsuStatus, discordStatus := canonicalConnectionStatuses(lang, db, readiness, runtimeHealthy)
	cards[0].first = kitsuStatus.Label
	cards[0].second = discordStatus.Label
	cards[0].firstClass = kitsuStatus.Class
	cards[0].secondClass = discordStatus.Class
	cards[1].first = fmt.Sprintf("%s %d", t(lang, "接続済み", "Connected"), connectedProductionCount(projects))
	cards[1].first = fmt.Sprintf("%s %d", t(lang, "接続済み", "Connected"), productionCounts.Connected)
	cards[1].second = fmt.Sprintf("%s %d", t(lang, "未接続", "Disconnected"), productionCounts.Disconnected)
	cards[1].secondClass = map[bool]string{true: "warning", false: "ok"}[productionCounts.Disconnected > 0]
	var body strings.Builder
	body.WriteString(`<section class="dashboard-cta"><div><h2>` + esc(tr(lang, "ia.new_connection")) + `</h2></div><a class="btn dashboard-cta-action" href="` + esc(withLang("/bot/setup", r)) + `">` + esc(t(lang, "新しい接続を開始", "Open setup")) + `</a></section><section class="dashboard-menu"><h2>` + esc(t(lang, "管理メニュー", "Management")) + `</h2><div class="dashboard-menu-grid">`)
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

type productionCounts struct {
	Total        int
	Connected    int
	Disconnected int
}

func productionConnectionCounts(projects []model.Project) productionCounts {
	var counts productionCounts
	for _, project := range projects {
		if project.ValidationOnly {
			continue
		}
		counts.Total++
		if project.ReadOnlyPreview {
			counts.Disconnected++
		} else {
			counts.Connected++
		}
	}
	return counts
}

func dashboardProductionMetric(lang string, counts productionCounts) string {
	return `<div class="metric-card"><div class="metric-label">` + esc(t(lang, "プロダクション", "Production")) + `</div><div class="metric-value">` + strconv.Itoa(counts.Total) + `</div><div class="production-count-breakdown"><span>` + esc(t(lang, "接続済み", "Connected")) + ` <strong>` + strconv.Itoa(counts.Connected) + `</strong></span><span>` + esc(t(lang, "未接続", "Disconnected")) + ` <strong>` + strconv.Itoa(counts.Disconnected) + `</strong></span></div></div>`
}

func productionListConnectionSummary(lang string, counts productionCounts) string {
	return `<div class="production-list-summary" aria-label="` + esc(t(lang, "プロダクション接続の概要", "Production connection summary")) + `"><span>` + esc(t(lang, "接続済み", "Connected")) + ` <strong>` + strconv.Itoa(counts.Connected) + `</strong></span><span>` + esc(t(lang, "未接続", "Disconnected")) + ` <strong>` + strconv.Itoa(counts.Disconnected) + `</strong></span></div>`
}

func connectedProductionCount(projects []model.Project) int {
	return productionConnectionCounts(projects).Connected
}

func productionConnectionStatus(project model.Project, lang string) (string, string) {
	if project.ReadOnlyPreview || project.ValidationOnly {
		return "neutral", t(lang, "未接続", "Disconnected")
	}
	return "ok", t(lang, "接続済み", "Connected")
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

// productionDirectoryNoticeMarkup explains an unloaded or unverified Discord
// member/role directory; both the view and the edit mode of Notifications show it.
func productionDirectoryNoticeMarkup(lang string, p model.Project, data productionNotificationReviewerView) string {
	var notice strings.Builder
	if data.GuildPending || data.RolesPending {
		notice.WriteString(`<p class="field-help guild-directory-notice" role="status">` + esc(t(lang, "Discordのメンバー・ロールは未取得（背景で取得中）です。取得が終わるまで一部の通知先は確認できません。", "Discord members and roles are not loaded yet (fetching in the background). Some recipients stay unverified until it finishes.")) + `</p>`)
	}
	notice.WriteString(guildNoticeMarkup(guildDirectoryNotice(lang, guildMemberListCache.view(p.DiscordGuildID, data.BotToken))))
	return notice.String()
}
