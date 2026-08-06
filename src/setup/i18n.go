package setup

import (
	"net/http"
	"net/url"
	"strings"
)

func currentLang(r *http.Request) string {
	if r == nil {
		return "ja"
	}
	lang := r.URL.Query().Get("lang")
	if lang == "en" {
		return "en"
	}
	return "ja"
}

func withLang(path string, r *http.Request) string {
	return appendLang(path, currentLang(r))
}

func langURL(r *http.Request, lang string) string {
	if r == nil || r.URL == nil {
		return "/?lang=" + lang
	}
	target := r.URL.Path
	if target == "" {
		target = "/"
	}
	if raw := r.URL.RawQuery; raw != "" {
		target += "?" + raw
	}
	return appendLang(target, lang)
}

func nextLang(lang string) string {
	if lang == "en" {
		return "ja"
	}
	return "en"
}

func toggleLangURL(r *http.Request) string {
	return langURL(r, nextLang(currentLang(r)))
}

func canonicalText(lang, en string) (string, bool) {
	if lang == "en" {
		return en, true
	}
	keys := map[string]string{
		"Task Type for dry-run":                 "copy.task_type_check",
		"Inspect dry-run":                       "copy.check_without_sending",
		"Pause routing":                         "copy.pause_notifications",
		"Resume routing":                        "copy.resume_notifications",
		"Bot / Runtime":                         "copy.bot_connection",
		"Kitsu runtime":                         "copy.kitsu_connection",
		"Runtime":                               "copy.connection_state",
		"Kitsu runtime email":                   "copy.kitsu_account",
		"Kitsu runtime password":                "copy.kitsu_password",
		"Bot Settings":                          "copy.bot_connection",
		"Open Bot Settings":                     "copy.open_bot_connection",
		"Project Routing":                       "copy.notification_destinations",
		"routing":                               "copy.notification_destinations",
		"project routing":                       "copy.notification_destinations",
		"Discord Server / Guild":                "copy.discord_server",
		"Discord Server / Guild ID":             "copy.discord_server",
		"Discord guild fallback":                "copy.discord_server_fallback",
		"Guild":                                 "copy.discord_server",
		"Step 2: Manage connected productions":  "copy.step2",
		"Step 3: Assign Discord Server / Guild": "copy.step3",
		"Step 4: Final Health Check":            "copy.step4",
		"Final Health Check":                    "copy.final_check",
		"Review Health":                         "copy.review_system_status",
		"New Connection Setup":                  "ia.new_connection",
		"Open Connected Productions":            "copy.open_connected_productions",
		"Connected Productions":                 "ia.productions",
		"Main task":                             "copy.main_task",
		"Kitsu project":                         "copy.kitsu_production",
		"Project type":                          "copy.production_type",
		"Select project type":                   "copy.select_production_type",
		"Language":                              "copy.language",
		"Japanese":                              "copy.japanese",
		"Run Setup":                             "copy.run_setup",
		"Bot account setup...":                  "copy.bot_connection_checking",
		"Setting up...":                         "copy.setting_up",
		"Setup Complete":                        "copy.setup_complete",
		"Setup Failed":                          "copy.setup_failed",
		"Back":                                  "wizard.back",
		"No recent activity.":                   "dashboard.no_recent_activity",
		"No activity yet.":                      "copy.no_activity",
		"Kitsu connection":                      "connections.kitsu",
		"Discord connection":                    "copy.discord_bot",
		"Discord bot token":                     "copy.discord_bot",
		"Kitsu hostname":                        "connections.host",
		"Kitsu integration account":             "connections.account",
		"Missing":                               "wizard.not_configured",
		"Not set":                               "wizard.not_configured",
		"Unknown":                               "copy.unknown",
		"Failed":                                "copy.failed",
		"Ready":                                 "copy.ready",
		"Partial":                               "copy.partial",
		"Reachable":                             "copy.reachable",
		"Authenticated":                         "copy.authenticated",
		"Not sent yet":                          "copy.not_sent",
		"Delivered":                             "copy.delivered",
		"Not ready":                             "copy.not_ready",
		"Not assigned":                          "wizard.not_configured",
		"Project ID":                            "copy.production_id",
		"Webhooks":                              "copy.notification_destinations",
		"Operator workflow":                     "copy.operator_workflow",
		"Setup required":                        "copy.setup_required",
		"Unavailable":                           "wizard.unavailable",
		"Action required":                       "status.action_required",
	}
	if key, ok := keys[en]; ok {
		return tr(lang, key), true
	}
	ja := map[string]string{
		"Task Type for dry-run":                 "確認するTask Type",
		"Inspect dry-run":                       "送信せずに確認",
		"Pause routing":                         "通知を一時停止",
		"Resume routing":                        "通知を再開",
		"Bot / Runtime":                         "Bot接続",
		"Kitsu runtime":                         "Kitsu接続",
		"Runtime":                               "接続状態",
		"Kitsu runtime email":                   "Kitsu連携アカウント",
		"Kitsu runtime password":                "Kitsu連携パスワード",
		"Bot Settings":                          "Bot接続",
		"Open Bot Settings":                     "Bot接続を開く",
		"Project Routing":                       "通知先設定",
		"routing":                               "通知先設定",
		"project routing":                       "通知先設定",
		"Discord Server / Guild":                "Discordサーバー",
		"Discord Server / Guild ID":             "Discordサーバー",
		"Discord guild fallback":                "Discordサーバーの予備設定",
		"Guild":                                 "Discordサーバー",
		"Step 2: Manage connected productions":  "Step 2: 接続済みProductionを管理",
		"Step 3: Assign Discord Server / Guild": "Step 3: Discordサーバーを選択",
		"Step 4: Final Health Check":            "Step 4: 最終確認",
		"Final Health Check":                    "最終確認",
		"Review Health":                         "システム状態を確認",
		"New Connection Setup":                  "新しいProductionを接続",
		"Open Connected Productions":            "接続済みProductionを開く",
		"Connected Productions":                 "接続済みProduction",
		"Main task":                             "主な操作",
		"Kitsu project":                         "Kitsu Production",
		"Project type":                          "Productionの種類",
		"Select project type":                   "Productionの種類を選択",
		"Language":                              "言語",
		"Japanese":                              "日本語",
		"Run Setup":                             "接続を実行",
		"Bot account setup...":                  "Bot接続を確認中...",
		"Setting up...":                         "設定中...",
		"Setup Complete":                        "接続設定が完了しました",
		"Setup Failed":                          "接続設定に失敗しました",
		"Back":                                  "戻る",
		"No recent activity.":                   "最近のアクティビティはありません。",
		"No activity yet.":                      "アクティビティはありません。",
		"Kitsu connection":                      "Kitsu接続",
		"Discord connection":                    "Discord Bot",
		"Discord bot token":                     "Discord Bot",
		"Kitsu hostname":                        "Kitsu接続先",
		"Kitsu integration account":             "Kitsu連携アカウント",
		"Missing":                               "未設定",
		"Not set":                               "未設定",
		"Unknown":                               "不明",
		"Failed":                                "失敗",
		"Ready":                                 "準備完了",
		"Partial":                               "一部未完了",
		"Reachable":                             "接続先を確認済み",
		"Authenticated":                         "認証済み",
		"Not sent yet":                          "未送信",
		"Delivered":                             "送信済み",
		"Not ready":                             "準備が必要",
		"Not assigned":                          "未設定",
		"Project ID":                            "Production ID",
		"Webhooks":                              "通知先",
		"Operator workflow":                     "操作の流れ",
		"Use this page to move in order from Bot / Runtime review to Project Routing, Guild assignment, and notification verification.":                 "Bot接続、通知先設定、Discordサーバーの選択、通知確認を順番に進めます。",
		"Review the shared settings used for notifications. Make changes in Bot Settings.":                                                              "通知に使う共有設定を確認します。変更はBot接続で行います。",
		"Before starting Project Routing, review the shared Bot / Runtime in Bot Settings.":                                                             "通知先設定を始める前に、Bot接続を確認してください。",
		"The shared Bot / Runtime used for notifications is ready. The prerequisite for Project Routing is in place.":                                   "通知に使うBot接続の準備が完了しました。通知先設定に進めます。",
		"After routing is created, assign each project to its destination Discord Server / Guild.":                                                      "通知先を作成したら、ProductionごとにDiscordサーバーを選択します。",
		"After guild assignment, use Health to confirm the notification destination and runtime status. If everything is healthy, setup is complete.":   "Discordサーバーを選択したら、システム状態で通知先と接続状態を確認します。問題がなければ設定完了です。",
		"Use Connected Productions as the review / edit page when you need to revisit guild assignment or saved connection details from project setup.": "接続済みProductionで、Discordサーバーの選択や保存済みの接続情報を確認・編集します。",
		"Kitsu server answered, but authentication is not complete.":                                                                                    "Kitsuは応答しましたが、認証が完了していません。",
		"Fix the Kitsu connection first.":         "先にKitsu接続を設定してください。",
		"Fix the Discord bot connection first.":   "先にDiscord Botを設定してください。",
		"Assign a Discord guild to this project.": "このProductionにDiscordサーバーを設定してください。",
		"Review Health to confirm the notification destination and runtime status before treating setup as complete.": "設定完了にする前に、システム状態で通知先と接続状態を確認してください。",
		"Setup is complete.":             "設定が完了しました。",
		"No Kitsu projects were loaded.": "KitsuのProductionを取得できませんでした。",
		"Select project":                 "Productionを選択",
		"already set up":                 "設定済み",
		"Channels that will be created for this project type:":               "このProductionの種類で作成されるチャンネル:",
		"Kitsu is the Single Source of Truth.":                               "Kitsuを正しい情報の基準として使用します。",
		"Discord is for notifications only. Make all task changes in Kitsu.": "Discordは通知専用です。タスクの変更はKitsuで行ってください。",
		"Next step":                     "次の操作",
		"Assigned":                      "設定済み",
		"Pending":                       "保留中",
		"Current":                       "現在の状態",
		"After Step 1":                  "Step 1の完了後",
		"Configured":                    "設定済み",
		"Review":                        "確認",
		"Final check":                   "最終確認",
		"Confirmed":                     "確認済み",
		"Last step":                     "最後の操作",
		"No connected productions yet.": "接続済みProductionはありません。",
		"Setup required":                "初期設定が必要です",
		"Unavailable":                   "利用不可",
		"Action required":               "対応が必要",
	}
	value, ok := ja[en]
	return value, ok
}

func t(lang, ja, en string) string {
	if value, ok := canonicalText(lang, en); ok {
		return value
	}
	// Keep legacy callers on the shared normal-user wording while the routes
	// migrate to catalog keys. This is intentionally limited to the confirmed
	// notification labels and does not translate external values.
	switch en {
	case "Task Type for dry-run":
		ja, en = "確認するTask Type", "Task Type to check"
	case "Inspect dry-run":
		ja, en = "送信せずに確認", "Check without sending"
	case "Pause routing":
		ja, en = "通知を一時停止", "Pause notifications"
	case "Resume routing":
		ja, en = "通知を再開", "Resume notifications"
	}
	if lang == "en" {
		return en
	}
	return ja
}

func appendLang(path, lang string) string {
	if path == "" {
		path = "/"
	}
	u, err := url.Parse(path)
	if err != nil {
		if path == "" {
			path = "/"
		}
		if lang == "" {
			return path
		}
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		return path + separator + "lang=" + url.QueryEscape(lang)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	values := u.Query()
	values.Set("lang", lang)
	u.RawQuery = values.Encode()
	return u.String()
}
