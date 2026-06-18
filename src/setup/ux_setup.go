package setup

import (
	"encoding/json"
	"fmt"
	"html"
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
			c.Label = t(lang, "最終ヘルス確認", "Final Health Check")
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
		diag.Warnings = append(diag.Warnings, "Legacy fallback webhook is configured; it is only used for unrouted notifications.")
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
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_email", Label: "Kitsu runtime email", Status: SetupError, Summary: "Missing", Fix: "Review Bot Settings and save the runtime email there."})
	}
	if password != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_password", Label: "Kitsu runtime password", Status: SetupOK, Summary: "Configured", Detail: "hidden"})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_password", Label: "Kitsu runtime password", Status: SetupError, Summary: "Missing", Fix: "Review Bot Settings and save the runtime password there."})
	}
	if strings.TrimSpace(kitsuHost) != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_hostname", Label: "Kitsu hostname", Status: SetupOK, Summary: "Configured", Detail: strings.TrimSpace(kitsuHost)})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_hostname", Label: "Kitsu hostname", Status: SetupError, Summary: "Missing", Fix: "Review the Kitsu hostname in Bot Settings."})
	}
	if strings.TrimSpace(botToken) != "" {
		checks = append(checks, SetupCheck{Key: "discord_bot_token", Label: "Discord bot token", Status: SetupOK, Summary: "Configured", Detail: "hidden"})
	} else {
		checks = append(checks, SetupCheck{Key: "discord_bot_token", Label: "Discord bot token", Status: SetupError, Summary: "Missing", Fix: "Review Bot Settings and save the shared bot token there."})
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
		Label:   "Final Health Check",
		Summary: "Not confirmed yet",
		Detail:  "Review Health to confirm the notification destination and runtime status before treating setup as complete.",
		Fix:     "Open Health and confirm the runtime is healthy and project webhook status looks correct.",
	}
	if verified {
		check.Status = SetupOK
		check.Summary = "Confirmed"
		check.Detail = "The final Health confirmation was recorded successfully."
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
		reasons = append(reasons, "Final health check: "+firstNonEmpty(diag.TestNotification.Fix, "Review Health to confirm the runtime and notification destination."))
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
		return t(lang, "このプロジェクトは最終ヘルス確認に進めます。", "This project is ready for the final Health check.")
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
