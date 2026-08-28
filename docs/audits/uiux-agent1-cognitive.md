# Agent 1 — Cognitive / HCI audit (v0.4.4)

対象: `C:\Users\mynti\Documents\KitsuSync-clean` の現行ワークツリー。監査日: 2026-08-28（JST）。対象は認知負荷、状態理解、判断・回復手順。UI/CSS、アクセシビリティ、レスポンシブ、用語、破壊操作の安全性、外部サーバー挙動は評価しない。

## 結論

主要な認知上の問題は、Connections画面でサービス別状態を判断できないこと（COG-001）、初回接続ウィザードの単一ステップへの判断集約（COG-002）、Dashboardの同一状態の多重提示（COG-003）、System Status内の診断とテスト送信の目的混在（COG-004）の4点。初回接続の完遂には複数の判断を保持する必要があり、通常運用では状態確認の入口が分散する。

## 採点基準

0 = 目的達成不能または判断材料が欠落、1 = 高い認知負荷・誤判断リスク、2 = 達成可能だが追加解釈・記憶が必要、3 = 目的と次の行動が一貫して明確。採点は画面の認知/HCIだけであり、実行成功率や外観の採点ではない。

## 主要画面スコア

| 画面 / route | score (0–3) | confidence | 根拠の要約 |
| --- | ---: | --- | --- |
| Dashboard `/bot/admin` | 2 | high | 概要4指標、対応キュー、通知システム、クイック操作、管理メニューを同一画面に提示。次の行動はあるが、状態の重複解釈が必要。 |
| Production list `/bot/admin/projects` | 2 | medium | DashboardからProduction一覧、Productionごとの問題からNotifications/Usersへ遷移する導線はソースで確認。実データを使った画面遷移は未確認。 |
| Production detail `/bot/admin/projects?project=<id>` | 2 | medium | Overview、Notifications、Users、Troubleshootingの複数タブ/セクションと、通知ルーティングの閲覧→明示Editを確認。目的別には分かれるが、状態判断は複数箇所にまたがる。 |
| Connections `/bot/admin/bot` | 1 | high | 現行rendererはBot state、必要権限、接続済みサーバーの単一status-list。Kitsu接続とDiscord Bot接続を独立に判断できない。 |
| User Linking `/bot/admin/users` | 2 | low | Current IA routeと人間のKitsu-to-Discord linkingという目的はルート登録・Dashboard導線で確認。実レンダーと空状態は今回runtime未起動のため未確認。 |
| System Status `/bot/admin/health` | 2 | high | Overall、API応答、4つの処理状態、最近の問題、5秒自動更新が同一画面に組み合わさる。状態は見えるが、診断と確認操作の境界に追加解釈が必要。 |
| New Production setup `/bot/setup` | 1 | high | 7段階の進捗に加え、Step 4へ複数の設定判断を集中。初回管理者が「何を決める段階か」を保持し続ける必要がある。 |
| Audit Log `/bot/admin/audit` | 2 | low | ルートとDashboardの「操作履歴と通知イベント」という目的は確認。ログの行動回復性・絞り込みは今回未確認。 |

## Journey table

| ID | journey / user type | route/screen | completion | exact evidence | friction / unnecessary step | user impact | recommended resolution / action | severity | confidence | external research |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| COG-001 | 接続状態を切り分ける / 通常管理者 | `/bot/admin/bot` | partial | `src/setup/ia_views.go:1323-1331` はBot state等の単一status-list | Kitsu/Discord別状態がなく、別画面へ戻って再解釈が必要 | 再設定対象の誤認・遅延 | サービス別状態と直接編集入口を分離提示 / redesign | P1 | high | no |
| COG-002 | ProductionをDiscordへ初回接続 / 初回通常管理者 | `/bot/setup` Step 4 | possible with high load | `src/setup/ia_views.go:2702-2756` にカテゴリ、Task Type包含/除外、順序、チャンネル名、通知言語 | 異なる目的の判断を同一Stepで保持し、Step 5で全計画を再把握 | 設定意図の取り違え、失敗原因の特定困難 | 必須計画と任意調整を分離し、レビューで差分表示 / redesign | P1 | high | no |
| COG-003 | 最初に直す対象を決める / 通常管理者 | `/bot/admin` | possible with interpretation | `src/setup/ia_views.go:318-323`, `2864-2895` に概要・キュー・通知状態・管理カード | 同じ状態/カウントが複数単位に再提示され、一次CTA選択が必要 | 初動遅延・見落とし | 一次目的を次の1操作に固定し重複値を除去または単一CTAへ集約 / remove | P2 | high | no |
| COG-004 | 原因確認後に到達確認する / 通常管理者 | `/bot/admin/health` | possible with interpretation | `src/setup/ia_views.go:1338-1358`, `src/setup/admin.go:2321-2335`, `src/setup/diagnostics.go:894-1020` | 読み取り診断と外部テスト送信が同一認知領域 | 不要な試験送信・結果の誤認 | 診断と到達確認を別タスクとして分離 / relocate | P2 | high | no |

## Findings

### COG-001 — ConnectionsでKitsu/Discordの独立状態を判断できない

- journey: 初回設定後に、どの接続が未設定/要確認かを特定して次の設定画面へ進む
- user type: 通常の管理者
- route/screen: `/bot/admin/bot`（Connections）
- severity: P1
- confidence: high
- evidence: `src/setup/ia_views.go:1323-1331` の `renderIABot` は、単一の `section-card` 内に `Bot state`、`Required permissions`、`Joined servers` のstatus rowを生成する。接続設定CTAも `/bot/admin/bot?edit=1` の単一入口である。対してCurrent IA仕様は `docs/CURRENT-IA-UI-SPEC.md` のConnections節で、Kitsu connectionとDiscord Bot connectionを独立カード/独立バッジとして要求している。
- inference: 管理者は「Kitsuは正常だがDiscordだけ未設定」のような部分状態をこの画面だけでは分類できず、次の作業対象を記憶または別画面で再確認する必要がある。これは設定順序・再試行対象の判断を阻害する。
- friction/blocker: サービスごとの状態と、それぞれに対応する行動の対応付けが欠落。
- unnecessary step: 状態を知るために別のDashboard/Setup表示へ戻る、または単一のBot設定画面を開いて再解釈する手順。
- user impact: 接続不良の切り分けが遅れ、不要な再設定を誘発する。
- recommended resolution: KitsuとDiscordを同一画面内の独立した状態単位として提示し、各状態から対象サービスの編集へ直接進める。
- action class: redesign
- external research supports recommendation: no; recommendation is derived from current route/source and the repository's Current IA contract.

### COG-002 — 初回ウィザードStep 4に判断が集中する

- journey: 1つのKitsu ProductionをDiscordへ接続し、作成内容を確認して実行する
- user type: 初回の通常管理者
- route/screen: `/bot/setup`、wizard Step 4（Task Type channel plan）
- severity: P1
- confidence: high
- evidence: `src/setup/ia_views.go:2438-2488` は7段階（前提条件、Production、Server、Plan、Review、Execute、Complete）をレンダーする。Step 4の `renderWizardPlanPolished`（`src/setup/ia_views.go:2702-2756`）には、Discordカテゴリ選択、Task Typeごとのチャンネル名入力、Task Typeの除外/追加、ドラッグまたは上下移動による順序変更、Discord通知言語選択が同じフォームに入る。次にStep 5のレビュー（`src/setup/ia_views.go:2759-2792`）で同じ計画を再確認する。
- inference: 「どのProduction/Serverを選ぶか」「どのTask Typeを通知対象にするか」「Discord上の名前/順序をどうするか」「通知文の言語をどうするか」という異なる目的の判断を一画面で保持するため、初回管理者は必須判断と任意調整の区別を自力で行う必要がある。レビューで同じ計画を再確認すること自体は有益だが、Step 4の編集内容が多く、意思決定の単位が大きい。
- friction/blocker: 設定粒度の違う選択・編集・言語設定が同一Stepに集約。
- unnecessary step: 設定を変更するたび、計画全体とレビューの差分を再把握すること。
- user impact: Task Type除外、チャンネル名変更、通知言語の意図を取り違えやすい。失敗時にどの判断が原因かも特定しにくい。
- recommended resolution: Step 4内で必須接続計画と任意カスタマイズを明確に分離し、レビューでは変更点と最終実行内容だけを比較可能にする。
- action class: redesign
- external research supports recommendation: no; recommendation is based on the observed task decomposition and current source evidence.

### COG-003 — Dashboardが同じ状態を複数の視覚単位で再提示する

- journey: Dashboardを開いて、最初に何を直すかを判断する
- user type: 通常の管理者
- route/screen: `/bot/admin`
- severity: P2
- confidence: high
- evidence: `src/setup/ia_views.go:318-323` は概要（Connected Productions、Needs attention、直近24時間の通知失敗、System status）、対応が必要なProductionキュー、Activity、Notification system、Quick actionsをレンダーする。続く `src/setup/ia_views.go:2864-2895` はConnections、Productions、User Linking、System Status、Audit Logの管理カードをさらに表示し、Productionの `attentionCount` と接続数も再表示する。
- inference: 管理者は「数字」「Production単位の問題」「通知状態」「管理カード」のどれを一次情報として行動すべきかを選ぶ必要があり、状態の同じ意味（対応が必要、接続済み、通知可否）が複数表現に分散する。表示量そのものが問題なのではなく、初動判断の優先順位が画面構造から一意に定まらない。
- friction/blocker: サマリーと下位管理カードに重複する状態・カウントがある。
- unnecessary step: 複数カードを突き合わせて、同じ問題の詳細入口を探すこと。
- user impact: 初動の遅延、誤った管理画面への遷移、状態の見落とし。
- recommended resolution: Dashboardの一次目的を「次の1操作」に固定し、詳細カードは一次判断に不要な重複値を減らすか、同じProduction/問題への一貫した単一CTAへ集約する。
- action class: remove
- external research supports recommendation: no; recommendation is derived from the current renderer's information duplication.

### COG-004 — System Status内で診断とテスト送信の目的が混在する

- journey: 通知停止の原因を確認し、必要なら到達確認を行う
- user type: 通常の管理者
- route/screen: `/bot/admin/health`
- severity: P2
- confidence: high
- evidence: `src/setup/ia_views.go:1338-1358` はSystem StatusにOverall、API応答、KitsuSync処理状態、最近のシステム問題を配置する。`src/setup/admin.go:2056-2064` は通常health rendererを使用し、同ファイルのhealth構成（`src/setup/admin.go:2321-2335`）では診断セクションに `renderDiagnosticsPanel` を追加する。`src/setup/diagnostics.go:894-1020` では同じ診断パネル内にProduction選択、Test destination選択、`Send test notification` 操作、Discord到達確認の説明を置く。
- inference: 読み取り専用の「原因を知る」操作と、外部Discordへ試験通知して「到達を確かめる」操作は目的と結果が異なる。両者がSystem Statusの同一認知領域にあるため、管理者は診断結果の確認と外部検証の実行を同じ種類の次の手順として解釈しやすい。
- friction/blocker: 状態観測、原因詳細、外部送信、外部側確認の手順境界が明確な段階として分離されていない。
- unnecessary step: 原因確認中に、テスト送信の対象Productionと送信先を選び直すための追加判断。
- user impact: 問題切り分けの途中で不要な試験送信を行う、または送信後にどの状態が変わったかを誤認する。
- recommended resolution: System Statusの読み取り診断と、到達確認の実行フローを明示的に別タスクとして分け、後者には実行前の目的・対象・期待結果をまとめて提示する。
- action class: relocate
- external research supports recommendation: no; recommendation is based on the current route composition and observed control grouping.

## Positive evidence / retain

- Dashboardの対応キューは、Productionごとの理由と次の操作を同じ行に置く実装になっている（`src/setup/ia_views.go:247-260`）。これは原因から対象画面へ移る際の記憶負荷を下げるため、retain。
- ウィザードは前提条件未達時にStep 1へ戻し、前提条件、Production、Server、Plan、Review、Execute、Completeを進捗表示する（`src/setup/ia_views.go:2390-2416`, `2438-2488`）。段階性そのものは有効であり、COG-002はStep 4の判断量に限定した指摘。
- 通知ルーティングは通常表示を読み取り専用にし、明示的なEdit入口を設けている（`src/setup/production_routing.go:344-357`）。閲覧と変更のモード分離はretain。

## 未確認範囲と検証記録

- 現行runtimeはアプリ本体が起動しておらず、Dockerでは `kitsu-test-mock` のみ稼働していた。`screenshots/` にも現行画面画像はなく、ブラウザでの実画面確認は未実施。
- `go test ./src/setup/... -count=1 -timeout=120s` は実行したが、環境の `CGO_ENABLED=0` により `go-sqlite3 requires cgo to work` で失敗した。したがってrendererのDB依存テスト結果は採用せず、ソース行を主証拠とした。
- 外部調査は実施していない。推奨は現行コード、Current IA仕様、v0.4.4リリースノートの突合から導出した。
- 本監査で作成・更新したファイルは本レポートのみ。UI/CSS、renderer/routes、product behavior、外部サーバー、Gitは変更していない。

## 参照ソース

- `src/setup/ia_views.go`
- `src/setup/admin.go`
- `src/setup/production_routing.go`
- `src/setup/diagnostics.go`
- `src/main.go`
- `docs/CURRENT-IA-UI-SPEC.md`
- `RELEASE_NOTES_v0.4.4.md`
