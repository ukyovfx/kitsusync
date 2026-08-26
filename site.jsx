const { useEffect, useState } = React;

const sections = {
  ja: [
    { id: 'overview', num: '01', label: '概要', kicker: 'KitsuSync公式ガイド', title: 'KitsuSyncの使い方', subtitle: 'KitsuのプロダクションをDiscord通知につなぐ', intro: 'KitsuSyncは、Kitsuのプロダクションで起きたタスクの変化を、プロダクションごとに設定したDiscordチャンネルへ通知する管理アプリです。管理者は、接続設定、プロダクションの通知ルーティング、ユーザー割り当て、監査・診断を一つの画面で行えます。', bullets: ['管理者ログイン後、接続設定でKitsuとDiscord Botを確認します。', '新しいプロダクションを接続ウィザードで選び、Discordサーバーとチャンネル計画を確認します。', 'プロダクションごとの通知ルーティングを確認し、必要に応じてユーザーとDiscord IDを割り当てます。', 'システム状態と監査ログで、通知の準備状況や失敗原因を確認します。'] },
    { id: 'routes', num: '02', label: '画面・URL', kicker: 'Pages & Routes', title: '画面・URL', subtitle: '目的ごとの入口', table: { headers: ['URL', '用途'], rows: [['/bot/login', '管理画面へのログイン'], ['/bot/setup', '新しいプロダクションの接続ウィザード'], ['/bot/admin', 'ダッシュボードと管理メニュー'], ['/bot/admin/bot', 'Kitsu接続とDiscord Bot接続'], ['/bot/admin/projects', 'プロダクション一覧とプロダクション詳細'], ['/bot/admin/users', 'KitsuユーザーとDiscord IDのユーザー紐づけ'], ['/bot/admin/checkers', '互換URL。ユーザー紐づけへ転送'], ['/bot/admin/health', 'システム状態、API応答、運用診断'], ['/bot/admin/audit', '監査ログとDaily Summary'], ['/bot/docs/', 'この公式ドキュメント'] ] } },
    { id: 'setup', num: '03', label: 'セットアップ', kicker: 'プロダクション接続', title: 'プロダクション接続ウィザード', subtitle: '準備状況から完了までの7段階', intro: '初回接続は、途中の確認画面でDiscordへの変更内容を確認してから実行します。接続済みのプロダクションは通常の新規接続には表示されません。リソースが古い場合は、プロダクション詳細から修復を開始します。', bullets: ['Step 1「準備状況」: KitsuとDiscord Botの接続状態を確認します。未設定の場合は先に接続設定を完了します。', 'Step 2「プロダクション」: 接続するKitsu プロダクションを選択します。', 'Step 3「Discordサーバー」: 通知先のDiscordサーバーを選びます。Botがアクセスできるサーバーだけが表示されます。', 'Step 4「チャンネル計画」: Task TypeごとのDiscordチャンネル名と順序を確認・調整します。作成、再利用、競合の状態が表示されます。', 'Step 5「内容確認」: プロダクション、サーバー、カテゴリ、チャンネル順を確認し、明示的に同意します。', 'Step 6「実行」: 確認済みの計画に沿ってDiscordリソースとKitsuSyncのルーティングを設定します。', 'Step 7「完了」: 保存結果を確認し、プロダクション詳細を開きます。競合や作成失敗がある場合は、画面の案内に従って計画を見直します。'] },
    { id: 'channels', num: '04', label: '通知チャンネル', kicker: 'Notification Routing', title: 'プロダクションの通知ルーティング', subtitle: 'Kitsu Task TypeからDiscordチャンネルへ', intro: 'プロダクション詳細の「通知」では、Kitsu Task TypeとDiscordチャンネルの対応を確認できます。通常は読み取り専用で表示し、編集するときだけ「編集」を選びます。', bullets: ['ルーティングの識別にはプロダクション IDと安定したTask Type IDを使います。表示名だけでは送信先を決めません。', '編集では、既存ルートの順序変更、送信先変更、ルート追加、通知対象からの除外をまとめて確認してから「変更を適用」します。', '「通知対象から外す」はルートだけを外し、Kitsu Task TypeやDiscordチャンネルを削除しません。', 'Discordチャンネルを削除する場合は、所有確認、権限確認、チャンネル名の入力が必要です。', 'チャンネルやWebhookが見つからない、所有情報が曖昧、権限が不足している場合は推測せず、修復または診断画面を使います。'] },
    { id: 'assignments', num: '05', label: 'ユーザー割り当て', kicker: 'Users & Roles', title: 'ユーザー・Discord ID・Reviewer / Checker', subtitle: '人の通知先とプロダクション内の役割', bullets: ['「ユーザー紐づけ」でKitsuの人間ユーザーとDiscordユーザーIDを対応付けます。Kitsu Botは通常の候補から除外されます。', 'プロダクション詳細の「ユーザー設定」では、グローバルに紐づいた人間ユーザーをそのプロダクションへ関連付けます。', 'Reviewer / Checkerはプロダクションに関連付けたユーザーから、Task Typeごとに割り当てます。', 'Discord IDが未設定でも、紐づけがないだけで通知全体が失敗することはありません。必要なときはユーザー紐づけを確認します。'] },
    { id: 'notifications', num: '06', label: '通知', kicker: 'Notification Behavior', title: '通知の動作', subtitle: 'Kitsuの変化を安全にDiscordへ届ける', intro: 'KitsuSyncはKitsuを読み取り、通知対象の変化だけをプロダクションのルートへ送ります。管理画面の言語と、プロダクションごとの通知言語は別に設定されます。', bullets: ['現在の通知イベントは、対象ステータスの変更、コメント更新、通知設定が有効な割り当て通知です。', 'プロダクション IDと安定したTask Type IDのルートがあり、完全なWebhook記録とDiscordチャンネルが確認できる場合だけ送信します。', 'ルートなし、古いTask Type、欠落チャンネル、欠落Webhook、曖昧な所有情報は送信せず、診断に記録します。', '通知言語はプロダクション単位で設定します。管理画面のJP/EN切り替えは通知本文の言語を変更しません。', '同じタスクの状態は重複送信を避け、成功した送信結果は監査ログと配信状態で追跡できます。'] },
    { id: 'preview', num: '07', label: 'プレビュー', kicker: 'Preview & Links', title: '通知プレビューと保存先リンク', subtitle: '送信前に内容と補助リンクを確認', intro: '通知プレビューが表示される画面では、選択したTask Typeの送信先と、決定的なJP/ENレンダラーが作るDiscordメッセージを読み取り専用で確認できます。プレビューは送信しません。', bullets: ['プレビューではプロダクション、Task Type、Discord チャンネル、プロダクションの通知言語、メンション対象の有無を確認します。', 'KitsuのタスクIDと設定済みホストがそろう場合だけKitsuリンクを表示します。表示名からURLを推測しません。', 'プレビュー画像やStorageリンクは、Kitsuから取得できる情報とプロダクションに設定した補助リンクだけを使います。', '送信先が未設定またはリソースが古い場合は、成功したように表示せず、通知ルーティングと診断を確認します。'] },
    { id: 'audit', num: '08', label: '監査・診断', kicker: 'Audit & Diagnostics', title: '監査ログとシステム状態', subtitle: '通知結果と問題の切り分け', bullets: ['「システム状態」ではKitsu、Discord Bot、プロダクションルーティング、通知準備状況を確認します。APIの応答時間と直近の観測も確認できます。', '「監査ログ」では通知対象、Task Type、状態、メッセージID、結果など、運用に必要な送信結果を確認します。', 'プロダクション詳細の「トラブルシューティング」では、接続、ルーティング、参加者取得、ユーザー紐づけ、最近の通知処理を順に確認します。', '問題がある場合は、原因と次の操作を確認してから修復します。古いチャンネルやWebhookを推測で再利用しません。'] },
    { id: 'data', num: '09', label: 'データ・バックアップ', kicker: 'Data & Recovery', title: 'データ・バックアップ', subtitle: '保存される設定と復旧の考え方', intro: 'KitsuSyncは、接続設定、プロダクションとの対応、ルーティング、ユーザー割り当て、監査情報をローカルデータとして保存します。運用前に保存先とバックアップ手順を決めてください。', bullets: ['SQLiteデータにはプロダクション、Discordカテゴリ・チャンネルの識別子、Webhook記録、ユーザー割り当て、監査情報が保存されます。', 'KitsuとDiscordのトークンなどの秘密情報は暗号化して保存し、画面やログに平文で表示しません。', 'バックアップはSQLiteデータ、ランタイム秘密鍵、運用設定を同じ世代で保管します。秘密鍵だけ、またはDBだけを戻すと復号や接続ができなくなることがあります。', '復旧後は管理画面、/health、プロダクション詳細のルーティングを確認し、Discord側のIDが現存するか診断します。', '不完全なセットアップや修復失敗では、既存の無関係なDiscordリソースを削除せず、診断結果とロールバック状態を確認します。'] },
    { id: 'security', num: '10', label: 'セキュリティ', kicker: 'Secure Operations', title: '安全な運用', subtitle: '管理者が守る項目', bullets: ['管理者アカウントとセッションを保護し、必要な人だけに管理画面を公開します。外部公開時はHTTPSと信頼できるリバースプロキシを使います。', 'KitsuとDiscordのトークン、Webhook URL、ランタイム秘密鍵をチャット、スクリーンショット、ログ、ドキュメントに貼り付けません。漏えいした可能性があれば、トークンやWebhookをローテーションします。', 'Discord Botには目的に必要な最小権限だけを付与します。チャンネル作成・並べ替え・削除やWebhook管理が必要な操作では、対象Guild、カテゴリ、チャンネルの所有確認も行います。', 'KitsuSyncは曖昧な所有情報や欠落リソースを推測で操作しません。まず「システム状態」またはプロダクション詳細の「トラブルシューティング」を確認します。', 'SQLite、ランタイム秘密鍵、運用設定のバックアップを安全に保管し、復旧テストでは秘密情報をログに出さないことを確認します。'] }
  ],
  en: [
    { id: 'overview', num: '01', label: 'Overview', kicker: 'Official KitsuSync Guide', title: 'How KitsuSync works', subtitle: 'Connect Kitsu Productions to Discord notifications', intro: 'KitsuSync is an admin application that sends changes from Kitsu Productions to the Discord channels configured for each Production. Administrators manage connections, Production routing, user assignments, audit records, and diagnostics in one place.', bullets: ['Sign in as an administrator and verify the Kitsu and Discord Bot connections.', 'Use the New Production Connection wizard to choose a Production, Discord server, and channel plan.', 'Review each Production’s notification routing and assign human Kitsu users to Discord IDs when needed.', 'Use System Status and Audit Log to confirm readiness and investigate delivery problems.'] },
    { id: 'routes', num: '02', label: 'Pages & Routes', kicker: 'Pages & Routes', title: 'Pages & Routes', subtitle: 'Where to go for each task', table: { headers: ['URL', 'Purpose'], rows: [['/bot/login', 'Sign in to the admin area'], ['/bot/setup', 'New Production Connection wizard'], ['/bot/admin', 'Dashboard and management menu'], ['/bot/admin/bot', 'Kitsu and Discord Bot connections'], ['/bot/admin/projects', 'Production list and Production details'], ['/bot/admin/users', 'Link Kitsu people to Discord IDs'], ['/bot/admin/checkers', 'Compatibility URL; redirects to User Linking'], ['/bot/admin/health', 'System Status, API response, and diagnostics'], ['/bot/admin/audit', 'Audit Log and Daily Summary'], ['/bot/docs/', 'This official documentation'] ] } },
    { id: 'setup', num: '03', label: 'Setup', kicker: 'Production Connection', title: 'Production connection wizard', subtitle: 'Seven steps from prerequisites to completion', intro: 'The first connection is staged: review the Discord changes before explicitly executing them. A Production that is already connected is not available for normal new setup; use the Production repair action when its resources are stale or missing.', bullets: ['Step 1 “Prerequisites”: verify Kitsu and Discord Bot readiness. Configure a missing connection first.', 'Step 2 “Production”: select the Kitsu Production to connect.', 'Step 3 “Discord Server”: choose the notification server. Only servers the Bot can access are listed.', 'Step 4 “Channel Plan”: review Task Type channel names and order. The plan shows create, reuse, and conflict states.', 'Step 5 “Review”: confirm the Production, server, category, and final channel order, then explicitly agree.', 'Step 6 “Execute”: apply the confirmed Discord resource and KitsuSync routing plan.', 'Step 7 “Complete”: review the saved result and open the Production. If a conflict or failure appears, revise the plan using the supplied recovery guidance.'] },
    { id: 'channels', num: '04', label: 'Channels', kicker: 'Notification Routing', title: 'Production notification routing', subtitle: 'Kitsu Task Type to Discord Channel', intro: 'Production details show the mapping between Kitsu Task Types and Discord Channels. The normal view is read-only; choose Edit when a routing change is needed.', bullets: ['Routes are identified by Production ID and stable Task Type ID. Display names alone are never used to choose a destination.', 'Edit mode stages reorder, destination changes, route additions, and removals until Apply changes is selected.', 'Remove from notifications removes only the KitsuSync route. It does not delete the Kitsu Task Type or Discord Channel.', 'Deleting a Discord channel requires ownership and permission checks plus exact channel-name confirmation.', 'Missing or ambiguous channels and webhooks fail closed. Use the repair flow or diagnostics instead of guessing a destination.'] },
    { id: 'assignments', num: '05', label: 'Assignments', kicker: 'Users & Roles', title: 'Users, Discord IDs, and Reviewer / Checker', subtitle: 'Human notification targets and Production roles', bullets: ['User Linking maps a human Kitsu user to a Discord user ID. Kitsu Bot identities are excluded from normal candidates.', 'Production details can associate a globally linked human with one Production.', 'Reviewer / Checker assignments are Production-scoped and select an associated user for a Task Type.', 'A missing Discord ID does not make the entire notification fail; return to User Linking when a mention is needed.'] },
    { id: 'notifications', num: '06', label: 'Notifications', kicker: 'Notification Behavior', title: 'Notification behavior', subtitle: 'Delivering Kitsu changes safely to Discord', intro: 'KitsuSync reads Kitsu and sends only supported changes through the Production route. The admin page language and each Production’s notification language are independent.', bullets: ['The supported events are status changes for notifiable statuses, comment updates, and assignment observations when assignment notifications are enabled.', 'A message is sent only when a Production + stable Task Type route, complete webhook record, and Discord channel are valid.', 'Missing routes, stale Task Types, missing channels or webhooks, and ambiguous ownership fail closed and are recorded for diagnosis.', 'Notification language is configured per Production. Switching the admin UI between Japanese and English does not change notification language.', 'Previously delivered task state is used to avoid duplicate sends, and successful results are traceable in Audit Log and delivery state.'] },
    { id: 'preview', num: '07', label: 'Preview', kicker: 'Preview & Links', title: 'Notification preview and storage links', subtitle: 'Review content and helper links before delivery', intro: 'Where the notification preview is available, it is read-only: select a Task Type to see its destination and the deterministic JP/EN Discord message. It never sends a message.', bullets: ['The preview identifies the Production, Task Type, Discord Channel, Production notification language, and whether mentions would be used.', 'A Kitsu link is shown only when the configured host and stable IDs are available. URLs are never guessed from display names.', 'Preview images and Storage Links use only data returned by Kitsu and links configured for the Production.', 'If a destination is missing or stale, the preview does not claim success. Review routing and diagnostics instead.'] },
    { id: 'audit', num: '08', label: 'Audit', kicker: 'Audit & Diagnostics', title: 'Audit Log and System Status', subtitle: 'Confirm delivery and find the cause of problems', bullets: ['System Status shows Kitsu, Discord Bot, Production routing, notification readiness, API response time, and recent observations.', 'Audit Log shows operational delivery results such as target, Task Type, status, message ID, and outcome.', 'Production Troubleshooting groups connection, routing, participant retrieval, User Linking, and recent notification processing checks.', 'When a problem appears, follow the stated cause and next action. Do not guess at stale channels or webhooks.'] },
    { id: 'data', num: '09', label: 'Data & Backup', kicker: 'Data & Recovery', title: 'Data & Backup', subtitle: 'Persisted settings and recovery guidance', intro: 'KitsuSync stores connection settings, Production relationships, routing, user assignments, and audit information locally. Decide how these files are protected and backed up before operating the service.', bullets: ['SQLite contains Production and Discord identifiers, webhook records, user assignments, and audit information.', 'Kitsu and Discord tokens are encrypted at rest and are not rendered or logged in plain text.', 'Back up the SQLite data, runtime secret key, and operational configuration as one generation. Restoring only the database or only the key can make secrets unreadable.', 'After recovery, verify the admin UI, /health, and Production routing, then check that referenced Discord IDs still exist.', 'A failed or incomplete setup should not delete unrelated Discord resources. Review rollback and diagnostics before retrying.'] },
    { id: 'security', num: '10', label: 'Security', kicker: 'Secure Operations', title: 'Secure operations', subtitle: 'What administrators must protect', bullets: ['Protect administrator accounts and sessions, expose the admin area only to the people who need it, and use HTTPS with a trusted reverse proxy when exposed beyond the local host.', 'Never put Kitsu or Discord tokens, webhook URLs, or the runtime secret key in chat, screenshots, logs, or documentation. Rotate tokens or webhooks if exposure is suspected.', 'Grant the Discord Bot only the permissions required for its tasks. Resource management also requires checks for the target Guild, category, channel ownership, and the relevant Discord permission.', 'KitsuSync fails closed when ownership is ambiguous or a resource is missing. Start with System Status or Production Troubleshooting.', 'Store backups securely and test recovery without exposing secrets.'] }
  ]
};

const guidance = {
  ja: {
    overview: { purpose: 'KitsuのProductionで起きた変化を、担当するDiscordチャンネルへ届けるための全体像を説明します。', when: '初めて使うときや、通知が届くまでの流れを確認したいときに読みます。', steps: ['接続設定でKitsuとDiscord Botを確認します。', '「新しいプロダクションを接続」から対象のプロダクションを接続します。', '通知チャンネル、ユーザー紐づけ、Reviewer / Checkerを必要に応じて設定します。', '「システム状態」と「監査ログ」で動作を確認します。'], after: 'Kitsuの変更をKitsuSyncが検出し、プロダクションのTask Typeに合うDiscord チャンネルを決め、通知を送信または更新します。', notes: ['管理画面の言語と、プロダクションの通知言語は別です。', '接続や通知の準備が不十分な場合、KitsuSyncは推測で送信しません。'], check: 'まず「接続設定」、次に「システム状態」、最後に対象プロダクションの「トラブルシューティング」を順に確認します。' },
    routes: { purpose: '管理画面の各ページを、目的から探せるようにします。', when: 'どの画面を開けばよいか分からないときに使います。', steps: ['管理者ログイン後、ダッシュボードから目的に合うページを開きます。', '接続は「接続設定」、プロダクションは「プロダクション一覧」、通知確認は「システム状態」または「監査ログ」を選びます。', 'Reviewer / Checkerはプロダクション詳細の「ユーザー設定」で設定します。「/bot/admin/checkers」はユーザー紐づけへ転送する互換URLです。'], after: '選んだページで、現在の設定や状態だけを確認・変更できます。', notes: ['管理画面のページはログインが必要です。', 'このドキュメントのURLは読み取り専用です。'], check: 'ログイン画面に戻る場合はセッションが切れています。もう一度ログインしてから元のURLを開きます。' },
    setup: { purpose: 'Kitsuのプロダクションを、通知先のDiscordサーバーとチャンネルにつなぎます。', when: '新しいプロダクションを初めて接続するときに使います。', steps: ['Step 1「準備状況」でKitsuとDiscord Botが「接続済」か確認します。', 'Step 2「Production」で接続するプロダクションを選びます。', 'Step 3「Discordサーバー」で通知先のサーバーを選びます。', 'Step 4「チャンネル計画」でTask Typeごとのチャンネル名と順序を確認します。', 'Step 5「内容確認」で対象と変更内容を確認し、確認欄に同意します。', 'Step 6「実行」で接続を実行します。', 'Step 7「完了」で保存結果を確認し、プロダクションを開きます。'], after: '選択したサーバー内に、管理対象のカテゴリ、Task Type用のDiscordチャンネル、通知に必要な記録が設定されます。', notes: ['「作成」「再利用」「競合」の表示を実行前に確認します。', '接続済みのプロダクションは新規接続の対象になりません。リソースが古い場合はプロダクション詳細から修復します。', '確認画面で止まる場合、名前の重複、権限、所有確認を解決します。'], check: 'Step 1の接続状態、Step 3のサーバーアクセス、Step 4の競合表示を順に確認します。Discordの変更が途中で失敗した場合は、結果画面のロールバックと診断を確認してから再試行します。' },
    channels: { purpose: 'プロダクションのTask Typeを、通知先のDiscord チャンネルへ対応付けます。', when: '通知先を変更したい、順序を整えたい、不要な通知ルートを外したいときに使います。', steps: ['プロダクション詳細の「通知」を開き、「通知ルーティング」を確認します。', '変更するときだけ「編集」を選び、Task Type、Discord チャンネル、順序を確認します。', '追加・変更・並べ替えをまとめて行い、「変更を適用」を選びます。', '不要なルートは「通知対象から外す」を選びます。'], after: '保存が成功すると、KitsuSyncのルーティングと管理対象Discordチャンネルの順序が更新されます。', notes: ['「通知対象から外す」はルートだけを外します。Discordチャンネルは削除しません。', '「Discordチャンネルを削除」は別の危険操作です。正確なチャンネル名の入力と所有確認が必要です。'], check: '保存後も状態が変わらない場合は、Discord Botの権限、対象サーバー、カテゴリ、チャンネルの存在を「トラブルシューティング」で確認します。' },
    assignments: { purpose: 'Kitsuの人間ユーザーをDiscordユーザーと対応付け、プロダクション内の役割を設定します。', when: '通知に人を表示したい、Reviewer / Checkerを指定したいときに使います。', steps: ['「ユーザー紐づけ」でKitsuユーザーを選び、Discord IDを登録します。', 'プロダクション詳細の「ユーザー設定」で、そのプロダクションにユーザーを関連付けます。', 'Reviewer / CheckerでユーザーとTask Typeを選び、割り当てます。'], after: '通知を作るとき、登録済みのDiscord IDを安全なメンション候補として使います。役割はプロダクション単位で適用されます。', notes: ['Kitsu Botは人間ユーザーの候補に含まれません。', 'Discord IDがない場合、その人のメンションは作られませんが、通知全体は失敗しません。', 'グローバルなユーザー紐づけと、プロダクションへの関連付けは別です。'], check: '候補が表示されない場合は、先に「ユーザー紐づけ」で人間ユーザーを登録し、対象プロダクションへ関連付けてください。' },
    notifications: { purpose: 'どのKitsuの変化が、どの条件でDiscordへ通知されるかを説明します。', when: '通知が届く条件や、同じ通知が重複しない理由を確認したいときに使います。', steps: ['KitsuSyncはKitsuの変更を確認します。', '通知対象のステータス変更、コメント更新、設定で有効な割り当て通知を判定します。', 'プロダクションとTask TypeのルートからDiscord チャンネルを決めます。', '必要な条件がそろえば、Discordメッセージを新規送信または更新します。'], after: '成功した送信は監査ログと配信状態で追跡できます。同じ状態の繰り返しは重複送信されません。', notes: ['ルート、チャンネル、Webhookのどれかが欠ける場合は送信しません。', '通知言語はプロダクションごとに設定されます。', '現在の通知対象外ステータスや未知の形式は送信されません。'], check: '「接続設定」→「システム状態」→対象プロダクションの「通知ルーティング」→「監査ログ」の順に確認します。' },
    preview: { purpose: '独立した通知プレビュー画面はありません。通知の送信先と監査結果を確認する方法を案内します。', when: 'ルートや通知結果を確認したいときに使います。', steps: ['プロダクション詳細の「通知」でTask TypeとDiscordチャンネルの対応を確認します。', '送信された通知の結果は「監査ログ」で確認します。', '記録にKitsuリンクが表示されている場合は、リンク先が正しいことを確認します。'], after: '「通知」と「監査ログ」は読み取り専用の確認画面です。これらを開いてもDiscordへメッセージは送信されません。', notes: ['送信先が未設定またはリソースが古い場合は、通知ルーティングとトラブルシューティングを確認します。', 'URLはKitsuから取得できる安定したIDがある場合だけ表示されます。'], check: '通知先が分からない場合は「通知ルーティング」を、送信結果を確認したい場合は「監査ログ」を先に確認します。' },
    audit: { purpose: '通知の準備状況、送信結果、問題の原因を確認します。', when: '通知が届かない、接続状態を確認したい、復旧後の結果を確認したいときに使います。', steps: ['「システム状態」でKitsuとDiscord Botが接続済みか確認します。', 'Productionルーティングと通知準備状況が正常か確認します。', '対象プロダクションの「トラブルシューティング」で接続、ルーティング、参加者、ユーザー紐づけを確認します。', '「監査ログ」で対象Task Typeと送信結果を確認します。'], after: '診断には、問題がある項目の原因と次に行う操作が表示されます。', notes: ['Discordの外部状態が不明な場合は成功とみなしません。', '古いチャンネルやWebhookを名前だけで再利用しません。'], check: 'Kitsu、Discord Bot、対象サーバー、カテゴリ、ルート、Webhook、監査ログの順に一つずつ確認します。' },
    data: { purpose: '設定と復旧に必要な保存データを確認します。', when: 'バックアップを作るとき、環境を復旧するとき、保存先を引き継ぐときに使います。', steps: ['SQLiteデータ、ランタイム秘密鍵、運用設定の保管場所を確認します。', '同じ時点の3つを安全な場所へバックアップします。', '復旧後に管理画面、/health、プロダクションのルーティングを確認します。'], after: '復旧した設定と秘密情報がそろっていれば、保存済みの接続やルーティングを読み戻せます。Discord側のチャンネルが現存するかは別途確認されます。', notes: ['SQLiteだけ、または秘密鍵だけを戻すと、暗号化された接続情報を使えない場合があります。', 'バックアップファイルや秘密鍵を画面・ログへ表示しません。'], check: 'まずファイルの世代をそろえ、次に/health、最後にDiscordリソースとプロダクションの診断を確認します。' },
    security: { purpose: '管理画面、接続情報、Discordリソースを安全に運用するための注意点を示します。', when: '初期設定、公開範囲の変更、権限設定、バックアップ、復旧の前に確認します。', steps: ['管理者ログインを必要な人だけに限定します。', '外部から利用する場合はHTTPSと信頼できるリバースプロキシを使います。', 'KitsuとDiscordのトークン、Webhook URL、ランタイム秘密鍵を秘密として扱います。', 'Discord Botには必要なGuildと操作に必要な最小権限だけを与えます。'], after: 'KitsuSyncは所有情報や権限を確認できない操作を止め、診断に安全なエラーを表示します。', notes: ['Webhook URLやトークンをログ、スクリーンショット、チャットへ貼りません。', '漏えいが疑われる場合は、該当するトークンやWebhookをローテーションします。', 'バックアップと復旧テストでも秘密情報を露出させません。'], check: '管理者アクセス、HTTPS、Bot権限、対象Guild、リソース所有、秘密情報の漏えい有無を順に確認します。' }
  },
  en: {
    overview: { purpose: 'This section explains how Kitsu changes reach the Discord channel assigned to a Production.', when: 'Read it when you are new to KitsuSync or need to understand the complete notification flow.', steps: ['Verify Kitsu and the Discord Bot in Connections.', 'Connect a Production from New Production Connection.', 'Review notification channels and configure User Linking and Reviewer / Checker when needed.', 'Use System Status and Audit Log to confirm operation.'], after: 'KitsuSync detects a Kitsu change, resolves the Discord Channel for the Production and Task Type, then sends or updates the notification.', notes: ['Admin UI language and Production notification language are separate.', 'KitsuSync does not guess a destination when a connection or route is incomplete.'], check: 'Check Connections first, then System Status, then the Production Troubleshooting view.' },
    routes: { purpose: 'This section helps you find the correct KitsuSync page for each task.', when: 'Use it when you are unsure where to change a setting or inspect a result.', steps: ['Sign in and start from Dashboard.', 'Use Connections for services, Production list for Production details, and System Status or Audit Log for notification checks.', 'Set Production-scoped Reviewer / Checker roles under Production details > Users. The compatibility URL /bot/admin/checkers leads to User Linking.'], after: 'The selected page shows the settings or status for that task.', notes: ['Admin pages require an authenticated session.', 'The documentation page itself is read-only.'], check: 'If you are returned to login, sign in again and reopen the original URL.' },
    setup: { purpose: 'This wizard connects one Kitsu Production to a Discord server and its notification channels.', when: 'Use it for a new Production connection.', steps: ['Step 1 “Prerequisites”: confirm Kitsu and Discord Bot are Connected.', 'Step 2 “Production”: select the Kitsu Production.', 'Step 3 “Discord Server”: select the notification server.', 'Step 4 “Channel Plan”: review Task Type channel names and order.', 'Step 5 “Review”: confirm the targets and agree to the planned changes.', 'Step 6 “Execute”: run the confirmed connection.', 'Step 7 “Complete”: check the result and open the Production.'], after: 'KitsuSync configures the managed Discord category and Task Type channels, then saves the Production routing state.', notes: ['Review create, reuse, and conflict states before execution.', 'Connected Productions are not offered as new connections. Use Production repair when resources are stale.', 'A blocked review normally means a name conflict, missing permission, or ownership check.'], check: 'Check prerequisites, Discord server access, and the Channel Plan conflicts in that order. Review rollback information before retrying a failed execution.' },
    channels: { purpose: 'This section maps each Kitsu Task Type to its Discord Channel.', when: 'Use it to change a destination, reorder channels, add a route, or stop notifications for one Task Type.', steps: ['Open Notifications in Production details.', 'Choose Edit only when you need to change routing.', 'Stage the route changes and select Apply changes.', 'Use Remove from notifications to remove only a route.'], after: 'A successful save updates KitsuSync routing and the order of managed Discord channels.', notes: ['Removing a route does not delete the Task Type or Discord Channel.', 'Deleting a Discord Channel is a separate dangerous action requiring ownership checks and exact-name confirmation.'], check: 'If the result does not change, check Bot permissions, the linked server and category, and channel existence in Troubleshooting.' },
    assignments: { purpose: 'This section connects human Kitsu users to Discord IDs and assigns Production roles.', when: 'Use it when a notification should mention a person or a Task Type needs a Reviewer / Checker.', steps: ['Create the human Kitsu-to-Discord mapping in User Linking.', 'Associate that linked user with the Production in Production Users.', 'Assign Reviewer / Checker by selecting the user and Task Type.'], after: 'The notification renderer can use the saved Discord ID for a safe user mention, and the role applies only to that Production.', notes: ['Kitsu Bot identities are excluded.', 'Without a Discord ID, the person is not mentioned but the notification can still be delivered.', 'Global User Linking and Production association are separate settings.'], check: 'If the user is missing, create the global link first and then associate the user with the Production.' },
    notifications: { purpose: 'This section explains which Kitsu changes can create or update Discord notifications.', when: 'Use it to understand delivery conditions and duplicate prevention.', steps: ['KitsuSync reads Kitsu changes.', 'It evaluates supported status changes, comment updates, and enabled assignment notifications.', 'It resolves a Discord Channel from the Production and Task Type route.', 'When all required data is valid, it sends a new message or updates the existing one.'], after: 'Successful delivery is recorded in Audit Log and delivery state. Repeated unchanged observations are not sent again.', notes: ['Missing routes, channels, webhooks, or ownership checks stop delivery safely.', 'Notification language is set per Production.', 'Unsupported or unknown event shapes are not sent.'], check: 'Check Connections, System Status, Production Notification Routing, and Audit Log in that order.' },
    preview: { purpose: 'There is no separate notification preview screen. This section explains how to check destinations and delivery results.', when: 'Use it to review routing and confirm what happened after a notification.', steps: ['Open Notifications in Production details to check the Task Type and Discord Channel mapping.', 'Open Audit Log to review recorded notification results.', 'When a Kitsu link appears in a record, confirm that it opens the intended item.'], after: 'Notifications and Audit Log are read-only views; opening them never sends a Discord message.', notes: ['If a destination is missing or stale, review Notification Routing and Troubleshooting.', 'Links appear only when Kitsu provides stable IDs and a configured host.'], check: 'If the destination is unclear, review Notification Routing first. If delivery is unclear, review Audit Log.' },
    audit: { purpose: 'This section explains how to confirm readiness and investigate missing notifications.', when: 'Use it after setup, after recovery, or whenever delivery is unexpected.', steps: ['Check Kitsu and Discord Bot status in System Status.', 'Check Production routing and notification readiness.', 'Review the Production Troubleshooting groups.', 'Inspect Audit Log for the Task Type and delivery outcome.'], after: 'Diagnostics show the failing area and, where available, the next action.', notes: ['Unknown external state is not reported as success.', 'Stale channels and webhooks are not reused by name alone.'], check: 'Check Kitsu, Discord Bot, server, category, route, webhook, and audit result in that order.' },
    data: { purpose: 'This section explains the local information required for backup and recovery.', when: 'Use it before changing hosts, creating backups, or restoring a service.', steps: ['Identify the SQLite data, runtime secret key, and operational configuration.', 'Back up all three from the same point in time.', 'After restore, verify the admin UI, /health, and Production routing.'], after: 'When the data and secret key match, saved connections and routing can be read again. Discord resources are still checked separately.', notes: ['Restoring only one part can make encrypted connection data unusable.', 'Never display backup files or secret keys in the UI or logs.'], check: 'Confirm the backup generation first, then /health, then Discord resource and Production diagnostics.' },
    security: { purpose: 'This section gives direct guidance for protecting KitsuSync access and integration credentials.', when: 'Review it during setup, permission changes, backups, and recovery.', steps: ['Limit administrator access to the people who need it.', 'Use HTTPS and a trusted reverse proxy when the service is exposed beyond the local host.', 'Treat Kitsu and Discord tokens, Webhook URLs, and the runtime secret key as secrets.', 'Grant the Discord Bot only the Guild access and permissions required for its work.'], after: 'KitsuSync stops operations when ownership or permissions cannot be verified and shows a safe diagnostic instead.', notes: ['Never place tokens or Webhook URLs in logs, screenshots, chat, or documentation.', 'Rotate exposed tokens or webhooks promptly.', 'Keep backups protected and test recovery without exposing secrets.'], check: 'Review admin access, HTTPS, Bot permissions, target Guild, resource ownership, and possible secret exposure in that order.' }
  }
};

function readInitialLang() {
  try {
    const qs = new URLSearchParams(window.location.search);
    const queryLang = qs.get('lang');
    if (queryLang === 'en' || queryLang === 'ja') {
      localStorage.setItem('admin_lang', queryLang);
      return queryLang;
    }
    const stored = localStorage.getItem('admin_lang');
    if (stored === 'en' || stored === 'ja') {
      return stored;
    }
  } catch (error) {}
  return 'ja';
}

function updateLangInUrl(lang) {
  const qs = new URLSearchParams(window.location.search);
  qs.set('lang', lang);
  const next = `${window.location.pathname}?${qs.toString()}${window.location.hash}`;
  window.history.replaceState({}, '', next);
}

function sectionIdFromLocation() {
  const requested = window.location.hash.replace(/^#/, '') || new URLSearchParams(window.location.search).get('section');
  return sections.ja.some((section) => section.id === requested) ? requested : sections.ja[0].id;
}

function OverviewSummary({ lang }) {
  const rows = lang === 'ja'
    ? [['Kitsu', 'タスク、ステータス、コメントを取得'], ['KitsuSync', '差分を判定し、プロダクションのルートを解決'], ['Discord', '対象チャンネルへ通知を送信・更新'], ['管理画面', '設定、割り当て、監査、診断を確認']]
    : [['Kitsu', 'Reads tasks, statuses, and comments'], ['KitsuSync', 'Detects changes and resolves Production routes'], ['Discord', 'Sends and updates notifications in the target channel'], ['Admin UI', 'Manages settings, assignments, audit, and diagnostics']];
  return (
    <section className="overview-summary" aria-labelledby="overview-workflow-title">
      <h2 id="overview-workflow-title">{lang === 'ja' ? 'ワークフロー概要' : 'Workflow overview'}</h2>
      <dl>{rows.map(([term, detail]) => <div key={term}><dt>{term}</dt><dd>{detail}</dd></div>)}</dl>
    </section>
  );
}

function markDocsReady() {
  document.body.classList.add('docs-ready');
  const fallback = document.getElementById('docs-fallback');
  if (fallback) {
    fallback.remove();
  }
}

function localizeJapaneseGuide(value, lang) {
  if (lang !== 'ja') return value;
  if (typeof value === 'string') return value.replace(/\bProductions?\b/g, 'プロダクション');
  if (Array.isArray(value)) return value.map((item) => localizeJapaneseGuide(item, lang));
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, localizeJapaneseGuide(item, lang)]));
  return value;
}

function Section({ section, lang }) {
  const guide = localizeJapaneseGuide(guidance[lang][section.id], lang);
  return (
    <section className="doc" id={section.id}>
      <div className="kicker">{section.kicker}</div>
      <h1 className="docnum"><span className="n">{section.num}</span>{section.title}</h1>
      <h2 className="docsub">{section.subtitle}</h2>
      {section.intro ? <p className="doc-intro">{section.intro}</p> : null}
      {guide ? <div className="doc-guidance">
        {Object.entries({
          purpose: lang === 'ja' ? 'このセクションの目的' : 'What this section is for',
          when: lang === 'ja' ? '使う場面' : 'When you need it',
          steps: lang === 'ja' ? '画面で行うこと' : 'What you do in the UI',
          after: lang === 'ja' ? '保存後の動作' : 'What happens after saving',
          notes: lang === 'ja' ? '注意点' : 'Important notes',
          check: lang === 'ja' ? '動かない場合の確認' : 'If it does not work'
        }).map(([key, title]) => <div className="doc-guidance-block" key={key}><h3>{title}</h3>{Array.isArray(guide[key]) ? <ul>{guide[key].map((item) => <li key={item}>{item}</li>)}</ul> : <p>{guide[key]}</p>}</div>)}
      </div> : null}
      {section.table ? (
        <div className="wf box">
          <table className="adm">
            <thead>
              <tr>{section.table.headers.map((header) => <th key={header}>{header}</th>)}</tr>
            </thead>
            <tbody>
              {section.table.rows.map((row) => (
                <tr key={row[0]}>{row.map((cell, index) => <td key={`${row[0]}-${index}`}>{cell}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  );
}

function Site() {
  const [lang, setLang] = useState(readInitialLang);
  const items = sections[lang];
  const [active, setActive] = useState(sectionIdFromLocation);

  useEffect(() => {
    localStorage.setItem('admin_lang', lang);
    updateLangInUrl(lang);
  }, [lang]);

  useEffect(() => {
    const syncSection = () => setActive(sectionIdFromLocation());
    window.addEventListener('hashchange', syncSection);
    window.addEventListener('popstate', syncSection);
    return () => {
      window.removeEventListener('hashchange', syncSection);
      window.removeEventListener('popstate', syncSection);
    };
  }, []);

  const toggle = () => setLang((current) => (current === 'ja' ? 'en' : 'ja'));
  const selectSection = (id) => {
    if (!sections.ja.some((section) => section.id === id)) return;
    const next = `${window.location.pathname}${window.location.search}#${id}`;
    window.history.pushState({}, '', next);
    setActive(id);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const navigation = (className = '') => (
    <div className={className}>
      <div className="navtitle">{lang === 'ja' ? '目次' : 'Contents'}</div>
      {items.map((item) => (
        <a
          key={item.id}
          href={`#${item.id}`}
          className={`navlink ${active === item.id ? 'active' : ''}`}
          aria-current={active === item.id ? 'page' : undefined}
          onClick={(event) => { event.preventDefault(); selectSection(item.id); }}
        >
          <span className="num">{item.num}</span>{item.label}
        </a>
      ))}
    </div>
  );

  return (
    <div className="site">
      <aside className="nav">
        <div className="brand">
          KitsuSync
          <small>{lang === 'ja' ? '公式ドキュメント' : 'Official Documentation'}</small>
        </div>
        <button className="lang-toggle" data-lang={lang} aria-label={lang === 'ja' ? 'Switch to English' : '日本語に切り替え'} onClick={toggle}>
          <span className="lang-thumb" aria-hidden="true" />
          <span className={`lang-option ${lang === 'ja' ? 'active' : ''}`}>JP</span>
          <span className={`lang-option ${lang === 'en' ? 'active' : ''}`}>EN</span>
        </button>
        {navigation('navgroup')}
        <div className="meta">v0.4.1<br />Product guide</div>
      </aside>
      <details className="mobile-nav">
        <summary>{lang === 'ja' ? '目次' : 'Contents'} <span aria-hidden="true">⌄</span></summary>
        {navigation('mobile-nav-list')}
      </details>
      <main className="body">
        <div className="hero">
          <div className="container">
            <h1>KitsuSync Documentation</h1>
            <p className="lead">{lang === 'ja' ? 'KitsuとDiscordを接続し、プロダクションごとの通知、ユーザー設定、監査と診断を確認するための公式ドキュメントです。' : 'Official guidance for connecting Kitsu and Discord, managing Production notifications, assigning users, and checking audit and diagnostic information.'}</p>
            <div className="toc">{items.map((item) => <a key={item.id} onClick={() => setActive(item.id)}>{item.label}</a>)}</div>
          </div>
        </div>
        <div className="container">
          {active === 'overview' ? <OverviewSummary lang={lang} /> : null}
          {items.filter((item) => item.id === active).map((item) => <Section key={item.id} section={item} lang={lang} />)}
        </div>
        <footer className="foot">
          <div className="left">KitsuSync Documentation v0.4.1</div>
          <div>{lang === 'ja' ? 'KitsuSync公式ドキュメント' : 'Official KitsuSync documentation'}</div>
        </footer>
      </main>
    </div>
  );
}

const rootElement = document.getElementById('root');
if (rootElement) {
  const root = ReactDOM.createRoot(rootElement);
  root.render(<Site />);
  markDocsReady();
}
