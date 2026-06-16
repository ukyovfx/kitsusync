package setup

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

// RenderWizardEntryPage renders the /bot/setup-wizard entry screen.
// Shows "Guided Setup / Setup Status" choice cards, or a completion banner if setup is done.
func RenderWizardEntryPage(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string), r *http.Request) string {
	lang := currentLang(r)
	diag := localizeSetupDiagnostics(lang, BuildSetupDiagnostics(db, refreshCreds))

	if diag.SetupComplete {
		body := fmt.Sprintf(`
<div class="section-card glass">
  <div class="guided-head">
    <div>
      <h2>%s</h2>
      <p class="guided-kicker">%s</p>
    </div>
    <div class="guided-pill" style="background:rgba(142,207,139,.18);color:#8ecf8b">%s</div>
  </div>
</div>
<div class="guided-banner ok" style="margin-top:12px">
  <strong>%s</strong>
  <p class="guided-note" style="margin-top:6px">%s</p>
  <div class="guided-actions" style="margin-top:12px">
    <a class="btn" href="%s">%s</a>
    <a class="btn-ghost" href="%s">%s</a>
  </div>
</div>`,
			html.EscapeString(t(lang, "セットアップ完了", "Setup Complete")),
			html.EscapeString(t(lang, "全ての条件が満たされています。KitsuSync は稼働中です。", "All conditions are met. KitsuSync is running.")),
			html.EscapeString(t(lang, "complete", "complete")),
			html.EscapeString(t(lang, "セットアップ完了", "Setup Complete")),
			html.EscapeString(t(lang, "Kitsu の変更は自動的に Discord に通知されます。", "Kitsu changes will be automatically posted to Discord.")),
			withLang("/bot/admin", r),
			html.EscapeString(t(lang, "Admin へ", "Go to Admin")),
			withLang("/bot/admin/setup", r),
			html.EscapeString(t(lang, "Manual Setup / Diagnostics", "Manual Setup / Diagnostics")),
		)
		return adminPage(lang, t(lang, "セットアップ完了", "Setup Complete"), r, body)
	}

	incomplete := len(incompleteReasons(diag))
	var summaryClass, summaryText string
	if incomplete == 0 {
		summaryClass = "ok"
		summaryText = t(lang, "セットアップ完了", "Setup complete")
	} else {
		summaryClass = "warn"
		summaryText = fmt.Sprintf(t(lang, "あと %d 項目", "%d item(s) remaining"), incomplete)
	}

	body := fmt.Sprintf(`
<div class="section-card glass">
  <div class="guided-head">
    <div>
      <h2>%s</h2>
      <p class="guided-kicker">%s</p>
    </div>
    <div class="guided-pill">%s</div>
  </div>
</div>
<div class="guided-banner %s" style="margin-bottom:18px">%s</div>
<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px">

  <div class="guided-overview" style="display:flex;flex-direction:column;gap:14px">
    <div class="guided-head"><h3>%s</h3></div>
    <p class="guided-note">%s</p>
    <ul class="guided-note" style="padding-left:18px;margin:0">
      <li>%s</li>
      <li>%s</li>
      <li>%s</li>
    </ul>
    <div class="guided-actions" style="margin-top:auto;padding-top:12px">
      <a class="btn" href="%s">%s</a>
    </div>
  </div>

  <div class="guided-overview" style="display:flex;flex-direction:column;gap:14px">
    <div class="guided-head"><h3>%s</h3></div>
    <p class="guided-note">%s</p>
    <ul class="guided-note" style="padding-left:18px;margin:0">
      <li>%s</li>
      <li>%s</li>
      <li>%s</li>
    </ul>
    <div class="guided-actions" style="margin-top:auto;padding-top:12px">
      <a class="btn-ghost" href="%s">%s</a>
    </div>
  </div>

</div>`,
		html.EscapeString(t(lang, "KitsuSync セットアップ", "KitsuSync Setup")),
		html.EscapeString(t(lang, "まずは Guided Setup から始めるのがおすすめです。", "We recommend starting with Guided Setup for first-time setup.")),
		html.EscapeString(t(lang, "初回セットアップ", "First-time setup")),
		summaryClass,
		html.EscapeString(summaryText),

		// Guided Setup card
		html.EscapeString(t(lang, "Guided Setup（おすすめ）", "Guided Setup (Recommended)")),
		html.EscapeString(t(lang, "初回導入ではこちらがおすすめです。1画面1ステップで順番に進み、各ステップに『なぜ必要か』の説明があります。", "Recommended for first-time setup. Move step by step, one screen at a time, with an explanation of why each step matters.")),
		html.EscapeString(t(lang, "Kitsu 接続 → Discord Bot → Project Setup の順に進む", "Kitsu → Discord Bot → Project Setup in order")),
		html.EscapeString(t(lang, "次に何をすればいいか常に表示される", "Always shows what to do next")),
		html.EscapeString(t(lang, "前のステップが完了しないと次に進めない", "Can't skip ahead until previous step is done")),
		withLang("/bot/setup-wizard?mode=guided", r),
		html.EscapeString(t(lang, "Guided Setup を開始 →", "Start Guided Setup →")),

		// Setup Status card
		html.EscapeString(t(lang, "Setup Status（準備状態の確認）", "Setup Status (Readiness overview)")),
		html.EscapeString(t(lang, "現在のセットアップ状況と準備状態を確認する画面です。別のセットアップ手順ではなく、Guided Setup の前後に状態確認や見直しを行うための画面です。", "Use this screen to review current setup status and readiness. It is not a separate setup flow, and it does not replace Guided Setup.")),
		html.EscapeString(t(lang, "8つの完了条件をカードで確認", "View all 8 completion conditions as cards")),
		html.EscapeString(t(lang, "現在どこが未完了かを確認できる", "Shows what is still incomplete right now")),
		html.EscapeString(t(lang, "自動セットアップは行わず、必要に応じて Guided Setup や Manual Setup へ進む", "Does not perform automatic setup actions; continue to Guided Setup or Manual Setup when needed")),
		withLang("/bot/setup-wizard?mode=quick", r),
		html.EscapeString(t(lang, "Setup Status を確認 →", "Check Setup Status →")),
	)
	return adminPage(lang, t(lang, "KitsuSync セットアップ", "KitsuSync Setup"), r, body)
}

// RenderQuickSetupPage renders the ?mode=quick status overview with 8 condition cards.
func RenderQuickSetupPage(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string), r *http.Request) string {
	lang := currentLang(r)
	diag := localizeSetupDiagnostics(lang, BuildSetupDiagnostics(db, refreshCreds))
	reasons := incompleteReasons(diag)

	var summaryClass, summaryText string
	if diag.SetupComplete {
		summaryClass = "ok"
		summaryText = t(lang, "セットアップ完了 — KitsuSync は稼働中です", "Setup complete — KitsuSync is running")
	} else {
		summaryClass = "warn"
		summaryText = fmt.Sprintf(
			t(lang, "あと %d 項目 — 各カードの Fix を確認し、Guided Setup で順を追って進めると確実です。", "%d item(s) remaining — check each card's Fix hint, or use Guided Setup to walk through step by step."),
			len(reasons),
		)
	}

	var envCards strings.Builder
	for _, c := range diag.Env {
		envCards.WriteString(renderCheckCard(lang, c))
	}

	var projectCards strings.Builder
	for _, p := range diag.Projects {
		projectCards.WriteString(renderProjectCard(lang, p))
	}
	if projectCards.Len() == 0 {
		projectCards.WriteString(fmt.Sprintf(
			`<div class="setup-card warn"><div class="setup-card-head"><div><h3>%s</h3></div><span class="pill">WARN</span></div>`+
				`<p>%s</p><p><a href="%s">%s</a></p></div>`,
			html.EscapeString(t(lang, "Project mapping", "Project mapping")),
			html.EscapeString(t(lang, "プロジェクトが設定されていません。Guided Setup の Step 3 で設定してください。", "No project configured. Set it up in Guided Setup Step 3.")),
			withLang("/bot/setup-wizard?mode=guided", r),
			html.EscapeString(t(lang, "Guided Setup Step 3 へ", "Go to Guided Setup Step 3")),
		))
	}

	body := fmt.Sprintf(`
<div class="section-card glass">
  <div class="guided-head">
    <div>
      <h2>%s</h2>
      <p class="guided-kicker">%s</p>
    </div>
    <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
      <a class="btn-ghost" style="font-size:.82rem;padding:5px 12px" href="%s">%s</a>
      <a class="btn-ghost" style="font-size:.82rem;padding:5px 12px" href="%s">%s</a>
      <a class="btn" style="font-size:.82rem;padding:5px 14px" href="%s">%s</a>
    </div>
  </div>
</div>
<div class="guided-banner %s" style="margin-bottom:16px"><strong>%s</strong></div>
<div class="section-card glass">
  <h3>%s</h3>
  <div class="setup-grid">%s</div>
</div>
<div class="section-card glass">
  <h3>%s</h3>
  <div class="setup-grid">%s%s</div>
</div>
<div class="section-card glass">
  <h3>%s</h3>
  <div class="setup-grid">%s</div>
</div>
<div class="section-card glass">
  <h3>%s</h3>
  <div class="setup-grid">%s</div>
</div>`,
		html.EscapeString(t(lang, "Setup Status", "Setup Status")),
		html.EscapeString(t(lang, "現在のセットアップ状況と準備状態を一覧で確認できます。ここではセットアップ処理は行いません。", "Check current setup status and readiness at a glance. This page does not perform setup actions.")),
		withLang("/bot/setup-wizard", r),
		html.EscapeString(t(lang, "← Entry", "← Entry")),
		withLang("/bot/admin/setup", r),
		html.EscapeString(t(lang, "Manual Setup / Diagnostics →", "Manual Setup / Diagnostics →")),
		withLang("/bot/setup-wizard?mode=guided", r),
		html.EscapeString(t(lang, "Guided Setup →", "Guided Setup →")),
		summaryClass,
		html.EscapeString(summaryText),
		html.EscapeString(t(lang, "環境変数", "Environment Variables")),
		envCards.String(),
		html.EscapeString(t(lang, "接続", "Connections")),
		renderCheckCard(lang, diag.Kitsu),
		renderCheckCard(lang, diag.Discord),
		html.EscapeString(t(lang, "Project マッピング", "Project Mapping")),
		projectCards.String(),
		html.EscapeString(t(lang, "テスト通知", "Test Notification")),
		renderCheckCard(lang, diag.TestNotification),
	)
	return adminPage(lang, t(lang, "Setup Status", "Setup Status"), r, body)
}
