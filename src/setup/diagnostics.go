package setup

import (
	"app/src/model"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

type diagCheck struct {
	Label  string
	Status string // "ok" | "warn" | "fail"
	Detail string
	Fix    string
}

type testNotificationState struct {
	Status        string
	Summary       string
	Detail        string
	Fix           string
	CanSend       bool
	TargetProject string
	APIPath       string
}

// DiagnosticsHandler runs environment and delivery-readiness checks on demand.
func DiagnosticsHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)

		kitsuHost, botToken, guildID, webhookURL := refreshCreds()
		checks := runDiagnostics(lang, kitsuHost, botToken, guildID, webhookURL, db)
		testState := buildTestNotificationState(lang, db, diagnosticsAPIBase(r)+"/api/setup/test-notification")

		allOK := testState.Status == "ok"
		anyFail := testState.Status == "fail"
		for _, c := range checks {
			if c.Status != "ok" {
				allOK = false
			}
			if c.Status == "fail" {
				anyFail = true
			}
		}

		var summary string
		switch {
		case allOK:
			summary = `<div class="diag-banner ok">` + esc(t(lang,
				"Runtime と通知確認を含めて、現在の KitsuSync 準備状態は良好です。",
				"KitsuSync currently looks ready, including runtime health and notification verification.",
			)) + `</div>`
		case anyFail:
			summary = `<div class="diag-banner fail">` + esc(t(lang,
				"実運用前に解決が必要な項目があります。赤い項目から確認してください。",
				"There are blockers to resolve before relying on this setup. Start with the red items.",
			)) + `</div>`
		default:
			summary = `<div class="diag-banner warn">` + esc(t(lang,
				"Runtime は動いていますが、運用前に確認しておきたい項目があります。",
				"The runtime is working, but there are still items worth confirming before relying on it.",
			)) + `</div>`
		}

		var rows strings.Builder
		for _, c := range checks {
			rows.WriteString(renderDiagRow(lang, c))
		}

		rerunURL := withLang("/bot/admin/diagnostics", r)
		body := fmt.Sprintf(`
<style>
.diag-banner{padding:14px 20px;border-radius:var(--radius-md);margin-bottom:18px;font-weight:600}
.diag-banner.ok{background:rgba(142,207,139,.18);border:1px solid rgba(142,207,139,.4);color:#8ecf8b}
.diag-banner.warn{background:rgba(255,200,80,.12);border:1px solid rgba(255,200,80,.35);color:#ffc850}
.diag-banner.fail{background:rgba(255,106,80,.14);border:1px solid rgba(255,106,80,.38);color:#ff6a50}
.diag-card{display:grid;gap:16px}
.diag-row td{color:var(--text)}
.diag-row.diag-warn td{color:#ffc850}
.diag-row.diag-fail td{color:#ff6a50}
.diag-icon{width:58px;padding-right:14px;white-space:nowrap}
.diag-mark{display:inline-flex;align-items:center;justify-content:center;width:30px;height:30px;border-radius:999px;font-size:15px;font-weight:700;border:1px solid rgba(255,255,255,.12);background:rgba(255,255,255,.04);color:var(--text)}
.diag-mark.ok{color:#8ecf8b;border-color:rgba(142,207,139,.28);background:rgba(142,207,139,.08)}
.diag-mark.warn{color:#ffc850;border-color:rgba(255,200,80,.3);background:rgba(255,200,80,.08)}
.diag-mark.fail{color:#ff6a50;border-color:rgba(255,106,80,.28);background:rgba(255,106,80,.08)}
.diag-detail{color:var(--muted);font-size:.875rem;margin-top:4px;line-height:1.6}
.diag-fix{margin-top:8px;padding:8px 10px;border-radius:12px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);font-size:.82rem;color:var(--muted-2);line-height:1.55}
.diag-state-card{padding:18px;border-radius:22px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08)}
.diag-state-head{display:flex;justify-content:space-between;gap:12px;align-items:flex-start;flex-wrap:wrap}
.diag-state-head h3{margin:0}
.diag-state-body{color:var(--muted);line-height:1.65}
.diag-state-body p{margin:10px 0 0}
.diag-state-note{margin-top:8px;padding:8px 10px;border-radius:12px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);font-size:.82rem;color:var(--muted-2)}
</style>
%s
<div class="diag-card">
  %s
  <div class="section-card glass">
    <div class="table-wrap">
      <table><tbody>%s</tbody></table>
    </div>
  </div>
</div>
<div class="button-row">
  <a class="btn" href="%s">%s</a>
  <a class="btn-ghost" href="%s">%s</a>
</div>`,
			summary,
			renderTestNotificationCard(lang, testState),
			rows.String(),
			rerunURL, esc(t(lang, "再確認", "Re-run checks")),
			withLang("/bot/admin", r), esc(t(lang, "管理画面へ", "Back to Admin")),
		)

		fmt.Fprint(w, adminPage(lang, t(lang, "環境診断", "Environment Diagnostics"), r, body))
	}
}

func runDiagnostics(lang, kitsuHost, botToken, guildID, webhookURL string, db *gorm.DB) []diagCheck {
	client := &http.Client{Timeout: 8 * time.Second}
	var checks []diagCheck

	botSettingsFix := t(lang,
		"Bot設定で共有 Bot Token を見直して保存し、再度 Diagnostics を実行してください。",
		"Review the shared bot token in Bot Settings, save it, and rerun Diagnostics.",
	)
	configureBotSettingsFix := t(lang,
		"Bot設定で共有 Bot Token を設定してください。",
		"Configure the shared bot token in Bot Settings.",
	)
	kitsuSettingsFix := t(lang,
		"Bot設定で Kitsu Runtime 接続を見直し、必要なら recent runtime logs も確認してください。",
		"Review the Kitsu runtime connection in Bot Settings and, if needed, confirm the recent runtime logs as well.",
	)
	blockedByBotToken := t(lang,
		"Discord Bot Token の確認が終わるまでこの確認は保留されます。",
		"This check is blocked until the Discord bot token is valid.",
	)
	blockedByMissingBotToken := t(lang,
		"Discord Bot Token が設定されるまでこの確認は保留されます。",
		"This check is blocked until a Discord bot token is configured.",
	)

	if kitsuHost == "" {
		checks = append(checks, diagCheck{
			Label:  "Kitsu hostname",
			Status: "fail",
			Detail: "No hostname configured.",
			Fix:    t(lang, "Bot設定で Kitsu hostname を確認してください。", "Review the Kitsu hostname in Bot Settings."),
		})
	} else {
		checks = append(checks, diagCheck{
			Label:  "Kitsu hostname",
			Status: "ok",
			Detail: kitsuHost,
		})
	}

	if kitsuHost != "" {
		pingURL := strings.TrimRight(kitsuHost, "/") + "/api/"
		resp, err := client.Get(pingURL)
		if err != nil {
			checks = append(checks, diagCheck{
				Label:  "Kitsu server reachable",
				Status: "fail",
				Detail: "HTTP request failed: " + err.Error(),
				Fix:    t(lang, "Bot設定の Kitsu hostname を確認し、Kitsu runtime が起動しているか見直してください。", "Confirm the Kitsu hostname in Bot Settings and verify that the Kitsu runtime is reachable."),
			})
		} else {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				checks = append(checks, diagCheck{
					Label:  "Kitsu server reachable",
					Status: "ok",
					Detail: fmt.Sprintf("HTTP %d - server responded.", resp.StatusCode),
				})
			} else {
				checks = append(checks, diagCheck{
					Label:  "Kitsu server reachable",
					Status: "warn",
					Detail: fmt.Sprintf("HTTP %d - server may be starting up.", resp.StatusCode),
					Fix:    kitsuSettingsFix,
				})
			}
		}
	}

	checks = append(checks, buildKitsuRuntimeCheck(lang, client, kitsuHost, kitsuSettingsFix))

	botTokenMissing := strings.TrimSpace(botToken) == ""
	botTokenBlocked := false
	if botTokenMissing {
		checks = append(checks, diagCheck{
			Label:  "Discord bot token",
			Status: "fail",
			Detail: "No bot token configured.",
			Fix:    configureBotSettingsFix,
		})
	} else {
		checks = append(checks, diagCheck{
			Label:  "Discord bot token",
			Status: "ok",
			Detail: "Token configured (hidden).",
		})
	}

	botUserID := ""
	if !botTokenMissing {
		body, status, err := botDo("GET", discordAPI+"/users/@me", nil, botToken)
		if err != nil {
			checks = append(checks, diagCheck{
				Label:  "Discord bot valid",
				Status: "fail",
				Detail: "Request failed: " + err.Error(),
				Fix:    "Check network connectivity to discord.com.",
			})
		} else if status == 200 {
			var result struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			}
			if json.Unmarshal(body, &result) == nil {
				botUserID = result.ID
				checks = append(checks, diagCheck{
					Label:  "Discord bot valid",
					Status: "ok",
					Detail: fmt.Sprintf("Bot user: %s (ID: %s)", result.Username, result.ID),
				})
			} else {
				checks = append(checks, diagCheck{
					Label:  "Discord bot valid",
					Status: "ok",
					Detail: "Bot token accepted.",
				})
			}
		} else if status == 401 {
			botTokenBlocked = true
			checks = append(checks, diagCheck{
				Label:  "Discord bot valid",
				Status: "fail",
				Detail: "Token rejected (HTTP 401 Unauthorized).",
				Fix:    botSettingsFix,
			})
		} else {
			checks = append(checks, diagCheck{
				Label:  "Discord bot valid",
				Status: "warn",
				Detail: fmt.Sprintf("Unexpected response: HTTP %d.", status),
				Fix:    "Check Discord API status at discordstatus.com.",
			})
		}
	}

	if strings.TrimSpace(guildID) == "" {
		checks = append(checks, diagCheck{
			Label:  "Discord guild fallback",
			Status: "ok",
			Detail: t(lang, "未設定です。通常は project-level Discord ID を使うため、この fallback は任意です。", "Not set. This fallback is optional because project-level Discord IDs are the normal path."),
		})
	} else {
		checks = append(checks, diagCheck{
			Label:  "Discord guild fallback",
			Status: "ok",
			Detail: t(lang, "互換用 fallback として設定されています。通常の通知経路は project-level Discord ID / webhook です。", "Configured as a compatibility fallback. The normal notification path uses project-level Discord IDs and webhooks."),
		})
	}

	if strings.TrimSpace(guildID) != "" {
		if botTokenBlocked {
			checks = append(checks, diagCheck{Label: "Discord guild accessible", Status: "warn", Detail: blockedByBotToken, Fix: botSettingsFix})
			checks = append(checks, diagCheck{Label: "Discord permissions (channels)", Status: "warn", Detail: blockedByBotToken, Fix: botSettingsFix})
			checks = append(checks, diagCheck{Label: "Discord permissions (webhooks)", Status: "warn", Detail: blockedByBotToken, Fix: botSettingsFix})
		} else if botTokenMissing {
			checks = append(checks, diagCheck{Label: "Discord guild accessible", Status: "warn", Detail: blockedByMissingBotToken, Fix: configureBotSettingsFix})
			checks = append(checks, diagCheck{Label: "Discord permissions (channels)", Status: "warn", Detail: blockedByMissingBotToken, Fix: configureBotSettingsFix})
			checks = append(checks, diagCheck{Label: "Discord permissions (webhooks)", Status: "warn", Detail: blockedByMissingBotToken, Fix: configureBotSettingsFix})
		} else {
			_, status, err := botDo("GET", discordAPI+"/guilds/"+guildID, nil, botToken)
			if err != nil {
				checks = append(checks, diagCheck{
					Label:  "Discord guild accessible",
					Status: "fail",
					Detail: "Request failed: " + err.Error(),
				})
			} else if status == 200 {
				checks = append(checks, diagCheck{
					Label:  "Discord guild accessible",
					Status: "ok",
					Detail: "Bot is a member of the guild.",
				})
			} else if status == 403 {
				checks = append(checks, diagCheck{
					Label:  "Discord guild accessible",
					Status: "fail",
					Detail: "HTTP 403 - bot is not a member of this guild.",
					Fix:    "Invite the bot to your server using the OAuth2 URL from the Discord Developer Portal. Required scopes: bot. Required permissions: Manage Channels, Manage Webhooks.",
				})
			} else if status == 404 {
				checks = append(checks, diagCheck{
					Label:  "Discord guild accessible",
					Status: "fail",
					Detail: "HTTP 404 - Guild ID not found.",
					Fix:    "Verify the Discord server ID in Project Management or Bot Settings.",
				})
			} else {
				checks = append(checks, diagCheck{
					Label:  "Discord guild accessible",
					Status: "warn",
					Detail: fmt.Sprintf("HTTP %d - unexpected response.", status),
				})
			}

			_, status, err = botDo("GET", discordAPI+"/guilds/"+guildID+"/channels", nil, botToken)
			if err != nil {
				checks = append(checks, diagCheck{
					Label:  "Discord permissions (channels)",
					Status: "warn",
					Detail: "Could not check: " + err.Error(),
				})
			} else if status == 200 {
				checks = append(checks, diagCheck{
					Label:  "Discord permissions (channels)",
					Status: "ok",
					Detail: "Bot can list channels in the guild.",
				})
			} else if status == 403 {
				checks = append(checks, diagCheck{
					Label:  "Discord permissions (channels)",
					Status: "fail",
					Detail: "HTTP 403 - bot cannot list channels.",
					Fix:    "In Discord Developer Portal -> OAuth2, add Manage Channels and Manage Webhooks, then re-invite the bot if needed.",
				})
			} else {
				checks = append(checks, diagCheck{
					Label:  "Discord permissions (channels)",
					Status: "warn",
					Detail: fmt.Sprintf("HTTP %d.", status),
				})
			}

			if botUserID != "" {
				body, mStatus, mErr := botDo("GET", discordAPI+"/guilds/"+guildID+"/members/"+botUserID, nil, botToken)
				if mErr == nil && mStatus == 200 {
					var member struct {
						Permissions string `json:"permissions"`
					}
					if json.Unmarshal(body, &member) == nil && member.Permissions != "" {
						var perms uint64
						fmt.Sscanf(member.Permissions, "%d", &perms)
						const manageWebhooks = uint64(1 << 29)
						const manageChannels = uint64(1 << 4)
						if perms&manageWebhooks != 0 && perms&manageChannels != 0 {
							checks = append(checks, diagCheck{
								Label:  "Discord permissions (webhooks)",
								Status: "ok",
								Detail: "MANAGE_CHANNELS and MANAGE_WEBHOOKS confirmed.",
							})
						} else {
							missing := []string{}
							if perms&manageChannels == 0 {
								missing = append(missing, "MANAGE_CHANNELS")
							}
							if perms&manageWebhooks == 0 {
								missing = append(missing, "MANAGE_WEBHOOKS")
							}
							checks = append(checks, diagCheck{
								Label:  "Discord permissions (webhooks)",
								Status: "fail",
								Detail: "Missing permissions: " + strings.Join(missing, ", "),
								Fix:    "In Discord Developer Portal -> Bot, grant these permissions and re-invite the bot.",
							})
						}
					} else {
						checks = append(checks, diagCheck{
							Label:  "Discord permissions (webhooks)",
							Status: "warn",
							Detail: "Could not read permission bits from member response.",
							Fix:    t(lang, "Bot設定と Discord サーバー側の権限を見直してください。", "Review Bot Settings and confirm the Discord server permissions manually."),
						})
					}
				} else {
					checks = append(checks, diagCheck{
						Label:  "Discord permissions (webhooks)",
						Status: "warn",
						Detail: "Could not retrieve bot member info to verify permissions.",
						Fix:    t(lang, "Bot設定と Discord サーバー側の権限を見直してください。", "Review Bot Settings and confirm the Discord server permissions manually."),
					})
				}
			}
		}
	}

	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master").Scan(&count).Error; err != nil {
		checks = append(checks, diagCheck{
			Label:  "Database (SQLite)",
			Status: "fail",
			Detail: "Query failed: " + err.Error(),
			Fix:    "Check that the data/ volume is mounted and not corrupted.",
		})
	} else {
		checks = append(checks, diagCheck{
			Label:  "Database (SQLite)",
			Status: "ok",
			Detail: "Database is responsive.",
		})
	}

	testPath := "./data/.diag_write_test"
	if err := os.WriteFile(testPath, []byte("ok"), 0600); err != nil {
		checks = append(checks, diagCheck{
			Label:  "data/ directory writable",
			Status: "fail",
			Detail: "Write test failed: " + err.Error(),
			Fix:    "Ensure the data/ directory is mounted with write permissions in docker-compose.yml.",
		})
	} else {
		os.Remove(testPath)
		checks = append(checks, diagCheck{
			Label:  "data/ directory writable",
			Status: "ok",
			Detail: "Write test passed.",
		})
	}

	return checks
}

func buildKitsuRuntimeCheck(lang string, client *http.Client, kitsuHost, kitsuSettingsFix string) diagCheck {
	snap := Stats.Snapshot()
	pollingHealthy := !snap.LastPollTime.IsZero() && snap.LastPollErr == ""
	recentPolling := pollingHealthy && time.Since(snap.LastPollTime) <= 5*time.Minute

	label := t(lang, "Kitsu runtime 確認", "Kitsu runtime check")
	jwtToken := strings.TrimSpace(os.Getenv("KitsuJWTToken"))
	if kitsuHost == "" {
		return diagCheck{
			Label:  label,
			Status: "fail",
			Detail: t(lang, "Kitsu hostname が未設定のため runtime 状態を確認できません。", "Kitsu runtime could not be checked because the hostname is missing."),
			Fix:    kitsuSettingsFix,
		}
	}

	authStatus := 0
	authErr := ""
	if jwtToken != "" {
		authURL := strings.TrimRight(kitsuHost, "/") + "/api/auth/user"
		req, err := http.NewRequest("GET", authURL, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
			resp, err := client.Do(req)
			if err != nil {
				authErr = err.Error()
			} else {
				authStatus = resp.StatusCode
				resp.Body.Close()
			}
		}
	}

	if recentPolling {
		detail := t(lang,
			"Runtime polling は正常です。最終 poll は %s 前で、直近の task 数は %d 件です。",
			"Runtime polling is healthy. The last poll was %s ago with %d tasks in the most recent cycle.",
		)
		detail = fmt.Sprintf(detail, formatDiagAge(snap.LastPollTime), snap.LastPollTaskCount)
		switch {
		case authStatus == 200:
			detail += " " + t(lang, "Direct auth check も成功しました。", "The direct auth check also succeeded.")
		case authStatus == 404:
			detail += " " + t(lang, "auth endpoint は HTTP 404 でしたが、runtime polling は継続できています。", "The auth endpoint returned HTTP 404, but runtime polling is still healthy.")
		case authErr != "":
			detail += " " + t(lang, "Direct auth check は確認できませんでしたが、runtime polling は動作中です。", "The direct auth check could not be confirmed, but runtime polling is still active.")
		}
		return diagCheck{Label: label, Status: "ok", Detail: detail}
	}

	if pollingHealthy {
		return diagCheck{
			Label:  label,
			Status: "warn",
			Detail: fmt.Sprintf(t(lang, "Runtime polling は成功していますが、最終 poll は %s 前です。", "Runtime polling has succeeded, but the last poll was %s ago."), formatDiagAge(snap.LastPollTime)),
			Fix:    kitsuSettingsFix,
		}
	}

	if snap.LastPollErr != "" {
		return diagCheck{
			Label:  label,
			Status: "fail",
			Detail: t(lang, "直近の runtime polling は失敗しています: ", "The most recent runtime poll failed: ")+snap.LastPollErr,
			Fix:    kitsuSettingsFix,
		}
	}

	if authStatus == 200 {
		return diagCheck{
			Label:  label,
			Status: "ok",
			Detail: t(lang, "Direct auth check は成功しました。Runtime polling はまだ記録されていません。", "The direct auth check succeeded. Runtime polling has not been recorded yet."),
		}
	}

	if authStatus == 404 {
		return diagCheck{
			Label:  label,
			Status: "warn",
			Detail: t(lang, "auth endpoint は HTTP 404 でした。runtime polling の記録もまだないため、動作確認を続けてください。", "The auth endpoint returned HTTP 404. There is also no recorded runtime poll yet, so keep verifying the runtime."),
			Fix:    kitsuSettingsFix,
		}
	}

	if authErr != "" {
		return diagCheck{
			Label:  label,
			Status: "warn",
			Detail: t(lang, "Direct auth check を確認できませんでした: ", "The direct auth check could not be confirmed: ")+authErr,
			Fix:    kitsuSettingsFix,
		}
	}

	if jwtToken == "" {
		return diagCheck{
			Label:  label,
			Status: "fail",
			Detail: t(lang, "有効な Kitsu runtime session が見つからず、recent polling の記録もありません。", "No active Kitsu runtime session was found, and there is no recent polling record."),
			Fix:    kitsuSettingsFix,
		}
	}

	return diagCheck{
		Label:  label,
		Status: "warn",
		Detail: t(lang, "Direct auth check と runtime polling の状態をまだ確定できていません。", "The direct auth check and runtime polling state could not be confirmed yet."),
		Fix:    kitsuSettingsFix,
	}
}

func buildTestNotificationState(lang string, db *gorm.DB, apiPath string) testNotificationState {
	state := testNotificationState{
		Status:  "warn",
		Summary: t(lang, "未確認", "Not verified"),
		Detail:  t(lang, "実際の Discord 通知配信はまだ確認されていません。", "Actual Discord notification delivery has not been verified yet."),
		Fix:     t(lang, "準備ができたら 1 回だけテスト通知を送信して、Discord 側の着弾を確認してください。", "When ready, send one test notification and confirm that it arrives in Discord."),
		APIPath: apiPath,
	}

	projects := model.ListProjects(db)
	if len(projects) == 1 {
		state.CanSend = true
		state.TargetProject = projects[0].KitsuProjectID
		state.Detail += " " + fmt.Sprintf(t(lang, "対象プロジェクト: %s", "Target project: %s"), projects[0].Name)
	} else if len(projects) > 1 {
		state.Fix = t(lang, "複数プロジェクトがあるため、この画面からは送信先を自動選択しません。必要なら project ごとの確認フローを使ってください。", "There are multiple projects, so this page does not auto-select one for a test send. Use a project-specific verification flow when needed.")
	} else {
		state.Fix = t(lang, "まず Project Management で project routing と webhook を作成してください。", "Create the project routing and webhook in Project Management first.")
	}

	verified := strings.EqualFold(strings.TrimSpace(model.GetSetting(db, setupTestNotificationVerifiedKey)), "true")
	projectID := strings.TrimSpace(model.GetSetting(db, setupTestNotificationProjectKey))
	verifiedAt := strings.TrimSpace(model.GetSetting(db, setupTestNotificationAtKey))
	if verified {
		state.Status = "ok"
		state.Summary = t(lang, "確認済み", "Verified")
		state.Detail = t(lang, "テスト通知の成功が記録されています。", "A successful test notification has been recorded.")
		if projectID != "" {
			if project := model.FindProjectByKitsuID(db, projectID); project != nil {
				state.Detail += " " + fmt.Sprintf(t(lang, "プロジェクト: %s", "Project: %s"), project.Name)
			} else {
				state.Detail += " " + fmt.Sprintf(t(lang, "プロジェクト ID: %s", "Project ID: %s"), projectID)
			}
		}
		if ts, err := time.Parse(time.RFC3339, verifiedAt); err == nil {
			state.Detail += " " + fmt.Sprintf(t(lang, "最終確認: %s", "Last verified: %s"), ts.Format("2006-01-02 15:04"))
		}
	}

	return state
}

func renderDiagRow(lang string, c diagCheck) string {
	icon := "✓"
	markClass := "ok"
	rowClass := "diag-ok"
	if c.Status == "warn" {
		icon = "⚠"
		markClass = "warn"
		rowClass = "diag-warn"
	} else if c.Status == "fail" {
		icon = "✕"
		markClass = "fail"
		rowClass = "diag-fail"
	}
	fix := ""
	if c.Fix != "" {
		fix = `<div class="diag-fix">` + html.EscapeString(t(lang, "対応: ", "Action: ")+c.Fix) + `</div>`
	}
	return fmt.Sprintf(
		`<tr class="diag-row %s"><td class="diag-icon"><span class="diag-mark %s">%s</span></td><td><strong>%s</strong><div class="diag-detail">%s</div>%s</td></tr>`,
		rowClass, markClass, icon, html.EscapeString(c.Label), html.EscapeString(c.Detail), fix,
	)
}

func renderTestNotificationCard(lang string, state testNotificationState) string {
	pillClass := "warn"
	if state.Status == "ok" {
		pillClass = "ok"
	} else if state.Status == "fail" {
		pillClass = "bad"
	}

	buttonHTML := ""
	if state.CanSend {
		buttonHTML = fmt.Sprintf(
			`<div class="button-row"><button class="btn" id="diagTestNotificationBtn" type="button" data-project-id="%s">%s</button></div>`,
			esc(state.TargetProject),
			esc(t(lang, "テスト通知を送信", "Send test notification")),
		)
	}

	fixHTML := ""
	if state.Fix != "" {
		fixHTML = `<div class="diag-state-note">` + esc(state.Fix) + `</div>`
	}

	return fmt.Sprintf(`
<div class="section-card glass diag-state-card">
  <div class="diag-state-head">
    <div>
      <h3>%s</h3>
      <div class="diag-state-body">
        <p id="diagTestNotificationDetail">%s</p>
        %s
      </div>
    </div>
    <span class="status-pill %s" id="diagTestNotificationBadge">%s</span>
  </div>
  %s
  <div class="diag-state-note" id="diagTestNotificationFeedback" hidden></div>
</div>
<script>
(function(){
  var btn = document.getElementById('diagTestNotificationBtn');
  if (!btn) { return; }
  var badge = document.getElementById('diagTestNotificationBadge');
  var detail = document.getElementById('diagTestNotificationDetail');
  var feedback = document.getElementById('diagTestNotificationFeedback');
  var original = btn.textContent;
  btn.addEventListener('click', async function(){
    if (btn.disabled) { return; }
    btn.disabled = true;
    btn.textContent = %q;
    if (feedback) {
      feedback.hidden = true;
      feedback.textContent = '';
    }
    try {
      var resp = await fetch(%q, {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ project_id: btn.getAttribute('data-project-id') })
      });
      var data = await resp.json();
      if (!resp.ok || data.error) {
        throw new Error(data.error || ('HTTP ' + resp.status));
      }
      if (badge) {
        badge.className = 'status-pill ok';
        badge.textContent = %q;
      }
      if (detail) {
        var msg = %q;
        if (data.project_name) {
          msg += ' ' + (%q + data.project_name);
        }
        detail.textContent = msg;
      }
      if (feedback) {
        feedback.hidden = false;
        feedback.textContent = %q;
      }
    } catch (err) {
      if (badge) {
        badge.className = 'status-pill warn';
        badge.textContent = %q;
      }
      if (feedback) {
        feedback.hidden = false;
        feedback.textContent = %q + (err && err.message ? err.message : 'unknown error');
      }
    } finally {
      btn.disabled = false;
      btn.textContent = original;
    }
  });
})();
</script>`,
		esc(t(lang, "通知配信の確認", "Notification delivery verification")),
		esc(state.Detail),
		fixHTML,
		pillClass,
		esc(state.Summary),
		buttonHTML,
		t(lang, "送信中...", "Sending..."),
		state.APIPath,
		t(lang, "確認済み", "Verified"),
		t(lang, "テスト通知の成功が記録されました。", "A successful test notification was recorded."),
		t(lang, "プロジェクト: ", "Project: "),
		t(lang, "テスト通知を送信しました。Discord 側の着弾も確認してください。", "A test notification was sent. Confirm that it arrived in Discord as well."),
		t(lang, "未確認", "Not verified"),
		t(lang, "テスト通知を送信できませんでした: ", "The test notification could not be sent: "),
	)
}

func diagnosticsAPIBase(r *http.Request) string {
	if r != nil && strings.HasPrefix(r.URL.Path, "/bot/") {
		return "/bot"
	}
	return ""
}

func formatDiagAge(ts time.Time) string {
	if ts.IsZero() {
		return "unknown"
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
