package setup

import (
	"fmt"
	"net/http"
)

func renderSetupRequiredPage(lang string, r *http.Request) string {
	body := fmt.Sprintf(`<div class="section-stack"><div class="section-card glass"><div class="page-heading"><div><p class="hint">%s</p></div><span class="status-pill bad" role="status">%s</span></div><div class="metric-grid"><div class="metric-card"><div class="metric-label">Kitsu</div><div class="metric-value">%s</div></div><div class="metric-card"><div class="metric-label">%s</div><div class="metric-value">%s</div></div></div></div><div class="section-card glass"><h3>%s</h3><p class="hint">%s</p><div class="button-row"><a class="btn" href="%s">%s</a></div></div><div class="section-card glass"><h3>Discord</h3><p class="hint">%s</p></div></div>`,
		t(lang, "Kitsu接続を設定すると通知を開始できます。", "Configure the Kitsu connection before notifications can start."),
		t(lang, "未接続", "Disconnected"), t(lang, "未接続", "Disconnected"),
		t(lang, "通知", "Notifications"), t(lang, "停止中", "Paused"),
		t(lang, "Kitsu接続設定", "Kitsu connection settings"),
		t(lang, "Kitsu Bot API Tokenを接続設定で読み取り専用検証し、成功した場合だけ保存します。", "Validate a Kitsu Bot API token read-only in Connections, then save it only after validation succeeds."),
		withLang("/bot/admin/bot?edit=1", r), t(lang, "接続設定を開く", "Open Connections"),
		t(lang, "Kitsu接続の後にBot接続とProductionを設定します。", "Configure the Discord Bot and Productions after Kitsu is connected."))
	return adminPage(lang, t(lang, "初期設定", "Initial setup"), r, body)
}

func RuntimeReadyRequired(ready func() bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ready != nil && !ready() {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			lang := currentLang(r)
			body := `<div class="section-card glass"><p>` + t(lang, "Kitsuは未接続のため、通知は停止しています。先にKitsu接続を設定してください。", "Kitsu is disconnected and notifications are paused. Configure the Kitsu connection first.") + `</p><div class="button-row"><a class="btn" href="` + withLang("/bot/admin/bot?edit=1", r) + `">` + t(lang, "Kitsu接続設定を開く", "Open Kitsu connection settings") + `</a></div></div>`
			_, _ = w.Write([]byte(adminPage(lang, t(lang, "初期設定が必要です", "Setup required"), r, body)))
			return
		}
		next(w, r)
	}
}
