package setup

import "fmt"

// uiText is the shared catalog for user-facing copy that appears in more
// than one route or in dynamic results. Product data and technical identifiers
// must be passed separately and are never translated.
var uiText = map[string]map[string]string{
	"ja": {
		"ia.dashboard": "\u30c0\u30c3\u30b7\u30e5\u30dc\u30fc\u30c9", "ia.productions": "Production", "ia.production_list": "Production\u4e00\u89a7", "ia.overview": "\u6982\u8981", "ia.notifications": "\u901a\u77e5", "ia.user_settings": "\u30e6\u30fc\u30b6\u30fc\u8a2d\u5b9a", "ia.storage_settings": "\u30b9\u30c8\u30ec\u30fc\u30b8\u8a2d\u5b9a", "ia.activity": "\u30a2\u30af\u30c6\u30a3\u30d3\u30c6\u30a3", "ia.troubleshooting": "\u30c8\u30e9\u30d6\u30eb\u30b7\u30e5\u30fc\u30c6\u30a3\u30f3\u30b0", "ia.advanced": "\u8a73\u7d30\u8a2d\u5b9a", "ia.danger": "Danger Zone", "ia.new_connection": "\u65b0\u3057\u3044Production\u3092\u63a5\u7d9a", "ia.user_mapping": "\u30e6\u30fc\u30b6\u30fc\u7d10\u3065\u3051", "ia.bot_connection": "Bot\u63a5\u7d9a", "ia.system_status": "\u30b7\u30b9\u30c6\u30e0\u72b6\u614b", "ia.audit_log": "\u76e3\u67fb\u30ed\u30b0", "ia.discord_server": "Discord\u30b5\u30fc\u30d0\u30fc", "ia.check_without_sending": "\u9001\u4fe1\u305b\u305a\u306b\u78ba\u8a8d", "ia.pause_notifications": "\u901a\u77e5\u3092\u4e00\u6642\u505c\u6b62", "ia.resume_notifications": "\u901a\u77e5\u3092\u518d\u958b", "ia.disconnect_production": "Production\u306e\u9023\u643a\u3092\u89e3\u9664", "ia.delete_discord_resources": "Discord\u5074\u306e\u30ea\u30bd\u30fc\u30b9\u3092\u524a\u9664", "channel_plan.select_server": "Discord\u30b5\u30fc\u30d0\u30fc\u3092\u9078\u629e", "channel_plan.server": "Discord\u30b5\u30fc\u30d0\u30fc", "channel_plan.check_without_sending": "\u9001\u4fe1\u305b\u305a\u306b\u78ba\u8a8d", "connections.title": "\u63a5\u7d9a\u8a2d\u5b9a", "connections.edit": "\u63a5\u7d9a\u8a2d\u5b9a\u3092\u7de8\u96c6", "connections.kitsu": "Kitsu\u63a5\u7d9a", "connections.discord": "Discord Bot\u63a5\u7d9a", "connections.host": "Kitsu\u30db\u30b9\u30c8", "connections.account": "Kitsu\u9023\u643a\u30a2\u30ab\u30a6\u30f3\u30c8", "wizard.error.select_production": "Production\u3092\u9078\u629e\u3057\u3066\u304f\u3060\u3055\u3044", "wizard.error.invalid_production": "\u9078\u629e\u3057\u305fProduction\u3092\u78ba\u8a8d\u3067\u304d\u307e\u305b\u3093\u3067\u3057\u305f", "wizard.error.already_connected": "\u3053\u306eProduction\u306f\u3059\u3067\u306b\u9023\u643a\u3055\u308c\u3066\u3044\u307e\u3059",
		"wizard.description": "ProductionとDiscordサーバーを順に選び、実行前に内容を確認します。", "wizard.progress": "接続設定の進行状況", "wizard.step_prerequisites": "準備状況", "wizard.step_production": "Production", "wizard.step_server": "Discordサーバー", "wizard.step_plan": "チャンネル計画", "wizard.step_review": "内容確認", "wizard.step_execute": "実行", "wizard.step_complete": "完了", "wizard.prerequisites_title": "接続の準備状況", "wizard.notification_state": "通知の状態", "wizard.connected": "接続済み", "wizard.not_configured": "未設定", "wizard.available": "利用可能", "wizard.unavailable": "利用不可", "wizard.unavailable_explanation": "Bot接続が完了するまで通知は利用できません。", "wizard.bot_required_explanation": "通知設定とサーバー選択にはBot接続が必要です。", "wizard.blocked_bot": "Bot接続が未設定のため、通知設定とサーバー選択はまだ開始できません。先にBot接続を完了してください。", "wizard.open_bot": "Bot接続を設定", "wizard.prerequisites_ready": "必要な接続がそろいました。Productionを選択できます。", "wizard.next": "次へ", "wizard.back": "戻る", "wizard.production_title": "Kitsu Productionを選択", "wizard.production_label": "Production", "wizard.production_help": "接続済みのProductionは選択できません。", "wizard.select_production": "Productionを選択", "wizard.already_connected": "接続済み", "wizard.server_title": "Discordサーバーを選択", "wizard.server_label": "Discordサーバー", "wizard.server_help": "Botがアクセスできるサーバーだけが表示されます。", "wizard.select_server": "Discordサーバーを選択", "wizard.plan_title": "チャンネル計画を確認", "wizard.plan_hint": "すべてのTask Typeについて、作成・再利用・確認が必要な結果を表示します。ここではDiscordへの変更は行いません。", "wizard.plan_caption": "Task Typeごとのチャンネル計画", "wizard.task_type": "Task Type", "wizard.channel": "Discordチャンネル", "wizard.result": "結果", "wizard.details": "詳細", "wizard.create": "作成", "wizard.reuse": "再利用", "wizard.conflict": "競合", "wizard.review_required": "確認が必要", "wizard.detail_create": "不足しているチャンネルを作成します。", "wizard.detail_reuse": "既存の正確なチャンネルを再利用します。", "wizard.detail_conflict": "名前または所有情報を確認してください。", "wizard.detail_review": "安全を確認するまで実行できません。", "wizard.plan_blocked": "チャンネル名の重複または所有情報を解決してください。競合が残っているため、次へ進めません。", "wizard.plan_unavailable": "サーバーのチャンネルを確認できませんでした。変更は行われていません。", "wizard.no_write": "この画面は確認専用です。明示的に確認するまでDiscordへの変更は行いません。", "wizard.review": "内容を確認する", "wizard.confirm": "表示された計画を確認しました。", "wizard.execute_title": "接続を実行", "wizard.execute_hint": "実行は内容確認画面で明示的に同意した場合だけ行われます。", "wizard.back_to_review": "内容確認に戻る", "wizard.execute": "計画を確認して実行", "wizard.complete_title": "接続設定が完了しました", "wizard.complete_message": "%s のチャンネル設定を保存しました。", "wizard.open_production": "Productionを開く", "status.action_required": "対応が必要", "status.needs_review": "確認が必要", "status.incomplete": "未設定", "status.paused": "一時停止中", "status.active": "有効",
		"setup_result.cleanup_started":          "Discord リソースのクリーンアップを開始しました",
		"setup_result.cleanup_channel_failed":   "チャンネルのクリーンアップに失敗しました: #%s: %s",
		"setup_result.cleanup_category_failed":  "Discord カテゴリのクリーンアップに失敗しました: %s",
		"setup_result.cleanup_webhooks_failed":  "セットアップ webhook 記録のクリーンアップに失敗しました: %s",
		"setup_result.cleanup_project_failed":   "セットアップ Production 記録のクリーンアップに失敗しました: %s",
		"setup_result.no_guild":                 "この Production に Discord Guild が設定されていません",
		"setup_result.unsupported_type":         "未対応の Production 種別です: %s",
		"setup_result.project_missing":          "Kitsu プロジェクトが見つからないため、フォールバック ID を使用します",
		"setup_result.already_configured":       "この Production はすでに設定されています",
		"setup_result.orphaned_webhooks":        "孤立した webhook を検出しました（%d 件）。セットアップ前にクリーンアップします",
		"setup_result.orphan_cleanup_failed":    "孤立した webhook のクリーンアップに失敗しました: %s",
		"setup_result.orphan_cleanup_done":      "孤立した webhook をクリーンアップしました",
		"setup_result.category_failed":          "Discord カテゴリの作成に失敗しました: %s",
		"setup_result.channel_failed":           "チャンネルの作成に失敗しました: #%s: %s",
		"setup_result.webhook_failed":           "チャンネルの webhook 作成に失敗しました: #%s: %s",
		"setup_result.cleanup_after_webhook":    "webhook エラー後のチャンネルクリーンアップに失敗しました: #%s: %s",
		"setup_result.rollback_incomplete":      "不完全なチャンネルをロールバックしました: #%s",
		"setup_result.db_failed":                "Discord セットアップは成功しましたが、データベース処理に失敗しました: %s",
		"setup_result.cleanup_warnings":         "Discord の自動クリーンアップに警告があります。リソースを手動で確認してください。",
		"setup_result.cleanup_done":             "Discord の自動クリーンアップが完了しました",
		"setup_result.kitsu_confirmed":          "Kitsu プロジェクトを確認しました",
		"setup_result.category_created":         "Discord カテゴリを作成しました",
		"setup_result.channel_ready":            "チャンネルを使用可能にしました: #%s",
		"setup_result.completed":                "プロジェクトのセットアップが完了しました",
		"setup_result.reused":                   "既存のリソースを再利用しました: %s",
		"setup_result.conflict":                 "競合を解消してから再試行してください: %s",
		"setup_result.stale":                    "計画が古くなっています。最新の計画を確認してください。",
		"setup_result.retry":                    "再試行できます。ロールバックが完了しました。",
		"setup_result.rollback_channel":         "ロールバックしたチャンネル: #%s",
		"setup_result.rollback_category":        "Discord カテゴリをロールバックしました",
		"setup_result.rollback_records":         "セットアップ記録をロールバックしました",
		"setup_result.partial_failure":          "セットアップは完了しませんでした。作成済みの Discord リソースをロールバックしています。",
		"bot_runtime.action_required":           "要対応",
		"bot_runtime.complete_setup":            "Bot 設定を完了",
		"bot_runtime.reauthenticate":            "再認証して編集",
		"bot_runtime.kitsu_hostname":            "Kitsu ホスト名",
		"bot_runtime.bot_token":                 "Bot トークン",
		"production_routing.title":              "Production 通知ルーティング",
		"production_routing.description":        "Production、Kitsu Task Type、設定済み送信先を選択します。未設定または一時停止中の Production は何も送信しません。",
		"production_routing.select_production":  "接続済み Production を選択",
		"production_routing.select_task_type":   "Task Type を選択",
		"production_routing.select_destination": "送信先を選択",
		"production_routing.save":               "保存して有効化",
		"production_routing.dry_run":            "dry-run を確認",
		"production_routing.pause":              "ルーティングを一時停止",
		"production_routing.resume":             "ルーティングを再開",
		"production_routing.unconfigured":       "未設定",
		"production_routing.enabled":            "有効",
		"production_routing.paused":             "一時停止中",
		"production_routing.diagnoses":          "最近のルーティング診断",
		"production_routing.no_diagnoses":       "記録されたスキップはありません。",
		"production_routing.advanced":           "詳細な識別子",
		"production_routing.status":             "状態",
		"production_routing.task_type":          "Task Type",
		"production_routing.display_name":       "表示名",
		"production_routing.destination":        "送信先",
		"production_routing.no_selection":       "ルートを設定し、非機密の診断を確認する Production を選択してください。",
		"channel_plan.title":                    "Task Type チャンネル",
		"channel_plan.select_guild":             "既存の Discord Guild を選択",
		"channel_plan.preview":                  "チャンネル計画を確認",
		"channel_plan.confirm":                  "この計画を確認して不足チャンネルを作成",
		"channel_plan.confirmed":                "この正確な計画を確認しました。",
		"channel_plan.description":              "選択した Production の既存 Discord Guild を選びます。完全な計画は明示的に確認するまで読み取り専用です。",
		"channel_plan.no_token":                 "Guild の読み取りには有効な Discord bot token が必要です。",
		"channel_plan.no_guild":                 "現在の text channel を読み取る Guild を選択してください。",
		"channel_plan.no_write":                 "Discord への書き込みは行いません。",
		"channel_plan.read_failed":              "選択した Guild を読み取れませんでした。Discord への書き込みは行いません。",
		"channel_plan.exact_plan":               "正確な Task Type チャンネル計画",
		"channel_plan.task_type":                "Task Type",
		"channel_plan.channel":                  "チャンネル",
		"channel_plan.action":                   "操作",
		"channel_plan.create_only":              "この計画では不足している text channel だけを作成します。既存の正確なチャンネルは再利用します。既存チャンネルの名前変更、削除、上書き、権限変更は行いません。",
		"channel_plan.blocked":                  "この計画はブロックされています。競合または stale reference を解消してから確認してください。Discord への書き込みは行いません。",
		"channel_plan.result_title":             "Task Type チャンネル計画",
		"channel_plan.review":                   "Connected Productions を確認",
		"channel_plan.no_confirmation":          "確認を受け取れませんでした。Discord への書き込みは行いません。",
		"channel_plan.production_missing":       "選択した Production は接続済みではありません。Discord への書き込みは行いません。",
		"channel_plan.guild_revalidate_failed":  "選択した Guild を再検証できませんでした。Discord への書き込みは行いません。",
		"channel_plan.stale":                    "Production、Task Types、または Guild のチャンネルが変更されたため、計画が stale です。新しい計画を確認してから再度確認してください。Discord への書き込みは行いません。",
		"channel_plan.invalid":                  "所有権、名前、または stale reference の問題により計画がブロックされています。Discord への書き込みは行いません。",
		"channel_plan.partial":                  "%d 件の書き込み後にチャンネル作成を停止しました。ルーティングは無効のままです。Discord を確認して更新された計画を再試行してください。",
		"channel_plan.verify_failed":            "Discord から完全な検証結果を取得できませんでした。ルーティングは無効のままで、mapping は保存していません。",
		"channel_plan.persist_failed":           "検証済み Discord 結果を保存できませんでした。ルーティングは無効のままです。",
		"channel_plan.routing_persist_failed":   "新しい通知ルーティングを保存できませんでした。ルーティングは無効のままです。",
		"channel_plan.guild_save_failed":        "mapping は検証できましたが、Production の Guild 割り当てを保存できませんでした。再試行前に結果を確認してください。",
		"channel_plan.completed":                "チャンネル計画が完了しました。不足チャンネル %d 件を作成し、既存チャンネル %d 件を再利用しました。ルーティング mapping は有効です。",
		"workflow.title":                        "Workflow Diagnosis",
		"workflow.read_only":                    "読み取り専用です。Kitsu または Discord への変更は行いません。",
		"workflow.disconnected":                 "Kitsu runtime は接続されていません。診断を実行するには再接続が必要です。",
		"workflow.back":                         "Connected Productions に戻る",
		"workflow.summary":                      "ルーティング概要",
		"workflow.template":                     "現在の cg template 比較",
		"workflow.similar_names":                "類似名は参考情報のみで、完全一致として扱いません。",
		"dry_run.select_production_task_type":   "接続済み Production と Task Type を選択してください。",
		"login.admin_access":                    "管理者ログイン",
		"login.description":                     "Kitsu の manager / admin アカウントでログインしてください。",
		"login.email":                           "メールアドレス",
		"login.password":                        "パスワード",
		"login.submit":                          "ログイン",
	},
	"en": {
		"ia.dashboard": "Dashboard", "ia.productions": "Productions", "ia.production_list": "Production list", "ia.overview": "Overview", "ia.notifications": "Notifications", "ia.user_settings": "User settings", "ia.storage_settings": "Storage settings", "ia.activity": "Activity", "ia.troubleshooting": "Troubleshooting", "ia.advanced": "Advanced settings", "ia.danger": "Danger Zone", "ia.new_connection": "New Production Connection", "ia.user_mapping": "User Linking", "ia.bot_connection": "Bot Connection", "ia.system_status": "System Status", "ia.audit_log": "Audit Log", "ia.discord_server": "Discord server", "ia.check_without_sending": "Check without sending", "ia.pause_notifications": "Pause notifications", "ia.resume_notifications": "Resume notifications", "ia.disconnect_production": "Disconnect Production", "ia.delete_discord_resources": "Delete Discord resources", "channel_plan.select_server": "Select a Discord server", "channel_plan.server": "Discord server", "channel_plan.check_without_sending": "Check without sending", "connections.title": "Connections", "connections.edit": "Edit connections", "connections.kitsu": "Kitsu connection", "connections.discord": "Discord Bot connection", "connections.host": "Kitsu host", "connections.account": "Kitsu integration account", "wizard.error.select_production": "Select a Production", "wizard.error.invalid_production": "The selected Production could not be verified", "wizard.error.already_connected": "This Production is already connected",
		"wizard.description": "Select a Production and Discord server in order, then review the exact plan before execution.", "wizard.progress": "Connection setup progress", "wizard.step_prerequisites": "Prerequisites", "wizard.step_production": "Production", "wizard.step_server": "Discord server", "wizard.step_plan": "Channel plan", "wizard.step_review": "Review", "wizard.step_execute": "Execute", "wizard.step_complete": "Complete", "wizard.prerequisites_title": "Connection prerequisites", "wizard.notification_state": "Notification state", "wizard.connected": "Connected", "wizard.not_configured": "Not configured", "wizard.available": "Available", "wizard.unavailable": "Unavailable", "wizard.unavailable_explanation": "Notifications are unavailable until Bot Connection is complete.", "wizard.bot_required_explanation": "Bot Connection is required for notification setup and server selection.", "wizard.blocked_bot": "Bot Connection is not configured, so server selection and notification setup are unavailable. Complete Bot Connection first.", "wizard.open_bot": "Set up Bot Connection", "wizard.prerequisites_ready": "All prerequisites are ready. Select a Production to continue.", "wizard.next": "Next", "wizard.back": "Back", "wizard.production_title": "Select a Kitsu Production", "wizard.production_label": "Production", "wizard.production_help": "Already-connected Productions cannot be selected.", "wizard.select_production": "Select a Production", "wizard.already_connected": "Already connected", "wizard.server_title": "Select a Discord server", "wizard.server_label": "Discord server", "wizard.server_help": "Only servers accessible to the bot are listed.", "wizard.select_server": "Select a Discord server", "wizard.plan_title": "Review the channel plan", "wizard.plan_hint": "Every Task Type is shown with its create, reuse, or review-required result. This step is read-only.", "wizard.plan_caption": "Task Type channel plan", "wizard.task_type": "Task Type", "wizard.channel": "Discord channel", "wizard.result": "Result", "wizard.details": "Details", "wizard.create": "Create", "wizard.reuse": "Reuse", "wizard.conflict": "Conflict", "wizard.review_required": "Review required", "wizard.detail_create": "A missing channel will be created.", "wizard.detail_reuse": "An exact existing channel will be reused.", "wizard.detail_conflict": "Review the name or ownership information.", "wizard.detail_review": "Execution is blocked until this is safe.", "wizard.plan_blocked": "This plan requires review and cannot be executed.", "wizard.plan_unavailable": "The server channels could not be read. No changes were made.", "wizard.no_write": "This is a review-only screen. No Discord change occurs until you explicitly confirm.", "wizard.review": "Review contents", "wizard.confirm": "I reviewed the exact plan shown above.", "wizard.execute_title": "Execute connection", "wizard.execute_hint": "Execution is available only after explicit confirmation on the review screen.", "wizard.back_to_review": "Back to review", "wizard.execute": "Confirm plan and execute", "wizard.complete_title": "Connection setup complete", "wizard.complete_message": "Channel settings were saved for %s.", "wizard.open_production": "Open Production", "status.action_required": "Action required", "status.needs_review": "Needs review", "status.incomplete": "Incomplete", "status.paused": "Paused", "status.active": "Active",
		"setup_result.cleanup_started":          "database transaction failed after Discord provisioning; attempting Discord cleanup",
		"setup_result.cleanup_channel_failed":   "cleanup failed for #%s: %s",
		"setup_result.cleanup_category_failed":  "cleanup failed for Discord category: %s",
		"setup_result.cleanup_webhooks_failed":  "cleanup failed for setup webhook records: %s",
		"setup_result.cleanup_project_failed":   "cleanup failed for setup project record: %s",
		"setup_result.no_guild":                 "no Discord guild is configured for this project",
		"setup_result.unsupported_type":         "unsupported project type: %s",
		"setup_result.project_missing":          "Kitsu project was not found, using a fallback project ID",
		"setup_result.already_configured":       "project is already configured",
		"setup_result.orphaned_webhooks":        "orphaned webhooks detected (%d rows); cleaning up before setup",
		"setup_result.orphan_cleanup_failed":    "failed to clean up orphaned webhooks: %s",
		"setup_result.orphan_cleanup_done":      "cleaned up orphaned webhooks",
		"setup_result.category_failed":          "failed to create Discord category: %s",
		"setup_result.channel_failed":           "failed to create #%s: %s",
		"setup_result.webhook_failed":           "failed to create webhook for #%s: %s",
		"setup_result.cleanup_after_webhook":    "cleanup failed for #%s after webhook error: %s",
		"setup_result.rollback_incomplete":      "rolled back incomplete channel: #%s",
		"setup_result.db_failed":                "Discord setup succeeded but database transaction failed: %s",
		"setup_result.cleanup_warnings":         "automatic Discord cleanup had warnings; verify Discord resources manually before retrying.",
		"setup_result.cleanup_done":             "automatic Discord cleanup completed",
		"setup_result.kitsu_confirmed":          "Kitsu project confirmed",
		"setup_result.category_created":         "Discord category created",
		"setup_result.channel_ready":            "channel ready: #%s",
		"setup_result.completed":                "project setup completed",
		"setup_result.reused":                   "Existing resource reused: %s",
		"setup_result.conflict":                 "Resolve the conflict and retry: %s",
		"setup_result.stale":                    "The plan is stale. Review the latest plan before retrying.",
		"setup_result.retry":                    "Safe to retry — rollback completed. You can run setup again immediately.",
		"setup_result.rollback_channel":         "rolled back channel: #%s",
		"setup_result.rollback_category":        "rolled back Discord category",
		"setup_result.rollback_records":         "rolled back setup records",
		"setup_result.partial_failure":          "project setup did not complete; created Discord resources are being rolled back",
		"bot_runtime.action_required":           "Action required",
		"bot_runtime.complete_setup":            "Complete Bot Setup",
		"bot_runtime.reauthenticate":            "Re-authenticate to edit",
		"bot_runtime.kitsu_hostname":            "KITSU HOSTNAME",
		"bot_runtime.bot_token":                 "BOT TOKEN",
		"production_routing.title":              "Production Notification Routing",
		"production_routing.description":        "Choose a Production, Kitsu Task Type, and configured destination. Unconfigured or paused Productions send nothing.",
		"production_routing.select_production":  "Select a connected Production",
		"production_routing.select_task_type":   "Select a Task Type",
		"production_routing.select_destination": "Select a destination",
		"production_routing.save":               "Save and activate",
		"production_routing.dry_run":            "Check without sending",
		"production_routing.pause":              "Pause notifications",
		"production_routing.resume":             "Resume notifications",
		"production_routing.unconfigured":       "Unconfigured",
		"production_routing.enabled":            "Enabled",
		"production_routing.paused":             "Paused",
		"production_routing.diagnoses":          "Recent routing diagnoses",
		"production_routing.no_diagnoses":       "No routing skips recorded.",
		"production_routing.advanced":           "Advanced identifiers",
		"production_routing.status":             "Status",
		"production_routing.task_type":          "Task Type",
		"production_routing.display_name":       "Display name",
		"production_routing.destination":        "Destination",
		"production_routing.no_selection":       "Select a Production to configure routes and inspect non-secret diagnoses.",
		"channel_plan.title":                    "Task Type Channels",
		"channel_plan.select_guild":             "Select an existing Discord Guild",
		"channel_plan.preview":                  "Preview exact channel plan",
		"channel_plan.confirm":                  "Confirm and create missing channels",
		"channel_plan.confirmed":                "I reviewed and confirm this exact plan.",
		"channel_plan.description":              "Select the Production's existing Discord Guild. The complete plan is read-only until you explicitly confirm it.",
		"channel_plan.no_token":                 "A valid Discord bot token is required to read Guilds.",
		"channel_plan.no_guild":                 "Select a Guild to read its current text channels.",
		"channel_plan.no_write":                 "No Discord write occurs.",
		"channel_plan.read_failed":              "The selected Guild could not be read. No Discord write occurs.",
		"channel_plan.exact_plan":               "Exact Task Type channel plan",
		"channel_plan.task_type":                "Task Type",
		"channel_plan.channel":                  "Channel",
		"channel_plan.action":                   "Action",
		"channel_plan.create_only":              "This exact plan will create only missing text channels. Existing exact channels are reused. Existing channels are never renamed, deleted, overwritten, or permission-edited.",
		"channel_plan.blocked":                  "This plan is blocked. Resolve the conflict or stale reference before confirmation. No Discord write occurs.",
		"channel_plan.result_title":             "Task Type channel plan",
		"channel_plan.review":                   "Review Connected Productions",
		"channel_plan.no_confirmation":          "Confirmation was not recorded. No Discord write occurred.",
		"channel_plan.production_missing":       "The selected Production is no longer connected. No Discord write occurred.",
		"channel_plan.guild_revalidate_failed":  "The selected Guild could not be revalidated. No Discord write occurred.",
		"channel_plan.stale":                    "The plan is stale because Production, Task Types, or Guild channels changed. Review the new plan before confirming again. No Discord write occurred.",
		"channel_plan.invalid":                  "The plan is blocked by a naming, ownership, or stale-reference issue. No Discord write occurred.",
		"channel_plan.partial":                  "Channel creation stopped after %d write(s). Routing remains inactive; review Discord and retry the refreshed plan.",
		"channel_plan.verify_failed":            "Discord did not return a complete verified result. Routing remains inactive and no mappings were saved.",
		"channel_plan.persist_failed":           "The verified Discord result could not be persisted. Routing remains inactive.",
		"channel_plan.routing_persist_failed":   "The new notification routing model could not be persisted. Routing remains inactive.",
		"channel_plan.guild_save_failed":        "Mappings were verified, but the Production Guild assignment could not be saved. Review the result before retrying.",
		"channel_plan.completed":                "Channel plan completed: %d missing text channel(s) created and %d exact channel(s) reused. Routing mappings are active.",
		"workflow.title":                        "Workflow Diagnosis",
		"workflow.read_only":                    "Read-only. No changes will be applied to Kitsu or Discord.",
		"workflow.disconnected":                 "Kitsu runtime is disconnected; reconnect is required before diagnosis can run.",
		"workflow.back":                         "Back to Connected Productions",
		"workflow.summary":                      "Routing summary",
		"workflow.template":                     "Current cg template comparison",
		"workflow.similar_names":                "Similar names are informational only and are never treated as exact matches.",
		"dry_run.select_production_task_type":   "Select a connected Production and Task Type.",
		"login.admin_access":                    "Admin Access",
		"login.description":                     "Sign in with a Kitsu manager or admin account.",
		"login.email":                           "Email",
		"login.password":                        "Password",
		"login.submit":                          "Login",
	},
}

/* func init() {
	uiText["ja"]["production.unconnected.source"] = "Kitsuから取得"
	uiText["ja"]["production.unconnected.status"] = "未接続"
	uiText["ja"]["production.unconnected.explanation"] = "このProductionはまだKitsuSyncに接続されていません。Discordサーバー、通知先、ユーザー設定、ストレージ設定は接続後に利用できます。"
	uiText["ja"]["production.unconnected.configure"] = "接続を設定"
	uiText["ja"]["production.unconnected.back"] = "Production一覧へ戻る"
	uiText["en"]["production.unconnected.source"] = "Loaded from Kitsu"
	uiText["en"]["production.unconnected.status"] = "Not connected"
	uiText["en"]["production.unconnected.explanation"] = "This Production is not yet connected to KitsuSync. Discord server, notification, user, and storage settings become available after connection."
	uiText["en"]["production.unconnected.configure"] = "Configure connection"
	uiText["en"]["production.unconnected.back"] = "Back to Productions"
	uiText["ja"]["channel_plan.duplicate_name"] = "\u8907\u6570\u306eTask Type\u304c\u540c\u3058\u540d\u524d\u3067\u3001\u540c\u3058Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u540d\u306b\u306a\u308a\u307e\u3059\u3002Kitsu\u5074\u3067Task Type\u540d\u3092\u5909\u66f4\u3059\u308b\u304b\u3001\u63a5\u7d9a\u524d\u306b\u5225\u3005\u306e\u30c1\u30e3\u30f3\u30cd\u30eb\u540d\u3092\u6307\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044\u3002"
	uiText["en"]["channel_plan.duplicate_name"] = "Multiple Task Types have the same name and resolve to the same Discord channel. Rename the Task Types in Kitsu or assign distinct channel names before connecting."
	uiText["ja"]["connections.status_required"] = "\u5bfe\u5fdc\u304c\u5fc5\u8981"
	uiText["ja"]["connections.status_configured"] = "\u8a2d\u5b9a\u6e08\u307f"
	uiText["ja"]["connections.hint_separate"] = "Kitsu\u63a5\u7d9a\u3068Discord Bot\u63a5\u7d9a\u3092\u305d\u308c\u305e\u308c\u78ba\u8a8d\u3057\u3066\u304f\u3060\u3055\u3044"
	uiText["ja"]["connections.hint_kitsu"] = "Kitsu\u63a5\u7d9a\u3092\u8a2d\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044"
	uiText["ja"]["connections.hint_discord"] = "Discord Bot\u63a5\u7d9a\u3092\u8a2d\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044"
	uiText["ja"]["connections.persistence_now"] = "変更は実行中のプロセスに反映され、アプリ設定にも保存されます。"
	uiText["ja"]["connections.persistence_restart"] = "再起動後は保存済みの設定が優先されます。環境変数は予備の設定としてのみ使用します。"
	uiText["ja"]["connections.persistence_now"] = "\u5909\u66f4\u306f\u5b9f\u884c\u4e2d\u306e\u30d7\u30ed\u30bb\u30b9\u306b\u53cd\u6620\u3055\u308c\u3001\u30a2\u30d7\u30ea\u8a2d\u5b9a\u306b\u3082\u4fdd\u5b58\u3055\u308c\u307e\u3059\u3002"
	uiText["ja"]["connections.persistence_restart"] = "\u518d\u8d77\u52d5\u5f8c\u306f\u4fdd\u5b58\u6e08\u307f\u306e\u8a2d\u5b9a\u304c\u512a\u5148\u3055\u308c\u307e\u3059\u3002\u74b0\u5883\u5909\u6570\u306f\u4e88\u5099\u306e\u8a2d\u5b9a\u3068\u3057\u3066\u306e\u307f\u4f7f\u7528\u3057\u307e\u3059\u3002"
	uiText["ja"]["system.kitsu"] = "Kitsu接続"
	uiText["ja"]["system.discord"] = "Discord接続"
	uiText["ja"]["system.bot"] = "Bot状態"
	uiText["ja"]["system.notifications"] = "通知状態"
	uiText["ja"]["system.overall"] = "全体の状態"
	uiText["ja"]["system.next_action"] = "次に必要な操作"
	uiText["en"]["system.kitsu"] = "Kitsu connection"
	uiText["en"]["system.discord"] = "Discord connection"
	uiText["en"]["system.bot"] = "Bot state"
	uiText["en"]["system.notifications"] = "Notifications"
	uiText["en"]["system.overall"] = "Overall status"
	uiText["en"]["system.next_action"] = "Next required action"
}

*/

func init() {
	uiText["ja"]["production.unconnected.source"] = "Kitsu\u304b\u3089\u53d6\u5f97"
	uiText["ja"]["system.kitsu"] = "Kitsu\u63a5\u7d9a"
	uiText["ja"]["system.discord"] = "Discord Bot"
	uiText["ja"]["system.production"] = "Production接続"
	uiText["ja"]["system.notifications"] = "\u901a\u77e5\u72b6\u614b"
	uiText["ja"]["system.overall"] = "\u5168\u4f53\u306e\u72b6\u614b"
	uiText["ja"]["system.next_action"] = "\u6b21\u306b\u5fc5\u8981\u306a\u64cd\u4f5c"
	uiText["en"]["system.kitsu"] = "Kitsu connection"
	uiText["en"]["system.discord"] = "Discord Bot"
	uiText["en"]["system.production"] = "Production connections"
	uiText["en"]["system.notifications"] = "Notifications"
	uiText["en"]["system.overall"] = "Overall status"
	uiText["en"]["system.next_action"] = "Next required action"
	uiText["ja"]["production.unconnected.status"] = "\u672a\u63a5\u7d9a"
	uiText["ja"]["production.unconnected.explanation"] = "\u3053\u306eProduction\u306f\u307e\u3060KitsuSync\u306b\u63a5\u7d9a\u3055\u308c\u3066\u3044\u307e\u305b\u3093\u3002Discord\u30b5\u30fc\u30d0\u30fc\u3001\u901a\u77e5\u5148\u3001\u30e6\u30fc\u30b6\u30fc\u8a2d\u5b9a\u3001\u30b9\u30c8\u30ec\u30fc\u30b8\u8a2d\u5b9a\u306f\u63a5\u7d9a\u5f8c\u306b\u5229\u7528\u3067\u304d\u307e\u3059\u3002"
	uiText["ja"]["production.unconnected.configure"] = "\u63a5\u7d9a\u3092\u8a2d\u5b9a"
	uiText["ja"]["production.unconnected.back"] = "Production\u4e00\u89a7\u3078\u623b\u308b"
	uiText["en"]["production.unconnected.source"] = "Loaded from Kitsu"
	uiText["en"]["production.unconnected.status"] = "Not connected"
	uiText["en"]["production.unconnected.explanation"] = "This Production is not yet connected to KitsuSync. Discord server, notification, user, and storage settings become available after connection."
	uiText["en"]["production.unconnected.configure"] = "Configure connection"
	uiText["en"]["production.unconnected.back"] = "Back to Productions"
	uiText["ja"]["channel_plan.duplicate_name"] = "\u8907\u6570\u306eTask Type\u304c\u540c\u3058\u540d\u524d\u3067\u3001\u540c\u3058Discord\u30c1\u30e3\u30f3\u30cd\u30eb\u540d\u306b\u306a\u308a\u307e\u3059\u3002Kitsu\u5074\u3067Task Type\u540d\u3092\u5909\u66f4\u3059\u308b\u304b\u3001\u63a5\u7d9a\u524d\u306b\u5225\u3005\u306e\u30c1\u30e3\u30f3\u30cd\u30eb\u540d\u3092\u6307\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044\u3002"
	uiText["en"]["channel_plan.duplicate_name"] = "Multiple Task Types have the same name and resolve to the same Discord channel. Rename the Task Types in Kitsu or assign distinct channel names before connecting."
	uiText["ja"]["connections.status_required"] = "\u5bfe\u5fdc\u304c\u5fc5\u8981"
	uiText["ja"]["connections.status_configured"] = "\u8a2d\u5b9a\u6e08\u307f"
	uiText["ja"]["connections.hint_separate"] = "Kitsu\u63a5\u7d9a\u3068Discord Bot\u63a5\u7d9a\u3092\u305d\u308c\u305e\u308c\u78ba\u8a8d\u3057\u3066\u304f\u3060\u3055\u3044"
	uiText["ja"]["connections.hint_kitsu"] = "Kitsu\u63a5\u7d9a\u3092\u8a2d\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044"
	uiText["ja"]["connections.hint_discord"] = "Discord Bot\u63a5\u7d9a\u3092\u8a2d\u5b9a\u3057\u3066\u304f\u3060\u3055\u3044"
	uiText["ja"]["connections.persistence_now"] = "\u5909\u66f4\u306f\u5b9f\u884c\u4e2d\u306e\u30d7\u30ed\u30bb\u30b9\u306b\u53cd\u6620\u3055\u308c\u3001\u30a2\u30d7\u30ea\u8a2d\u5b9a\u306b\u3082\u4fdd\u5b58\u3055\u308c\u307e\u3059\u3002"
	uiText["ja"]["connections.persistence_restart"] = "\u518d\u8d77\u52d5\u5f8c\u306f\u4fdd\u5b58\u6e08\u307f\u306e\u8a2d\u5b9a\u304c\u512a\u5148\u3055\u308c\u307e\u3059\u3002\u74b0\u5883\u5909\u6570\u306f\u4e88\u5099\u306e\u8a2d\u5b9a\u3068\u3057\u3066\u306e\u307f\u4f7f\u7528\u3057\u307e\u3059\u3002"
	uiText["ja"]["wizard.error.select_production"] = "Production\u3092\u9078\u629e\u3057\u3066\u304f\u3060\u3055\u3044"
	uiText["ja"]["wizard.error.invalid_production"] = "\u9078\u629e\u3057\u305fProduction\u3092\u78ba\u8a8d\u3067\u304d\u307e\u305b\u3093\u3067\u3057\u305f"
	uiText["ja"]["wizard.error.already_connected"] = "\u3053\u306eProduction\u306f\u3059\u3067\u306b\u9023\u643a\u3055\u308c\u3066\u3044\u307e\u3059"
	uiText["ja"]["wizard.production_title"] = "Kitsu Production\u3092\u9078\u629e"
	uiText["ja"]["wizard.production_label"] = "Production"
	uiText["ja"]["wizard.production_help"] = "\u9023\u643a\u6e08\u307f\u306eProduction\u306f\u9078\u629e\u3067\u304d\u307e\u305b\u3093"
	uiText["ja"]["wizard.select_production"] = "Production\u3092\u9078\u629e"
	uiText["ja"]["wizard.already_connected"] = "\u9023\u643a\u6e08\u307f"
	uiText["ja"]["wizard.next"] = "\u6b21\u3078"
	uiText["ja"]["wizard.back"] = "\u623b\u308b"
	uiText["en"]["connections.status_required"] = "Action required"
	uiText["en"]["connections.status_configured"] = "Configured"
	uiText["en"]["connections.hint_separate"] = "Review Kitsu and Discord Bot connections separately."
	uiText["en"]["connections.hint_kitsu"] = "Complete the Kitsu connection first."
	uiText["en"]["connections.hint_discord"] = "Complete the Discord Bot connection."
	uiText["en"]["connections.persistence_now"] = "Token changes take effect immediately for the running process and are also saved in app settings."
	uiText["en"]["connections.persistence_restart"] = "After restart, the saved token is used first. Environment variables remain fallback sources only."
	uiText["ja"]["connections.host_help"] = "Kitsuサーバーの接続先を入力してください。"
	uiText["ja"]["connections.account_help"] = "Kitsuへの接続に使用するアカウントです。"
	uiText["ja"]["connections.password_help"] = "保存済みのパスワードは表示されません。"
	uiText["ja"]["connections.token_help"] = "必要な場合だけ、新しいTokenを入力してください。保存済みのTokenは表示されません。変更は保存後に反映されます。"
	uiText["en"]["connections.host_help"] = "Enter the address of the Kitsu server."
	uiText["en"]["connections.account_help"] = "This account is used to connect to Kitsu."
	uiText["en"]["connections.password_help"] = "The saved password is not displayed."
	uiText["en"]["connections.token_help"] = "Enter a new token only when needed. The saved token is not displayed. Changes take effect after saving."
	uiText["en"]["wizard.error.select_production"] = "Select a Production"
	uiText["en"]["wizard.error.invalid_production"] = "The selected Production could not be verified"
	uiText["en"]["wizard.error.already_connected"] = "This Production is already connected"
	uiText["ja"]["dashboard.no_recent_activity"] = "最近のアクティビティはありません。"
	uiText["en"]["dashboard.no_recent_activity"] = "No recent activity."
	uiText["ja"]["dashboard.setup_required"] = "初期設定が必要です"
	uiText["en"]["dashboard.setup_required"] = "Setup required"
	uiText["ja"]["dashboard.unavailable"] = "利用不可"
	uiText["en"]["dashboard.unavailable"] = "Unavailable"
	uiText["ja"]["copy.task_type_check"] = "\u78ba\u8a8d\u3059\u308bTask Type"
	uiText["en"]["copy.task_type_check"] = "Task Type to check"
	uiText["ja"]["copy.check_without_sending"] = "\u9001\u4fe1\u305b\u305a\u306b\u78ba\u8a8d"
	uiText["en"]["copy.check_without_sending"] = "Check without sending"
	uiText["ja"]["copy.pause_notifications"] = "\u901a\u77e5\u3092\u4e00\u6642\u505c\u6b62"
	uiText["en"]["copy.pause_notifications"] = "Pause notifications"
	uiText["ja"]["copy.resume_notifications"] = "\u901a\u77e5\u3092\u518d\u958b"
	uiText["en"]["copy.resume_notifications"] = "Resume notifications"
	uiText["ja"]["copy.bot_connection"] = "Bot\u63a5\u7d9a"
	uiText["en"]["copy.bot_connection"] = "Bot Connection"
	uiText["ja"]["copy.kitsu_connection"] = "Kitsu\u63a5\u7d9a"
	uiText["en"]["copy.kitsu_connection"] = "Kitsu connection"
	uiText["ja"]["copy.connection_state"] = "\u63a5\u7d9a\u72b6\u614b"
	uiText["en"]["copy.connection_state"] = "Connection state"
	uiText["ja"]["copy.kitsu_account"] = "Kitsu\u9023\u643a\u30a2\u30ab\u30a6\u30f3\u30c8"
	uiText["en"]["copy.kitsu_account"] = "Kitsu integration account"
	uiText["ja"]["copy.kitsu_password"] = "Kitsu\u9023\u643a\u30d1\u30b9\u30ef\u30fc\u30c9"
	uiText["en"]["copy.kitsu_password"] = "Kitsu integration password"
	uiText["ja"]["copy.open_bot_connection"] = "Bot\u63a5\u7d9a\u3092\u958b\u304f"
	uiText["en"]["copy.open_bot_connection"] = "Open Bot Connection"
	uiText["ja"]["copy.notification_destinations"] = "\u901a\u77e5\u5148\u8a2d\u5b9a"
	uiText["en"]["copy.notification_destinations"] = "Notification destinations"
	uiText["ja"]["copy.discord_server"] = "Discord\u30b5\u30fc\u30d0\u30fc"
	uiText["en"]["copy.discord_server"] = "Discord server"
	uiText["ja"]["copy.discord_server_fallback"] = "Discord\u30b5\u30fc\u30d0\u30fc\u306e\u4e88\u5099\u8a2d\u5b9a"
	uiText["en"]["copy.discord_server_fallback"] = "Discord server fallback"
	uiText["ja"]["copy.step2"] = "Step 2: \u63a5\u7d9a\u6e08\u307fProduction\u3092\u7ba1\u7406"
	uiText["en"]["copy.step2"] = "Step 2: Manage connected productions"
	uiText["ja"]["copy.step3"] = "Step 3: Discord\u30b5\u30fc\u30d0\u30fc\u3092\u9078\u629e"
	uiText["en"]["copy.step3"] = "Step 3: Assign Discord Server / Guild"
	uiText["ja"]["copy.step4"] = "Step 4: \u6700\u7d42\u78ba\u8a8d"
	uiText["en"]["copy.step4"] = "Step 4: Final Health Check"
	uiText["ja"]["copy.final_check"] = "\u6700\u7d42\u78ba\u8a8d"
	uiText["en"]["copy.final_check"] = "Final Health Check"
	uiText["ja"]["copy.review_system_status"] = "\u30b7\u30b9\u30c6\u30e0\u72b6\u614b\u3092\u78ba\u8a8d"
	uiText["en"]["copy.review_system_status"] = "Review System Status"
	uiText["ja"]["copy.open_connected_productions"] = "\u63a5\u7d9a\u6e08\u307fProduction\u3092\u958b\u304f"
	uiText["en"]["copy.open_connected_productions"] = "Open Connected Productions"
	uiText["ja"]["copy.main_task"] = "\u4e3b\u306a\u64cd\u4f5c"
	uiText["en"]["copy.main_task"] = "Main task"
	uiText["ja"]["copy.kitsu_production"] = "Kitsu Production"
	uiText["en"]["copy.kitsu_production"] = "Kitsu project"
	uiText["ja"]["copy.production_type"] = "Production\u306e\u7a2e\u985e"
	uiText["en"]["copy.production_type"] = "Project type"
	uiText["ja"]["copy.select_production_type"] = "Production\u306e\u7a2e\u985e\u3092\u9078\u629e"
	uiText["en"]["copy.select_production_type"] = "Select project type"
	uiText["ja"]["copy.language"] = "\u8a00\u8a9e"
	uiText["en"]["copy.language"] = "Language"
	uiText["ja"]["copy.japanese"] = "\u65e5\u672c\u8a9e"
	uiText["en"]["copy.japanese"] = "Japanese"
	uiText["ja"]["copy.run_setup"] = "\u63a5\u7d9a\u3092\u5b9f\u884c"
	uiText["en"]["copy.run_setup"] = "Run Setup"
	uiText["ja"]["copy.bot_connection_checking"] = "Bot\u63a5\u7d9a\u3092\u78ba\u8a8d\u4e2d..."
	uiText["en"]["copy.bot_connection_checking"] = "Bot account setup..."
	uiText["ja"]["copy.setting_up"] = "\u8a2d\u5b9a\u4e2d..."
	uiText["en"]["copy.setting_up"] = "Setting up..."
	uiText["ja"]["copy.setup_complete"] = "\u63a5\u7d9a\u8a2d\u5b9a\u304c\u5b8c\u4e86\u3057\u307e\u3057\u305f"
	uiText["en"]["copy.setup_complete"] = "Setup Complete"
	uiText["ja"]["copy.setup_failed"] = "\u63a5\u7d9a\u8a2d\u5b9a\u306b\u5931\u6557\u3057\u307e\u3057\u305f"
	uiText["en"]["copy.setup_failed"] = "Setup Failed"
	uiText["ja"]["copy.no_activity"] = "\u30a2\u30af\u30c6\u30a3\u30d3\u30c6\u30a3\u306f\u3042\u308a\u307e\u305b\u3093\u3002"
	uiText["en"]["copy.no_activity"] = "No activity yet."
	uiText["ja"]["copy.discord_bot"] = "Discord Bot"
	uiText["en"]["copy.discord_bot"] = "Discord Bot"
	uiText["ja"]["copy.unknown"] = "\u4e0d\u660e"
	uiText["en"]["copy.unknown"] = "Unknown"
	uiText["ja"]["copy.failed"] = "\u5931\u6557"
	uiText["en"]["copy.failed"] = "Failed"
	uiText["ja"]["copy.ready"] = "\u6e96\u5099\u5b8c\u4e86"
	uiText["en"]["copy.ready"] = "Ready"
	uiText["ja"]["copy.partial"] = "\u4e00\u90e8\u672a\u5b8c\u4e86"
	uiText["en"]["copy.partial"] = "Partial"
	uiText["ja"]["copy.reachable"] = "\u63a5\u7d9a\u5148\u3092\u78ba\u8a8d\u6e08\u307f"
	uiText["en"]["copy.reachable"] = "Reachable"
	uiText["ja"]["copy.authenticated"] = "\u8a8d\u8a3c\u6e08\u307f"
	uiText["en"]["copy.authenticated"] = "Authenticated"
	uiText["ja"]["copy.not_sent"] = "\u672a\u9001\u4fe1"
	uiText["en"]["copy.not_sent"] = "Not sent yet"
	uiText["ja"]["copy.delivered"] = "\u9001\u4fe1\u6e08\u307f"
	uiText["en"]["copy.delivered"] = "Delivered"
	uiText["ja"]["copy.not_ready"] = "\u6e96\u5099\u304c\u5fc5\u8981"
	uiText["en"]["copy.not_ready"] = "Not ready"
	uiText["ja"]["copy.production_id"] = "Production ID"
	uiText["en"]["copy.production_id"] = "Project ID"
	uiText["ja"]["copy.operator_workflow"] = "\u64cd\u4f5c\u306e\u6d41\u308c"
	uiText["en"]["copy.operator_workflow"] = "Operator workflow"
	uiText["ja"]["copy.setup_required"] = "\u521d\u671f\u8a2d\u5b9a\u304c\u5fc5\u8981\u3067\u3059"
	uiText["en"]["copy.setup_required"] = "Setup required"
	uiText["ja"]["ia.productions"] = "プロダクション"
	uiText["ja"]["ia.production_list"] = "プロダクション一覧"
	uiText["ja"]["ia.new_connection"] = "新しいプロダクションを接続"
	uiText["ja"]["system.production"] = "プロダクション接続"
	uiText["ja"]["wizard.description"] = "プロダクションとDiscordサーバーを順に選び、実行前に内容を確認します。"
	uiText["ja"]["wizard.step_production"] = "プロダクション"
	uiText["ja"]["wizard.production_title"] = "Kitsuプロダクションを選択"
	uiText["ja"]["wizard.production_label"] = "プロダクション"
	uiText["ja"]["wizard.production_help"] = "連携済みのプロダクションは選択できません"
	uiText["ja"]["wizard.select_production"] = "プロダクションを選択"
	uiText["ja"]["wizard.error.select_production"] = "プロダクションを選択してください"
	uiText["ja"]["wizard.error.invalid_production"] = "選択したプロダクションを確認できませんでした"
	uiText["ja"]["wizard.error.already_connected"] = "このプロダクションはすでに連携済みです"
	uiText["ja"]["wizard.prerequisites_ready"] = "必要な接続がそろいました。プロダクションを選択できます。"
	uiText["ja"]["wizard.prerequisites_title"] = "接続の前提条件"
	uiText["ja"]["wizard.open_kitsu"] = "Kitsu接続を設定"
	uiText["ja"]["wizard.already_connected"] = "連携済み"
	uiText["ja"]["production.unconnected.explanation"] = "このプロダクションはまだKitsuSyncに接続されていません。Discordサーバー、通知先、ユーザー設定、ストレージ設定は接続後に利用できます。"
	uiText["ja"]["production.unconnected.back"] = "プロダクション一覧へ戻る"
	uiText["ja"]["copy.kitsu_production"] = "Kitsuプロダクション"
	uiText["ja"]["copy.production_type"] = "プロダクションの種類"
	uiText["ja"]["copy.select_production_type"] = "プロダクションの種類を選択"
	uiText["en"]["system.production"] = "Production connections"
	uiText["en"]["wizard.open_kitsu"] = "Configure Kitsu connection"
}

func tr(lang, key string) string {
	if lang != "en" {
		lang = "ja"
	}
	if value, ok := uiText[lang][key]; ok {
		return value
	}
	return "[missing translation: " + key + "]"
}

func trf(lang, key string, args ...any) string {
	return fmt.Sprintf(tr(lang, key), args...)
}
