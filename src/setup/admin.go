package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
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

func AdminIndexWithRuntime(db *gorm.DB, runtimeHealthy func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.URL.Query().Get("legacy") != "1" {
			renderIADashboardWithRuntime(w, r, db, runtimeHealthy)
			return
		}

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

		// ---- Active productions ----
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
		allTaskTypes := kitsu.GetTaskTypes().Each
		projectsWithUnassigned := 0
		for _, proj := range projects {
			assigned := map[string]bool{}
			for _, wh := range allWebhooks {
				if wh.KitsuProjectID == proj.KitsuProjectID && strings.TrimSpace(wh.TaskType) != "" {
					assigned[wh.TaskType] = true
				}
			}
			if len(assigned) < len(allTaskTypes) {
				projectsWithUnassigned++
			}
		}

		statusChip := func(class, text string) string {
			return `<span class="status-pill ` + class + `">` + esc(text) + `</span>`
		}
		tileStatus := func(items ...string) string {
			var out strings.Builder
			out.WriteString(`<div class="tile-sub" style="display:flex;flex-wrap:wrap;gap:8px">`)
			for _, item := range items {
				if strings.TrimSpace(item) == "" {
					continue
				}
				out.WriteString(item)
			}
			out.WriteString(`</div>`)
			return out.String()
		}

		var projectsCard string
		if projectCount == 0 {
			projectsCard = fmt.Sprintf(`
<div class="section-card glass">
  <h3>%s</h3>
  %s
  <p class="hint" style="margin-top:12px">%s</p>
</div>`,
				esc(t(lang, "\u30a2\u30af\u30c6\u30a3\u30d6\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3", "Active Productions")),
				emptyState("\U0001F3AC", t(lang, "\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u672a\u8a2d\u5b9a", "No productions configured"), t(lang, "\u65b0\u898f\u9023\u643a\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u304b\u3089\u6700\u521d\u306e production connection \u3092\u8a2d\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044\u3002", "Use New Connection Setup to configure your first production connection.")),
				esc(t(lang, "Next \u30bb\u30af\u30b7\u30e7\u30f3\u304b\u3089\u65b0\u898f\u9023\u643a\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u3092\u958b\u3044\u3066\u304f\u3060\u3055\u3044\u3002", "Open New Connection Setup from the Next section.")),
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
					withLang("/bot/admin/projects?project="+url.QueryEscape(proj.KitsuProjectID), r),
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
				esc(t(lang, "\u30a2\u30af\u30c6\u30a3\u30d6\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3", "Active Productions")),
				withLang("/bot/admin/projects", r),
				esc(t(lang, "\u9023\u643a\u6e08\u307f\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u3092\u958b\u304f \u2192", "Open Connected Productions \u2192")),
				esc(t(lang, "プロダクション名", "プロダクション")),
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
					esc(t(lang, "\u8a73\u7d30", "Details")),
				))
			}
			warns.WriteString(`</div></div>`)
			warningsCard = warns.String()
		}

		// ---- Quick navigation ----
		var connectedStatus string
		switch {
		case projectCount == 0:
			connectedStatus = tileStatus(
				statusChip("bad", t(lang, "連携済み: 0", "Connected: 0")),
				statusChip("warn", t(lang, "未設定", "Unset")),
			)
		case projectsWithUnassigned > 0:
			connectedStatus = tileStatus(
				statusChip("ok", fmt.Sprintf(t(lang, "連携済み: %d", "Connected: %d"), projectCount)),
				statusChip("warn", fmt.Sprintf(t(lang, "未割当あり: %d", "Unassigned: %d"), projectsWithUnassigned)),
			)
		default:
			connectedStatus = tileStatus(
				statusChip("ok", fmt.Sprintf(t(lang, "連携済み: %d", "Connected: %d"), projectCount)),
				statusChip("ok", t(lang, "割当済み", "Assigned")),
			)
		}

		projectsWithUserAssignments := 0
		projectsWithReviewerAssignments := 0
		for _, project := range projects {
			projectUserAssignments := len(model.ListProjectUserMaps(db, project.ID))
			projectReviewerAssignments := len(model.ListProjectCheckerMaps(db, project.ID))
			if projectUserAssignments > 0 {
				projectsWithUserAssignments++
			}
			if projectReviewerAssignments > 0 {
				projectsWithReviewerAssignments++
			}
		}
		usersStatus := tileStatus(
			statusChip(map[bool]string{true: "ok", false: "bad"}[projectsWithUserAssignments > 0], fmt.Sprintf(t(lang, "割り当て production: %d/%d", "Assigned productions: %d/%d"), projectsWithUserAssignments, projectCount)),
		)
		if projectCount == 0 {
			usersStatus = tileStatus(
				statusChip("bad", t(lang, "対象なし", "No target")),
				statusChip("warn", t(lang, "未設定", "Unset")),
			)
		} else if projectsWithReviewerAssignments == 0 {
			usersStatus = tileStatus(
				statusChip(map[bool]string{true: "ok", false: "bad"}[projectsWithUserAssignments > 0], fmt.Sprintf(t(lang, "割り当て production: %d/%d", "Assigned productions: %d/%d"), projectsWithUserAssignments, projectCount)),
				statusChip("warn", fmt.Sprintf(t(lang, "レビュアー production: %d/%d", "Reviewer productions: %d/%d"), projectsWithReviewerAssignments, projectCount)),
			)
		} else {
			usersStatus = tileStatus(
				statusChip(map[bool]string{true: "ok", false: "bad"}[projectsWithUserAssignments > 0], fmt.Sprintf(t(lang, "割り当て production: %d/%d", "Assigned productions: %d/%d"), projectsWithUserAssignments, projectCount)),
				statusChip("ok", fmt.Sprintf(t(lang, "レビュアー production: %d/%d", "Reviewer productions: %d/%d"), projectsWithReviewerAssignments, projectCount)),
			)
		}

		effectiveHost := effectiveRuntimeKitsuEndpoint(db)
		sharedReadiness := sharedBotRuntimeReadiness(db, effectiveHost, storedRuntimeDiscordBotToken(db))
		botTokenConfigured := sharedReadiness.DiscordConfigured
		runtimeConfigured := sharedReadiness.KitsuConfigured
		botStatus := tileStatus(
			statusChip(map[bool]string{true: "ok", false: "bad"}[botTokenConfigured], t(lang, map[bool]string{true: "Bot設定済み", false: "Bot未設定"}[botTokenConfigured], map[bool]string{true: "Bot set", false: "Bot unset"}[botTokenConfigured])),
			statusChip(map[bool]string{true: "ok", false: "bad"}[runtimeConfigured], t(lang, map[bool]string{true: "Runtime設定済み", false: "Runtime未設定"}[runtimeConfigured], map[bool]string{true: "Runtime set", false: "Runtime unset"}[runtimeConfigured])),
		)

		systemStatus := tileStatus(
			statusChip(map[bool]string{true: "ok", false: "bad"}[pollingActive], t(lang, map[bool]string{true: "稼働中", false: "未ポーリング"}[pollingActive], map[bool]string{true: "Running", false: "No polls"}[pollingActive])),
		)
		switch {
		case hasBrokenWebhook:
			systemStatus = tileStatus(
				statusChip(map[bool]string{true: "ok", false: "bad"}[pollingActive], t(lang, map[bool]string{true: "稼働中", false: "未ポーリング"}[pollingActive], map[bool]string{true: "Running", false: "No polls"}[pollingActive])),
				statusChip("warn", fmt.Sprintf(t(lang, "警告あり: %d", "Warnings: %d"), len(unhealthy))),
			)
		case hasPollErr:
			systemStatus = tileStatus(
				statusChip("warn", t(lang, "警告あり", "Warning")),
				statusChip("bad", t(lang, "ポーラー要確認", "Poller issue")),
			)
		case isHealthy:
			systemStatus = tileStatus(
				statusChip("ok", t(lang, "稼働中", "Running")),
				statusChip("ok", t(lang, "正常", "Healthy")),
			)
		}

		auditCount := len(model.ListAuditLogs(db, 1))
		auditStatus := tileStatus(
			statusChip(map[bool]string{true: "ok", false: "bad"}[auditCount > 0], t(lang, map[bool]string{true: "履歴あり", false: "未記録"}[auditCount > 0], map[bool]string{true: "History present", false: "No history"}[auditCount > 0])),
		)

		type navLink struct {
			icon, href, titleJA, titleEN, statusHTML string
		}
		links := []navLink{
			{"\U0001F5C2", "/bot/admin/projects", "\u9023\u643a\u6e08\u307f\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u7ba1\u7406", "Connected Productions", connectedStatus},
			{"\U0001F464", "/bot/admin/users", "\u30e6\u30fc\u30b6\u30fc\u5272\u308a\u5f53\u3066", "Users", usersStatus},
			{"\U0001F916", "/bot/admin/bot", "Bot\u8a2d\u5b9a", "Bot Settings", botStatus},
			{"\u2764", "/bot/admin/health", "\u30b7\u30b9\u30c6\u30e0\u72b6\u614b", "System Status", systemStatus},
			{"\U0001F9FE", "/bot/admin/audit", "\u76e3\u67fb\u30ed\u30b0", "Audit Log", auditStatus},
		}
		var navGrid strings.Builder
		navGrid.WriteString(`<div class="dashboard-grid">`)
		for _, lnk := range links {
			navGrid.WriteString(fmt.Sprintf(
				`<a class="tile glass" href="%s"><div class="tile-icon">%s</div><div class="tile-title">%s</div>%s</a>`,
				withLang(lnk.href, r), lnk.icon, t(lang, lnk.titleJA, lnk.titleEN), lnk.statusHTML,
			))
		}
		navGrid.WriteString(`</div>`)

		nextTitle := t(lang, "\u65b0\u898f\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7", "New Setup")
		nextCardTitle := t(lang, "\u65b0\u898f\u9023\u643a\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u3092\u958b\u304f", "Open New Connection Setup")
		nextCardBody := t(lang, "\u65b0\u3057\u3044 production connection \u306e\u8ffd\u52a0\u306f\u65b0\u898f\u9023\u643a\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u304b\u3089\u9032\u3081\u307e\u3059\u3002\u65e2\u5b58\u306e\u9023\u643a\u72b6\u6cc1\u306f Step 2 \u3068 \u9023\u643a\u6e08\u307f\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u7ba1\u7406 \u3067\u78ba\u8a8d\u3067\u304d\u307e\u3059\u3002", "Add new production connections from New Connection Setup. Existing connected productions remain visible in Step 2 and in Connected Productions.")
		nextPrimaryHref := withLang("/bot/setup", r)
		nextPrimaryLabel := t(lang, "\u65b0\u898f\u9023\u643a\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u3092\u958b\u304f", "Open New Connection Setup")
		nextBadge := `<span class="status-pill bad">` + esc(t(lang, "\u512a\u5148", "Priority")) + `</span>`
		nextProjectList := ""
		if !hasIssues && projectCount == 0 {
			nextCardBody = t(lang, "\u6700\u521d\u306e production connection \u306f\u65b0\u898f\u9023\u643a\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u304b\u3089\u958b\u59cb\u3057\u307e\u3059\u3002\u5171\u6709 Bot / Runtime \u306e\u524d\u63d0\u78ba\u8a8d\u306f Bot\u8a2d\u5b9a\u3067\u884c\u3063\u3066\u304f\u3060\u3055\u3044\u3002", "Start the first production connection in New Connection Setup. Review shared Bot / Runtime prerequisites in Bot Settings.")
			nextBadge = `<span class="status-pill warn">` + esc(t(lang, "\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7", "Setup")) + `</span>`
		} else if !hasIssues && projectCount > 0 {
			nextCardBody = t(lang, "\u65b0\u3057\u3044 production connection \u306e\u8ffd\u52a0\u306f\u65b0\u898f\u9023\u643a\u30bb\u30c3\u30c8\u30a2\u30c3\u30d7\u304b\u3089\u9032\u3081\u307e\u3059\u3002\u65e2\u5b58\u306e\u9023\u643a\u72b6\u6cc1\u306f Step 2 \u3067\u540d\u524d\u3092\u78ba\u8a8d\u3057\u3001\u8a73\u7d30\u306f \u9023\u643a\u6e08\u307f\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u7ba1\u7406 \u3067\u898b\u76f4\u3057\u307e\u3059\u3002", "Add new production connections from New Connection Setup. Use Step 2 for connected production names and Connected Productions for detailed review.")
			nextBadge = `<span class="status-pill ok">` + esc(t(lang, "\u6e96\u5099\u6e08\u307f", "Ready")) + `</span>`
			var projectNameTags strings.Builder
			for _, proj := range projects {
				projectNameTags.WriteString(`<span class="tag">` + esc(proj.Name) + `</span>`)
			}
			nextProjectList = `<div style="display:flex;gap:8px;flex-wrap:wrap;margin-top:12px"><span class="hint" style="width:100%">` + esc(t(lang, "連携済みプロダクション", "Connected productions")) + `</span>` + projectNameTags.String() + `</div>`
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
  %s
  <div class="button-row" style="margin-top:14px">
    <a class="btn" href="%s">%s</a>
  </div>
</div>`,
			esc(nextCardTitle),
			esc(nextCardBody),
			nextBadge,
			nextProjectList,
			nextPrimaryHref,
			esc(nextPrimaryLabel),
		)

		body := `<div class="section-stack">` +
			`<div><div class="eyebrow">NOW</div><h2 style="margin:6px 0 0">` + esc(t(lang, "\u6982\u8981", "Overview")) + `</h2></div>` +
			statusBar + pollerCard + warningsCard + projectsCard +
			`<div><div class="eyebrow">NEXT</div><h2 style="margin:6px 0 0">` + esc(nextTitle) + `</h2></div>` +
			nextActionCard +
			`<div><div class="eyebrow">LATER</div><h2 style="margin:6px 0 0">` + esc(t(lang, "\u8a73\u7d30\u8a2d\u5b9a", "Advanced Settings")) + `</h2></div>` +
			navGrid.String() +
			`</div>`

		fmt.Fprint(w, adminPage(lang, t(lang, "\u30c0\u30c3\u30b7\u30e5\u30dc\u30fc\u30c9", "Dashboard"), r, body))
	}
}

func AdminProjectsHandler(db *gorm.DB, fallbackGuildID, botToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		fallbackGuildID = strings.TrimSpace(fallbackGuildID)
		if r.Method == http.MethodPost && model.IsValidationOnlyProject(db, strings.TrimSpace(r.FormValue("project_id"))) {
			http.Error(w, "validation-only Production is read-only", http.StatusForbidden)
			return
		}
		if handleCurrentProductionUserMutation(w, r, db, botToken) {
			return
		}
		if handleProductionNotificationApply(w, r, lang, db, botToken) {
			return
		}
		if r.Method == http.MethodPost {
			if hasValidationOnlyProject(db) {
				http.Error(w, "validation-only profile is read-only", http.StatusForbidden)
				return
			}
			action := strings.TrimSpace(r.FormValue("action"))
			projectID := strings.TrimSpace(r.FormValue("project_id"))
			if action == "save_team_links" {
				http.Redirect(w, r, saveTeamUserLinks(db, r, botToken), http.StatusSeeOther)
				return
			}
			redirectURL := withLang("/bot/admin/projects", r)
			if projectID != "" {
				redirectURL += "&project=" + url.QueryEscape(projectID)
			}
			if action == "save_notification_language" {
				language := normalizedProductionNotificationLanguage(r.FormValue("notification_language"))
				var project model.Project
				result := db.Where("kitsu_project_id = ?", projectID).First(&project)
				if projectID != "" && result.Error == nil {
					project.Language = language
					result = db.Save(&project)
				}
				if projectID != "" && result.Error == nil {
					http.Redirect(w, r, redirectURL+"&tab=notifications&msg=saved", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, redirectURL+"&tab=notifications&msg=error", http.StatusSeeOther)
				return
			}
			switch action {
			case "execute_current_ia_discord_delete", "execute_validated_channel_delete", "complete_connection_cleanup", "remove_connection":
				if !botEditAllowed(r) {
					http.Redirect(w, r, recentBotAuthenticationLoginURL(r, lang), http.StatusSeeOther)
					return
				}
			}
			if action == "execute_current_ia_discord_delete" {
				expected := t(lang, "削除", "DELETE")
				if projectID == "" || strings.TrimSpace(r.FormValue("confirm_text")) != expected {
					http.Redirect(w, r, redirectURL+"&msg=error", http.StatusSeeOther)
					return
				}
				project := model.FindProjectByKitsuID(db, projectID)
				if project == nil {
					http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=error", http.StatusSeeOther)
					return
				}
				effectiveGuildID := strings.TrimSpace(project.DiscordGuildID)
				if effectiveGuildID == "" {
					effectiveGuildID = fallbackGuildID
				}
				execResult := executeConnectedProductionStrongDelete(lang, *project, effectiveGuildID, botToken, db)
				if !execResult.ConnectionDeleted {
					w.WriteHeader(http.StatusConflict)
				}
				fmt.Fprint(w, renderIAConnectedProductionDeleteResultRefined(lang, r, *project, execResult))
				return
			}
			if action == "preview_remove_connection_with_discord" {
				if projectID != "" {
					http.Redirect(w, r, redirectURL+"&danger_preview=1", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=error", http.StatusSeeOther)
				return
			}
			if action == "validate_remove_connection_channels" {
				if projectID != "" {
					http.Redirect(w, r, redirectURL+"&validated_channels=1", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=error", http.StatusSeeOther)
				return
			}
			if action == "execute_validated_channel_delete" {
				expected := t(lang, "削除", "delete")
				if projectID == "" || strings.TrimSpace(r.FormValue("confirm_text")) != expected {
					http.Redirect(w, r, redirectURL+"&validated_channels=1&msg=error", http.StatusSeeOther)
					return
				}
				project := model.FindProjectByKitsuID(db, projectID)
				if project == nil {
					http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=error", http.StatusSeeOther)
					return
				}
				effectiveGuildID := strings.TrimSpace(project.DiscordGuildID)
				if effectiveGuildID == "" {
					effectiveGuildID = fallbackGuildID
				}
				execResult := executeConnectedProductionStrongDelete(lang, *project, effectiveGuildID, botToken, db)
				if !execResult.ConnectionDeleted {
					w.WriteHeader(http.StatusConflict)
				}
				fmt.Fprint(w, renderConnectedProductionStrongDeleteResultPage(lang, r, *project, execResult))
				return
			}
			if action == "complete_connection_cleanup" {
				expected := t(lang, "\u524a\u9664", "delete")
				if projectID == "" || strings.TrimSpace(r.FormValue("confirm_text")) != expected {
					http.Redirect(w, r, redirectURL+"&msg=error", http.StatusSeeOther)
					return
				}
				project := model.FindProjectByKitsuID(db, projectID)
				if project == nil {
					http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=error", http.StatusSeeOther)
					return
				}
				if err := deleteProjectConnectionOnlyIfNoWebhooks(projectID, db); err == nil {
					http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=saved", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, redirectURL+"&msg=error", http.StatusSeeOther)
				return
			}
			if action == "remove_connection" {
				expected := t(lang, "削除", "delete")
				if projectID != "" && strings.TrimSpace(r.FormValue("confirm_text")) == expected {
					if err := DeleteProjectConnectionOnly(projectID, db); err == nil {
						http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=saved", http.StatusSeeOther)
						return
					}
				}
				http.Redirect(w, r, redirectURL+"&msg=error", http.StatusSeeOther)
				return
			}
			guildID := strings.TrimSpace(r.FormValue("guild_id"))
			if projectID != "" {
				if err := model.UpdateProjectGuildID(db, projectID, guildID); err == nil {
					http.Redirect(w, r, redirectURL+"&msg=saved", http.StatusSeeOther)
					return
				}
			}
			http.Redirect(w, r, redirectURL+"&msg=error", http.StatusSeeOther)
			return
		}

		renderIAProductionList(w, r, db, fallbackGuildID, botToken)
	}
}

type connectedProductionChannelCandidate struct {
	ChannelID   string
	StoredNames []string
}

type connectedProductionChannelValidationResult struct {
	ChannelID     string
	StoredNames   []string
	CurrentName   string
	CurrentType   int
	CurrentParent string
	Reason        string
}

type connectedProductionChannelDeleteExecution struct {
	ValidationSummary   string
	Checks              []string
	Deleted             []connectedProductionChannelValidationResult
	Skipped             []connectedProductionChannelValidationResult
	Failed              []connectedProductionChannelValidationResult
	CleanupWarnings     []connectedProductionChannelValidationResult
	RemainingWebhooks   int
	CategoryDeleted     bool
	ConnectionDeleted   bool
	DeletedWebhookCount int
	CategoryError       string
	ConnectionError     string
}

func buildConnectedProductionChannelCandidates(webhooks []model.ProjectWebhook) []connectedProductionChannelCandidate {
	byID := map[string]map[string]bool{}
	for _, wh := range webhooks {
		channelID := strings.TrimSpace(wh.DiscordChannelID)
		if channelID == "" {
			continue
		}
		if _, ok := byID[channelID]; !ok {
			byID[channelID] = map[string]bool{}
		}
		channelName := strings.TrimSpace(wh.ChannelName)
		if channelName != "" {
			byID[channelID][channelName] = true
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]connectedProductionChannelCandidate, 0, len(ids))
	for _, id := range ids {
		names := make([]string, 0, len(byID[id]))
		for name := range byID[id] {
			names = append(names, name)
		}
		sort.Strings(names)
		out = append(out, connectedProductionChannelCandidate{ChannelID: id, StoredNames: names})
	}
	return out
}

func validateConnectedProductionChannelCandidates(lang string, project model.Project, effectiveGuildID, botToken string, allWebhooks []model.ProjectWebhook, candidates []connectedProductionChannelCandidate) ([]connectedProductionChannelValidationResult, []connectedProductionChannelValidationResult, string, []string) {
	checks := []string{
		t(lang, "現在の guild channel list に対象 channel が存在する", "Candidate channel exists in the current guild channel list"),
		t(lang, "現在の channel type が text channel である", "Current channel type is text channel"),
		t(lang, "同じ channel ID が他 production から参照されていない", "Channel ID is not referenced by another production"),
		t(lang, "保存済み category がある場合、現在の parent が一致する", "Current parent matches the recorded project category when one is saved"),
		t(lang, "保存済み channel name がある場合、現在の name が一致する", "Current channel name matches one of the stored channel names when names are saved"),
	}
	results := make([]connectedProductionChannelValidationResult, 0, len(candidates))
	for _, candidate := range candidates {
		results = append(results, connectedProductionChannelValidationResult{
			ChannelID:   candidate.ChannelID,
			StoredNames: append([]string(nil), candidate.StoredNames...),
		})
	}
	if len(results) == 0 {
		return nil, nil, t(lang, "この production には保存済みの Discord channel candidate がありません。", "No stored Discord channel candidates were found for this production."), checks
	}

	effectiveGuildID = strings.TrimSpace(effectiveGuildID)
	if effectiveGuildID == "" {
		for i := range results {
			results[i].Reason = t(lang, "Skip: この production には有効な Discord guild ID がありません。", "Skipped: no Discord guild ID is configured for this production.")
		}
		return nil, results, t(lang, "有効な Discord guild ID がないため、candidate を検証できませんでした。", "Validation could not confirm candidates because this production has no effective Discord guild ID."), checks
	}
	trimmedToken := strings.TrimSpace(botToken)
	if trimmedToken == "" {
		for i := range results {
			results[i].Reason = t(lang, "Skip: 検証に使う Discord bot token がありません。", "Skipped: Discord bot token is not available for validation.")
		}
		return nil, results, t(lang, "この runtime では Discord bot token が利用できないため、Discord 照合を実行できませんでした。", "Validation could not contact Discord because the bot token is unavailable in this runtime."), checks
	}

	body, status, err := botDo("GET", discordAPI+"/guilds/"+url.PathEscape(effectiveGuildID)+"/channels", nil, trimmedToken)
	if err != nil {
		for i := range results {
			results[i].Reason = t(lang, "Skip: Discord から guild channel list を取得できませんでした。", "Skipped: failed to read the guild channel list from Discord.")
		}
		return nil, results, t(lang, "Discord から現在の guild channel list を取得できませんでした: ", "Validation could not read the current guild channel list from Discord: ") + err.Error(), checks
	}
	if status != http.StatusOK {
		for i := range results {
			results[i].Reason = fmt.Sprintf(t(lang, "Skip: guild channel list 取得時に Discord が HTTP %d を返しました。", "Skipped: Discord returned HTTP %d while listing guild channels."), status)
		}
		return nil, results, fmt.Sprintf(t(lang, "Discord から現在の guild channel list を取得できませんでした (HTTP %d)。", "Validation could not read the current guild channel list from Discord (HTTP %d)."), status), checks
	}

	var guildChannels []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Type     int    `json:"type"`
		ParentID string `json:"parent_id"`
	}
	if err := json.Unmarshal(body, &guildChannels); err != nil {
		for i := range results {
			results[i].Reason = t(lang, "Skip: Discord channel list の応答を解析できませんでした。", "Skipped: Discord channel list response could not be parsed.")
		}
		return nil, results, t(lang, "Discord が返した guild channel list を解析できませんでした。", "Validation could not parse the guild channel list returned by Discord."), checks
	}

	channelsByID := make(map[string]struct {
		Name     string
		Type     int
		ParentID string
	}, len(guildChannels))
	for _, ch := range guildChannels {
		channelsByID[strings.TrimSpace(ch.ID)] = struct {
			Name     string
			Type     int
			ParentID string
		}{
			Name:     strings.TrimSpace(ch.Name),
			Type:     ch.Type,
			ParentID: strings.TrimSpace(ch.ParentID),
		}
	}

	otherProjectRefs := map[string][]string{}
	for _, wh := range allWebhooks {
		channelID := strings.TrimSpace(wh.DiscordChannelID)
		projectID := strings.TrimSpace(wh.KitsuProjectID)
		if channelID == "" || projectID == "" || projectID == project.KitsuProjectID {
			continue
		}
		otherProjectRefs[channelID] = append(otherProjectRefs[channelID], projectID)
	}
	for channelID, refs := range otherProjectRefs {
		sort.Strings(refs)
		otherProjectRefs[channelID] = compactSortedStrings(refs)
	}

	recordedCategoryID := strings.TrimSpace(project.DiscordCategoryID)
	deletable := make([]connectedProductionChannelValidationResult, 0, len(results))
	skipped := make([]connectedProductionChannelValidationResult, 0)
	for _, result := range results {
		current, ok := channelsByID[result.ChannelID]
		if !ok {
			result.Reason = t(lang, "Skip: 現在の guild channel list にこの channel が見つかりません。", "Skipped: current channel was not found in the guild channel list.")
			skipped = append(skipped, result)
			continue
		}
		result.CurrentName = current.Name
		result.CurrentType = current.Type
		result.CurrentParent = current.ParentID
		if current.Type != 0 {
			result.Reason = fmt.Sprintf(t(lang, "Skip: 現在の channel type は %d で、期待する text channel type 0 ではありません。", "Skipped: current channel type is %d, not the expected text channel type 0."), current.Type)
			skipped = append(skipped, result)
			continue
		}
		if refs := otherProjectRefs[result.ChannelID]; len(refs) > 0 {
			result.Reason = t(lang, "Skip: 同じ Discord channel ID が他 production からも参照されています (", "Skipped: the same Discord channel ID is also referenced by another production (") + strings.Join(refs, ", ") + ")."
			skipped = append(skipped, result)
			continue
		}
		if recordedCategoryID != "" && strings.TrimSpace(current.ParentID) != recordedCategoryID {
			result.Reason = t(lang, "Skip: 現在の parent/category が保存済み project category と一致しません。", "Skipped: current parent/category no longer matches the recorded project category.")
			skipped = append(skipped, result)
			continue
		}
		if len(result.StoredNames) > 0 {
			matchesStoredName := false
			for _, name := range result.StoredNames {
				if strings.TrimSpace(name) == current.Name {
					matchesStoredName = true
					break
				}
			}
			if !matchesStoredName {
				result.Reason = t(lang, "Skip: 現在の channel name が保存済み channel name 参照と一致しません。", "Skipped: current channel name does not match the stored channel name reference.")
				skipped = append(skipped, result)
				continue
			}
		}
		deletable = append(deletable, result)
	}

	summary := fmt.Sprintf(t(lang, "保存済み channel candidate %d 件を現在の Discord guild state と照合しました。この pass ではまだ何も削除しません。", "Validated %d stored channel candidate(s) against the current Discord guild state. This pass still does not delete anything."), len(results))
	return deletable, skipped, summary, checks
}

func isDiscordNotFoundDeleteError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 404 Not Found")
}

func deleteOperationErrorClass(err error) string {
	if err == nil {
		return "unknown"
	}
	switch {
	case strings.Contains(err.Error(), "HTTP 401 Unauthorized"):
		return "authentication_failed"
	case strings.Contains(err.Error(), "HTTP 403 Forbidden"):
		return "permission_denied"
	case strings.Contains(err.Error(), "HTTP 404 Not Found"):
		return "resource_not_found"
	case strings.Contains(err.Error(), "HTTP 429"):
		return "rate_limited"
	case strings.Contains(err.Error(), "HTTP "):
		return "discord_api_error"
	default:
		return "operation_failed"
	}
}

func safeDeleteOperationMessage(lang, stage string, err error) string {
	class := deleteOperationErrorClass(err)
	slog.Warn("Connected Production delete failed", "stage", stage, "error_class", class)
	if class == "resource_not_found" {
		return t(lang, "対象のDiscordリソースはすでに存在しません。", "The Discord resource was already missing.")
	}
	if stage == "database_cleanup" {
		return t(lang, "KitsuSyncの連携状態の整理に失敗しました。要確認です。接続状態を見直してください。", "KitsuSync connection cleanup failed. Review the connection state.")
	}
	return t(lang, "Discordリソースの削除に失敗しました。権限と接続状態を確認してください。", "Discord resource deletion failed. Check the Bot permissions and connection state.")
}

var (
	strongDeleteValidateCandidates = validateConnectedProductionChannelCandidates
	strongDeleteChannelDelete      = DeleteChannel
	strongDeleteConnectionCleanup  = deleteProjectConnectionOnlyForMutation
	strongDeleteInspectWebhook     = inspectRoutingWebhook
	strongDeleteInspectChannel     = inspectManagedDiscordChannel
)

type routingWebhookIdentity struct {
	ID        string `json:"id"`
	Type      int    `json:"type"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
}

func routingWebhookMatches(identity routingWebhookIdentity, id, guild, channel string) bool {
	return identity.Type == 1 && strings.TrimSpace(identity.ID) == strings.TrimSpace(id) &&
		strings.TrimSpace(identity.GuildID) == strings.TrimSpace(guild) && strings.TrimSpace(identity.ChannelID) == strings.TrimSpace(channel)
}

func discordWebhookID(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "discord.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("Discord webhook response was not a trusted URL")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "webhooks" || strings.TrimSpace(parts[2]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", errors.New("Discord webhook response was incomplete")
	}
	return parts[2], nil
}

func inspectRoutingWebhook(raw string) (routingWebhookIdentity, bool, error) {
	var identity routingWebhookIdentity
	if _, err := discordWebhookID(raw); err != nil {
		return identity, true, nil
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimSpace(raw), nil)
	if err != nil {
		return identity, false, errors.New("managed webhook read could not be prepared")
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		return identity, false, errors.New("managed webhook could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusUnauthorized {
		return identity, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return identity, false, errors.New("managed webhook read failed")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&identity); err != nil {
		return routingWebhookIdentity{}, false, errors.New("managed webhook response was invalid")
	}
	return identity, false, nil
}

type managedDiscordChannelIdentity struct {
	ID      string `json:"id"`
	GuildID string `json:"guild_id"`
	Type    int    `json:"type"`
}

func inspectManagedDiscordChannel(channelID, token string) (managedDiscordChannelIdentity, int, error) {
	var identity managedDiscordChannelIdentity
	body, status, err := botDo(http.MethodGet, discordAPI+"/channels/"+url.PathEscape(strings.TrimSpace(channelID)), nil, token)
	if err != nil {
		return identity, status, err
	}
	if status == http.StatusNotFound {
		return identity, status, nil
	}
	if status != http.StatusOK {
		return identity, status, fmt.Errorf("Discord channel read failed")
	}
	if err := json.Unmarshal(body, &identity); err != nil || strings.TrimSpace(identity.ID) != strings.TrimSpace(channelID) {
		return managedDiscordChannelIdentity{}, status, fmt.Errorf("Discord channel response was invalid")
	}
	return identity, status, nil
}

func listStrongDeleteWebhookInventory(db *gorm.DB, productionID string) ([]model.ProjectWebhook, []model.ProjectWebhook, error) {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return nil, nil, gorm.ErrInvalidData
	}
	var all []model.ProjectWebhook
	if err := db.Order("kitsu_project_id asc, id asc").Find(&all).Error; err != nil {
		return nil, nil, err
	}
	var production []model.ProjectWebhook
	if err := db.Where("kitsu_project_id = ?", productionID).Order("id asc").Find(&production).Error; err != nil {
		return nil, nil, err
	}
	return all, production, nil
}

func recoverableStrongDeleteTargets(db *gorm.DB, project model.Project, channelID string, webhooks []model.ProjectWebhook, allWebhooks []model.ProjectWebhook, token string) ([]model.ManagedChannelDeletionTarget, bool) {
	targets := make([]model.ManagedChannelDeletionTarget, 0)
	rows := make([]model.ProjectWebhook, 0)
	for _, webhook := range webhooks {
		if strings.TrimSpace(webhook.DiscordChannelID) == strings.TrimSpace(channelID) {
			rows = append(rows, webhook)
		}
	}
	if len(rows) == 0 {
		return nil, false
	}
	for _, webhook := range allWebhooks {
		if strings.TrimSpace(webhook.DiscordChannelID) == strings.TrimSpace(channelID) && strings.TrimSpace(webhook.KitsuProjectID) != project.KitsuProjectID {
			return nil, false
		}
	}
	for _, webhook := range rows {
		tombstone, err := model.FindManagedChannelDeletion(db, webhook.ID)
		webhookID, idErr := discordWebhookID(webhook.WebhookURL)
		if err != nil || idErr != nil || tombstone == nil || tombstone.Disposition == model.ChannelDeletionComplete ||
			tombstone.ProductionID != project.KitsuProjectID || tombstone.GuildID != project.DiscordGuildID ||
			tombstone.ChannelID != channelID || tombstone.DiscordWebhookID != webhookID {
			return nil, false
		}
		targets = append(targets, model.ManagedChannelDeletionTarget{WebhookRowID: webhook.ID, GuildID: tombstone.GuildID, ChannelID: tombstone.ChannelID, DiscordWebhookID: tombstone.DiscordWebhookID})
	}
	_, status, err := strongDeleteInspectChannel(channelID, token)
	if err != nil || status != http.StatusNotFound {
		return nil, false
	}
	return targets, true
}

func executeConnectedProductionStrongDelete(lang string, project model.Project, effectiveGuildID, botToken string, db *gorm.DB) connectedProductionChannelDeleteExecution {
	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	defer unlockApply()
	currentProject := model.FindProjectByKitsuID(db, project.KitsuProjectID)
	if currentProject == nil {
		return connectedProductionChannelDeleteExecution{ConnectionError: t(lang, "連携先が更新されたため削除を中止しました。再読み込みしてください。", "The Production connection changed. Reload and retry.")}
	}
	project = *currentProject
	mutationToken, mutationErr := model.BeginProductionDeletionMutation(db, project.KitsuProjectID)
	if mutationErr != nil {
		return connectedProductionChannelDeleteExecution{ConnectionError: safeDeleteOperationMessage(lang, "database_cleanup", mutationErr)}
	}
	finishFailedMutation := func(result *connectedProductionChannelDeleteExecution) {
		if result.ConnectionDeleted {
			return
		}
		if err := model.FinishProductionDeletionMutation(db, project.KitsuProjectID, mutationToken, true); err != nil {
			result.ConnectionError = t(lang, "削除の復旧状態を保存できません。現在の状態を確認してください。", "Deletion recovery state could not be persisted. Inspect the current state before retrying.")
		}
	}
	if strings.TrimSpace(project.DiscordGuildID) != "" {
		effectiveGuildID = strings.TrimSpace(project.DiscordGuildID)
	}
	allWebhooks, webhooks, inventoryErr := listStrongDeleteWebhookInventory(db, project.KitsuProjectID)
	if inventoryErr != nil {
		result := connectedProductionChannelDeleteExecution{ConnectionError: safeDeleteOperationMessage(lang, "database_cleanup", inventoryErr)}
		finishFailedMutation(&result)
		return result
	}
	candidates := buildConnectedProductionChannelCandidates(webhooks)
	deletable, skipped, summary, checks := strongDeleteValidateCandidates(lang, project, effectiveGuildID, botToken, allWebhooks, candidates)
	result := connectedProductionChannelDeleteExecution{
		ValidationSummary: summary,
		Checks:            checks,
		Skipped:           skipped,
	}
	webhookTargetsByChannel := make(map[string][]model.ManagedChannelDeletionTarget)
	verifiedDeletable := make([]connectedProductionChannelValidationResult, 0, len(deletable))
	recoveredDeletable := make([]connectedProductionChannelValidationResult, 0)
	for i := 0; i < len(skipped); {
		candidate := skipped[i]
		if targets, ok := recoverableStrongDeleteTargets(db, project, candidate.ChannelID, webhooks, allWebhooks, botToken); ok {
			webhookTargetsByChannel[candidate.ChannelID] = targets
			recoveredDeletable = append(recoveredDeletable, candidate)
			skipped = append(skipped[:i], skipped[i+1:]...)
			continue
		}
		i++
	}
	result.Skipped = skipped
	for _, candidate := range deletable {
		rows := make([]model.ProjectWebhook, 0)
		for _, webhook := range webhooks {
			if strings.TrimSpace(webhook.DiscordChannelID) == strings.TrimSpace(candidate.ChannelID) {
				rows = append(rows, webhook)
			}
		}
		if len(rows) == 0 {
			candidate.Reason = t(lang, "保存済みWebhookを確認できません。", "Stored webhook identity could not be verified.")
			result.Skipped = append(result.Skipped, candidate)
			continue
		}
		verified := make([]model.ManagedChannelDeletionTarget, 0, len(rows))
		valid := true
		for _, webhook := range rows {
			webhookID, parseErr := discordWebhookID(webhook.WebhookURL)
			if parseErr != nil {
				valid = false
				break
			}
			identity, missing, inspectErr := strongDeleteInspectWebhook(webhook.WebhookURL)
			if inspectErr != nil || missing || !routingWebhookMatches(identity, webhookID, project.DiscordGuildID, candidate.ChannelID) {
				valid = false
				break
			}
			verified = append(verified, model.ManagedChannelDeletionTarget{WebhookRowID: webhook.ID, GuildID: project.DiscordGuildID, ChannelID: candidate.ChannelID, DiscordWebhookID: webhookID})
		}
		if !valid {
			candidate.Reason = t(lang, "Discord Webhookの所有先を確認できません。", "Discord webhook ownership could not be verified.")
			result.Skipped = append(result.Skipped, candidate)
			continue
		}
		webhookTargetsByChannel[candidate.ChannelID] = verified
		verifiedDeletable = append(verifiedDeletable, candidate)
	}
	verifiedDeletable = append(verifiedDeletable, recoveredDeletable...)
	deletable = verifiedDeletable
	var deletionTargets []model.ManagedChannelDeletionTarget
	for _, candidate := range deletable {
		deletionTargets = append(deletionTargets, webhookTargetsByChannel[candidate.ChannelID]...)
	}
	if len(deletionTargets) > 0 {
		allowLastRoute := len(result.Skipped) == 0 && len(deletable) == len(candidates)
		if err := model.BeginManagedChannelDeletionBatchForProductionTeardown(db, project.KitsuProjectID, deletionTargets, time.Now().UTC(), allowLastRoute, mutationToken); err != nil {
			for _, candidate := range deletable {
				candidate.Reason = safeDeleteOperationMessage(lang, "database_cleanup", err)
				result.Failed = append(result.Failed, candidate)
			}
			deletable = nil
		}
	}
	for _, candidate := range deletable {
		if err := strongDeleteChannelDelete(candidate.ChannelID, botToken); err != nil {
			failed := candidate
			failed.Reason = safeDeleteOperationMessage(lang, "channel_delete", err)
			for _, target := range webhookTargetsByChannel[candidate.ChannelID] {
				if stateErr := model.RecordManagedChannelDeletionFailure(db, target.WebhookRowID, "discord_channel_delete_failed"); stateErr != nil {
					failed.Reason = t(lang, "削除状態の復旧情報を保存できません。", "Deletion recovery state could not be persisted.")
				}
			}
			result.Failed = append(result.Failed, failed)
			continue
		}
		finalized := true
		for _, target := range webhookTargetsByChannel[candidate.ChannelID] {
			if err := model.FinalizeManagedChannelDeletionForProductionTeardown(db, target.WebhookRowID, time.Now().UTC(), mutationToken); err != nil {
				finalized = false
				break
			}
		}
		if !finalized {
			warn := candidate
			warn.Reason = t(lang, "Discord削除後のデータベース整理に失敗しました。", "Database finalization failed after Discord deletion.")
			result.CleanupWarnings = append(result.CleanupWarnings, warn)
			continue
		}
		result.Deleted = append(result.Deleted, candidate)
		result.DeletedWebhookCount += len(webhookTargetsByChannel[candidate.ChannelID])
	}

	categoryID := strings.TrimSpace(project.DiscordCategoryID)
	result.CategoryDeleted = categoryID == ""
	canRemoveConnection := len(result.Failed) == 0 && len(result.Skipped) == 0 && len(result.CleanupWarnings) == 0
	if categoryID != "" && !canRemoveConnection {
		result.CategoryError = t(lang, "管理対象チャンネルの削除が未完了のため、カテゴリ削除を保留しました。", "Category deletion was deferred because managed channel cleanup is incomplete.")
	}
	if canRemoveConnection && categoryID != "" {
		if err := strongDeleteChannelDelete(categoryID, botToken); err != nil {
			if isDiscordNotFoundDeleteError(err) {
				result.CategoryDeleted = true
			} else {
				result.CategoryError = safeDeleteOperationMessage(lang, "category_delete", err)
				canRemoveConnection = false
			}
		} else {
			result.CategoryDeleted = true
		}
	}
	if canRemoveConnection && result.CategoryDeleted {
		if err := strongDeleteConnectionCleanup(project.KitsuProjectID, db, mutationToken); err != nil {
			result.ConnectionError = safeDeleteOperationMessage(lang, "database_cleanup", err)
		} else {
			result.ConnectionDeleted = true
		}
	} else if result.CategoryError != "" {
		result.ConnectionError = t(lang, "Discord 側の削除が完了していないため、連携情報の削除は止めました。", "Connection cleanup was skipped because Discord deletion did not complete.")
	}
	_, remaining, remainingErr := listStrongDeleteWebhookInventory(db, project.KitsuProjectID)
	if remainingErr != nil {
		if result.ConnectionError == "" {
			result.ConnectionError = safeDeleteOperationMessage(lang, "database_cleanup", remainingErr)
		}
	} else {
		result.RemainingWebhooks = len(remaining)
	}
	finishFailedMutation(&result)
	return result
}

func renderConnectedProductionStrongDeleteResultPage(lang string, r *http.Request, project model.Project, result connectedProductionChannelDeleteExecution) string {
	renderList := func(items []connectedProductionChannelValidationResult, includeReason bool) string {
		if len(items) == 0 {
			return `<p class="field-help" style="margin:0">` + esc(t(lang, "なし", "None")) + `</p>`
		}
		var out strings.Builder
		out.WriteString(`<ul class="list-tight" style="margin:0;padding-left:18px">`)
		for _, item := range items {
			line := `<code>` + esc(item.ChannelID) + `</code>`
			if strings.TrimSpace(item.CurrentName) != "" {
				line += ` <span class="hint">(` + esc(item.CurrentName) + `)</span>`
			} else if len(item.StoredNames) > 0 {
				line += ` <span class="hint">(` + esc(strings.Join(item.StoredNames, ", ")) + `)</span>`
			}
			if includeReason && strings.TrimSpace(item.Reason) != "" {
				line += `<div class="field-help">` + esc(item.Reason) + `</div>`
			}
			out.WriteString(`<li>` + line + `</li>`)
		}
		out.WriteString(`</ul>`)
		return out.String()
	}

	statusLabel := t(lang, "\u524a\u9664\u304c\u5b8c\u4e86\u3057\u307e\u3057\u305f", "Delete completed")
	statusClass := "ok"
	summary := t(lang, "\u3053\u306e production \u306e KitsuSync \u9023\u643a\u3068 Discord \u5074\u306e\u7247\u4ed8\u3051\u3092\u5b9f\u884c\u3057\u307e\u3057\u305f\u3002", "This delete removed the connection and cleaned up the Discord side for this production.")
	if result.ConnectionDeleted == false {
		statusLabel = t(lang, "\u4e00\u90e8\u78ba\u8a8d\u304c\u5fc5\u8981\u3067\u3059", "Review required")
		statusClass = "warn"
		summary = t(lang, "\u524a\u9664\u306f\u5b9f\u884c\u3057\u307e\u3057\u305f\u304c\u3001\u4e00\u90e8\u306e\u78ba\u8a8d\u304c\u6b8b\u3063\u3066\u3044\u307e\u3059\u3002", "Delete was attempted, but some review notes remain.")
	}

	completionBody, completionStatusClass, completionStatusLabel, completionSummary := renderConnectedProductionCompletionAction(lang, project, result)
	body := fmt.Sprintf(`
<div class="section-stack">
  <div class="section-card glass">
    <div class="page-heading" style="margin-bottom:14px">
      <div>
        <div class="eyebrow">%s</div>
        <h2 style="margin:6px 0 0">%s</h2>
        <p class="hint" style="margin:6px 0 0">%s</p>
      </div>
      <span class="status-pill %s">%s</span>
    </div>
    <div class="metric-grid">
      <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host">%s</div></div>
      <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host"><code>%s</code></div></div>
      <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
      <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
    </div>
    <p class="field-help" style="margin:12px 0 0">%s</p>
  </div>
  <div class="section-card glass">
    <div class="eyebrow">%s</div>
    %s
  </div>
  <div class="section-card glass">
    <div class="eyebrow">%s</div>
    %s
  </div>
  <div class="section-card glass">
    <div class="eyebrow">%s</div>
    %s
  </div>
  <div class="section-card glass" style="border-color:#ffd4a8">
    <div class="page-heading" style="margin-bottom:14px">
      <div>
        <div class="eyebrow">%s</div>
        <h3 style="margin:6px 0 0">%s</h3>
        <p class="hint" style="margin:6px 0 0">%s</p>
      </div>
      <span class="status-pill %s">%s</span>
    </div>
    %s
  </div>
  <div class="button-row">
    <a class="btn" href="%s">%s</a>
  </div>
</div>`,
		esc(t(lang, "CONNECTED PRODUCTION", "CONNECTED PRODUCTION")),
		esc(t(lang, "\u9023\u643a\u3068 Discord \u30c1\u30e3\u30f3\u30cd\u30eb\u3092\u524a\u9664", "Remove connection and Discord channels")),
		esc(summary),
		statusClass,
		esc(statusLabel),
		esc(t(lang, "プロダクション", "Production")),
		esc(project.Name),
		esc(t(lang, "Project ID", "Project ID")),
		esc(project.KitsuProjectID),
		esc(t(lang, "\u500b\u5225\u306b\u524a\u9664\u3057\u305f channel", "Channels deleted individually")),
		len(result.Deleted),
		esc(t(lang, "\u78ba\u8a8d\u30e1\u30e2", "Review notes")),
		len(result.Failed)+len(result.CleanupWarnings)+len(result.Skipped),
		esc(result.ValidationSummary),
		esc(t(lang, "\u500b\u5225\u306b\u524a\u9664\u3067\u304d\u305f channel", "Channels deleted individually")),
		renderList(result.Deleted, false),
		esc(t(lang, "\u500b\u5225\u78ba\u8a8d\u3067\u304d\u306a\u304b\u3063\u305f channel", "Channels not confirmed individually")),
		renderList(result.Skipped, true),
		esc(t(lang, "\u524a\u9664\u5931\u6557 / DB \u6ce8\u610f", "Delete failures / DB notes")),
		renderList(append(append([]connectedProductionChannelValidationResult{}, result.Failed...), result.CleanupWarnings...), true),
		esc(t(lang, "\u524a\u9664\u7d50\u679c", "Delete result")),
		esc(t(lang, "\u9023\u643a\u306e\u524a\u9664\u7d50\u679c", "Delete result")),
		esc(completionSummary),
		completionStatusClass,
		esc(completionStatusLabel),
		completionBody,
		esc(withLang("/bot/admin/projects", r)),
		esc(t(lang, "\u9023\u643a\u6e08\u307f\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u7ba1\u7406\u306b\u623b\u308b", "Back to Connected Productions")),
	)
	return adminPage(lang, t(lang, "連携済みプロダクション管理", "Connected Productions"), r, body)
}

func renderConnectedProductionCompletionAction(lang string, project model.Project, result connectedProductionChannelDeleteExecution) (string, string, string, string) {
	if result.ConnectionDeleted {
		body := `<p class="field-help" style="margin:0 0 12px">` +
			esc(t(lang, "KitsuSync 側の連携情報も削除しました。実行後はこの production を再設定できます。", "The KitsuSync-side connection data was also removed. You can set this production up again now.")) +
			`</p><p class="field-help" style="margin:0">` +
			esc(t(lang, "Discord チャンネルと category もこの削除に含めて処理しました。", "Discord channels and the category were also included in this delete.")) +
			`</p>`
		return body, "ok", t(lang, "削除しました", "Deleted"), t(lang, "この production を再設定できます。", "You can set this production up again.")
	}

	blockers := []string{}
	if result.CategoryError != "" {
		blockers = append(blockers, t(lang, "Discord category を削除できませんでした: ", "Could not delete the Discord category: ")+result.CategoryError)
	}
	if result.ConnectionError != "" {
		blockers = append(blockers, result.ConnectionError)
	}
	if result.RemainingWebhooks > 0 {
		blockers = append(blockers, fmt.Sprintf(t(lang, "まだ整理が必要な連携先が %d 件あります", "There are still %d connection rows to clean up."), result.RemainingWebhooks))
	}
	if len(result.CleanupWarnings) > 0 {
		blockers = append(blockers, fmt.Sprintf(t(lang, "削除後に要確認の項目が %d 件あります", "There are %d items that still need follow-up."), len(result.CleanupWarnings)))
	}
	if len(blockers) == 0 {
		blockers = append(blockers, t(lang, "一部の削除が完了していません。", "Some parts of the delete did not complete."))
	}
	var out strings.Builder
	out.WriteString(`<p class="field-help" style="margin:0 0 12px">` + esc(t(lang, "削除は実行しましたが、まだ要確認の項目があります。", "The delete was attempted, but some follow-up is still needed.")) + `</p>`)
	out.WriteString(`<ul class="list-tight" style="margin:0;padding-left:18px">`)
	for _, blocker := range blockers {
		out.WriteString(`<li>` + esc(blocker) + `</li>`)
	}
	out.WriteString(`</ul>`)
	return out.String(), "warn", t(lang, "要確認", "Needs review"), t(lang, "一部の削除結果を確認してください。", "Please review the remaining delete notes.")
}

func compactSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := []string{values[0]}
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

// HealthHandler shows the runtime health dashboard.
// Level 1 (default): overall status + last sync.
// Level 2 (details accordion): polling, sends, forum settings table, resource usage.
// Forum settings issues auto-expand the details accordion.
// Legacy POST action=reconnect_webhook is rejected so repairs stay inside Notifications Apply.
func HealthHandler(db *gorm.DB, pollInterval time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.Method == http.MethodGet && r.URL.Query().Get("legacy") != "1" {
			renderIAHealth(w, r, db, pollInterval)
			return
		}

		// Handle reconnect action
		if r.Method == http.MethodPost && r.FormValue("action") == "reconnect_webhook" {
			http.Error(w, t(lang, "通知先の修復は Production の Notifications Apply から実行してください。", "Repair this destination through the Production Notifications Apply flow."), http.StatusConflict)
			return
		}

		snap := Stats.Snapshot()
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

		// Overall health determination. Forum settings per Production replace the
		// old per-webhook health: /bot/admin/health must not assume ProjectWebhooks.
		type forumHealthRow struct {
			name     string
			routes   int
			roles    int
			tags     int
			missing  bool
			problems []model.ForumSettingsProblem
			check    string // automatic forum creation failure, if any
		}
		var forumRows []forumHealthRow
		forumIssueCount := 0
		for _, p := range model.ListProjects(db) {
			check := model.GetSetting(db, forumAutoCreateCheckKey(p.KitsuProjectID))
			settings, ok, err := model.LoadProductionForumSettings(db, p.KitsuProjectID)
			if err != nil || !ok {
				forumIssueCount++
				forumRows = append(forumRows, forumHealthRow{name: p.Name, missing: true, check: check})
				continue
			}
			problems := model.ForumSettingsProblems(settings)
			if len(problems) > 0 || check != "" {
				forumIssueCount++
			}
			forumRows = append(forumRows, forumHealthRow{name: p.Name, routes: len(settings.Routes), roles: len(settings.StatusRoles), tags: len(settings.Tags), problems: problems, check: check})
		}
		pollingActive := !snap.LastPollTime.IsZero()
		isHealthy := pollingActive && forumIssueCount == 0

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
			withLang("/bot/admin/health", r), esc(t(lang, "状態を更新", "Refresh status")),
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

		// Forum settings health table
		var forumTableHTML string
		if len(forumRows) == 0 {
			forumTableHTML = `<p class="hint">` + esc(t(lang, "接続済みProductionなし（接続後に表示されます）。", "No connected productions (shown after connection).")) + `</p>`
		} else {
			var forumRowsHTML strings.Builder
			okForumCount := 0
			for _, row := range forumRows {
				var statusCell string
				if row.missing || len(row.problems) > 0 || row.check != "" {
					statusCell = `<span class="status-pill bad">` + esc(t(lang, "要確認", "Attention")) + `</span>`
					if row.missing {
						statusCell += `<div style="font-size:.78rem;color:var(--muted-2);margin-top:3px">` + esc(t(lang, "フォーラム設定が未保存です。Productionのフォーラム設定を保存してください。", "Forum settings are not saved yet. Save the Production's forum settings.")) + `</div>`
					}
					if row.check != "" {
						statusCell += `<div style="font-size:.78rem;color:var(--muted-2);margin-top:3px">` + esc(t(lang, "接続時のフォーラム自動作成に失敗しました。「Botでフォーラムとタグを作成」で再実行できます: ", "Automatic forum creation after connecting failed. Run \"Create forums and tags with the Bot\" to retry: ")+row.check) + `</div>`
					}
					for _, problem := range row.problems {
						statusCell += `<div style="font-size:.78rem;color:var(--muted-2);margin-top:3px">` + esc(forumProblemText(lang, problem, nil)) + `</div>`
					}
				} else {
					okForumCount++
					statusCell = `<span class="status-pill ok">` + esc(t(lang, "\u6b63\u5e38", "OK")) + `</span>`
				}
				forumRowsHTML.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%d</td><td>%d</td><td>%d</td><td>%s</td></tr>`,
					esc(row.name), row.routes, row.roles, row.tags, statusCell))
			}
			forumTableHTML = `<div class="button-row" style="margin:0 0 14px 0">` +
				`<span class="status-pill ok">` + esc(fmt.Sprintf(t(lang, "正常: %d", "Healthy: %d"), okForumCount)) + `</span>` +
				`<span class="status-pill bad">` + esc(fmt.Sprintf(t(lang, "要確認: %d", "Attention: %d"), forumIssueCount)) + `</span>` +
				`</div>` +
				`<div class="table-wrap"><table><thead><tr>` +
				`<th>` + esc(t(lang, "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8", "Project")) + `</th>` +
				`<th>` + esc(t(lang, "フォーラム数", "Forums")) + `</th>` +
				`<th>` + esc(t(lang, "ステータス役割", "Status roles")) + `</th>` +
				`<th>` + esc(t(lang, "タグ", "Tags")) + `</th>` +
				`<th>` + esc(t(lang, "\u72b6\u614b", "Status")) + `</th>` +
				`</tr></thead><tbody>` + forumRowsHTML.String() + `</tbody></table></div>`
			if forumIssueCount > 0 {
				forumTableHTML += `<p class="hint" style="color:#ff6a50;margin-top:10px">` +
					esc(t(lang, "要確認のフォーラム設定があります。Productionのフォーラム設定を確認して保存してください。",
						"Some forum settings need attention. Review and save them in the Production forum settings.")) + `</p>`
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
		if forumIssueCount > 0 || !pollingActive {
			detailsOpen = " open"
		}

		detailsAccordion := fmt.Sprintf(`
<details class="accordion"%s>
  <summary>
    <div class="accordion-summary-main">
      <div class="tile-title">%s</div>
      <div class="tile-sub">%s</div>
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
			esc(t(lang, "ランタイム詳細", "Runtime Details")),
			esc(t(lang, "Polling / Discord 送信 / フォーラム設定 / リソース", "Polling / Discord Sends / Forum Settings / Resources")),
			esc(t(lang, "\u30dd\u30fc\u30ea\u30f3\u30b0", "Polling")), pollRows,
			esc(t(lang, "Discord \u9001\u4fe1", "Discord Sends")), sendRows,
			esc(t(lang, "フォーラム設定", "Forum Settings")), forumTableHTML,
			esc(t(lang, "\u30ea\u30bd\u30fc\u30b9", "Resources")), resourceRows,
		)

		diagnosticsSection := `<div class="section-card glass"><div class="page-heading" style="margin-bottom:16px"><div><h3 style="margin:0">` +
			esc(t(lang, "通知と診断", "Delivery & Diagnostics")) +
			`</h3><p class="hint" style="margin:8px 0 0">` +
			esc(t(lang, "通知確認と runtime / configuration checks をまとめて確認できます。", "Review delivery verification with the current runtime / configuration checks.")) +
			`</p></div></div>` +
			renderDiagnosticsPanel(lang, r, db, func() (string, string, string, string) {
				return model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db), model.GetSetting(db, "discord.guildID"), model.GetSetting(db, "discord.webhook_url")
			}, withLang("/bot/admin/health", r), false) +
			`</div>`

		body := `<div class="section-stack">` + summaryCard + detailsAccordion + diagnosticsSection + `</div>` +
			`<div class="button-row" style="margin-top:16px">` +
			`<a class="btn-ghost" href="` + withLang("/bot/admin", r) + `">` + esc(t(lang, "\u7ba1\u7406\u753b\u9762\u3078", "Back to Admin")) + `</a>` +
			`</div>`

		fmt.Fprint(w, adminPage(lang, t(lang, "\u30b7\u30b9\u30c6\u30e0\u72b6\u614b", "System Status"), r, body))
	}
}

func UsersHandler(db *gorm.DB, kitsuHostname string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.URL.Query().Get("legacy") == "1" {
			if projectID := strings.TrimSpace(r.URL.Query().Get("project")); projectID != "" {
				if model.FindProjectByKitsuID(db, projectID) != nil {
					target := withLang("/bot/admin/projects?project="+url.QueryEscape(projectID)+"&tab=users", r)
					http.Redirect(w, r, target, http.StatusSeeOther)
					return
				}
			}
			http.Redirect(w, r, withLang("/bot/admin/users?msg=legacy-user-settings", r), http.StatusSeeOther)
			return
		}
		if r.URL.Query().Get("legacy") != "1" && strings.TrimSpace(r.URL.Query().Get("project")) == "" && r.Method == http.MethodPost {
			if r.FormValue("action") == "save_global_links" {
				if unlinkID := parseUint(r.FormValue("unlink_user_id")); unlinkID > 0 {
					if user := model.FindUserMapByID(db, unlinkID); user != nil {
						model.UpdateUserMap(db, unlinkID, user.KitsuName, user.KitsuEmail, "")
						model.UpdateUserMapDisplayName(db, unlinkID, "")
						db.Model(&model.UserMap{}).Where("id = ?", unlinkID).Update("discord_guild_id", "")
					}
					http.Redirect(w, r, globalUserLinkingURL(r, userLinkingGuildForRedirect(db, r), "saved"), http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, saveGlobalUserLinks(db, r), http.StatusSeeOther)
				return
			}
			id := parseUint(r.FormValue("user_id"))
			action := r.FormValue("action")
			saved := false
			redirectURL := withLang("/bot/admin/users?msg=error", r)
			selectedGuildID := ""
			if action == "remove_global_link" && id > 0 {
				selectedGuildID = userLinkingGuildForRedirect(db, r)
				if user := model.FindUserMapByID(db, id); user != nil {
					model.UpdateUserMap(db, id, user.KitsuName, user.KitsuEmail, "")
					model.UpdateUserMapDisplayName(db, id, "")
					db.Model(&model.UserMap{}).Where("id = ?", id).Update("discord_guild_id", "")
					saved = true
				}
			} else if action == "save_global_link" {
				saved, redirectURL = saveGlobalUserLink(db, r)
			}
			msg := "error"
			if saved {
				msg = "saved"
			}
			if action == "remove_global_link" {
				redirectURL = globalUserLinkingURL(r, selectedGuildID, msg)
			} else if action == "save_global_link" && !saved && redirectURL != "" {
				separator := "?"
				if strings.Contains(redirectURL, "?") {
					separator = "&"
				}
				redirectURL += separator + "msg=error"
			}
			if redirectURL == "" {
				redirectURL = withLang("/bot/admin/users?msg=error", r)
			}
			http.Redirect(w, r, redirectURL, http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodGet && r.URL.Query().Get("legacy") != "1" && r.URL.Query().Get("project") == "" {
			if id := parseUint(r.URL.Query().Get("edit")); id > 0 {
				if user := model.FindUserMapByID(db, id); user != nil {
					renderGlobalUserLinkForm(w, r, db, user)
					return
				}
			}
			renderGlobalUserLinking(w, r, db)
			return
		}
		selectedProjectID := strings.TrimSpace(r.URL.Query().Get("project"))
		if r.Method == http.MethodPost {
			if formProjectID := strings.TrimSpace(r.FormValue("assignment_project_id")); formProjectID != "" {
				selectedProjectID = formProjectID
			}
		}
		projects := model.ListProjects(db)
		project := configuredAssignmentProject(db, selectedProjectID)
		useProjectScoped := project != nil
		activeProjectID := ""
		if project != nil {
			activeProjectID = project.KitsuProjectID
		}
		usersPageURL := withLang("/bot/admin/users", r)
		if activeProjectID != "" {
			usersPageURL += "&project=" + url.QueryEscape(activeProjectID)
		}
		botEmail := botAccountEmail(db)
		kitsuPeople := filterAssignablePersons(ListKitsuPersons(kitsuHostname), botEmail)
		if project != nil && strings.TrimSpace(project.KitsuProjectID) != "" {
			if projectPeople := ListKitsuProjectParticipants(project.KitsuProjectID); len(projectPeople) > 0 {
				kitsuPeople = filterAssignablePersons(projectPeople, botEmail)
			}
		}
		projectUserRows := []model.ProjectUserMap{}
		legacyUserRows := model.ListUserMap(db)
		if useProjectScoped {
			projectUserRows = model.ListProjectUserMaps(db, project.ID)
		}

		if r.Method == http.MethodPost {
			if hasValidationOnlyProject(db) {
				http.Error(w, "validation-only profile is read-only", http.StatusForbidden)
				return
			}
			id := parseUint(r.FormValue("user_id"))
			name := strings.TrimSpace(r.FormValue("kitsu_name"))
			email := strings.TrimSpace(r.FormValue("kitsu_email"))
			discordID := strings.TrimSpace(r.FormValue("discord_id"))
			discordDisplayName := strings.TrimSpace(r.FormValue("discord_display_name"))
			previousName, previousEmail := "", ""
			if id > 0 {
				if useProjectScoped {
					if existing := model.FindProjectUserMapByID(db, id); existing != nil {
						previousName = strings.TrimSpace(existing.KitsuName)
						previousEmail = strings.TrimSpace(existing.KitsuEmail)
					}
				} else {
					if existing := model.FindUserMapByID(db, id); existing != nil {
						previousName = strings.TrimSpace(existing.KitsuName)
						previousEmail = strings.TrimSpace(existing.KitsuEmail)
					}
				}
			}
			switch r.FormValue("action") {
			case "delete":
				if useProjectScoped {
					model.DeleteProjectUserMapByID(db, id)
					deleteProjectCheckerAssignmentsForUser(db, project.ID, name, email)
				} else {
					model.DeleteUserMapByID(db, id)
					deleteLegacyCheckerAssignmentsForUser(db, name, email)
				}
			default:
				if name != "" && (discordID == "" || discordIDRegexp.MatchString(discordID)) {
					if useProjectScoped {
						if id > 0 {
							model.UpdateProjectUserMap(db, id, name, email, discordID)
						} else {
							model.UpsertProjectUserMap(db, project.ID, name, email, discordID)
						}
						if previousName != "" {
							syncProjectCheckerAssignmentUserIdentity(db, project.ID, previousName, previousEmail, name, email, discordID)
						}
					} else {
						if id > 0 {
							model.UpdateUserMap(db, id, name, email, discordID)
							model.UpdateUserMapDisplayName(db, id, discordDisplayName)
						} else {
							model.UpsertUserMapWithEmail(db, name, email, discordID)
							var created model.UserMap
							if db.Where("kitsu_name = ?", name).First(&created).Error == nil {
								model.UpdateUserMapDisplayName(db, created.ID, discordDisplayName)
							}
						}
						if previousName != "" {
							syncLegacyCheckerAssignmentUserIdentity(db, previousName, previousEmail, name, email)
						}
					}
				}
			}
			http.Redirect(w, r, usersPageURL+"&msg=saved", http.StatusSeeOther)
			return
		}

		if r.Method == http.MethodGet && r.URL.Query().Get("legacy") != "1" && r.URL.Query().Get("project") == "" {
			renderIAUsers(w, r, db)
			return
		}

		editID := parseUint(r.URL.Query().Get("edit"))
		selectedName, selectedEmail, selectedDiscordID, selectedDiscordDisplayName := "", "", "", ""
		if editID > 0 {
			if useProjectScoped {
				if editUser := model.FindProjectUserMapByID(db, editID); editUser != nil {
					selectedName = editUser.KitsuName
					selectedEmail = editUser.KitsuEmail
					selectedDiscordID = editUser.DiscordUserID
				}
			} else {
				if editUser := model.FindUserMapByID(db, editID); editUser != nil {
					selectedName = editUser.KitsuName
					selectedEmail = editUser.KitsuEmail
					selectedDiscordID = editUser.DiscordID
					selectedDiscordDisplayName = editUser.DiscordDisplayName
				}
			}
		}
		personOptions := buildAssignmentPersonOptions(kitsuAssignmentOptions(kitsuPeople), selectedName, selectedEmail, lang)

		var rows strings.Builder
		assignments := buildUnifiedAssignments(projectUserRows, legacyUserRows, useProjectScoped)
		for _, user := range assignments {
			discordID := `<span class="status-pill bad">` + t(lang, "ID未設定", "No ID") + `</span>`
			if strings.TrimSpace(user.DiscordID) != "" {
				discordID = `<code>` + esc(user.DiscordID) + `</code>`
			}
			actionHTML := `<span class="muted">` + t(lang, "ユーザー割り当て未作成", "Create a user assignment first") + `</span>`
			if user.RowID > 0 {
				actionHTML = fmt.Sprintf(`<div class="inline-actions"><a class="btn-ghost" href="%s">%s</a><form method="POST" class="delete-form" data-confirm="%s" data-require-text="%s"><input type="hidden" name="action" value="delete"><input type="hidden" name="assignment_project_id" value="%s"><input type="hidden" name="user_id" value="%d"><input type="hidden" name="kitsu_name" value="%s"><input type="hidden" name="kitsu_email" value="%s"><button class="btn-danger" type="submit">%s</button></form></div>`,
					usersPageURL+"&edit="+strconv.FormatUint(uint64(user.RowID), 10), t(lang, "編集", "Edit"), esc(user.KitsuName), t(lang, "削除", "delete"), esc(activeProjectID), user.RowID, esc(user.KitsuName), esc(user.KitsuEmail), t(lang, "削除", "Delete"))
			}
			rows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td></tr>`,
				esc(user.KitsuName), discordID, actionHTML))
		}
		if rows.Len() == 0 {
			rows.WriteString(`<tr><td colspan="3" class="muted">` + t(lang, "まだユーザー割り当てがありません。", "No user assignments yet.") + `</td></tr>`)
		}

		formTitle := t(lang, "ユーザー割り当てを追加", "Add User Assignment")
		userID := uint(0)
		if editID > 0 && (selectedName != "" || selectedEmail != "" || selectedDiscordID != "") {
			formTitle = t(lang, "ユーザー割り当てを編集", "Edit User Assignment")
			userID = editID
		}
		scopeHint := ""
		selectorCard := ""
		if useProjectScoped {
			scopeHint = `<p class="field-help">` + t(lang, "現在編集中の production: ", "Currently editing production: ") + `<strong>` + esc(project.Name) + `</strong></p>`
			var projectOptions strings.Builder
			for _, candidate := range projects {
				selected := ""
				if candidate.KitsuProjectID == activeProjectID {
					selected = ` selected`
				}
				projectOptions.WriteString(fmt.Sprintf(`<option value="%s"%s>%s</option>`, esc(candidate.KitsuProjectID), selected, esc(candidate.Name)))
			}
			selectorCard = `<div class="section-card glass"><h3>` + t(lang, "編集対象のプロダクション", "Editing production") + `</h3><p class="hint">` + t(lang, "ユーザー割り当てと reviewer / checker 設定はプロダクションごとに切り替えて編集します。", "Switch the production here to edit user assignments and reviewer/checker settings per production.") + `</p><form method="GET" action="/bot/admin/users"><input type="hidden" name="lang" value="` + esc(lang) + `"><div class="form-grid"><div><label>` + t(lang, "プロダクション", "Production") + `</label><select name="project">` + projectOptions.String() + `</select></div></div><div class="button-row"><button type="submit" class="btn">` + t(lang, "表示を切り替え", "Switch production") + `</button></div></form></div>`
		}
		assignmentActionRow := `<div class="button-row"><a class="btn-ghost" href="` + esc(usersPageURL) + `">` + t(lang, "キャンセル", "Cancel") + `</a><button type="submit" class="btn">` + t(lang, "保存", "Save") + `</button></div>`
		body := fmt.Sprintf(`
<div class="section-stack">
  %s
  <form method="POST" class="section-stack" style="margin:0">
    <div class="section-card glass">
      <h3>%s</h3>
      <p class="hint">%s</p>
      %s
      <input type="hidden" name="user_id" value="%d">
      <input type="hidden" name="assignment_project_id" value="%s">
      <input type="hidden" id="kitsuNameInput" name="kitsu_name" value="%s">
      <input type="hidden" id="kitsuEmailInput" name="kitsu_email" value="%s">
      <div class="form-grid">
        <div><label>%s</label><select id="personSelect" onchange="syncPersonSelect()" required>%s</select></div>
        <div><label>%s</label><input type="text" name="discord_id" value="%s" placeholder="123456789012345678"><div class="field-help">%s</div></div>
        <div><label>%s</label><input type="text" name="discord_display_name" value="%s"><div class="field-help">%s</div></div>
      </div>
      %s
    </div>
    <div class="section-card glass"><h3>%s</h3><div class="table-wrap"><table><thead><tr><th>%s</th><th>Discord ID</th><th>%s</th></tr></thead><tbody>%s</tbody></table></div></div>
  </form>
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
			selectorCard,
			formTitle,
			t(lang, "User = タスク割り当て時に @mention します。Reviewer / Checker 設定は notifications 画面から行います。", "User = @mentioned when a task is assigned. Reviewer / Checker settings are handled from the notifications screen."),
			scopeHint,
			userID, esc(activeProjectID), esc(selectedName), esc(selectedEmail),
			t(lang, "Kitsuユーザー", "Kitsu user"), personOptions,
			t(lang, "DiscordユーザーID", "Discord user ID"), esc(selectedDiscordID), t(lang, "未入力の場合は ID未設定 と表示されます。", "If empty, the UI will show No ID."),
			t(lang, "Discordユーザー表示名", "Discord display name"), esc(selectedDiscordDisplayName), t(lang, "通常画面にはIDではなく表示名を表示します。", "The normal UI shows this name instead of the internal ID."), assignmentActionRow,
			t(lang, "現在の割り当て", "Current assignments"), t(lang, "名前", "Name"), t(lang, "操作", "Actions"), rows.String(),
		)
		fmt.Fprint(w, adminPage(lang, t(lang, "ユーザー割り当て", "User Assignment"), r, body))
	}
}

func DriveHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		projectID := strings.TrimSpace(r.FormValue("kitsu_project_id"))
		if projectID == "" {
			http.Error(w, "a connected Production is required", http.StatusBadRequest)
			return
		}
		if model.IsValidationOnlyProject(db, projectID) {
			http.Error(w, "validation-only Production is read-only", http.StatusForbidden)
			return
		}
		storageURL := strings.TrimSpace(r.FormValue("storage_url"))
		if storageURL != "" && !validConfiguredStorageURL(storageURL) {
			http.Error(w, "storage URL must be an absolute HTTP or HTTPS URL", http.StatusBadRequest)
			return
		}
		if err := model.SetProjectStorageURL(db, projectID, storageURL); err != nil {
			target := withLang("/bot/admin/projects?project="+url.QueryEscape(projectID)+"&tab=storage-settings&drive_error=save", r)
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		if model.GetProjectStorageURL(db, projectID) != storageURL {
			target := withLang("/bot/admin/projects?project="+url.QueryEscape(projectID)+"&tab=storage-settings&drive_error=readback", r)
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		target := withLang("/bot/admin/projects?project="+url.QueryEscape(projectID)+"&tab=storage-settings&drive_saved=1", r)
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
}

func validConfiguredStorageURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "YOUR_FOLDER_ID") {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
}

type connectionStatus struct {
	Class string
	Label string
}

func connectionSecretStatus(value, lang string) connectionStatus {
	if strings.TrimSpace(value) == "" {
		return connectionStatus{Class: "warning", Label: t(lang, "未設定", "Not configured")}
	}
	return connectionStatus{Class: "muted", Label: t(lang, "保存済み", "Saved")}
}

const connectionSecretMask = "••••••••••••••••••••"

func connectionSecretDisplay(value, lang string) string {
	if strings.TrimSpace(value) == "" {
		return connectionStatusPill(connectionSecretStatus(value, lang))
	}
	return `<span class="secret-mask" aria-label="` + esc(t(lang, "保存済みtokenは非表示", "Saved token hidden")) + `">` + connectionSecretMask + `</span>`
}

func connectionHealthStatus(lang string, configured, healthy bool) connectionStatus {
	if !configured {
		return connectionStatus{Class: "warning", Label: t(lang, "未設定", "Not configured")}
	}
	if healthy {
		return connectionStatus{Class: "ok", Label: t(lang, "接続済み", "Connected")}
	}
	return connectionStatus{Class: "warn", Label: t(lang, "要確認", "Needs review")}
}

func connectionSaveLabel(lang string, service string, configured bool) string {
	if configured {
		return t(lang, "接続を確認", "Check connection")
	}
	return t(lang, service+"接続を保存", "Save "+service+" connection")
}

func canonicalConnectionHealthStatus(lang string, configured, healthy bool) connectionStatus {
	if !configured {
		return connectionStatus{Class: "warning", Label: t(lang, "\u672a\u8a2d\u5b9a", "Not configured")}
	}
	if healthy {
		return connectionStatus{Class: "ok", Label: t(lang, "\u63a5\u7d9a\u6e08\u307f", "Connected")}
	}
	return connectionStatus{Class: "warn", Label: t(lang, "要確認", "Needs review")}
}

func canonicalConnectionPageStatus(lang string, readiness SharedBotRuntimeReadiness, kitsuHealthy, discordHealthy bool) connectionStatus {
	if !readiness.KitsuConfigured || !readiness.DiscordConfigured {
		return canonicalConnectionHealthStatus(lang, false, false)
	}
	return canonicalConnectionHealthStatus(lang, true, kitsuHealthy && discordHealthy)
}

func canonicalConnectionStatuses(lang string, db *gorm.DB, readiness SharedBotRuntimeReadiness, runtimeHealthy func() bool) (connectionStatus, connectionStatus) {
	kitsuHealthy := false
	if runtimeHealthy != nil {
		kitsuHealthy = runtimeHealthy()
	} else {
		kitsuHealthy = checkKitsuStatus(normalizeKitsuHostname(KitsuHostForUI(db))).Authenticated
	}
	guildID := strings.TrimSpace(model.GetSetting(db, "discord.guildID"))
	botToken := storedRuntimeDiscordBotToken(db)
	discord := checkDiscordStatus(botToken, guildID)
	discordHealthy := discord.BotValid && (guildID == "" || discord.GuildValid)
	return canonicalConnectionHealthStatus(lang, readiness.KitsuConfigured, kitsuHealthy), canonicalConnectionHealthStatus(lang, readiness.DiscordConfigured, discordHealthy)
}

func connectionStatusPill(value connectionStatus) string {
	return `<span class="status-pill ` + esc(value.Class) + `" role="status">` + esc(value.Label) + `</span>`
}

func renderConnectionsPageSafeWithRuntime(w http.ResponseWriter, r *http.Request, db *gorm.DB, runtimeHealthy func() bool) {
	lang := currentLang(r)
	hostRaw := strings.TrimSpace(KitsuHostForUI(db))
	host := safeKitsuHostDisplay(hostRaw)
	readinessHost := normalizeKitsuHostname(hostRaw)
	if host == "" {
		host = t(lang, "未設定", "Not configured")
	}
	botToken := storedRuntimeDiscordBotToken(db)
	readiness := sharedBotRuntimeReadiness(db, readinessHost, botToken)
	kitsuStatus, discordStatus := canonicalConnectionStatuses(lang, db, readiness, runtimeHealthy)
	kitsuHealthy := kitsuStatus.Class == "ok"
	discordHealthy := discordStatus.Class == "ok"
	pageStatus := canonicalConnectionPageStatus(lang, readiness, kitsuHealthy, discordHealthy)
	statusClass, statusLabel := pageStatus.Class, pageStatus.Label
	statusHint := t(lang, "Kitsu接続とDiscord Bot接続の状態を確認してください。", "Review the Kitsu and Discord Bot connection states.")
	if !readiness.KitsuConfigured || !readiness.DiscordConfigured {
		statusHint = t(lang, "必要な接続を設定してください。", "Configure the required connections.")
	} else if !kitsuHealthy || !discordHealthy {
		statusHint = t(lang, "KitsuまたはDiscord Botの接続を確認してください。", "Check the Kitsu or Discord Bot connection.")
	}
	if r.URL.Query().Get("legacy") != "1" {
		body := renderConnectionsDisplayBodyWithHealth(lang, r, db, statusHint, statusClass, statusLabel, host, botToken, kitsuHealthy, discordHealthy)
		fmt.Fprint(w, adminPage(lang, tr(lang, "connections.title"), r, body))
		return
	}
	body := `<div class="section-card glass connections-card"><div class="page-heading connections-card-header"><div><p class="hint">` + esc(statusHint) + `</p></div><span class="status-pill ` + esc(statusClass) + `" role="status">` + esc(statusLabel) + `</span></div><div class="settings-block connections-section"><h2>` + esc(tr(lang, "connections.kitsu")) + `</h2><dl class="status-list"><dt>` + esc(tr(lang, "connections.host")) + `</dt><dd><code>` + esc(host) + `</code></dd><dt>` + esc(tr(lang, "connections.account")) + `</dt><dd>` + connectionStatusPill(connectionSecretStatus(storedRuntimeKitsuEmail(db), lang)) + `</dd></dl></div><div class="settings-block connections-section"><h2>` + esc(tr(lang, "connections.discord")) + `</h2><dl class="status-list"><dt>Discord Bot Token</dt><dd>` + connectionStatusPill(connectionSecretStatus(botToken, lang)) + `</dd></dl></div><div class="button-row connections-actions"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot?edit=1", r)) + `">` + esc(tr(lang, "connections.edit")) + `</a><a class="btn" href="` + esc(withLang("/bot/setup", r)) + `">` + esc(tr(lang, "ia.new_connection")) + `</a></div></div>`
	fmt.Fprint(w, adminPage(lang, tr(lang, "connections.title"), r, body))
}

func renderConnectionsDisplayBodyWithHealthRaw(lang string, r *http.Request, db *gorm.DB, statusHint, statusClass, statusLabel, host, botToken string, kitsuHealthy, discordHealthy bool) string {
	kitsuConfigured := strings.TrimSpace(StoredRuntimeKitsuToken(db)) != ""
	discordConfigured := strings.TrimSpace(botToken) != ""
	kitsuStatus := canonicalConnectionHealthStatus(lang, kitsuConfigured, kitsuHealthy)
	discordStatus := canonicalConnectionHealthStatus(lang, discordConfigured, discordHealthy)
	editURL := withLang("/bot/admin/bot?edit=1", r)
	kitsuTokenBox := `<div class="conn-robox is-unset">` + esc(t(lang, "未設定", "Not configured")) + `</div>`
	if kitsuConfigured {
		kitsuTokenBox = `<div class="conn-robox">` + esc(connectionSecretMask) + `</div>`
	}
	discordTokenBox := `<div class="conn-robox is-unset">` + esc(t(lang, "未設定", "Not configured")) + `</div>`
	if discordConfigured {
		discordTokenBox = `<div class="conn-robox">` + esc(connectionSecretMask) + `</div>`
	}
	linkURL := host
	apiBaseURL := ""
	if db != nil {
		if pub := strings.TrimSpace(PublicKitsuURL(db)); pub != "" {
			linkURL = pub
		}
		apiBaseURL = strings.TrimSpace(model.GetSetting(db, KitsuAPIBaseURLSettingKey))
	}
	if strings.TrimSpace(linkURL) == "" {
		linkURL = t(lang, "未設定", "Not configured")
	}
	endpointModeLabel := t(lang, "自動検出", "Automatic")
	apiBaseRow := ""
	if apiBaseURL != "" {
		apiBaseRow = `<label class="conn-field-label">` + esc(t(lang, "API Base URL", "API Base URL")) + `</label><div class="hostrow"><span class="hostval">` + esc(apiBaseURL) + `</span></div>`
	}
	discordNote := t(lang, "Botトークンがまだ設定されていません。", "Bot token is not configured yet.")
	if discordConfigured && discordHealthy {
		discordNote = t(lang, "Discord Botは接続済みです。", "Discord Bot is connected.")
	} else if discordConfigured {
		discordNote = t(lang, "Discord Botの接続を確認してください。", "Check the Discord Bot connection.")
	}
	return `<div class="connections-summary-grid">` +
		`<section class="section-card glass connections-card"><div class="page-heading connections-card-header"><h2>` + esc(tr(lang, "connections.kitsu")) + `</h2><span class="status-pill ` + esc(kitsuStatus.Class) + `" role="status">` + esc(kitsuStatus.Label) + `</span></div>` +
		`<label class="conn-field-label">` + esc(t(lang, "Kitsu Bot APIトークン", "Kitsu Bot API token")) + `</label>` + kitsuTokenBox +
		`<label class="conn-field-label">` + esc(t(lang, "外部 Kitsu URL（リンク用）", "External Kitsu URL (for links)")) + `</label>` +
		`<div class="hostrow"><span class="hostval">` + esc(linkURL) + `</span></div>` +
		`<p class="conn-note">` + esc(t(lang, "Discord通知のリンクに使います。", "Used for links in Discord notifications.")) + `</p>` +
		`<details class="connection-advanced-details"><summary><span>` + esc(t(lang, "詳細設定", "Advanced settings")) + `</span><span class="v">` + esc(t(lang, "接続先（API用）：", "API endpoint: ")) + esc(endpointModeLabel) + ` <span class="chev">▸</span></span></summary>` +
		`<div style="margin-top:10px;"><label class="conn-field-label">` + esc(t(lang, "Kitsu接続先（API用）", "Kitsu endpoint (for API)")) + `</label><div class="hostrow"><span class="hostval">` + esc(host) + `</span></div>` + apiBaseRow + `</div></details></section>` +
		`<section class="section-card glass connections-card"><div class="page-heading connections-card-header"><h2>` + esc(tr(lang, "connections.discord")) + `</h2><span class="status-pill ` + esc(discordStatus.Class) + `" role="status">` + esc(discordStatus.Label) + `</span></div>` +
		`<label class="conn-field-label">` + esc(t(lang, "Discord Botトークン", "Discord Bot Token")) + `</label>` + discordTokenBox +
		`<p class="conn-note">` + esc(discordNote) + `</p></section>` +
		`</div><div class="button-row connections-actions"><a class="btn" href="` + esc(editURL) + `">` + esc(t(lang, "接続設定を編集", "Edit connection settings")) + `</a><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(t(lang, "プロダクション一覧へ戻る", "Back to Productions")) + `</a></div>` +
		renderConnectionsSubpageCards(lang, r)
}

func renderConnectionsSubpageCards(lang string, r *http.Request) string {
	cards := []struct {
		title, desc, href, action string
	}{
		{
			title:  t(lang, "タスクステータス", "Task statuses"),
			desc:   t(lang, "Kitsuのステータスを「確認待ち」「リテイク」「完了」「未割り当て」に割り当てます。", "Assign Kitsu task statuses to Waiting, Retake, Done, or Unassigned."),
			href:   withLang("/bot/admin/bot/statuses", r),
			action: t(lang, "タスクステータスを開く", "Open task statuses"),
		},
		{
			title:  t(lang, "受け渡しの配線", "Handoff wiring"),
			desc:   t(lang, "タスク完了時に次工程へ通知する基本の配線を設定します。", "Configure default handoff wiring between task types."),
			href:   withLang("/bot/admin/bot/wiring", r),
			action: t(lang, "受け渡しの配線を開く", "Open handoff wiring"),
		},
	}
	var b strings.Builder
	b.WriteString(`<div class="section-stack" style="margin-top:24px;">`)
	for _, card := range cards {
		b.WriteString(`<section class="section-card glass"><div class="page-heading"><div><h2>` + esc(card.title) + `</h2><p class="hint">` + esc(card.desc) + `</p></div><div class="button-row"><a class="btn" href="` + esc(card.href) + `">` + esc(card.action) + `</a></div></div></section>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func renderConnectionsDisplayBodyWithHealth(lang string, r *http.Request, db *gorm.DB, statusHint, statusClass, statusLabel, host, botToken string, kitsuHealthy, discordHealthy bool) string {
	return renderConnectionsDisplayBodyWithHealthRaw(lang, r, db, statusHint, statusClass, statusLabel, host, botToken, kitsuHealthy, discordHealthy)
}

func renderConnectionsEditFormWithIdentityRows(lang string, r *http.Request, db *gorm.DB, statusHint, statusClass, statusLabel, runtimeHost string, kitsuHealthy, discordHealthy bool, discordBotName string) string {
	externalURL := ExternalKitsuURL(db)
	return renderConnectionsEditFormWithExternalURLState(lang, r, db, statusHint, statusClass, statusLabel, runtimeHost, kitsuHealthy, discordHealthy, discordBotName, externalURL, externalURL, "")
}

func renderConnectionsEditFormWithExternalURLState(lang string, r *http.Request, db *gorm.DB, statusHint, statusClass, statusLabel, runtimeHost string, kitsuHealthy, discordHealthy bool, discordBotName, externalURLDraft, externalURLInitialValue, externalURLSaveError string) string {
	resolvedHost := safeKitsuHostDisplay(runtimeHost)
	displayHost := resolvedHost
	if displayHost == "" {
		displayHost = t(lang, "未設定", "Not configured")
	}
	notice := ""
	if r.URL.Query().Get("msg") == "kitsu_bot_saved" {
		notice = `<div class="notice notice-success" role="status">` + esc(t(lang, "Kitsu接続を保存しました。", "Kitsu connection saved.")) + `</div>`
	} else if r.URL.Query().Get("msg") == "discord_saved" {
		notice = `<div class="notice notice-success" role="status">` + esc(t(lang, "Discord Bot接続を保存しました。", "Discord Bot connection saved.")) + `</div>`
	} else if r.URL.Query().Get("msg") == "external_kitsu_url_saved" {
		notice = `<div class="notice notice-success" role="status">` + esc(t(lang, "外部Kitsu URLを保存しました。", "External Kitsu URL saved.")) + `</div>`
	}
	kitsuConfigured := strings.TrimSpace(StoredRuntimeKitsuToken(db)) != ""
	discordConfigured := strings.TrimSpace(storedRuntimeDiscordBotToken(db)) != ""
	kitsuStatus := connectionHealthStatus(lang, kitsuConfigured, kitsuHealthy)
	discordStatus := connectionHealthStatus(lang, discordConfigured, discordHealthy)
	identity := strings.TrimSpace(discordBotName)
	if identity == "" {
		identity = t(lang, "未検出", "Not detected")
	}
	effectiveURL := PublicKitsuURL(db)
	checkLink := ""
	if effectiveURL != "" {
		checkLink = `<a class="btn-ghost" href="` + esc(effectiveURL) + `" target="_blank" rel="noopener noreferrer">` + esc(t(lang, "リンクを確認", "Check link")) + `</a>`
	}
	apiBaseURL := ""
	if db != nil {
		apiBaseURL = strings.TrimSpace(model.GetSetting(db, KitsuAPIBaseURLSettingKey))
	}
	externalURLSaveDisabled := ""
	if externalURLDraft == externalURLInitialValue {
		externalURLSaveDisabled = " disabled"
	}
	externalURLField := `<div class="connection-form-field"><label class="conn-field-label" for="kitsu-external-url">` + esc(t(lang, "外部 Kitsu URL（リンク用）", "External Kitsu URL (for links)")) + `</label><div class="connection-external-url-controls"><input id="kitsu-external-url" type="url" name="kitsu_external_url" form="external-kitsu-url-form" value="` + esc(externalURLDraft) + `" data-initial-value="` + esc(externalURLInitialValue) + `" aria-describedby="external-kitsu-url-help` + func() string {
		if externalURLSaveError != "" {
			return ` external-kitsu-url-error`
		}
		return ""
	}() + `">` + checkLink + `<button id="save-external-kitsu-url" type="submit" form="external-kitsu-url-form" class="btn sm"` + externalURLSaveDisabled + `>` + esc(t(lang, "保存", "Save")) + `</button></div><p id="external-kitsu-url-help" class="conn-note">` + esc(t(lang, "Discord通知のリンクに使います。", "Used for links in Discord notifications.")) + `</p>` + func() string {
		if externalURLSaveError == "" {
			return ""
		}
		return `<p id="external-kitsu-url-error" class="conn-note" style="color:var(--danger)" role="alert">` + esc(externalURLSaveError) + `</p>`
	}() + `</div>`
	manualMode := resolvedHost == ""
	detailsOpen := ""
	if r != nil && r.URL != nil {
		switch strings.TrimSpace(r.URL.Query().Get("endpoint_mode")) {
		case "manual":
			manualMode = true
			detailsOpen = " open"
		case "auto":
			manualMode = false
			detailsOpen = " open"
		}
	}
	autoSelectedClass := " on"
	manualSelectedClass := ""
	if manualMode {
		autoSelectedClass = ""
		manualSelectedClass = " on"
	}
	endpointPanel := `<div data-kitsu-endpoint-auto><input type="hidden" id="kitsu-hostname-auto" name="kitsu_hostname" form="kitsu-connection-form" value="` + esc(resolvedHost) + `" data-kitsu-host-auto><p class="conn-note"><code>` + esc(displayHost) + `</code></p></div>`
	if manualMode {
		endpointPanel = `<div class="connection-host-manual" data-kitsu-endpoint-manual>` +
			`<div class="connection-form-field"><label class="conn-field-label" for="kitsu-hostname">` + esc(tr(lang, "connections.host")) + `</label><input id="kitsu-hostname" type="url" name="kitsu_hostname" form="kitsu-connection-form" value="` + esc(resolvedHost) + `" data-initial-value="` + esc(resolvedHost) + `"></div>` +
			`<div class="connection-form-field" data-kitsu-internal-endpoint hidden><label class="conn-field-label" for="kitsu-internal-url">` + esc(t(lang, "内部 Kitsu URL", "Internal Kitsu URL")) + `</label><input id="kitsu-internal-url" type="url" name="kitsu_internal_url" form="kitsu-connection-form" autocomplete="url"></div>` +
			`<div class="connection-form-field"><label class="conn-field-label" for="kitsu-api-base-url">` + esc(t(lang, "API Base URL", "API Base URL")) + `</label><input id="kitsu-api-base-url" type="url" name="kitsu_api_base_url" form="kitsu-connection-form" value="` + esc(apiBaseURL) + `" autocomplete="url"></div>` +
			`</div>`
	}
	advancedDetails := `<details class="connection-advanced-details" id="connections-advanced"` + detailsOpen + `><summary><span>` + esc(t(lang, "詳細設定", "Advanced settings")) + `</span><span class="chev">▸</span></summary>` +
		`<div style="margin-top:12px;">` +
		`<label class="conn-field-label">` + esc(t(lang, "Kitsu接続先（API用）", "Kitsu endpoint (for API)")) + `</label>` +
		`<div class="seg" role="group" aria-label="` + esc(t(lang, "Kitsu接続先（API用）", "Kitsu endpoint (for API)")) + `">` +
		`<a class="` + strings.TrimSpace(autoSelectedClass) + `" href="` + esc(withLang("/bot/admin/bot?edit=1&endpoint_mode=auto", r)) + `" data-endpoint-mode="auto">` + esc(t(lang, "自動検出", "Automatic")) + `</a>` +
		`<a class="` + strings.TrimSpace(manualSelectedClass) + `" href="` + esc(withLang("/bot/admin/bot?edit=1&endpoint_mode=manual", r)) + `" data-endpoint-mode="manual">` + esc(t(lang, "手動設定", "Manual setup")) + `</a>` +
		`</div>` +
		endpointPanel +
		`</div></details>`
	kitsuTokenInput := `<input id="kitsu-bot-token" type="password" name="kitsu_bot_token" autocomplete="new-password" aria-describedby="kitsu-token-help" data-token-input="kitsu"` + func() string {
		if kitsuConfigured {
			return ` placeholder="` + esc(connectionSecretMask) + `"`
		}
		return ""
	}() + `>`
	discordTokenInput := `<input id="discord-bot-token" type="password" name="bot_token" autocomplete="new-password" aria-describedby="discord-token-help" data-token-input="discord"` + func() string {
		if discordConfigured {
			return ` placeholder="` + esc(connectionSecretMask) + `"`
		}
		return ""
	}() + `>`
	discordStatusNote := t(lang, "Botトークンがまだ設定されていません。", "Bot token is not configured yet.")
	if discordConfigured && discordHealthy {
		discordStatusNote = t(lang, "Discord Botは接続済みです。", "Discord Bot is connected.")
	} else if discordConfigured {
		discordStatusNote = t(lang, "Discord Botの接続を確認してください。", "Check the Discord Bot connection.")
	}
	pageCancel := `<div class="button-row connections-navigation"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(t(lang, "キャンセル", "Cancel")) + `</a></div>`
	body := `<div class="section-stack connections-edit-stack" id="connections-edit">` + pageCancel + notice +
		`<div class="connections-edit-grid">` +
		`<section class="section-card glass connections-card"><div class="page-heading connections-card-header"><h2>` + esc(tr(lang, "connections.kitsu")) + `</h2><span class="status-pill ` + esc(kitsuStatus.Class) + `" role="status">` + esc(kitsuStatus.Label) + `</span></div>` +
		`<form method="POST" class="connection-save-form" id="kitsu-connection-form"><input type="hidden" name="action" value="save_kitsu"><div class="connection-form-field"><label class="conn-field-label" for="kitsu-bot-token">` + esc(t(lang, "Kitsu Bot APIトークン", "Kitsu Bot API token")) + `</label>` + kitsuTokenInput + `</div>` + externalURLField + advancedDetails + `<div class="button-row connections-actions conn-card-actions"><button id="kitsu-recheck" type="submit" class="btn">` + esc(connectionSaveLabel(lang, "Kitsu", kitsuConfigured)) + `</button></div></form>` +
		`<form method="POST" id="external-kitsu-url-form" hidden><input type="hidden" name="action" value="save_external_kitsu_url"></form></section>` +
		`<section class="section-card glass connections-card"><div class="page-heading connections-card-header"><h2>` + esc(tr(lang, "connections.discord")) + `</h2><span class="status-pill ` + esc(discordStatus.Class) + `" role="status">` + esc(discordStatus.Label) + `</span></div>` +
		`<form method="POST" class="connection-save-form"><input type="hidden" name="action" value="save_discord"><div class="connection-form-field"><label class="conn-field-label" for="discord-bot-token">` + esc(t(lang, "Discord Botトークン", "Discord Bot Token")) + `</label>` + discordTokenInput + `</div><p class="conn-note" style="margin-top:10px;">` + esc(discordStatusNote) + `</p><div class="connection-field-row"><dt>` + esc(t(lang, "Bot", "Bot")) + `</dt><dd>` + esc(identity) + `</dd></div><div class="button-row connections-actions conn-card-actions"><button id="discord-recheck" type="submit" class="btn">` + esc(connectionSaveLabel(lang, "Discord Bot", discordConfigured)) + `</button></div></form></section>` +
		`</div><div class="button-row connections-navigation connections-footer"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/projects", r)) + `">` + esc(t(lang, "プロダクション一覧へ戻る", "Back to Productions")) + `</a></div><script>(function(){var saveText='` + esc(t(lang, "変更を保存", "Save changes")) + `';var host=document.getElementById('kitsu-hostname');var hostSubmit=document.getElementById('kitsu-recheck');if(host&&hostSubmit){host.addEventListener('input',function(){if(host.value!==host.getAttribute('data-initial-value'))hostSubmit.textContent=saveText;});}var externalURL=document.getElementById('kitsu-external-url'),externalURLSaveButton=document.getElementById('save-external-kitsu-url');if(externalURL&&externalURLSaveButton){function updateExternalURLSaveState(){externalURLSaveButton.disabled=externalURL.value===externalURL.getAttribute('data-initial-value');}externalURL.addEventListener('input',updateExternalURLSaveState);updateExternalURLSaveState();}}());</script></div>`
	return body
}

func renderConnectionsEditFormWithHealthRaw(lang string, r *http.Request, db *gorm.DB, statusHint, statusClass, statusLabel, runtimeHost string, kitsuHealthy, discordHealthy bool, discordBotName string) string {
	body := renderConnectionsEditFormWithIdentityRows(lang, r, db, statusHint, statusClass, statusLabel, runtimeHost, kitsuHealthy, discordHealthy, discordBotName)
	botLabel := esc(t(lang, "Bot", "Bot"))
	kitsuName := strings.TrimSpace(model.GetSetting(db, RuntimeKitsuBotNameSettingKey))
	if kitsuName == "" {
		kitsuName = t(lang, "未検出", "Not detected")
	}
	discordName := strings.TrimSpace(discordBotName)
	if discordName == "" {
		discordName = t(lang, "未検出", "Not detected")
	}
	for i := 0; i < 2; i++ {
		start := strings.Index(body, `<div class="connection-field-row"><dt>`+botLabel+`</dt><dd>`)
		if start < 0 {
			break
		}
		relEnd := strings.Index(body[start:], `</div>`)
		if relEnd < 0 {
			break
		}
		body = body[:start] + body[start+relEnd+len(`</div>`):]
	}
	return body
}

func renderConnectionsEditFormWithHealth(lang string, r *http.Request, db *gorm.DB, statusHint, statusClass, statusLabel, runtimeHost string, kitsuHealthy, discordHealthy bool, discordBotName string) string {
	return renderConnectionsEditFormWithHealthRaw(lang, r, db, statusHint, statusClass, statusLabel, runtimeHost, kitsuHealthy, discordHealthy, discordBotName)
}

func BotHandler(db *gorm.DB, kitsuReconnect func()) http.HandlerFunc {
	return BotHandlerWithRuntime(db, kitsuReconnect, nil)
}

// botMutationRequiresRecentAuthentication asks for a step-up only when the
// request actually submits a new credential. Viewing the page and ordinary
// edits (hostname, guild, display settings) never require re-authentication.
func botMutationRequiresRecentAuthentication(r *http.Request, action string) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	switch action {
	case "save_kitsu":
		return strings.TrimSpace(r.FormValue("kitsu_bot_token")) != ""
	case "save_discord":
		return strings.TrimSpace(r.FormValue("bot_token")) != ""
	default:
		if r.URL.Query().Get("legacy") != "1" {
			return false
		}
		return strings.TrimSpace(r.FormValue("kitsu_runtime_password")) != "" ||
			strings.TrimSpace(r.FormValue("bot_token")) != ""
	}
}

func recentBotAuthenticationLoginURL(r *http.Request, lang string) string {
	nextQuery := url.Values{"edit": {"1"}}
	if r != nil && r.URL.Query().Get("legacy") == "1" {
		nextQuery.Set("legacy", "1")
	}
	if lang != "" {
		nextQuery.Set("lang", lang)
	}
	next := "/bot/admin/bot?" + nextQuery.Encode()
	return appendLang("/bot/login?next="+url.QueryEscape(next), lang)
}

func BotHandlerWithRuntime(db *gorm.DB, kitsuReconnect func(), runtimeHealthy func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("BotHandler entered", "method", r.Method, "path", r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		path := strings.TrimRight(r.URL.Path, "/")
		if strings.HasSuffix(path, "/connections") {
			target := "/bot/admin/bot"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		isStatusesSubpage := strings.HasSuffix(path, "/statuses")
		isWiringSubpage := strings.HasSuffix(path, "/wiring")
		if r.Method == http.MethodGet && r.URL.Query().Get("legacy") != "1" {
			if isStatusesSubpage {
				notice := ""
				if r.URL.Query().Get("msg") == "saved" {
					notice = `<div class="notice notice-success" role="status">` + esc(t(lang, "タスクステータスを保存しました。", "Task statuses saved.")) + `</div>`
				}
				content := renderStatusRolesSection(db, lang, r)
				if content == "" {
					content = `<section class="section-card glass"><p class="hint">` + esc(t(lang, "利用可能なステータスがありません。", "No task statuses are available.")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(t(lang, "接続設定へ戻る", "Back to Connections")) + `</a></div></section>`
				}
				fmt.Fprint(w, adminPage(lang, t(lang, "タスクステータス", "Task statuses"), r, `<div class="section-stack">`+notice+content+`</div>`))
				return
			}
			if isWiringSubpage {
				notice := ""
				if r.URL.Query().Get("msg") == "saved" {
					notice = `<div class="notice notice-success" role="status">` + esc(t(lang, "受け渡しの配線を保存しました。", "Handoff wiring saved.")) + `</div>`
				}
				content := renderStudioRoutesSection(db, lang, r)
				if content == "" {
					content = `<section class="section-card glass"><p class="hint">` + esc(t(lang, "利用可能なタスクタイプがありません。", "No task types are available.")) + `</p><div class="button-row"><a class="btn-ghost" href="` + esc(withLang("/bot/admin/bot", r)) + `">` + esc(t(lang, "接続設定へ戻る", "Back to Connections")) + `</a></div></section>`
				}
				fmt.Fprint(w, adminPage(lang, t(lang, "受け渡しの配線", "Handoff wiring"), r, `<div class="section-stack">`+notice+content+`</div>`))
				return
			}
			if r.URL.Query().Get("edit") != "1" {
				renderConnectionsPageSafeWithRuntime(w, r, db, runtimeHealthy)
				return
			}
		}
		effectiveHost := KitsuHostForUI(db)
		editMode := r.URL.Query().Get("edit") == "1"
		var parseErr error
		if r.Method == http.MethodPost {
			parseErr = r.ParseForm()
		}
		action := strings.TrimSpace(r.FormValue("action"))
		actionClass := "empty"
		if action == "save_kitsu" {
			actionClass = "save_kitsu"
		} else if action != "" {
			actionClass = "other"
		}
		slog.Debug("BotHandler request parsed",
			"method", r.Method,
			"edit_query_present", editMode,
			"query_action_present", strings.TrimSpace(r.URL.Query().Get("action")) != "",
			"parse_error", parseErr != nil,
			"action_class", actionClass,
		)
		if botMutationRequiresRecentAuthentication(r, action) && !botEditAllowed(r) {
			http.Redirect(w, r, recentBotAuthenticationLoginURL(r, lang), http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			if action == "save_status_roles" {
				if err := saveStatusRoles(db, r); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, adminPage(lang, t(lang, "タスクステータス", "Task statuses"), r, `<div class="section-stack"><div class="notice notice-error" role="alert"><strong>`+esc(t(lang, "保存できませんでした", "Could not save"))+`</strong>`+forumSaveErrorMarkup(lang, err, nil)+`</div></div>`))
					return
				}
				http.Redirect(w, r, withLang("/bot/admin/bot/statuses", r)+"&msg=saved", http.StatusSeeOther)
				return
			}
			if action == "save_studio_routes" {
				if err := saveStudioRoutes(db, r); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, adminPage(lang, t(lang, "受け渡しの配線", "Handoff wiring"), r, `<div class="section-stack"><div class="notice notice-error" role="alert"><strong>`+esc(t(lang, "保存できませんでした", "Could not save"))+`</strong><p>`+esc(err.Error())+`</p></div></div>`))
					return
				}
				http.Redirect(w, r, withLang("/bot/admin/bot/wiring", r)+"&msg=saved", http.StatusSeeOther)
				return
			}
			if action == "save_external_kitsu_url" {
				draft, present := r.PostForm["kitsu_external_url"]
				if !present || len(draft) != 1 {
					renderExternalKitsuURLSaveError(w, r, db, http.StatusBadRequest, "", t(lang, "外部Kitsu URLを入力するか、空欄で保存してください。", "Enter an External Kitsu URL or save it empty."))
					return
				}
				externalURLDraft := draft[0]
				externalURL := strings.TrimSpace(externalURLDraft)
				if externalURL != "" {
					normalizedExternalURL, err := validateKitsuEndpoint(externalURL)
					if err != nil {
						renderExternalKitsuURLSaveError(w, r, db, http.StatusBadRequest, externalURLDraft, t(lang, "外部Kitsu URLを確認してください。", "Check the External Kitsu URL."))
						return
					}
					externalURL = strings.TrimRight(normalizedExternalURL, "/")
				}
				if err := persistExternalKitsuURL(db, externalURL); err != nil {
					slog.Warn("External Kitsu URL persistence failed", "error_class", "external_url_persistence_failed")
					renderExternalKitsuURLSaveError(w, r, db, http.StatusInternalServerError, externalURLDraft, t(lang, "外部Kitsu URLを保存できませんでした。入力内容を確認して再試行してください。", "The External Kitsu URL could not be saved. Check the value and try again."))
					return
				}
				http.Redirect(w, r, withLang("/bot/admin/bot?edit=1", r)+"&msg=external_kitsu_url_saved", http.StatusSeeOther)
				return
			}
			if action == "save_kitsu" {
				displayedHost := strings.TrimSpace(r.FormValue("kitsu_hostname"))
				slog.Debug("Kitsu save_kitsu handler reached",
					"handler_reached", true,
					"hostname_present", displayedHost != "",
					"email_present", strings.TrimSpace(r.FormValue("kitsu_runtime_email")) != "",
					"submitted_password_present", strings.TrimSpace(r.FormValue("kitsu_runtime_password")) != "",
				)
				hostForAuth, endpointErr := runtimeEndpointFromDisplay(db, displayedHost)
				if internalRaw := strings.TrimSpace(r.FormValue("kitsu_internal_url")); internalRaw != "" {
					if err := enforceKitsuHTTPScheme(internalRaw); err != nil {
						endpointErr = err
					} else if normalized, err := NormalizeKitsuURL(internalRaw, APISourceExplicit); err != nil {
						endpointErr = err
					} else {
						hostForAuth = normalized.DisplayBaseURL
					}
				}
				if endpointErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsuホストを入力してください。", "Enter the Kitsu host.")))
					return
				}
				if kitsuEndpointChangeBlocked(hostForAuth) {
					rejectPinnedKitsuEndpointChange(w, lang, "admin_save_kitsu")
					return
				}
				apiOverride := strings.TrimSpace(r.FormValue("kitsu_api_base_url"))
				if kitsuAPIOverrideBlocked(apiOverride) {
					rejectPinnedKitsuEndpointChange(w, lang, "admin_save_kitsu_api_override")
					return
				}
				connection, connectionErr := ResolveKitsuConnection(context.Background(), hostForAuth, apiOverride)
				if connectionErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu API URLを確認できませんでした。", "The Kitsu API URL could not be verified.")))
					return
				}
				if apiOverride != "" {
					apiOverride = connection.ResolvedAPIBaseURL
				}
				externalURLToSave := ""
				externalURLWasSubmitted := false
				publicRaw, externalPresent := r.PostForm["kitsu_external_url"]
				if !externalPresent {
					// Accept the previous field name for bookmarked/forms from older
					// installations, while storing the value under the new key.
					publicRaw, externalPresent = r.PostForm["kitsu_public_url"]
				}
				if externalPresent {
					externalURLWasSubmitted = true
					externalURL := ""
					if len(publicRaw) > 0 {
						externalURL = strings.TrimSpace(publicRaw[0])
					}
					if externalURL != "" {
						normalizedExternalURL, err := validateKitsuEndpoint(externalURL)
						if err != nil {
							w.WriteHeader(http.StatusBadRequest)
							fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "外部Kitsu URLを確認してください。", "Check the External Kitsu URL.")))
							return
						}
						externalURLToSave = normalizedExternalURL
					}
				}
				submittedBotToken := strings.TrimSpace(r.FormValue("kitsu_bot_token"))
				botToken := submittedBotToken
				if botToken == "" {
					botToken = StoredRuntimeKitsuToken(db)
				}
				if botToken != "" {
					validation := ValidateKitsuBotToken(db, strings.TrimSuffix(connection.ResolvedAPIBaseURL, "/api"), botToken, true)
					slog.Info("Kitsu Bot token validation result",
						"classification", validation.Classification,
						"stage", validation.Stage,
						"authenticated", validation.Authenticated,
						"compatible", validation.Compatible(),
						"failure_endpoint", validation.Failure.Endpoint,
						"failure_status", validation.Failure.StatusCode,
						"failure_error_class", validation.Failure.ErrorClass,
					)
					if !validation.Compatible() {
						w.WriteHeader(http.StatusBadRequest)
						fmt.Fprint(w, renderKitsuConnectionError(lang, botTokenValidationUserMessageSafe(lang, validation)))
						return
					}
					previousHost := effectiveRuntimeKitsuEndpoint(db)
					if err := persistKitsuConnection(db, submittedBotToken, validation, externalURLWasSubmitted, externalURLToSave, displayedHost, hostForAuth, apiOverride); err != nil {
						slog.Warn("Kitsu connection persistence failed", "error_class", "connection_persistence_failed")
						w.WriteHeader(http.StatusInternalServerError)
						fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu接続設定を安全に保存できませんでした。", "The Kitsu connection settings could not be saved safely.")))
						return
					}
					if apiOverride != "" {
						os.Setenv("KITSU_API_BASE_URL", apiOverride)
					} else {
						os.Unsetenv("KITSU_API_BASE_URL")
					}
					if _, pinned := operatorPinnedKitsuHost(); !pinned {
						SetAppManagedKitsuHostname(hostForAuth)
						logKitsuEndpointChange("admin_save_kitsu", previousHost, hostForAuth)
					}
					if kitsuReconnect != nil {
						go kitsuReconnect()
					}
					http.Redirect(w, r, withLang("/bot/admin/bot", r)+"&msg=kitsu_bot_saved", http.StatusSeeOther)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, renderKitsuConnectionError(lang, r, t(lang, "Kitsu Bot API Tokenを入力してください。", "Enter a Kitsu Bot API token.")))
				return
			}
			if action == "save_discord" {
				token := strings.TrimSpace(r.FormValue("bot_token"))
				if token == "" {
					token = storedRuntimeDiscordBotToken(db)
				}
				slog.Info("Discord save token received",
					"token_present", token != "",
				)
				if token == "" {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Discord Bot Tokenを入力してください。", "Enter the Discord Bot Token.")))
					return
				}
				if err := validateDiscordBotTokenForSave(token); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Discord Botに接続できませんでした。Tokenと接続状態を確認してください。", "Discord Bot connection failed. Check the token and connection status.")))
					return
				}
				if err := setRuntimeDiscordBotToken(db, token); err != nil {
					slog.Warn("Discord save token persistence failed", "error_class", "credential_persistence_failed")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Discord Bot Tokenを安全に保存できませんでした。", "The Discord Bot Token could not be stored securely.")))
					return
				}
				storedToken := storedRuntimeDiscordBotToken(db)
				slog.Info("Discord save token persisted",
					"stored_token_present", storedToken != "",
					"fingerprint_match", storedToken == token,
				)
				http.Redirect(w, r, withLang("/bot/admin/bot", r)+"&msg=discord_saved", http.StatusSeeOther)
				return
			}
			if r.URL.Query().Get("legacy") != "1" {
				slog.Warn("BotHandler rejected POST", "stage", "unrecognized_action", "status", http.StatusBadRequest)
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "保存する接続を選択してください。", "Choose which connection to save.")))
				return
			}
			kitsuChanged := false
			runtimeEmail := strings.TrimSpace(r.FormValue("kitsu_runtime_email"))
			runtimePassword := r.FormValue("kitsu_runtime_password")
			submittedHosts, hasSubmittedHost := r.PostForm["kitsu_hostname"]
			displayedHost := ""
			if len(submittedHosts) > 0 {
				displayedHost = strings.TrimSpace(submittedHosts[0])
			}
			if runtimeEmail != "" || strings.TrimSpace(runtimePassword) != "" {
				if runtimeEmail == "" || strings.TrimSpace(runtimePassword) == "" {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu実行用のメールアドレスとパスワードを入力してください。", "Enter both the Kitsu runtime email and password.")))
					return
				}
				if !hasSubmittedHost || displayedHost == "" {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu接続先を確認できませんでした。", "The Kitsu connection endpoint is missing.")))
					return
				}
				hostForAuth, endpointErr := runtimeEndpointFromDisplay(db, displayedHost)
				if endpointErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu接続先を確認できませんでした。", "The Kitsu connection endpoint is invalid.")))
					return
				}
				if kitsuEndpointChangeBlocked(hostForAuth) {
					rejectPinnedKitsuEndpointChange(w, lang, "admin_legacy_save")
					return
				}
				if !allowSetupProbe(r) {
					w.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "試行回数が多すぎます。1分ほど待ってから再試行してください。", "Too many attempts. Wait about a minute and try again.")))
					return
				}
				if err := RecoverRuntimeCredentials(db, hostForAuth, runtimeEmail, runtimePassword); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu接続を確認できませんでした。入力内容とKitsuの接続先を確認してください。", "Kitsu authentication could not be verified. Check the credentials and Kitsu endpoint.")+" ("+err.Error()+")"))
					return
				}
				if _, pinned := operatorPinnedKitsuHost(); !pinned && model.GetSetting(db, "kitsu.hostname") != hostForAuth {
					logKitsuEndpointChange("admin_legacy_save", model.GetSetting(db, "kitsu.hostname"), hostForAuth)
					model.SetSetting(db, "kitsu.hostname", hostForAuth)
				}
				kitsuChanged = true
			}
			if _, pinned := operatorPinnedKitsuHost(); !pinned {
				if runtimeHost := effectiveRuntimeKitsuEndpoint(db); runtimeHost != "" {
					SetAppManagedKitsuHostname(runtimeHost)
				}
			}
			if value := strings.TrimSpace(r.FormValue("bot_token")); value != "" {
				if err := setRuntimeDiscordBotToken(db, value); err != nil {
					slog.Warn("legacy Discord save token persistence failed", "error_class", "credential_persistence_failed")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, adminPage(lang, t(lang, "保存に失敗しました", "Save failed"), r, `<div class="section-card glass"><p>`+t(lang, "Discord Bot Tokenを安全に保存できませんでした。", "The Discord Bot Token could not be stored securely.")+`</p></div>`))
					return
				}
			}
			if value := strings.TrimSpace(r.FormValue("kitsu_runtime_email")); value != "" {
				setRuntimeKitsuEmail(db, value)
				kitsuChanged = true
			}
			if value := strings.TrimSpace(r.FormValue("kitsu_runtime_password")); value != "" {
				if err := setRuntimeKitsuPassword(db, value); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, adminPage(lang, t(lang, "保存に失敗しました", "Save failed"), r, `<div class="section-card glass"><p>`+t(lang, "Runtime credential を安全に保存できませんでした。", "Could not safely store the runtime credential.")+`</p></div>`))
					return
				}
				kitsuChanged = true
			}
			if kitsuChanged && kitsuReconnect != nil {
				go kitsuReconnect()
			}
			http.Redirect(w, r, withLang("/bot/admin/bot", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		sharedReadiness := sharedBotRuntimeReadiness(db, effectiveHost, storedRuntimeDiscordBotToken(db))
		kitsuHealthy := false
		if runtimeHealthy != nil {
			kitsuHealthy = runtimeHealthy()
		} else {
			kitsuHealthy = checkKitsuStatus(normalizeKitsuHostname(effectiveHost)).Authenticated
		}
		guildID := strings.TrimSpace(model.GetSetting(db, "discord.guildID"))
		discordStatus := checkDiscordStatus(storedRuntimeDiscordBotToken(db), guildID)
		discordHealthy := discordStatus.BotValid && (guildID == "" || discordStatus.GuildValid)
		kitsuConfigured := strings.TrimSpace(StoredRuntimeKitsuToken(db)) != ""
		discordConfigured := strings.TrimSpace(storedRuntimeDiscordBotToken(db)) != ""
		pageStatus := canonicalConnectionHealthStatus(lang, kitsuConfigured && discordConfigured, kitsuHealthy && discordHealthy)
		statusClass, statusLabel := pageStatus.Class, pageStatus.Label
		primaryAction := tr(lang, "bot_runtime.complete_setup")
		if discordConfigured {
			primaryAction = tr(lang, "bot_runtime.reauthenticate")
		}
		statusHint := t(lang, "Kitsu接続とDiscord Bot接続をそれぞれ確認してください。", "Review Kitsu and Discord Bot connections separately.")
		if !sharedReadiness.KitsuConfigured {
			statusHint = t(lang, "Kitsu接続を設定してください。", "Complete the Kitsu connection first.")
		} else if !sharedReadiness.DiscordConfigured {
			statusHint = t(lang, "Discord Bot接続を設定してください。", "Complete the Discord Bot connection.")
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
			t(lang, "共有Bot / Runtime 設定", "Shared Bot / Runtime"), t(lang, "新規連携セットアップで使う共有 Bot / Runtime の設定を確認・更新できます。", "Review and update the shared Bot / Runtime settings used by New Connection Setup."), statusClass, statusLabel, esc(effectiveHost), t(lang, "Bot Token", "Bot Token"), secretStatus(storedRuntimeDiscordBotToken(db), lang), withLang("/bot/admin/bot?edit=1", r), primaryAction, withLang("/bot/setup", r), t(lang, "新規連携セットアップへ戻る", "Back to New Connection Setup"), withLang("/bot/admin/projects", r), t(lang, "連携済みプロダクション管理を開く", "Open Connected Productions"))
		view = strings.Replace(view, "<div class=\"metric-label\">Kitsu hostname</div>", "<div class=\"metric-label\">"+esc(tr(lang, "bot_runtime.kitsu_hostname"))+"</div>", 1)
		view = strings.Replace(view, ">Bot Token<", ">"+esc(tr(lang, "bot_runtime.bot_token"))+"<", 1)
		if !editMode {
			fmt.Fprint(w, adminPage(lang, t(lang, "共有Bot / Runtime 設定", "Shared Bot / Runtime"), r, view))
			return
		}
		form := renderConnectionsEditFormWithHealth(lang, r, db, statusHint, statusClass, statusLabel, effectiveHost, kitsuHealthy, discordHealthy, discordStatus.BotName)
		fmt.Fprint(w, adminPage(lang, tr(lang, "connections.title"), r, form))
		return
	}
}

func AuditLogHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.URL.Query().Get("legacy") != "1" {
			renderIAAudit(w, r, db)
			return
		}
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

type assignmentUserOption struct {
	RowID      uint
	KitsuName  string
	KitsuEmail string
	DiscordID  string
}

type kitsuConnectionPersistence struct {
	storeToken    func(*gorm.DB, string) error
	storeMetadata func(*gorm.DB, BotTokenValidationResult) error
	set           func(*gorm.DB, string, string) error
	delete        func(*gorm.DB, string) error
}

type externalKitsuURLPersistence struct {
	set    func(*gorm.DB, string, string) error
	delete func(*gorm.DB, string) error
}

func persistExternalKitsuURL(db *gorm.DB, externalURL string) error {
	return persistExternalKitsuURLWith(db, externalKitsuURLPersistence{model.SetSettingWithError, model.DeleteSettingWithError}, externalURL)
}

func persistExternalKitsuURLWith(db *gorm.DB, persistence externalKitsuURLPersistence, externalURL string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if externalURL == "" {
			if err := persistence.delete(tx, ExternalKitsuURLSettingKey); err != nil {
				return err
			}
			return persistence.delete(tx, PublicKitsuURLSettingKey)
		}
		if err := persistence.set(tx, ExternalKitsuURLSettingKey, externalURL); err != nil {
			return err
		}
		return persistence.delete(tx, PublicKitsuURLSettingKey)
	})
}

func renderExternalKitsuURLSaveError(w http.ResponseWriter, r *http.Request, db *gorm.DB, status int, draft, message string) {
	lang := currentLang(r)
	host := KitsuHostForUI(db)
	readiness := sharedBotRuntimeReadiness(db, normalizeKitsuHostname(host), storedRuntimeDiscordBotToken(db))
	kitsuStatus := connectionHealthStatus(lang, readiness.KitsuConfigured, false)
	discordStatus := connectionHealthStatus(lang, readiness.DiscordConfigured, false)
	pageStatus := canonicalConnectionPageStatus(lang, readiness, false, false)
	statusHint := t(lang, "必要な接続を設定してください。", "Configure the required connections.")
	if readiness.KitsuConfigured && readiness.DiscordConfigured {
		statusHint = t(lang, "Kitsu接続とDiscord Bot接続をそれぞれ確認してください。", "Review Kitsu and Discord Bot connections separately.")
	}
	externalURLInitialValue := ExternalKitsuURL(db)
	form := renderConnectionsEditFormWithExternalURLState(lang, r, db, statusHint, pageStatus.Class, pageStatus.Label, host, kitsuStatus.Class == "ok", discordStatus.Class == "ok", "", draft, externalURLInitialValue, message)
	w.WriteHeader(status)
	fmt.Fprint(w, adminPage(lang, tr(lang, "connections.title"), r, form))
}

func defaultKitsuConnectionPersistence() kitsuConnectionPersistence {
	return kitsuConnectionPersistence{setRuntimeKitsuToken, StoreValidatedKitsuBotMetadata, model.SetSettingWithError, model.DeleteSettingWithError}
}

func persistKitsuConnection(db *gorm.DB, submittedToken string, validation BotTokenValidationResult, externalSubmitted bool, externalURL, displayHost, runtimeHost, apiOverride string) error {
	return persistKitsuConnectionWith(db, defaultKitsuConnectionPersistence(), submittedToken, validation, externalSubmitted, externalURL, displayHost, runtimeHost, apiOverride)
}

func persistKitsuConnectionWith(db *gorm.DB, persistence kitsuConnectionPersistence, submittedToken string, validation BotTokenValidationResult, externalSubmitted bool, externalURL, displayHost, runtimeHost, apiOverride string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if submittedToken != "" {
			if err := persistence.storeToken(tx, submittedToken); err != nil {
				return err
			}
		}
		if err := persistence.storeMetadata(tx, validation); err != nil {
			return err
		}
		if externalSubmitted {
			if externalURL == "" {
				if err := persistence.delete(tx, ExternalKitsuURLSettingKey); err != nil {
					return err
				}
				if err := persistence.delete(tx, PublicKitsuURLSettingKey); err != nil {
					return err
				}
			} else if err := persistence.set(tx, ExternalKitsuURLSettingKey, externalURL); err != nil {
				return err
			}
		}
		for _, setting := range []struct{ key, value string }{
			{KitsuDisplayURLSettingKey, displayHost},
			{"kitsu.hostname", runtimeHost},
		} {
			if err := persistence.set(tx, setting.key, setting.value); err != nil {
				return err
			}
		}
		if apiOverride == "" {
			return persistence.delete(tx, KitsuAPIBaseURLSettingKey)
		}
		return persistence.set(tx, KitsuAPIBaseURLSettingKey, apiOverride)
	})
}

func botAccountEmail(db *gorm.DB) string {
	return strings.ToLower(storedRuntimeKitsuEmail(db))
}

func filterAssignablePersons(persons []KitsuPerson, botEmail string) []KitsuPerson {
	filtered := make([]KitsuPerson, 0, len(persons))
	for _, person := range persons {
		if !person.Active || person.Archived || person.IsBot {
			continue
		}
		if shouldExcludeBotPerson(person, botEmail) {
			continue
		}
		filtered = append(filtered, person)
	}
	return filtered
}

func shouldExcludeBotPerson(person KitsuPerson, botEmail string) bool {
	if person.IsBot {
		return true
	}
	return strings.TrimSpace(botEmail) != "" && strings.EqualFold(strings.TrimSpace(person.Email), strings.TrimSpace(botEmail))
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

func configuredAssignmentProject(db *gorm.DB, selectedProjectID string) *model.Project {
	projects := model.ListProjects(db)
	if len(projects) == 0 {
		return nil
	}
	selectedProjectID = strings.TrimSpace(selectedProjectID)
	if selectedProjectID != "" {
		for _, project := range projects {
			if project.KitsuProjectID == selectedProjectID {
				copyProject := project
				return &copyProject
			}
		}
	}
	return &projects[0]
}

type unifiedAssignmentRow struct {
	RowID      uint
	KitsuName  string
	KitsuEmail string
	DiscordID  string
}

func assignmentIdentityKey(name, email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" {
		return "email:" + email
	}
	return "name:" + strings.ToLower(strings.TrimSpace(name))
}

func buildAssignmentPersonOptions(users []assignmentUserOption, selectedName, selectedEmail, lang string) string {
	var out strings.Builder
	out.WriteString(`<option value="">` + t(lang, "選択してください", "Select user") + `</option>`)
	for _, user := range users {
		isSelected := (selectedEmail != "" && user.KitsuEmail == selectedEmail) || (selectedEmail == "" && selectedName != "" && user.KitsuName == selectedName)
		out.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s" data-email="%s" %s>%s</option>`, esc(user.KitsuEmail), esc(user.KitsuName), esc(user.KitsuEmail), selectedAttr(isSelected), esc(user.KitsuName)))
	}
	return out.String()
}

func kitsuAssignmentOptions(persons []KitsuPerson) []assignmentUserOption {
	options := make([]assignmentUserOption, 0, len(persons))
	for _, person := range persons {
		options = append(options, assignmentUserOption{
			KitsuName:  strings.TrimSpace(person.FullName),
			KitsuEmail: strings.TrimSpace(person.Email),
		})
	}
	return options
}

func buildUnifiedAssignments(projectUsers []model.ProjectUserMap, legacyUsers []model.UserMap, useProjectScoped bool) []unifiedAssignmentRow {
	rows := map[string]unifiedAssignmentRow{}
	addBase := func(rowID uint, name, email, discordID string) {
		key := assignmentIdentityKey(name, email)
		current := rows[key]
		current.RowID = rowID
		current.KitsuName = strings.TrimSpace(name)
		current.KitsuEmail = strings.TrimSpace(email)
		current.DiscordID = strings.TrimSpace(discordID)
		rows[key] = current
	}
	if useProjectScoped {
		for _, row := range projectUsers {
			addBase(row.ID, row.KitsuName, row.KitsuEmail, row.DiscordUserID)
		}
	} else {
		for _, row := range legacyUsers {
			addBase(row.ID, row.KitsuName, row.KitsuEmail, row.DiscordID)
		}
	}
	out := make([]unifiedAssignmentRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].KitsuName) < strings.ToLower(out[j].KitsuName)
	})
	return out
}

func syncProjectCheckerAssignmentUserIdentity(db *gorm.DB, projectRowID uint, previousName, previousEmail, name, email, discordID string) {
	previousIdentity := assignmentIdentityKey(previousName, previousEmail)
	for _, row := range model.ListProjectCheckerMaps(db, projectRowID) {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) != previousIdentity {
			continue
		}
		model.UpdateProjectCheckerMapWithUser(db, row.ID, row.TaskType, name, email, discordID, row.OverrideDiscordID)
	}
}

func syncLegacyCheckerAssignmentUserIdentity(db *gorm.DB, previousName, previousEmail, name, email string) {
	previousIdentity := assignmentIdentityKey(previousName, previousEmail)
	for _, row := range model.ListCheckerMap(db) {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) != previousIdentity {
			continue
		}
		model.UpdateCheckerMapWithOverride(db, row.ID, row.TaskType, name, email, row.OverrideDiscordID)
	}
}

func deleteProjectCheckerAssignmentsForUser(db *gorm.DB, projectRowID uint, name, email string) {
	for _, row := range model.ListProjectCheckerMaps(db, projectRowID) {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) == assignmentIdentityKey(name, email) {
			model.DeleteProjectCheckerMapByID(db, row.ID)
		}
	}
}

func deleteLegacyCheckerAssignmentsForUser(db *gorm.DB, name, email string) {
	for _, row := range model.ListCheckerMap(db) {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) == assignmentIdentityKey(name, email) {
			model.DeleteCheckerEntryByID(db, row.ID)
		}
	}
}

func parseUint(value string) uint {
	n, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 0)
	return uint(n)
}

func secretStatus(value, lang string) string {
	if strings.TrimSpace(value) == "" {
		return `<span class="status-pill bad">` + t(lang, "未設定", "Not configured") + `</span>`
	}
	return `<span class="status-pill ok">` + t(lang, "設定済み / 非表示", "Configured / hidden") + `</span>`
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
		message = `<div class="toast glass" role="status" aria-live="polite">` + t(lang, "保存しました。", "Saved.") + `</div>`
	}
	nav := `<div class="nav-card">` + iaNav(lang, r) + `</div>`
	pageHeading := ""
	if strings.TrimSpace(title) != "" {
		pageHeading = `<div class="page-heading"><div><h1>` + esc(title) + `</h1></div></div>`
	}
	content := `<div class="page-card glass editorial-workbench">` + pageHeading +
		message + body + `</div>` +
		`<div id="deleteModal" class="delete-modal" role="dialog" aria-modal="true" aria-labelledby="deleteModalTitle"><div class="delete-box glass"><h2 id="deleteModalTitle" class="delete-title">` + esc(t(lang, "操作の確認", "Confirm action")) + `</h2><p id="deleteModalText" class="delete-text"></p><p id="deleteModalHelper" class="field-help hidden"></p><div id="deleteModalInputWrap" class="delete-input hidden"><label for="deleteModalInput"><span class="sr-only">` + esc(t(lang, "確認ワード", "Confirmation phrase")) + `</span><input id="deleteModalInput" type="text" autocomplete="off" autocapitalize="off" spellcheck="false"></label><div class="field-help">` + esc(t(lang, "確認ワード", "Confirmation phrase")) + `: <code id="deleteModalExpected"></code></div></div><div class="button-row"><button id="deleteCancelBtn" type="button" class="btn-ghost">` + esc(t(lang, "キャンセル", "Cancel")) + `</button><button id="deleteConfirmBtn" type="button" class="btn-danger">` + esc(t(lang, "実行する", "Continue")) + `</button></div></div></div>` +
		baseAdminJS(lang)
	return appShell("KitsuSync", "", lang, r, nav, content)
}
