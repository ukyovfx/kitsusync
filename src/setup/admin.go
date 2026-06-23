package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"encoding/json"
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
				esc(t(lang, "\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u540d", "Production")),
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

		storedHost := model.GetSetting(db, "kitsu.hostname")
		autoHost := publicKitsuHostnameFromRequest(r, storedHost)
		effectiveHost := autoHost
		if storedHost != "" {
			effectiveHost = normalizeKitsuHostname(storedHost)
		}
		botTokenConfigured := strings.TrimSpace(storedRuntimeDiscordBotToken(db)) != ""
		runtimeConfigured := effectiveHost != "" && strings.TrimSpace(storedRuntimeKitsuEmail(db)) != ""
		botStatus := tileStatus(
			statusChip(map[bool]string{true: "ok", false: "bad"}[botTokenConfigured], t(lang, map[bool]string{true: "Bot設定済み", false: "Bot未設定"}[botTokenConfigured], map[bool]string{true: "Bot set", false: "Bot unset"}[botTokenConfigured])),
			statusChip(map[bool]string{true: "ok", false: "bad"}[runtimeConfigured], t(lang, map[bool]string{true: "Runtime設定済み", false: "Runtime未設定"}[runtimeConfigured], map[bool]string{true: "Runtime set", false: "Runtime unset"}[runtimeConfigured])),
		)

		storageConfiguredCount := 0
		for _, project := range projects {
			if strings.TrimSpace(project.StorageURL) != "" {
				storageConfiguredCount++
			}
		}
		storageStatus := tileStatus(
			statusChip(map[bool]string{true: "ok", false: "bad"}[storageConfiguredCount > 0], fmt.Sprintf(t(lang, "保存先設定: %d", "Storage set: %d"), storageConfiguredCount)),
		)
		if projectCount > storageConfiguredCount {
			storageStatus = tileStatus(
				statusChip(map[bool]string{true: "ok", false: "bad"}[storageConfiguredCount > 0], fmt.Sprintf(t(lang, "保存先設定: %d", "Storage set: %d"), storageConfiguredCount)),
				statusChip("warn", fmt.Sprintf(t(lang, "未設定: %d", "Unset: %d"), projectCount-storageConfiguredCount)),
			)
		}

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
			{"\U0001F4C1", "/bot/admin/drive", "\u30b9\u30c8\u30ec\u30fc\u30b8", "Storage", storageStatus},
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

		if handleProjectRoutingMutation(w, r, lang, fallbackGuildID, botToken, db) {
			return
		}

		if r.Method == http.MethodPost {
			action := strings.TrimSpace(r.FormValue("action"))
			projectID := strings.TrimSpace(r.FormValue("project_id"))
			redirectURL := withLang("/bot/admin/projects", r)
			if projectID != "" {
				redirectURL += "&project=" + url.QueryEscape(projectID)
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
					http.Redirect(w, r, redirectURL+"&danger_preview=1&validated_channels=1", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, withLang("/bot/admin/projects", r)+"&msg=error", http.StatusSeeOther)
				return
			}
			if action == "execute_validated_channel_delete" {
				expected := t(lang, "削除", "delete")
				if projectID == "" || strings.TrimSpace(r.FormValue("confirm_text")) != expected {
					http.Redirect(w, r, redirectURL+"&danger_preview=1&validated_channels=1&msg=error", http.StatusSeeOther)
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
				execResult := executeConnectedProductionValidatedChannelDelete(lang, *project, effectiveGuildID, botToken, db)
				fmt.Fprint(w, renderConnectedProductionChannelDeleteResultPage(lang, r, *project, execResult))
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

		allTaskTypes := kitsu.GetTaskTypes().Each
		allWebhooks := model.ListAllProjectWebhooks(db)
		selectedProjectID := strings.TrimSpace(r.URL.Query().Get("project"))
		dangerPreviewProjectID := ""
		if strings.TrimSpace(r.URL.Query().Get("danger_preview")) != "" {
			dangerPreviewProjectID = selectedProjectID
		}
		validatedChannelProjectID := ""
		if strings.TrimSpace(r.URL.Query().Get("validated_channels")) != "" {
			validatedChannelProjectID = selectedProjectID
		}
		var blocks strings.Builder
		for _, p := range model.ListProjects(db) {
			effectiveGuildID := strings.TrimSpace(p.DiscordGuildID)
			if effectiveGuildID == "" {
				effectiveGuildID = fallbackGuildID
			}
			webhooks := model.ListProjectWebhooks(db, p.KitsuProjectID)
			assignedCount := 0
			assignedTaskTypes := map[string]bool{}
			channelNames := map[string]bool{}
			for _, wh := range webhooks {
				if wh.TaskType != "" {
					assignedTaskTypes[wh.TaskType] = true
					assignedCount++
				}
				if strings.TrimSpace(wh.ChannelName) != "" {
					channelNames[wh.ChannelName] = true
				}
			}
			unassignedCount := 0
			for _, tt := range allTaskTypes {
				if !assignedTaskTypes[tt.Name] {
					unassignedCount++
				}
			}
			channelCount := len(channelNames)
			statusClass := "bad"
			statusLabel := t(lang, "要確認", "Needs review")
			switch {
			case effectiveGuildID != "" && assignedCount > 0 && unassignedCount == 0:
				statusClass = "ok"
				statusLabel = t(lang, "接続済み", "Connected")
			case effectiveGuildID != "" && (assignedCount > 0 || channelCount > 0):
				statusClass = "warn"
				statusLabel = t(lang, "確認中", "Review")
			}
			categoryID := strings.TrimSpace(p.DiscordCategoryID)
			if categoryID == "" {
				categoryID = "—"
			}
			projectLang := strings.TrimSpace(p.Language)
			if projectLang == "" {
				projectLang = "ja"
			}
			previewCategoryID := categoryID
			previewChannelOrder := make([]string, 0)
			previewChannelNames := map[string]map[string]bool{}
			for _, wh := range webhooks {
				channelID := strings.TrimSpace(wh.DiscordChannelID)
				if channelID == "" {
					continue
				}
				if _, ok := previewChannelNames[channelID]; !ok {
					previewChannelOrder = append(previewChannelOrder, channelID)
					previewChannelNames[channelID] = map[string]bool{}
				}
				channelName := strings.TrimSpace(wh.ChannelName)
				if channelName != "" {
					previewChannelNames[channelID][channelName] = true
				}
			}
			sort.Strings(previewChannelOrder)
			previewCount := len(previewChannelOrder)
			var previewChannelsHTML strings.Builder
			if previewCount == 0 {
				previewChannelsHTML.WriteString(`<p class="field-help" style="margin:0">` + esc(t(lang, "保存済みの DiscordChannelID は見つかりませんでした。現時点では KitsuSync 側に記録された対象 channel はありません。", "No stored DiscordChannelID values were found. There are currently no recorded Discord-side channel targets in KitsuSync.")) + `</p>`)
			} else {
				previewChannelsHTML.WriteString(`<ul class="list-tight" style="margin:0;padding-left:18px">`)
				for _, channelID := range previewChannelOrder {
					names := make([]string, 0, len(previewChannelNames[channelID]))
					for name := range previewChannelNames[channelID] {
						names = append(names, name)
					}
					sort.Strings(names)
					label := `<code>` + esc(channelID) + `</code>`
					if len(names) > 0 {
						label += ` <span class="hint">(` + esc(strings.Join(names, ", ")) + `)</span>`
					}
					previewChannelsHTML.WriteString(`<li>` + label + `</li>`)
				}
				previewChannelsHTML.WriteString(`</ul>`)
			}
			var validatedHTML string
			if validatedChannelProjectID != "" && validatedChannelProjectID == p.KitsuProjectID {
				candidates := buildConnectedProductionChannelCandidates(webhooks)
				deletableCandidates, skippedCandidates, validationSummary, validationChecks := validateConnectedProductionChannelCandidates(lang, p, effectiveGuildID, botToken, allWebhooks, candidates)
				validatedHTML = renderConnectedProductionChannelValidationCard(lang, r, p, validationSummary, validationChecks, deletableCandidates, skippedCandidates)
			}
			dangerPreviewHTML := ""
			if dangerPreviewProjectID != "" && dangerPreviewProjectID == p.KitsuProjectID {
				dangerPreviewHTML = fmt.Sprintf(`
    <div class="section-card glass" style="border-color:#ffb08f">
      <div class="page-heading" style="margin-bottom:14px">
        <div>
          <h3 style="margin:0">%s</h3>
          <p class="hint" style="margin:6px 0 0">%s</p>
        </div>
        <span class="status-pill warn">%s</span>
      </div>
      <div class="metric-grid">
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host">%s</div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host"><code>%s</code></div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host"><code>%s</code></div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
      </div>
      <p class="field-help" style="margin:12px 0 0">%s</p>
      <div class="section-stack" style="margin-top:12px">
        <div>
          <div class="eyebrow">%s</div>
          %s
        </div>
      </div>
      <div class="button-row" style="margin-top:14px">
        <form method="POST" style="margin:0">
          <input type="hidden" name="action" value="validate_remove_connection_channels">
          <input type="hidden" name="project_id" value="%s">
          <button type="submit" class="btn">%s</button>
        </form>
        <a class="btn-ghost" href="%s">%s</a>
      </div>
    </div>`,
					esc(t(lang, "Discord 削除候補のプレビュー", "Preview delete scope only")),
					esc(t(lang, "これは確認用の dry-run です。この画面からは Discord channel / category はまだ削除されません。保存済みの接続情報から、将来の削除候補として見えている範囲だけを表示します。", "This preview is dry-run only. It does not delete Discord channels or category. It only shows the delete scope visible from saved connection data.")),
					esc(t(lang, "PREVIEW ONLY", "PREVIEW ONLY")),
					esc(t(lang, "Production", "Production")),
					esc(p.Name),
					esc(t(lang, "Project ID", "Project ID")),
					esc(p.KitsuProjectID),
					esc(t(lang, "Recorded Discord Category ID", "Recorded Discord Category ID")),
					esc(previewCategoryID),
					esc(t(lang, "Referenced channel IDs", "Referenced channel IDs")),
					previewCount,
					esc(t(lang, "現在の保存データだけでは ownership は確定できません。特に category 配下の channel 全体や、手動変更済み channel の安全削除はまだ判断していません。", "Current saved data does not prove ownership. In particular, this preview does not yet decide whether category-wide or manually edited channels would be safe to delete.")),
					esc(t(lang, "Stored channel references", "Stored channel references")),
					previewChannelsHTML.String(),
					esc(p.KitsuProjectID),
					esc(t(lang, "channel 候補を検証する", "Validate channel-only delete candidates")),
					esc(withLang("/bot/admin/projects?project="+url.QueryEscape(p.KitsuProjectID), r)),
					esc(t(lang, "プレビューを閉じる", "Close preview")),
				)
			}
			openAttr := ""
			if selectedProjectID != "" && p.KitsuProjectID == selectedProjectID {
				openAttr = " open"
			}
			blocks.WriteString(fmt.Sprintf(`
<details class="accordion"%s>
  <summary>
    <div class="accordion-summary-main">
      <div class="eyebrow">%s</div>
      <div class="tile-title">%s</div>
      <div class="tile-sub">%s</div>
    </div>
    <div class="accordion-summary-side">
      <span class="status-pill %s">%s</span>
      <span class="accordion-trigger"><span>%s</span><span class="accordion-caret">⌄</span></span>
    </div>
  </summary>
  <div class="accordion-body section-stack" style="padding-top:16px">
    <div class="section-card glass">
      <div class="page-heading" style="margin-bottom:14px">
        <div>
          <h3 style="margin:0">%s</h3>
          <p class="hint" style="margin:6px 0 0">%s</p>
        </div>
        <span class="status-pill %s">%s</span>
      </div>
      <div class="metric-grid">
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host"><code>%s</code></div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%s</div></div>
      </div>
      <p class="hint" style="margin:12px 0 0">%s <code>%s</code> ・ %s <code>%s</code></p>
    </div>
    <form method="POST" class="section-card glass">
      <input type="hidden" name="project_id" value="%s">
      <div class="page-heading" style="margin-bottom:14px">
        <div>
          <h3 style="margin:0">%s</h3>
          <p class="hint" style="margin:6px 0 0">%s</p>
        </div>
      </div>
      <div class="form-grid">
        <div>
          <label>Discord Guild ID</label>
          <input type="text" name="guild_id" value="%s" placeholder="123456789012345678">
          <p class="field-help">%s</p>
        </div>
      </div>
      <div class="button-row"><button type="submit" class="btn">%s</button></div>
    </form>
    <form method="POST" class="section-card glass delete-form" data-confirm="%s" data-require-text="%s">
      <input type="hidden" name="action" value="remove_connection">
      <input type="hidden" name="project_id" value="%s">
      <div class="page-heading" style="margin-bottom:14px">
        <div>
          <h3 style="margin:0">%s</h3>
          <p class="hint" style="margin:6px 0 0">%s</p>
        </div>
      </div>
      <p class="field-help" style="margin:0 0 12px">%s</p>
      <div class="button-row"><button type="submit" class="btn-danger">%s</button></div>
    </form>
    <form method="POST" class="section-card glass">
      <input type="hidden" name="action" value="preview_remove_connection_with_discord">
      <input type="hidden" name="project_id" value="%s">
      <div class="page-heading" style="margin-bottom:14px">
        <div>
          <h3 style="margin:0">%s</h3>
          <p class="hint" style="margin:6px 0 0">%s</p>
        </div>
      </div>
      <p class="field-help" style="margin:0 0 12px">%s</p>
      <div class="button-row"><button type="submit" class="btn-danger">%s</button></div>
    </form>
    %s
    %s
    %s
  </div>
</details>`,
				openAttr,
				esc(t(lang, "CONNECTED PRODUCTION", "CONNECTED PRODUCTION")),
				esc(p.Name),
				esc(fmt.Sprintf("%s%d / %s%d / %s%d", t(lang, "割り当て済み ", "Assigned "), assignedCount, t(lang, "未割り当て ", "Unassigned "), unassignedCount, t(lang, "チャンネル ", "Channels "), channelCount)),
				statusClass,
				esc(statusLabel),
				esc(t(lang, "詳細を見る", "Open details")),
				esc(p.Name),
				esc(t(lang, "この production の Discord 側 routing をここで確認・修正します。未割り当ての task type があれば、ここから channel 作成や割り当てを続けられます。", "Review and fix this production's Discord routing here. If task types are still unassigned, continue channel creation and assignment from here.")),
				statusClass,
				esc(statusLabel),
				esc(t(lang, "Discord Guild ID", "Discord Guild ID")),
				esc(fallbackText(effectiveGuildID, "—")),
				esc(t(lang, "Assigned task types", "Assigned task types")),
				assignedCount,
				esc(t(lang, "Unassigned task types", "Unassigned task types")),
				unassignedCount,
				esc(t(lang, "Language", "Language")),
				esc(strings.ToUpper(projectLang)),
				esc(t(lang, "Project ID", "Project ID")),
				esc(p.KitsuProjectID),
				esc(t(lang, "Discord Category ID", "Discord Category ID")),
				esc(categoryID),
				esc(p.KitsuProjectID),
				esc(t(lang, "Discord ID を編集", "Edit Discord ID")),
				esc(t(lang, "この production が使う Discord Server / Guild ID をここで確認・更新します。", "Review or update the Discord Server / Guild ID used by this production here.")),
				esc(effectiveGuildID),
				esc(t(lang, "この保存は Discord ID のみを更新します。", "This save action updates only the Discord ID.")),
				esc(t(lang, "保存", "Save")),
				esc(t(lang, p.Name+" の KitsuSync 連携だけを削除します。Discord channel / category は削除されません。", "Remove only the KitsuSync connection for "+p.Name+". Discord channels and category are kept.")),
				esc(t(lang, "削除", "delete")),
				esc(p.KitsuProjectID),
				esc(t(lang, "連携を削除", "Remove connection")),
				esc(t(lang, "この production の KitsuSync 側の接続情報だけを解除します。Discord の channel / category はそのまま残ります。", "Remove only this production's KitsuSync-side connection records. Discord channels and category stay as they are.")),
				esc(t(lang, "削除されるのは KitsuSync の保存データのみです。Discord 側の削除はこの操作では行いません。", "This deletes only KitsuSync's saved connection data. No Discord-side deletion happens in this action.")),
				esc(t(lang, "連携を削除", "Remove connection")),
				esc(p.KitsuProjectID),
				esc(t(lang, "Discordのチャンネルごと連携を削除", "Remove connection and Discord channels")),
				esc(t(lang, "将来の危険削除フロー候補です。この pass では実行せず、保存済みの category / channel 参照だけを preview 表示します。", "This is the more destructive delete path. In this pass it still does not execute; it only previews saved category / channel references.")),
				esc(t(lang, "この preview は Discord 側を削除しません。ownership が十分に証明できていないため、今は dry-run の確認だけを行います。", "This preview does not execute Discord deletion. Ownership is not proven strongly enough yet, so this step is dry-run only.")),
				esc(t(lang, "削除候補をプレビュー", "Preview Discord delete scope")),
				dangerPreviewHTML,
				validatedHTML,
				renderProjectChannels(p, webhooks, allTaskTypes, lang, r),
			))
		}
		if blocks.Len() == 0 {
			blocks.WriteString(emptyState("\U0001F5C2", t(lang, "まだ連携済みプロダクションがありません", "No connected productions yet."), t(lang, "先に新規連携セットアップで production connection を作成してから、ここで確認・編集してください。", "Create the first production connection in New Connection Setup, then review it here.")))
		}
		body := `<div class="section-stack"><div class="section-card glass"><p class="hint">` + esc(t(lang, "このページでは連携済み production ごとに Discord 側の接続情報と routing を管理します。新しい接続は新規連携セットアップから作成し、既存の見直しはここで行います。", "Use this page to manage Discord connection details and routing for each connected production. Create new connections in New Connection Setup, then review existing ones here.")) + `</p></div>` + blocks.String() + `</div>`
		fmt.Fprint(w, adminPage(lang, t(lang, "\u9023\u643a\u6e08\u307f\u30d7\u30ed\u30c0\u30af\u30b7\u30e7\u30f3\u7ba1\u7406", "Connected Productions"), r, body))
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
	ValidationSummary string
	Checks            []string
	Deleted           []connectedProductionChannelValidationResult
	Skipped           []connectedProductionChannelValidationResult
	Failed            []connectedProductionChannelValidationResult
	CleanupWarnings   []connectedProductionChannelValidationResult
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

	body, status, err := botDo("GET", discordAPI+"/guilds/"+effectiveGuildID+"/channels", nil, trimmedToken)
	if err != nil {
		for i := range results {
			results[i].Reason = t(lang, "Skip: Discord から guild channel list を取得できませんでした。", "Skipped: failed to read the guild channel list from Discord.")
		}
		return nil, results, t(lang, "Discord から現在の guild channel list を取得できませんでした: ", "Validation could not read the current guild channel list from Discord: ")+err.Error(), checks
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

func renderConnectedProductionChannelValidationCard(lang string, r *http.Request, project model.Project, summary string, checks []string, deletable []connectedProductionChannelValidationResult, skipped []connectedProductionChannelValidationResult) string {
	var checksHTML strings.Builder
	checksHTML.WriteString(`<ul class="list-tight" style="margin:0;padding-left:18px">`)
	for _, check := range checks {
		checksHTML.WriteString(`<li>` + esc(check) + `</li>`)
	}
	checksHTML.WriteString(`</ul>`)

	renderList := func(items []connectedProductionChannelValidationResult, includeReason bool) string {
		if len(items) == 0 {
			return `<p class="field-help" style="margin:0">` + esc(t(lang, "該当なし", "None")) + `</p>`
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

	return fmt.Sprintf(`
    <div class="section-card glass" style="border-color:#ffd4a8">
      <div class="page-heading" style="margin-bottom:14px">
        <div>
          <h3 style="margin:0">%s</h3>
          <p class="hint" style="margin:6px 0 0">%s</p>
        </div>
        <span class="status-pill warn">%s</span>
      </div>
      <div class="metric-grid">
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host">%s</div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value metric-value-host"><code>%s</code></div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
        <div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%d</div></div>
      </div>
      <p class="field-help" style="margin:12px 0 0">%s</p>
      <div class="section-stack" style="margin-top:12px">
        <div>
          <div class="eyebrow">%s</div>
          %s
        </div>
        <div>
          <div class="eyebrow">%s</div>
          %s
        </div>
        <div>
          <div class="eyebrow">%s</div>
          %s
        </div>
      </div>
      <div class="button-row" style="margin-top:14px">
        %s
        <a class="btn-ghost" href="%s">%s</a>
      </div>
    </div>`,
		esc(t(lang, "channel-only 削除候補の検証", "Validation-only review for channel delete candidates")),
		esc(t(lang, "Discord 上の current state と保存済み参照を照合した確認結果です。まだ削除は実行されません。将来の destructive 実装に進める候補と、手動確認が必要な候補を分けて表示します。", "This is a validation-only review card. It compares saved references with the current Discord state and does not execute deletion. It separates candidates that look safe enough from candidates that still need manual review.")),
		esc(t(lang, "VALIDATION ONLY", "VALIDATION ONLY")),
		esc(t(lang, "Production", "Production")),
		esc(project.Name),
		esc(t(lang, "Project ID", "Project ID")),
		esc(project.KitsuProjectID),
		esc(t(lang, "Deletable candidates", "Validated deletable candidates")),
		len(deletable),
		esc(t(lang, "Skipped candidates", "Skipped candidates")),
		len(skipped),
		esc(summary),
		esc(t(lang, "Checks performed", "Checks performed")),
		checksHTML.String(),
		esc(t(lang, "Deletable candidates", "Deletable candidates")),
		renderList(deletable, false),
		esc(t(lang, "Skipped / uncertain candidates", "Skipped / uncertain candidates")),
		renderList(skipped, true),
		renderConnectedProductionChannelDeleteAction(lang, project, len(deletable)),
		esc(withLang("/bot/admin/projects?project="+url.QueryEscape(project.KitsuProjectID)+"&danger_preview=1", r)),
		esc(t(lang, "検証前のプレビューへ戻る", "Back to saved-data preview")),
	)
}

func renderConnectedProductionChannelDeleteAction(lang string, project model.Project, deletableCount int) string {
	if deletableCount == 0 {
		return `<p class="field-help" style="margin:0">` + esc(t(lang, "削除を実行できる validated candidate はまだありません。skipped / uncertain candidate を確認してください。", "There are no validated candidates available for deletion yet. Review the skipped or uncertain candidates first.")) + `</p>`
	}
	return `<form method="POST" class="delete-form" style="margin:0" data-confirm="` +
		esc(t(lang, project.Name+" の validated Discord channel だけを削除します。production connection 全体や category は削除されません。", "Delete only the validated Discord channels for "+project.Name+". The production connection, project row, category, and unlink-only connection removal are not performed.")) +
		`" data-require-text="` + esc(t(lang, "削除", "delete")) + `">` +
		`<input type="hidden" name="action" value="execute_validated_channel_delete">` +
		`<input type="hidden" name="project_id" value="` + esc(project.KitsuProjectID) + `">` +
		`<button type="submit" class="btn-danger">` + esc(t(lang, "validated channel を削除", "Delete validated channels only")) + `</button></form>`
}

func executeConnectedProductionValidatedChannelDelete(lang string, project model.Project, effectiveGuildID, botToken string, db *gorm.DB) connectedProductionChannelDeleteExecution {
	allWebhooks := model.ListAllProjectWebhooks(db)
	webhooks := model.ListProjectWebhooks(db, project.KitsuProjectID)
	candidates := buildConnectedProductionChannelCandidates(webhooks)
	deletable, skipped, summary, checks := validateConnectedProductionChannelCandidates(lang, project, effectiveGuildID, botToken, allWebhooks, candidates)
	result := connectedProductionChannelDeleteExecution{
		ValidationSummary: summary,
		Checks:            checks,
		Skipped:           skipped,
	}
	for _, candidate := range deletable {
		if err := DeleteChannel(candidate.ChannelID, botToken); err != nil {
			failed := candidate
			failed.Reason = t(lang, "Delete failed: ", "Delete failed: ") + err.Error()
			result.Failed = append(result.Failed, failed)
			continue
		}
		result.Deleted = append(result.Deleted, candidate)
		if err := db.Where("kitsu_project_id = ? AND discord_channel_id = ?", project.KitsuProjectID, candidate.ChannelID).Delete(&model.ProjectWebhook{}).Error; err != nil {
			warn := candidate
			warn.Reason = t(lang, "Discord deletion succeeded, but DB webhook cleanup failed: ", "Discord deletion succeeded, but DB webhook cleanup failed: ") + err.Error()
			result.CleanupWarnings = append(result.CleanupWarnings, warn)
			continue
		}
	}
	return result
}

func renderConnectedProductionChannelDeleteResultPage(lang string, r *http.Request, project model.Project, result connectedProductionChannelDeleteExecution) string {
	renderList := func(items []connectedProductionChannelValidationResult, includeReason bool) string {
		if len(items) == 0 {
			return `<p class="field-help" style="margin:0">` + esc(t(lang, "該当なし", "None")) + `</p>`
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

	var checksHTML strings.Builder
	checksHTML.WriteString(`<ul class="list-tight" style="margin:0;padding-left:18px">`)
	for _, check := range result.Checks {
		checksHTML.WriteString(`<li>` + esc(check) + `</li>`)
	}
	checksHTML.WriteString(`</ul>`)

	statusLabel := t(lang, "一部完了", "Completed with review notes")
	statusClass := "warn"
	switch {
	case len(result.Failed) == 0 && len(result.CleanupWarnings) == 0:
		statusLabel = t(lang, "削除完了", "Deletion completed")
		statusClass = "ok"
	case len(result.Deleted) == 0 && len(result.Failed) > 0:
		statusLabel = t(lang, "削除失敗あり", "Deletion failed")
		statusClass = "bad"
	}

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
    <p class="field-help" style="margin:8px 0 0">%s</p>
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
  <div class="section-card glass">
    <div class="eyebrow">%s</div>
    %s
  </div>
  <div class="section-card glass">
    <div class="eyebrow">%s</div>
    %s
  </div>
  <div class="button-row">
    <a class="btn-ghost" href="%s">%s</a>
  </div>
</div>`,
		esc(t(lang, "CONNECTED PRODUCTION", "CONNECTED PRODUCTION")),
		esc(t(lang, "validated channel deletion result", "Channel-only delete result")),
		esc(t(lang, "validated candidate だけを対象に Discord channel deletion を実行した結果です。production connection 全体、project row、category は削除していません。", "This result covers actual Discord channel deletion only for validated candidates. Category deletion, project-row deletion, and unlink-only connection removal were not performed automatically.")),
		statusClass,
		esc(statusLabel),
		esc(t(lang, "Production", "Production")),
		esc(project.Name),
		esc(t(lang, "Project ID", "Project ID")),
		esc(project.KitsuProjectID),
		esc(t(lang, "Deleted channels", "Deleted channels")),
		len(result.Deleted),
		esc(t(lang, "Skipped + failed", "Not deleted / needs follow-up")),
		len(result.Skipped)+len(result.Failed)+len(result.CleanupWarnings),
		esc(result.ValidationSummary),
		esc(t(lang, "DB cleanup runs only for channels that were deleted successfully on Discord. Project, ProjectSetting, ProjectUserMap, ProjectCheckerMap, and Discord category remain untouched in this pass.", "DB cleanup runs only for channels that were deleted successfully on Discord. Project, ProjectSetting, ProjectUserMap, ProjectCheckerMap, Discord category, and unlink-only connection removal remain untouched in this pass.")),
		esc(t(lang, "Deleted channels", "Actually deleted on Discord")),
		renderList(result.Deleted, false),
		esc(t(lang, "Skipped / failed validation", "Not deleted: skipped / failed validation")),
		renderList(result.Skipped, true),
		esc(t(lang, "Failed deletions", "Delete attempted but failed")),
		renderList(result.Failed, true),
		esc(t(lang, "DB cleanup warnings", "Follow-up needed: DB cleanup warnings")),
		renderList(result.CleanupWarnings, true),
		esc(t(lang, "Checks used at execution time", "Validation checks used before execution")),
		checksHTML.String(),
		esc(withLang("/bot/admin/projects?project="+url.QueryEscape(project.KitsuProjectID)+"&danger_preview=1&validated_channels=1", r)),
		esc(t(lang, "validated review に戻る", "Back to validated review")),
	)
	return adminPage(lang, t(lang, "連携済みプロダクション管理", "Connected Productions"), r, body)
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
			botToken := storedRuntimeDiscordBotToken(db)
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
		healthyWebhookCount := 0
		warnWebhookCount := 0
		failedWebhookCount := 0
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
				failedWebhookCount++
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
				warnWebhookCount++
				statusCell = `<span class="status-pill" style="background:rgba(255,200,80,.14);color:#ffc850;border-color:rgba(255,200,80,.3)">` +
					esc(t(lang, "\u26a0\ufe0f 7\u65e5\u4ee5\u4e0a\u672a\u9001\u4fe1", "\u26a0\ufe0f No activity 7d+")) + `</span>`
			} else {
				healthyWebhookCount++
				statusCell = `<span class="status-pill ok">` + esc(t(lang, "\u6b63\u5e38", "OK")) + `</span>`
			}
			webhookRows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>#%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				esc(projName), esc(wh.ChannelName), esc(wh.TaskType), statusCell, lastSendCell, actionCell))
		}

		var webhookTableHTML string
		if len(allWebhooks) == 0 {
			webhookTableHTML = `<p class="hint">` + esc(t(lang, "Webhook\u306a\u3057\uff08\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u8a2d\u5b9a\u5f8c\u306b\u8868\u793a\uff09", "No webhooks configured (shown after project setup).")) + `</p>`
		} else {
			webhookTableHTML = `<div class="button-row" style="margin:0 0 14px 0">` +
				`<span class="status-pill ok">` + esc(fmt.Sprintf(t(lang, "正常: %d", "Healthy: %d"), healthyWebhookCount)) + `</span>` +
				`<span class="status-pill warn">` + esc(fmt.Sprintf(t(lang, "警告: %d", "Warning: %d"), warnWebhookCount)) + `</span>` +
				`<span class="status-pill bad">` + esc(fmt.Sprintf(t(lang, "失敗: %d", "Failed: %d"), failedWebhookCount)) + `</span>` +
				`</div>` +
				`<div class="table-wrap"><table><thead><tr>` +
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
			esc(t(lang, "Polling / Discord 送信 / Webhook ヘルス / リソース", "Polling / Discord Sends / Webhook Health / Resources")),
			esc(t(lang, "\u30dd\u30fc\u30ea\u30f3\u30b0", "Polling")), pollRows,
			esc(t(lang, "Discord \u9001\u4fe1", "Discord Sends")), sendRows,
			esc(t(lang, "Webhook \u30d8\u30eb\u30b9", "Webhook Health")), webhookTableHTML,
			esc(t(lang, "\u30ea\u30bd\u30fc\u30b9", "Resources")), resourceRows,
		)

		diagnosticsSection := `<div class="section-card glass"><div class="page-heading" style="margin-bottom:16px"><div><h3 style="margin:0">` +
			esc(t(lang, "通知と診断", "Delivery & Diagnostics")) +
			`</h3><p class="hint" style="margin:8px 0 0">` +
			esc(t(lang, "通知確認と runtime / configuration checks をまとめて確認できます。", "Review delivery verification with the current runtime / configuration checks.")) +
			`</p></div></div>` +
			renderDiagnosticsPanel(lang, r, db, func() (string, string, string, string) {
				return model.GetSetting(db, "kitsu.hostname"), storedRuntimeDiscordBotToken(db), model.GetSetting(db, "discord.guild_id"), model.GetSetting(db, "discord.webhook_url")
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
		projectUserRows := []model.ProjectUserMap{}
		projectCheckerRows := []model.ProjectCheckerMap{}
		legacyUserRows := model.ListUserMap(db)
		legacyCheckerRows := model.ListCheckerMap(db)
		if useProjectScoped {
			projectUserRows = model.ListProjectUserMaps(db, project.ID)
			projectCheckerRows = model.ListProjectCheckerMaps(db, project.ID)
		}
		taskTypes := assignmentTaskTypes(db, project)
		assignedUsers := buildLegacyAssignableUsers(legacyUserRows, botEmail)
		if useProjectScoped {
			assignedUsers = buildProjectAssignableUsers(projectUserRows, botEmail)
		}

		if r.Method == http.MethodPost {
			id := parseUint(r.FormValue("user_id"))
			name := strings.TrimSpace(r.FormValue("kitsu_name"))
			email := strings.TrimSpace(r.FormValue("kitsu_email"))
			discordID := strings.TrimSpace(r.FormValue("discord_id"))
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
			case "save_checker":
				selectedTaskTypes := selectedTaskTypesFromForm(r, taskTypes)
				if reviewer := findAssignmentUserByIdentity(assignedUsers, strings.TrimSpace(r.FormValue("reviewer_identity"))); reviewer != nil {
					if useProjectScoped {
						syncProjectCheckerAssignmentsForUser(db, project.ID, reviewer.KitsuName, reviewer.KitsuEmail, reviewer.DiscordID, selectedTaskTypes)
					} else {
						syncLegacyCheckerAssignmentsForUser(db, reviewer.KitsuName, reviewer.KitsuEmail, selectedTaskTypes)
					}
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
						} else {
							model.UpsertUserMapWithEmail(db, name, email, discordID)
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

		editID := parseUint(r.URL.Query().Get("edit"))
		selectedName, selectedEmail, selectedDiscordID := "", "", ""
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
				}
			}
		}
		personOptions := buildAssignmentPersonOptions(kitsuAssignmentOptions(kitsuPeople), selectedName, selectedEmail, lang)
		selectedReviewerIdentity := strings.TrimSpace(r.URL.Query().Get("reviewer"))
		if selectedReviewerIdentity == "" && (selectedName != "" || selectedEmail != "") {
			selectedReviewerIdentity = assignmentIdentityKey(selectedName, selectedEmail)
		}
		selectedReviewer := findAssignmentUserByIdentity(assignedUsers, selectedReviewerIdentity)
		if selectedReviewer == nil && len(assignedUsers) > 0 {
			selectedReviewer = &assignedUsers[0]
			selectedReviewerIdentity = assignmentIdentityKey(selectedReviewer.KitsuName, selectedReviewer.KitsuEmail)
		}
		selectedCheckerTaskTypes := []string{}
		if selectedReviewer != nil {
			if useProjectScoped {
				selectedCheckerTaskTypes = projectCheckerTaskTypesForUser(projectCheckerRows, selectedReviewer.KitsuName, selectedReviewer.KitsuEmail)
			} else {
				selectedCheckerTaskTypes = legacyCheckerTaskTypesForUser(legacyCheckerRows, selectedReviewer.KitsuName, selectedReviewer.KitsuEmail)
			}
		}
		reviewerUserOptions := buildAssignedUserOptions(assignedUsers, selectedReviewerIdentity, lang)

		var rows strings.Builder
		assignments := buildUnifiedAssignments(projectUserRows, legacyUserRows, projectCheckerRows, legacyCheckerRows, useProjectScoped)
		for _, user := range assignments {
			discordID := `<span class="status-pill bad">` + t(lang, "ID未設定", "No ID") + `</span>`
			if strings.TrimSpace(user.DiscordID) != "" {
				discordID = `<code>` + esc(user.DiscordID) + `</code>`
			}
			reviewerStatus := reviewerTaskTypeBadges(user.CheckerTaskTypes, lang)
			actionHTML := `<span class="muted">` + t(lang, "ユーザー割り当て未作成", "Create a user assignment first") + `</span>`
			if user.RowID > 0 {
				actionHTML = fmt.Sprintf(`<div class="inline-actions"><a class="btn-ghost" href="%s">%s</a><form method="POST" class="delete-form" data-confirm="%s" data-require-text="%s"><input type="hidden" name="action" value="delete"><input type="hidden" name="assignment_project_id" value="%s"><input type="hidden" name="user_id" value="%d"><input type="hidden" name="kitsu_name" value="%s"><input type="hidden" name="kitsu_email" value="%s"><button class="btn-danger" type="submit">%s</button></form></div>`,
					usersPageURL+"&edit="+strconv.FormatUint(uint64(user.RowID), 10), t(lang, "編集", "Edit"), esc(user.KitsuName), t(lang, "削除", "delete"), esc(activeProjectID), user.RowID, esc(user.KitsuName), esc(user.KitsuEmail), t(lang, "削除", "Delete"))
			}
			rows.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				esc(user.KitsuName), discordID, reviewerStatus, actionHTML))
		}
		if rows.Len() == 0 {
			rows.WriteString(`<tr><td colspan="4" class="muted">` + t(lang, "まだユーザー割り当てがありません。", "No user assignments yet.") + `</td></tr>`)
		}

		orphanReviewers := orphanReviewerAssignments(assignments, lang)
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
			selectorCard = `<div class="section-card glass"><h3>` + t(lang, "編集対象の production", "Editing production") + `</h3><p class="hint">` + t(lang, "ユーザー割り当てと reviewer / checker 設定は production ごとに切り替えて編集します。", "Switch the production here to edit user assignments and reviewer/checker settings per production.") + `</p><form method="GET" action="/bot/admin/users"><input type="hidden" name="lang" value="` + esc(lang) + `"><div class="form-grid"><div><label>` + t(lang, "Production", "Production") + `</label><select name="project">` + projectOptions.String() + `</select></div></div><div class="button-row"><button type="submit" class="btn">` + t(lang, "表示を切り替え", "Switch production") + `</button></div></form></div>`
		}
		assignmentActionRow := `<div class="button-row"><button type="submit" class="btn">` + t(lang, "保存", "Save") + `</button><a class="btn-ghost" href="` + esc(usersPageURL) + `">` + t(lang, "キャンセル", "Cancel") + `</a></div>`
		body := fmt.Sprintf(`
<div class="section-stack">
  %s
  <form method="POST" class="section-stack" style="margin:0">
    <div class="section-card glass">
      <h3>%s</h3>
      <p class="hint">%s</p>
      <p class="field-help">%s</p>
      <p class="field-help">%s</p>
      %s
      <input type="hidden" name="user_id" value="%d">
      <input type="hidden" name="assignment_project_id" value="%s">
      <input type="hidden" id="kitsuNameInput" name="kitsu_name" value="%s">
      <input type="hidden" id="kitsuEmailInput" name="kitsu_email" value="%s">
      <div class="form-grid">
        <div><label>%s</label><select id="personSelect" onchange="syncPersonSelect()" required>%s</select></div>
        <div><label>%s</label><input type="text" name="discord_id" value="%s" placeholder="123456789012345678"><div class="field-help">%s</div></div>
      </div>
      %s
    </div>
    <div class="section-card glass"><h3>%s</h3><div class="table-wrap"><table><thead><tr><th>%s</th><th>Discord ID</th><th>%s</th><th>%s</th></tr></thead><tbody>%s</tbody></table></div></div>
    <div class="section-card glass">
      <h3>%s</h3>
      <p class="hint">%s</p>
      %s
      <div class="field-help">%s</div>
    </div>
  </form>
  %s
</div>
<script>
function syncPersonSelect(){
  var sel = document.getElementById('personSelect');
  var opt = sel && sel.options ? sel.options[sel.selectedIndex] : null;
  document.getElementById('kitsuNameInput').value = opt ? (opt.getAttribute('data-name') || '') : '';
  document.getElementById('kitsuEmailInput').value = opt ? (opt.getAttribute('data-email') || '') : '';
}
var checkerTaskTypes = [];
var checkerTaskTypeIcons = {};
function escapeAttr(value){
  return String(value).replace(/&/g, '&amp;').replace(/\"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
function initCheckerTaskTypes(){
  var allEl = document.getElementById('checkerTaskTypeAll');
  var iconsEl = document.getElementById('checkerTaskTypeIcons');
  var selectedEl = document.getElementById('checkerTaskTypeSelected');
  if(!allEl || !iconsEl || !selectedEl){
    return;
  }
  try {
    checkerTaskTypes = JSON.parse(selectedEl.textContent || '[]');
  } catch (err) {
    checkerTaskTypes = [];
  }
  try {
    checkerTaskTypeIcons = JSON.parse(iconsEl.textContent || '{}');
  } catch (err) {
    checkerTaskTypeIcons = {};
  }
  renderCheckerTaskTypes();
}
function checkerTaskTypeIcon(taskType){
  return checkerTaskTypeIcons[taskType] || '🏷️';
}
function renderCheckerTaskTypes(){
  var allEl = document.getElementById('checkerTaskTypeAll');
  var select = document.getElementById('checkerTaskTypeSelect');
  var chips = document.getElementById('checkerTaskTypeChips');
  var hidden = document.getElementById('checkerTaskTypeHiddenInputs');
  if(!allEl || !select || !chips || !hidden){
    return;
  }
  var allTaskTypes = [];
  try {
    allTaskTypes = JSON.parse(allEl.textContent || '[]');
  } catch (err) {
    allTaskTypes = [];
  }
  select.innerHTML = '';
  var placeholder = document.createElement('option');
  placeholder.value = '';
  placeholder.textContent = %q;
  select.appendChild(placeholder);
  allTaskTypes.forEach(function(taskType){
    if(checkerTaskTypes.indexOf(taskType) !== -1){
      return;
    }
    var option = document.createElement('option');
    option.value = taskType;
    option.textContent = checkerTaskTypeIcon(taskType) + ' ' + taskType;
    select.appendChild(option);
  });
  hidden.innerHTML = checkerTaskTypes.map(function(taskType){
    return '<input type=\"hidden\" name=\"checker_task_type\" value=\"' + escapeAttr(taskType) + '\">';
  }).join('');
  if(checkerTaskTypes.length === 0){
    chips.innerHTML = '<span class=\"status-pill ok\">' + %q + '</span>';
    return;
  }
  chips.innerHTML = checkerTaskTypes.map(function(taskType){
    return '<span class=\"status-pill warn\">' + checkerTaskTypeIcon(taskType) + ' ' + escapeAttr(taskType) + ' <button type=\"button\" class=\"btn-ghost\" data-task-type=\"' + escapeAttr(taskType) + '\" style=\"padding:2px 8px;min-height:auto\" onclick=\"removeCheckerTaskType(this.getAttribute(&quot;data-task-type&quot;))\">%s</button></span>';
  }).join('');
}
function addCheckerTaskType(){
  var select = document.getElementById('checkerTaskTypeSelect');
  if(!select || !select.value){
    return;
  }
  if(checkerTaskTypes.indexOf(select.value) === -1){
    checkerTaskTypes.push(select.value);
    checkerTaskTypes.sort();
  }
  renderCheckerTaskTypes();
}
function removeCheckerTaskType(taskType){
  checkerTaskTypes = checkerTaskTypes.filter(function(value){ return value !== taskType; });
  renderCheckerTaskTypes();
}
document.addEventListener('DOMContentLoaded', function(){
  syncPersonSelect();
  initCheckerTaskTypes();
});
</script>`,
			selectorCard,
			formTitle,
			t(lang, "User = タスク割り当て時に @mention します。Reviewer / Checker は必要な人だけ追加で設定します。", "User = @mentioned when a task is assigned. Reviewer / Checker is optional and only needed for specific people."),
			t(lang, "チェッカーでない人には割り当てないでください。必要な人だけ設定してください。", "Only configure reviewer/checker for people who actually need it."),
			t(lang, "Task type ごとの reviewer/checker は 1 人です。既に他の人に設定されている task type を保存すると、その人から移動します。", "Each task type has a single reviewer/checker. Saving a task type already assigned to someone else will move it to this person."),
			scopeHint,
			userID, esc(activeProjectID), esc(selectedName), esc(selectedEmail),
			t(lang, "Kitsuユーザー", "Kitsu user"), personOptions,
			t(lang, "DiscordユーザーID", "Discord user ID"), esc(selectedDiscordID), t(lang, "未入力の場合は ID未設定 と表示されます。", "If empty, the UI will show No ID."), assignmentActionRow,
			t(lang, "現在の割り当て", "Current assignments"), t(lang, "名前", "Name"), t(lang, "レビュアー / チェッカー", "Reviewer / Checker"), t(lang, "操作", "Actions"), rows.String(),
			t(lang, "レビュアー / チェッカー task type", "Reviewer / Checker task types"),
			t(lang, "この selector は、選択中の production に割り当て済みのユーザーだけを表示します。新しいユーザーは先に上で割り当てを作成してください。", "This selector only shows users already assigned to the selected production. Create the user assignment above first for new people."),
			reviewerAssignmentSection(usersPageURL, activeProjectID, reviewerUserOptions, selectedReviewerIdentity, selectedReviewer, taskTypes, selectedCheckerTaskTypes, lang),
			t(lang, "task type を 1 つずつ追加してください。追加済みの task type は下に表示され、不要なら外せます。", "Add task types one by one. Selected task types appear below and can be removed if not needed."),
			orphanReviewers,
			t(lang, "task type を選択", "Select task type"),
			t(lang, "未設定", "Not set"),
			esc(t(lang, "外す", "Remove")),
		)
		fmt.Fprint(w, adminPage(lang, t(lang, "ユーザー割り当て", "User Assignment"), r, body))
	}
}

func CheckersHandler(_ *gorm.DB, _ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, withLang("/bot/admin/users", r), http.StatusSeeOther)
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
			blocks.WriteString(emptyState("📁", t(lang, "まだプロジェクトがありません", "No projects yet"), t(lang, "先に新規連携セットアップで project routing を作成してから補助リンクを設定してください。", "Create project routing in New Connection Setup first, then add helper links here.")))
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
				setRuntimeDiscordBotToken(db, value)
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
			t(lang, "共有Bot / Runtime 設定", "Shared Bot / Runtime"), t(lang, "新規連携セットアップで使う共有 Bot / Runtime の設定を確認・更新できます。", "Review and update the shared Bot / Runtime settings used by New Connection Setup."), statusClass, statusLabel, esc(effectiveHost), t(lang, "Bot Token", "Bot Token"), secretStatus(storedRuntimeDiscordBotToken(db), lang), withLang("/bot/admin/bot?edit=1", r), t(lang, "再認証して編集する", "Re-authenticate to edit"), withLang("/bot/setup", r), t(lang, "新規連携セットアップへ戻る", "Back to New Connection Setup"), withLang("/bot/admin/projects", r), t(lang, "連携済みプロダクション管理を開く", "Open Connected Productions"))
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
				authNoticeHTML(lang, t(lang, "再認証済み", "Re-authenticated"), t(lang, "編集モードは一時的に有効です。", "Edit mode is temporarily enabled.")), t(lang, "Discord 設定", "Discord settings"), esc(effectiveHost), t(lang, "必要な時だけ新しい Token を入力してください。", "Only paste a new token when rotating it."), t(lang, "このトークン変更は現在実行中のプロセスに即時反映され、アプリ設定にも保存されます。", "Token changes take effect immediately for the running process and are also saved in app settings."), t(lang, "再起動後は保存済み token が優先されます。.env.local / 環境変数は fallback としてのみ使われます。", "After restart, the saved token is used first. .env.local / environment variables remain fallback sources only."), t(lang, "Kitsu Runtime 接続", "Kitsu runtime connection"), t(lang, "Runtime メール", "Runtime email"), esc(kitsuEmail), t(lang, "Runtime パスワード", "Runtime password"), t(lang, "必要な時だけ専用 Runtime パスワードを入力してください。", "Only paste a new dedicated runtime password when rotating it."), t(lang, "保存", "Save"), withLang("/bot/setup", r), t(lang, "新規連携セットアップへ戻る", "Back to New Connection Setup"), withLang("/bot/admin/projects", r), t(lang, "連携済みプロダクション管理を開く", "Open Connected Productions"))
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

type assignmentUserOption struct {
	RowID uint
	KitsuName string
	KitsuEmail string
	DiscordID string
}

func buildPersonOptions(persons []KitsuPerson, selectedName, selectedEmail, lang string) string {
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
	if (selectedEmail != "" || selectedName != "") && !found {
		out.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s" data-email="%s" selected>%s</option>`, esc(selectedEmail), esc(selectedName), esc(selectedEmail), esc(selectedName)))
	}
	return out.String()
}

func botAccountEmail(db *gorm.DB) string {
	return strings.ToLower(storedRuntimeKitsuEmail(db))
}

func filterAssignablePersons(persons []KitsuPerson, botEmail string) []KitsuPerson {
	filtered := make([]KitsuPerson, 0, len(persons))
	for _, person := range persons {
		if shouldExcludeBotPerson(person, botEmail) {
			continue
		}
		filtered = append(filtered, person)
	}
	return filtered
}

func shouldExcludeBotPerson(person KitsuPerson, botEmail string) bool {
	if !strings.EqualFold(strings.TrimSpace(person.Email), strings.TrimSpace(botEmail)) {
		return false
	}
	if !person.Active {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(person.FullName))
	return name == "bot" || name == strings.ToLower(runtimeBotFirstName+" "+runtimeBotLastName)
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

func buildProjectAssignableUsers(rows []model.ProjectUserMap, botEmail string) []assignmentUserOption {
	filtered := make([]assignmentUserOption, 0, len(rows))
	for _, row := range rows {
		if botEmail != "" && strings.EqualFold(strings.TrimSpace(row.KitsuEmail), botEmail) {
			continue
		}
		filtered = append(filtered, assignmentUserOption{
			RowID: row.ID,
			KitsuName: row.KitsuName,
			KitsuEmail: row.KitsuEmail,
			DiscordID: row.DiscordUserID,
		})
	}
	return filtered
}

func buildLegacyAssignableUsers(rows []model.UserMap, botEmail string) []assignmentUserOption {
	filtered := make([]assignmentUserOption, 0, len(rows))
	for _, row := range filterAssignableUsers(rows, botEmail) {
		filtered = append(filtered, assignmentUserOption{
			RowID: row.ID,
			KitsuName: row.KitsuName,
			KitsuEmail: row.KitsuEmail,
			DiscordID: row.DiscordID,
		})
	}
	return filtered
}

func buildAssignedUserOptions(users []assignmentUserOption, selectedIdentity, lang string) string {
	var out strings.Builder
	out.WriteString(`<option value="">` + t(lang, "選択してください", "Select user") + `</option>`)
	for _, user := range users {
		identity := assignmentIdentityKey(user.KitsuName, user.KitsuEmail)
		out.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s" data-email="%s" data-discord-id="%s" %s>%s</option>`, esc(identity), esc(user.KitsuName), esc(user.KitsuEmail), esc(user.DiscordID), selectedAttr(identity == selectedIdentity), esc(user.KitsuName)))
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

func findAssignmentUser(users []assignmentUserOption, email, name string) *assignmentUserOption {
	for _, user := range users {
		if email != "" && strings.EqualFold(strings.TrimSpace(user.KitsuEmail), strings.TrimSpace(email)) {
			copyUser := user
			return &copyUser
		}
	}
	for _, user := range users {
		if name != "" && strings.EqualFold(strings.TrimSpace(user.KitsuName), strings.TrimSpace(name)) {
			copyUser := user
			return &copyUser
		}
	}
	return nil
}

func findAssignmentUserByIdentity(users []assignmentUserOption, identity string) *assignmentUserOption {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil
	}
	for _, user := range users {
		if assignmentIdentityKey(user.KitsuName, user.KitsuEmail) == identity {
			copyUser := user
			return &copyUser
		}
	}
	return nil
}

func checkerResolvedInput(lang, value string) string {
	return `<label>Resolved ID</label><input type="text" name="resolved_discord_id" value="` + esc(value) + `" placeholder="123456789012345678"><div class="field-help">` + t(lang, "通常はユーザー割り当ての Discord ID を自動参照します。必要なときだけ直接上書きしてください。", "Discord IDs are normally resolved from User Assignment. Only override this when needed.") + `</div>`
}

func projectCheckerResolvedID(row model.ProjectCheckerMap) string {
	if strings.TrimSpace(row.OverrideDiscordID) != "" {
		return strings.TrimSpace(row.OverrideDiscordID)
	}
	return strings.TrimSpace(row.DiscordUserID)
}

func projectCheckerDisplayName(row model.ProjectCheckerMap, users []assignmentUserOption) string {
	if strings.TrimSpace(row.KitsuName) != "" {
		return strings.TrimSpace(row.KitsuName)
	}
	if user := findAssignmentUser(users, row.KitsuEmail, row.KitsuName); user != nil {
		return user.KitsuName
	}
	for _, user := range users {
		if row.DiscordUserID != "" && strings.TrimSpace(user.DiscordID) == strings.TrimSpace(row.DiscordUserID) {
			return user.KitsuName
		}
	}
	return ""
}

type unifiedAssignmentRow struct {
	RowID             uint
	KitsuName         string
	KitsuEmail        string
	DiscordID         string
	CheckerTaskTypes  []string
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

func buildUnifiedAssignments(projectUsers []model.ProjectUserMap, legacyUsers []model.UserMap, projectCheckers []model.ProjectCheckerMap, legacyCheckers []model.CheckerMap, useProjectScoped bool) []unifiedAssignmentRow {
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
	addChecker := func(name, email, taskType, discordID string) {
		key := assignmentIdentityKey(name, email)
		current := rows[key]
		if current.KitsuName == "" {
			current.KitsuName = strings.TrimSpace(name)
			current.KitsuEmail = strings.TrimSpace(email)
		}
		if current.DiscordID == "" {
			current.DiscordID = strings.TrimSpace(discordID)
		}
		if strings.TrimSpace(taskType) != "" && !containsAssignmentTaskType(current.CheckerTaskTypes, taskType) {
			current.CheckerTaskTypes = append(current.CheckerTaskTypes, taskType)
			sort.Strings(current.CheckerTaskTypes)
		}
		rows[key] = current
	}
	if useProjectScoped {
		for _, row := range projectUsers {
			addBase(row.ID, row.KitsuName, row.KitsuEmail, row.DiscordUserID)
		}
		for _, row := range projectCheckers {
			addChecker(row.KitsuName, row.KitsuEmail, row.TaskType, row.DiscordUserID)
		}
	} else {
		for _, row := range legacyUsers {
			addBase(row.ID, row.KitsuName, row.KitsuEmail, row.DiscordID)
		}
		for _, row := range legacyCheckers {
			addChecker(row.KitsuName, row.KitsuEmail, row.TaskType, row.DiscordID)
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

func reviewerTaskTypePicker(taskTypes, selected []string, lang string) string {
	taskTypesJSON, _ := json.Marshal(taskTypes)
	selectedJSON, _ := json.Marshal(selected)
	iconsJSON, _ := json.Marshal(taskTypeIconMap(taskTypes))
	return fmt.Sprintf(`
<div class="checker-task-picker">
  <div style="display:flex;gap:10px;align-items:flex-start;flex-wrap:wrap">
    <select id="checkerTaskTypeSelect" style="min-width:220px"></select>
    <button type="button" class="btn-ghost" onclick="addCheckerTaskType()">%s</button>
  </div>
  <div id="checkerTaskTypeHiddenInputs"></div>
  <div id="checkerTaskTypeChips" style="display:flex;gap:8px;flex-wrap:wrap;margin-top:12px"></div>
  <script type="application/json" id="checkerTaskTypeAll">%s</script>
  <script type="application/json" id="checkerTaskTypeIcons">%s</script>
  <script type="application/json" id="checkerTaskTypeSelected">%s</script>
</div>`,
		esc(t(lang, "追加", "Add")),
		string(taskTypesJSON),
		string(iconsJSON),
		string(selectedJSON),
	)
}

func reviewerAssignmentSection(usersPageURL, activeProjectID, reviewerUserOptions, selectedReviewerIdentity string, selectedReviewer *assignmentUserOption, taskTypes, selectedCheckerTaskTypes []string, lang string) string {
	if selectedReviewer == nil {
		return `<p class="field-help">` + t(lang, "まだ割り当て済みユーザーがありません。先に上の Add/Edit User Assignment で production へユーザーを追加してください。", "There are no assigned users yet. Add a user to this production above before configuring reviewer/checker task types.") + `</p>`
	}
	return `<form method="GET" action="/bot/admin/users"><input type="hidden" name="lang" value="` + esc(lang) + `"><input type="hidden" name="project" value="` + esc(activeProjectID) + `"><div class="form-grid"><div><label>` + t(lang, "Reviewer / Checker を編集するユーザー", "User to edit reviewer/checker settings") + `</label><select name="reviewer">` + reviewerUserOptions + `</select></div></div><div class="button-row"><button type="submit" class="btn-ghost">` + t(lang, "設定を読み込む", "Load settings") + `</button></div></form>` +
		`<form method="POST" class="section-stack" style="margin-top:12px"><input type="hidden" name="action" value="save_checker"><input type="hidden" name="assignment_project_id" value="` + esc(activeProjectID) + `"><input type="hidden" name="reviewer_identity" value="` + esc(selectedReviewerIdentity) + `"><p class="field-help">` + t(lang, "現在編集中: ", "Currently editing: ") + `<strong>` + esc(selectedReviewer.KitsuName) + `</strong></p><div class="checker-option-list">` + reviewerTaskTypePicker(taskTypes, selectedCheckerTaskTypes, lang) + `</div><div class="button-row"><button type="submit" class="btn">` + t(lang, "レビュアー設定を保存", "Save reviewer settings") + `</button></div></form>`
}

func taskTypeIconMap(taskTypes []string) map[string]string {
	icons := make(map[string]string, len(taskTypes))
	for _, taskType := range taskTypes {
		icons[taskType] = taskTypeIcon(taskType)
	}
	return icons
}

func reviewerTaskTypeBadges(taskTypes []string, lang string) string {
	if len(taskTypes) == 0 {
		return `<span class="status-pill ok">` + t(lang, "未設定", "Not set") + `</span>`
	}
	var out strings.Builder
	for _, taskType := range taskTypes {
		out.WriteString(`<span class="status-pill warn" style="margin-right:6px">` + taskTypeIcon(taskType) + ` ` + esc(taskType) + `</span>`)
	}
	return out.String()
}

func orphanReviewerAssignments(rows []unifiedAssignmentRow, lang string) string {
	var items []string
	for _, row := range rows {
		if row.RowID == 0 && len(row.CheckerTaskTypes) > 0 {
			items = append(items, `<li><strong>`+esc(row.KitsuName)+`</strong>: `+reviewerTaskTypeBadges(row.CheckerTaskTypes, lang)+`</li>`)
		}
	}
	if len(items) == 0 {
		return ""
	}
	return `<div class="section-card glass"><h3>` + t(lang, "要確認の reviewer 設定", "Reviewer settings to review") + `</h3><p class="hint">` + t(lang, "ユーザー割り当てのベース行が無い reviewer / checker 設定があります。必要ならユーザー割り当てを作成してから整理してください。", "Some reviewer / checker settings do not have a base user assignment row yet. Create a user assignment for them if you need to keep them.") + `</p><ul>` + strings.Join(items, "") + `</ul></div>`
}

func projectCheckerTaskTypesForUser(rows []model.ProjectCheckerMap, name, email string) []string {
	selected := make([]string, 0)
	key := assignmentIdentityKey(name, email)
	for _, row := range rows {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) == key {
			selected = append(selected, row.TaskType)
		}
	}
	sort.Strings(selected)
	return selected
}

func legacyCheckerTaskTypesForUser(rows []model.CheckerMap, name, email string) []string {
	selected := make([]string, 0)
	key := assignmentIdentityKey(name, email)
	for _, row := range rows {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) == key {
			selected = append(selected, row.TaskType)
		}
	}
	sort.Strings(selected)
	return selected
}

func selectedTaskTypesFromForm(r *http.Request, validTaskTypes []string) []string {
	valid := make(map[string]bool, len(validTaskTypes))
	for _, taskType := range validTaskTypes {
		valid[taskType] = true
	}
	selected := make([]string, 0)
	seen := map[string]bool{}
	for _, taskType := range r.Form["checker_task_type"] {
		taskType = strings.TrimSpace(taskType)
		if taskType == "" || !valid[taskType] || seen[taskType] {
			continue
		}
		seen[taskType] = true
		selected = append(selected, taskType)
	}
	sort.Strings(selected)
	return selected
}

func syncProjectCheckerAssignmentsForUser(db *gorm.DB, projectRowID uint, name, email, discordID string, selectedTaskTypes []string) {
	selectedSet := make(map[string]bool, len(selectedTaskTypes))
	for _, taskType := range selectedTaskTypes {
		selectedSet[taskType] = true
		model.UpsertProjectCheckerMapWithUser(db, projectRowID, taskType, name, email, discordID, "")
	}
	for _, row := range model.ListProjectCheckerMaps(db, projectRowID) {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) == assignmentIdentityKey(name, email) && !selectedSet[row.TaskType] {
			model.DeleteProjectCheckerMapByID(db, row.ID)
		}
	}
}

func syncLegacyCheckerAssignmentsForUser(db *gorm.DB, name, email string, selectedTaskTypes []string) {
	existingRows := model.ListCheckerMap(db)
	identityKey := assignmentIdentityKey(name, email)
	selectedSet := make(map[string]bool, len(selectedTaskTypes))
	for _, taskType := range selectedTaskTypes {
		selectedSet[taskType] = true
		for _, row := range existingRows {
			if row.TaskType == taskType && assignmentIdentityKey(row.KitsuName, row.KitsuEmail) != identityKey {
				model.DeleteCheckerEntryByID(db, row.ID)
			}
		}
		model.AddCheckerMapByUserWithOverride(db, taskType, name, email, "")
	}
	for _, row := range model.ListCheckerMap(db) {
		if assignmentIdentityKey(row.KitsuName, row.KitsuEmail) != identityKey {
			continue
		}
		if selectedSet[row.TaskType] {
			model.UpdateCheckerMapWithOverride(db, row.ID, row.TaskType, name, email, "")
		} else {
			model.DeleteCheckerEntryByID(db, row.ID)
		}
	}
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

func assignmentTaskTypes(db *gorm.DB, project *model.Project) []string {
	taskTypes := []string{}
	if project != nil {
		seen := map[string]bool{}
		for _, row := range model.ListProjectWebhooks(db, project.KitsuProjectID) {
			taskType := strings.TrimSpace(row.TaskType)
			if taskType == "" || seen[taskType] {
				continue
			}
			seen[taskType] = true
			taskTypes = append(taskTypes, taskType)
		}
	}
	if len(taskTypes) == 0 {
		taskTypes = AllTaskTypeNames()
	}
	sort.Strings(taskTypes)
	return taskTypes
}

func containsAssignmentTaskType(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
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
		`<a class="nav-chip" href="` + withLang("/bot/setup", r) + `">` + t(lang, "新規連携セットアップ", "New Connection Setup") + `</a>` +
		`<a class="nav-chip" href="` + withLang("/bot/logout", r) + `">` + t(lang, "ログアウト", "Logout") + `</a>` +
		`</div>`
	content := `<div class="page-card glass"><div class="page-heading"><div><h1>` + esc(title) + `</h1></div></div>` +
		message + body + `</div>` +
		`<div id="deleteModal" class="delete-modal"><div class="delete-box glass"><h2 class="delete-title">` + esc(t(lang, "削除の確認", "Confirm deletion")) + `</h2><p id="deleteModalText" class="delete-text"></p><p id="deleteModalHelper" class="field-help hidden"></p><div id="deleteModalInputWrap" class="delete-input hidden"><label><span class="sr-only">Confirm text</span><input id="deleteModalInput" type="text" autocomplete="off" autocapitalize="off" spellcheck="false"></label><div class="field-help">` + esc(t(lang, "確認ワード", "Confirmation word")) + `: <code id="deleteModalExpected"></code></div></div><div class="button-row"><button id="deleteConfirmBtn" type="button" class="btn-danger">` + esc(t(lang, "削除する", "Delete")) + `</button><button id="deleteCancelBtn" type="button" class="btn-ghost">` + esc(t(lang, "キャンセル", "Cancel")) + `</button></div></div></div>` +
		baseAdminJS(lang)
	return appShell("KitsuSync", "", lang, r, nav, content)
}

