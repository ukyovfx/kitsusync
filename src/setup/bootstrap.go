package setup

import (
	"fmt"
	"net/http"
)

func renderSetupRequiredPage(lang string, r *http.Request) string {
	body := fmt.Sprintf(`<div class="setup-required-state"><p class="setup-required-message">%s</p><div class="button-row"><a class="btn" href="%s">%s</a></div></div>`,
		t(lang, "Kitsu接続を設定すると通知を開始できます。", "Configure the Kitsu connection before notifications can start."),
		withLang("/bot/admin/bot?edit=1", r), t(lang, "接続設定を開く", "Open Connections"))
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
