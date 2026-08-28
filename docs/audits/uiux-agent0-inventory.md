# KitsuSync v0.4.4 — Agent 0 情報インベントリ

対象: `C:\Users\mynti\Documents\KitsuSync-clean` の現行 checkout (`codex/v0.4.4-notification-card`)
監査種別: 設計監査のみ。画面・route・オブジェクト所有範囲・状態表示の事実インベントリ。
作成日: 2026-08-28 (Asia/Tokyo)

## 調査方法と範囲

- 現行 source: `src/main.go`, `src/setup/ia_views.go`, `src/setup/admin.go`, `src/setup/current_routing.go`, `src/setup/production_routing.go`, `src/setup/diagnostics.go`, `src/setup/middleware.go`, `src/setup/root_route.go`, `src/setup/setupapi.go`, `src/setup/ui.go`。
- 補助 source: `docs/CURRENT-IA-UI-SPEC.md`, `docs/CURRENT-IA-UI-ACCEPTANCE.md`, `src/setup/*_test.go`。
- ローカル runtime は `127.0.0.1:8090` への未認証 GET のみ実施。外部サーバー、認証、POST、実データ変更、スクリーンショット撮影は実施していない。
- このレポートで「Evidence」は source/runtime に直接現れる事実、「Inference」はその事実からの限定的な IA 上の読み取りを示す。severity は設計監査上の観測優先度であり、実装修正要求ではない。
- 画面ラベルは日本語/英語の両方が source に存在する場合、代表して `JP / EN` と記載する。

## 現行 sitemap / route map

| Object scope | Route | Current purpose and entry |
|---|---|---|
| Global auth | `/bot/login` (also `/login`) | Kitsu manager/admin credentialsでログイン。Kitsu base URL は自動検出できない場合のみ表示。 |
| Global entry | `/bot/` | 未認証なら `/bot/login?next=...`、認証済みかつ runtime ready なら `/bot/admin`、それ以外は `/bot/setup` へ redirect。 |
| Global setup | `/bot/setup` (also `/setup`) | New Production Connection wizard。Kitsu/Discord前提、Production、Discord server、channel plan、review、execute、complete。 |
| Global dashboard | `/bot/admin` | Dashboard、状態サマリー、attention queue、管理メニュー。 |
| Global connections | `/bot/admin/bot` | Kitsu と Discord Bot の接続概要。`?edit=1` が編集フォーム。 |
| Production collection | `/bot/admin/projects` | available/live Kitsu Production と local connection の統合一覧。`?project=<id>` で選択中 Production detail。 |
| Production-scoped | `/bot/admin/projects?project=<id>&tab=...` | `overview`, `notifications`, `users`, `storage-settings`, `activity`, `troubleshooting`, `advanced`, `danger-zone`。 |
| Global human linking | `/bot/admin/users` | Kitsu human user と Discord user ID の global link。選択した Discord server の member listを利用。 |
| Compatibility / legacy | `/bot/admin/checkers` | compatibility URL。現行 UI の主入口ではなく、旧 checker handler を登録。 |
| Global diagnostics | `/bot/admin/health` | Overall system health、API response status、KitsuSync operational status、issues/diagnostic details。 |
| Global audit | `/bot/admin/audit` | 最大200件の audit log。Dashboard の recent activity とは別ページ。 |
| Compatibility / legacy | `/bot/admin/production-routing` | GET は compatibility handler で Current Production の notifications tabへ redirect。POST は routing handler。 |
| Compatibility / legacy | `/bot/admin/workflow-diagnosis` | GET は compatibility handler、POST は workflow diagnosis。 |
| Compatibility alias | `/bot/admin/diagnostics` | `/bot/admin/health` へ redirect。 |
| Global/diagnostic API | `/bot/api/setup/status`, `/observability`, `/projects`, `/preview-project`, `/test-kitsu`, `/test-discord`, `/test-notification`, `/mapping` | 認証必須。`apply-project`, mapping users/checkers は mutation API。通常の画面入口ではない。root prefix (`/api/setup/...`) も登録。 |
| Documentation | `/bot/docs/` | `docs.html` の公式ドキュメント入口。 |

Evidence: `src/main.go:859-940` の route 登録、`src/setup/root_route.go:10-34` の `/bot/` 分岐、`docs/CURRENT-IA-UI-SPEC.md:9-19` の canonical route table。

## 画面別事実インベントリ

### F-001 — Login

- Finding ID: `IA0-F-001`
- Screen/route: `/bot/login` (`/login` alias)
- Scope: global/authentication
- Evidence: `src/setup/middleware.go:349-421` は POST で Kitsu login API を検証し、manager/admin 以外を拒否し、成功時は session cookie を発行して `next` に redirect。GET は login page を描画。`src/setup/middleware.go:503-561` は `KitsuベースURL / Kitsu base URL` を必要時だけ表示し、`メールアドレス / Email`、`パスワード / Password`、`ログイン / Login` を出力する。runtime GET は 200、JP の h1 `KitsuSync`、3つの主要フォーム要素を確認。
- Inference: login は管理画面/Setup の共通前段で、通常の画面内に置かれたログインではない。
- Severity: P3 (inventory)
- Confidence: high (source + local runtime)

### IA0-F-002 — Setup Wizard

- Screen/route: `/bot/setup`
- Scope: global setup; selected Production/Guild は session/query state
- Evidence: `src/setup/ia_views.go:2438-2489` は 7 step (`Prerequisites`, `Production`, `Server`, `Plan`, `Review`, `Execute`, `Complete`) を描画。前提未達時は step 1 に戻し、connected Production は通常選択不可、`repair=1` のとき既存 Production の repair を許可する。`src/setup/ia_views.go:2504-2514` は Kitsu、Discord Bot、Production connections、Notifications を前提状態として表示。`2546-2580` は Production 選択と invalid/already-connected error。`2609-2756` は server/channel plan、Task Type追加・除外・並べ替え、create/reuse/conflict、notification language を扱う。`2759-2793` は Review で Production/server/category/channel order と明示的 confirm checkbox を表示。`2845-2857` は Complete から `/bot/admin/projects?project=<id>` に戻す。
- Inference: Setup は「新しい Production 接続」の staged workflow で、既存 Production の通常管理画面とは別の global entry だが、repair 時だけ selected Production context を再利用する。
- Severity: P3 (inventory)
- Confidence: high

### IA0-F-003 — Dashboard / management hierarchy

- Screen/route: `/bot/admin`
- Scope: global operational summary
- Evidence: `src/setup/ia_views.go:243-331` は summary (Connected Productions, Needs attention, notification failures, System status)、attention queue、activity を生成。`2864-2896` の management cards は Connections `/bot/admin/bot`、Productions `/bot/admin/projects`、User Linking `/bot/admin/users`、System Status `/bot/admin/health`、Audit Log `/bot/admin/audit` と New Connection `/bot/setup` を入口にする。`src/setup/ia_views.go:23-34` の primary nav は Productions, User Linking, Connections, System Status, Audit Log。
- Inference: Production は Dashboard の管理メニューの一項目であり、Productionを選択した後に機能タブが現れる primary management object と読める。
- Severity: P3 (inventory)
- Confidence: high

### IA0-F-004 — Productions list and selected Production detail

- Screen/route: `/bot/admin/projects`; selected state `?project=<id>`
- Scope: Production collection / one Production
- Evidence: `src/setup/ia_views.go:476-500` は `availableProjects(db)` を live Kitsu と local project state から作り、各 row に Production name、Connected/Disconnected、notification hint、Open Production を表示。live-only record は `ReadOnlyPreview` として `Disconnected`、通常接続数から除外 (`49-89`, `2907-2921`)。`549-577` は selected Production header、selected status、tablist を描画。`562` の tabs は Overview, Notifications, Users, Storage settings, Activity, Troubleshooting, Advanced, Danger zone。`638-708` は tab dispatch と Overview の Production state, Discord connection, Notification routing, Users/participants, Current issues。未接続 preview は `579-583` で Configure connection と Back を表示。
- Inference: `/bot/admin/projects` は collection と detail の二つの表示状態を一つの route family にまとめ、`project` query が selected Production context を確定する。
- Severity: P3 (inventory)
- Confidence: high

### IA0-F-005 — Notifications, routing, mapping, preview, Test Notification

- Screen/route: selected Production `tab=notifications`; compatibility `/bot/admin/production-routing`; diagnostic Test Notification API
- Scope: Production-scoped notification routing; Test Notification is diagnostic action
- Evidence: `src/setup/ia_views.go:1057-1071` は normal Notifications panel を read-only routing summary として描画し、`edit_routing=1` で editor に切り替える。`src/setup/current_routing.go:278-295` は Kitsu Task Type → Discord Channel の one-to-one mapping、Edit、Add Task Type、Apply changes、Cancel を表示し、route removal は Kitsu/Discord resource deletion ではない旨を表示。`src/main.go:915-921` は old production-routing GET を compatibility handler、POST を routing handler に分ける。`src/setup/diagnostics.go:1003-1031` は Test notification の selected channel への synthetic Discord message 送信と成功/失敗表示を定義し、`src/main.go:881-888` は `/bot/api/setup/test-notification` を session-required mutation route として登録。`src/setup/setupapi.go:1097-1319` は `/api/setup/mapping`、`mapping/users`、`mapping/checkers` の project-scoped state/save API を定義。
- Inference: normal notification configuration は Production detail 内で所有され、Test Notification は routing configuration 自体ではなく diagnostic verification action。mapping API は UI route ではなく同じ機能領域の backend contract。
- Severity: P3 (inventory)
- Confidence: high

### IA0-F-006 — Production Users / User Linking / Reviewer-Checker

- Screen/route: global `/bot/admin/users`; selected Production `tab=users` / `tab=user-settings`; compatibility `/bot/admin/checkers`
- Scope: global human identity link vs Production-scoped association/role
- Evidence: `src/setup/ia_views.go:2214-2315` は global User Linking で Discord server selector、Kitsu users table、Discord user selector、Linked/Not set/Needs verification、Save/Unlink を描画。Bot account は filtering path (`filterAssignablePersons`, `filterAssignableUsers`) から除外される。`2132-2196` は legacy/global mapping renderer と read-only live Kitsu data fallback を保持。`757-837` は selected Production の local associated users、Add a user、Assigned、Reviewer / Checker と remove actions を描画し、eligible candidates は global linked human user かつ未関連付け。`840-921` には別の scalable renderer があり、source 上は search/status/add-users query を扱うが、`src/setup/ia_views.go:757-759` の normal connected path は simple renderer を選ぶため、現行 normal path ではその scalable renderer は選択されない。`src/main.go:895-902` は `/admin/users` と `/admin/checkers` を登録。`src/setup/admin.go:2749` は checker handler を定義。
- Inference: identity link (global) と Production association/Reviewer-Checker (local) は ownership を分離する設計。`/bot/admin/checkers` は current primary nav に含まれず、compatibility/legacy entry として競合候補。
- Severity: P3 (inventory; competing legacy entry noted)
- Confidence: high for source facts; medium for the compatibility interpretation (handler name and route comments support it)

### IA0-F-007 — Connections read/edit

- Screen/route: `/bot/admin/bot`; edit `?edit=1`
- Scope: global Kitsu + global Discord Bot connection
- Evidence: `src/setup/ia_views.go:1323-1331` の normal IA summary は Bot state、required permissions、joined servers と `?edit=1` への Connect or reconnect。`src/setup/admin.go:2903-2929` は Kitsu と Discord Bot を separate named sections/cards として read view に描画し、host/account/token health/status と Edit/New Connection を出力。`src/main.go:904-910` は BotHandler を runtime reconnect と validation mutation guard の下で route。`src/setup/middleware.go:103-105` は edit next に限定した一時的 BotEdit 権限状態を session に保持。
- Inference: Connections は Dashboard/primary nav から到達する global settings、Production detail の配下ではない。
- Severity: P3 (inventory)
- Confidence: high

### IA0-F-008 — System Status / API response / operational diagnostics

- Screen/route: `/bot/admin/health`
- Scope: global diagnostic/operational status
- Evidence: `src/setup/ia_views.go:1338-1358` は Event monitoring, Notification processing, Internal data, Connection/routing integrity を pipeline health items として生成。`1361-1368` は Overall system health、API response status、KitsuSync operational status の順で組み立てる。`1426-1442` は observation 無しを `Not checked`、有りを current response time と local time として表示。`src/main.go:932-936` は health route と `/admin/diagnostics` alias redirect を登録。
- Inference: System Status は Production context を必要としない global read model で、Production detail の Troubleshooting とは別の粒度。
- Severity: P3 (inventory)
- Confidence: high

### IA0-F-009 — Audit Log and activity surfaces

- Screen/route: `/bot/admin/audit`; Dashboard activity; selected Production `tab=activity`
- Scope: global audit vs Production-filtered activity
- Evidence: `src/setup/ia_views.go:1934-1957` は最大200件を日時降順で表示し、日時、Production、Action、未記録値、Success/Failed を table 化。empty は `No audit log entries.`。`243-287` は Dashboard で最大5件の recent activity と直近24時間 failure count を使う。`1140-1156` は selected Production の activity を `log.ProjectName == p.Name` で絞り、empty は `No activity yet.`。`src/main.go:932-933` は global audit route を登録。
- Inference: 同じ audit data に global list、Dashboard preview、Production-local activity の三つの presentation entry がある。機能重複ではなく scope の違いだが、利用者は三つの履歴表示を認識し得る。
- Severity: P3 (inventory)
- Confidence: high

### IA0-F-010 — Empty, error, loading, and blocked states

- Screen/route: Login, Setup, Productions, Production detail, User Linking, System Status, routing/diagnostics
- Scope: cross-screen state inventory
- Evidence: `src/setup/ia_views.go:494-496` は no Production を `No Productions / Connect a new Production`。`787-805`, `818-831`, `920-921` は no Production users、no eligible global users、no Task Types を表示。`1116-1124`, `1153-1155`, `1130-1137` は no participants, no assignments, no activity, no Task Types。`2192-2193`, `2311-2314` は no global links / no Kitsu users。`1426-1429` は API observations empty を `Not checked`。`2077-2097` は Discord member list unavailable/malformed/mismatch/access/intent を別 notice と action で表示。`2559-2577` は Setup の select/already-connected/invalid Production errors。`src/setup/middleware.go:421-435` は login failure/invalid Kitsu URL/session persistence error を alert として表示。
- Inference: empty/error state は多くの画面で次の action を伴うが、実データがないため各状態が runtime で同時にどう見えるかは未確認。
- Severity: P3 (inventory)
- Confidence: high for source; low for actual populated rendering because unauthenticated runtime only

### IA0-F-011 — Mobile/responsive route content

- Screen/route: shared admin shell and all above routes
- Scope: presentation behavior inventory only
- Evidence: `src/setup/ui.go:152` defines mobile rules at max 760px: Production rows become one column, section nav horizontally scrolls, activity rows stack, mapping list text aligns left, form actions stretch. `src/setup/ui.go` also defines max 640px and max 480px rules for top actions, mobile nav, shell padding, tables, and wizard steps. `src/setup/ia_views.go:23-34` renders desktop primary nav and a `<details class="mobile-nav">` duplicate navigation container in `appShell` (`src/setup/ui.go`, `appShell` function near shell markup).
- Inference: mobile access uses the same route map and adds a collapsible navigation presentation; no separate mobile routes are registered.
- Severity: P3 (inventory)
- Confidence: high for source; not visually runtime-verified at mobile viewport

## Compatibility / competing entry-point inventory

| Finding ID | Route | Evidence | Inference | Severity | Confidence |
|---|---|---|---|---|---|
| `IA0-F-012` | `/bot/admin/checkers` | `src/main.go:899-902` registers it; `src/setup/admin.go:2749` defines `CheckersHandler`; current `iaNav` at `src/setup/ia_views.go:23-34` omits it. | Legacy/compatibility route can expose a second checker-oriented workflow beside selected Production Users. | P3 | high |
| `IA0-F-013` | `/bot/admin/production-routing` | `src/main.go:915-921`; `src/setup/production_routing.go:53-65`; GET compatibility handler redirects to `/bot/admin/projects` with selected project. | Old routing URL remains bookmark-compatible but is not a normal current IA destination. | P3 | high |
| `IA0-F-014` | `/bot/admin/diagnostics` | `src/main.go:935-937` redirects to `/bot/admin/health`. | Explicit diagnostic alias is a duplicate URL for System Status. | P3 | high |
| `IA0-F-015` | `/setup` and `/bot/setup`; `/login` and `/bot/login`; `/api/setup/*` and `/bot/api/setup/*` | `src/main.go:859-891` registers both prefixes. | Root and `/bot` aliases support deployment/bookmark compatibility; they are duplicate address paths, not separate normal workflows. | P3 | high |

## Recommended product-level sitemap (inventory-derived, not implementation prescription)

1. Global authentication: `/bot/login` → authenticated `/bot/` redirect.
2. Global Dashboard: `/bot/admin` with next action and links to Connections, Productions, User Linking, System Status, Audit Log, and New Production setup.
3. Global setup: `/bot/setup` as the only normal new-connection wizard; keep repair as a selected-Production exception.
4. Production management: `/bot/admin/projects` → `/bot/admin/projects?project=<id>`; keep routing, users, storage, activity, troubleshooting, advanced, and danger as selected-Production sections.
5. Global settings/diagnostics: `/bot/admin/bot`, `/bot/admin/health`, `/bot/admin/audit`, `/bot/admin/users`.
6. Compatibility URLs: retain redirects/bookmark support, but do not make `/checkers`, `/production-routing`, or `/diagnostics` primary navigation destinations.

Action class: documentation/inventory only. No implementation action was authorized or performed.

## Validation and unconfirmed range

- Confirmed by source: route registration, handler ownership, selected-Production tabs, global-vs-Production scope, wizard step structure, mapping/API endpoints, empty/error branches, and CSS mobile route presentation.
- Confirmed by local runtime: `127.0.0.1:8090` was listening on 2026-08-28; GET `/bot/login` returned 200 and rendered the login form; GET `/bot/` and `/bot/admin` returned 303 to `/bot/login?lang=ja&next=...`.
- Not confirmed: authenticated JP/EN rendered screens after login; actual Kitsu Production/member/Task Type data; Discord guild/channel/member responses; Test Notification execution; POST mutations; desktop/mobile screenshots; browser console/network behavior; external server state.
- No UI/CSS/renderer/route/product behavior/external server/Git changes were made. The only created file is this report under `docs/audits`.

## Primary evidence sources

- [src/main.go](../../src/main.go) — route registration (`:859-940`).
- [src/setup/ia_views.go](../../src/setup/ia_views.go) — current IA navigation and renderers.
- [src/setup/admin.go](../../src/setup/admin.go) — admin handlers and Connections/Production surfaces.
- [src/setup/current_routing.go](../../src/setup/current_routing.go) — Production routing summary/editor.
- [src/setup/setupapi.go](../../src/setup/setupapi.go) — setup/status/mapping/test-notification APIs.
- [src/setup/middleware.go](../../src/setup/middleware.go) — login/session/redirect behavior.
- [src/setup/root_route.go](../../src/setup/root_route.go) — `/bot/` entry redirect behavior.
- [src/setup/ui.go](../../src/setup/ui.go) — shared shell and responsive rules.
- [docs/CURRENT-IA-UI-SPEC.md](../CURRENT-IA-UI-SPEC.md) — project canonical route/context specification used as corroborating documentation, not as runtime proof.
- [src/setup/ia_views_test.go](../../src/setup/ia_views_test.go) — renderer-level route/state assertions.
