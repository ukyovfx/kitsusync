package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

// --- Response types for /api/setup/* ---

// SetupStatusResponse is the response body for GET /api/setup/status.
type SetupStatusResponse struct {
	Kitsu                KitsuStatusInfo       `json:"kitsu"`
	Discord              DiscordStatusInfo     `json:"discord"`
	Poller               PollerStatusInfo      `json:"poller"`
	Project              ProjectStatusInfo     `json:"project"`
	Projects             []ProjectGuildHealth  `json:"projects,omitempty"`
	Diagnostics          SetupDiagnostics      `json:"diagnostics,omitempty"`
	ProjectSetupApplied  bool                  `json:"project_setup_applied"`
	NotificationVerified bool                  `json:"notification_verified"`
	SetupComplete        bool                  `json:"setup_complete"`
	SetupWarnings        []string              `json:"setup_warnings"`
	IncompleteReasons    []string              `json:"incomplete_reasons,omitempty"`
	Readiness            NotificationReadiness `json:"readiness"`
}

type NotificationReadiness struct {
	KitsuConfigured              bool   `json:"kitsu_configured"`
	KitsuConnected               bool   `json:"kitsu_connected"`
	KitsuReady                   bool   `json:"kitsu_ready"`
	DiscordBotConfigured         bool   `json:"discord_bot_configured"`
	DiscordAPIValidated          bool   `json:"discord_api_validated"`
	ProductionRoutingConfigured  bool   `json:"production_routing_configured"`
	OverallNotificationReadiness string `json:"overall_notification_readiness"`
}

// KitsuStatusInfo holds the current Kitsu connectivity and auth state.
type KitsuStatusInfo struct {
	Configured      bool    `json:"configured"`
	Reachable       bool    `json:"reachable"`
	Authenticated   bool    `json:"authenticated"`
	DiscoverySource string  `json:"discovery_source,omitempty"`
	Error           *string `json:"error"`
}

// DiscordStatusInfo holds the current Discord bot and guild state.
type DiscordStatusInfo struct {
	Configured  bool                  `json:"configured"`
	BotValid    bool                  `json:"bot_valid"`
	BotName     string                `json:"bot_name,omitempty"`
	GuildValid  bool                  `json:"guild_valid"`
	GuildName   string                `json:"guild_name,omitempty"`
	Permissions DiscordPermissionInfo `json:"permissions"`
	Error       *string               `json:"error"`
}

// DiscordPermissionInfo reports which critical bot permissions are confirmed.
type DiscordPermissionInfo struct {
	ManageChannels bool `json:"manage_channels"`
	ManageWebhooks bool `json:"manage_webhooks"`
}

func discordPermissionInfoFromBits(perms uint64) DiscordPermissionInfo {
	const administrator = uint64(1 << 3)
	const manageChannels = uint64(1 << 4)
	const manageWebhooks = uint64(1 << 29)
	isAdministrator := perms&administrator != 0
	return DiscordPermissionInfo{
		ManageChannels: isAdministrator || perms&manageChannels != 0,
		ManageWebhooks: isAdministrator || perms&manageWebhooks != 0,
	}
}

type discordRolePermission struct {
	ID          string `json:"id"`
	Permissions string `json:"permissions"`
}

func discordPermissionsFromRoles(guildID string, roles []discordRolePermission, memberRoleIDs []string) (DiscordPermissionInfo, error) {
	memberRoles := make(map[string]bool, len(memberRoleIDs))
	for _, roleID := range memberRoleIDs {
		memberRoles[strings.TrimSpace(roleID)] = true
	}

	var permissions uint64
	roleFound := false
	for _, role := range roles {
		roleID := strings.TrimSpace(role.ID)
		if roleID != strings.TrimSpace(guildID) && !memberRoles[roleID] {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimSpace(role.Permissions), 10, 64)
		if err != nil {
			return DiscordPermissionInfo{}, fmt.Errorf("discord role permissions were not numeric")
		}
		permissions |= value
		roleFound = true
	}
	if !roleFound {
		return DiscordPermissionInfo{}, fmt.Errorf("discord bot roles were not available")
	}

	const administrator = uint64(1 << 3)
	if permissions&administrator != 0 {
		return DiscordPermissionInfo{ManageChannels: true, ManageWebhooks: true}, nil
	}
	return DiscordPermissionInfo{
		ManageChannels: permissions&(1<<4) != 0,
		ManageWebhooks: permissions&(1<<29) != 0,
	}, nil
}

// discordPermissionBitsFromRoles is kept for focused legacy unit tests; the
// effective permission calculation intentionally ignores member.Permissions.
func discordPermissionBitsFromRoles(guildID string, memberRoleIDs []string, rolePermissions map[string]string, _ string) uint64 {
	roles := make([]discordRolePermission, 0, len(rolePermissions))
	for id, permissions := range rolePermissions {
		roles = append(roles, discordRolePermission{ID: id, Permissions: permissions})
	}
	info, err := discordPermissionsFromRoles(guildID, roles, memberRoleIDs)
	if err != nil {
		return 0
	}
	var bits uint64
	if info.ManageChannels {
		bits |= 1 << 4
	}
	if info.ManageWebhooks {
		bits |= 1 << 29
	}
	return bits
}

func fetchDiscordEffectivePermissions(guildID, botUserID, botToken string) (DiscordPermissionInfo, error) {
	rolesBody, status, err := botDo("GET", discordAPI+"/guilds/"+guildID+"/roles", nil, botToken)
	if err != nil {
		return DiscordPermissionInfo{}, err
	}
	if status != http.StatusOK {
		return DiscordPermissionInfo{}, fmt.Errorf("Discord roles request returned HTTP %d", status)
	}
	var roles []discordRolePermission
	if err := json.Unmarshal(rolesBody, &roles); err != nil {
		return DiscordPermissionInfo{}, fmt.Errorf("Discord roles response was invalid")
	}

	memberBody, status, err := botDo("GET", discordAPI+"/guilds/"+guildID+"/members/"+botUserID, nil, botToken)
	if err != nil {
		return DiscordPermissionInfo{}, err
	}
	if status != http.StatusOK {
		return DiscordPermissionInfo{}, fmt.Errorf("Discord bot member request returned HTTP %d", status)
	}
	var member struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(memberBody, &member); err != nil {
		return DiscordPermissionInfo{}, fmt.Errorf("Discord bot member response was invalid")
	}
	return discordPermissionsFromRoles(guildID, roles, member.Roles)
}

// PollerStatusInfo describes the poller's last known state.
type PollerStatusInfo struct {
	Running         bool       `json:"running"`
	LastPollAt      *time.Time `json:"last_poll_at"`
	LastTaskCount   int        `json:"last_task_count"`
	LastError       *string    `json:"last_error"`
	PollIntervalSec int        `json:"poll_interval_sec"`
}

// ProjectStatusInfo describes configured project state in the DB.
type ProjectStatusInfo struct {
	Selected     bool   `json:"selected"`
	ProjectID    string `json:"project_id,omitempty"`
	ProjectName  string `json:"project_name,omitempty"`
	Template     string `json:"template,omitempty"`
	ChannelCount int    `json:"channel_count"`
	WebhookCount int    `json:"webhook_count"`
}

type ProjectGuildHealth struct {
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	GuildID         string `json:"guild_id,omitempty"`
	GuildValid      bool   `json:"guild_valid"`
	PermissionValid bool   `json:"permission_valid"`
	WebhookHealth   string `json:"webhook_health"`
	CategoryValid   bool   `json:"category_valid"`
	ResourceHealth  string `json:"resource_health"`
	StaleChannels   int    `json:"stale_channel_count"`
}

// SetupProjectItem is one entry in the GET /api/setup/projects response.
type SetupProjectItem struct {
	ProjectID     string `json:"project_id"`
	ProjectName   string `json:"project_name"`
	TaskTypeCount int    `json:"task_type_count"`
	IsConfigured  bool   `json:"is_configured"`
	GuildID       string `json:"guild_id,omitempty"`
}

// ApplyProjectRequest is the request body for POST /api/setup/apply-project.
type ApplyProjectRequest struct {
	ProjectID string `json:"project_id"`
	Template  string `json:"template"`
	Language  string `json:"language"`
	GuildID   string `json:"guild_id"`
}

// ApplyProjectResponse is the response body for POST /api/setup/apply-project.
type ApplyProjectResponse struct {
	OK                  bool     `json:"ok"`
	ProjectID           string   `json:"project_id"`
	ProjectName         string   `json:"project_name"`
	GuildID             string   `json:"guild_id,omitempty"`
	DiscordServerName   string   `json:"discord_server_name,omitempty"`
	ChannelCount        int      `json:"channels_created"`
	WebhookCount        int      `json:"webhooks_created"`
	SafeToRetry         bool     `json:"safe_to_retry"`
	ProjectSetupApplied bool     `json:"project_setup_applied"`
	Warnings            []string `json:"warnings,omitempty"`
	Lines               []string `json:"lines"`
	Error               *string  `json:"error"`
}

type PreviewProjectRequest struct {
	ProjectID       string            `json:"project_id"`
	Template        string            `json:"template"`
	Language        string            `json:"language"`
	GuildID         string            `json:"guild_id"`
	Mode            string            `json:"mode"`
	ChannelBindings map[string]string `json:"channel_bindings,omitempty"`
}

type PreviewProjectChannel struct {
	Name      string   `json:"name"`
	TaskTypes []string `json:"task_types"`
}

type PreviewProjectResponse struct {
	OK                bool                    `json:"ok"`
	ProjectID         string                  `json:"project_id"`
	ProjectName       string                  `json:"project_name,omitempty"`
	DiscordServerName string                  `json:"discord_server_name,omitempty"`
	GuildID           string                  `json:"guild_id,omitempty"`
	CategoryName      string                  `json:"category_name,omitempty"`
	ChannelsToCreate  []PreviewProjectChannel `json:"channels_to_create,omitempty"`
	ChannelsToReuse   []PreviewProjectChannel `json:"channels_to_reuse,omitempty"`
	WebhooksToCreate  int                     `json:"webhooks_to_create"`
	Warnings          []string                `json:"warnings,omitempty"`
	Error             *string                 `json:"error,omitempty"`
}

// TestKitsuRequest is the request body for POST /api/setup/test-kitsu.
type TestKitsuRequest struct {
	Hostname string `json:"hostname"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TestKitsuResponse is the response body for POST /api/setup/test-kitsu.
type TestKitsuResponse struct {
	Reachable     bool    `json:"reachable"`
	Authenticated bool    `json:"authenticated"`
	Error         *string `json:"error"`
}

// TestDiscordRequest is the request body for POST /api/setup/test-discord.
type TestDiscordRequest struct {
	BotToken string `json:"bot_token"`
	GuildID  string `json:"guild_id"`
}

// TestDiscordResponse is the response body for POST /api/setup/test-discord.
type TestDiscordResponse struct {
	BotValid    bool                  `json:"bot_valid"`
	BotName     string                `json:"bot_name,omitempty"`
	GuildValid  bool                  `json:"guild_valid"`
	GuildName   string                `json:"guild_name,omitempty"`
	Permissions DiscordPermissionInfo `json:"permissions"`
	Error       *string               `json:"error"`
}

type TestNotificationRequest struct {
	ProjectID string `json:"project_id"`
	Message   string `json:"message"`
}

type TestNotificationResponse struct {
	OK                   bool    `json:"ok"`
	ProjectID            string  `json:"project_id"`
	ProjectName          string  `json:"project_name,omitempty"`
	MessageID            string  `json:"message_id,omitempty"`
	NotificationVerified bool    `json:"notification_verified"`
	Error                *string `json:"error"`
}

// --- Handlers ---

// SetupStatusHandler handles GET /api/setup/status.
// Returns a unified JSON snapshot of Kitsu, Discord, Poller, and Project readiness.
func SetupStatusHandler(db *gorm.DB, pollIntervalSec int, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		kitsuHost, botToken, guildID, _ := refreshCreds()
		snapshot := BuildSetupDiagnostics(db, refreshCreds)
		kitsuDiscovery := DiscoverKitsuHost(db)
		kitsuStatus := checkKitsuStatus(kitsuHost)
		kitsuStatus.DiscoverySource = kitsuDiscovery.Source
		resp := SetupStatusResponse{
			Kitsu:                kitsuStatus,
			Discord:              buildProjectAwareDiscordStatus(db, botToken, guildID),
			Poller:               buildPollerStatus(pollIntervalSec),
			Project:              buildProjectStatus(db),
			Projects:             buildProjectGuildHealth(db, botToken, guildID),
			Diagnostics:          snapshot,
			ProjectSetupApplied:  snapshot.ProjectSetupApplied,
			NotificationVerified: snapshot.NotificationVerified,
		}
		resp.SetupComplete = snapshot.SetupComplete
		resp.SetupWarnings = append(resp.SetupWarnings, snapshot.Warnings...)
		resp.IncompleteReasons = incompleteReasons(snapshot)
		resp.Readiness = NotificationReadiness{
			KitsuConfigured:              snapshot.Kitsu.Status != SetupError && snapshot.Kitsu.Summary != "Unknown",
			KitsuConnected:               snapshot.Kitsu.Status == SetupOK,
			KitsuReady:                   snapshot.Kitsu.Status == SetupOK,
			DiscordBotConfigured:         checkSetupToken(snapshot.Env),
			DiscordAPIValidated:          snapshot.Discord.Status == SetupOK,
			ProductionRoutingConfigured:  snapshot.ProductionRoutingConfigured,
			OverallNotificationReadiness: snapshot.OverallNotificationReadiness,
		}
		json.NewEncoder(w).Encode(resp)
	}
}

func checkSetupToken(checks []SetupCheck) bool {
	for _, check := range checks {
		if check.Key == "discord_bot_token" {
			return check.Status == SetupOK
		}
	}
	return false
}

// ProjectsHandler handles GET /api/setup/projects.
// Returns all Kitsu projects enriched with task type count and DB configuration state.
func ProjectsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		kitsuProjects := kitsu.GetProjects()
		taskTypeCount := len(kitsu.GetTaskTypes().Each)

		configured := model.ListProjects(db)
		configuredByID := make(map[string]model.Project, len(configured))
		for _, p := range configured {
			configuredByID[p.KitsuProjectID] = p
		}

		out := make([]SetupProjectItem, 0, len(kitsuProjects.Each))
		for _, p := range kitsuProjects.Each {
			cfg, ok := configuredByID[p.ID]
			out = append(out, SetupProjectItem{
				ProjectID:     p.ID,
				ProjectName:   strings.TrimSpace(p.Name),
				TaskTypeCount: taskTypeCount,
				IsConfigured:  ok,
				GuildID:       strings.TrimSpace(cfg.DiscordGuildID),
			})
		}

		json.NewEncoder(w).Encode(out)
	}
}

// PreviewProjectHandler handles POST /api/setup/preview-project.
// It is read-only and never creates Discord resources, webhooks, DB records, or mapping updates.
func PreviewProjectHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req PreviewProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errStr := "invalid request body"
			json.NewEncoder(w).Encode(PreviewProjectResponse{Error: &errStr})
			return
		}

		projectID := strings.TrimSpace(req.ProjectID)
		if projectID == "" {
			errStr := "project_id is required"
			json.NewEncoder(w).Encode(PreviewProjectResponse{Error: &errStr})
			return
		}

		template := strings.ToLower(strings.TrimSpace(req.Template))
		if template == "" {
			template = "cg"
		}
		tmpl, ok := Templates[template]
		if !ok {
			errStr := "unsupported template: " + template
			json.NewEncoder(w).Encode(PreviewProjectResponse{ProjectID: projectID, Error: &errStr})
			return
		}

		language := strings.TrimSpace(req.Language)
		if language != "en" {
			language = "ja"
		}

		project := kitsu.GetProject(projectID)
		projectName := strings.TrimSpace(project.Name)
		if projectName == "" {
			errStr := "project not found in Kitsu (id: " + projectID + ")"
			json.NewEncoder(w).Encode(PreviewProjectResponse{ProjectID: projectID, Error: &errStr})
			return
		}

		_, botToken, fallbackGuildID, _ := refreshCreds()
		resolvedGuildID, usedFallback := resolveProjectGuildForSetup(db, projectID, strings.TrimSpace(req.GuildID), fallbackGuildID)
		if resolvedGuildID == "" {
			errStr := "no Discord guild is configured for this project"
			json.NewEncoder(w).Encode(PreviewProjectResponse{ProjectID: projectID, ProjectName: projectName, Error: &errStr})
			return
		}

		serverName := resolvedGuildID
		if info := checkDiscordStatus(botToken, resolvedGuildID); strings.TrimSpace(info.GuildName) != "" {
			serverName = strings.TrimSpace(info.GuildName)
		}

		resp := PreviewProjectResponse{
			OK:                true,
			ProjectID:         projectID,
			ProjectName:       projectName,
			DiscordServerName: serverName,
			GuildID:           resolvedGuildID,
			CategoryName:      projectName,
			ChannelsToCreate:  buildTemplatePreviewChannels(tmpl, language),
		}
		resp.WebhooksToCreate = len(resp.ChannelsToCreate)

		if usedFallback {
			resp.Warnings = append(resp.Warnings, "This project does not have a fixed Discord Server yet, so the fallback server will be used.")
		}
		if strings.ToLower(strings.TrimSpace(req.Mode)) == "reuse" {
			resp.Warnings = append(resp.Warnings, "Existing channel reuse is not available yet in v0.1.0. This preview shows the new-channel plan instead.")
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// ApplyProjectHandler handles POST /api/setup/apply-project.
// Runs the full channel/webhook creation flow for the given Kitsu project.
func ApplyProjectHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req ApplyProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errStr := "invalid request body"
			json.NewEncoder(w).Encode(ApplyProjectResponse{Error: &errStr})
			return
		}

		projectID := strings.TrimSpace(req.ProjectID)
		if projectID == "" {
			errStr := "project_id is required"
			json.NewEncoder(w).Encode(ApplyProjectResponse{Error: &errStr})
			return
		}

		template := strings.ToLower(strings.TrimSpace(req.Template))
		if template == "" {
			template = "cg"
		}
		if _, ok := Templates[template]; !ok {
			errStr := "unsupported template: " + template
			json.NewEncoder(w).Encode(ApplyProjectResponse{ProjectID: projectID, Error: &errStr})
			return
		}

		language := strings.TrimSpace(req.Language)
		if language != "en" {
			language = "ja"
		}

		// Resolve project name from Kitsu
		project := kitsu.GetProject(projectID)
		projectName := strings.TrimSpace(project.Name)
		if projectName == "" {
			errStr := "project not found in Kitsu (id: " + projectID + ")"
			json.NewEncoder(w).Encode(ApplyProjectResponse{ProjectID: projectID, Error: &errStr})
			return
		}

		kitsuHost, botToken, guildID, _ := refreshCreds()

		reqGuildID := strings.TrimSpace(req.GuildID)
		result := RunProjectSetup(projectID, projectName, template, language, kitsuHost, guildID, reqGuildID, botToken, db)
		resolvedGuildID, _ := resolveProjectGuildForSetup(db, projectID, reqGuildID, guildID)
		serverName := resolvedGuildID
		if info := checkDiscordStatus(botToken, resolvedGuildID); strings.TrimSpace(info.GuildName) != "" {
			serverName = strings.TrimSpace(info.GuildName)
		}

		resp := ApplyProjectResponse{
			OK:                result.OK,
			ProjectID:         projectID,
			ProjectName:       projectName,
			GuildID:           resolvedGuildID,
			DiscordServerName: serverName,
			SafeToRetry:       result.SafeToRetry,
			Lines:             result.Lines,
		}

		if result.OK {
			model.SetSetting(db, setupProjectAppliedKey, "true")
			model.SetSetting(db, setupProjectAppliedProjectKey, projectID)
			model.SetSetting(db, setupProjectAppliedAtKey, time.Now().Format(time.RFC3339))
			model.SetSetting(db, setupTestNotificationVerifiedKey, "false")
			model.DeleteSetting(db, setupTestNotificationProjectKey)
			model.DeleteSetting(db, setupTestNotificationAtKey)
			// Count channels and webhook assignments created
			webhooks := model.ListProjectWebhooks(db, projectID)
			seenChannels := make(map[string]bool)
			webhookCount := 0
			for _, wh := range webhooks {
				if wh.TaskType != "" {
					webhookCount++
				}
				seenChannels[wh.ChannelName] = true
			}
			resp.ChannelCount = len(seenChannels)
			resp.WebhookCount = webhookCount
			resp.ProjectSetupApplied = true
		} else {
			errStr := "project setup failed — see lines for details"
			for _, line := range result.Lines {
				if strings.HasPrefix(line, "FAIL:") {
					errStr = strings.TrimPrefix(line, "FAIL: ")
					break
				}
			}
			resp.Error = &errStr
		}

		json.NewEncoder(w).Encode(resp)
	}
}

// TestNotificationHandler sends a small message to one project webhook and marks the setup as verified.
func TestNotificationHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req TestNotificationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errStr := "invalid request body"
			json.NewEncoder(w).Encode(TestNotificationResponse{Error: &errStr})
			return
		}

		projectID := strings.TrimSpace(req.ProjectID)
		if projectID == "" {
			projects := model.ListProjects(db)
			if len(projects) == 1 {
				projectID = projects[0].KitsuProjectID
			}
		}
		if projectID == "" {
			errStr := "project_id is required"
			json.NewEncoder(w).Encode(TestNotificationResponse{Error: &errStr})
			return
		}

		project := model.FindProjectByKitsuID(db, projectID)
		if project == nil {
			errStr := "project not found"
			json.NewEncoder(w).Encode(TestNotificationResponse{ProjectID: projectID, Error: &errStr})
			return
		}

		webhooks := model.ListProjectWebhooks(db, projectID)
		targetURL := ""
		for _, wh := range webhooks {
			if strings.TrimSpace(wh.WebhookURL) != "" {
				targetURL = strings.TrimSpace(wh.WebhookURL)
				break
			}
		}
		if targetURL == "" {
			errStr := "no webhook configured for this project"
			json.NewEncoder(w).Encode(TestNotificationResponse{ProjectID: projectID, Error: &errStr})
			return
		}

		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = "KitsuSync test notification"
		}
		res := discord.SendMessage(discord.Payload{
			Username: "KitsuSync",
			Content:  message,
		}, targetURL, "", "")
		if strings.TrimSpace(res.MessageID) == "" {
			errStr := "test notification failed to send"
			json.NewEncoder(w).Encode(TestNotificationResponse{ProjectID: projectID, Error: &errStr})
			return
		}

		model.SetSetting(db, setupTestNotificationVerifiedKey, "true")
		model.SetSetting(db, setupTestNotificationProjectKey, projectID)
		model.SetSetting(db, setupTestNotificationAtKey, time.Now().Format(time.RFC3339))

		json.NewEncoder(w).Encode(TestNotificationResponse{
			OK:                   true,
			ProjectID:            projectID,
			ProjectName:          project.Name,
			MessageID:            res.MessageID,
			NotificationVerified: true,
		})
	}
}

// TestKitsuHandler handles POST /api/setup/test-kitsu.
// Verifies connectivity and authentication against a provided Kitsu endpoint.
func TestKitsuHandler(db *gorm.DB, onRuntimeConfigured ...func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req TestKitsuRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeTestKitsuError(w, "invalid request body")
			return
		}

		hostname := strings.TrimSpace(req.Hostname)
		if hostname == "" {
			hostname = publicKitsuHostnameFromRequest(r, os.Getenv("KITSU_HOSTNAME"))
		}
		email := strings.TrimSpace(req.Email)
		password := req.Password

		if hostname == "" || email == "" || password == "" {
			writeTestKitsuError(w, "hostname(auto), email, and password are required")
			return
		}
		if !strings.HasPrefix(hostname, "http://") && !strings.HasPrefix(hostname, "https://") {
			writeTestKitsuError(w, "hostname must start with http:// or https://")
			return
		}
		if !strings.HasSuffix(hostname, "/") {
			hostname += "/"
		}

		client := &http.Client{Timeout: 10 * time.Second}
		pingURL := strings.TrimRight(hostname, "/") + "/api/"
		resp, err := client.Get(pingURL)
		if err != nil {
			writeTestKitsuError(w, "Kitsu server not reachable: "+err.Error())
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 500 {
			writeTestKitsuError(w, fmt.Sprintf("Kitsu server returned HTTP %d", resp.StatusCode))
			return
		}

		loginOK, loginReason := tryKitsuLogin(hostname, email, password)
		if !loginOK {
			errStr := loginReason
			json.NewEncoder(w).Encode(TestKitsuResponse{Reachable: true, Error: &errStr})
			return
		}

		model.SetSetting(db, "kitsu.hostname", hostname)
		setRuntimeKitsuEmail(db, email)
		if err := setRuntimeKitsuPassword(db, password); err != nil {
			writeTestKitsuError(w, "could not safely store runtime credentials")
			return
		}
		if len(onRuntimeConfigured) > 0 && onRuntimeConfigured[0] != nil {
			onRuntimeConfigured[0]()
		}

		json.NewEncoder(w).Encode(TestKitsuResponse{Reachable: true, Authenticated: true})
	}
}

// TestDiscordHandler handles POST /api/setup/test-discord.
// Verifies bot token validity, guild membership, and required permissions.
func TestDiscordHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req TestDiscordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeTestDiscordError(w, "invalid request body")
			return
		}

		botToken := strings.TrimSpace(req.BotToken)
		guildID := strings.TrimSpace(req.GuildID)

		if botToken == "" || guildID == "" {
			writeTestDiscordError(w, "bot_token and guild_id are required")
			return
		}

		info := checkDiscordStatus(botToken, guildID)
		json.NewEncoder(w).Encode(TestDiscordResponse{
			BotValid:    info.BotValid,
			BotName:     info.BotName,
			GuildValid:  info.GuildValid,
			GuildName:   info.GuildName,
			Permissions: info.Permissions,
			Error:       info.Error,
		})
	}
}

// --- Internal helpers ---

func checkKitsuStatus(kitsuHost string) (info KitsuStatusInfo) {
	started := time.Now()
	defer func() {
		classification := "success"
		if info.Error != nil {
			classification = "error"
		}
		Stats.RecordAPIObservation("kitsu", started, info.Authenticated, classification)
	}()
	if kitsuHost == "" {
		errStr := "KITSU_HOSTNAME not configured"
		return KitsuStatusInfo{Error: &errStr}
	}
	info = KitsuStatusInfo{Configured: true}

	client := &http.Client{Timeout: 8 * time.Second}
	pingURL := strings.TrimRight(kitsuHost, "/") + "/api/"
	resp, err := client.Get(pingURL)
	if err != nil {
		errStr := "server not reachable: " + err.Error()
		info.Error = &errStr
		return info
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		errStr := fmt.Sprintf("server returned HTTP %d", resp.StatusCode)
		info.Error = &errStr
		return info
	}
	info.Reachable = true

	jwtToken := os.Getenv("KitsuJWTToken")
	if jwtToken == "" {
		errStr := "no active Kitsu session token — check startup logs"
		info.Error = &errStr
		return info
	}

	authURL := strings.TrimRight(kitsuHost, "/") + "/api/auth/user"
	req, err := http.NewRequest("GET", authURL, nil)
	if err != nil {
		errStr := "could not build auth check request"
		info.Error = &errStr
		return info
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	authResp, err := client.Do(req)
	if err != nil {
		errStr := "auth check failed: " + err.Error()
		info.Error = &errStr
		return info
	}
	authResp.Body.Close()
	if authResp.StatusCode == 200 {
		info.Authenticated = true
	} else {
		errStr := fmt.Sprintf("session token rejected (HTTP %d)", authResp.StatusCode)
		info.Error = &errStr
	}
	return info
}

func checkDiscordStatus(botToken, guildID string) (info DiscordStatusInfo) {
	started := time.Now()
	defer func() {
		classification := "success"
		if info.Error != nil {
			classification = "error"
		}
		Stats.RecordAPIObservation("discord", started, info.BotValid, classification)
	}()
	if botToken == "" {
		errStr := "DISCORD_BOT_TOKEN not configured"
		return DiscordStatusInfo{Error: &errStr}
	}
	if guildID == "" {
		info = DiscordStatusInfo{Configured: true}
		body, status, err := botDo("GET", discordAPI+"/users/@me", nil, botToken)
		if err == nil && status == http.StatusOK {
			var me struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			}
			if json.Unmarshal(body, &me) == nil && strings.TrimSpace(me.ID) != "" {
				info.BotValid = true
				info.BotName = me.Username
				return info
			}
		}
		if err != nil {
			errStr := "bot identity validation failed"
			info.Error = &errStr
		} else {
			errStr := fmt.Sprintf("bot identity rejected (HTTP %d)", status)
			info.Error = &errStr
		}
		return info
	}
	info = DiscordStatusInfo{Configured: true}

	var botUserID string
	body, status, err := botDo("GET", discordAPI+"/users/@me", nil, botToken)
	if err != nil {
		errStr := "Discord API unreachable: " + err.Error()
		info.Error = &errStr
		return info
	}
	switch status {
	case 200:
		var me struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		}
		if json.Unmarshal(body, &me) == nil {
			botUserID = me.ID
			info.BotName = me.Username
		}
		info.BotValid = true
	case 401:
		errStr := "bot token invalid (HTTP 401)"
		info.Error = &errStr
		return info
	default:
		errStr := fmt.Sprintf("unexpected Discord API response (HTTP %d)", status)
		info.Error = &errStr
		return info
	}

	body, status, err = botDo("GET", discordAPI+"/guilds/"+guildID, nil, botToken)
	if err == nil && status == 200 {
		var guild struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(body, &guild) == nil {
			info.GuildName = guild.Name
		}
		info.GuildValid = true
	} else if status == 403 || status == 404 {
		errStr := fmt.Sprintf("guild not accessible (HTTP %d)", status)
		info.Error = &errStr
	} else if err != nil {
		errStr := "guild validation failed"
		info.Error = &errStr
	} else {
		errStr := fmt.Sprintf("unexpected Discord guild response (HTTP %d)", status)
		info.Error = &errStr
	}

	if botUserID != "" && info.GuildValid {
		if permissions, permissionErr := fetchDiscordEffectivePermissions(guildID, botUserID, botToken); permissionErr != nil {
			errStr := permissionErr.Error()
			info.Error = &errStr
		} else {
			info.Permissions = permissions
		}
	}

	return info
}

func buildPollerStatus(pollIntervalSec int) PollerStatusInfo {
	snap := Stats.Snapshot()
	info := PollerStatusInfo{
		PollIntervalSec: pollIntervalSec,
		LastTaskCount:   snap.LastPollTaskCount,
	}
	if snap.PollCount > 0 {
		info.Running = true
		t := snap.LastPollTime
		info.LastPollAt = &t
	}
	if snap.LastPollErr != "" {
		info.LastError = &snap.LastPollErr
	}
	return info
}

func buildProjectStatus(db *gorm.DB) ProjectStatusInfo {
	projects := model.ListProjects(db)
	if len(projects) == 0 {
		return ProjectStatusInfo{}
	}

	// Use the first project for identity fields; aggregate counts across all projects.
	first := projects[0]
	info := ProjectStatusInfo{
		Selected:    true,
		ProjectID:   first.KitsuProjectID,
		ProjectName: first.Name,
		Template:    first.ProjectType,
	}

	allWebhooks := model.ListAllProjectWebhooks(db)
	seenChannels := make(map[string]bool)
	webhookCount := 0
	for _, wh := range allWebhooks {
		if wh.TaskType != "" {
			webhookCount++
		}
		seenChannels[wh.KitsuProjectID+"/"+wh.ChannelName] = true
	}
	info.ChannelCount = len(seenChannels)
	info.WebhookCount = webhookCount

	return info
}

func buildProjectGuildHealth(db *gorm.DB, botToken, fallbackGuildID string) []ProjectGuildHealth {
	projects := model.ListProjects(db)
	out := make([]ProjectGuildHealth, 0, len(projects))
	for _, p := range projects {
		guildID := strings.TrimSpace(p.DiscordGuildID)
		// A Production's saved Guild is authoritative. The compatibility
		// fallback must never make a different Guild look healthy.
		discordInfo := checkDiscordStatus(botToken, guildID)
		wh := model.ListProjectWebhooks(db, p.KitsuProjectID)
		webhookHealth := "missing"
		if len(wh) > 0 {
			webhookHealth = "ok"
		}
		report := inspectProjectDiscordResources(p, db)
		resourceHealth := "stale"
		if report.Healthy() {
			resourceHealth = "ok"
		}
		out = append(out, ProjectGuildHealth{
			ProjectID:       p.KitsuProjectID,
			ProjectName:     p.Name,
			GuildID:         guildID,
			GuildValid:      discordInfo.GuildValid,
			PermissionValid: discordInfo.Permissions.ManageChannels && discordInfo.Permissions.ManageWebhooks,
			WebhookHealth:   webhookHealth,
			CategoryValid:   report.CategoryPresent,
			ResourceHealth:  resourceHealth,
			StaleChannels:   report.StaleChannelCount,
		})
	}
	return out
}

func buildProjectAwareDiscordStatus(db *gorm.DB, botToken, fallbackGuildID string) DiscordStatusInfo {
	for _, project := range model.ListProjects(db) {
		if strings.TrimSpace(project.DiscordGuildID) != "" {
			return checkDiscordStatus(botToken, strings.TrimSpace(project.DiscordGuildID))
		}
	}
	return checkDiscordStatus(botToken, fallbackGuildID)
}

func writeTestKitsuError(w http.ResponseWriter, msg string) {
	json.NewEncoder(w).Encode(TestKitsuResponse{Error: &msg})
}

func writeTestDiscordError(w http.ResponseWriter, msg string) {
	json.NewEncoder(w).Encode(TestDiscordResponse{Error: &msg})
}

func tryKitsuLogin(hostname, email, password string) (bool, string) {
	loginURL := strings.TrimRight(hostname, "/") + "/api/auth/login"
	payload := map[string]string{
		"email":    email,
		"password": password,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return false, "failed to build auth request"
	}
	req, err := http.NewRequest(http.MethodPost, loginURL, bytes.NewReader(b))
	if err != nil {
		return false, "failed to build auth request"
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "authentication request failed: " + err.Error()
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	bodyText := strings.TrimSpace(string(respBody))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var authResp struct {
			Token string `json:"access_token"`
		}
		if err := json.Unmarshal(respBody, &authResp); err != nil || strings.TrimSpace(authResp.Token) == "" {
			return false, "authentication endpoint returned success but token payload was invalid"
		}
		return true, ""
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if bodyText != "" {
			return false, fmt.Sprintf("authentication failed (HTTP %d): %s", resp.StatusCode, bodyText)
		}
		return false, fmt.Sprintf("authentication failed (HTTP %d): email/password mismatch, SSO-only account, or local password not set", resp.StatusCode)
	}
	if bodyText != "" {
		return false, fmt.Sprintf("authentication failed (HTTP %d): %s", resp.StatusCode, bodyText)
	}
	return false, fmt.Sprintf("authentication failed (HTTP %d)", resp.StatusCode)
}

// --- Mapping API types ---

// MappingStateResponse is the response body for GET /api/setup/mapping.
type MappingStateResponse struct {
	ProjectRowID uint                  `json:"project_row_id"`
	ProjectID    string                `json:"project_id"`
	ProjectName  string                `json:"project_name"`
	Persons      []MappingPerson       `json:"persons"`
	TaskTypes    []string              `json:"task_types"`
	UserMaps     []MappingUserEntry    `json:"user_maps"`
	CheckerMaps  []MappingCheckerEntry `json:"checker_maps"`
}

// MappingPerson is a Kitsu person with their current Discord mapping status.
type MappingPerson struct {
	KitsuName     string `json:"kitsu_name"`
	KitsuEmail    string `json:"kitsu_email"`
	DiscordUserID string `json:"discord_user_id"`
}

// MappingUserEntry is one saved user mapping entry.
type MappingUserEntry struct {
	KitsuName     string `json:"kitsu_name"`
	KitsuEmail    string `json:"kitsu_email"`
	DiscordUserID string `json:"discord_user_id"`
}

// MappingCheckerEntry is one saved checker mapping entry.
type MappingCheckerEntry struct {
	TaskType      string `json:"task_type"`
	DiscordUserID string `json:"discord_user_id"`
}

// SaveUserMappingRequest is the body for POST /api/setup/mapping/users.
type SaveUserMappingRequest struct {
	ProjectID string             `json:"project_id"`
	Mappings  []MappingUserEntry `json:"mappings"`
}

// SaveCheckerMappingRequest is the body for POST /api/setup/mapping/checkers.
type SaveCheckerMappingRequest struct {
	ProjectID string                `json:"project_id"`
	Mappings  []MappingCheckerEntry `json:"mappings"`
}

// MappingStateHandler handles GET /api/setup/mapping.
// Returns current project info, all Kitsu persons, task types from DB, and existing mappings.
func MappingStateHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		projects := model.ListProjects(db)
		if len(projects) == 0 {
			errStr := "no project configured — complete Step 3 first"
			json.NewEncoder(w).Encode(map[string]string{"error": errStr})
			return
		}
		proj := projects[0]

		// Build person list from Kitsu
		allPersons := kitsu.GetPersons()
		persons := make([]MappingPerson, 0, len(allPersons.Each))
		for _, p := range allPersons.Each {
			if !p.Active {
				continue
			}
			fullName := strings.TrimSpace(p.FullName)
			if fullName == "" {
				fullName = strings.TrimSpace(p.FirstName + " " + p.LastName)
			}
			if fullName == "" {
				continue
			}
			persons = append(persons, MappingPerson{
				KitsuName:  fullName,
				KitsuEmail: p.Email,
			})
		}

		// Collect distinct task types from project webhooks
		webhooks := model.ListProjectWebhooks(db, proj.KitsuProjectID)
		seenTypes := make(map[string]bool)
		taskTypes := make([]string, 0)
		for _, wh := range webhooks {
			if wh.TaskType != "" && !seenTypes[wh.TaskType] {
				seenTypes[wh.TaskType] = true
				taskTypes = append(taskTypes, wh.TaskType)
			}
		}

		// Existing mappings
		rawUserMaps := model.ListProjectUserMaps(db, proj.ID)
		userMaps := make([]MappingUserEntry, 0, len(rawUserMaps))
		for _, m := range rawUserMaps {
			userMaps = append(userMaps, MappingUserEntry{
				KitsuName:     m.KitsuName,
				KitsuEmail:    m.KitsuEmail,
				DiscordUserID: m.DiscordUserID,
			})
		}

		rawCheckers := model.ListProjectCheckerMaps(db, proj.ID)
		checkerMaps := make([]MappingCheckerEntry, 0, len(rawCheckers))
		for _, c := range rawCheckers {
			checkerMaps = append(checkerMaps, MappingCheckerEntry{
				TaskType:      c.TaskType,
				DiscordUserID: c.DiscordUserID,
			})
		}

		json.NewEncoder(w).Encode(MappingStateResponse{
			ProjectRowID: proj.ID,
			ProjectID:    proj.KitsuProjectID,
			ProjectName:  proj.Name,
			Persons:      persons,
			TaskTypes:    taskTypes,
			UserMaps:     userMaps,
			CheckerMaps:  checkerMaps,
		})
	}
}

func resolveProjectGuildForSetup(db *gorm.DB, projectID, requestedGuildID, fallbackGuildID string) (string, bool) {
	guildID := strings.TrimSpace(requestedGuildID)
	if guildID != "" {
		return guildID, false
	}
	projectGuild := projectGuildSetting(db, projectID)
	if projectGuild != "" {
		return projectGuild, false
	}
	guildID = model.ResolveProjectGuildID(db, projectID, fallbackGuildID)
	return guildID, strings.TrimSpace(guildID) != "" && projectGuild == ""
}

func projectGuildSetting(db *gorm.DB, projectID string) string {
	project := model.FindProjectByKitsuID(db, projectID)
	if project == nil {
		return ""
	}
	return strings.TrimSpace(project.DiscordGuildID)
}

func buildTemplatePreviewChannels(tmpl ProjectTemplate, language string) []PreviewProjectChannel {
	groups := make(map[string][]string)
	order := make([]string, 0, len(tmpl.Channels))
	for _, ch := range tmpl.Channels {
		name := ch.Name(language)
		if _, ok := groups[name]; !ok {
			order = append(order, name)
		}
		groups[name] = append(groups[name], ch.TaskType)
	}
	out := make([]PreviewProjectChannel, 0, len(order))
	for _, name := range order {
		out = append(out, PreviewProjectChannel{Name: name, TaskTypes: groups[name]})
	}
	return out
}

// SaveUserMappingHandler handles POST /api/setup/mapping/users.
// Upserts project-scoped user → Discord ID mappings; entries with empty discord_user_id are deleted.
func SaveUserMappingHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req SaveUserMappingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		proj := model.FindProjectByKitsuID(db, strings.TrimSpace(req.ProjectID))
		if proj == nil {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}

		for _, m := range req.Mappings {
			name := strings.TrimSpace(m.KitsuName)
			if name == "" {
				continue
			}
			discordID := strings.TrimSpace(m.DiscordUserID)
			if discordID == "" {
				model.DeleteProjectUserMapByName(db, proj.ID, name)
			} else {
				model.UpsertProjectUserMap(db, proj.ID, name, strings.TrimSpace(m.KitsuEmail), discordID)
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// SaveCheckerMappingHandler handles POST /api/setup/mapping/checkers.
// Upserts project-scoped task type → checker Discord ID mappings; empty discord_user_id deletes the entry.
func SaveCheckerMappingHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var req SaveCheckerMappingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		proj := model.FindProjectByKitsuID(db, strings.TrimSpace(req.ProjectID))
		if proj == nil {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}

		for _, m := range req.Mappings {
			taskType := strings.TrimSpace(m.TaskType)
			if taskType == "" {
				continue
			}
			discordID := strings.TrimSpace(m.DiscordUserID)
			if discordID == "" {
				model.DeleteProjectCheckerMapByTaskType(db, proj.ID, taskType)
			} else {
				model.UpsertProjectCheckerMap(db, proj.ID, taskType, discordID)
			}
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
