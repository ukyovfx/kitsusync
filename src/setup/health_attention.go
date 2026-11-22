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
	table := `<table class="data-table mobile-card-table" aria-label="` + esc(t(lang, "最終成功時刻", "Last success times")) + `"><thead><tr><th scope="col">` + esc(t(lang, "対象", "Stage")) + `</th><th scope="col">` + esc(t(lang, "状態", "State")) + `</th><th scope="col">` + esc(t(lang, "最終成功", "Last success")) + `</th></tr></thead><tbody>` + rows + `</tbody></table>`
	return `<section class="section-card glass liveness" aria-labelledby="liveness-title"><div class="page-heading"><div><h2 id="liveness-title">` + esc(t(lang, "動作の最終確認", "Liveness")) + `</h2><p class="hint">` + esc(t(lang, "ポーリング・Kitsu 取得・Discord 送信が最後に成功した時刻です。ポーリング成功が止まると要確認になります。", "When polling, the Kitsu fetch and the Discord send last succeeded. A stalled poll is marked as needing review.")) + `</p></div>` + headerPill + `</div>` + table + `</section>`
}

// renderHandoffAttentionSection lists the handoff outcomes an operator
// should look at: shelved pairs that another Task Type hands off (a likely
// mapping mistake, recorded by the detector) and handoffs that fell back to
// the Production's admins because the receiving task had no assignee.
// It renders nothing when there is nothing to review.
func renderHandoffAttentionSection(lang string, db *gorm.DB) string {
	if db == nil {
		return ""
	}
	var shelfRows []string
	for _, project := range model.ListProjects(db) {
		name := project.Name
		if strings.TrimSpace(name) == "" {
			name = project.KitsuProjectID
		}
		for _, mark := range model.ListSuspiciousShelves(db, project.KitsuProjectID) {
			pair := mark.TaskTypeName + " × " + mark.ProductType
			detail := t(lang, mark.OtherTaskTypeName+" では受け渡しになっています", "Handed off for "+mark.OtherTaskTypeName)
			shelfRows = append(shelfRows, fmt.Sprintf(`<tr><th scope="row" class="mobile-card-lead"><span class="mobile-card-lead-label" data-label="%s">%s</span></th><td data-label="%s">%s</td><td data-label="%s">%s</td><td data-label="%s"><span class="status-pill bad" role="status">%s</span></td></tr>`,
				esc(t(lang, "プロダクション", "Production")), esc(name), esc(t(lang, "棚置きの組み合わせ", "Shelved pair")), esc(pair), esc(t(lang, "理由", "Reason")), esc(detail), esc(t(lang, "状態", "State")), esc(t(lang, "要確認", "Needs review"))))
		}
	}
	fallbackCount, err := model.CountFallbackHandoffEvents(db)
	if err != nil {
		fallbackCount = 0
	}
	if len(shelfRows) == 0 && fallbackCount == 0 {
		return ""
	}
	var body strings.Builder
	if len(shelfRows) > 0 {
		body.WriteString(`<table class="data-table mobile-card-table" aria-label="` + esc(t(lang, "棚置きの要確認", "Shelved pairs to review")) + `"><thead><tr><th scope="col">` + esc(t(lang, "プロダクション", "Production")) + `</th><th scope="col">` + esc(t(lang, "棚置きの組み合わせ", "Shelved pair")) + `</th><th scope="col">` + esc(t(lang, "理由", "Reason")) + `</th><th scope="col">` + esc(t(lang, "状態", "State")) + `</th></tr></thead><tbody>` + strings.Join(shelfRows, "") + `</tbody></table>`)
	}
	if fallbackCount > 0 {
		body.WriteString(`<p class="hint">` + esc(fmt.Sprintf(t(lang, "受け側に担当者がおらず管理者へ送られた受け渡し: %d 件", "Handoffs sent to the admins because the receiving task had no assignee: %d"), fallbackCount)) + `</p>`)
		if recent, err := model.FallbackHandoffEvents(db, 5); err == nil && len(recent) > 0 {
			var items []string
			for _, event := range recent {
				items = append(items, `<li>`+esc(event.EntityName+" ("+event.SourceTaskTypeName+" × "+event.ProductType+" → "+event.NotifyTaskTypeName+")")+`</li>`)
			}
			body.WriteString(`<ul class="handoff-fallback-list">` + strings.Join(items, "") + `</ul>`)
		}
	}
	headerPill := ""
	if len(shelfRows) > 0 {
		headerPill = `<span class="status-pill bad">` + esc(fmt.Sprint(len(shelfRows))) + `</span>`
	}
	return `<section class="section-card glass handoff-attention" aria-labelledby="handoff-attention-title"><div class="page-heading"><div><h2 id="handoff-attention-title">` + esc(t(lang, "受け渡しの要確認", "Handoff items to review")) + `</h2><p class="hint">` + esc(t(lang, "通知はされていません。対応表の誤りの可能性がある棚置きと、担当者不在で管理者に送られた受け渡しを表示します。", "Nothing here was notified. Shelved pairs that may be mapping mistakes and handoffs that fell back to the admins.")) + `</p></div>` + headerPill + `</div>` + body.String() + `</section>`
}
