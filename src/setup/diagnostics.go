package setup

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
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

// DiagnosticsHandler runs pre-flight environment checks on demand.
// Pass a refreshCreds func so credentials are always current (read from DB/env at request time).
func DiagnosticsHandler(db *gorm.DB, refreshCreds func() (kitsuHost, botToken, guildID, webhookURL string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		lang := currentLang(r)

		kitsuHost, botToken, guildID, webhookURL := refreshCreds()
		checks := runDiagnostics(lang, kitsuHost, botToken, guildID, webhookURL, db)

		allOK := true
		anyFail := false
		for _, c := range checks {
			if c.Status != "ok" {
				allOK = false
			}
			if c.Status == "fail" {
				anyFail = true
			}
		}

		var summary string
		if allOK {
			summary = `<div class="diag-banner ok">` + esc(t(lang, "すべてのチェックが通過しました。セットアップ準備完了です。", "All checks passed. Ready to run project setup.")) + `</div>`
		} else if anyFail {
			summary = `<div class="diag-banner fail">` + esc(t(lang, "一部のチェックが失敗しました。セットアップ前に修正してください。", "Some checks failed. Please resolve them before running setup.")) + `</div>`
		} else {
			summary = `<div class="diag-banner warn">` + esc(t(lang, "警告があります。確認してから進んでください。", "Warnings found. Review before continuing.")) + `</div>`
		}

		var rows strings.Builder
		for _, c := range checks {
			icon := "[OK]"
			rowClass := "diag-ok"
			if c.Status == "warn" {
				icon = "[!]"
				rowClass = "diag-warn"
			} else if c.Status == "fail" {
				icon = "[X]"
				rowClass = "diag-fail"
			}
			fix := ""
			if c.Fix != "" {
				fix = `<div class="diag-fix">` + html.EscapeString(t(lang, "対処: ", "Fix: ")+c.Fix) + `</div>`
			}
			rows.WriteString(fmt.Sprintf(`<tr class="%s"><td class="diag-icon">%s</td><td><strong>%s</strong><div class="diag-detail">%s</div>%s</td></tr>`,
				rowClass, icon, html.EscapeString(c.Label), html.EscapeString(c.Detail), fix))
		}

		rerunURL := withLang("/bot/admin/diagnostics", r)
		body := fmt.Sprintf(`
<style>
.diag-banner{padding:14px 20px;border-radius:var(--radius-md);margin-bottom:18px;font-weight:600}
.diag-banner.ok{background:rgba(142,207,139,.18);border:1px solid rgba(142,207,139,.4);color:#8ecf8b}
.diag-banner.warn{background:rgba(255,200,80,.12);border:1px solid rgba(255,200,80,.35);color:#ffc850}
.diag-banner.fail{background:rgba(255,106,80,.14);border:1px solid rgba(255,106,80,.38);color:#ff6a50}
.diag-ok td{color:var(--text)}
.diag-warn td{color:#ffc850}
.diag-fail td{color:#ff6a50}
.diag-icon{font-size:1.05rem;padding-right:14px;white-space:nowrap}
.diag-detail{color:var(--muted);font-size:.875rem;margin-top:3px}
.diag-fix{margin-top:6px;padding:6px 10px;border-radius:8px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);font-size:.82rem;color:var(--muted-2)}
.diag-fix::before{content:"-> "}
</style>
%s
<div class="section-card glass">
  <div class="table-wrap">
    <table><tbody>%s</tbody></table>
  </div>
</div>
<div class="button-row">
  <a class="btn" href="%s">%s</a>
  <a class="btn-ghost" href="%s">%s</a>
</div>`,
			summary,
			rows.String(),
			rerunURL, esc(t(lang, "再チェック", "Re-run checks")),
			withLang("/bot/admin", r), esc(t(lang, "管理画面へ", "Back to Admin")),
		)

		fmt.Fprint(w, adminPage(lang, t(lang, "環境診断", "Environment Diagnostics"), r, body))
	}
}

func runDiagnostics(lang, kitsuHost, botToken, guildID, webhookURL string, db *gorm.DB) []diagCheck {
	client := &http.Client{Timeout: 8 * time.Second}
	var checks []diagCheck

	botSettingsFix := t(lang, "Bot設定で共有 Bot Token を見直して保存し、再度 Diagnostics を実行してください。", "Review the shared bot token in Bot Settings, save it, and rerun Diagnostics.")
	configureBotSettingsFix := t(lang, "Bot設定で共有 Bot Token を設定してください。", "Configure the shared bot token in Bot Settings.")
	kitsuSettingsFix := t(lang, "Bot設定で Kitsu Runtime 接続を見直し、必要なら recent runtime logs も確認してください。", "Review the Kitsu runtime connection in Bot Settings and, if needed, confirm the recent runtime logs as well.")
	blockedByBotToken := t(lang, "Discord Bot Token の確認が終わるまでこの確認は保留されます。", "This check is blocked until the Discord bot token is valid.")
	blockedByMissingBotToken := t(lang, "Discord Bot Token が設定されるまでこの確認は保留されます。", "This check is blocked until a Discord bot token is configured.")

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

	jwtToken := os.Getenv("KitsuJWTToken")
	if jwtToken == "" {
		checks = append(checks, diagCheck{
			Label:  "Kitsu auth",
			Status: "fail",
			Detail: "No active Kitsu session token. App may have failed to authenticate at startup.",
			Fix:    kitsuSettingsFix,
		})
	} else if kitsuHost != "" {
		authURL := strings.TrimRight(kitsuHost, "/") + "/api/auth/user"
		req, err := http.NewRequest("GET", authURL, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+jwtToken)
			resp, err := client.Do(req)
			if err != nil {
				checks = append(checks, diagCheck{
					Label:  "Kitsu auth",
					Status: "warn",
					Detail: "Could not verify token: " + err.Error(),
					Fix:    kitsuSettingsFix,
				})
			} else {
				resp.Body.Close()
				switch resp.StatusCode {
				case 200:
					checks = append(checks, diagCheck{
						Label:  "Kitsu auth",
						Status: "ok",
						Detail: "Session token is valid.",
					})
				case 404:
					checks = append(checks, diagCheck{
						Label:  "Kitsu auth",
						Status: "warn",
						Detail: t(lang, "auth 確認 endpoint が HTTP 404 を返しました。runtime polling が動いていても、この確認だけ一致しない場合があります。", "The auth verification endpoint returned HTTP 404. Runtime polling may still be healthy even when this verification endpoint does not match."),
						Fix:    kitsuSettingsFix,
					})
				default:
					checks = append(checks, diagCheck{
						Label:  "Kitsu auth",
						Status: "fail",
						Detail: fmt.Sprintf("Runtime auth check returned HTTP %d.", resp.StatusCode),
						Fix:    kitsuSettingsFix,
					})
				}
			}
		}
	} else {
		checks = append(checks, diagCheck{
			Label:  "Kitsu auth",
			Status: "ok",
			Detail: "Session token present.",
		})
	}

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
							Fix:    "Manually verify the bot has MANAGE_CHANNELS and MANAGE_WEBHOOKS in your Discord server.",
						})
					}
				} else {
					checks = append(checks, diagCheck{
						Label:  "Discord permissions (webhooks)",
						Status: "warn",
						Detail: "Could not retrieve bot member info to verify permissions.",
						Fix:    "Manually verify the bot has MANAGE_CHANNELS and MANAGE_WEBHOOKS.",
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

	if strings.TrimSpace(webhookURL) == "" {
		checks = append(checks, diagCheck{
			Label:  "Fallback webhook (legacy optional)",
			Status: "ok",
			Detail: t(lang, "未設定です。これは unrouted notification 用の旧式 fallback で、通常の project routing には不要です。", "Not configured. This is a legacy fallback for unrouted notifications and is not required for normal project routing."),
		})
	} else {
		req, err := http.NewRequest("GET", webhookURL, nil)
		if err != nil {
			checks = append(checks, diagCheck{
				Label:  "Fallback webhook (legacy optional)",
				Status: "warn",
				Detail: "Invalid webhook URL: " + err.Error(),
				Fix:    t(lang, "fallback delivery をまだ使う場合だけ、この webhook を更新してください。", "Update this webhook only if you still rely on legacy fallback delivery."),
			})
		} else {
			resp, err := client.Do(req)
			if err != nil {
				checks = append(checks, diagCheck{
					Label:  "Fallback webhook (legacy optional)",
					Status: "warn",
					Detail: "Request failed: " + err.Error(),
					Fix:    t(lang, "fallback delivery をまだ使う場合だけ、この webhook の疎通を見直してください。", "Check this webhook only if you still rely on legacy fallback delivery."),
				})
			} else {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					checks = append(checks, diagCheck{
						Label:  "Fallback webhook (legacy optional)",
						Status: "ok",
						Detail: t(lang, "legacy fallback webhook は利用可能です。通常の project routing が優先されます。", "The legacy fallback webhook is reachable. Normal project routing still takes precedence."),
					})
				} else if resp.StatusCode == 404 {
					checks = append(checks, diagCheck{
						Label:  "Fallback webhook (legacy optional)",
						Status: "warn",
						Detail: t(lang, "HTTP 404: legacy fallback webhook は Discord 側で削除されています。通常の project routing だけなら一次障害ではありません。", "HTTP 404: the legacy fallback webhook has been deleted in Discord. This is not a primary failure if normal project routing is in use."),
						Fix:    t(lang, "fallback delivery をまだ使う場合だけ、この webhook を再作成してください。", "Recreate this webhook only if you still rely on legacy fallback delivery."),
					})
				} else {
					checks = append(checks, diagCheck{
						Label:  "Fallback webhook (legacy optional)",
						Status: "warn",
						Detail: fmt.Sprintf("HTTP %d - unexpected response.", resp.StatusCode),
					})
				}
			}
		}
	}

	return checks
}
