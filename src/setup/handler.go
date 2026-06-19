package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/basicauth"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gookit/slog"
	"gorm.io/gorm"
)

type SetupResult struct {
	Lines       []string
	OK          bool
	Duration    time.Duration
	HadRollback bool
	SafeToRetry bool
}

func (r *SetupResult) ok(msg string) { r.Lines = append(r.Lines, "OK: "+msg) }
func (r *SetupResult) warn(msg string) { r.Lines = append(r.Lines, "WARN: "+msg) }
func (r *SetupResult) fail(msg string) { r.Lines = append(r.Lines, "FAIL: "+msg) }
func (r *SetupResult) rolled(msg string) {
	r.Lines = append(r.Lines, "ROLLED BACK: "+msg)
	r.HadRollback = true
}

type createdSetupChannel struct {
	ID   string
	Name string
}

func cleanupDiscordArtifactsAfterDBFailure(categoryID, botToken string, channels []createdSetupChannel, res *SetupResult) (hadCleanupError bool) {
	res.warn("database transaction failed after Discord provisioning; attempting Discord cleanup")
	slog.Warn("Project setup attempting Discord cleanup after database failure", "createdChannels", len(channels), "hasCategory", categoryID != "")

	for i := len(channels) - 1; i >= 0; i-- {
		ch := channels[i]
		if ch.ID == "" {
			continue
		}
		if err := DeleteChannel(ch.ID, botToken); err != nil {
			hadCleanupError = true
			res.warn(fmt.Sprintf("cleanup failed for #%s: %v", ch.Name, err))
			slog.Warn("Project setup cleanup could not delete Discord channel", "channelName", ch.Name, "channelID", ch.ID, "err", err)
		} else {
			res.ok("rolled back channel: #" + ch.Name)
		}
	}
	if categoryID != "" {
		if err := DeleteChannel(categoryID, botToken); err != nil {
			hadCleanupError = true
			res.warn("cleanup failed for Discord category: " + err.Error())
			slog.Warn("Project setup cleanup could not delete Discord category", "categoryID", categoryID, "err", err)
		} else {
			res.ok("rolled back Discord category")
		}
	}

	return hadCleanupError
}

func cleanupSetupArtifacts(kitsuProjectID, categoryID, botToken string, channels []createdSetupChannel, db *gorm.DB, res *SetupResult) {
	for i := len(channels) - 1; i >= 0; i-- {
		ch := channels[i]
		if ch.ID == "" {
			continue
		}
		if err := DeleteChannel(ch.ID, botToken); err != nil {
			res.warn(fmt.Sprintf("cleanup failed for #%s: %v", ch.Name, err))
			slog.Warn("Setup cleanup could not delete Discord channel", "channelName", ch.Name, "channelID", ch.ID, "err", err)
		} else {
			res.ok("rolled back channel: #" + ch.Name)
		}
	}
	if categoryID != "" {
		if err := DeleteChannel(categoryID, botToken); err != nil {
			res.warn("cleanup failed for Discord category: " + err.Error())
			slog.Warn("Setup cleanup could not delete Discord category", "categoryID", categoryID, "err", err)
		} else {
			res.ok("rolled back Discord category")
		}
	}
	if err := db.Where("kitsu_project_id = ?", kitsuProjectID).Delete(&model.ProjectWebhook{}).Error; err != nil {
		res.warn("cleanup failed for setup webhook records: " + err.Error())
		slog.Error("Setup cleanup could not delete webhook records", "kitsuProjectID", kitsuProjectID, "err", err)
		return
	}
	if err := db.Where("kitsu_project_id = ?", kitsuProjectID).Delete(&model.Project{}).Error; err != nil {
		res.warn("cleanup failed for setup project record: " + err.Error())
		slog.Error("Setup cleanup could not delete project record", "kitsuProjectID", kitsuProjectID, "err", err)
		return
	}
	res.ok("rolled back setup records")
}

func RunProjectSetup(kitsuProjectID, projectName, projectType, language, kitsuHost, fallbackGuildID, requestedGuildID, botToken string, db *gorm.DB) (res SetupResult) {
	setupStart := time.Now()
	guildID := strings.TrimSpace(requestedGuildID)
	if guildID == "" {
		guildID = model.ResolveProjectGuildID(db, kitsuProjectID, fallbackGuildID)
	}
	if guildID == "" {
		res.fail("no Discord guild is configured for this project")
		return res
	}
	slog.Info("Project setup started", "projectName", projectName, "projectType", projectType, "kitsuProjectID", kitsuProjectID, "language", language)
	defer func() {
		res.Duration = time.Since(setupStart)
		slog.Info("Project setup finished", "projectName", projectName, "projectType", projectType, "kitsuProjectID", kitsuProjectID, "ok", res.OK, "duration", res.Duration.String())
	}()
	if language != "en" {
		language = "ja"
	}

	tmpl, ok := Templates[strings.ToLower(projectType)]
	if !ok {
		res.fail("unsupported project type: " + projectType)
		slog.Warn("RunProjectSetup failed: unsupported project type", "projectType", projectType)
		return res
	}

	if kitsuProjectID == "" {
		kitsuProjectID = GetKitsuProjectID(kitsuHost, projectName)
	}
	if kitsuProjectID == "" {
		res.warn("Kitsu project was not found, using a fallback project ID")
		kitsuProjectID = "unknown-" + strings.ToLower(strings.ReplaceAll(projectName, " ", "-"))
	} else {
		res.ok("Kitsu project confirmed")
	}

	if existing := model.FindProjectByKitsuID(db, kitsuProjectID); existing != nil {
		res.fail("project is already configured")
		return res
	}

	// Auto-heal orphaned webhooks from a previous failed setup attempt.
	// Since there is no Project record, these rows are garbage and safe to delete.
	var orphanedCount int64
	db.Model(&model.ProjectWebhook{}).Where("kitsu_project_id = ?", kitsuProjectID).Count(&orphanedCount)
	if orphanedCount > 0 {
		res.warn(fmt.Sprintf("orphaned webhooks detected (%d rows); cleaning up before setup", orphanedCount))
		if err := model.DeleteWebhooksByProjectID(db, kitsuProjectID); err != nil {
			res.fail("failed to clean up orphaned webhooks: " + err.Error())
			slog.Error("RunProjectSetup: failed to delete orphaned webhooks", "kitsuProjectID", kitsuProjectID, "err", err)
			return res
		}
		res.ok("cleaned up orphaned webhooks")
	}

	categoryStart := time.Now()
	slog.Info("Project setup creating Discord category", "projectName", projectName, "guildID", guildID)
	categoryID, err := CreateCategory(guildID, projectName, botToken)
	if err != nil {
		res.fail("failed to create Discord category: " + err.Error())
		slog.Error("Project setup Discord category creation failed", "projectName", projectName, "guildID", guildID, "duration", time.Since(categoryStart).String(), "err", err)
		return res
	}
	res.ok("Discord category created")
	slog.Info("Project setup Discord category created", "projectName", projectName, "categoryID", categoryID, "duration", time.Since(categoryStart).String())

	// Phase 1: Discord API operations only - no DB writes until all succeed.
	type webhookData struct {
		ChannelName string
		TaskType    string
		WebhookURL  string
		ChannelID   string
	}
	var webhooksToSave []webhookData
	var createdChannels []createdSetupChannel
	hadDiscordFailure := false

	// Group template entries by channel name so multiple TaskTypes share one Discord channel.
	type channelGroup struct {
		name      string
		taskTypes []string
	}
	groupMap := make(map[string]*channelGroup)
	var groupOrder []string
	for _, ch := range tmpl.Channels {
		name := ch.Name(language)
		if _, ok := groupMap[name]; !ok {
			groupMap[name] = &channelGroup{name: name}
			groupOrder = append(groupOrder, name)
		}
		groupMap[name].taskTypes = append(groupMap[name].taskTypes, ch.TaskType)
	}

	for idx, name := range groupOrder {
		group := groupMap[name]
		channelStart := time.Now()
		slog.Info("Project setup creating Discord channel", "projectName", projectName, "channelName", name, "taskTypes", group.taskTypes, "channelIndex", idx+1, "channelTotal", len(groupOrder))
		channelID, err := CreateTextChannel(guildID, categoryID, name, botToken)
		if err != nil {
			res.fail(fmt.Sprintf("failed to create #%s: %v", name, err))
			slog.Warn("Project setup channel creation failed", "projectName", projectName, "channelName", name, "duration", time.Since(channelStart).String(), "err", err)
			hadDiscordFailure = true
			continue
		}
		slog.Info("Project setup Discord channel created", "projectName", projectName, "channelName", name, "channelID", channelID, "duration", time.Since(channelStart).String())
		webhookStart := time.Now()
		slog.Info("Project setup creating Discord webhook", "projectName", projectName, "channelName", name, "channelID", channelID)
		webhookURL, err := CreateWebhook(channelID, projectName, botToken)
		if err != nil {
			res.fail(fmt.Sprintf("failed to create webhook for #%s: %v", name, err))
			slog.Warn("Project setup webhook creation failed", "projectName", projectName, "channelName", name, "channelID", channelID, "duration", time.Since(webhookStart).String(), "err", err)
			hadDiscordFailure = true
			if cleanupErr := DeleteChannel(channelID, botToken); cleanupErr != nil {
				res.warn(fmt.Sprintf("cleanup failed for #%s after webhook error: %v", name, cleanupErr))
				slog.Warn("Project setup failed to roll back channel after webhook error", "projectName", projectName, "channelName", name, "channelID", channelID, "err", cleanupErr)
			} else {
				res.ok("rolled back incomplete channel: #" + name)
			}
			continue
		}
		slog.Info("Project setup Discord webhook created", "projectName", projectName, "channelName", name, "channelID", channelID, "duration", time.Since(webhookStart).String())
		// Accumulate in memory; DB writes happen atomically after all Discord calls succeed.
		// Store one record per TaskType, all pointing to the same channel and webhook.
		for _, taskType := range group.taskTypes {
			webhooksToSave = append(webhooksToSave, webhookData{
				ChannelName: name,
				TaskType:    taskType,
				WebhookURL:  webhookURL,
				ChannelID:   channelID,
			})
		}
		createdChannels = append(createdChannels, createdSetupChannel{ID: channelID, Name: name})
		res.ok("channel ready: #" + name)
	}

	if hadDiscordFailure {
		res.fail("project setup did not complete; created Discord resources are being rolled back")
		slog.Warn("Project setup ended with Discord provisioning failures; rolling back", "projectName", projectName, "kitsuProjectID", kitsuProjectID, "createdChannels", len(createdChannels))
		cleanupSetupArtifacts(kitsuProjectID, categoryID, botToken, createdChannels, db, &res)
		res.SafeToRetry = true
		return
	}

	// Phase 3: Atomic DB transaction - save project + all webhooks together.
	dbStart := time.Now()
	slog.Info("Project setup persisting database records", "projectName", projectName, "kitsuProjectID", kitsuProjectID, "webhookCount", len(webhooksToSave))
	if txErr := db.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateProject(tx, kitsuProjectID, projectName, projectType, guildID, categoryID, language); err != nil {
			return fmt.Errorf("failed to create project record: %w", err)
		}
		for _, wh := range webhooksToSave {
			if err := model.CreateProjectWebhook(tx, kitsuProjectID, wh.ChannelName, wh.TaskType, wh.WebhookURL, wh.ChannelID); err != nil {
				return fmt.Errorf("failed to save webhook for #%s: %w", wh.ChannelName, err)
			}
		}
		return nil
	}); txErr != nil {
		res.fail("Discord setup succeeded but database transaction failed: " + txErr.Error())
		cleanupHadErrors := cleanupDiscordArtifactsAfterDBFailure(categoryID, botToken, createdChannels, &res)
		if cleanupHadErrors {
			res.fail("automatic Discord cleanup had warnings; verify Discord resources manually before retrying.")
			res.SafeToRetry = false
		} else {
			res.ok("automatic Discord cleanup completed")
			res.SafeToRetry = true
		}
		slog.Error("Project setup database transaction failed", "projectName", projectName, "kitsuProjectID", kitsuProjectID, "duration", time.Since(dbStart).String(), "cleanupHadErrors", cleanupHadErrors, "err", txErr)
		return
	}
	slog.Info("Project setup database records persisted", "projectName", projectName, "kitsuProjectID", kitsuProjectID, "webhookCount", len(webhooksToSave), "duration", time.Since(dbStart).String())

	res.ok("project setup completed")
	res.OK = true
	slog.Info("RunProjectSetup completed successfully", "projectName", projectName, "kitsuProjectID", kitsuProjectID, "categoryID", categoryID)
	return
}

func DeleteProject(kitsuProjectID, botToken string, db *gorm.DB) ([]string, error) {
	var logs []string

	var webhooks []model.ProjectWebhook
	db.Where("kitsu_project_id = ?", kitsuProjectID).Find(&webhooks)
	for _, wh := range webhooks {
		if wh.DiscordChannelID != "" {
			if _, status, err := botDo("DELETE", fmt.Sprintf("%s/channels/%s", discordAPI, wh.DiscordChannelID), nil, botToken); err != nil || status >= 400 {
				logs = append(logs, "failed to delete #"+wh.ChannelName)
			} else {
				logs = append(logs, "deleted #"+wh.ChannelName)
			}
		}
	}

	project := model.FindProjectByKitsuID(db, kitsuProjectID)
	if project != nil && project.DiscordCategoryID != "" {
		_, _, _ = botDo("DELETE", fmt.Sprintf("%s/channels/%s", discordAPI, project.DiscordCategoryID), nil, botToken)
		logs = append(logs, "deleted category")
	}

	if project != nil {
		model.DeleteProjectScopedData(db, project.ID)
	}

	if err := db.Where("kitsu_project_id = ?", kitsuProjectID).Delete(&model.ProjectWebhook{}).Error; err != nil {
		logs = append(logs, "failed to delete project records from database")
		return logs, fmt.Errorf("db delete webhooks: %w", err)
	}
	if err := db.Where("kitsu_project_id = ?", kitsuProjectID).Delete(&model.Project{}).Error; err != nil {
		logs = append(logs, "failed to delete project record from database")
		return logs, fmt.Errorf("db delete project: %w", err)
	}
	logs = append(logs, "deleted project records")
	return logs, nil
}

func Handler(kitsuHost, fallbackGuildID, botToken string, db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)

		if r.Method == http.MethodPost && r.FormValue("action") == "bot_setup" {
			_ = r.ParseForm()
			kitsuHostInput := publicKitsuHostnameFromRequest(r, model.GetSetting(db, "kitsu.hostname"))
			adminEmail := strings.TrimSpace(r.FormValue("bot_admin_email"))
			adminPassword := r.FormValue("bot_admin_password")
			if adminEmail == "" || adminPassword == "" {
				fmt.Fprint(w, renderBotSetupError(lang, t(lang, "スタジオ管理者のメールアドレスとパスワードを入力してください。", "Enter the studio admin email and password.")))
				return
			}
			if kitsuHostInput == "" {
				fmt.Fprint(w, renderBotSetupError(lang, t(lang, "公開ホストを検出できませんでした。公開URLの /bot から開き直してください。", "Could not detect the public host. Re-open this page from the public /bot URL.")))
				return
			}
			botEmail, botPassword, err := CreateKitsuBotAccount(kitsuHostInput, adminEmail, adminPassword)
			if err != nil {
				fmt.Fprint(w, renderBotSetupError(lang, t(lang, "Botアカウントの作成に失敗しました: ", "Bot account creation failed: ")+err.Error()))
				return
			}
			model.SetSetting(db, "kitsu.hostname", kitsuHostInput)
			setRuntimeKitsuEmail(db, botEmail)
			setRuntimeKitsuPassword(botPassword)
			fmt.Fprint(w, renderBotSetupSuccess(lang))
			return
		}

		if r.Method == http.MethodPost && r.FormValue("action") == "channel_delete" {
			_ = r.ParseForm()
			channelName := r.FormValue("channel_name")
			id, _ := strconv.ParseUint(r.FormValue("webhook_id"), 10, 64)
			expected := t(lang, "削除", "delete")
			if strings.TrimSpace(r.FormValue("confirm_text")) != expected {
				fmt.Fprint(w, page(lang, t(lang, "削除確認の入力が一致しませんでした", "Deletion confirmation did not match"), "#ff6a50", channelName, `<li>`+html.EscapeString(expected)+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			if err := DeleteProjectChannel(db, botToken, uint(id)); err != nil {
				fmt.Fprint(w, page(lang, t(lang, "チャンネル削除に失敗しました", "Channel delete failed"), "#ff6a50", "", `<li>`+html.EscapeString(err.Error())+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			http.Redirect(w, r, withLang("/bot/setup", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		if r.Method == http.MethodPost && r.FormValue("action") == "task_type_delete" {
			_ = r.ParseForm()
			id, _ := strconv.ParseUint(r.FormValue("webhook_id"), 10, 64)
			taskType := r.FormValue("task_type")
			expected := t(lang, "削除", "delete")
			if strings.TrimSpace(r.FormValue("confirm_text")) != expected {
				fmt.Fprint(w, page(lang, t(lang, "削除確認の入力が一致しませんでした", "Deletion confirmation did not match"), "#ff6a50", taskType, `<li>`+html.EscapeString(expected)+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			model.DeleteProjectWebhookByID(db, uint(id))
			http.Redirect(w, r, withLang("/bot/setup", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		// create_channel: create a new Discord channel + webhook in the project category, stored pending (task_type="")
		if r.Method == http.MethodPost && r.FormValue("action") == "create_channel" {
			_ = r.ParseForm()
			projectID := r.FormValue("kitsu_project_id")
			// Server-side merge: if "custom" was selected, use channel_name_custom; otherwise use channel_preset
			channelName := strings.TrimSpace(r.FormValue("channel_name_custom"))
			if channelName == "" {
				channelName = strings.TrimSpace(r.FormValue("channel_preset"))
			}
			if channelName == "" || projectID == "" {
				fmt.Fprint(w, page(lang, t(lang, "入力値が不正です", "Invalid input"), "#ff6a50", "", `<li>`+t(lang, "チャンネル名を入力してください。", "Please enter a channel name.")+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			project := model.FindProjectByKitsuID(db, projectID)
			if project == nil {
				fmt.Fprint(w, page(lang, t(lang, "プロジェクトが見つかりません", "Project not found"), "#ff6a50", "", `<li>`+t(lang, "プロジェクト情報を取得できませんでした。", "Could not load project info.")+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			effectiveGuildID := strings.TrimSpace(project.DiscordGuildID)
			if effectiveGuildID == "" {
				effectiveGuildID = strings.TrimSpace(fallbackGuildID)
			}
			if effectiveGuildID == "" {
				fmt.Fprint(w, page(lang, t(lang, "Discord Guild が未設定です", "Discord guild is not configured"), "#ff6a50", channelName, `<li>`+t(lang, "Admin > プロダクション連携管理 で Guild ID を設定してください。", "Set a guild ID in Admin > Production Connection Management.")+`</li>`, `<a href="`+withLang("/bot/admin/projects", r)+`">`+t(lang, "プロダクション連携管理", "Production Connection Management")+`</a>`))
				return
			}
			channelID, err := CreateTextChannel(effectiveGuildID, project.DiscordCategoryID, channelName, botToken)
			if err != nil {
				fmt.Fprint(w, page(lang, t(lang, "チャンネル作成に失敗しました", "Channel creation failed"), "#ff6a50", channelName, `<li>`+html.EscapeString(err.Error())+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			webhookURL, err := CreateWebhook(channelID, channelName, botToken)
			if err != nil {
				fmt.Fprint(w, page(lang, t(lang, "Webhook 作成に失敗しました", "Webhook creation failed"), "#ff6a50", channelName, `<li>`+html.EscapeString(err.Error())+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			// Store as pending (task_type="") — assign task types via the assign form
			if err := model.CreateProjectWebhook(db, projectID, channelName, "", webhookURL, channelID); err != nil {
				fmt.Fprint(w, page(lang, t(lang, "DB への保存に失敗しました", "Failed to save to database"), "#ff6a50", channelName, `<li>`+html.EscapeString(err.Error())+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			http.Redirect(w, r, withLang("/bot/setup", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		if r.Method == http.MethodPost && r.FormValue("action") == "task_assign" {
			_ = r.ParseForm()
			projectID := strings.TrimSpace(r.FormValue("kitsu_project_id"))
			taskType := strings.TrimSpace(r.FormValue("task_type"))
			channelName := strings.TrimSpace(r.FormValue("channel_name"))

			if projectID == "" || taskType == "" || channelName == "" {
				fmt.Fprint(w, page(lang, t(lang, "入力値が不正です", "Invalid input"), "#ff6a50", taskType, `<li>`+t(lang, "プロジェクト、タスクタイプ、チャンネルはすべて必須です。", "Project, task type, and channel are all required.")+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}

			// If a pending (task_type="") record exists for this channel, update it rather than creating a new row
			if pending := model.FindPendingChannel(db, projectID, channelName); pending != nil {
				if err := db.Model(pending).Update("task_type", taskType).Error; err != nil {
					fmt.Fprint(w, page(lang, t(lang, "割り当てに失敗しました", "Assignment failed"), "#ff6a50", taskType, `<li>`+html.EscapeString(err.Error())+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
					return
				}
				http.Redirect(w, r, withLang("/bot/setup", r)+"&msg=saved", http.StatusSeeOther)
				return
			}

			// Find an existing record for this channel to copy webhook URL and Discord channel ID
			existing := model.FindChannelRecord(db, projectID, channelName)
			if existing == nil {
				fmt.Fprint(w, page(lang, t(lang, "チャンネル情報が見つかりません", "Channel not found"), "#ff6a50", channelName, `<li>`+t(lang, "指定されたチャンネルに対応する webhook が存在しません。", "No webhook exists for the specified channel.")+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}

			// Create new ProjectWebhook record copying channel metadata
			webhook := model.ProjectWebhook{
				KitsuProjectID:   projectID,
				TaskType:         taskType,
				ChannelName:      channelName,
				WebhookURL:       existing.WebhookURL,
				DiscordChannelID: existing.DiscordChannelID,
			}

			if err := db.Create(&webhook).Error; err != nil {
				fmt.Fprint(w, page(lang, t(lang, "割り当てに失敗しました", "Assignment failed"), "#ff6a50", taskType, `<li>`+html.EscapeString(err.Error())+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}

			http.Redirect(w, r, withLang("/bot/setup", r)+"&msg=saved", http.StatusSeeOther)
			return
		}

		// delete_final: step 2 — re-authenticate with Kitsu admin credentials then execute delete
		if r.Method == http.MethodPost && r.FormValue("action") == "delete_final" {
			_ = r.ParseForm()
			pid := r.FormValue("kitsu_project_id")
			projectName := r.FormValue("project_name")
			adminEmail := strings.TrimSpace(r.FormValue("admin_email"))
			adminPassword := r.FormValue("admin_password")
			if adminEmail == "" || adminPassword == "" {
				fmt.Fprint(w, page(lang, t(lang, "管理者認証に失敗しました", "Admin authentication failed"), "#ff6a50", projectName, `<li>`+t(lang, "メールアドレスとパスワードを入力してください。", "Please enter email and password.")+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			if token := basicauth.AuthForJWTToken(kitsuHost+"api/auth/login", adminEmail, adminPassword); token == "" {
				fmt.Fprint(w, page(lang, t(lang, "管理者認証に失敗しました", "Admin authentication failed"), "#ff6a50", projectName, `<li>`+t(lang, "Kitsu 管理者のメールアドレスとパスワードを確認してください。", "Check the Kitsu admin email and password.")+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			logs, err := DeleteProject(pid, botToken, db)
			if err != nil {
				logs = append(logs, "cleanup warning: "+err.Error())
				slog.Error("Project delete completed with cleanup errors", "kitsuProjectID", pid, "projectName", projectName, "err", err)
			}
			fmt.Fprint(w, renderDeleteResult(lang, projectName, logs))
			return
		}

		// delete: step 1 — validate fixed confirmation word then show re-auth page
		if r.Method == http.MethodPost && r.FormValue("action") == "delete" {
			_ = r.ParseForm()
			pid := r.FormValue("kitsu_project_id")
			projectName := r.FormValue("project_name")
			expected := t(lang, "削除", "delete")
			if strings.TrimSpace(r.FormValue("confirm_text")) != expected {
				fmt.Fprint(w, page(lang, t(lang, "削除確認の入力が一致しませんでした", "Deletion confirmation did not match"), "#ff6a50", projectName, `<li>`+html.EscapeString(expected)+`</li>`, `<a href="`+withLang("/bot/setup", r)+`">`+t(lang, "戻る", "Back")+`</a>`))
				return
			}
			fmt.Fprint(w, renderDeleteReauthPage(lang, pid, projectName, r))
			return
		}

		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			kitsuProjectID := r.FormValue("kitsu_project_id")
			projectName := strings.TrimSpace(r.FormValue("project_name"))
			projectType := r.FormValue("project_type")
			language := r.FormValue("language")
			if projectName == "" || projectType == "" {
				http.Error(w, "project name and type are required", http.StatusBadRequest)
				return
			}
			requestedGuildID := strings.TrimSpace(r.FormValue("guild_id"))
			if requestedGuildID == "" {
				http.Error(w, "project-level guild_id is required", http.StatusBadRequest)
				return
			}
			result := RunProjectSetup(kitsuProjectID, projectName, projectType, language, kitsuHost, fallbackGuildID, requestedGuildID, botToken, db)
			fmt.Fprint(w, renderResult(lang, projectName, result, r))
			return
		}

		var projects []model.Project
		db.Find(&projects)
		kitsuProjects := ListKitsuProjects(kitsuHost)
		setupDone := map[string]bool{}
		for _, project := range projects {
			setupDone[project.KitsuProjectID] = true
		}

		kitsuHostStored := model.GetSetting(db, "kitsu.hostname")
		kitsuEmailStored := storedRuntimeKitsuEmail(db)
		detectedHost := publicKitsuHostnameFromRequest(r, kitsuHostStored)
		fmt.Fprint(w, renderForm(r, projects, kitsuProjects, setupDone, db, kitsuHostStored, kitsuEmailStored, detectedHost, fallbackGuildID))
	}
}

func DeleteProjectChannel(db *gorm.DB, botToken string, webhookID uint) error {
	webhook := model.FindProjectWebhookByID(db, webhookID)
	if webhook == nil {
		return fmt.Errorf("channel record not found")
	}
	if webhook.DiscordChannelID != "" {
		if err := DeleteChannel(webhook.DiscordChannelID, botToken); err != nil {
			return err
		}
	}
	return model.DeleteProjectWebhooksByChannelName(db, webhook.KitsuProjectID, webhook.ChannelName)
}

func renderProjectChannels(project model.Project, webhooks []model.ProjectWebhook, allTaskTypes []kitsu.TaskType, lang string, r *http.Request) string {
	projectLang := project.Language
	if projectLang == "" {
		projectLang = "ja"
	}

	// Separate pending (task_type="") and active webhooks, then group active by channel name
	type taskItem struct {
		Name      string
		WebhookID uint
	}
	type channelGroup struct {
		Name      string
		Items     []taskItem
		WebhookID uint
	}
	groupMap := make(map[string]*channelGroup)
	var groupOrder []string
	pendingMap := make(map[string]uint) // channelName → representative webhook ID
	var pendingOrder []string

	for _, wh := range webhooks {
		if wh.TaskType == "" {
			if _, ok := pendingMap[wh.ChannelName]; !ok {
				pendingMap[wh.ChannelName] = wh.ID
				pendingOrder = append(pendingOrder, wh.ChannelName)
			}
		} else {
			if _, ok := groupMap[wh.ChannelName]; !ok {
				groupMap[wh.ChannelName] = &channelGroup{Name: wh.ChannelName, WebhookID: wh.ID}
				groupOrder = append(groupOrder, wh.ChannelName)
			}
			groupMap[wh.ChannelName].Items = append(groupMap[wh.ChannelName].Items, taskItem{Name: wh.TaskType, WebhookID: wh.ID})
		}
	}

	// Collect all existing channel names (active + pending) for Section A exclusion
	existingNames := make(map[string]bool)
	for _, n := range groupOrder {
		existingNames[n] = true
	}
	for _, n := range pendingOrder {
		existingNames[n] = true
	}

	// Generate channel list HTML (active channels + pending channels)
	var channelsHTML strings.Builder
	if len(groupOrder) == 0 && len(pendingOrder) == 0 {
		channelsHTML.WriteString(`<div class="muted">` + t(lang, "まだチャンネルがありません。", "No channels yet.") + `</div>`)
	} else {
		// Active channels
		for _, chName := range groupOrder {
			group := groupMap[chName]
			channelsHTML.WriteString(`<div class="channel-group">`)
			channelsHTML.WriteString(fmt.Sprintf(`<div class="channel-header">
        <span class="channel-name">#%s</span>
        <div style="display:flex;gap:6px;flex-wrap:wrap;">
          <form method="POST" class="delete-form" data-confirm="%s" data-require-text="%s">
            <input type="hidden" name="action" value="channel_delete">
            <input type="hidden" name="webhook_id" value="%d">
            <input type="hidden" name="channel_name" value="%s">
            <button class="btn-danger" type="submit">%s</button>
          </form>
        </div>
      </div>`,
				esc(group.Name),
				esc(t(lang, "#"+group.Name+" を削除しますか？Discord チャンネルも削除されます。", "Delete #"+group.Name+"? The Discord channel will also be removed.")),
				t(lang, "削除", "delete"),
				group.WebhookID, esc(group.Name), t(lang, "削除", "Delete")))
			channelsHTML.WriteString(`<ul class="task-list">`)
			for _, item := range group.Items {
				channelsHTML.WriteString(fmt.Sprintf(`<li style="display:flex;justify-content:space-between;align-items:center;gap:8px;"><span class="tag">%s %s</span><form method="POST" class="delete-form" style="margin:0;flex-shrink:0;" data-confirm="%s" data-require-text="%s"><input type="hidden" name="action" value="task_type_delete"><input type="hidden" name="webhook_id" value="%d"><input type="hidden" name="task_type" value="%s"><button class="btn-danger" style="padding:6px 12px;font-size:12px;" type="submit">%s</button></form></li>`,
					taskTypeIcon(item.Name), esc(item.Name), esc(t(lang, item.Name+" の通知ルートを削除します。", "Remove routing for "+item.Name+".")), t(lang, "削除", "delete"), item.WebhookID, esc(item.Name), t(lang, "削除", "Delete")))
			}
			channelsHTML.WriteString(`</ul></div>`)
		}
		// Pending channels (created but not yet assigned to any task type)
		for _, chName := range pendingOrder {
			whID := pendingMap[chName]
			channelsHTML.WriteString(fmt.Sprintf(`<div class="channel-group"><div class="channel-header" style="gap:8px;">
        <span class="channel-name">#%s</span>
        <span class="tag" style="opacity:.65;font-size:.76rem">%s</span>
        <form method="POST" class="delete-form" style="margin-left:auto;" data-confirm="%s" data-require-text="%s">
          <input type="hidden" name="action" value="channel_delete">
          <input type="hidden" name="webhook_id" value="%d">
          <input type="hidden" name="channel_name" value="%s">
          <button class="btn-danger" type="submit">%s</button>
        </form>
      </div></div>`,
				esc(chName),
				t(lang, "割り当て待ち", "Awaiting assignment"),
				esc(t(lang, "#"+chName+" を削除しますか？Discord チャンネルも削除されます。", "Delete #"+chName+"? The Discord channel will also be removed.")),
				t(lang, "削除", "delete"),
				whID, esc(chName), t(lang, "削除", "Delete")))
		}
	}

	// Find unassigned task types (ignore pending channels in assigned check)
	assignedMap := make(map[string]bool)
	for _, wh := range webhooks {
		if wh.TaskType != "" {
			assignedMap[wh.TaskType] = true
		}
	}
	var unassignedTaskTypes []kitsu.TaskType
	for _, tt := range allTaskTypes {
		if !assignedMap[tt.Name] {
			unassignedTaskTypes = append(unassignedTaskTypes, tt)
		}
	}

	// Section A: Add channel — template channels not yet in DB + custom name option
	var addChannelHTML strings.Builder
	{
		tmpl, hasTmpl := Templates[strings.ToLower(project.ProjectType)]
		var templateChannelNames []string
		seen := make(map[string]bool)
		if hasTmpl {
			for _, ch := range tmpl.Channels {
				name := ch.Name(projectLang)
				if !existingNames[name] && !seen[name] {
					templateChannelNames = append(templateChannelNames, name)
					seen[name] = true
				}
			}
		}
		var presetOpts strings.Builder
		presetOpts.WriteString(fmt.Sprintf(`<option value="">-- %s --</option>`, esc(t(lang, "テンプレートから選択", "Select from template"))))
		for _, name := range templateChannelNames {
			presetOpts.WriteString(fmt.Sprintf(`<option value="%s">%s</option>`, esc(name), esc(name)))
		}
		presetOpts.WriteString(fmt.Sprintf(`<option value="__custom__">%s</option>`, esc(t(lang, "カスタム名を入力...", "Enter custom name..."))))
		pid := esc(project.KitsuProjectID)
		addChannelHTML.WriteString(fmt.Sprintf(`
<details class="accordion">
  <summary>
    <div class="accordion-summary-main">
      <strong>%s</strong>
      <div class="tile-sub" style="margin-top:4px">%s</div>
    </div>
    <div class="accordion-summary-side"><span class="accordion-caret">⌄</span></div>
  </summary>
  <div class="accordion-body" style="padding-top:12px">
    <form method="POST" style="display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap;">
      <input type="hidden" name="action" value="create_channel">
      <input type="hidden" name="kitsu_project_id" value="%s">
      <div style="flex:1;min-width:200px">
        <label style="font-size:.8rem;color:var(--muted-2)">%s</label>
        <select name="channel_preset" onchange="toggleCustomInput('%s',this.value)" style="width:100%%">%s</select>
      </div>
      <div id="chCustomWrap_%s" style="flex:1;min-width:180px;display:none">
        <label style="font-size:.8rem;color:var(--muted-2)">%s</label>
        <input type="text" id="chCustomInput_%s" name="channel_name_custom" placeholder="my-channel" style="width:100%%">
      </div>
      <button type="submit" class="btn-sm" style="align-self:flex-end">%s</button>
    </form>
  </div>
</details>`,
			t(lang, "チャンネルを追加", "Add channel"),
			t(lang, "新しい Discord チャンネルを作成してプロジェクトに追加します。", "Create a new Discord channel and add it to this project."),
			pid,
			t(lang, "チャンネル名", "Channel name"),
			pid, presetOpts.String(),
			pid,
			t(lang, "カスタム名", "Custom name"),
			pid,
			t(lang, "作成", "Create"),
		))
	}

	// Section B: Unassigned task types — includes pending channels in dropdown
	allChannelNames := append(append([]string{}, groupOrder...), pendingOrder...)
	var unassignedHTML strings.Builder
	if len(unassignedTaskTypes) > 0 && len(allChannelNames) > 0 {
		var ttOpts strings.Builder
		ttOpts.WriteString(fmt.Sprintf(`<option value="">-- %s --</option>`, esc(t(lang, "タスクタイプを選択", "Select task type"))))
		for _, tt := range unassignedTaskTypes {
			ttOpts.WriteString(fmt.Sprintf(`<option value="%s">%s %s</option>`, esc(tt.Name), taskTypeIcon(tt.Name), esc(tt.Name)))
		}
		var chOpts strings.Builder
		chOpts.WriteString(fmt.Sprintf(`<option value="">-- %s --</option>`, esc(t(lang, "チャンネルを選択", "Select channel"))))
		for _, chName := range groupOrder {
			chOpts.WriteString(fmt.Sprintf(`<option value="%s">#%s</option>`, esc(chName), esc(chName)))
		}
		for _, chName := range pendingOrder {
			chOpts.WriteString(fmt.Sprintf(`<option value="%s">#%s（%s）</option>`,
				esc(chName), esc(chName), t(lang, "割り当て待ち", "awaiting")))
		}
		unassignedHTML.WriteString(fmt.Sprintf(`
<details class="accordion">
  <summary>
    <div class="accordion-summary-main">
      <strong>%s</strong>
      <div class="tile-sub" style="margin-top:4px">%s</div>
    </div>
    <div class="accordion-summary-side">
      <span class="tag">%d</span>
      <span class="accordion-caret">⌄</span>
    </div>
  </summary>
  <div class="accordion-body" style="padding-top:12px">
    <form method="POST" style="display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap;">
      <input type="hidden" name="action" value="task_assign">
      <input type="hidden" name="kitsu_project_id" value="%s">
      <div style="flex:1;min-width:180px"><label style="font-size:.8rem;color:var(--muted-2)">%s</label><select name="task_type" required style="width:100%%">%s</select></div>
      <div style="flex:1;min-width:180px"><label style="font-size:.8rem;color:var(--muted-2)">%s</label><select name="channel_name" required style="width:100%%">%s</select></div>
      <button type="submit" class="btn-sm" style="align-self:flex-end">%s</button>
    </form>
  </div>
</details>`,
			t(lang, "未割り当てのタスクタイプ", "Unassigned task types"),
			t(lang, "Kitsuにあるがルーティングされていないタスクタイプ", "Task types in Kitsu not yet routed to a channel"),
			len(unassignedTaskTypes),
			esc(project.KitsuProjectID),
			t(lang, "タスクタイプ", "Task type"), ttOpts.String(),
			t(lang, "送信先チャンネル", "Target channel"), chOpts.String(),
			t(lang, "割り当て", "Assign"),
		))
	}

	return fmt.Sprintf(`
<div class="accordion-body">
  <div class="section-stack">
    <div class="project-panel-head">
      <div>
        <div class="eyebrow">%s</div>
        <div class="tile-sub">%s</div>
      </div>
      <div class="project-panel-meta">
        <span class="tag">%s</span>
        <span class="tag">%s: %s</span>
        <form method="POST" class="delete-form" data-confirm="%s" data-require-text="%s">
          <input type="hidden" name="action" value="delete">
          <input type="hidden" name="kitsu_project_id" value="%s">
          <input type="hidden" name="project_name" value="%s">
          <button type="submit" class="btn-danger">%s</button>
        </form>
      </div>
    </div>
    <div class="section-card glass">
      <h3>%s</h3>
      <div class="channel-groups">%s</div>
    </div>
    %s
    %s
  </div>
</div>`,
		t(lang, "プロジェクト管理", "Project controls"),
		t(lang, "展開後にチャンネルと通知導線を編集できます。", "Manage channels and notification routing after expanding."),
		esc(project.ProjectType),
		t(lang, "言語", "Language"),
		esc(displayProjectLang(projectLang)),
		esc(t(lang, project.Name+" を完全に削除しますか？この操作は取り消せません。", "Permanently delete "+project.Name+"? This cannot be undone.")),
		t(lang, "削除", "delete"),
		esc(project.KitsuProjectID),
		esc(project.Name),
		t(lang, "削除", "Delete"),
		t(lang, "現在のチャンネル", "Current channels"),
		channelsHTML.String(),
		addChannelHTML.String(),
		unassignedHTML.String(),
	)
}

func requiredWordForLang(lang string) string {
	return t(lang, "削除", "delete")
}

func displayProjectLang(lang string) string {
	if strings.EqualFold(lang, "ja") {
		return "jp"
	}
	return lang
}

func renderForm(r *http.Request, projects []model.Project, kitsuProjects []KitsuProject, setupDone map[string]bool, db *gorm.DB, kitsuHostStored, kitsuEmailStored, detectedHost, fallbackGuildID string) string {
	lang := currentLang(r)

	var projectOptions strings.Builder
	projectOptions.WriteString(`<option value="">` + t(lang, "プロジェクトを選択", "Select project") + `</option>`)
	if len(kitsuProjects) == 0 {
		projectOptions.WriteString(`<option value="" disabled>` + t(lang, "Kitsuプロジェクトを取得できませんでした。", "No Kitsu projects were loaded.") + `</option>`)
	}
	for _, project := range kitsuProjects {
		if setupDone[project.ID] {
			projectOptions.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s" disabled>%s (%s)</option>`, project.ID, project.Name, project.Name, t(lang, "設定済み", "already set up")))
		} else {
			projectOptions.WriteString(fmt.Sprintf(`<option value="%s" data-name="%s">%s</option>`, project.ID, project.Name, project.Name))
		}
	}

	// Determine setup step for progress indicator using only existing lightweight signals.
	step1Done := kitsuHostStored != "" && kitsuEmailStored != ""
	projectRoutingDone := len(projects) > 0
	guildStepDone := false
	for _, project := range projects {
		if strings.TrimSpace(project.DiscordGuildID) != "" {
			guildStepDone = true
			break
		}
	}
	stepClass := func(done, active bool) string {
		if done {
			return "done"
		}
		if active {
			return "active"
		}
		return "pending"
	}
	step1Class := stepClass(step1Done, !step1Done)
	step2Class := stepClass(projectRoutingDone, step1Done && !projectRoutingDone)
	step3Class := stepClass(guildStepDone, projectRoutingDone && !guildStepDone)
	step4Class := stepClass(false, guildStepDone)
	stepIndicator := fmt.Sprintf(`<div class="setup-steps">
  <div class="setup-step %s"><span class="step-num">1</span><span class="step-label">%s</span></div>
  <div class="step-connector"></div>
  <div class="setup-step %s"><span class="step-num">2</span><span class="step-label">%s</span></div>
  <div class="step-connector"></div>
  <div class="setup-step %s"><span class="step-num">3</span><span class="step-label">%s</span></div>
  <div class="step-connector"></div>
  <div class="setup-step %s"><span class="step-num">4</span><span class="step-label">%s</span></div>
</div>`,
		step1Class,
		t(lang, "Bot / Runtime", "Bot / Runtime"),
		step2Class,
		t(lang, "プロジェクトルーティング", "Project Routing"),
		step3Class,
		t(lang, "Discord Server / Guild", "Discord Server / Guild"),
		step4Class,
		t(lang, "最終ヘルス確認", "Final Health Check"),
	)

	workflowCard := func(stepNum int, stateClass, title, body, status, href, cta string, emphasized bool) string {
		emphasisClass := ""
		if emphasized {
			emphasisClass = " workflow-card-emphasis"
		}
		ctaHTML := ""
		if href != "" && cta != "" {
			ctaHTML = `<div class="workflow-card-actions"><a class="btn-ghost" href="` + href + `">` + cta + `</a></div>`
		}
		return `<div class="workflow-card workflow-card-` + stateClass + emphasisClass + `">` +
			`<div class="workflow-card-head"><span class="workflow-step-badge">` + fmt.Sprintf("%d", stepNum) + `</span><span class="status-pill ` + map[string]string{"done": "ok", "active": "warn", "pending": "bad"}[stateClass] + `">` + status + `</span></div>` +
			`<h3>` + title + `</h3>` +
			`<p class="hint">` + body + `</p>` +
			ctaHTML +
			`</div>`
	}

	workflowOverview := `<div class="section-card glass workflow-overview">` +
		`<div class="page-heading workflow-overview-head"><div><div class="eyebrow">PROJECT MANAGEMENT</div><h2 style="margin:6px 0 0">` + t(lang, "運用フロー", "Operator workflow") + `</h2><p class="hint" style="margin:8px 0 0">` + t(lang, "このページは Bot / Runtime の確認から Project Routing、Guild、通知確認までを順番に進める運用画面です。", "Use this page to move in order from Bot / Runtime review to Project Routing, Guild assignment, and notification verification.") + `</p></div></div>` +
		stepIndicator +
		`<div class="workflow-grid">` +
		workflowCard(
			1,
			step1Class,
			t(lang, "Bot / Runtime", "Bot / Runtime"),
			t(lang, "通知に使う共有設定の状態を確認します。変更や見直しは Bot設定で行います。", "Review the shared settings used for notifications. Make changes in Bot Settings."),
			map[string]string{"done": t(lang, "準備完了", "Ready"), "active": t(lang, "現在の手順", "Current"), "pending": t(lang, "待機中", "Pending")}[step1Class],
			appendLang("/bot/admin/bot", lang),
			t(lang, "Bot設定", "Bot Settings"),
			step1Class == "active",
		) +
		workflowCard(
			2,
			step2Class,
			t(lang, "Project Routing", "Project Routing"),
			t(lang, "Kitsu project ごとの Discord category / channel / webhook routing をここで作成します。", "Create Discord category / channel / webhook routing for each Kitsu project here."),
			map[string]string{"done": t(lang, "作成済み", "Configured"), "active": t(lang, "主作業", "Main task"), "pending": t(lang, "次の手順", "Next")}[step2Class],
			"",
			"",
			step2Class == "active",
		) +
		workflowCard(
			3,
			step3Class,
			t(lang, "Discord Server / Guild", "Discord Server / Guild"),
			t(lang, "Routing 作成後に、各 project を送信先の Discord Server / Guild に割り当てます。", "After routing is created, assign each project to its destination Discord Server / Guild."),
			map[string]string{"done": t(lang, "割り当て済み", "Assigned"), "active": t(lang, "次の手順", "Next"), "pending": t(lang, "待機中", "Pending")}[step3Class],
			appendLang("/bot/admin/projects", lang),
			t(lang, "プロダクション連携管理", "Production Connection Management"),
			step3Class == "active",
		) +
		workflowCard(
			4,
			step4Class,
			t(lang, "最終ヘルス確認", "Final Health Check"),
			t(lang, "最後にヘルスで通知先と runtime の状態を確認します。正常であれば、このセットアップは完了です。", "Finally, use Health to confirm the notification destination and runtime status. If everything is healthy, setup is complete."),
			map[string]string{"done": t(lang, "確認完了", "Confirmed"), "active": t(lang, "最終確認", "Final check"), "pending": t(lang, "最後のステップ", "Last step")}[step4Class],
			appendLang("/bot/admin/health", lang),
			t(lang, "ヘルスで最終確認", "Review Health"),
			step4Class == "active",
		) +
		`</div></div>`

	var botCard strings.Builder
	var fallbackBotCard string
	if step1Done {
		botCard.WriteString(`<div class="section-card glass workflow-status-card">` + `<div class="page-heading"><div><div class="eyebrow">STEP 1</div><h3 style="margin:6px 0 0">` + t(lang, "Bot / Runtime", "Bot / Runtime") + `</h3><p class="hint" style="margin:8px 0 0">` + t(lang, "通知に使う共有 Bot / Runtime は準備済みです。Project Routing を進める前提は整っています。", "The shared Bot / Runtime used for notifications is ready. The prerequisite for Project Routing is in place.") + `</p></div><span class="status-pill ok">` + t(lang, "準備完了", "Ready") + `</span></div><div class="metric-grid"><div class="metric-card"><div class="metric-label">Kitsu hostname</div><div class="metric-value metric-value-host"><code>` + esc(kitsuHostStored) + `</code></div></div></div><div class="button-row"><a class="btn" href="` + appendLang("/bot/admin/bot", lang) + `">` + t(lang, "Bot設定を開く", "Open Bot Settings") + `</a></div></div>`)
	} else {
		botCard.WriteString(`<div class="section-card glass workflow-status-card">` + `<div class="page-heading"><div><div class="eyebrow">STEP 1</div><h3 style="margin:6px 0 0">` + t(lang, "Bot / Runtime", "Bot / Runtime") + `</h3><p class="hint" style="margin:8px 0 0">` + t(lang, "Project Routing を始める前に、共有 Bot / Runtime を Bot設定で確認してください。", "Before starting Project Routing, review the shared Bot / Runtime in Bot Settings.") + `</p></div><span class="status-pill bad">` + t(lang, "未準備", "Not ready") + `</span></div>` +
			`<div class="notice-box">💡 ` + t(lang, "Kitsu hostname は現在の公開ホストから自動設定されます。共有設定を整えたら、このページに戻って次の step を進めます。", "The Kitsu hostname is detected from the current public host. After updating the shared settings, return here for the next step.") + `</div>` +
			`<div class="metric-grid"><div class="metric-card"><div class="metric-label">Detected host</div><div class="metric-value metric-value-host"><code>` + esc(detectedHost) + `</code></div></div></div>` +
			`<div class="button-row"><a class="btn" href="` + appendLang("/bot/admin/bot", lang) + `">` + t(lang, "Bot設定を開く", "Open Bot Settings") + `</a></div></div>`)
	}

	// Build template JSON for channel preview JS
	var templateJSON strings.Builder
	templateJSON.WriteString("{")
	first := true
	for tmplKey, tmpl := range Templates {
		if !first {
			templateJSON.WriteString(",")
		}
		first = false
		templateJSON.WriteString(fmt.Sprintf("%q:[", tmplKey))
		for i, ch := range tmpl.Channels {
			if i > 0 {
				templateJSON.WriteString(",")
			}
			templateJSON.WriteString(fmt.Sprintf(`{"nameJA":%q,"nameEN":%q,"taskType":%q}`,
				ch.NameJA, ch.NameEN, ch.TaskType))
		}
		templateJSON.WriteString("]")
	}
	templateJSON.WriteString("}")

	previewLabelsJSON := fmt.Sprintf(`{"willCreate":%q}`,
		t(lang, "このプロジェクトタイプで作成されるチャンネル:", "Channels that will be created for this project type:"))

	sotBadge := fmt.Sprintf(`<div class="sot-badge">%s<strong>%s</strong> %s</div>`,
		`<span class="sot-icon">💡</span> `,
		t(lang, "Kitsu は Single Source of Truth です。", "Kitsu is the Single Source of Truth."),
		t(lang, "Discord は通知専用です。タスクの変更は Kitsu 側で行ってください。", "Discord is for notifications only. Make all task changes in Kitsu."),
	)

	guildNextStatus := `<span class="status-pill bad">` + t(lang, "次の手順", "Next step") + `</span>`
	if guildStepDone {
		guildNextStatus = `<span class="status-pill ok">` + t(lang, "割り当て済み", "Assigned") + `</span>`
	}
	testNextStatus := `<span class="status-pill warn">` + t(lang, "確認待ち", "Review") + `</span>`

	projectRoutingStatus := `<span class="status-pill warn">` + t(lang, "主作業", "Main task") + `</span>`
	if !step1Done {
		projectRoutingStatus = `<span class="status-pill bad">` + t(lang, "Step 1 の後", "After Step 1") + `</span>`
	} else if projectRoutingDone {
		projectRoutingStatus = `<span class="status-pill ok">` + t(lang, "作成済み", "Configured") + `</span>`
	}
	guildInputHelp := t(lang, "この project の送信先 Discord Server / Guild ID をここで指定します。", "Enter the destination Discord Server / Guild ID for this project here.")
	if strings.TrimSpace(fallbackGuildID) != "" {
		guildInputHelp = t(lang, "この project の送信先 Discord Server / Guild ID をここで指定します。空欄のまま実行すると、互換用の fallback Guild ID が使われます。", "Enter the destination Discord Server / Guild ID for this project here. If you leave this blank, the compatibility fallback Guild ID will be used.")
	}

	guildInputHelp = t(lang, "この project の通知先 Discord Server / Guild ID をここで指定します。", "Enter the Discord Server / Guild ID used as this project's notification destination.")
	body := fmt.Sprintf(`
<div class="section-stack">
  %s
  %s
  %s
  <div class="section-card glass workflow-primary">
    <div class="page-heading" style="margin-bottom:14px">
      <div><div class="eyebrow">STEP 2</div><h3 style="margin:6px 0 0">%s</h3><p class="hint" style="margin:8px 0 0">%s</p></div>
      %s
    </div>
    <div class="section-stack">%s</div>
  </div>
  <div class="section-card glass workflow-routing-form">
    <div class="page-heading" style="margin-bottom:14px">
      <div><div class="eyebrow">STEP 2</div><h3 style="margin:6px 0 0">%s</h3><p class="hint" style="margin:8px 0 0">%s</p></div>
      <span class="status-pill warn">%s</span>
    </div>
    <form method="POST" onsubmit="startSetup(this)">
      <input type="hidden" name="project_name" id="projectNameInput">
      <div class="form-grid">
        <div class="form-span-2">
          <label>%s</label>
          <select name="kitsu_project_id" id="kitsuProjectSelect" required onchange="syncProjectName(this)">%s</select>
        </div>
        <div>
          <label>%s</label>
          <select name="project_type" id="projectTypeSelect" required onchange="updateChannelPreview(this)">
            <option value="">%s</option>
            <option value="cg">CG / VFX</option>
          </select>
        </div>
        <div>
          <label>%s</label>
          <select name="language" id="projectLangSelect" required onchange="updateChannelPreview(document.getElementById('projectTypeSelect'))">
            <option value="ja">%s</option>
            <option value="en">English</option>
          </select>
        </div>
        <div class="form-span-2">
          <label>%s</label>
          <input type="text" name="guild_id" placeholder="123456789012345678" required>
          <p class="field-help">%s</p>
        </div>
      </div>
      <div id="channelPreview" style="display:none;margin-top:16px;"></div>
      <div class="button-row">
        <button type="submit" class="btn" id="setupBtn">%s</button>
      </div>
    </form>
  </div>
  <div class="section-card glass">
    <div class="page-heading" style="margin-bottom:14px">
      <div><h3 style="margin:0">%s</h3><p class="hint" style="margin:6px 0 0">%s</p></div>
      %s
    </div>
    <div class="button-row">
      <a class="btn" href="%s">%s</a>
    </div>
  </div>
  <div class="section-card glass">
    <div class="page-heading" style="margin-bottom:14px">
      <div><h3 style="margin:0">%s</h3><p class="hint" style="margin:6px 0 0">%s</p></div>
      %s
    </div>
    <div class="button-row">
      <a class="btn-ghost" href="%s">%s</a>
    </div>
  </div>
  %s
</div>
<script>
var CHANNEL_TEMPLATES = %s;
var PREVIEW_LABELS = %s;
function updateChannelPreview(sel){
  var preview = document.getElementById('channelPreview');
  if(!preview) return;
  var type = sel ? sel.value : '';
  if(!type){ preview.style.display='none'; return; }
  var langSel = document.getElementById('projectLangSelect');
  var lang = langSel ? langSel.value : 'ja';
  var channels = CHANNEL_TEMPLATES[type];
  if(!channels || !channels.length){ preview.style.display='none'; return; }
  // Group by channel name
  var groups = {}, order = [];
  channels.forEach(function(ch){
    var name = lang === 'en' ? ch.nameEN : ch.nameJA;
    if(!groups[name]){ groups[name]=[];order.push(name); }
    groups[name].push(ch.taskType);
  });
  var html = '<div class="channel-preview-box">';
  html += '<div class="preview-title">' + PREVIEW_LABELS.willCreate + '</div>';
  html += '<div class="preview-channels">';
  order.forEach(function(name){
    html += '<div class="preview-channel"><span class="preview-ch-name">#' + name + '</span>';
    html += '<span class="preview-tasks">' + groups[name].join(', ') + '</span></div>';
  });
  html += '</div></div>';
  preview.innerHTML = html;
  preview.style.display = 'block';
}
function startBotSetup(form){
  if(form.dataset.submitting === '1'){ return; }
  form.dataset.submitting = '1';
  var btn = form.querySelector('#botSetupBtn');
  if(btn){ btn.disabled = true; btn.textContent = %q; }
}
function startSetup(form){
  if(form.dataset.submitting === '1'){ return false; }
  form.dataset.submitting = '1';
  var btn = document.getElementById('setupBtn');
  if(btn){ btn.disabled = true; btn.textContent = %q; }
  form.submit();
  return false;
}
function syncProjectName(sel){
  var opt = sel.options[sel.selectedIndex];
  document.getElementById('projectNameInput').value = opt ? (opt.getAttribute('data-name') || '') : '';
}
document.addEventListener('DOMContentLoaded', function(){
  var sel = document.getElementById('kitsuProjectSelect');
  if(sel){ syncProjectName(sel); }
});
</script>`,
		workflowOverview,
		botCard.String(),
		sotBadge,
		t(lang, "Step 2: 連携済み production を管理", "Step 2: Manage connected productions"),
		t(lang, "既存の production connection 一覧はこのページに埋め込まず、専用の管理ページで見直します。新しい routing の追加は次のフォームから進めてください。", "Review existing production connections in the dedicated management page instead of inside this step. Use the next form when you need to add new routing."),
		projectRoutingStatus,
		appendLang("/bot/admin/projects", lang),
		t(lang, "プロダクション連携管理を開く", "Open Production Connection Management"),
		t(lang, "新規プロジェクトのルーティング作成", "Create New Project Routing"),
		t(lang, "ここがプロジェクト管理の主作業です。project ごとの Discord category / channels / webhooks routing をここで設定します。最初の routing もここから作成します。", "This is the main Project Management task. Configure Discord category / channels / webhooks routing per project here, including the first routing."),
		t(lang, "主作業", "Main task"),
		t(lang, "Kitsuプロジェクト", "Kitsu project"),
		projectOptions.String(),
		t(lang, "プロジェクトタイプ", "Project type"),
		t(lang, "プロジェクトタイプを選択", "Select project type"),
		t(lang, "言語", "Language"),
		t(lang, "日本語", "Japanese"),
		t(lang, "Discord Server / Guild ID", "Discord Server / Guild ID"),
		guildInputHelp,
		t(lang, "セットアップ実行", "Run Setup"),
		t(lang, "Step 3: Discord Server / Guild の割り当て", "Step 3: Assign Discord Server / Guild"),
		t(lang, "Project setup 時に保存した Guild 割り当てや接続情報を見直すときは、プロダクション連携管理 を review / edit 用に使います。", "Use Production Connection Management as the review / edit page when you need to revisit guild assignment or saved connection details from project setup."),
		guildNextStatus,
		appendLang("/bot/admin/projects", lang),
		t(lang, "プロダクション連携管理を開く", "Open Production Connection Management"),
		t(lang, "Step 4: 最終ヘルス確認", "Step 4: Final Health Check"),
		t(lang, "Guild 割り当て後は、ヘルスで通知先と runtime の状態を確認します。正常であれば、このセットアップは完了です。", "After guild assignment, use Health to confirm the notification destination and runtime status. If everything is healthy, setup is complete."),
		testNextStatus,
		appendLang("/bot/admin/health", lang),
		t(lang, "ヘルスで最終確認", "Review Health"),
		fallbackBotCard,
		templateJSON.String(),
		previewLabelsJSON,
		t(lang, "Botアカウント設定中...", "Bot account setup..."),
		t(lang, "設定中...", "Setting up..."),
	)
	return adminPage(lang, t(lang, "プロジェクト管理", "Project Management"), r, body)
}

func renderResult(lang, projectName string, result SetupResult, r *http.Request) string {
	title := t(lang, "セットアップ完了", "Setup Complete")
	color := "#8ecf8b"
	backURL := withLang("/bot/setup", r)

	var retryBadge, durationNote string
	if result.Duration > 0 {
		durationNote = fmt.Sprintf(`<span style="color:var(--muted);font-size:.85rem;margin-left:10px">(%s)</span>`, result.Duration.Round(time.Millisecond))
	}

	var footer string
	if result.OK {
		// Success footer: reversibility notice + delete button + back link + auto-redirect
		kitsuProjectID := r.FormValue("kitsu_project_id")
		deleteForm := ""
		if kitsuProjectID != "" {
			deleteForm = `<form method="POST" class="delete-form" style="display:inline;" data-confirm="` +
				esc(t(lang, projectName+" を完全に削除しますか？この操作は取り消せません。", "Permanently delete "+projectName+"? This cannot be undone.")) + `" data-require-text="` +
				esc(t(lang, "削除", "delete")) + `">` +
				`<input type="hidden" name="action" value="delete">` +
				`<input type="hidden" name="kitsu_project_id" value="` + esc(kitsuProjectID) + `">` +
				`<input type="hidden" name="project_name" value="` + esc(projectName) + `">` +
				`<button type="submit" class="btn-ghost" style="color:#ff6a50;border-color:rgba(255,106,80,.3);">` +
				esc(t(lang, "このプロジェクトを削除", "Delete this project")) + `</button></form>`
		}
		footer = `<p style="text-align:center;color:var(--muted);font-size:.84rem;margin-bottom:6px">` +
			esc(t(lang, "✅ KitsuSync の接続のみ削除します（Discord channel はそのまま残ります）", "✅ Only KitsuSync connection is deleted (Discord channels will remain)")) + `</p>` +
			`<p style="text-align:center;color:#b8b5ae;font-size:.9rem;margin-bottom:12px">` +
			esc(t(lang, "5秒後にプロジェクト管理へ戻ります。", "Returning to Project Management in 5 seconds.")) + `</p>` +
			`<div style="display:flex;gap:12px;justify-content:center;flex-wrap:wrap;">` +
			`<a href="` + backURL + `" class="btn">` + esc(t(lang, "プロジェクト管理へ", "Back to Project Management")) + `</a>` +
			deleteForm + `</div>` +
			`<script>setTimeout(function(){location.href=` + strconv.Quote(backURL) + `;},5000);</script>`
	} else {
		title = t(lang, "セットアップ失敗", "Setup Failed")
		color = "#ff6a50"
		if result.SafeToRetry {
			retryBadge = `<div style="padding:10px 14px;border-radius:10px;background:rgba(142,207,139,.14);border:1px solid rgba(142,207,139,.35);color:#8ecf8b;font-size:.88rem;margin-bottom:14px">` +
				esc(t(lang, "✅ 再試行可能 — ロールバックが完了しています。そのままセットアップを再実行できます。",
					"✅ Safe to retry — rollback completed. You can run setup again immediately.")) + `</div>`
		} else {
			retryBadge = `<div style="padding:10px 14px;border-radius:10px;background:rgba(255,106,80,.13);border:1px solid rgba(255,106,80,.35);color:#ff6a50;font-size:.88rem;margin-bottom:14px">` +
				esc(t(lang, "⚠️ 手動での確認が必要です — Discordのチャンネルが残っている可能性があります。再試行前に確認・削除してください。",
					"⚠️ Manual cleanup needed — Discord channels may still exist. Check and remove them before retrying.")) + `</div>`
		}
		footer = `<a href="` + backURL + `">` + esc(t(lang, "戻る", "Back")) + `</a>`
	}

	// Group log lines by status for clearer error inventory
	var okLines, warnLines, failLines, rolledLines []string
	for _, line := range result.Lines {
		switch {
		case strings.HasPrefix(line, "OK:"):
			okLines = append(okLines, strings.TrimPrefix(line, "OK: "))
		case strings.HasPrefix(line, "WARN:"):
			warnLines = append(warnLines, strings.TrimPrefix(line, "WARN: "))
		case strings.HasPrefix(line, "FAIL:"):
			failLines = append(failLines, strings.TrimPrefix(line, "FAIL: "))
		case strings.HasPrefix(line, "ROLLED BACK:"):
			rolledLines = append(rolledLines, strings.TrimPrefix(line, "ROLLED BACK: "))
		}
	}

	var inventoryHTML strings.Builder
	if len(okLines) > 0 {
		inventoryHTML.WriteString(`<div class="inventory-group">`)
		inventoryHTML.WriteString(`<div class="inventory-label ok">✅ ` + esc(t(lang, "作成完了", "Created")) + `</div>`)
		inventoryHTML.WriteString(`<ul class="inventory-list">`)
		for _, l := range okLines {
			inventoryHTML.WriteString(`<li style="color:#8ecf8b">` + html.EscapeString(l) + `</li>`)
		}
		inventoryHTML.WriteString(`</ul></div>`)
	}
	if len(failLines) > 0 {
		inventoryHTML.WriteString(`<div class="inventory-group">`)
		inventoryHTML.WriteString(`<div class="inventory-label fail">❌ ` + esc(t(lang, "失敗", "Failed")) + `</div>`)
		inventoryHTML.WriteString(`<ul class="inventory-list">`)
		for _, l := range failLines {
			inventoryHTML.WriteString(`<li style="color:#ff6a50;font-weight:600">` + html.EscapeString(l) + `</li>`)
		}
		inventoryHTML.WriteString(`</ul></div>`)
	}
	if len(warnLines) > 0 {
		inventoryHTML.WriteString(`<div class="inventory-group">`)
		inventoryHTML.WriteString(`<div class="inventory-label warn">⚠️ ` + esc(t(lang, "警告", "Warnings")) + `</div>`)
		inventoryHTML.WriteString(`<ul class="inventory-list">`)
		for _, l := range warnLines {
			inventoryHTML.WriteString(`<li style="color:#ffc850">` + html.EscapeString(l) + `</li>`)
		}
		inventoryHTML.WriteString(`</ul></div>`)
	}
	if len(rolledLines) > 0 {
		inventoryHTML.WriteString(`<div class="inventory-group">`)
		inventoryHTML.WriteString(`<div class="inventory-label rolled">↩️ ` + esc(t(lang, "ロールバック済み", "Rolled back")) + `</div>`)
		inventoryHTML.WriteString(`<ul class="inventory-list">`)
		for _, l := range rolledLines {
			inventoryHTML.WriteString(`<li style="color:#b8b5ae;font-style:italic">` + html.EscapeString(l) + `</li>`)
		}
		inventoryHTML.WriteString(`</ul></div>`)
	}

	// Fallback: if no grouping matched, show raw lines
	if inventoryHTML.Len() == 0 {
		inventoryHTML.WriteString(`<ul>`)
		for _, line := range result.Lines {
			inventoryHTML.WriteString(`<li>` + html.EscapeString(line) + `</li>`)
		}
		inventoryHTML.WriteString(`</ul>`)
	}

	sub := projectName + durationNote
body := fmt.Sprintf(`<div class="page-card glass" style="width:100%%;max-width:760px;margin:6vh auto 0"><div class="page-heading"><div><div class="eyebrow">`+t(lang, "プロジェクト管理", "Project Management")+`</div><h1 style="color:%s">%s</h1><p>%s</p></div></div><div class="section-card glass">%s<div class="setup-inventory">%s</div></div><div class="button-row">%s</div></div>`,
		color, esc(title), sub, retryBadge, inventoryHTML.String(), footer)
	return appShell("KitsuSync", "", lang, nil, "", body)
}

// renderDeleteReauthPage renders the re-authentication page shown as step 2 of project deletion.
func renderDeleteReauthPage(lang, pid, projectName string, r *http.Request) string {
	backURL := withLang("/bot/setup", r)
	warning := fmt.Sprintf(
		`<p style="color:#ff6a50;font-weight:600;margin-bottom:8px">⚠️ %s</p>`+
			`<p style="color:var(--muted);font-size:.88rem;margin-bottom:16px">%s</p>`,
		esc(t(lang, projectName+" を完全に削除します。この操作は取り消せません。", "Permanently deleting "+projectName+". This cannot be undone.")),
		esc(t(lang, "Discord カテゴリとすべてのチャンネルも削除されます。Kitsu 管理者の認証情報を入力して確認してください。",
			"The Discord category and all channels will also be deleted. Enter Kitsu admin credentials to confirm.")))
	form := `<form method="POST" class="section-stack">` +
		`<input type="hidden" name="action" value="delete_final">` +
		`<input type="hidden" name="kitsu_project_id" value="` + esc(pid) + `">` +
		`<input type="hidden" name="project_name" value="` + esc(projectName) + `">` +
		`<div class="form-grid">` +
		`<div><label>` + t(lang, "Kitsu 管理者メール", "Kitsu admin email") + `</label>` +
		`<input type="email" name="admin_email" placeholder="admin@studio.local" required autocomplete="username"></div>` +
		`<div><label>` + t(lang, "Kitsu 管理者パスワード", "Kitsu admin password") + `</label>` +
		`<input type="password" name="admin_password" placeholder="Password" required autocomplete="current-password"></div>` +
		`</div>` +
		`<div class="button-row">` +
		`<button type="submit" class="btn-danger">` + esc(t(lang, "完全に削除する", "Delete permanently")) + `</button>` +
		`<a href="` + backURL + `">` + esc(t(lang, "キャンセル", "Cancel")) + `</a>` +
		`</div></form>`
	body := fmt.Sprintf(
		`<div class="page-card glass" style="width:100%%;max-width:760px;margin:6vh auto 0">`+
			`<div class="page-heading"><div><div class="eyebrow">`+t(lang, "プロジェクト管理", "Project Management")+`</div>`+
			`<h1 style="color:#ff6a50">%s</h1></div></div>`+
			`<div class="section-card glass">%s%s</div></div>`,
		esc(t(lang, "プロジェクト削除の確認", "Confirm project deletion")),
		warning, form)
	return appShell("KitsuSync", "", lang, nil, "", body)
}

func renderDeleteResult(lang, projectName string, logs []string) string {
	var lines strings.Builder
	for _, line := range logs {
		lines.WriteString(`<li>` + html.EscapeString(line) + `</li>`)
	}
	return page(lang, t(lang, "削除完了", "Delete Complete"), "#ff6a50", projectName, lines.String(), `<a href="`+appendLang("/bot/setup", lang)+`">`+t(lang, "戻る", "Back")+`</a>`)
}

func page(lang, title, color, sub, listItems, footer string) string {
	body := fmt.Sprintf(`<div class="page-card glass" style="width:100%%;max-width:760px;margin:6vh auto 0"><div class="page-heading"><div><div class="eyebrow">Project Setup</div><h1 style="color:%s">%s</h1><p>%s</p></div></div><div class="section-card glass"><div class="table-wrap"><table><tbody>%s</tbody></table></div></div><div class="button-row">%s</div></div>`,
		color, esc(title), esc(sub), listItems, footer)
	return appShell("KitsuSync", "", lang, nil, "", body)
}

func renderBotSetupSuccess(lang string) string {
	return page(lang, t(lang, "Bot設定完了", "Bot Setup Complete"), "#8ecf8b", t(lang, "Botアカウントを作成し、実行環境へ適用しました。", "The bot account was created and applied to the running environment."), `<li>`+esc(t(lang, "次はプロジェクト管理で project routing を設定してください。", "Continue in Project Management to configure project routing."))+`</li>`, `<a href="`+appendLang("/bot/setup", lang)+`">`+t(lang, "プロジェクト管理へ戻る", "Back to Project Management")+`</a>`)
}

func renderBotSetupError(lang, errMsg string) string {
	return page(lang, t(lang, "Bot設定失敗", "Bot Setup Failed"), "#ff6a50", t(lang, "Botアカウントを作成できませんでした。", "The bot account could not be created."), `<li>`+html.EscapeString(errMsg)+`</li>`, `<a href="`+appendLang("/bot/setup", lang)+`">`+t(lang, "戻る", "Back")+`</a>`)
}
