package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

type SetupStatus string

const (
	SetupOK      SetupStatus = "ok"
	SetupWarn    SetupStatus = "warn"
	SetupError   SetupStatus = "error"
	SetupUnknown SetupStatus = "unknown"
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
	ProjectID        string      `json:"project_id"`
	ProjectName      string      `json:"project_name"`
	GuildID          string      `json:"guild_id,omitempty"`
	GuildStatus      SetupStatus `json:"guild_status"`
	PermissionStatus SetupStatus `json:"permission_status"`
	WebhookStatus    SetupStatus `json:"webhook_status"`
	ChannelCount     int         `json:"channel_count"`
	WebhookCount     int         `json:"webhook_count"`
	Summary          string      `json:"summary"`
	Raw              string      `json:"raw,omitempty"`
}

type SetupDiagnostics struct {
	Timestamp                    time.Time            `json:"timestamp"`
	Env                          []SetupCheck         `json:"env"`
	Kitsu                        SetupCheck           `json:"kitsu"`
	Discord                      SetupCheck           `json:"discord"`
	Projects                     []ProjectSetupStatus `json:"projects"`
	TestNotification             SetupCheck           `json:"test_notification"`
	ProjectSetupApplied          bool                 `json:"project_setup_applied"`
	NotificationVerified         bool                 `json:"notification_verified"`
	SetupComplete                bool                 `json:"setup_complete"`
	ProductionRoutingConfigured  bool                 `json:"production_routing_configured"`
	OverallNotificationReadiness string               `json:"overall_notification_readiness"`
	NextAction                   string               `json:"next_action"`
	Warnings                     []string             `json:"warnings"`
	AppliedProjectID             string               `json:"applied_project_id,omitempty"`
	AppliedProjectName           string               `json:"applied_project_name,omitempty"`
	VerifiedProjectID            string               `json:"verified_project_id,omitempty"`
	VerifiedProjectName          string               `json:"verified_project_name,omitempty"`
}

// SharedBotRuntimeReadiness is the single source of truth used by the Setup
// Wizard, Bot Settings summary, Dashboard, and System Status. Notification
// readiness also requires at least one valid enabled Production route.
type SharedBotRuntimeReadiness struct {
	KitsuConfigured     bool
	RuntimeHealthKnown  bool
	RuntimeKitsuHealthy bool
	DiscordConfigured   bool
	ProductionConnected bool
	RoutingReady        bool
	PrerequisitesReady  bool
	OverallReady        bool
	State               ReadinessState
}

type ReadinessState string

const (
	ReadinessSetupRequired      ReadinessState = "setup_required"
	ReadinessBotSetupRequired   ReadinessState = "bot_setup_required"
	ReadinessProductionRequired ReadinessState = "production_required"
	ReadinessRoutingRequired    ReadinessState = "routing_required"
	ReadinessReady              ReadinessState = "ready"
)

func sharedBotRuntimeReadiness(db *gorm.DB, kitsuHost, botToken string) SharedBotRuntimeReadiness {
	kitsuConfigured := strings.TrimSpace(kitsuHost) != "" &&
		strings.TrimSpace(StoredRuntimeKitsuToken(db)) != ""
	discordConfigured := strings.TrimSpace(botToken) != ""
	productionConnected := len(model.ListProjects(db)) > 0
	routingReady := hasReadyProductionRouting(db)
	prerequisitesReady := kitsuConfigured && discordConfigured
	state := ReadinessSetupRequired
	switch {
	case !kitsuConfigured:
		state = ReadinessSetupRequired
	case !discordConfigured:
		state = ReadinessBotSetupRequired
	case !productionConnected:
		state = ReadinessProductionRequired
	case !routingReady:
		state = ReadinessRoutingRequired
	default:
		state = ReadinessReady
	}
	return SharedBotRuntimeReadiness{
		KitsuConfigured: kitsuConfigured, DiscordConfigured: discordConfigured,
		ProductionConnected: productionConnected, RoutingReady: routingReady,
		PrerequisitesReady: prerequisitesReady, OverallReady: state == ReadinessReady,
		State: state,
	}
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

	diag.Env = buildEnvChecks(db, kitsuHost, botToken, guildID, "en")
	diag.Kitsu = buildKitsuCheck(kitsuHost, "en")
	diag.Discord = buildProjectAwareDiscordCheck(db, botToken, guildID)
	diag.Projects = buildProjectChecks(db, botToken, guildID)
	diag.TestNotification = buildTestNotificationCheck(db, "en")
	diag.ProjectSetupApplied = strings.EqualFold(strings.TrimSpace(model.GetSetting(db, setupProjectAppliedKey)), "true")
	diag.AppliedProjectID = strings.TrimSpace(model.GetSetting(db, setupProjectAppliedProjectKey))
	if diag.AppliedProjectID != "" {
		if p := model.FindProjectByKitsuID(db, diag.AppliedProjectID); p != nil {
			diag.AppliedProjectName = p.Name
		}
	}
	diag.NotificationVerified = diag.TestNotification.Status == SetupOK
	diag.ProductionRoutingConfigured = hasReadyProductionRouting(db)
	diag.VerifiedProjectID = strings.TrimSpace(model.GetSetting(db, setupTestNotificationProjectKey))
	if diag.VerifiedProjectID != "" {
		if p := model.FindProjectByKitsuID(db, diag.VerifiedProjectID); p != nil {
			diag.VerifiedProjectName = p.Name
		}
	}

	if strings.TrimSpace(webhookURL) != "" {
		diag.Warnings = append(diag.Warnings, "Legacy fallback webhook is configured; it is only used for unrouted notifications.")
	}

	diag.OverallNotificationReadiness = "blocked"
	if diag.Kitsu.Status == SetupOK && diag.Discord.Status == SetupOK && diag.ProductionRoutingConfigured {
		diag.OverallNotificationReadiness = "ready_pending_final_verification"
	}
	diag.SetupComplete = isSetupComplete(diag)
	diag.NextAction = nextActionForDiagnostics(diag)
	return diag
}

func buildProjectAwareDiscordCheck(db *gorm.DB, botToken, fallbackGuildID string) SetupCheck {
	for _, project := range model.ListProjects(db) {
		if strings.TrimSpace(project.DiscordGuildID) != "" {
			return buildDiscordCheck(botToken, strings.TrimSpace(project.DiscordGuildID))
		}
	}
	return buildDiscordCheck(botToken, fallbackGuildID)
}

// ProjectDiscordReadiness is the read-only readiness view used by /health.
// It validates the saved Guild for each connected Production and treats stale
// managed resources as blocked routing, without using the fallback Guild.
func ProjectDiscordReadiness(db *gorm.DB, botToken, fallbackGuildID string) (apiValidated, routingReady bool) {
	projects := model.ListProjects(db)
	if len(projects) == 0 {
		apiStatus := buildProjectAwareDiscordStatus(db, botToken, fallbackGuildID)
		return apiStatus.BotValid && apiStatus.GuildValid && apiStatus.Permissions.ManageChannels && apiStatus.Permissions.ManageWebhooks,
			hasReadyProductionRouting(db)
	}

	apiValidated = true
	resourcesHealthy := true
	for _, project := range projects {
		report := inspectProjectDiscordResources(project, db)
		apiValidated = apiValidated && report.GuildValid && report.ManageChannels
		resourcesHealthy = resourcesHealthy && report.Healthy()
	}
	return apiValidated, hasReadyProductionRouting(db) && resourcesHealthy
}

func buildEnvChecks(db *gorm.DB, kitsuHost, botToken, guildID, lang string) []SetupCheck {
	var checks []SetupCheck
	email := storedRuntimeKitsuEmail(db)
	password := strings.TrimSpace(os.Getenv(RuntimeKitsuPasswordEnv))
	credentialState := InspectRuntimeKitsuCredential(db)
	if email != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_email", Label: t(lang, "Kitsu連携アカウント", "Kitsu integration account"), Status: SetupOK, Summary: t(lang, "設定済み", "Configured"), Detail: email})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_email", Label: t(lang, "Kitsu連携アカウント", "Kitsu integration account"), Status: SetupError, Summary: t(lang, "未設定", "Missing"), Fix: t(lang, "接続設定でKitsu連携アカウントを保存してください。", "Save the Kitsu integration account in Connections.")})
	}
	if password != "" || credentialState.Decryptable {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_password", Label: t(lang, "Kitsu連携アカウントのパスワード", "Kitsu integration account password"), Status: SetupOK, Summary: t(lang, "設定済み", "Configured"), Detail: "hidden"})
	} else if credentialState.CiphertextPresent && credentialState.ErrorClass != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_password", Label: t(lang, "Kitsu runtime password", "Kitsu integration account password"), Status: SetupError, Summary: t(lang, "復旧が必要", "Recovery required"), Detail: t(lang, "保存済みのruntime認証情報を復号できません。", "The stored runtime credential could not be decrypted."), Fix: t(lang, "接続設定でKitsuパスワードを再入力してください。", "Re-enter the Kitsu password in Connections.")})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_runtime_password", Label: t(lang, "Kitsu連携アカウントのパスワード", "Kitsu integration account password"), Status: SetupError, Summary: t(lang, "未設定", "Missing"), Fix: t(lang, "接続設定でKitsu連携アカウントのパスワードを保存してください。", "Save the Kitsu integration account password in Connections.")})
	}
	if strings.TrimSpace(kitsuHost) != "" {
		checks = append(checks, SetupCheck{Key: "kitsu_hostname", Label: t(lang, "Kitsuホスト", "Kitsu host"), Status: SetupOK, Summary: t(lang, "設定済み", "Configured"), Detail: strings.TrimSpace(kitsuHost)})
	} else {
		checks = append(checks, SetupCheck{Key: "kitsu_hostname", Label: t(lang, "Kitsuホスト", "Kitsu host"), Status: SetupError, Summary: t(lang, "未設定", "Missing"), Fix: t(lang, "接続設定でKitsuホストを確認してください。", "Review the Kitsu host in Connections.")})
	}
	if strings.TrimSpace(botToken) != "" {
		checks = append(checks, SetupCheck{Key: "discord_bot_token", Label: t(lang, "Discord Bot", "Discord Bot"), Status: SetupOK, Summary: t(lang, "設定済み", "Configured"), Detail: "hidden"})
	} else {
		checks = append(checks, SetupCheck{Key: "discord_bot_token", Label: t(lang, "Discord Bot", "Discord Bot"), Status: SetupError, Summary: t(lang, "未設定", "Missing"), Fix: t(lang, "接続設定でDiscord Botを保存してください。", "Save the Discord Bot in Connections.")})
	}
	if strings.TrimSpace(guildID) != "" {
		checks = append(checks, SetupCheck{Key: "discord_guild_id", Label: "Discord guild fallback", Status: SetupWarn, Summary: "Configured", Detail: strings.TrimSpace(guildID), Fix: "Per-project guilds are preferred; fallback guild is only a compatibility default."})
	} else {
		checks = append(checks, SetupCheck{Key: "discord_guild_id", Label: "Discord guild fallback", Status: SetupWarn, Summary: "Not set", Fix: "Assign guild IDs per project in /bot/admin/projects."})
	}
	return checks
}

func buildKitsuCheck(kitsuHost, lang string) SetupCheck {
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
		check.Detail = t(lang, "Kitsu server は応答しましたが、認証は完了していません。", "Kitsu server answered, but authentication is not complete.")
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

func buildTestNotificationCheck(db *gorm.DB, lang string) SetupCheck {
	verified := strings.EqualFold(strings.TrimSpace(model.GetSetting(db, setupTestNotificationVerifiedKey)), "true")
	projectID := strings.TrimSpace(model.GetSetting(db, setupTestNotificationProjectKey))
	verifiedAt := strings.TrimSpace(model.GetSetting(db, setupTestNotificationAtKey))
	check := SetupCheck{
		Key:     "test_notification",
		Label:   "Final Health Check",
		Summary: "Not confirmed yet",
		Detail:  t(lang, "setup を完了扱いにする前に Health で通知先と runtime 状態を確認してください。", "Review Health to confirm the notification destination and runtime status before treating setup as complete."),
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
	if !diag.ProductionRoutingConfigured {
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

func hasReadyProductionRouting(db *gorm.DB) bool {
	for _, project := range model.ListProjects(db) {
		if settings, ok, err := model.LoadProductionForumSettings(db, project.KitsuProjectID); err == nil && ok && len(settings.Routes) > 0 {
			return true
		}
	}
	return false
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
	if !diag.ProductionRoutingConfigured {
		return "Select a connected Production, Task Type, and valid destination in Production Notification Routing."
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
	if !diag.ProductionRoutingConfigured {
		reasons = append(reasons, "Production routing: configure at least one enabled valid Production-ID / Task-Type-ID route.")
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
