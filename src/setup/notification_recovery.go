package setup

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

type VFXFilesDeliveryVerifier func(db *gorm.DB, eventID, attemptID, messageID, threadID, actorEmail string, now time.Time) error

// NotificationRecoveryHandler exposes unresolved VFX Files delivery state to
// authenticated administrators. It only offers provider readback; there is
// deliberately no retry or manual-success action.
func NotificationRecoveryHandler(db *gorm.DB, verify VFXFilesDeliveryVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		session, ok := currentSessionData(r)
		if !ok {
			http.Redirect(w, r, appendLang("/bot/login?next=%2Fbot%2Fadmin%2Fnotifications%2Frecovery", currentLang(r)), http.StatusSeeOther)
			return
		}
		if !(strings.EqualFold(session.Role, "manager") || strings.EqualFold(session.Role, "admin")) {
			http.Error(w, "administrator access required", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			if !botEditAllowed(r) {
				http.Error(w, "recent administrator authentication required", http.StatusForbidden)
				return
			}
			if verify == nil {
				http.Error(w, "provider verification unavailable; delivery remains unresolved", http.StatusServiceUnavailable)
				return
			}
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid form", http.StatusBadRequest)
				return
			}
			eventID := strings.TrimSpace(r.FormValue("event_id"))
			attemptID := strings.TrimSpace(r.FormValue("attempt_id"))
			messageID := strings.TrimSpace(r.FormValue("message_id"))
			threadID := strings.TrimSpace(r.FormValue("thread_id"))
			if eventID == "" || len(eventID) > 64 || attemptID == "" || len(attemptID) > 32 || !adminDiscordID(messageID) || (threadID != "" && !adminDiscordID(threadID)) {
				http.Error(w, "invalid reconciliation evidence; delivery remains unresolved", http.StatusBadRequest)
				return
			}
			if err := verify(db, eventID, attemptID, messageID, threadID, session.Email, time.Now().UTC()); err != nil {
				http.Error(w, "Discord readback did not prove this exact delivery; it remains unknown", http.StatusConflict)
				return
			}
			http.Redirect(w, r, withLang("/bot/admin/notifications/recovery?verified=1", r), http.StatusSeeOther)
			return
		}
		rows, err := model.ListUnknownVFXFilesDeliveries(db, 100)
		if err != nil {
			http.Error(w, "delivery journal unavailable", http.StatusServiceUnavailable)
			return
		}
		writeNotificationRecoveryPage(w, r, rows)
	}
}

func adminDiscordID(value string) bool {
	if len(value) < 17 || len(value) > 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func writeNotificationRecoveryPage(w http.ResponseWriter, r *http.Request, rows []model.UnknownVFXFilesDelivery) {
	lang := currentLang(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var body strings.Builder
	body.WriteString(`<!doctype html><html lang="` + esc(lang) + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + esc(t(lang, "通知配送の再照合", "Notification delivery reconciliation")) + `</title><link rel="stylesheet" href="/static/css/app.css"></head><body><main class="app-shell"><header class="page-header"><h1>` + esc(t(lang, "通知配送の再照合", "Notification delivery reconciliation")) + `</h1><a class="btn-ghost" href="` + esc(withLang("/bot/admin", r)) + `">` + esc(t(lang, "管理画面へ戻る", "Back to admin")) + `</a></header>`)
	if r.URL.Query().Get("verified") == "1" {
		body.WriteString(`<p role="status">` + esc(t(lang, "Discordの読み戻し証拠を検証し、配送記録へ反映しました。", "Discord readback evidence was verified and recorded.")) + `</p>`)
	}
	body.WriteString(`<p>` + esc(t(lang, "unknownの通知は自動再送されません。Message IDを入力すると、保存済みWebhookでDiscordへ読み戻しを行い、送信先と本文が一致した場合だけ配送済みに確定します。検証できない場合はunknownのままです。", "Unknown notifications are never retried automatically. Enter a Message ID to read it back through the stored webhook. The record is marked delivered only when the destination and rendered payload match; otherwise it remains unknown.")) + `</p>`)
	if len(rows) == 0 {
		body.WriteString(`<p role="status">` + esc(t(lang, "未解決のFiles Plugin配送はありません。", "No unresolved Files Plugin deliveries.")) + `</p>`)
	}
	for _, row := range rows {
		attemptStarted := "not recorded"
		if row.AttemptStartedAt != nil {
			attemptStarted = row.AttemptStartedAt.UTC().Format(time.RFC3339)
		}
		body.WriteString(`<section class="section-card glass"><h2>` + esc(row.ProductionID) + ` / ` + esc(row.TaskID) + `</h2><dl><dt>Event ID</dt><dd><code>` + esc(row.EventID) + `</code></dd><dt>Plugin Source Event ID</dt><dd><code>` + esc(row.SourceEventID) + `</code></dd><dt>Attempt ID</dt><dd><code>` + esc(row.AttemptID) + `</code></dd><dt>Attempt started</dt><dd>` + esc(attemptStarted) + `</dd><dt>Task Type</dt><dd>` + esc(row.TaskTypeID) + `</dd><dt>Destination Channel</dt><dd>` + esc(row.DestinationChannelID) + `</dd><dt>Failure</dt><dd>` + esc(row.FailureClass) + `</dd><dt>Attempts</dt><dd>` + fmt.Sprint(row.AttemptCount) + `</dd><dt>Payload SHA-256</dt><dd><code>` + esc(row.PayloadSHA256) + `</code></dd></dl><form method="post"><input type="hidden" name="event_id" value="` + esc(row.EventID) + `"><input type="hidden" name="attempt_id" value="` + esc(row.AttemptID) + `"><label>Discord Message ID <input name="message_id" inputmode="numeric" pattern="[0-9]{17,20}" maxlength="20" required></label><label>Thread ID, if posted in a thread <input name="thread_id" inputmode="numeric" pattern="[0-9]{17,20}" maxlength="20"></label><button class="btn" type="submit">` + esc(t(lang, "Discordで証拠を検証", "Verify evidence with Discord")) + `</button></form></section>`)
	}
	body.WriteString(`</main></body></html>`)
	_, _ = w.Write([]byte(body.String()))
}
