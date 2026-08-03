package setup

import (
	"net/http"
)

func renderSetupRequiredPage(lang string, r *http.Request) string {
	body := `<div class="section-stack">` +
		`<div class="section-card glass"><div class="page-heading"><div><div class="eyebrow">SETUP REQUIRED</div><h2>` + t(lang, "初期設定が必要です", "Setup required") + `</h2><p class="hint">` + t(lang, "Kitsu との接続を設定すると、通知を開始できます。", "Configure the Kitsu connection before notifications can start.") + `</p></div><span class="status-pill bad">` + t(lang, "未接続", "Disconnected") + `</span></div>` +
		`<div class="metric-grid"><div class="metric-card"><div class="metric-label">Kitsu</div><div class="metric-value">` + t(lang, "未接続", "Disconnected") + `</div></div><div class="metric-card"><div class="metric-label">` + t(lang, "通知", "Notifications") + `</div><div class="metric-value">` + t(lang, "停止中", "Paused") + `</div></div></div></div>` +
		`<div class="section-card glass"><h3>` + t(lang, "Kitsu 接続を設定", "Configure Kitsu connection") + `</h3><p class="hint">` + t(lang, "ログインした Kitsu 管理者権限を使って、通知専用の runtime アカウントを安全に準備します。管理画面のログイン session は通知処理には使いません。", "Use the signed-in Kitsu administrator permission to prepare a dedicated runtime account. The browser session is not reused by notification processing.") + `</p><form method="POST"><input type="hidden" name="action" value="runtime_setup_from_session"><div class="button-row"><button class="btn" type="submit">` + t(lang, "Kitsu 接続を設定", "Configure Kitsu connection") + `</button></div></form></div>` +
		`<div class="section-card glass"><h3>Discord</h3><p class="hint">` + t(lang, "Kitsu 接続の完了後に、Bot Settings と production setup から設定します。", "Configure Discord later from Bot Settings and production setup.") + `</p></div></div>`
	return adminPage(lang, t(lang, "初期設定", "Initial setup"), r, body)
}

func RuntimeReadyRequired(ready func() bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil && !ready() {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			lang := currentLang(r)
			body := `<div class="section-card glass"><h2>` + t(lang, "初期設定が必要です", "Setup required") + `</h2><p>` + t(lang, "Kitsu は未接続で、通知は停止しています。先に Kitsu 接続を設定してください。", "Kitsu is disconnected and notifications are paused. Configure the Kitsu connection first.") + `</p><div class="button-row"><a class="btn" href="` + withLang("/bot/setup", r) + `">` + t(lang, "初期設定を開く", "Open setup") + `</a></div></div>`
			_, _ = w.Write([]byte(adminPage(lang, t(lang, "初期設定が必要です", "Setup required"), r, body)))
			return
		}
		next(w, r)
	}
}
