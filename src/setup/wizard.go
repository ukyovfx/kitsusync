package setup

import (
	"fmt"
	"net/http"

	"gorm.io/gorm"
)

func renderSetupCompatHandoffPage(lang string, r *http.Request, title, bodyCopy string) string {
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
    </div>
  </div>
</div>
<script>
setTimeout(function(){
  window.location.replace(%q);
}, 1200);
</script>`,
		esc(title),
		esc(bodyCopy),
		esc(t(lang, "通常のセットアップや運用は /bot/setup の Project Management から進めてください。", "Use Project Management in /bot/setup for normal setup and operator workflow.")),
		esc(t(lang, "移動済み", "Moved")),
		withLang("/bot/setup", r),
		esc(t(lang, "Project Management を開く", "Open Project Management")),
		withLang("/bot/admin/diagnostics", r),
		esc(t(lang, "Diagnostics を開く", "Open Diagnostics")),
		withLang("/bot/setup", r),
	)
	return adminPage(lang, title, r, body)
}

// WizardHandler keeps /bot/setup-wizard as a compatibility route and hands operators
// off to Project Management, which is now the main setup surface.
func WizardHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		fmt.Fprint(w, renderSetupCompatHandoffPage(
			lang,
			r,
			t(lang, "セットアップ画面の入口が変わりました", "Setup entry has moved"),
			t(lang, "/bot/setup-wizard は互換用の案内ページです。Setup flow has moved to Project Management.", "/bot/setup-wizard is now a compatibility handoff page. Setup flow has moved to Project Management."),
		))
	}
}
