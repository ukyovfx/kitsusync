package setup

import (
	"app/src/model"
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/gookit/slog"
	"gorm.io/gorm"
)

func DeleteProject(kitsuProjectID, botToken string, db *gorm.DB) ([]string, error) {
	kitsuProjectID = strings.TrimSpace(kitsuProjectID)
	if kitsuProjectID == "" {
		return nil, fmt.Errorf("empty project id")
	}
	unlockApply := lockProductionNotificationApply(kitsuProjectID)
	defer unlockApply()

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

	if err := deleteProjectConnectionOnly(kitsuProjectID, db); err != nil {
		logs = append(logs, "failed to delete project records from database")
		return logs, err
	}
	logs = append(logs, "deleted project records")
	return logs, nil
}

func DeleteProjectConnectionOnly(kitsuProjectID string, db *gorm.DB) error {
	kitsuProjectID = strings.TrimSpace(kitsuProjectID)
	if kitsuProjectID == "" {
		return fmt.Errorf("empty project id")
	}
	unlockApply := lockProductionNotificationApply(kitsuProjectID)
	defer unlockApply()
	return deleteProjectConnectionOnly(kitsuProjectID, db)
}

func deleteProjectConnectionOnlyIfNoWebhooks(kitsuProjectID string, db *gorm.DB) error {
	kitsuProjectID = strings.TrimSpace(kitsuProjectID)
	project := model.FindProjectByKitsuID(db, kitsuProjectID)
	if project == nil {
		return fmt.Errorf("project not found: %s", kitsuProjectID)
	}
	unlockApply := lockProductionNotificationApply(project.KitsuProjectID)
	defer unlockApply()
	project = model.FindProjectByKitsuID(db, kitsuProjectID)
	if project == nil {
		return fmt.Errorf("project not found: %s", kitsuProjectID)
	}
	if remaining := len(model.ListProjectWebhooks(db, kitsuProjectID)); remaining != 0 {
		return fmt.Errorf("project still has %d notification destinations", remaining)
	}
	return deleteProjectConnectionOnly(kitsuProjectID, db)
}

func deleteProjectConnectionOnly(kitsuProjectID string, db *gorm.DB) error {
	return deleteProjectConnectionOnlyForMutation(kitsuProjectID, db, "")
}

func deleteProjectConnectionOnlyForMutation(kitsuProjectID string, db *gorm.DB, allowedMutationToken string) error {
	kitsuProjectID = strings.TrimSpace(kitsuProjectID)
	if kitsuProjectID == "" {
		return fmt.Errorf("empty project id")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		project := model.FindProjectByKitsuID(tx, kitsuProjectID)
		if project == nil {
			return fmt.Errorf("project not found: %s", kitsuProjectID)
		}
		if err := model.AcquireProductionNotificationWriteLock(tx, kitsuProjectID); err != nil {
			return err
		}
		if err := model.EnsureProductionNotificationMutationIdle(tx, kitsuProjectID, allowedMutationToken); err != nil {
			return err
		}
		if err := model.EnsureNoUnresolvedNotificationDeliveries(tx, kitsuProjectID); err != nil {
			return err
		}
		if err := model.EnsureNoUnresolvedManagedChannelDeletions(tx, kitsuProjectID); err != nil {
			return err
		}
		if err := model.DeleteProjectScopedData(tx, project.ID); err != nil {
			return fmt.Errorf("db delete project-scoped data: %w", err)
		}
		if err := model.DeleteProductionOperationalState(tx, kitsuProjectID); err != nil {
			return fmt.Errorf("db delete production routing state: %w", err)
		}
		if err := tx.Where("kitsu_project_id = ?", kitsuProjectID).Delete(&model.ProjectWebhook{}).Error; err != nil {
			return fmt.Errorf("db delete webhooks: %w", err)
		}
		result := tx.Where("kitsu_project_id = ?", kitsuProjectID).Delete(&model.Project{})
		if result.Error != nil {
			return fmt.Errorf("db delete project: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("db delete project: expected one row, removed %d", result.RowsAffected)
		}
		if model.FindProjectByKitsuID(tx, kitsuProjectID) != nil ||
			len(model.ListProjectWebhooks(tx, kitsuProjectID)) != 0 ||
			model.HasProductionOperationalState(tx, kitsuProjectID) {
			return fmt.Errorf("db delete verification failed for project: %s", kitsuProjectID)
		}
		return nil
	})
}

func isReadinessRecoveryAction(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	switch strings.TrimSpace(r.FormValue("action")) {
	case "delete", "delete_final":
		return true
	default:
		return false
	}
}

func Handler(kitsuHost, fallbackGuildID, botToken string, db *gorm.DB, runtimeReady func() bool, onRuntimeConfigured func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)
		if handleFirstTimeConnectionAction(w, r, lang, kitsuHost, botToken, db) {
			return
		}
		if r.Method == http.MethodPost && r.FormValue("action") == "bot_setup" {
			// Human email/password setup is no longer part of the current runtime
			// authentication flow. Keep the old action as a safe redirect for
			// bookmarked legacy forms; token validation happens in Connections.
			http.Redirect(w, r, withLang("/bot/admin/bot/connections?edit=1", r), http.StatusSeeOther)
			return
			/*
				_ = r.ParseForm()
				kitsuHostInput := effectiveRuntimeKitsuEndpoint(db)
				runtimeEmail := strings.TrimSpace(r.FormValue("kitsu_runtime_email"))
				runtimePassword := r.FormValue("kitsu_runtime_password")
				// Accept the old field names for bookmarked/legacy forms, but keep the
				// operation Kitsu-only. This path never creates a Kitsu or Discord bot.
				if runtimeEmail == "" {
					runtimeEmail = strings.TrimSpace(r.FormValue("bot_admin_email"))
				}
				if runtimePassword == "" {
					runtimePassword = r.FormValue("bot_admin_password")
				}
				if runtimeEmail == "" || strings.TrimSpace(runtimePassword) == "" {
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu実行用のメールアドレスとパスワードを入力してください。", "Enter the Kitsu runtime email and password.")))
					return
				}
				if kitsuHostInput == "" {
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsuの接続先を確認できませんでした。Kitsu接続設定を確認してください。", "The Kitsu endpoint could not be determined. Review the Kitsu connection settings.")))
					return
				}
				if err := RecoverRuntimeCredentials(db, kitsuHostInput, runtimeEmail, runtimePassword); err != nil {
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu接続を確認できませんでした。入力内容とKitsuの接続先を確認してください。", "Kitsu authentication could not be verified. Check the credentials and Kitsu endpoint.")+" ("+err.Error()+")"))
					return
				}
				model.SetSetting(db, "kitsu.hostname", kitsuHostInput)
				if onRuntimeConfigured != nil {
					onRuntimeConfigured()
				}
				if runtimeReady == nil || !runtimeReady() {
					fmt.Fprint(w, renderKitsuConnectionError(lang, t(lang, "Kitsu接続を確認できませんでした。設定は保存されていません。", "Kitsu authentication could not be verified. The setup is not complete.")))
					return
				}
				fmt.Fprint(w, renderKitsuConnectionSuccess(lang))
				return
			*/
		}

		if r.Method == http.MethodPost && r.FormValue("action") == "runtime_setup_from_session" {
			// The legacy session action used to create a Kitsu bot account. Runtime
			// credential setup is now an explicit Kitsu-only form operation.
			http.Redirect(w, r, withLang("/bot/admin/bot/connections?edit=1", r), http.StatusSeeOther)
			return
		}

		if runtimeReady != nil && !runtimeReady() && !isReadinessRecoveryAction(r) {
			fmt.Fprint(w, renderSetupRequiredPage(lang, r))
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
			if err := authenticateDeleteAdmin(r.Context(), db, kitsuHost, adminEmail, adminPassword); err != nil {
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

		renderIANewConnection(w, r, db)
	}
}

func authenticateDeleteAdmin(ctx context.Context, db *gorm.DB, kitsuHost, email, password string) error {
	apiOverride := ""
	if db != nil {
		apiOverride = model.GetSetting(db, KitsuAPIBaseURLSettingKey)
	}
	connection, err := ResolveKitsuConnection(ctx, kitsuHost, apiOverride)
	if err != nil {
		return err
	}
	_, role, err := AuthenticateKitsuCredentials(ctx, connection, email, password)
	if err != nil {
		return err
	}
	if !isStudioManagerOrHigher(role) {
		return connectionError("auth_failed")
	}
	return nil
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
		`<a class="btn-ghost" href="` + backURL + `">` + esc(t(lang, "キャンセル", "Cancel")) + `</a>` +
		`<button type="submit" class="btn-danger">` + esc(t(lang, "完全に削除する", "Delete permanently")) + `</button>` +
		`</div></form>`
	body := fmt.Sprintf(
		`<div class="page-card glass" style="width:100%%;max-width:760px;margin:6vh auto 0">`+
			`<div class="page-heading"><div><div class="eyebrow">`+t(lang, "新規連携セットアップ", "New Connection Setup")+`</div>`+
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

func renderKitsuConnectionError(lang string, args ...interface{}) string {
	errMsg := "Kitsu connection could not be verified."
	if len(args) > 0 {
		if message, ok := args[len(args)-1].(string); ok && message != "" {
			errMsg = message
		}
	}
	return page(lang, t(lang, "Kitsu接続を確認できませんでした", "Kitsu connection could not be verified"), "#ff6a50", t(lang, "Bot tokenを確認し、接続設定からもう一度試してください。", "Check the Bot token and try again from Connections."), `<li>`+html.EscapeString(errMsg)+`</li>`, `<a href="`+appendLang("/bot/admin/bot/connections?edit=1", lang)+`">`+t(lang, "接続設定へ戻る", "Back to Connections")+`</a>`)
}
