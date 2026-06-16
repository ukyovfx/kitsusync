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
<div class="section-card glass" style="margin-bottom:16px;border-color:rgba(255,141,72,.32)">
  <div class="guided-head">
    <div>
      <h3>%s</h3>
      <p class="guided-note">%s</p>
    </div>
    <div class="guided-pill" style="background:rgba(255,141,72,.16);color:#ffb27f">%s</div>
  </div>
</div>
<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px">

  <div class="guided-overview" style="display:flex;flex-direction:column;gap:14px;border:1px solid rgba(255,141,72,.32);background:linear-gradient(180deg, rgba(255,141,72,.08), rgba(255,255,255,.03))">
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

</div>
<div class="section-card glass" style="margin-top:16px">
  <div class="guided-head">
    <div>
      <h3>%s</h3>
      <p class="guided-note">%s</p>
    </div>
    <div class="guided-pill">%s</div>
  </div>
  <div class="guided-actions" style="margin-top:12px">
    <a class="btn-ghost" href="%s">%s</a>
    <a class="btn-ghost" href="%s">%s</a>
  </div>
</div>`,
		html.EscapeString(t(lang, "KitsuSync セットアップ", "KitsuSync Setup")),
		html.EscapeString(t(lang, "初回の運用者は Guided Setup から始めてください。再開時は Setup Status で現在の準備状態を確認できます。", "First-time operators should start with Guided Setup. Returning operators can review the current readiness in Setup Status.")),
		html.EscapeString(t(lang, "Start here", "Start here")),
		summaryClass,
		html.EscapeString(summaryText),
		html.EscapeString(t(lang, "最初にクリックする場所", "First click for first-time setup")),
		html.EscapeString(t(lang, "初回セットアップでは Guided Setup を最優先にしてください。Setup Status は別フローではなく、現在の状態確認と再開前の見直し用です。", "For first-time setup, Guided Setup is the primary path. Setup Status is not a separate flow; use it to review the current state before resuming.")),
		html.EscapeString(t(lang, "Primary path", "Primary path")),

		// Guided Setup card
		html.EscapeString(t(lang, "Guided Setup（最初はこちら）", "Guided Setup (Start here)")),
		html.EscapeString(t(lang, "初回セットアップで最もわかりやすい進み方です。何から始めるべきか迷ったら、このカードから進めてください。", "This is the clearest path for first-time setup. If you are unsure where to begin, start from this card.")),
		html.EscapeString(t(lang, "Kitsu 接続 → Discord Bot → Project Setup の順に進む", "Follows Kitsu → Discord Bot → Project Setup in order")),
		html.EscapeString(t(lang, "各ステップで次にやることが表示される", "Each step shows what to do next")),
		html.EscapeString(t(lang, "前のステップが完了してから次に進める", "You move forward after the previous step is complete")),
		withLang("/bot/setup-wizard?mode=guided", r),
		html.EscapeString(t(lang, "Guided Setup を開始", "Start Guided Setup")),

		// Setup Status card
		html.EscapeString(t(lang, "Setup Status（状態確認・見直し用）", "Setup Status (Review readiness)")),
		html.EscapeString(t(lang, "現在のセットアップ状況と準備状態を確認する画面です。初回導入の開始場所ではなく、状態確認や再開前の見直しに使います。", "Use this page to review current setup status and readiness. It is not the starting point for first-time setup.")),
		html.EscapeString(t(lang, "8つの完了条件を一覧で確認できる", "Review all 8 completion conditions at a glance")),
		html.EscapeString(t(lang, "どこまで完了していて何が未完了かを見直せる", "Shows what is already ready and what is still incomplete")),
		html.EscapeString(t(lang, "セットアップ処理は行わず、必要なら Guided Setup や Manual Setup に進む", "Does not perform setup actions; continue to Guided Setup or Manual Setup if needed")),
		withLang("/bot/setup-wizard?mode=quick", r),
		html.EscapeString(t(lang, "Setup Status を確認", "Review Setup Status")),
		html.EscapeString(t(lang, "Advanced / Troubleshooting", "Advanced / Troubleshooting")),
		html.EscapeString(t(lang, "初回セットアップの標準導線ではありません。手動修正や深い診断が必要な場合だけ使ってください。", "These are not the default first-time setup paths. Use them only when you need manual fixes or deeper diagnostics.")),
		html.EscapeString(t(lang, "Optional", "Optional")),
		withLang("/bot/admin/setup", r),
		html.EscapeString(t(lang, "Manual Setup を開く", "Open Manual Setup")),
		withLang("/bot/admin/diagnostics", r),
		html.EscapeString(t(lang, "Diagnostics を開く", "Open Diagnostics")),
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
