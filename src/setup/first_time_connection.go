package setup

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"app/src/model"
	"gorm.io/gorm"
)

type firstTimeDiscordCheck struct {
	BotValid       bool
	GuildValid     bool
	GuildName      string
	ManageChannels bool
	Reason         string
}

type firstTimePermissionError struct {
	Missing []string
}

func (e firstTimePermissionError) Error() string {
	return "required Discord permissions are missing"
}

func firstTimePermissionLabel(lang, permission string) string {
	if lang == "ja" {
		return map[string]string{
			"manage_channels": "チャンネルの管理",
		}[permission]
	}
	return map[string]string{
		"manage_channels": "Manage Channels",
	}[permission]
}

func firstTimeConnectionErrorMessage(lang string, err error) string {
	var permissionErr firstTimePermissionError
	if errors.As(err, &permissionErr) {
		missing := make([]string, 0, len(permissionErr.Missing))
		for _, permission := range permissionErr.Missing {
			missing = append(missing, firstTimePermissionLabel(lang, permission))
		}
		if lang == "ja" {
			return "Discord Botの権限が不足しています\n\n不足している権限:\n- " + strings.Join(missing, "\n- ") + "\n\nDiscordサーバーでBotの権限を変更してから、もう一度実行してください。"
		}
		return "The Discord Bot is missing required permissions.\n\nMissing permissions:\n- " + strings.Join(missing, "\n- ") + "\n\nUpdate the Bot permissions in the Discord server, then try again."
	}
	return err.Error()
}

// firstTimeConnectionPlan is the forum-only connection plan: a Discord
// category ("KitsuSync area"), the Kitsu Project row, and the notification
// language. Forum channels themselves are created from the forum settings
// page (自動作成), so the wizard provisions no channels or webhooks.
type firstTimeConnectionPlan struct {
	Project              KitsuProject
	GuildID              string
	GuildName            string
	NotificationLanguage string
	CategoryID           string
	CategoryIsNew        bool
}

type firstTimeConnectionOps struct {
	Projects       func(string, *gorm.DB) []KitsuProject
	DiscordCheck   func(string, string) firstTimeDiscordCheck
	ListChannels   func(string, string) ([]DiscordGuildChannel, error)
	CreateCategory func(string, string, string) (string, error)
	DeleteChannel  func(string, string) error
}

var defaultFirstTimeConnectionOps = firstTimeConnectionOps{
	Projects: func(_ string, db *gorm.DB) []KitsuProject {
		projects, _ := setupKitsuProjects(db)
		return projects
	},
	DiscordCheck: func(token, guild string) firstTimeDiscordCheck {
		status := checkDiscordStatus(token, guild)
		reason := ""
		if status.Error != nil {
			reason = *status.Error
		}
		return firstTimeDiscordCheck{BotValid: status.BotValid, GuildValid: status.GuildValid, GuildName: status.GuildName, ManageChannels: status.Permissions.ManageChannels, Reason: reason}
	},
	ListChannels:   ListGuildChannels,
	CreateCategory: CreateCategory,
	DeleteChannel:  DeleteChannel,
}

var firstTimeOps = defaultFirstTimeConnectionOps

var firstTimeExecutionMu sync.Mutex

func validateFirstTimeConnectionRequest(r *http.Request, kitsuHost, botToken string, db *gorm.DB) (firstTimeConnectionPlan, error) {
	ops := firstTimeOps
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	guildID := strings.TrimSpace(r.FormValue("guild_id"))
	if projectID == "" || guildID == "" || strings.TrimSpace(r.FormValue("confirm_plan")) != "yes" {
		return firstTimeConnectionPlan{}, fmt.Errorf("Production and Discord server confirmation are required")
	}
	existingProject := model.FindProjectByKitsuID(db, projectID)
	if existingProject == nil && model.HasProductionOperationalState(db, projectID) {
		return firstTimeConnectionPlan{}, fmt.Errorf("Production has incomplete setup state that requires cleanup before retry")
	}
	if existingProject != nil && !productionRepairMode(r) {
		return firstTimeConnectionPlan{}, fmt.Errorf("Production is already connected")
	}
	var project KitsuProject
	for _, candidate := range ops.Projects(kitsuHost, db) {
		if strings.TrimSpace(candidate.ID) == projectID {
			project = candidate
			break
		}
	}
	if project.ID == "" {
		return firstTimeConnectionPlan{}, fmt.Errorf("The selected Production could not be verified")
	}
	notificationLanguage := strings.TrimSpace(r.FormValue("notification_language"))
	if notificationLanguage != "en" {
		notificationLanguage = "ja"
	}
	if strings.TrimSpace(botToken) == "" {
		return firstTimeConnectionPlan{}, fmt.Errorf("Discord Bot is not configured")
	}
	status := ops.DiscordCheck(botToken, guildID)
	if !status.BotValid || !status.GuildValid || !status.ManageChannels {
		if status.BotValid && status.GuildValid && !status.ManageChannels {
			return firstTimeConnectionPlan{}, firstTimePermissionError{Missing: []string{"manage_channels"}}
		}
		if status.Reason != "" {
			return firstTimeConnectionPlan{}, fmt.Errorf("Discord validation failed: %s", status.Reason)
		}
		return firstTimeConnectionPlan{}, fmt.Errorf("Required Discord permissions are not available")
	}
	categoryID := strings.TrimSpace(r.FormValue("category_id"))
	categoryIsNew := categoryID == "" || categoryID == "__create__"
	if !categoryIsNew {
		channels, err := ops.ListChannels(guildID, botToken)
		if err != nil {
			return firstTimeConnectionPlan{}, fmt.Errorf("Discord channel read failed")
		}
		found := false
		for _, channel := range channels {
			if channel.Type == 4 && channel.ID == categoryID {
				found = true
				break
			}
		}
		if !found {
			return firstTimeConnectionPlan{}, fmt.Errorf("The selected Discord category was not found")
		}
	}
	if existingProject != nil {
		if stored := strings.TrimSpace(existingProject.DiscordCategoryID); stored != "" {
			categoryID = stored
			categoryIsNew = false
		}
	}
	return firstTimeConnectionPlan{Project: project, GuildID: guildID, GuildName: status.GuildName, NotificationLanguage: notificationLanguage, CategoryID: categoryID, CategoryIsNew: categoryIsNew}, nil
}

func renderFirstTimeExecutionPage(lang string, r *http.Request, plan firstTimeConnectionPlan) string {
	return adminPage(lang, tr(lang, "wizard.execute_title"), r, renderWizardFrame(lang, 6, renderWizardExecutionCard(lang, r, plan)))
}

func firstTimeGuildName(lang, guildName string) string {
	if strings.TrimSpace(guildName) != "" {
		return guildName
	}
	return tr(lang, "wizard.server_summary")
}

func normalizedProductionNotificationLanguage(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "en") {
		return "en"
	}
	return "ja"
}

func renderWizardExecutionCard(lang string, r *http.Request, plan firstTimeConnectionPlan) string {
	form := `<form id="wizard-execution-form" method="POST" action="` + html.EscapeString(withLang("/bot/setup", r)) + `"><input type="hidden" name="action" value="execute_production_connection"><input type="hidden" name="confirm_plan" value="yes"><input type="hidden" name="project_id" value="` + html.EscapeString(plan.Project.ID) + `"><input type="hidden" name="guild_id" value="` + html.EscapeString(plan.GuildID) + `"><input type="hidden" name="category_id" value="` + html.EscapeString(plan.CategoryID) + `"><input type="hidden" name="notification_language" value="` + html.EscapeString(normalizedProductionNotificationLanguage(plan.NotificationLanguage)) + `"><noscript><div class="button-row"><a class="btn-ghost" href="` + html.EscapeString(setupWizardURL(r, 5, plan.Project.ID, plan.GuildID, true)) + `">` + html.EscapeString(tr(lang, "wizard.back_to_review")) + `</a><button class="btn" type="submit">` + html.EscapeString(tr(lang, "wizard.execute")) + `</button></div></noscript></form>`
	targets := `<dl class="wizard-connection-summary"><div><dt>` + html.EscapeString(tr(lang, "wizard.production_summary")) + `</dt><dd>` + html.EscapeString(plan.Project.Name) + `</dd></div><div><dt>` + html.EscapeString(tr(lang, "wizard.server_summary")) + `</dt><dd>` + html.EscapeString(firstTimeGuildName(lang, plan.GuildName)) + `</dd></div><div><dt>` + html.EscapeString(tr(lang, "wizard.area_summary")) + `</dt><dd>` + html.EscapeString(KitsuSyncCategoryName(plan.Project.Name)) + `</dd></div></dl>`
	return `<section class="section-card glass" aria-labelledby="wizard-execute-title" role="status" aria-live="polite"><h2 id="wizard-execute-title">` + html.EscapeString(tr(lang, "wizard.execute_title")) + `</h2><p class="hint">` + html.EscapeString(tr(lang, "wizard.execute_hint")) + `</p>` + targets + `<p class="field-help">` + html.EscapeString(tr(lang, "wizard.execute_started")) + `</p>` + form + `<script>(function(){var form=document.getElementById('wizard-execution-form');if(form){form.submit();}})();</script></section>`
}

func renderFirstTimeExecutionError(lang string, r *http.Request, message string) string {
	messageHTML := strings.ReplaceAll(html.EscapeString(message), "\n", "<br>")
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	guildID := strings.TrimSpace(r.FormValue("guild_id"))
	backURL := withLang("/bot/setup", r)
	if projectID != "" && guildID != "" {
		backURL = setupWizardURL(r, 5, projectID, guildID, true)
	}
	body := `<section class="section-card glass" role="alert" aria-live="assertive"><h2>` + html.EscapeString(tr(lang, "wizard.execute_title")) + `</h2><p class="state-explanation">` + messageHTML + `</p><div class="button-row"><a class="btn-ghost" href="` + html.EscapeString(backURL) + `">` + html.EscapeString(tr(lang, "wizard.back_to_review")) + `</a></div></section>`
	return adminPage(lang, tr(lang, "wizard.execute_title"), r, renderWizardFrame(lang, 6, body))
}

// rollbackFirstTimeConnection removes the resources this connection created.
// The forum-only wizard creates at most one category and one Project row.
func rollbackFirstTimeConnection(db *gorm.DB, botToken string, ops firstTimeConnectionOps, plan firstTimeConnectionPlan, createdCategory bool) error {
	if createdCategory && strings.TrimSpace(plan.CategoryID) != "" {
		if err := ops.DeleteChannel(plan.CategoryID, botToken); err != nil && !isDiscordNotFoundDeleteError(err) {
			slog.Warn("first-time setup category rollback failed", "category_id", safeLogField(plan.CategoryID), "error", err)
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := model.DeleteProductionOperationalState(tx, plan.Project.ID); err != nil {
			return err
		}
		return tx.Where("kitsu_project_id = ?", plan.Project.ID).Delete(&model.Project{}).Error
	})
}

func executeFirstTimeConnectionLocked(plan firstTimeConnectionPlan, botToken, lang string, r *http.Request, db *gorm.DB) (result string) {
	ops := firstTimeOps
	project := model.FindProjectByKitsuID(db, plan.Project.ID)
	createdProject := project == nil
	createdCategory := false
	completed := false
	defer func() {
		if !completed && createdProject {
			if err := rollbackFirstTimeConnection(db, botToken, ops, plan, createdCategory); err != nil {
				result = appendFirstTimeRollbackNote(result, err)
			}
		}
	}()
	if project != nil {
		// Repair mode: the Project row and its category already exist.
	} else {
		categoryID := plan.CategoryID
		if plan.CategoryIsNew {
			var err error
			categoryID, err = ops.CreateCategory(plan.GuildID, KitsuSyncCategoryName(plan.Project.Name), botToken)
			if err != nil {
				return renderFirstTimeExecutionError(lang, r, "Discordカテゴリを作成できませんでした。変更は保存されていません。")
			}
			createdCategory = true
			// Rollback deletes plan.CategoryID; it must see the ID we just created.
			plan.CategoryID = categoryID
		}
		if err := model.CreateProject(db, plan.Project.ID, plan.Project.Name, "kitsu", plan.GuildID, categoryID, normalizedProductionNotificationLanguage(plan.NotificationLanguage)); err != nil {
			return renderFirstTimeExecutionError(lang, r, "Production接続を保存できませんでした。作成したDiscordカテゴリは削除しました。")
		}
	}
	completed = true
	slog.Info("first-time production connection completed (forum mode)", "project_id", plan.Project.ID, "guild_id", safeLogField(plan.GuildID), "category_id", safeLogField(plan.CategoryID))
	return adminPage(lang, tr(lang, "wizard.complete_title"), r, renderWizardFrame(lang, 7, renderWizardComplete(lang, r, db, plan.Project.ID)))
}

// appendFirstTimeRollbackNote adds a rollback failure to the already-rendered
// HTML result. The error text is escaped: result is markup, and an error string
// must never be able to inject any.
func appendFirstTimeRollbackNote(result string, err error) string {
	return result + " " + html.EscapeString(err.Error())
}

// safeLogField strips line breaks so form-supplied IDs cannot forge
// log lines (ログ混入). IDs are opaque; line breaks carry no signal.
func safeLogField(value string) string {
	// CodeQL recognizes strings.ReplaceAll as a log-injection sanitizer; it does not recognize strings.Map.
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return value
}

// KitsuSyncCategoryName is the Discord category name that groups everything a
// Production owns in the shared server.
func KitsuSyncCategoryName(productionName string) string {
	name := strings.TrimSpace(productionName)
	if name == "" {
		name = "Production"
	}
	const prefix = "KitsuSync – "
	result := prefix + name
	if len([]rune(result)) <= 100 {
		return result
	}
	runes := []rune(result)
	return strings.TrimRight(string(runes[:100]), " –")
}

func handleFirstTimeConnectionAction(w http.ResponseWriter, r *http.Request, lang, kitsuHost, botToken string, db *gorm.DB) bool {
	if r.Method != http.MethodPost {
		return false
	}
	action := strings.TrimSpace(r.FormValue("action"))
	if action != "prepare_production_connection" && action != "execute_production_connection" {
		return false
	}
	if action == "prepare_production_connection" {
		plan, err := validateFirstTimeConnectionRequest(r, kitsuHost, botToken, db)
		if err != nil {
			fmt.Fprint(w, renderFirstTimeExecutionError(lang, r, firstTimeConnectionErrorMessage(lang, err)))
			return true
		}
		fmt.Fprint(w, renderFirstTimeExecutionPage(lang, r, plan))
		return true
	}
	firstTimeExecutionMu.Lock()
	defer firstTimeExecutionMu.Unlock()
	unlockApply := lockProductionNotificationApply(strings.TrimSpace(r.FormValue("project_id")))
	defer unlockApply()
	plan, err := validateFirstTimeConnectionRequest(r, kitsuHost, botToken, db)
	if err != nil {
		fmt.Fprint(w, renderFirstTimeExecutionError(lang, r, firstTimeConnectionErrorMessage(lang, err)))
		return true
	}
	fmt.Fprint(w, executeFirstTimeConnectionLocked(plan, botToken, lang, r, db))
	return true
}
