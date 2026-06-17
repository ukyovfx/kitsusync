package setup

import (
	"fmt"
	"net/http"

	"gorm.io/gorm"
)

// WizardHandler keeps /bot/setup-wizard as a compatibility route and hands operators
// off to Project Management, which is now the main setup surface.
func WizardHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		body := fmt.Sprintf(`
<div class="section-stack">
  <div class="section-card glass" style="border-color:rgba(255,141,72,.32)">
    <div class="page-heading">
      <div>
        <h2>%s</h2>
        <p class="hint" style="margin:8px 0 0">%s</p>
        <p class="hint" style="margin:8px 0 0">%s</p>
      </div>
      <span class="status-pill warn">%s</span>
    </div>
    <div class="button-row" style="margin-top:16px">
      <a class="btn" href="%s">%s</a>
      <a class="btn-ghost" href="%s">%s</a>
      <a class="btn-ghost" href="%s">%s</a>
    </div>
  </div>
</div>
<script>
setTimeout(function(){
  window.location.replace(%q);
}, 1200);
</script>`,
			esc(t(lang, "セットアップの入り口が変わりました", "Setup entry has moved")),
			esc(t(lang, "/bot/setup-wizard は互換用の案内ページになりました。通常のセットアップ / 運用は Project Management から進めてください。", "/bot/setup-wizard is now a compatibility handoff page. Use Project Management for normal setup and operator workflow.")),
			esc(t(lang, "詳しい確認や修正が必要な場合は Manual Setup / Diagnostics を使えます。", "If you need deeper checks or repairs, Manual Setup / Diagnostics remain available.")),
			esc(t(lang, "移行済み", "Moved")),
			withLang("/bot/setup", r),
			esc(t(lang, "Project Management を開く", "Open Project Management")),
			withLang("/bot/admin/setup", r),
			esc(t(lang, "Manual Setup", "Manual Setup")),
			withLang("/bot/admin/diagnostics", r),
			esc(t(lang, "Diagnostics", "Diagnostics")),
			withLang("/bot/setup", r),
		)
		fmt.Fprint(w, adminPage(lang, t(lang, "セットアップ移行", "Setup Handoff"), r, body))
	}
}
