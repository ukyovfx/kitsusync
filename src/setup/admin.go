package setup

import (
	"app/src/model"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

var discordIDRegexp = regexp.MustCompile(`^[0-9]{17,19}$`)

func AdminIndex(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)

		snap := Stats.Snapshot()
		unhealthy := Stats.WebhookHealthList()
		pollErr := Stats.LastPollError()

		fmtAgo := func(ts time.Time) string {
			if ts.IsZero() {
				return t(lang, "\u672a\u5b9f\u884c", "never")
			}
			ago := time.Since(ts).Round(time.Second)
			return ago.String() + " " + t(lang, "\u524d", "ago")
		}

		// ---- System status bar ----
		pollingActive := !snap.LastPollTime.IsZero()
		hasBrokenWebhook := len(unhealthy) > 0
		hasPollErr := pollErr != ""
		hasIssues := hasBrokenWebhook || hasPollErr || !pollingActive
		isHealthy := pollingActive && !hasBrokenWebhook && !hasPollErr

		statusPill := `<span class="status-pill ok">` + esc(t(lang, "\u6b63\u5e38", "Healthy")) + `</span>`
		if !isHealthy {
			statusPill = `<span class="status-pill bad">✗ ` + esc(t(lang, "\u554f\u984c\u3042\u308a", "Issues detected")) + `</span>`
		}

		lastSyncText := t(lang, "\u672a\u540c\u671f", "No sync yet")
		if !snap.LastPollTime.IsZero() {
			lastSyncText = t(lang, "\u6700\u7d42\u540c\u671f: ", "Last sync: ") + fmtAgo(snap.LastPollTime)
		}

		statusBar := fmt.Sprintf(`
<div class="section-card glass">
  <div style="display:flex;align-items:center;gap:16px;flex-wrap:wrap">
    %s
    <span style="color:var(--muted);font-size:.95rem;flex:1">%s</span>
    <a class="btn-ghost" href="%s" style="font-size:.85rem">%s</a>
  </div>
</div>`,
			statusPill,
			esc(lastSyncText),
			withLang("/bot/admin", r),
			esc(t(lang, "\u66f4\u65b0", "Refresh")),
		)

		// ---- Poller status card ----
		pollerIcon := "\U0001F7E2"
		pollerLabel := t(lang, "\u7a3c\u50cd\u4e2d", "Running")
		if !pollingActive {
			pollerIcon = "\U0001F534"
			pollerLabel = t(lang, "\u672a\u30dd\u30fc\u30ea\u30f3\u30b0", "No polls yet")
		}

		var pollerDetails strings.Builder
		pollerDetails.WriteString(fmt.Sprintf(`<div class="metric-grid">
  <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%s %s</div></div>
  <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%s</div></div>
  <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
  <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
</div>`,
			esc(t(lang, "\u72b6\u614b", "Status")), pollerIcon, esc(pollerLabel),
			esc(t(lang, "\u6700\u7d42\u30dd\u30fc\u30ea\u30f3\u30b0", "Last poll")), esc(fmtAgo(snap.LastPollTime)),
			esc(t(lang, "\u6700\u7d42\u30bf\u30b9\u30af\u6570", "Last task count")), snap.LastPollTaskCount,
			esc(t(lang, "\u7d2f\u8a08\u30dd\u30fc\u30ea\u30f3\u30b0\u56de\u6570", "Total polls")), snap.PollCount,
		))
		if hasPollErr {
			pollerDetails.WriteString(fmt.Sprintf(
				`<div class="status-pill bad" style="margin-top:10px;font-size:.82rem;display:inline-flex;max-width:100%%;white-space:normal;align-items:flex-start">⚠ %s</div>`,
				esc(pollErr),
			))
		}

		pollerCard := fmt.Sprintf(`
<div class="section-card glass">
  <div class="page-heading" style="margin-bottom:14px">
    <h3 style="margin:0">%s</h3>
    <a class="btn-ghost" href="%s" style="font-size:.82rem">%s</a>
  </div>
  %s
</div>`,
			esc(t(lang, "\u30dd\u30fc\u30e9\u30fc", "Poller")),
			withLang("/bot/admin/health", r),
			esc(t(lang, "\u8a73\u7d30 \u2192", "Details \u2192")),
			pollerDetails.String(),
		)

		// ---- Active projects ----
		projects := model.ListProjects(db)
		allWebhooks := model.ListAllProjectWebhooks(db)

		type projectStats struct {
			channelCount int
			webhookCount int
		}
		pStats := make(map[string]projectStats)
		for _, wh := range allWebhooks {
			ps := pStats[wh.KitsuProjectID]
			if wh.DiscordChannelID != "" {
				ps.channelCount++
			}
			if wh.TaskType != "" {
				ps.webhookCount++
			}
			pStats[wh.KitsuProjectID] = ps
		}

		projectCount := len(projects)

		var projectsCard string
		if projectCount == 0 {
			projectsCard = fmt.Sprintf(`
<div class="section-card glass">
  <h3>%s</h3>
  %s
  <p class="hint" style="margin-top:12px">%s</p>
</div>`,
				esc(t(lang, "\u30a2\u30af\u30c6\u30a3\u30d6\u30d7\u30ed\u30b8\u30a7\u30af\u30c8", "Active Projects")),
				emptyState("\U0001F3AC", t(lang, "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u672a\u8a2d\u5b9a", "No projects configured"), t(lang, "Project Management \u304b\u3089\u6700\u521d\u306e\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u7d4c\u8def\u3092\u8a2d\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044\u3002", "Use Project Management to configure your first project routing.")),
				esc(t(lang, "Next \u30bb\u30af\u30b7\u30e7\u30f3\u304b\u3089 Project Management \u3092\u958b\u3044\u3066\u304f\u3060\u3055\u3044\u3002", "Open Project Management from the Next section.")),
			)
		} else {
			var projectRows strings.Builder
			for _, proj := range projects {
				ps := pStats[proj.KitsuProjectID]
				projectRows.WriteString(fmt.Sprintf(`
<tr>
  <td><strong>%s</strong></td>
  <td>%s</td>
  <td style="text-align:center">%d</td>
  <td style="text-align:center">%d</td>
  <td><a class="btn-ghost" href="%s" style="font-size:.8rem">%s</a></td>
</tr>`,
					esc(proj.Name),
					esc(strings.ToUpper(proj.ProjectType)),
					ps.channelCount,
					ps.webhookCount,
					withLang("/bot/setup", r)+"&project="+url.QueryEscape(proj.KitsuProjectID),
					esc(t(lang, "\u7ba1\u7406", "Manage")),
				))
			}
			projectsCard = fmt.Sprintf(`
<div class="section-card glass">
  <div class="page-heading" style="margin-bottom:14px">
    <h3 style="margin:0">%s</h3>
    <a class="btn-ghost" href="%s" style="font-size:.82rem">%s</a>
  </div>
  <div class="table-wrap">
    <table>
      <thead><tr>
        <th>%s</th><th>%s</th>
        <th style="text-align:center">%s</th>
        <th style="text-align:center">%s</th>
        <th></th>
      </tr></thead>
      <tbody>%s</tbody>
    </table>
  </div>
</div>`,
				esc(t(lang, "\u30a2\u30af\u30c6\u30a3\u30d6\u30d7\u30ed\u30b8\u30a7\u30af\u30c8", "Active Projects")),
				withLang("/bot/setup", r),
				esc(t(lang, "Project Management \u2192", "Project Management \u2192")),
				esc(t(lang, "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u540d", "Project")),
				esc(t(lang, "\u30c6\u30f3\u30d7\u30ec\u30fc\u30c8", "Template")),
				esc(t(lang, "\u30c1\u30e3\u30f3\u30cd\u30eb\u6570", "Channels")),
				esc(t(lang, "Webhook \u6570", "Webhooks")),
				projectRows.String(),
			)
		}

		// ---- Warnings ----
		var warningsCard string
		if hasBrokenWebhook || hasPollErr {
			var warns strings.Builder
			warns.WriteString(`<div class="section-card glass" style="border-color:rgba(255,106,80,.35)">`)
			warns.WriteString(`<h3 style="color:var(--danger)">⚠ ` + esc(t(lang, "\u8b66\u544a", "Warnings")) + `</h3>`)
			warns.WriteString(`<div class="section-stack" style="gap:8px">`)
			if hasPollErr {
				warns.WriteString(fmt.Sprintf(
					`<div class="status-badge badge-err">%s: %s \u2014 <a href="%s" style="color:inherit">%s</a></div>`,
					esc(t(lang, "\u30dd\u30fc\u30e9\u30fc\u30a8\u30e9\u30fc", "Poller error")),
					esc(pollErr),
					withLang("/bot/admin/health", r),
					esc(t(lang, "\u8a73\u7d30", "Details")),
				))
			}
			for _, entry := range unhealthy {
				shortURL := entry.URL
				if len(shortURL) > 40 {
					shortURL = shortURL[:40] + "\u2026"
				}
				warns.WriteString(fmt.Sprintf(
					`<div class="status-badge badge-err">%s <code>%s</code> \u2014 %s %d \u2014 <a href="%s" style="color:inherit">%s</a></div>`,
					esc(t(lang, "Webhook \u969c\u5bb3:", "Webhook failing:")),
					esc(shortURL),
					esc(t(lang, "\u5931\u6557\u6570:", "failures:")),
					entry.FailureCount,
					withLang("/bot/admin/health", r),
					esc(t(lang, "\u518d\u63a5\u7d9a", "Reconnect")),
				))
			}
			warns.WriteString(`</div></div>`)
			warningsCard = warns.String()
		}

		// ---- Quick navigation ----
		type navLink struct {
			icon, href, titleJA, titleEN string
		}
		links := []navLink{
			{"\U0001F5C2", "/bot/admin/projects", "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u3068Guild", "Projects & Guilds"},
			{"\u2764", "/bot/admin/health", "\u30d8\u30eb\u30b9", "Health"},
			{"\U0001F50D", "/bot/admin/diagnostics", "\u74b0\u5883\u8a3a\u65ad", "Diagnostics"},
			{"\U0001F464", "/bot/admin/users", "\u30e6\u30fc\u30b6\u30fc\u5272\u308a\u5f53\u3066", "Users"},
			{"\u2705", "/bot/admin/checkers", "\u30ec\u30d3\u30e5\u30a2\u30fc\u5272\u308a\u5f53\u3066", "Reviewers"},
			{"\U0001F916", "/bot/admin/bot", "Bot\u8a2d\u5b9a", "Bot Settings"},
			{"\U0001F4C1", "/bot/admin/drive", "\u30b9\u30c8\u30ec\u30fc\u30b8", "Storage"},
			{"\U0001F9FE", "/bot/admin/audit", "\u76e3\u67fb\u30ed\u30b0 (v0.2+)", "Audit Log (v0.2+)"},
		}
		var navGrid strings.Builder
		navGrid.WriteString(`<div class="dashboard-grid">`)
		for _, lnk := range links {
			navGrid.WriteString(fmt.Sprintf(
				`<a class="tile glass" href="%s"><div class="tile-icon">%s</div><div class="tile-title">%s</div></a>`,
				withLang(lnk.href, r), lnk.icon, t(lang, lnk.titleJA, lnk.titleEN),
			))
		}
		navGrid.WriteString(`</div>`)

		setupSummaryTitle := t(lang, "\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u72b6\u614b", "Setup / Project State")
		setupSummaryText := t(lang, "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u3092\u8ffd\u52a0\u3059\u308b\u3068\u3001\u3053\u3053\u304b\u3089 Discord \u3078\u306e\u7d4c\u8def\u8a2d\u5b9a\u3092\u958b\u59cb\u3067\u304d\u307e\u3059\u3002", "Add your first project to begin Discord routing from here.")
		setupSummaryState := `<span class="status-pill warn">` + esc(t(lang, "\u672a\u8a2d\u5b9a", "Not started")) + `</span>`
		if projectCount > 0 {
			setupSummaryText = fmt.Sprintf(
				t(lang, "%d \u4ef6\u306e\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u304c\u8a2d\u5b9a\u6e08\u307f\u3067\u3059\u3002\u5fc5\u8981\u306b\u5fdc\u3058\u3066 Project Management \u3067\u30c1\u30e3\u30f3\u30cd\u30eb / Webhook \u3092\u898b\u76f4\u3057\u3066\u304f\u3060\u3055\u3044\u3002", "%d project(s) are configured. Use Project Management when you need to review channels or webhooks."),
				projectCount,
			)
			setupSummaryState = `<span class="status-pill ok">` + esc(t(lang, "\u8a2d\u5b9a\u6e08\u307f", "Configured")) + `</span>`
		}
		if hasIssues {
			setupSummaryText = t(lang, "\u73fe\u5728\u306e\u72b6\u614b\u306b\u554f\u984c\u304c\u3042\u308a\u307e\u3059\u3002\u307e\u305a\u306f Health / Diagnostics \u3067\u7a3c\u50cd\u72b6\u614b\u3092\u78ba\u8a8d\u3057\u3066\u304f\u3060\u3055\u3044\u3002", "There are issues in the current state. Check Health / Diagnostics first before moving on.")
			setupSummaryState = `<span class="status-pill bad">` + esc(t(lang, "\u8981\u78ba\u8a8d", "Needs attention")) + `</span>`
		}
		setupSummaryCard := fmt.Sprintf(`
<div class="section-card glass">
  <div class="page-heading" style="margin-bottom:10px">
    <h3 style="margin:0">%s</h3>
    %s
  </div>
  <p class="hint" style="margin:0">%s</p>
</div>`,
			esc(setupSummaryTitle),
			setupSummaryState,
			esc(setupSummaryText),
		)

		nextTitle := t(lang, "\u6b21\u306b\u3084\u308b\u3053\u3068", "Next")
		nextCardTitle := t(lang, "\u72b6\u614b\u3092\u898b\u76f4\u3059", "Review system status")
		nextCardBody := t(lang, "\u307e\u305a\u306f Health \u3068 Diagnostics \u3067\u554f\u984c\u306e\u3042\u308b\u7a3c\u50cd\u72b6\u614b\u3092\u78ba\u8a8d\u3057\u3001\u540c\u671f\u3084 webhook \u306e\u72b6\u614b\u3092\u6574\u7406\u3057\u3066\u304f\u3060\u3055\u3044\u3002", "Start with Health and Diagnostics to review runtime issues, sync status, and webhook health.")
		nextPrimaryHref := withLang("/bot/admin/health", r)
		nextPrimaryLabel := t(lang, "Health \u3092\u958b\u304f", "Open Health")
		nextSecondaryHref := withLang("/bot/admin/diagnostics", r)
		nextSecondaryLabel := t(lang, "Diagnostics \u3092\u958b\u304f", "Open Diagnostics")
		nextBadge := `<span class="status-pill bad">` + esc(t(lang, "\u512a\u5148", "Priority")) + `</span>`
		if !hasIssues && projectCount == 0 {
			nextCardTitle = t(lang, "Project Management \u3092\u958b\u304f", "Open Project Management")
			nextCardBody = t(lang, "\u6700\u521d\u306e\u30d7\u30ed\u30b8\u30a7\u30af\u30c8 routing \u306f Project Management \u304b\u3089\u59cb\u3081\u307e\u3059\u3002\u5171\u6709 Bot / Runtime \u306e\u524d\u63d0\u78ba\u8a8d\u306f Bot Settings \u304b\u3089\u884c\u3044\u3001\u8a73\u3057\u3044\u78ba\u8a8d\u304c\u5fc5\u8981\u306a\u5834\u5408\u306f Manual Setup / Diagnostics \u3092\u4f7f\u3063\u3066\u304f\u3060\u3055\u3044\u3002", "Start your first project routing in Project Management. Use Bot Settings to review shared bot / runtime prerequisites, and use Manual Setup / Diagnostics only when you need deeper checks.")
			nextPrimaryHref = withLang("/bot/setup", r)
			nextPrimaryLabel = t(lang, "Project Management \u3092\u958b\u304f", "Open Project Management")
			nextSecondaryHref = withLang("/bot/admin/bot", r)
			nextSecondaryLabel = t(lang, "Bot Settings \u3092\u78ba\u8a8d", "Review Bot Settings")
			nextBadge = `<span class="status-pill warn">` + esc(t(lang, "\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7", "Setup")) + `</span>`
		} else if !hasIssues && projectCount > 0 {
			nextCardTitle = t(lang, "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u3092\u7ba1\u7406\u3059\u308b", "Manage project routing")
			nextCardBody = t(lang, "\u30c1\u30e3\u30f3\u30cd\u30eb\u3001Webhook\u3001\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u3054\u3068\u306e Discord \u7d4c\u8def\u306e\u78ba\u8a8d\u306f Project Management \u304b\u3089\u884c\u3048\u307e\u3059\u3002", "Use Project Management to review channels, webhooks, and project-specific Discord routing.")
			nextPrimaryHref = withLang("/bot/setup", r)
			nextPrimaryLabel = t(lang, "Project Management \u3092\u958b\u304f", "Open Project Management")
			nextSecondaryHref = withLang("/bot/admin/projects", r)
			nextSecondaryLabel = t(lang, "Projects & Guilds \u3092\u78ba\u8a8d", "Review Projects & Guilds")
			nextBadge = `<span class="status-pill ok">` + esc(t(lang, "\u6e96\u5099\u6e08\u307f", "Ready")) + `</span>`
		}
		nextActionCard := fmt.Sprintf(`
<div class="section-card glass" style="border-color:rgba(255,141,72,.35);box-shadow:0 0 0 1px rgba(255,141,72,.14) inset">
  <div class="page-heading" style="margin-bottom:12px">
    <div>
      <h3 style="margin:0">%s</h3>
      <p class="hint" style="margin:6px 0 0">%s</p>
    </div>
    %s
  </div>
  <div class="button-row" style="margin-top:14px">
    <a class="btn" href="%s">%s</a>
    <a class="btn-ghost" href="%s">%s</a>
  </div>
</div>`,
			esc(nextCardTitle),
			esc(nextCardBody),
			nextBadge,
			nextPrimaryHref,
			esc(nextPrimaryLabel),
			nextSecondaryHref,
			esc(nextSecondaryLabel),
		)

		body := `<div class="section-stack">` +
			`<div><div class="eyebrow">NOW</div><h2 style="margin:6px 0 0">` + esc(t(lang, "\u73fe\u5728\u306e\u72b6\u614b", "Now")) + `</h2></div>` +
			statusBar + pollerCard + warningsCard + setupSummaryCard + projectsCard +
			`<div><div class="eyebrow">NEXT</div><h2 style="margin:6px 0 0">` + esc(nextTitle) + `</h2></div>` +
			nextActionCard +
			`<div class="section-card glass"><div class="page-heading" style="margin-bottom:14px"><div><div class="eyebrow">LATER</div><h3 style="margin:0">` + esc(t(lang, "\u5f8c\u3067\u4f7f\u3046\u7ba1\u7406\u6a5f\u80fd", "Later / Advanced")) + `</h3><p class="hint" style="margin:6px 0 0">` + esc(t(lang, "\u3053\u308c\u3089\u306f\u521d\u56de\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u5b8c\u4e86\u5f8c\u3084\u500b\u5225\u8abf\u6574\u306e\u3068\u304d\u306b\u4f7f\u3046\u7ba1\u7406\u30da\u30fc\u30b8\u3067\u3059\u3002", "These are secondary admin pages for follow-up setup, maintenance, and detailed adjustments.")) + `</p></div></div>` + navGrid.String() + `</div>` +
			`</div>`

		fmt.Fprint(w, adminPage(lang, t(lang, "\u30c0\u30c3\u30b7\u30e5\u30dc\u30fc\u30c9", "Dashboard"), r, body))
	}
}

func AdminProjectsHandler(db *gorm.DB, fallbackGuildID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		fallbackGuildID = strings.TrimSpace(fallbackGuildID)

		if r.Method == http.MethodPost {
			projectID := strings.TrimSpace(r.FormValue("project_id"))
			guildID := strings.TrimSpace(r.FormValue("guild_id"))
			if projectID != "" {
				if err := model.UpdateProjectGuildID(db, projectID, guildID); err == nil {
					http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=saved", http.StatusSeeOther)
					return
				}
			}
			http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=error", http.StatusSeeOther)
			return
		}

		var blocks strings.Builder
		for _, p := range model.ListProjects(db) {
			effectiveGuildID := strings.TrimSpace(p.DiscordGuildID)
			if effectiveGuildID == "" {
				effectiveGuildID = fallbackGuildID
			}
			blocks.WriteString(fmt.Sprintf(`
<form method="POST" class="section-card glass">
  <input type="hidden" name="project_id" value="%s">
  <div class="page-heading"><div><h3 style="margin:0">%s</h3><p class="hint">%s: <code>%s</code></p></div></div>
  <div class="form-grid">
    <div><label>Discord Guild ID</label><input type="text" name="guild_id" value="%s" placeholder="123456789012345678"></div>
  </div>
  <div class="button-row"><button type="submit" class="btn">%s</button></div>
</form>`,
				esc(p.KitsuProjectID),
				esc(p.Name),
				esc(t(lang, "Project ID", "Project ID")),
				esc(p.KitsuProjectID),
				esc(effectiveGuildID),
				esc(t(lang, "保存", "Save")),
			))
		}
		if blocks.Len() == 0 {
			blocks.WriteString(emptyState("\U0001F5C2", t(lang, "まだプロジェクトがありません", "No projects configured yet."), t(lang, "先に Project Management で routing と Guild ID を設定してから、ここで review / edit を行ってください。", "Set routing and Guild ID in Project Management first, then use this page for review / edit.")))
		}
		body := `<div class="section-stack"><div class="section-card glass"><p class="hint">` + esc(t(lang, "このページは既存 project の Discord Server / Guild 割り当てを review / edit するための管理画面です。新しい project の Guild ID は Project Management setup 側で入力してください。", "Use this page to review or edit Discord Server / Guild assignment for existing projects. Enter the Guild ID for new projects in Project Management setup.")) + `</p></div>` + blocks.String() + `</div>`
		fmt.Fprint(w, adminPage(lang, t(lang, "Projects & Guilds", "Projects & Guilds"), r, body))
	}
}

// HealthHandler shows the runtime health dashboard.
// Level 1 (default): overall status + last sync.
// Level 2 (details accordion): polling, sends, webhook table, resource usage.
// Broken webhooks auto-expand the details accordion.
// POST action=reconnect_webhook&webhook_id=N — recreates the Discord webhook for that channel.
func HealthHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)

		// Handle reconnect action
		if r.Method == http.MethodPost && r.FormValue("action") == "reconnect_webhook" {
			webhookID := parseUint(r.FormValue("webhook_id"))
			wh := model.FindProjectWebhookByID(db, webhookID)
			if wh == nil || wh.DiscordChannelID == "" {
				http.Redirect(w, r, withLang("/bot/admin/health", r)+"&msg=reconnect_fail", http.StatusSeeOther)
				return
			}
			botToken := os.Getenv("DISCORD_BOT_TOKEN")
			newURL, err := CreateWebhook(wh.DiscordChannelID, wh.ChannelName, botToken)
			if err != nil {
				slog.Warn("Webhook reconnect failed", "channelID", wh.DiscordChannelID, "err", err)
				http.Redirect(w, r, withLang("/bot/admin/health", r)+"&msg=reconnect_fail", http.StatusSeeOther)
				return
			}
			if err := model.UpdateProjectWebhookURL(db, wh.ID, newURL); err != nil {
				http.Redirect(w, r, withLang("/bot/admin/health", r)+"&msg=reconnect_fail", http.StatusSeeOther)
				return
			}
			// Clear failure counters for the old URL so health shows clean immediately
			Stats.RecordSend(1, 0, newURL, "")
			slog.Info("Webhook reconnected", "channelName", wh.ChannelName, "newURL", newURL[:30]+"…")
			http.Redirect(w, r, withLang("/bot/admin/health", r)+"&msg=reconnect_ok", http.StatusSeeOther)
			return
		}

		snap := Stats.Snapshot()
		unhealthy := Stats.WebhookHealthList()
		uptime := time.Since(snap.StartTime).Round(time.Second)

		// Memory usage via runtime stats
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		memMB := mem.Alloc / 1024 / 1024

		fmtTime := func(t time.Time) string {
			if t.IsZero() {
				return "\u2014"
			}
			ago := time.Since(t).Round(time.Second)
			return t.Format("2006-01-02 15:04:05") + " (" + ago.String() + " ago)"
		}
		fmtAgo := func(ts time.Time) string {
			if ts.IsZero() {
				return t(lang, "\u672a\u5b9f\u884c", "never")
			}
			ago := time.Since(ts).Round(time.Second)
			return ago.String() + " " + t(lang, "\u524d", "ago")
		}

		// Overall health determination
		hasBrokenWebhook := len(unhealthy) > 0
		pollingActive := !snap.LastPollTime.IsZero()
		isHealthy := pollingActive && !hasBrokenWebhook

		overallBadge := func() string {
			if isHealthy {
				return `<span class="status-pill ok" style="font-size:1rem;padding:10px 18px">` + esc(t(lang, "\u6b63\u5e38", "Healthy")) + `</span>`
			}
			return `<span class="status-pill bad" style="font-size:1rem;padding:10px 18px">✗ ` + esc(t(lang, "\u554f\u984c\u3042\u308a", "Issues detected")) + `</span>`
		}

		lastSyncText := func() string {
			if snap.LastPollTime.IsZero() {
				return t(lang, "\u540c\u671f\u672a\u5b9f\u884c", "No sync yet")
			}
			return t(lang, "\u6700\u7d42\u540c\u671f:", "Last sync:") + " " + fmtAgo(snap.LastPollTime)
		}

		// Level 1: summary card
		summaryCard := fmt.Sprintf(`
<div class="section-card glass">
  <div style="display:flex;align-items:center;gap:16px;flex-wrap:wrap">
    %s
    <span style="color:var(--muted);font-size:.95rem;flex:1">%s</span>
    <a class="btn-ghost" href="%s" style="font-size:.85rem">%s</a>
  </div>
</div>`,
			overallBadge(),
			esc(lastSyncText()),
			withLang("/bot/admin/health", r), esc(t(lang, "\u66f4\u65b0", "Refresh")),
		)

		// --- Level 2: detailed sections (inside accordion) ---

		// Polling rows
		pollStatusBadge := `<span class="status-pill ok">` + esc(t(lang, "\u7a3c\u50cd\u4e2d", "Active")) + `</span>`
		if !pollingActive {
			pollStatusBadge = `<span class="status-pill bad">` + esc(t(lang, "\u672a\u5b9f\u884c", "No polls yet")) + `</span>`
		}
		pollRows := fmt.Sprintf(`
<tr><td>%s</td><td>%s</td></tr>
<tr><td>%s</td><td>%d</td></tr>
<tr><td>%s</td><td>%s</td></tr>
<tr><td>%s</td><td>%d</td></tr>`,
			esc(t(lang, "\u30dd\u30fc\u30ea\u30f3\u30b0\u72b6\u614b", "Polling status")), pollStatusBadge,
			esc(t(lang, "\u5b9f\u884c\u56de\u6570 (\u8d77\u52d5\u5f8c)", "Poll count (since startup)")), snap.PollCount,
			esc(t(lang, "\u6700\u7d42\u30dd\u30fc\u30ea\u30f3\u30b0", "Last poll")), esc(fmtTime(snap.LastPollTime)),
			esc(t(lang, "\u6700\u7d42\u30dd\u30fc\u30ea\u30f3\u30b0: \u30bf\u30b9\u30af\u6570", "Last poll: task count")), snap.LastPollTaskCount,
		)

		// Send rows
		sendStatusBadge := `<span class="status-pill ok">` + esc(t(lang, "\u6b63\u5e38", "OK")) + `</span>`
		if snap.SendFailureTotal > 0 && snap.SendSuccessTotal == 0 {
			sendStatusBadge = `<span class="status-pill bad">` + esc(t(lang, "\u5931\u6557\u3042\u308a", "Failures")) + `</span>`
		}
		sendRows := fmt.Sprintf(`
<tr><td>%s</td><td>%s</td></tr>
<tr><td>%s</td><td>%d</td></tr>
<tr><td>%s</td><td>%d</td></tr>
<tr><td>%s</td><td>%s</td></tr>`,
			esc(t(lang, "\u9001\u4fe1\u72b6\u614b", "Send status")), sendStatusBadge,
			esc(t(lang, "\u9001\u4fe1\u6210\u529f (\u8d77\u52d5\u5f8c)", "Sent (since startup)")), snap.SendSuccessTotal,
			esc(t(lang, "\u9001\u4fe1\u5931\u6557 (\u8d77\u52d5\u5f8c)", "Failed (since startup)")), snap.SendFailureTotal,
			esc(t(lang, "\u6700\u7d42\u9001\u4fe1", "Last send")), esc(fmtTime(snap.LastSendTime)),
		)

		// Webhook health table
		allWebhooks := model.ListAllProjectWebhooks(db)
		projects := model.ListProjects(db)
		projectNames := make(map[string]string)
		for _, p := range projects {
			projectNames[p.KitsuProjectID] = p.Name
		}
		failedURLs := make(map[string]WebhookHealthEntry)
		for _, e := range unhealthy {
			failedURLs[e.URL] = e
		}

		reconnectMsg := r.URL.Query().Get("msg")

		var webhookRows strings.Builder
		for _, wh := range allWebhooks {
			projName := projectNames[wh.KitsuProjectID]
			if projName == "" {
				projName = wh.KitsuProjectID
			}
			lastSend := Stats.WebhookLastSend(wh.WebhookURL)
			lastSendCell := `<span style="color:var(--muted-2)">\u2014</span>`
			if !lastSend.IsZero() {
				lastSendCell = esc(fmtAgo(lastSend))
			}

			var statusCell string
			var actionCell string
			if entry, bad := failedURLs[wh.WebhookURL]; bad {
				statusCell = fmt.Sprintf(`<span class="status-pill bad">%s %d</span>`,
					esc(t(lang, "\u5931\u6557:", "Failed:")), entry.FailureCount)
				if entry.LastError != "" {
					statusCell += `<div style="font-size:.78rem;color:var(--muted-2);margin-top:3px">` + html.EscapeString(entry.LastError) + `</div>`
				}
				// Reconnect button \u2014 only if channel ID exists (channel not deleted)
				if wh.DiscordChannelID != "" {
					actionCell = fmt.Sprintf(`<form method="POST"><input type="hidden" name="action" value="reconnect_webhook"><input type="hidden" name="webhook_id" value="%d"><button class="btn-sm" type="submit">%s</button></form>`,
						wh.ID, esc(t(lang, "\u518d\u63a5\u7d9a", "Reconnect")))
				}
			} else if Stats.WebhookInactive(wh.WebhookURL, 7*24*time.Hour) {
				statusCell = `<span class="status-pill" style="background:rgba(255,200,80,.14);color:#ffc850;border-color:rgba(255,200,80,.3)">` +
					esc(t(lang, "\u26a0\ufe0f 7\u65e5\u4ee5\u4e0a\u672a\u9001\u4fe1", "\u26a0\ufe0f No activity 7d+")) + `</span>`
			} else {
				statusCell = `<span class="status-pill ok">` + esc(t(lang, "\u6b63\u5e38", "OK")) + `</span>`
			}
			webhookRows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>#%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				esc(projName), esc(wh.ChannelName), esc(wh.TaskType), statusCell, lastSendCell, actionCell))
		}

		var webhookTableHTML string
		if len(allWebhooks) == 0 {
			webhookTableHTML = `<p class="hint">` + esc(t(lang, "Webhook\u306a\u3057\uff08\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u8a2d\u5b9a\u5f8c\u306b\u8868\u793a\uff09", "No webhooks configured (shown after project setup).")) + `</p>`
		} else {
			webhookTableHTML = `<div class="table-wrap"><table><thead><tr>` +
				`<th>` + esc(t(lang, "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8", "Project")) + `</th>` +
				`<th>` + esc(t(lang, "\u30c1\u30e3\u30f3\u30cd\u30eb", "Channel")) + `</th>` +
				`<th>` + esc(t(lang, "\u30bf\u30b9\u30af\u7a2e\u5225", "Task type")) + `</th>` +
				`<th>` + esc(t(lang, "\u72b6\u614b", "Status")) + `</th>` +
				`<th>` + esc(t(lang, "\u6700\u7d42\u9001\u4fe1", "Last send")) + `</th>` +
				`<th></th>` +
				`</tr></thead><tbody>` + webhookRows.String() + `</tbody></table></div>`
			if hasBrokenWebhook {
				webhookTableHTML += `<p class="hint" style="color:#ff6a50;margin-top:10px">` +
					esc(t(lang, "\u5931\u6557\u4e2d\u306eWebhook\u304c\u3042\u308a\u307e\u3059\u3002\u30c1\u30e3\u30f3\u30cd\u30eb\u304c\u5b58\u5728\u3059\u308b\u5834\u5408\u306f\u300c\u518d\u63a5\u7d9a\u300d\u3067\u4fee\u5fa9\u3067\u304d\u307e\u3059\u3002",
						"Some webhooks are failing. If the channel still exists, use Reconnect to repair.")) + `</p>`
			}
			if reconnectMsg == "reconnect_ok" {
				webhookTableHTML += `<p class="hint" style="color:#8ecf8b;margin-top:10px">✓ ` + esc(t(lang, "Webhook\u3092\u518d\u63a5\u7d9a\u3057\u307e\u3057\u305f\u3002", "Webhook reconnected successfully.")) + `</p>`
			} else if reconnectMsg == "reconnect_fail" {
				webhookTableHTML += `<p class="hint" style="color:#ff6a50;margin-top:10px">✗ ` + esc(t(lang, "\u518d\u63a5\u7d9a\u306b\u5931\u6557\u3057\u307e\u3057\u305f\u3002\u30c1\u30e3\u30f3\u30cd\u30eb\u304c\u524a\u9664\u3055\u308c\u3066\u3044\u308b\u53ef\u80fd\u6027\u304c\u3042\u308a\u307e\u3059\u3002", "Reconnect failed. The channel may have been deleted.")) + `</p>`
			}
		}

		// Resource usage card
		resourceRows := fmt.Sprintf(`
<tr><td>%s</td><td>%s</td></tr>
<tr><td>%s</td><td>%d MB</td></tr>
<tr><td>%s</td><td>%s</td></tr>`,
			esc(t(lang, "\u8d77\u52d5\u6642\u523b", "Started")), esc(snap.StartTime.Format("2006-01-02 15:04:05")),
			esc(t(lang, "\u30e1\u30e2\u30ea\u4f7f\u7528\u91cf", "Memory usage")), memMB,
			esc(t(lang, "\u7a3c\u50cd\u6642\u9593", "Uptime")), esc(uptime.String()),
		)

		// auto-open accordion when broken
		detailsOpen := ""
		if hasBrokenWebhook || !pollingActive {
			detailsOpen = " open"
		}

		detailsAccordion := fmt.Sprintf(`
<details class="accordion"%s>
  <summary>
    <div class="accordion-summary-main">
      <div class="tile-title">%s</div>
    </div>
    <div class="accordion-summary-side">
      <span class="accordion-caret">⌄</span>
    </div>
  </summary>
  <div class="accordion-body section-stack" style="padding-top:16px">
    <div class="section-card glass">
      <h3>%s</h3>
      <div class="table-wrap"><table><tbody>%s</tbody></table></div>
    </div>
    <div class="section-card glass">
      <h3>%s</h3>
      <div class="table-wrap"><table><tbody>%s</tbody></table></div>
    </div>
    <div class="section-card glass">
      <h3>%s</h3>
      %s
    </div>
    <div class="section-card glass">
      <h3>%s</h3>
      <div class="table-wrap"><table><tbody>%s</tbody></table></div>
    </div>
  </div>
</details>`,
			detailsOpen,
			esc(t(lang, "\u8a73\u7d30\u3092\u898b\u308b", "Show details")),
			esc(t(lang, "\u30dd\u30fc\u30ea\u30f3\u30b0", "Polling")), pollRows,
			esc(t(lang, "Discord \u9001\u4fe1", "Discord Sends")), sendRows,
			esc(t(lang, "Webhook \u30d8\u30eb\u30b9", "Webhook Health")), webhookTableHTML,
			esc(t(lang, "\u30ea\u30bd\u30fc\u30b9", "Resources")), resourceRows,
		)

		body := `<div class="section-stack">` + summaryCard + detailsAccordion + `</div>` +
			`<div class="button-row" style="margin-top:16px">` +
			`<a class="btn-ghost" href="` + withLang("/bot/admin", r) + `">` + esc(t(lang, "\u7ba1\u7406\u753b\u9762\u3078", "Back to Admin")) + `</a>` +
			`</div>`

		fmt.Fprint(w, adminPage(lang, t(lang, "\u30b7\u30b9\u30c6\u30e0\u30d8\u30eb\u30b9", "System Health"), r, body))
	}
}

func UsersHandler(db *gorm.DB, kitsuHostname string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.Method == http.MethodPost {
			id := parseUint(r.FormValue("user_id"))
			name := strings.TrimSpace(r.FormValue("kitsu_name"))
			email := strings.TrimSpace(r.FormValue("kitsu_email"))
			discordID := strings.TrimSpace(r.FormValue("discord_id"))
			switch r.FormValue("action") {
			case "delete":
				model.DeleteUserMapByID(db, id)
			default:
				if name != "" && (discordID == "" || discordIDRegexp.MatchString(discordID)) {
					if id > 0 {
						model.UpdateUserMap(db, id, name, email, discordID)
					} else {
						model.UpsertUserMapWithEmail(db, name, email, discordID)
					}
				}
			}
			http.Redirect(w, r, withLang("/bot/admin/users", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		editID := parseUint(r.URL.Query().Get("edit"))
		var editUser *model.UserMap
		if editID > 0 {
			editUser = model.FindUserMapByID(db, editID)
		}
		people := filterAssignablePersons(ListKitsuPersons(kitsuHostname), botAccountEmail(db))
		personOptions := buildPersonOptions(people, editUser, lang)

		var rows strings.Builder
		for _, user := range filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db)) {
			discordID := `<span class="status-pill bad">` + t(lang, "ID未設定", "No ID") + `</span>`
			if strings.TrimSpace(user.DiscordID) != "" {
				discordID = `<code>` + esc(user.DiscordID) + `</code>`
			}
			rows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td><div class="inline-actions"><a class="btn-ghost" href="%s">%s</a><form method="POST" class="delete-form" data-confirm="%s" data-require-text="%s"><input type="hidden" name="action" value="delete"><input type="hidden" name="user_id" value="%d"><button class="btn-danger" type="submit">%s</button></form></div></td></tr>`,
				esc(user.KitsuName), discordID, withLang("/bot/admin/users", r)+"&edit="+strconv.FormatUint(uint64(user.ID), 10), t(lang, "編集", "Edit"), esc(user.KitsuName), t(lang, "削除", "delete"), user.ID, t(lang, "削除", "Delete")))
		}
		if rows.Len() == 0 {
			rows.WriteString(`<tr><td colspan="3" class="muted">` + t(lang, "まだユーザー割り当てがありません。", "No user assignments yet.") + `</td></tr>`)
		}

		formTitle := t(lang, "新規割り当て", "New Assignment")
		userID := uint(0)
		selectedName, selectedEmail, selectedDiscordID := "", "", ""
		if editUser != nil {
			formTitle = t(lang, "ユーザー割り当てを編集", "Edit Assignment")
			userID = editUser.ID
			selectedName = editUser.KitsuName
			selectedEmail = editUser.KitsuEmail
			selectedDiscordID = editUser.DiscordID
		}
		body := fmt.Sprintf(`
<div class="section-stack">
  <div class="section-card glass">
    <h3>%s</h3>
    <p class="hint">%s</p>
    <form method="POST">
      <input type="hidden" name="user_id" value="%d">
      <input type="hidden" id="kitsuNameInput" name="kitsu_name" value="%s">
      <input type="hidden" id="kitsuEmailInput" name="kitsu_email" value="%s">
      <div class="form-grid">
        <div><label>%s</label><select id="personSelect" onchange="syncPersonSelect()" required>%s</select></div>
        <div><label>%s</label><input type="text" name="discord_id" value="%s" placeholder="123456789012345678"><div class="field-help">%s</div></div>
      </div>
      <div class="button-row"><button type="submit" class="btn">%s</button><a class="btn-ghost" href="%s">%s</a></div>
    </form>
  </div>
  <div class="section-card glass"><h3>%s</h3><div class="table-wrap"><table><thead><tr><th>%s</th><th>Discord ID</th><th>%s</th></tr></thead><tbody>%s</tbody></table></div></div>
</div>
<script>
function syncPersonSelect(){
  var sel = document.getElementById('personSelect');
  var opt = sel && sel.options ? sel.options[sel.selectedIndex] : null;
  document.getElementById('kitsuNameInput').value = opt ? (opt.getAttribute('data-name') || '') : '';
  document.getElementById('kitsuEmailInput').value = opt ? (opt.getAttribute('data-email') || '') : '';
}
document.addEventListener('DOMContentLoaded', syncPersonSelect);
</script>`,
			formTitle, t(lang, "User = タスク割り当て時に @mention します", "User = @mentioned when task is assigned"), userID, esc(selectedName), esc(selectedEmail), t(lang, "Kitsuユーザー", "Kitsu user"), personOptions, t(lang, "DiscordユーザーID", "Discord user ID"), esc(selectedDiscordID), t(lang, "未入力の場合は ID未設定 と表示されます。", "If empty, the UI will show No ID."), t(lang, "保存", "Save"), withLang("/bot/admin/users", r), t(lang, "キャンセル", "Cancel"), t(lang, "現在の割り当て", "Current assignments"), t(lang, "名前", "Name"), t(lang, "操作", "Actions"), rows.String())
		fmt.Fprint(w, adminPage(lang, t(lang, "ユーザー割り当て", "User Assignment"), r, body))
	}
}

func CheckersHandler(db *gorm.DB, kitsuHostname string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.Method == http.MethodPost {
			id := parseUint(r.FormValue("checker_id"))
			taskType := strings.TrimSpace(r.FormValue("task_type"))
			kitsuName := strings.TrimSpace(r.FormValue("kitsu_name"))
			kitsuEmail := strings.TrimSpace(r.FormValue("kitsu_email"))
			overrideDiscordID := strings.TrimSpace(r.FormValue("resolved_discord_id"))
			switch r.FormValue("action") {
			case "delete":
				model.DeleteCheckerEntryByID(db, id)
			default:
				if id > 0 {
					model.UpdateCheckerMapWithOverride(db, id, taskType, kitsuName, kitsuEmail, overrideDiscordID)
				} else {
					model.AddCheckerMapByUserWithOverride(db, taskType, kitsuName, kitsuEmail, overrideDiscordID)
				}
			}
			http.Redirect(w, r, withLang("/bot/admin/checkers", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		editID := parseUint(r.URL.Query().Get("edit"))
		var editChecker *model.CheckerMap
		if editID > 0 {
			for _, row := range model.ListCheckerMap(db) {
				if row.ID == editID {
					copyRow := row
					editChecker = &copyRow
					break
				}
			}
		}
		taskOptions := buildTaskOptions(AllTaskTypeNames(), selectedTask(editChecker), lang)
		userOptions := buildAssignedUserOptions(filterAssignableUsers(model.ListUserMap(db), botAccountEmail(db)), selectedCheckerEmail(editChecker), selectedCheckerName(editChecker), lang)

		var rows strings.Builder
		for _, checker := range model.ListCheckerMap(db) {
			resolvedID := model.ResolveCheckerDiscordID(db, checker)
			status := `<span class="status-pill bad">` + t(lang, "ID未設定", "No ID") + `</span>`
			if resolvedID != "" {
				status = `<code>` + esc(resolvedID) + `</code>`
			}
			rows.WriteString(fmt.Sprintf(`<tr><td>%s %s</td><td>%s</td><td>%s</td><td><div class="inline-actions"><a class="btn-ghost" href="%s">%s</a><form method="POST" class="delete-form" data-confirm="%s / %s" data-require-text="%s"><input type="hidden" name="action" value="delete"><input type="hidden" name="checker_id" value="%d"><button class="btn-danger" type="submit">%s</button></form></div></td></tr>`,
				taskTypeIcon(checker.TaskType), esc(checker.TaskType), esc(checker.KitsuName), status, withLang("/bot/admin/checkers", r)+"&edit="+strconv.FormatUint(uint64(checker.ID), 10), t(lang, "編集", "Edit"), esc(checker.TaskType), esc(checker.KitsuName), t(lang, "削除", "delete"), checker.ID, t(lang, "削除", "Delete")))
		}
		if rows.Len() == 0 {
			rows.WriteString(`<tr><td colspan="4" class="muted">` + t(lang, "まだレビュアー割り当てがありません。", "No reviewer assignments yet.") + `</td></tr>`)
		}

		formTitle := t(lang, "新規割り当て", "New Assignment")
		checkerID := uint(0)
		selectedName, selectedEmail := "", ""
		if editChecker != nil {
			formTitle = t(lang, "レビュアー割り当てを編集", "Edit Reviewer Assignment")
			checkerID = editChecker.ID
			selectedName = editChecker.KitsuName
			selectedEmail = editChecker.KitsuEmail
		}
		body := fmt.Sprintf(`
<div class="section-stack">
  <div class="section-card glass">
    <h3>%s</h3>
    <p class="hint">%s</p>
    <form method="POST">
      <input type="hidden" name="checker_id" value="%d">
      <input type="hidden" id="checkerNameInput" name="kitsu_name" value="%s">
      <input type="hidden" id="checkerEmailInput" name="kitsu_email" value="%s">
      <div class="form-grid">
        <div><label>%s</label><select name="task_type" required>%s</select></div>
        <div><label>%s</label><select id="checkerUserSelect" onchange="syncCheckerUser()" required>%s</select></div>
        <div class="form-span-2">%s</div>
      </div>
      <div class="button-row"><button type="submit" class="btn">%s</button><a class="btn-ghost" href="%s">%s</a></div>
    </form>
  </div>
  <div class="section-card glass"><h3>%s</h3><div class="table-wrap"><table><thead><tr><th>%s</th><th>%s</th><th>Resolved ID</th><th>%s</th></tr></thead><tbody>%s</tbody></table></div></div>
</div>
<script>
function syncCheckerUser(){
  var sel = document.getElementById('checkerUserSelect');
  var opt = sel && sel.options ? sel.options[sel.selectedIndex] : null;
  document.getElementById('checkerNameInput').value = opt ? (opt.getAttribute('data-name') || '') : '';
  document.getElementById('checkerEmailInput').value = opt ? (opt.getAttribute('data-email') || '') : '';
}
document.addEventListener('DOMContentLoaded', syncCheckerUser);
</script>`,
			formTitle, t(lang, "Reviewer = ステータス変更時に @mention します", "Reviewer = @mentioned when status changes"), checkerID, esc(selectedName), esc(selectedEmail), t(lang, "タスクタイプ", "Task type"), taskOptions, t(lang, "ユーザー", "User"), userOptions, checkerResolvedInput(lang, db, editChecker), t(lang, "保存", "Save"), withLang("/bot/admin/checkers", r), t(lang, "キャンセル", "Cancel"), t(lang, "現在の割り当て", "Current assignments"), t(lang, "タスクタイプ", "Task type"), t(lang, "ユーザー", "User"), t(lang, "操作", "Actions"), rows.String())
		fmt.Fprint(w, adminPage(lang, t(lang, "レビュアー割り当て", "Reviewer Assignment"), r, body))
	}
}

func DriveHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.Method == http.MethodPost {
			projectID := strings.TrimSpace(r.FormValue("kitsu_project_id"))
			storageURL := strings.TrimSpace(r.FormValue("storage_url"))
			if projectID != "" {
				model.SetProjectStorageURL(db, projectID, storageURL)
			}
			http.Redirect(w, r, withLang("/bot/admin/drive", r)+"&msg=saved", http.StatusSeeOther)
			return
		}
		var blocks strings.Builder
		for _, project := range model.ListProjects(db) {
			blocks.WriteString(fmt.Sprintf(`<div class="section-card glass"><h3>%s</h3><form method="POST"><input type="hidden" name="kitsu_project_id" value="%s"><label>%s</label><input type="url" name="storage_url" value="%s" placeholder="https://drive.google.com/..."><div class="button-row"><button type="submit" class="btn">%s</button></div></form></div>`,
				esc(project.Name), esc(project.KitsuProjectID), t(lang, "補助リンク", "Helper link"), esc(project.StorageURL), t(lang, "保存", "Save")))
		}
		if blocks.Len() == 0 {
			blocks.WriteString(emptyState("📁", t(lang, "まだプロジェクトがありません", "No projects yet"), t(lang, "先に Project Management で project routing を作成してから補助リンクを設定してください。", "Create project routing in Project Management first, then add helper links here.")))
		}
		body := `<div class="section-stack"><div class="section-card glass"><p class="hint">` + t(lang, "プロジェクトごとの補助リンク（Drive など）を設定します。", "Set helper links per project (Drive, etc.).") + `</p></div>` + blocks.String() + `</div>`
		fmt.Fprint(w, adminPage(lang, t(lang, "ストレージリンク", "Storage Links"), r, body))
	}
}

func BotHandler(db *gorm.DB, kitsuReconnect func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		storedHost := model.GetSetting(db, "kitsu.hostname")
		autoHost := publicKitsuHostnameFromRequest(r, storedHost)
		effectiveHost := autoHost
		if storedHost != "" {
			effectiveHost = normalizeKitsuHostname(storedHost)
		}
		editMode := r.URL.Query().Get("edit") == "1"
		if editMode && !botEditAllowed(r) {
			http.Redirect(w, r, appendLang("/bot/login?next="+url.QueryEscape(r.URL.RequestURI()), lang), http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			kitsuChanged := false
			if storedHost == "" && autoHost != "" {
				model.SetSetting(db, "kitsu.hostname", autoHost)
				os.Setenv("KITSU_HOSTNAME", autoHost)
				kitsuChanged = true
			} else if storedHost != "" {
				os.Setenv("KITSU_HOSTNAME", normalizeKitsuHostname(storedHost))
			}
			if value := strings.TrimSpace(r.FormValue("bot_token")); value != "" {
				os.Setenv("DISCORD_BOT_TOKEN", value)
			}
			// Keep accepting this for backward compatibility with legacy fallback configuration.
			if value := strings.TrimSpace(r.FormValue("guild_id")); value != "" {
				model.SetSetting(db, "discord.guildID", value)
			}
			if value := strings.TrimSpace(r.FormValue("kitsu_runtime_email")); value != "" {
				setRuntimeKitsuEmail(db, value)
				kitsuChanged = true
			}
			if value := strings.TrimSpace(r.FormValue("kitsu_runtime_password")); value != "" {
				setRuntimeKitsuPassword(value)
				kitsuChanged = true
			}
			if kitsuChanged && kitsuReconnect != nil {
				go kitsuReconnect()
			}
			http.Redirect(w, r, withLang("/bot/admin/bot", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		kitsuEmail := storedRuntimeKitsuEmail(db)
		configured := effectiveHost != "" && kitsuEmail != ""
		statusClass := "bad"
		statusLabel := t(lang, "未設定", "Not configured")
		if configured {
			statusClass = "ok"
			statusLabel = t(lang, "設定済み", "Configured")
		}
		view := fmt.Sprintf(`
<div class="section-stack">
  <div class="section-card glass">
	    <div class="page-heading"><div><h3>%s</h3><p class="hint">%s</p></div><span class="status-pill %s">%s</span></div>
    <div class="metric-grid">
	      <div class="metric-card"><div class="metric-label">Kitsu hostname</div><div class="metric-value metric-value-host"><code>%s</code></div></div>
      <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%s</div></div>
    </div>
    <div class="button-row"><a class="btn" data-edit-lock-link="1" href="%s">%s</a><a class="btn-ghost" href="%s">%s</a><a class="btn-ghost" href="%s">%s</a></div>
  </div>
</div>`,
			t(lang, "共有Bot / Runtime 設定", "Shared Bot / Runtime"), t(lang, "Project Management で使う共有 Bot / Runtime の設定を確認・更新できます。", "Review and update the shared Bot / Runtime settings used by Project Management."), statusClass, statusLabel, esc(effectiveHost), t(lang, "Bot Token", "Bot Token"), secretStatus(os.Getenv("DISCORD_BOT_TOKEN"), lang), withLang("/bot/admin/bot?edit=1", r), t(lang, "再認証して編集する", "Re-authenticate to edit"), withLang("/bot/setup", r), t(lang, "Project Managementへ戻る", "Back to Project Management"), withLang("/bot/admin/projects", r), t(lang, "Projects & Guilds を見直す", "Review Projects & Guilds"))
		if !editMode {
			fmt.Fprint(w, adminPage(lang, t(lang, "共有Bot / Runtime 設定", "Shared Bot / Runtime"), r, view))
			return
		}
		edit := fmt.Sprintf(`
<div class="section-stack">
  %s
  <form method="POST" class="section-stack">
    <div class="section-card glass"><h3>%s</h3><div class="form-grid">
      <div class="form-span-2"><label>Kitsu hostname</label><input type="text" value="%s" readonly></div>
      <div class="form-span-2"><label>Discord Bot Token</label><input type="password" name="bot_token" autocomplete="new-password" placeholder="%s"><p class="field-help">%s</p><p class="field-help">%s</p></div>
    </div></div>
    <div class="section-card glass"><h3>%s</h3><div class="form-grid">
      <div><label>%s</label><input type="email" name="kitsu_runtime_email" value="%s" placeholder="kitsusync-bot@local.invalid"></div>
      <div><label>%s</label><input type="password" name="kitsu_runtime_password" autocomplete="new-password" placeholder="%s"></div>
    </div></div>
    <div class="button-row"><button type="submit" class="btn">%s</button><a class="btn-ghost" href="%s">%s</a><a class="btn-ghost" href="%s">%s</a></div>
  </form>
</div>`,
				authNoticeHTML(lang, t(lang, "再認証済み", "Re-authenticated"), t(lang, "編集モードは一時的に有効です。", "Edit mode is temporarily enabled.")), t(lang, "Discord 設定", "Discord settings"), esc(effectiveHost), t(lang, "必要な時だけ新しい Token を入力してください。", "Only paste a new token when rotating it."), t(lang, "このトークン変更は現在実行中のプロセスにのみ反映されます。", "Token changes apply only to the currently running process."), t(lang, "再起動時は .env.local / 環境変数の値が再読み込みされます。永続ローテーションは .env.local も更新してください。", "On restart, the token is reloaded from .env.local / environment. Update .env.local for durable rotation."), t(lang, "Kitsu Runtime 接続", "Kitsu runtime connection"), t(lang, "Runtime メール", "Runtime email"), esc(kitsuEmail), t(lang, "Runtime パスワード", "Runtime password"), t(lang, "必要な時だけ専用 Runtime パスワードを入力してください。", "Only paste a new dedicated runtime password when rotating it."), t(lang, "保存", "Save"), withLang("/bot/setup", r), t(lang, "Project Managementへ戻る", "Back to Project Management"), withLang("/bot/admin/projects", r), t(lang, "Projects & Guilds を見直す", "Review Projects & Guilds"))
		fmt.Fprint(w, adminPage(lang, t(lang, "Bot設定", "Bot Settings"), r, edit))
	}
}

func AuditLogHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		groups := map[string][]model.AuditLog{}
		var names []string
		for _, log := range model.ListAuditLogs(db, 200) {
			name := fallbackText(log.ProjectName, t(lang, "未割り当て", "Unassigned"))
			if _, ok := groups[name]; !ok {
				names = append(names, name)
			}
			groups[name] = append(groups[name], log)
		}
		sort.Strings(names)
		var body strings.Builder
		body.WriteString(`<div class="section-stack">`)
		for _, name := range names {
			var rows strings.Builder
			for _, log := range groups[name] {
				result := `<span class="status-pill ok">` + t(lang, "成功", "OK") + `</span>`
				if !log.Success {
					result = `<span class="status-pill bad">` + t(lang, "失敗", "Failed") + `</span>`
				}
				rows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s → %s</td><td><code>%s</code></td><td>%s</td></tr>`, 
					log.CreatedAt.Format("2006-01-02 15:04:05"), esc(log.EntityName), esc(log.TaskType), fallbackText(log.OldStatus, "-"), fallbackText(log.NewStatus, "-"), esc(log.DiscordMsgID), result))
			}
			body.WriteString(fmt.Sprintf(`<details class="accordion" open><summary><div><div class="eyebrow">PRODUCTION</div><div class="tile-title">%s</div><div class="tile-sub">%d logs</div></div><span class="accordion-caret">⌄</span></summary><div class="accordion-body"><div class="table-wrap"><table><thead><tr><th>%s</th><th>%s</th><th>%s</th><th>%s</th><th>Message</th><th>%s</th></tr></thead><tbody>%s</tbody></table></div></div></details>`, 
				esc(name), len(groups[name]), t(lang, "日時", "Time"), t(lang, "対象", "Target"), t(lang, "タスクタイプ", "Task type"), t(lang, "状態", "Status"), t(lang, "結果", "Result"), rows.String()))
		}
		if len(names) == 0 {
			body.WriteString(emptyState("🧾", t(lang, "まだ監査ログがありません。", "No audit logs yet."), ""))
		}
		body.WriteString(`</div>`)
		fmt.Fprint(w, adminPage(lang, t(lang, "監査ログ", "Audit Log"), r, body.String()))
	}
}

func buildPersonOptions(persons []KitsuPerson, selected *model.UserMap, lang string) string {
	selectedEmail, selectedName := "", ""
	if selected != nil {
		selectedEmail = selected.KitsuEmail
		selectedName = selected.KitsuName
	}
	var out strings.Builder
	out.WriteString(`<option value="">` + t(lang, "選択してください", "Select user") + `</option>`)
	found := false
	for _, person := range persons {
		isSelected := (selectedEmail != "" && person.Email == selectedEmail) || (selectedEmail == "" && selectedName != "" && person.FullName == selectedName)
		if isSelected {
			found = true
		}
		out.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s" data-email="%s" %s>%s</option>`, esc(person.Email), esc(person.FullName), esc(person.Email), selectedAttr(isSelected), esc(person.FullName)))
	}
	if selected != nil && !found {
		out.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s" data-email="%s" selected>%s</option>`, esc(selected.KitsuEmail), esc(selected.KitsuName), esc(selected.KitsuEmail), esc(selected.KitsuName)))
	}
	return out.String()
}

func botAccountEmail(db *gorm.DB) string {
	return strings.ToLower(storedRuntimeKitsuEmail(db))
}

func filterAssignablePersons(persons []KitsuPerson, botEmail string) []KitsuPerson {
	if botEmail == "" {
		return persons
	}
	filtered := make([]KitsuPerson, 0, len(persons))
	for _, person := range persons {
		if !strings.EqualFold(strings.TrimSpace(person.Email), botEmail) {
			filtered = append(filtered, person)
		}
	}
	return filtered
}

func filterAssignableUsers(users []model.UserMap, botEmail string) []model.UserMap {
	if botEmail == "" {
		return users
	}
	filtered := make([]model.UserMap, 0, len(users))
	for _, user := range users {
		if !strings.EqualFold(strings.TrimSpace(user.KitsuEmail), botEmail) {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

func buildAssignedUserOptions(users []model.UserMap, selectedEmail, selectedName, lang string) string {
	var out strings.Builder
	out.WriteString(`<option value="">` + t(lang, "選択してください", "Select user") + `</option>`)
	for _, user := range users {
		isSelected := (selectedEmail != "" && user.KitsuEmail == selectedEmail) || (selectedEmail == "" && selectedName != "" && user.KitsuName == selectedName)
		out.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s" data-email="%s" %s>%s</option>`, esc(user.KitsuEmail), esc(user.KitsuName), esc(user.KitsuEmail), selectedAttr(isSelected), esc(user.KitsuName)))
	}
	return out.String()
}

func buildTaskOptions(taskTypes []string, selected, lang string) string {
	sort.Strings(taskTypes)
	var out strings.Builder
	out.WriteString(`<option value="">` + t(lang, "選択してください", "Select task type") + `</option>`)
	for _, taskType := range taskTypes {
		out.WriteString(fmt.Sprintf(`<option value="%s" %s>%s %s</option>`, esc(taskType), selectedAttr(taskType == selected), taskTypeIcon(taskType), esc(taskType)))
	}
	return out.String()
}

func taskTypeIcon(taskType string) string {
	switch strings.ToLower(strings.TrimSpace(taskType)) {
	case "*", "general":
		return "📢"
	case "animation":
		return "🏃"
	case "background art":
		return "🖼️"
	case "color grading":
		return "🌈"
	case "compositing":
		return "🧩"
	case "concept":
		return "💭"
	case "design":
		return "🎨"
	case "edit":
		return "✂️"
	case "fx":
		return "✨"
	case "layout":
		return "📐"
	case "lighting":
		return "💡"
	case "lookdev":
		return "🔍"
	case "modeling":
		return "🧊"
	case "paint", "cleanup":
		return "🖌️"
	case "rendering":
		return "💻"
	case "rigging":
		return "🦴"
	case "script":
		return "📜"
	case "shading":
		return "🌓"
	case "sound":
		return "🔊"
	case "storyboard":
		return "📝"
	case "texturing":
		return "🧵"
	default:
		return "🏷️"
	}
}

func selectedTask(row *model.CheckerMap) string {
	if row == nil {
		return ""
	}
	return row.TaskType
}

func selectedCheckerName(row *model.CheckerMap) string {
	if row == nil {
		return ""
	}
	return row.KitsuName
}

func selectedCheckerEmail(row *model.CheckerMap) string {
	if row == nil {
		return ""
	}
	return row.KitsuEmail
}

func selectedCheckerOverride(row *model.CheckerMap) string {
	if row == nil {
		return ""
	}
	return row.OverrideDiscordID
}

func checkerResolvedInput(lang string, db *gorm.DB, row *model.CheckerMap) string {
	if row == nil {
		return `<div class="field-help">` + t(lang, "通常はユーザー割り当ての Discord ID を自動参照します。", "Discord IDs are normally resolved from User Assignment.") + `</div>`
	}
	value := selectedCheckerOverride(row)
	if value == "" {
		value = model.ResolveCheckerDiscordID(db, *row)
	}
	return `<label>Resolved ID</label><input type="text" name="resolved_discord_id" value="` + esc(value) + `" placeholder="123456789012345678">`
}

func parseUint(value string) uint {
	n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return uint(n)
}

func secretStatus(value, lang string) string {
	if strings.TrimSpace(value) == "" {
		return `<span class="status-pill bad">` + t(lang, "未設定", "Not configured") + `</span>`
	}
	return `<span class="status-pill ok">` + t(lang, "設定済み / 非表示", "Configured / hidden") + `</span>`
}

func valueStatus(value, lang string) string {
	if strings.TrimSpace(value) == "" {
		return `<span class="status-pill bad">` + t(lang, "未設定", "Not configured") + `</span>`
	}
	return `<span class="status-pill ok">` + esc(value) + `</span>`
}

func esc(s string) string { return html.EscapeString(s) }

func emptyState(icon, title, sub string) string {
	return `<div class="empty"><div class="tile-icon" style="margin:0 auto 14px">` + esc(icon) + `</div><h3>` + esc(title) + `</h3><p class="hint">` + esc(sub) + `</p></div>`
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return esc(value)
}

func adminPage(lang, title string, r *http.Request, body string) string {
	message := ""
	if r != nil && r.URL.Query().Get("msg") != "" {
		message = `<div class="toast glass">` + t(lang, "保存しました。", "Saved.") + `</div>`
	}
	nav := `<div class="nav-card glass">` +
		`<a class="nav-chip" href="` + withLang("/bot/admin", r) + `">` + t(lang, "管理", "Admin") + `</a>` +
		`<a class="nav-chip" href="` + withLang("/bot/setup", r) + `">` + t(lang, "Project Management", "Project Management") + `</a>` +
		`<a class="nav-chip" href="` + withLang("/bot/docs/", r) + `">` + t(lang, "ドキュメント", "Docs") + `</a>` +
		`<a class="nav-chip" href="` + withLang("/bot/logout", r) + `">` + t(lang, "ログアウト", "Logout") + `</a>` +
		`</div>`
	content := `<div class="page-card glass"><div class="page-heading"><div><h1>` + esc(title) + `</h1></div></div>` +
		message + body + `</div>` +
		`<div id="deleteModal" class="delete-modal"><div class="delete-box glass"><h2 class="delete-title">` + esc(t(lang, "削除の確認", "Confirm deletion")) + `</h2><p id="deleteModalText" class="delete-text"></p><p id="deleteModalHelper" class="field-help hidden"></p><div id="deleteModalInputWrap" class="delete-input hidden"><label><span class="sr-only">Confirm text</span><input id="deleteModalInput" type="text" autocomplete="off" autocapitalize="off" spellcheck="false"></label><div class="field-help">` + esc(t(lang, "確認ワード", "Confirmation word")) + `: <code id="deleteModalExpected"></code></div></div><div class="button-row"><button id="deleteConfirmBtn" type="button" class="btn-danger">` + esc(t(lang, "削除する", "Delete")) + `</button><button id="deleteCancelBtn" type="button" class="btn-ghost">` + esc(t(lang, "キャンセル", "Cancel")) + `</button></div></div></div>` +
		baseAdminJS(lang)
	return appShell("KitsuSync", "", lang, r, nav, content)
}

