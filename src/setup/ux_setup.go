package setup

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

type SetupStatus string

const (
	SetupOK       SetupStatus = "ok"
	SetupWarn     SetupStatus = "warn"
	SetupError    SetupStatus = "error"
	SetupUnknown  SetupStatus = "unknown"
)

type SetupCheck struct {
	Key     string      `json:"key"`
	Label   string      `json:"label"`
	Status  SetupStatus `json:"status"`
	Summary string      `json:"summary"`
	Detail  string      `json:"detail,omitempty"`
	Fix     string      `json:"fix,omitempty"`
	Raw     string      `json:"raw,omitempty"`
}

type ProjectSetupStatus struct {
	ProjectID       string      `json:"project_id"`
	ProjectName     string      `json:"project_name"`
	GuildID         string      `json:"guild_id,omitempty"`
	GuildStatus     SetupStatus `json:"guild_status"`
	PermissionStatus SetupStatus `json:"permission_status"`
	WebhookStatus   SetupStatus `json:"webhook_status"`
	ChannelCount    int         `json:"channel_count"`
	WebhookCount    int         `json:"webhook_count"`
	Summary         string      `json:"summary"`
	Raw             string      `json:"raw,omitempty"`
}

type SetupDiagnostics struct {
	Timestamp               time.Time           `json:"timestamp"`
	Env                     []SetupCheck         `json:"env"`
	Kitsu                   SetupCheck          `json:"kitsu"`
	Discord                 SetupCheck          `json:"discord"`
	Projects                []ProjectSetupStatus `json:"projects"`
	TestNotification        SetupCheck          `json:"test_notification"`
	ProjectSetupApplied     bool                `json:"project_setup_applied"`
	NotificationVerified    bool                `json:"notification_verified"`
	SetupComplete           bool                `json:"setup_complete"`
	NextAction              string              `json:"next_action"`
	Warnings                []string            `json:"warnings"`
	AppliedProjectID        string              `json:"applied_project_id,omitempty"`
	AppliedProjectName      string              `json:"applied_project_name,omitempty"`
	VerifiedProjectID       string              `json:"verified_project_id,omitempty"`
	VerifiedProjectName     string              `json:"verified_project_name,omitempty"`
}

func localizeSetupDiagnostics(lang string, diag SetupDiagnostics) SetupDiagnostics {
	localizeCheck := func(c SetupCheck) SetupCheck {
		switch c.Key {
		case "kitsu_runtime_email":
			c.Label = t(lang, "Kitsu 実行用メール", "Kitsu runtime email")
		case "kitsu_runtime_password":
			c.Label = t(lang, "Kitsu 実行用パスワード", "Kitsu runtime password")
		case "kitsu_hostname":
			c.Label = t(lang, "Kitsu ホスト名", "Kitsu hostname")
		case "discord_bot_token":
			c.Label = t(lang, "Discord Bot トークン", "Discord bot token")
		case "discord_guild_id":
			c.Label = t(lang, "Discord Guild フォールバック", "Discord guild fallback")
		case "kitsu":
			c.Label = t(lang, "Kitsu 接続", "Kitsu connection")
		case "discord":
			c.Label = t(lang, "Discord 接続", "Discord connection")
		case "test_notification":
			c.Label = t(lang, "テスト通知", "Test notification")
		}
		switch c.Summary {
		case "Configured":
			c.Summary = t(lang, "設定済み", "Configured")
		case "Missing":
			c.Summary = t(lang, "未設定", "Missing")
		case "Not set":
			c.Summary = t(lang, "未設定", "Not set")
		case "Unknown":
			c.Summary = t(lang, "不明", "Unknown")
		case "Failed":
			c.Summary = t(lang, "失敗", "Failed")
		case "Ready":
			c.Summary = t(lang, "準備完了", "Ready")
		case "Partial":
			c.Summary = t(lang, "一部未完了", "Partial")
		case "Reachable":
			c.Summary = t(lang, "到達可能", "Reachable")
		case "Authenticated":
			c.Summary = t(lang, "認証済み", "Authenticated")
		case "Not sent yet":
			c.Summary = t(lang, "未送信", "Not sent yet")
		case "Delivered":
			c.Summary = t(lang, "送信成功", "Delivered")
		}
		return c
	}
	for i := range diag.Env {
		diag.Env[i] = localizeCheck(diag.Env[i])
	}
	diag.Kitsu = localizeCheck(diag.Kitsu)
	diag.Discord = localizeCheck(diag.Discord)
	diag.TestNotification = localizeCheck(diag.TestNotification)
	for i := range diag.Projects {
		switch diag.Projects[i].Summary {
		case "Ready":
			diag.Projects[i].Summary = t(lang, "準備完了", "Ready")
		case "Partial":
			diag.Projects[i].Summary = t(lang, "一部未完了", "Partial")
		case "Not ready":
			diag.Projects[i].Summary = t(lang, "未準備", "Not ready")
		}
	}
	switch diag.NextAction {
	case "Setup is complete.":
		diag.NextAction = t(lang, "セットアップは完了しています。", "Setup is complete.")
	case "Fix the Kitsu connection first.":
		diag.NextAction = t(lang, "先に Kitsu 接続を修正してください。", "Fix the Kitsu connection first.")
	case "Fix the Discord bot connection first.":
		diag.NextAction = t(lang, "先に Discord Bot 接続を修正してください。", "Fix the Discord bot connection first.")
	case "Assign a guild ID and make sure the bot can create channels and webhooks.":
		diag.NextAction = t(lang, "Guild ID を割り当て、Bot にチャンネル/Webhook作成権限があることを確認してください。", "Assign a guild ID and make sure the bot can create channels and webhooks.")
	}
	return diag
}

const (
	setupProjectAppliedKey           = "setup.project_setup_applied"
	setupProjectAppliedProjectKey    = "setup.project_setup_project_id"
	setupProjectAppliedAtKey         = "setup.project_setup_applied_at"
	setupTestNotificationVerifiedKey = "setup.test_notification_verified"
	setupTestNotificationProjectKey  = "setup.test_notification_project_id"
	setupTestNotificationAtKey       = "setup.test_notification_verified_at"
)

func BuildSetupDiagnostics(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) SetupDiagnostics {
	kitsuHost, botToken, guildID, webhookURL := refreshCreds()
	diag := SetupDiagnostics{Timestamp: time.Now()}

	diag.Env = buildEnvChecks(db, kitsuHost, botToken, guildID)
	diag.Kitsu = buildKitsuCheck(kitsuHost)
	diag.Discord = buildDiscordCheck(botToken, guildID)
	diag.Projects = buildProjectChecks(db, botToken, guildID)
	diag.TestNotification = buildTestNotificationCheck(db)
	diag.ProjectSetupApplied = strings.EqualFold(strings.TrimSpace(model.GetSetting(db, setupProjectAppliedKey)), "true")
	diag.AppliedProjectID = strings.TrimSpace(model.GetSetting(db, setupProjectAppliedProjectKey))
	if diag.AppliedProjectID != "" {
		if p := model.FindProjectByKitsuID(db, diag.AppliedProjectID); p != nil {
			diag.AppliedProjectName = p.Name
		}
	}
	diag.NotificationVerified = diag.TestNotification.Status == SetupOK
	diag.VerifiedProjectID = strings.TrimSpace(model.GetSetting(db, setupTestNotificationProjectKey))
	if diag.VerifiedProjectID != "" {
		if p := model.FindProjectByKitsuID(db, diag.VerifiedProjectID); p != nil {
			diag.VerifiedProjectName = p.Name
		}
	}

	if strings.TrimSpace(webhookURL) != "" {
		diag.Warnings = append(diag.Warnings, "Fallback webhook is configured; it is only used for unrouted notifications.")
	}

	diag.SetupComplete = isSetupComplete(diag)
	diag.NextAction = nextActionForDiagnostics(diag)
	return diag
}

func buildEnvChecks(db *gorm.DB, kitsuHost, botToken, guildID string) []SetupCheck {
	var checks []SetupCheck
	email := storedRuntimeKitsuEmail(db)
	password := strings.TrimSpace(os.Getenv(RuntimeKitsuPasswordEnv))
	if email != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_email", Label: "Kitsu runtime email", Status: SetupOK, Summary: "Configured", Detail: email})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_email", Label: "Kitsu runtime email", Status: SetupError, Summary: "Missing", Fix: "Set KITSU_RUNTIME_EMAIL or use /bot/admin/bot."})
	}
	if password != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_password", Label: "Kitsu runtime password", Status: SetupOK, Summary: "Configured", Detail: "hidden"})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_password", Label: "Kitsu runtime password", Status: SetupError, Summary: "Missing", Fix: "Set KITSU_RUNTIME_PASSWORD or use /bot/admin/bot."})
	}
	if strings.TrimSpace(kitsuHost) != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_hostname", Label: "Kitsu hostname", Status: SetupOK, Summary: "Configured", Detail: strings.TrimSpace(kitsuHost)})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_hostname", Label: "Kitsu hostname", Status: SetupError, Summary: "Missing", Fix: "Set KITSU_HOSTNAME in .env."})
	}
	if strings.TrimSpace(botToken) != "" {
		checks = append(checks, SetupCheck{Key: "discord_bot_token", Label: "Discord bot token", Status: SetupOK, Summary: "Configured", Detail: "hidden"})
	} else {
		checks = append(checks, SetupCheck{Key: "discord_bot_token", Label: "Discord bot token", Status: SetupError, Summary: "Missing", Fix: "Set DISCORD_BOT_TOKEN in .env."})
	}
	if strings.TrimSpace(guildID) != "" {
		checks = append(checks, SetupCheck{Key: "discord_guild_id", Label: "Discord guild fallback", Status: SetupWarn, Summary: "Configured", Detail: strings.TrimSpace(guildID), Fix: "Per-project guilds are preferred; fallback guild is only a compatibility default."})
	} else {
		checks = append(checks, SetupCheck{Key: "discord_guild_id", Label: "Discord guild fallback", Status: SetupWarn, Summary: "Not set", Fix: "Assign guild IDs per project in /bot/admin/projects."})
	}
	return checks
}

func buildKitsuCheck(kitsuHost string) SetupCheck {
	info := checkKitsuStatus(kitsuHost)
	check := SetupCheck{Key: "kitsu", Label: "Kitsu connection", Summary: "Unknown"}
	switch {
	case info.Authenticated:
		check.Status = SetupOK
		check.Summary = "Authenticated"
		check.Detail = "Kitsu API reachable and session token valid."
	case info.Reachable:
		check.Status = SetupWarn
		check.Summary = "Reachable"
		check.Detail = "Kitsu server answered, but authentication is not complete."
		if info.Error != nil {
			check.Fix = *info.Error
		}
	case info.Error != nil:
		check.Status = SetupError
		check.Summary = "Failed"
		check.Detail = *info.Error
		check.Fix = "Verify KITSU_HOSTNAME and runtime credentials."
	default:
		check.Status = SetupUnknown
		check.Summary = "Unknown"
	}
	check.Raw = mustJSON(info)
	return check
}

func buildDiscordCheck(botToken, guildID string) SetupCheck {
	info := checkDiscordStatus(botToken, guildID)
	check := SetupCheck{Key: "discord", Label: "Discord connection", Summary: "Unknown"}
	switch {
	case info.BotValid && info.GuildValid && info.Permissions.ManageChannels && info.Permissions.ManageWebhooks:
		check.Status = SetupOK
		check.Summary = "Ready"
		check.Detail = fmt.Sprintf("Bot: %s / Guild: %s", fallbackOrText(info.BotName, "Bot"), fallbackOrText(info.GuildName, guildID))
	case info.BotValid && info.GuildValid:
		check.Status = SetupWarn
		check.Summary = "Partial"
		check.Detail = "Bot and guild are reachable, but required permissions are missing."
		check.Fix = "Grant Manage Channels and Manage Webhooks to the bot role."
	case info.Error != nil:
		check.Status = SetupError
		check.Summary = "Failed"
		check.Detail = *info.Error
		check.Fix = "Verify the bot token, guild ID, and Discord invite."
	default:
		check.Status = SetupUnknown
		check.Summary = "Unknown"
	}
	check.Raw = mustJSON(info)
	return check
}

func buildProjectChecks(db *gorm.DB, botToken, fallbackGuildID string) []ProjectSetupStatus {
	projects := model.ListProjects(db)
	out := make([]ProjectSetupStatus, 0, len(projects))
	for _, p := range projects {
		guildID := strings.TrimSpace(p.DiscordGuildID)
		if guildID == "" {
			guildID = strings.TrimSpace(fallbackGuildID)
		}
		discordInfo := checkDiscordStatus(botToken, guildID)
		webhooks := model.ListProjectWebhooks(db, p.KitsuProjectID)
		channelCount := 0
		webhookCount := 0
		for _, wh := range webhooks {
			if wh.DiscordChannelID != "" {
				channelCount++
			}
			if strings.TrimSpace(wh.WebhookURL) != "" {
				webhookCount++
			}
		}

		guildStatus := SetupError
		permissionStatus := SetupError
		webhookStatus := SetupError
		if guildID == "" {
			guildStatus = SetupWarn
		} else if discordInfo.GuildValid {
			guildStatus = SetupOK
		}
		if discordInfo.Permissions.ManageChannels && discordInfo.Permissions.ManageWebhooks {
			permissionStatus = SetupOK
		} else if discordInfo.GuildValid {
			permissionStatus = SetupWarn
		}
		if webhookCount > 0 {
			webhookStatus = SetupOK
		} else if channelCount > 0 {
			webhookStatus = SetupWarn
		}

		summary := "Not ready"
		if guildStatus == SetupOK && permissionStatus == SetupOK && webhookStatus == SetupOK {
			summary = "Ready"
		} else if guildStatus == SetupOK || permissionStatus == SetupOK || webhookStatus == SetupOK {
			summary = "Partial"
		}

		out = append(out, ProjectSetupStatus{
			ProjectID:        p.KitsuProjectID,
			ProjectName:      p.Name,
			GuildID:          guildID,
			GuildStatus:      guildStatus,
			PermissionStatus: permissionStatus,
			WebhookStatus:    webhookStatus,
			ChannelCount:     channelCount,
			WebhookCount:     webhookCount,
			Summary:          summary,
			Raw:              mustJSON(discordInfo),
		})
	}
	return out
}

func buildTestNotificationCheck(db *gorm.DB) SetupCheck {
	verified := strings.EqualFold(strings.TrimSpace(model.GetSetting(db, setupTestNotificationVerifiedKey)), "true")
	projectID := strings.TrimSpace(model.GetSetting(db, setupTestNotificationProjectKey))
	verifiedAt := strings.TrimSpace(model.GetSetting(db, setupTestNotificationAtKey))
	check := SetupCheck{
		Key:     "test_notification",
		Label:   "Test notification",
		Summary: "Not sent yet",
		Detail:  "Run the test notification once to confirm the project webhook can receive messages.",
		Fix:     "Use Health or diagnostics to verify test notification delivery.",
	}
	if verified {
		check.Status = SetupOK
		check.Summary = "Delivered"
		check.Detail = "A test notification was delivered successfully."
		if projectID != "" {
			check.Detail += " Project: " + projectID
		}
		if verifiedAt != "" {
			check.Detail += " At: " + verifiedAt
		}
	}
	return check
}

func isSetupComplete(diag SetupDiagnostics) bool {
	for _, c := range diag.Env {
		if c.Status == SetupError {
			return false
		}
	}
	if diag.Kitsu.Status != SetupOK {
		return false
	}
	if diag.Discord.Status != SetupOK {
		return false
	}
	projectReady := false
	for _, p := range diag.Projects {
		if p.GuildStatus == SetupOK && p.PermissionStatus == SetupOK && p.WebhookStatus == SetupOK {
			projectReady = true
			break
		}
	}
	if !projectReady {
		return false
	}
	return diag.TestNotification.Status == SetupOK
}

func nextActionForDiagnostics(diag SetupDiagnostics) string {
	for _, c := range diag.Env {
		if c.Status == SetupError {
			return c.Fix
		}
	}
	if diag.Kitsu.Status != SetupOK {
		if diag.Kitsu.Fix != "" {
			return diag.Kitsu.Fix
		}
		return "Fix the Kitsu connection first."
	}
	if diag.Discord.Status != SetupOK {
		if diag.Discord.Fix != "" {
			return diag.Discord.Fix
		}
		return "Fix the Discord bot connection first."
	}
	for _, p := range diag.Projects {
		if p.GuildStatus != SetupOK || p.PermissionStatus != SetupOK || p.WebhookStatus != SetupOK {
			return "Assign a guild ID and make sure the bot can create channels and webhooks."
		}
	}
	if diag.TestNotification.Status != SetupOK {
		return diag.TestNotification.Fix
	}
	return "Setup is complete."
}

// incompleteReasons returns a list of all conditions blocking SetupComplete.
func incompleteReasons(diag SetupDiagnostics) []string {
	var reasons []string
	for _, c := range diag.Env {
		if c.Status == SetupError {
			reasons = append(reasons, c.Label+": "+firstNonEmpty(c.Fix, c.Summary))
		}
	}
	if diag.Kitsu.Status != SetupOK {
		reasons = append(reasons, "Kitsu: "+firstNonEmpty(diag.Kitsu.Fix, diag.Kitsu.Detail, "Fix the Kitsu connection."))
	}
	if diag.Discord.Status != SetupOK {
		reasons = append(reasons, "Discord: "+firstNonEmpty(diag.Discord.Fix, diag.Discord.Detail, "Fix the Discord bot connection."))
	}
	projectReady := false
	for _, p := range diag.Projects {
		if p.GuildStatus == SetupOK && p.PermissionStatus == SetupOK && p.WebhookStatus == SetupOK {
			projectReady = true
			break
		}
	}
	if !projectReady {
		reasons = append(reasons, "Project: Assign a guild and make sure the bot can create channels and webhooks.")
	}
	if diag.TestNotification.Status != SetupOK {
		reasons = append(reasons, "Test notification: "+firstNonEmpty(diag.TestNotification.Fix, "Send a test notification to verify delivery."))
	}
	return reasons
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func fallbackOrText(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}

func statusBadge(status SetupStatus) string {
	switch status {
	case SetupOK:
		return "ok"
	case SetupWarn:
		return "warn"
	case SetupError:
		return "error"
	default:
		return "unknown"
	}
}

func renderCheckCard(lang string, c SetupCheck) string {
	return fmt.Sprintf(`
<div class="setup-card %s">
  <div class="setup-card-head">
    <div>
      <h3>%s</h3>
      <p class="hint">%s</p>
    </div>
    <span class="pill">%s</span>
  </div>
  <p>%s</p>
  %s
  %s
  <details><summary>%s</summary><pre>%s</pre></details>
</div>`,
		statusBadge(c.Status),
		html.EscapeString(c.Label),
		html.EscapeString(c.Key),
		html.EscapeString(strings.ToUpper(string(c.Status))),
		html.EscapeString(c.Summary),
		renderDetailLine(lang, t(lang, "詳細", "Detail"), c.Detail),
		renderDetailLine(lang, t(lang, "対処", "Fix"), c.Fix),
		html.EscapeString(t(lang, "生データ", "Raw details")),
		html.EscapeString(c.Raw),
	)
}

func renderProjectCard(lang string, p ProjectSetupStatus) string {
	return fmt.Sprintf(`
<div class="setup-card %s">
  <div class="setup-card-head">
    <div>
      <h3>%s</h3>
      <p class="hint">%s: <code>%s</code></p>
    </div>
    <span class="pill">%s</span>
  </div>
  <div class="project-grid">
    <div><strong>%s</strong><div>%s</div></div>
    <div><strong>%s</strong><div>%d</div></div>
    <div><strong>%s</strong><div>%d</div></div>
    <div><strong>%s</strong><div>%s</div></div>
  </div>
  <p>%s</p>
  <details><summary>%s</summary><pre>%s</pre></details>
</div>`,
		statusBadge(projectOverallStatus(p)),
		html.EscapeString(p.ProjectName),
		html.EscapeString(t(lang, "Project ID", "Project ID")),
		html.EscapeString(p.ProjectID),
		html.EscapeString(strings.ToUpper(p.Summary)),
		html.EscapeString(t(lang, "Guild", "Guild")),
		html.EscapeString(fallbackOrText(p.GuildID, t(lang, "未割り当て", "Not assigned"))),
		html.EscapeString(t(lang, "チャンネル", "Channels")),
		p.ChannelCount,
		html.EscapeString(t(lang, "Webhook", "Webhooks")),
		p.WebhookCount,
		html.EscapeString(t(lang, "権限", "Permissions")),
		html.EscapeString(strings.ToUpper(string(p.PermissionStatus))),
		html.EscapeString(projectSummary(lang, p)),
		html.EscapeString(t(lang, "生データ", "Raw details")),
		html.EscapeString(p.Raw),
	)
}

func projectOverallStatus(p ProjectSetupStatus) SetupStatus {
	if p.GuildStatus == SetupOK && p.PermissionStatus == SetupOK && p.WebhookStatus == SetupOK {
		return SetupOK
	}
	if p.GuildStatus == SetupError || p.PermissionStatus == SetupError || p.WebhookStatus == SetupError {
		return SetupError
	}
	if p.GuildStatus == SetupWarn || p.PermissionStatus == SetupWarn || p.WebhookStatus == SetupWarn {
		return SetupWarn
	}
	return SetupUnknown
}

func projectSummary(lang string, p ProjectSetupStatus) string {
	if p.GuildStatus == SetupOK && p.PermissionStatus == SetupOK && p.WebhookStatus == SetupOK {
		return t(lang, "このプロジェクトはテスト通知の準備ができています。", "This project is ready for test notification.")
	}
	if p.GuildStatus != SetupOK {
		return t(lang, "このプロジェクトに Discord Guild を割り当ててください。", "Assign a Discord guild to this project.")
	}
	if p.PermissionStatus != SetupOK {
		return t(lang, "Manage Channels と Manage Webhooks 権限を付与してください。", "Grant Manage Channels and Manage Webhooks.")
	}
	if p.WebhookStatus != SetupOK {
		return t(lang, "このプロジェクトに少なくとも1つのチャンネルと webhook を作成してください。", "Create at least one channel and webhook for this project.")
	}
	return t(lang, "プロジェクト設定を見直してください。", "Review project setup.")
}

func renderDetailLine(_ string, label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fmt.Sprintf(`<p><strong>%s:</strong> %s</p>`, html.EscapeString(label), html.EscapeString(value))
}

func renderDiagnosticsSummary(lang string, diag SetupDiagnostics) string {
	var sections []string
	for _, c := range diag.Env {
		sections = append(sections, renderCheckCard(lang, c))
	}
	sections = append(sections, renderCheckCard(lang, diag.Kitsu))
	sections = append(sections, renderCheckCard(lang, diag.Discord))
	sections = append(sections, renderCheckCard(lang, diag.TestNotification))
	for _, p := range diag.Projects {
		sections = append(sections, renderProjectCard(lang, p))
	}
	return strings.Join(sections, "")
}

func renderGuidedOverview(lang string, diag SetupDiagnostics) string {
	items := make([]string, 0, 8)
	for _, c := range diag.Env {
		items = append(items, fmt.Sprintf(
			`<div class="wizard-check %s"><div class="wizard-check-head"><strong>%s</strong><span class="pill">%s</span></div><p>%s</p></div>`,
			statusBadge(c.Status),
			html.EscapeString(c.Label),
			html.EscapeString(c.Summary),
			html.EscapeString(firstNonEmpty(c.Fix, c.Detail, c.Summary)),
		))
	}
	items = append(items, fmt.Sprintf(
		`<div class="wizard-check %s"><div class="wizard-check-head"><strong>%s</strong><span class="pill">%s</span></div><p>%s</p></div>`,
		statusBadge(diag.Kitsu.Status),
		html.EscapeString(diag.Kitsu.Label),
		html.EscapeString(diag.Kitsu.Summary),
		html.EscapeString(firstNonEmpty(diag.Kitsu.Fix, diag.Kitsu.Detail, diag.Kitsu.Summary)),
	))
	items = append(items, fmt.Sprintf(
		`<div class="wizard-check %s"><div class="wizard-check-head"><strong>%s</strong><span class="pill">%s</span></div><p>%s</p></div>`,
		statusBadge(diag.Discord.Status),
		html.EscapeString(diag.Discord.Label),
		html.EscapeString(diag.Discord.Summary),
		html.EscapeString(firstNonEmpty(diag.Discord.Fix, diag.Discord.Detail, diag.Discord.Summary)),
	))
	for _, p := range diag.Projects {
		items = append(items, fmt.Sprintf(
			`<div class="wizard-check %s"><div class="wizard-check-head"><strong>%s</strong><span class="pill">%s</span></div><p>%s</p></div>`,
			statusBadge(projectOverallStatus(p)),
			html.EscapeString(p.ProjectName),
			html.EscapeString(p.Summary),
			html.EscapeString(projectSummary(lang, p)),
		))
	}
	items = append(items, fmt.Sprintf(
		`<div class="wizard-check %s"><div class="wizard-check-head"><strong>%s</strong><span class="pill">%s</span></div><p>%s</p></div>`,
		statusBadge(diag.TestNotification.Status),
		html.EscapeString(diag.TestNotification.Label),
		html.EscapeString(diag.TestNotification.Summary),
		html.EscapeString(firstNonEmpty(diag.TestNotification.Fix, diag.TestNotification.Detail, diag.TestNotification.Summary)),
	))
	return `<div class="wizard-check-grid">` + strings.Join(items, "") + `</div>`
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func SetupManualHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if r.URL.Path != "" {
			fmt.Fprint(w, renderSetupCompatHandoffPage(
				lang,
				r,
				t(lang, "Manual Setup has moved", "Manual Setup has moved"),
				t(lang, "/bot/admin/setup は互換用の案内ページです。Setup flow has moved to Project Management.", "/bot/admin/setup is now a compatibility handoff page. Setup flow has moved to Project Management."),
			))
			return
		}
		diag := localizeSetupDiagnostics(lang, BuildSetupDiagnostics(db, refreshCreds))

		var envCards strings.Builder
		for _, c := range diag.Env {
			envCards.WriteString(renderCheckCard(lang, c))
		}

		var projectCards strings.Builder
		var projectOptions strings.Builder
		for _, p := range diag.Projects {
			projectCards.WriteString(renderProjectCard(lang, p))
			projectOptions.WriteString(fmt.Sprintf(`<option value="%s">%s</option>`, html.EscapeString(p.ProjectID), html.EscapeString(p.ProjectName)))
		}
		if projectCards.Len() == 0 {
			projectCards.WriteString(`<div class="setup-card warn"><h3>` + esc(t(lang, "プロジェクト未設定", "No projects yet")) + `</h3><p>` + esc(t(lang, "先に Guided Setup を進めてから戻ってきてください。", "Run Guided Setup first, then come back here.")) + `</p></div>`)
			projectOptions.WriteString(`<option value="">` + esc(t(lang, "プロジェクト未設定", "No projects yet")) + `</option>`)
		}

		bannerClass := "warn"
		if diag.SetupComplete {
			bannerClass = "ok"
		} else if diag.Kitsu.Status == SetupError || diag.Discord.Status == SetupError {
			bannerClass = "error"
		} else {
			for _, c := range diag.Env {
				if c.Status == SetupError {
					bannerClass = "error"
					break
				}
			}
		}

		body := fmt.Sprintf(`
<style>
.setup-banner{padding:16px 18px;border-radius:14px;margin-bottom:18px;font-weight:700}
.setup-banner.ok{background:rgba(142,207,139,.14);border:1px solid rgba(142,207,139,.35);color:#8ecf8b}
.setup-banner.warn{background:rgba(255,200,80,.12);border:1px solid rgba(255,200,80,.3);color:#ffc850}
.setup-banner.error{background:rgba(255,106,80,.12);border:1px solid rgba(255,106,80,.34);color:#ff6a50}
.setup-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px}
.setup-card{padding:16px;border-radius:16px;border:1px solid var(--line);background:rgba(255,255,255,.03)}
.setup-card.ok{border-color:rgba(142,207,139,.35)}
.setup-card.warn{border-color:rgba(255,200,80,.28)}
.setup-card.error{border-color:rgba(255,106,80,.34)}
.setup-card-head{display:flex;justify-content:space-between;gap:10px;align-items:flex-start;margin-bottom:10px}
.setup-card h3{margin:0}
.pill{padding:4px 10px;border-radius:999px;background:rgba(255,255,255,.08);font-size:.75rem;font-weight:700}
.project-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin:12px 0}
.project-grid strong{display:block;color:var(--muted-2);font-size:.74rem;text-transform:uppercase;letter-spacing:.08em}
.setup-topbar{display:flex;gap:12px;align-items:center;justify-content:space-between;flex-wrap:wrap}
.setup-note{color:var(--muted);margin-top:8px}
</style>
<div class="section-card glass">
  <div class="setup-topbar">
    <div>
      <h2 style="margin:0">%s</h2>
      <p class="setup-note">%s</p>
    </div>
    <div class="setup-banner %s">%s</div>
  </div>
  <p class="hint">%s</p>
</div>
<div class="section-stack">
  <div class="section-card glass"><h3>%s</h3><div class="setup-grid">%s</div></div>
  <div class="section-card glass"><h3>%s</h3><div class="setup-grid">%s%s</div></div>
  <div class="section-card glass">
    <h3>%s</h3>
    <p class="hint">%s</p>
    <form id="manualTestNotification" class="guided-form" data-json="test-notification" method="post" action="%s">
      <div><label>%s</label><select name="project_id">%s</select></div>
      <div><label>%s</label><input name="message" value="KitsuSync test notification"></div>
      <div class="guided-actions"><button class="btn" type="submit">%s</button></div>
    </form>
  </div>
  <div class="section-card glass"><h3>%s</h3><div class="section-stack">%s</div></div>
  <div class="section-card glass"><h3>%s</h3><p class="hint">%s</p><div class="button-row"><a class="btn" href="%s">%s</a><a class="btn-ghost" href="%s">%s</a></div></div>
</div>`,
			esc(t(lang, "Manual Setup", "Manual Setup")),
			esc(diag.NextAction),
			bannerClass,
			esc(strings.ToUpper(func() string {
				if diag.SetupComplete {
					return "complete"
				}
				return "incomplete"
			}())),
			esc(t(lang, "このページではセットアップ全体の状態を確認し、不足している項目だけを修正できます。", "Use this page to see the full setup state, retry checks, and fix only the missing parts.")),
			esc(t(lang, "環境設定", "Environment")),
			envCards.String(),
			esc(t(lang, "接続状態", "Connection")),
			renderCheckCard(lang, diag.Kitsu),
			renderCheckCard(lang, diag.Discord),
			esc(t(lang, "テスト通知", "Test Notification")),
			esc(t(lang, "実際に1通 Discord へ送信して到達確認します。", "Send one real Discord message to verify delivery.")),
			withLang("/api/setup/test-notification", r),
			esc(t(lang, "プロジェクト", "Project")),
			projectOptions.String(),
			esc(t(lang, "メッセージ", "Message")),
			esc(t(lang, "テスト通知を送信", "Send Test Notification")),
			esc(t(lang, "プロジェクトとGuild", "Projects & Guilds")),
			projectCards.String(),
			esc(t(lang, "高度設定 / 任意", "Advanced / Optional")),
			esc(t(lang, "Channel Mapping、Forum Channel Setup、Checker Mapping、Role Mention、Event Mapping は v0.1.0 では高度設定扱いです。", "Channel Mapping, Forum Channel Setup, Checker Mapping, Role Mention, and Event Mapping remain advanced for v0.1.0.")),
			withLang("/bot/setup", r),
			esc(t(lang, "Project Management を開く", "Open Project Management")),
			withLang("/bot/admin/diagnostics", r),
			esc(t(lang, "Diagnostics を開く", "Open Diagnostics")),
		)
		body += `<script>
document.getElementById('manualTestNotification').addEventListener('submit', async function(ev){
  ev.preventDefault();
  var payload = {};
  new FormData(this).forEach(function(value, key){ payload[key] = value; });
  var res = await fetch(` + fmt.Sprintf("%q", withLang("/api/setup/test-notification", r)) + `, {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(payload)});
  var txt = await res.text();
  alert(txt);
  window.location.reload();
});
</script>`
		fmt.Fprint(w, adminPage(lang, t(lang, "Manual Setup", "Manual Setup"), r, body))
	}
}

func RenderGuidedSetupPage(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string), r *http.Request) string {
	lang := currentLang(r)
	diag := localizeSetupDiagnostics(lang, BuildSetupDiagnostics(db, refreshCreds))
	kitsuHost, _, _, _ := refreshCreds()
	autoKitsuHost := publicKitsuHostnameFromRequest(r, kitsuHost)
	stepTitle := t(lang, "ようこそ", "Welcome")
	stepBody := t(lang, "上から順に進めれば、何をしているかと次に何をすべきかが分かるようにしています。", "Start from the top. Each step explains why it exists and what to do next.")

	projectOptions := make([]string, 0, len(diag.Projects))
	for _, p := range diag.Projects {
		projectOptions = append(projectOptions, fmt.Sprintf(`<option value="%s">%s</option>`, html.EscapeString(p.ProjectID), html.EscapeString(p.ProjectName)))
	}
	projectSelect := strings.Join(projectOptions, "")
	if projectSelect == "" {
		projectSelect = `<option value="">No projects yet</option>`
	}

	body := fmt.Sprintf(`
<style>
.guided-shell{display:grid;grid-template-columns:280px minmax(0,1fr);gap:18px}
.guided-nav,.guided-main{display:flex;flex-direction:column;gap:14px}
.step-card,.guided-step{padding:16px;border-radius:16px;border:1px solid var(--line);background:rgba(255,255,255,.03)}
.guided-step.active{border-color:rgba(255,141,72,.45);background:rgba(255,141,72,.06)}
.guided-step.done{border-color:rgba(142,207,139,.35);background:rgba(142,207,139,.05)}
.step-title{display:flex;justify-content:space-between;gap:10px;align-items:center;margin-bottom:10px}
.step-title h3{margin:0}
.step-list{display:flex;flex-direction:column;gap:8px}
.step-pill{font-size:.75rem;padding:4px 8px;border-radius:999px;background:rgba(255,255,255,.08)}
.guided-form{display:grid;gap:10px;max-width:560px}
.guided-form input,.guided-form select{width:100%%;padding:10px 12px;border-radius:12px;border:1px solid var(--line);background:rgba(255,255,255,.04);color:var(--text)}
.guided-form label{font-size:.78rem;color:var(--muted-2)}
.guided-note{color:var(--muted);font-size:.92rem}
.guided-actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:8px}
.guided-success{color:#8ecf8b;font-weight:700}
.guided-warning{color:#ffc850;font-weight:700}
.wizard-check-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}
.wizard-check{padding:12px;border-radius:14px;border:1px solid var(--line);background:rgba(255,255,255,.02)}
.wizard-check.ok{border-color:rgba(142,207,139,.35)}
.wizard-check.warn{border-color:rgba(255,200,80,.28)}
.wizard-check.error{border-color:rgba(255,106,80,.34)}
.wizard-check-head{display:flex;justify-content:space-between;gap:8px;align-items:flex-start;margin-bottom:8px}
.wizard-check p{margin:0;color:var(--muted);font-size:.9rem;line-height:1.45}
@media (max-width: 980px){.guided-shell{grid-template-columns:1fr}}
</style>
<div class="section-card glass">
  <h2 style="margin:0 0 8px">%s</h2>
  <p class="guided-note">%s</p>
		<div class="step-pill">%s</div>
	</div>
	<div class="guided-shell">
	  <div class="guided-nav">
	    <div class="step-card">
	      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
	      <div class="step-list">
	        <div class="guided-step active"><strong>1. %s</strong><div class="guided-note">%s</div></div>
	        <div class="guided-step %s"><strong>2. %s</strong><div class="guided-note">%s</div></div>
	        <div class="guided-step %s"><strong>3. %s</strong><div class="guided-note">%s</div></div>
	        <div class="guided-step %s"><strong>4. %s</strong><div class="guided-note">%s</div></div>
	        <div class="guided-step %s"><strong>5. %s</strong><div class="guided-note">%s</div></div>
	        <div class="guided-step %s"><strong>6. %s</strong><div class="guided-note">%s</div></div>
	        <div class="guided-step %s"><strong>7. %s</strong><div class="guided-note">%s</div></div>
	        <div class="guided-step %s"><strong>8. %s</strong><div class="guided-note">%s</div></div>
	      </div>
	    </div>
	  </div>
  <div class="guided-main">
    <div class="guided-step active">
      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
      <p class="guided-note">%s</p>
      <div>
        %s
      </div>
    </div>
    <div class="guided-step">
      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
      <form class="guided-form" method="post" action="%s" data-json="test-kitsu">
        <div><label>%s</label><input name="hostname" value="%s" placeholder="http://kitsu.local/"></div>
        <div><label>%s</label><input name="email" placeholder="bot@studio.com"></div>
        <div><label>%s</label><input name="password" type="password"></div>
        <div class="guided-actions"><button class="btn" type="submit">%s</button></div>
      </form>
    </div>
    <div class="guided-step">
      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
      <form class="guided-form" method="post" action="%s" data-json="test-discord">
        <div><label>%s</label><input name="bot_token" type="password" placeholder="hidden token"></div>
        <div><label>Guild ID</label><input name="guild_id" placeholder="123456789012345678"></div>
        <div class="guided-actions"><button class="btn" type="submit">%s</button></div>
      </form>
    </div>
    <div class="guided-step">
      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
      <form class="guided-form" method="post" action="%s" data-json="apply-project">
        <div><label>%s</label><select name="project_id">%s</select></div>
        <div><label>Guild ID</label><input name="guild_id" placeholder="123456789012345678"></div>
        <div><label>%s</label><input name="template" value="cg"></div>
        <div><label>%s</label><select name="language"><option value="ja">%s</option><option value="en">%s</option></select></div>
        <div class="guided-actions"><button class="btn" type="submit">%s</button></div>
      </form>
    </div>
    <div class="guided-step">
      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
      <p class="guided-note">%s</p>
      <div class="guided-actions"><a class="btn" href="%s">%s</a></div>
    </div>
    <div class="guided-step">
      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
      <form class="guided-form" method="post" action="%s" data-json="test-notification">
        <div><label>%s</label><input name="project_id" placeholder="%s"></div>
        <div><label>%s</label><input name="message" value="KitsuSync test notification"></div>
        <div class="guided-actions"><button class="btn" type="submit">%s</button></div>
      </form>
      <p class="guided-note">%s</p>
    </div>
    <div class="guided-step %s">
      <div class="step-title"><h3>%s</h3><span class="step-pill">%s</span></div>
      <p class="guided-note">%s</p>
      <div class="%s">%s</div>
      <div class="guided-actions">
        <a class="btn" href="%s">%s</a>
        <a class="btn-ghost" href="%s">%s</a>
      </div>
    </div>
  </div>
</div>
<script>
document.querySelectorAll('form[data-json]').forEach(function(form){
  form.addEventListener('submit', async function(ev){
    ev.preventDefault();
    var action = form.getAttribute('data-json');
    var payload = {};
    new FormData(form).forEach(function(value, key){ payload[key] = value; });
    var map = {
      'test-kitsu': '%s',
      'test-discord': '%s',
      'apply-project': '%s',
      'test-notification': '%s'
    };
    var res = await fetch(map[action], {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(payload)});
    var txt = await res.text();
    alert(txt);
    window.location.reload();
  });
});
</script>`,
		esc(stepTitle),
		esc(stepBody),
		esc(diag.NextAction),
		esc(t(lang, "セットアップの流れ", "Setup Flow")),
		esc("Current status: "+func() string { if diag.SetupComplete { return "complete" } ; return "in progress" }()),
		esc(t(lang, "ようこそ", "Welcome")),
		esc(t(lang, "何が起こるかと、なぜ必要かを説明します。", "What will happen and why.")),
		envStepClass(diag.Env),
		esc(t(lang, "システム確認", "System Check")),
		esc(t(lang, "環境変数と起動状態を確認します。", "Validate env and startup state.")),
		stepClassFromStatus(diag.Kitsu.Status),
		esc(t(lang, "Kitsu 接続確認", "Kitsu Connection")),
		esc(t(lang, "Kitsu に到達できるか確認します。", "Confirm Kitsu is reachable.")),
		stepClassFromStatus(diag.Discord.Status),
		esc(t(lang, "Discord Bot 確認", "Discord Bot")),
		esc(t(lang, "トークン、Guild、権限を確認します。", "Validate token, guild, and permissions.")),
		projectStepClass(diag.Projects),
		esc(t(lang, "Project -> Guild", "Project -> Guild")),
		esc(t(lang, "1つの project を 1つの guild に紐付けます。", "Assign one project to one guild.")),
		projectStepClass(diag.Projects),
		esc(t(lang, "テストチャンネル", "Test Channel")),
		esc(t(lang, "project 用のチャンネルと webhook を作成します。", "Create the project channels/webhooks.")),
		stepClassFromStatus(diag.TestNotification.Status),
		esc(t(lang, "テスト通知", "Test Notification")),
		esc(t(lang, "1通送信して到達確認します。", "Send one message to confirm delivery.")),
		stepClassFromBool(diag.SetupComplete),
		esc(t(lang, "完了", "Complete")),
		esc(t(lang, "Guided Setup はここで完了です。", "Guided Setup is done.")),
		esc(t(lang, "システム確認", "System Check")),
		esc(t(lang, "共通診断", "Common Diagnostics")),
		esc(t(lang, "この画面は Manual Setup と同じ診断モデルを使うため、表示される状態は常に一致します。", "This page reuses the same status model as Manual Setup, so you always see the same truth.")),
		renderGuidedOverview(lang, diag),
		esc(t(lang, "Kitsu 接続確認", "Kitsu Connection")),
		esc(t(lang, "Step 3", "Step 3")),
		withLang("/api/setup/test-kitsu", r),
		esc(t(lang, "Kitsu Hostname (auto)", "Kitsu Hostname (auto)")),
		html.EscapeString(autoKitsuHost),
		esc(t(lang, "メールアドレス", "Email")),
		esc(t(lang, "パスワード", "Password")),
		esc(t(lang, "Kitsu 接続テスト", "Test Kitsu")),
		esc(t(lang, "Discord Bot", "Discord Bot")),
		esc(t(lang, "Step 4", "Step 4")),
		withLang("/api/setup/test-discord", r),
		esc(t(lang, "Bot Token", "Bot Token")),
		esc(t(lang, "Discord 接続確認", "Validate Discord")),
		esc(t(lang, "Project -> Guild", "Project -> Guild")),
		esc(t(lang, "Step 5", "Step 5")),
		withLang("/api/setup/apply-project", r),
		esc(t(lang, "プロジェクト", "Project")),
		projectSelect,
		esc(t(lang, "テンプレート", "Template")),
		esc(t(lang, "言語", "Language")),
		esc(t(lang, "日本語", "Japanese")),
		esc(t(lang, "英語", "English")),
		esc(t(lang, "チャンネル作成を実行", "Create Channel Setup")),
		esc(t(lang, "テストチャンネル", "Test Channel")),
		esc(t(lang, "Step 6", "Step 6")),
		esc(t(lang, "作成されたチャンネルや webhook を確認・再実行したい場合は project setup 画面を開いてください。", "Open the direct project setup screen if you want to inspect or retry the created channels and webhooks.")),
		withLang("/bot/setup", r),
		esc(t(lang, "Project Setup を開く", "Open Project Setup")),
		esc(t(lang, "テスト通知", "Test Notification")),
		esc(t(lang, "Step 7", "Step 7")),
		withLang("/api/setup/test-notification", r),
		esc(t(lang, "Project ID", "Project ID")),
		esc(t(lang, "project が1件なら空欄で可", "optional, leave blank if one project")),
		esc(t(lang, "メッセージ", "Message")),
		esc(t(lang, "テスト通知を送信", "Send Test Notification")),
		esc(t(lang, "送信に成功すると diagnostics に記録され、Setup Complete が見えるようになります。", "The test will be marked in diagnostics so Setup Complete becomes visible after a successful delivery.")),
		esc(t(lang, "完了", "Complete")),
		func() string { if diag.SetupComplete { return "done" } ; return "pending" }(),
		esc(func() string { if diag.SetupComplete { return "Setup Complete" } ; return "Not complete yet" }()),
		esc(diag.NextAction),
		func() string { if diag.SetupComplete { return "guided-success" } ; return "guided-warning" }(),
		esc(func() string { if diag.SetupComplete { return "Everything required for the first release is in place." } ; return "Finish the highlighted checks to complete setup." }()),
		withLang("/bot/admin/setup", r),
		esc(t(lang, "Manual Setup を開く", "Open Manual Setup")),
		withLang("/bot/admin", r),
		esc(t(lang, "Admin を開く", "Open Admin")),
		withLang("/api/setup/test-kitsu", r),
		withLang("/api/setup/test-discord", r),
		withLang("/api/setup/apply-project", r),
		withLang("/api/setup/test-notification", r),
	)

	return adminPage(lang, t(lang, "Guided Setup", "Guided Setup"), r, body)
}

func envStepClass(checks []SetupCheck) string {
	for _, c := range checks {
		if c.Status == SetupError {
			return "guided-step"
		}
	}
	return "guided-step done"
}

func stepClassFromStatus(status SetupStatus) string {
	if status == SetupOK {
		return "guided-step done"
	}
	if status == SetupError {
		return "guided-step"
	}
	return "guided-step active"
}

func projectStepClass(projects []ProjectSetupStatus) string {
	if len(projects) == 0 {
		return "guided-step"
	}
	for _, p := range projects {
		if projectOverallStatus(p) == SetupOK {
			return "guided-step done"
		}
	}
	return "guided-step active"
}

func stepClassFromBool(ok bool) string {
	if ok {
		return "guided-step done"
	}
	return "guided-step active"
}
