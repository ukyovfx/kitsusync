package setup

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"app/src/model"
)

// livenessThreshold is how long the system status tolerates silence from the
// poll loop before it asks for a review: ten poll intervals, but never less
// than ten minutes, so a short interval does not cry wolf during a restart.
func livenessThreshold(pollInterval time.Duration) time.Duration {
	threshold := 10 * pollInterval
	if threshold < 10*time.Minute {
		threshold = 10 * time.Minute
	}
	return threshold
}

// pollStale reports whether the last successful poll is older than the
// liveness threshold. A poll that has never succeeded is not stale; the
// status shows it as never run instead.
func pollStale(lastPoll, now time.Time, pollInterval time.Duration) bool {
	if lastPoll.IsZero() {
		return false
	}
	return now.Sub(lastPoll) > livenessThreshold(pollInterval)
}

// lastKitsuFetchSuccess returns the newest successful Kitsu API observation.
func lastKitsuFetchSuccess(stats RuntimeSnapshot) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, observation := range stats.APIObservations["kitsu"] {
		if !observation.Success {
			continue
		}
		if !found || observation.At.After(latest) {
			latest = observation.At
			found = true
		}
	}
	return latest, found
}

// livenessRow renders one last-success row of the liveness table.
func livenessRow(lang, label string, at time.Time, known bool, pillClass, pillLabel string) string {
	when := t(lang, "未実行", "never")
	if known {
		when = time.Since(at).Round(time.Second).String() + " " + t(lang, "前", "ago")
	}
	return fmt.Sprintf(`<tr><th scope="row" class="mobile-card-lead"><span class="mobile-card-lead-label" data-label="%s">%s</span></th><td data-label="%s"><span class="status-pill %s" role="status">%s</span></td><td data-label="%s">%s</td></tr>`,
		esc(t(lang, "対象", "Stage")), esc(label), esc(t(lang, "状態", "State")), esc(pillClass), esc(pillLabel), esc(t(lang, "最終成功", "Last success")), esc(when))
}

// renderLivenessSection shows when the poll loop, the Kitsu fetch and the
// Discord send last succeeded. A poll success older than the liveness
// threshold is marked as needing review: a silent poller is exactly the
// failure this page exists to catch.
func renderLivenessSection(lang string, stats RuntimeSnapshot, pollInterval time.Duration) string {
	stale := pollStale(stats.LastPollTime, time.Now(), pollInterval)
	pollPillClass, pollPillLabel := "ok", t(lang, "正常", "OK")
	if stats.LastPollTime.IsZero() {
		pollPillClass, pollPillLabel = "warning", t(lang, "未実行", "Never run")
	} else if stale {
		pollPillClass, pollPillLabel = "bad", t(lang, "要確認", "Needs review")
	}
	kitsuAt, kitsuKnown := lastKitsuFetchSuccess(stats)
	kitsuPillClass, kitsuPillLabel := "ok", t(lang, "正常", "OK")
	if !kitsuKnown {
		kitsuPillClass, kitsuPillLabel = "warning", t(lang, "未確認", "Unconfirmed")
	}
	sendKnown := !stats.LastSendSuccessTime.IsZero()
	sendPillClass, sendPillLabel := "ok", t(lang, "正常", "OK")
	if !sendKnown {
		sendPillClass, sendPillLabel = "warning", t(lang, "未実行", "Never run")
	}
	rows := strings.Join([]string{
		livenessRow(lang, t(lang, "ポーリング", "Polling"), stats.LastPollTime, !stats.LastPollTime.IsZero(), pollPillClass, pollPillLabel),
		livenessRow(lang, t(lang, "Kitsu 取得", "Kitsu fetch"), kitsuAt, kitsuKnown, kitsuPillClass, kitsuPillLabel),
		livenessRow(lang, t(lang, "Discord 送信", "Discord send"), stats.LastSendSuccessTime, sendKnown, sendPillClass, sendPillLabel),
	}, "")
	headerPill := ""
	if stale {
		headerPill = `<span class="status-pill bad" role="status">` + esc(t(lang, "要確認", "Needs review")) + `</span>`
	}
	table := `<table class="data-table mobile-card-table health-status-table" aria-label="` + esc(t(lang, "最終成功時刻", "Last success times")) + `"><thead><tr><th scope="col">` + esc(t(lang, "対象", "Stage")) + `</th><th scope="col">` + esc(t(lang, "状態", "State")) + `</th><th scope="col">` + esc(t(lang, "最終成功", "Last success")) + `</th></tr></thead><tbody>` + rows + `</tbody></table>`
	return `<section class="section-card glass liveness" aria-labelledby="liveness-title"><div class="page-heading"><div><h2 id="liveness-title">` + esc(t(lang, "動作の最終確認", "Liveness")) + `</h2><p class="hint">` + esc(t(lang, "ポーリング・Kitsu 取得・Discord 送信が最後に成功した時刻です。ポーリング成功が止まると要確認になります。", "When polling, the Kitsu fetch and the Discord send last succeeded. A stalled poll is marked as needing review.")) + `</p></div>` + headerPill + `</div>` + table + `</section>`
}

// renderHandoffAttentionSection lists the handoff outcomes an operator
// should look at: handoffs that fell back to the Production's admins because
// the receiving task had no assignee. (The suspicious-shelf check was
// abolished with the publish detector, redesign core v2: route mistakes are
// now caught by the save-time validation and the decision log.)
// It renders nothing when there is nothing to review.
func renderHandoffAttentionSection(lang string, db *gorm.DB) string {
	if db == nil {
		return ""
	}
	fallbackCount, err := model.CountFallbackHandoffEvents(db)
	if err != nil || fallbackCount == 0 {
		return ""
	}
	var body strings.Builder
	body.WriteString(`<p class="hint">` + esc(fmt.Sprintf(t(lang, "受け側に担当者がおらず管理者へ送られた受け渡し: %d 件", "Handoffs sent to the admins because the receiving task had no assignee: %d"), fallbackCount)) + `</p>`)
	if recent, err := model.FallbackHandoffEvents(db, 5); err == nil && len(recent) > 0 {
		var items []string
		for _, event := range recent {
			label := event.EntityName + " (" + event.SourceTaskTypeName
			if strings.TrimSpace(event.ProductType) != "" {
				label += " × " + event.ProductType
			}
			label += " → " + event.NotifyTaskTypeName + ")"
			items = append(items, `<li>`+esc(label)+`</li>`)
		}
		body.WriteString(`<ul class="handoff-fallback-list">` + strings.Join(items, "") + `</ul>`)
	}
	return `<section class="section-card glass handoff-attention" aria-labelledby="handoff-attention-title"><div class="page-heading"><div><h2 id="handoff-attention-title">` + esc(t(lang, "受け渡しの要確認", "Handoff items to review")) + `</h2><p class="hint">` + esc(t(lang, "担当者不在で管理者に送られた受け渡しを表示します。", "Handoffs that fell back to the admins because nobody was assigned.")) + `</p></div></div>` + body.String() + `</section>`
}
