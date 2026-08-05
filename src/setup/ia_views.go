package setup

import (
	"app/src/model"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// The IA views are deliberately read-only. Existing POST handlers remain the
// single mutation path so plan fingerprints, revalidation, confirmations and
// fail-closed persistence are not duplicated by the presentation layer.
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

func iaStatus(db *gorm.DB, project model.Project, lang string) (string, string, string) {
	cfg := model.FindProductionNotificationConfig(db, project.KitsuProjectID)
	if cfg == nil {
		return "bad", t(lang, "未設定", "Incomplete"), t(lang, "通知先が設定されていません。通知先を1つ以上設定してください。", "No notification route is configured. Add at least one valid destination.")
	}
	issues := model.ValidateProductionNotificationConfig(db, project.KitsuProjectID, model.ListProductionNotificationRoutes(db, project.KitsuProjectID))
	if len(issues) > 0 {
		return "bad", t(lang, "更新が必要", "Needs attention"), t(lang, "通知先の確認が必要です。詳細を確認してから修正してください。", "Notification destinations need attention. Review the details before changing them.")
	}
	if !cfg.Enabled {
		return "warn", t(lang, "一時停止中", "Paused"), t(lang, "設定は有効ですが、通知を一時停止しています。", "The configuration is valid, but notifications are paused.")
	}
	return "ok", t(lang, "有効", "Active"), t(lang, "通知先の設定が有効です。", "Notification destinations are active.")
}

func renderIADashboard(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	projects := model.ListProjects(db)
	attention, paused := 0, 0
	var attentionRows, pausedRows strings.Builder
	for _, p := range projects {
		class, label, hint := iaStatus(db, p, lang)
		if class == "bad" {
			attention++
		}
		if class == "warn" {
			paused++
		}
		row := fmt.Sprintf(`<li><a href="%s"><strong>%s</strong></a><span class="status-pill %s">%s</span><span class="field-help">%s</span></li>`, esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID), r)), esc(p.Name), class, esc(label), esc(hint))
		if class == "bad" {
			attentionRows.WriteString(row)
		}
		if class == "warn" {
			pausedRows.WriteString(row)
		}
	}
	if attentionRows.Len() == 0 {
		attentionRows.WriteString(`<li class="muted">` + esc(t(lang, "対応が必要なProductionはありません。", "No Productions need attention.")) + `</li>`)
	}
	if pausedRows.Len() == 0 {
		pausedRows.WriteString(`<li class="muted">` + esc(t(lang, "通知停止中のProductionはありません。", "No Productions have notifications paused.")) + `</li>`)
	}
	body := `<div class="section-stack">` +
		`<section class="section-card glass" aria-labelledby="dashboard-summary"><h2 id="dashboard-summary">` + esc(tr(lang, "ia.overview")) + `</h2><div class="metric-grid"><div class="metric-card"><div class="metric-label">` + esc(t(lang, "接続済みProduction", "Connected Productions")) + `</div><div class="metric-value">` + fmt.Sprint(len(projects)) + `</div></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "全体の接続状態", "Overall connection status")) + `</div><div class="metric-value">` + esc(t(lang, "確認済み", "Connected")) + `</div></div></div></section>` +
		`<section class="section-card glass" aria-labelledby="dashboard-attention"><h2 id="dashboard-attention">` + esc(t(lang, "要対応のProduction", "Productions needing attention")) + `</h2><ul class="list-tight">` + attentionRows.String() + `</ul></section>` +
		`<section class="section-card glass" aria-labelledby="dashboard-paused"><h2 id="dashboard-paused">` + esc(t(lang, "通知停止中のProduction", "Productions with notifications paused")) + `</h2><ul class="list-tight">` + pausedRows.String() + `</ul></section>` +
		`<section class="section-card glass" aria-labelledby="dashboard-next"><h2 id="dashboard-next">` + esc(t(lang, "次に必要な操作", "Next required actions")) + `</h2><p class="hint">` + esc(t(lang, "状態を確認するProductionを選択するか、新しいProductionを接続してください。", "Select a Production that needs attention or connect a new Production.")) + `</p><div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(tr(lang, "ia.production_list")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/setup", r)) + `">` + esc(tr(lang, "ia.new_connection")) + `</a></div></section>` +
		`</div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.dashboard"), r, body))
}

func renderIAProductionList(w http.ResponseWriter, r *http.Request, db *gorm.DB, fallbackGuildID string) {
	lang := currentLang(r)
	selectedID := strings.TrimSpace(r.URL.Query().Get("project"))
	if selectedID != "" {
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
		rows.WriteString(emptyState("—", t(lang, "Productionがありません", "No Productions"), t(lang, "新しいProductionを接続してください。", "Connect a new Production.")))
	}
	body := `<div class="section-stack"><section class="section-card glass"><p class="hint">` + esc(t(lang, "Production一覧では状態を確認し、管理するProductionを1つ選択します。設定項目は選択後に表示されます。", "Use this list to review states and open one Production. Settings appear only after selection.")) + `</p></section>` + rows.String() + `</div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.production_list"), r, body))
}

func renderIASelectedProduction(w http.ResponseWriter, r *http.Request, db *gorm.DB, p model.Project, fallbackGuildID string) {
	lang := currentLang(r)
	class, label, hint := iaStatus(db, p, lang)
	sectionLink := func(id, title string) string {
		return `<a class="section-link" href="#` + id + `">` + esc(title) + `</a>`
	}
	statusSection := renderConnectedProductionNotificationSection(db, p, lang, r, class, label, hint, model.ValidateProductionNotificationConfig(db, p.KitsuProjectID, model.ListProductionNotificationRoutes(db, p.KitsuProjectID)))
	var mappings strings.Builder
	for _, m := range model.ListProductionChannelMappings(db, p.KitsuProjectID) {
		mappings.WriteString(`<li><strong>` + esc(m.TaskTypeName) + `</strong><span>→ ` + esc(m.ChannelName) + `</span></li>`)
	}
	if mappings.Len() == 0 {
		mappings.WriteString(`<li class="muted">` + esc(t(lang, "チャンネル設定はありません。", "No channel settings yet.")) + `</li>`)
	}
	var activity strings.Builder
	for _, log := range model.ListAuditLogs(db, 40) {
		if log.ProjectName == p.Name {
			activity.WriteString(`<li>` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + ` — ` + esc(log.EntityName) + ` — ` + esc(t(lang, "結果", "Result")) + `</li>`)
		}
	}
	if activity.Len() == 0 {
		activity.WriteString(`<li class="muted">` + esc(t(lang, "履歴はありません。", "No activity yet.")) + `</li>`)
	}
	var diagnoses strings.Builder
	for _, d := range model.ListNotificationRoutingDiagnoses(db, p.KitsuProjectID, 10) {
		diagnoses.WriteString(`<li>` + esc(d.Detail) + `</li>`)
	}
	if diagnoses.Len() == 0 {
		diagnoses.WriteString(`<li class="muted">` + esc(t(lang, "現在の問題はありません。", "No current problems.")) + `</li>`)
	}
	advanced := fmt.Sprintf(`<details class="advanced-details"><summary>%s</summary><dl class="detail-list"><dt>%s</dt><dd><code>%s</code></dd><dt>%s</dt><dd><code>%s</code></dd><dt>%s</dt><dd><code>%s</code></dd></dl></details>`, esc(tr(lang, "ia.advanced")), esc(t(lang, "Production ID", "Production ID")), esc(p.KitsuProjectID), esc(t(lang, "DiscordサーバーID", "Discord server ID")), esc(p.DiscordGuildID), esc(t(lang, "カテゴリID", "Category ID")), esc(p.DiscordCategoryID))
	danger := `<details class="advanced-details danger-zone"><summary>` + esc(tr(lang, "ia.danger")) + `</summary><div class="danger-actions"><div><h3>` + esc(tr(lang, "ia.disconnect_production")) + `</h3><p class="hint">` + esc(t(lang, "KitsuSyncの連携情報だけを削除します。Discord側のリソースは削除しません。", "Remove only the KitsuSync connection. Discord resources remain.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "削除", "delete")) + `"><input type="hidden" name="action" value="remove_connection"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-ghost" type="submit">` + esc(tr(lang, "ia.disconnect_production")) + `</button></form></div><div><h3>` + esc(tr(lang, "ia.delete_discord_resources")) + `</h3><p class="hint">` + esc(t(lang, "Discord側のチャンネルとカテゴリを削除します。内容を確認してから実行してください。", "Delete Discord-side channels and category only after reviewing the exact impact.")) + `</p><form method="POST" class="delete-form" data-confirm="` + esc(t(lang, "削除", "delete")) + `"><input type="hidden" name="action" value="preview_remove_connection_with_discord"><input type="hidden" name="project_id" value="` + esc(p.KitsuProjectID) + `"><button class="btn-danger" type="submit">` + esc(tr(lang, "ia.delete_discord_resources")) + `</button></form></div></div></details>`
	body := `<div class="production-context"><div class="page-heading"><div><div class="eyebrow">` + esc(tr(lang, "ia.productions")) + `</div><h1>` + esc(p.Name) + `</h1><p class="hint">` + esc(t(lang, "選択中のProduction", "Selected Production")) + `</p></div><span class="status-pill ` + class + `">` + esc(label) + `</span></div><nav class="section-nav" aria-label="` + esc(t(lang, "Production内のセクション", "Production sections")) + `">` + sectionLink("overview", tr(lang, "ia.overview")) + sectionLink("notifications", tr(lang, "ia.notifications")) + sectionLink("user-settings", tr(lang, "ia.user_settings")) + sectionLink("storage-settings", tr(lang, "ia.storage_settings")) + sectionLink("activity", tr(lang, "ia.activity")) + sectionLink("troubleshooting", tr(lang, "ia.troubleshooting")) + sectionLink("advanced", tr(lang, "ia.advanced")) + sectionLink("danger-zone", tr(lang, "ia.danger")) + `</nav>` +
		`<section id="overview" class="section-card glass"><h2>` + esc(tr(lang, "ia.overview")) + `</h2><p class="state-explanation" role="status">` + esc(hint) + `</p><p class="field-help">` + esc(t(lang, "Discordサーバー", "Discord server")) + `: ` + esc(fallbackText(p.DiscordGuildID, t(lang, "未接続", "Not connected"))) + `</p></section>` +
		`<section id="notifications" class="section-stack">` + statusSection + `<div class="section-card glass"><h2>` + esc(t(lang, "Task TypeとDiscordチャンネル", "Task Type to Discord channel settings")) + `</h2><ul class="mapping-list">` + mappings.String() + `</ul></div></section>` +
		`<section id="user-settings" class="section-card glass"><h2>` + esc(tr(lang, "ia.user_settings")) + `</h2><p class="hint">` + esc(t(lang, "Reviewer / Checkerの割り当てはこのProductionに紐づきます。", "Reviewer / Checker assignments belong to this Production.")) + `</p><a class="btn-ghost" href="` + esc(withLang("/bot/admin/users?project="+url.QueryEscape(p.KitsuProjectID), r)) + `">` + esc(t(lang, "ユーザー設定を開く", "Open user settings")) + `</a></section>` +
		`<section id="storage-settings" class="section-card glass"><h2>` + esc(tr(lang, "ia.storage_settings")) + `</h2><p class="hint">` + esc(t(lang, "このProductionの保存先とリンクを管理します。", "Manage storage destinations and links for this Production.")) + `</p><form method="POST" action="` + esc(withLang("/bot/admin/drive", r)) + `"><input type="hidden" name="kitsu_project_id" value="` + esc(p.KitsuProjectID) + `"><label for="storage-url">` + esc(t(lang, "保存先リンク", "Storage link")) + `</label><input id="storage-url" type="url" name="storage_url" value="` + esc(p.StorageURL) + `"><button class="btn" type="submit">` + esc(t(lang, "保存", "Save")) + `</button></form></section>` +
		`<section id="activity" class="section-card glass"><h2>` + esc(tr(lang, "ia.activity")) + `</h2><ul class="list-tight" role="log">` + activity.String() + `</ul></section>` +
		`<section id="troubleshooting" class="section-card glass"><h2>` + esc(tr(lang, "ia.troubleshooting")) + `</h2><p class="hint">` + esc(t(lang, "現在の問題、原因、次の操作を確認できます。詳細情報は必要な場合だけ開いてください。", "Review the current problem, cause, and next action. Open technical details only when needed.")) + `</p><ul class="list-tight">` + diagnoses.String() + `</ul></section>` + advanced + `<div id="danger-zone">` + danger + `</div></div>`
	fmt.Fprint(w, adminPage(lang, p.Name, r, body))
}

func renderIABot(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	ready := sharedBotRuntimeReadiness(db, model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db)).OverallReady
	state := t(lang, "対応が必要", "Action required")
	if ready {
		state = t(lang, "設定済み", "Configured")
	}
	body := `<div class="section-stack"><section class="section-card glass"><div class="page-heading"><div><h2>` + esc(tr(lang, "ia.bot_connection")) + `</h2><p class="hint">` + esc(t(lang, "Botの接続状態、参加中のDiscordサーバー、必要な権限を確認します。", "Review bot connection state, joined Discord servers, and required permissions.")) + `</p></div><span class="status-pill ` + map[bool]string{true: "ok", false: "bad"}[ready] + `">` + esc(state) + `</span></div><dl class="status-list"><dt>` + esc(t(lang, "Botの接続状態", "Bot connection state")) + `</dt><dd>` + esc(state) + `</dd><dt>` + esc(t(lang, "必要な権限", "Required permissions")) + `</dt><dd>` + esc(t(lang, "チャンネルの表示と送信", "View and send in channels")) + `</dd></dl><a class="btn" href="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `">` + esc(t(lang, "接続を設定または再接続", "Connect or reconnect")) + `</a></section><section class="section-card glass"><h2>` + esc(t(lang, "参加中のDiscordサーバー", "Joined Discord servers")) + `</h2><p class="hint">` + esc(t(lang, "Bot接続後に確認できます。", "Available after the bot is connected.")) + `</p></section></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.bot_connection"), r, body))
}

func renderIAHealth(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	body := `<div class="section-stack"><section class="section-card glass"><h2>` + esc(tr(lang, "ia.system_status")) + `</h2><p class="hint">` + esc(t(lang, "接続状態と全体の問題、次に必要な操作を確認します。", "Review connection states, global problems, and next actions.")) + `</p><div class="metric-grid"><div class="metric-card"><div class="metric-label">Kitsu</div><div class="metric-value">` + esc(t(lang, "接続状態を確認", "Review connection")) + `</div></div><div class="metric-card"><div class="metric-label">Discord</div><div class="metric-value">` + esc(t(lang, "Bot接続を確認", "Review bot connection")) + `</div></div><div class="metric-card"><div class="metric-label">` + esc(t(lang, "通知", "Notifications")) + `</div><div class="metric-value">` + esc(t(lang, "送信せずに確認", "Check without sending")) + `</div></div></div><div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(tr(lang, "ia.productions")) + `</a></div></section><details class="advanced-details"><summary>` + esc(tr(lang, "ia.advanced")) + `</summary><p class="hint">` + esc(t(lang, "技術的な診断情報はここに限定します。", "Technical diagnostic information is limited to this disclosure.")) + `</p></details></div>`
	_ = db
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.system_status"), r, body))
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
		rows.WriteString(`<tr><td>` + esc(log.CreatedAt.Format("2006-01-02 15:04")) + `</td><td>` + esc(log.ProjectName) + `</td><td>` + esc(log.EntityName) + `</td><td>` + esc(t(lang, "未記録", "Not recorded")) + `</td><td><span class="status-pill ` + map[bool]string{true: "ok", false: "bad"}[log.Success] + `">` + esc(result) + `</span></td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="5" class="muted">` + esc(t(lang, "監査ログはありません。", "No audit log entries.")) + `</td></tr>`)
	}
	body := `<section class="section-card glass"><p class="hint">` + esc(t(lang, "設定変更、通知、問題と復旧の履歴を確認できます。技術的な識別子は詳細表示に限定しています。", "Review configuration, notification, failure, and recovery history. Technical identifiers remain in details.")) + `</p><div class="table-wrap"><table><thead><tr><th>` + esc(t(lang, "日時", "Date and time")) + `</th><th>Production</th><th>` + esc(t(lang, "操作内容", "Action")) + `</th><th>` + esc(t(lang, "実行ユーザー", "Acting user")) + `</th><th>` + esc(t(lang, "結果", "Result")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></div></section>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.audit_log"), r, body))
}

func renderIAUsers(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	var rows strings.Builder
	for _, u := range model.ListUserMap(db) {
		rows.WriteString(`<tr><td>` + esc(u.KitsuName) + `</td><td>` + esc(t(lang, "対応付け済み", "Connected")) + `</td></tr>`)
	}
	if rows.Len() == 0 {
		rows.WriteString(`<tr><td colspan="2" class="muted">` + esc(t(lang, "ユーザー対応付けはありません。", "No user mappings yet.")) + `</td></tr>`)
	}
	body := `<section class="section-card glass"><p class="hint">` + esc(t(lang, "KitsuユーザーとDiscordユーザーの対応付けだけを管理します。Reviewer / Checkerは選択Productionのユーザー設定で管理します。", "Manage only Kitsu user to Discord user correspondence. Reviewer / Checker belongs in the selected Production's user settings.")) + `</p><table><thead><tr><th>` + esc(t(lang, "Kitsuユーザー", "Kitsu user")) + `</th><th>` + esc(t(lang, "Discordユーザー", "Discord user")) + `</th></tr></thead><tbody>` + rows.String() + `</tbody></table></section>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.user_mapping"), r, body))
}

func renderIANewConnection(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	lang := currentLang(r)
	var options strings.Builder
	for _, p := range model.ListProjects(db) {
		options.WriteString(`<li><strong>` + esc(p.Name) + `</strong><span class="status-pill">` + esc(t(lang, "接続済み", "Connected")) + `</span></li>`)
	}
	if options.Len() == 0 {
		options.WriteString(`<li class="muted">` + esc(t(lang, "Productionを選択できます。", "Productions will appear here when available.")) + `</li>`)
	}
	body := `<div class="section-stack"><section class="section-card glass"><h2>` + esc(t(lang, "接続の手順", "Connection flow")) + `</h2><ol class="step-list"><li>` + esc(t(lang, "Kitsu Productionを選択", "Select a Kitsu Production")) + `</li><li>` + esc(t(lang, "Discordサーバーを選択", "Select a Discord server")) + `</li><li>` + esc(t(lang, "作成または再利用するチャンネルを確認", "Review channels to create or reuse")) + `</li><li>` + esc(t(lang, "計画を確認してから実行", "Confirm the exact plan before execution")) + `</li><li>` + esc(t(lang, "完了後、選択したProductionを開く", "Open the selected Production after completion")) + `</li></ol><p class="hint">` + esc(t(lang, "チャンネルの作成・再利用・競合は完全な計画として表示され、明示的な確認前にはDiscordへ書き込みません。", "Create, reuse, and conflict results are shown as a complete plan. No Discord write occurs before explicit confirmation.")) + `</p></section><section class="section-card glass"><h2>` + esc(t(lang, "接続済みProduction", "Connected Productions")) + `</h2><ul class="list-tight">` + options.String() + `</ul><div class="button-row"><a class="btn" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(tr(lang, "ia.productions")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(tr(lang, "ia.bot_connection")) + `</a></div></section></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "ia.new_connection"), r, body))
}
