# Agent 9 — Content / microcopy JP–EN

Status: documentation-only content design. This report changes no UI code, route, behavior, or `DESIGN.md`.

## 1. Scope and evidence boundary

This report defines the content system for the Current IA: labels, helper/empty/warning/destructive/success/error copy, technical terms, status names, button language, punctuation/capitalization, JP/EN parity, and consequence-first writing. Current facts are source/evidence observations. Rules marked “future” are design requirements, not claims that the current UI already satisfies them.

Primary evidence:

- `docs/CURRENT-IA-UI-SPEC.md:43-80,91-118,120-141` — route content contract, status vocabulary, safety and JP/EN parity.
- `docs/SETUP_WIZARD.md:18-34,36-54,67-89,106-114` — setup stages, readiness, fail-closed planning, and preview/execute boundary.
- `docs/NOTIFICATION_SAFETY.md:3-7` — no-send/ambiguous-ownership safety boundary.
- `docs/UI-UX-DISTRIBUTED-AUDIT.md:18-23,38-42,150-168` — Production-first, consequence-first and parity intent.
- `src/setup/i18n_catalog.go:10-110` — shared JP/EN catalog for IA, wizard, routing, channel-plan, result, workflow, and login copy.
- `src/setup/i18n.go:8-17,31-132,134-246` — locale selection, legacy-label canonicalization, and fallback wording.
- `src/setup/ia_views.go:23-34,549-575,787-835,1116-1160,1238-1321,1415-1435,2180-2205,2385-2520,2545-2760` — current rendered labels and empty/error/state branches.
- `src/setup/diagnostics.go:671-745,907-1032` — notification test wording and the current send/verification distinction.
- `src/setup/production_routing.go:102-175,217-290` — dry-run, save, pause/resume, and routing state copy.
- `src/setup/first_time_connection.go:245-285,339-420` and `src/setup/channel_plan_handler.go:15-155,288-290` — setup execution, rollback, stale/partial failure, and result copy.
- `src/setup/project_discord_health.go:112-131` and `src/setup/workflow_diagnosis.go:217-240,411-453` — repair and diagnostic failure copy.

No secrets, tokens, webhook URLs, response bodies, or secret values are reproduced here.

## 2. Current content facts

### 2.1 Locale and catalog

- `currentLang()` accepts only `en`; all other values resolve to Japanese (`src/setup/i18n.go:8-17`). The future content rule is therefore a two-locale contract, not a general locale negotiation system.
- The shared catalog already covers the main IA, wizard, routing, channel-plan, setup-result, runtime, workflow, and login keys in both languages (`src/setup/i18n_catalog.go:10-110`).
- Legacy English strings are mapped to canonical catalog keys or explicit Japanese equivalents by `canonicalText()` and `t()` (`src/setup/i18n.go:31-132,134-246`). This is a compatibility mechanism, not proof that every current string has one canonical key.
- Current copy visibly mixes product terms and translated terms: for example `Production`, `Task Type`, `Discord`, `Bot`, `Guild`, `webhook`, `mapping`, and `stale` appear in Japanese strings (`src/setup/i18n_catalog.go:10-110`; `src/setup/workflow_diagnosis.go:445-502`).
- The canonical IA contract requires JP/EN equivalence of state, order, actions, and information density and rejects unintended language leakage/mojibake (`docs/CURRENT-IA-UI-SPEC.md:113-118`). Runtime evidence confirms only the reachable Login and navigation states, not every authenticated or failure state (`docs/UI-UX-RUNTIME-EVIDENCE.md:21-44,116-128`).

### 2.2 Current status and state facts

The canonical status table defines these semantic pairs: `接続済 / Connected`, `未接続 / Disconnected`, `未設定 / Not configured`, `要確認 / Needs review`, `エラー / Error`, `正常 / Healthy`, `接続待 / Waiting`, and `利用不可 / Unavailable` (`docs/CURRENT-IA-UI-SPEC.md:65-80`). The catalog also contains shorter operational states such as `有効 / Enabled`, `一時停止中 / Paused`, `準備完了 / Ready`, `競合 / Conflict`, and `確認が必要 / Review required` (`src/setup/i18n_catalog.go:10-11,49-67`). These must not be treated as interchangeable synonyms: a connection state, a routing state, a plan classification, and an action-needed state describe different dimensions.

The setup plan already distinguishes `作成 / Create`, `再利用 / Reuse`, `競合 / Conflict`, and `確認が必要 / Review required`, and explains that preview is read-only until explicit confirmation (`src/setup/i18n_catalog.go:11,69-97`; `docs/SETUP_WIZARD.md:24-32,52-54,106-114`).

### 2.3 Current consequence gaps

- Test Notification marks a project `Verified` after the send API response while still instructing the operator to confirm arrival in Discord (`src/setup/diagnostics.go:962-990,1023-1031`). Future copy must distinguish “sent by KitsuSync” from “arrival confirmed by operator.”
- Setup execution and partial failure already expose rollback/cleanup concepts, but the visible result paths use generic retry/review language and can return to a generic review surface (`src/setup/first_time_connection.go:273-282,339-418`; `src/setup/channel_plan_handler.go:80-151,288-290`). Future copy must state operation outcome, remaining consequence, and retry eligibility before the action.
- Workflow Diagnosis shows a disconnected state and a back link, but the source does not provide a reconnect action in that dead-end (`src/setup/workflow_diagnosis.go:217-237,411-440`). Future copy must name the missing prerequisite and the recovery destination when the IA allows it; this report does not change the route.
- Empty branches generally provide an action or explanation, e.g. no Productions, no users, no assignments, no activity, no Task Types, and `Not checked` observations (`src/setup/ia_views.go:494-496,787-835,900-930,1116-1160,1415-1435,2180-2205`). The future rule is to preserve that actionability without implying a failure when the state is simply empty.

## 3. Content model: state before explanation, consequence before control

### 3.1 Universal message grammar (future rule)

| Rule | WHAT | WHY | WHEN | WHEN NOT |
|---|---|---|---|---|
| State first | Put the human-readable state first, then the object/value, then the reason, then the next action. | Operators need to know what happened before reading implementation detail. | Connection, readiness, routing, setup, health, empty, error, and recovery states. | Do not lead with an internal ID, API error, or generic “Something went wrong.” |
| Consequence first | For a write or failure, state what changed or did not change and what the operator should do next. | Prevents unsafe retry, false completion, and premature incident closure. | Create/reuse, routing save, pause/resume, notification test, cleanup, rollback, delete, and stale-plan states. | Do not put the action label before the consequence when the action can create, send, delete, or pause. |
| One decision | Give one primary next action; secondary inspection/back actions remain visibly secondary. | Reduces competing recovery paths. | Empty, blocked, disconnected, partial-failure, and setup-complete states. | Do not offer “Retry”, “Review”, “Back”, and “Open” with equal emphasis when only one is safe. |
| Evidence on demand | Keep operator meaning in normal copy; expose IDs, counts, scopes, and technical diagnostics in Details. | Supports recovery without making raw technical data the headline. | Health, Workflow Diagnosis, Audit Log, and repair states. | Do not hide destination, affected Production, failure reason, or no-write consequence when needed to make a safe decision (`docs/CURRENT-IA-UI-SPEC.md:111,124-128`). |

### 3.2 Required message slots

Future state copy should use these slots where applicable:

`[State] — [object/scope]. [Consequence or reason]. [Next action].`

Examples (future wording, not implementation claims):

- `Needs review — Discord resources for <Production> could not be verified. No new routing was enabled. Review the repair plan.`
- `Sent — the test notification was accepted by Discord for <destination>. Confirm that it arrived in Discord to complete verification.`
- `Not ready — the Discord Bot connection is not configured. Notifications remain disabled. Configure the Bot connection.`
- `Blocked — the plan contains an ownership conflict. No Discord changes were made. Resolve the conflict and review the plan again.`

Do not expose secret-related values in any slot. Use only safe names, masked indicators, counts, timestamps, and non-secret identifiers allowed by the IA contract (`docs/CURRENT-IA-UI-SPEC.md:111-118`).

## 4. Label and terminology standard

### 4.1 Canonical product terms (future rule)

| Concept | Japanese | English | Usage rule |
|---|---|---|---|
| Product object | `Production` (recommended stable product term) | `Production` | Keep the product term if the screen means a Kitsu Production. Do not alternate with `project` in the same user-facing context; source currently does both (`src/setup/i18n_catalog.go:17-20,32-35`; `src/setup/workflow_diagnosis.go:424`). |
| Task identity | `Task Type` | `Task Type` | Keep as a technical product term; explain it once in helper copy when the audience is first-time setup. Do not translate it in one screen and leave it technical in another. |
| Discord destination container | `Discordサーバー` | `Discord server` | Use this normal-user term. Reserve `Guild` / `Guild ID` for diagnostics or a field whose API identity is material; the setup spec describes the user concept as Discord Server / Guild ID (`docs/SETUP_WIZARD.md:24-32,67-75`). |
| Notification mapping | `通知先設定` or `通知ルーティング` by context | `Notification destinations` or `Notification routing` by context | Use `routing` for the editable mapping and `destination` for the selected target. Do not use `mapping`, `route`, and `destination` as synonyms in one control group. |
| Connection | `接続` / `接続済み` | `Connection` / `Connected` | Use `接続` for an action or configuration; use `接続済み` only for the positive state. Do not use `連携` for a different meaning unless the screen explicitly describes association/linking. |
| User association | `ユーザーリンク` for global linking; `Productionユーザー` for local association | `User Linking`; `Production users` | Keep global linking and Production-local association separate (`docs/CURRENT-IA-UI-SPEC.md:126,137-141`). Do not label both as `Users`. |
| Read-only inspection | `送信せずに確認` | `Check without sending` | Use for dry-run/preview inspection. Do not use `Test`, `Run`, or `Send` for a no-write action (`src/setup/i18n_catalog.go:10-11,49-57`). |
| Details | `詳細` or `詳細情報` | `Details` | Use `Details` for safe diagnostic expansion; do not put raw IDs or implementation terms in the primary label (`docs/CURRENT-IA-UI-SPEC.md:111,128`). |

### 4.2 Technical terms and disclosure (future rule)

WHAT: preserve exact technical terms only when they identify a real external/system concept; pair them with a human action or explanation. WHY: changing names can make reconciliation with Kitsu/Discord impossible, while unexplained jargon slows beginners. WHEN: `Production`, `Task Type`, Discord server, channel, webhook, routing, dry-run, stable ID, and status short names. WHEN NOT: do not surface API field names, raw classifications, or `Guild` merely to sound technical; do not expose secrets or raw responses.

Recommended first-use explanations:

- `Task Type — Kitsuの作業種別。通知先の単位です。` / `Task Type — the Kitsu work type used as the notification mapping key.`
- `通知ルーティング — Task TypeからDiscordチャンネルへの送信先設定。` / `Notification routing — the Task Type-to-Discord Channel destination mapping.`
- `dry-run — 送信せずに実行条件を確認する操作。` / `dry-run — inspect the intended action without sending a notification.`

After first use, use the short canonical term consistently. Do not translate external names, user-provided Production names, channel names, or technical IDs.

## 5. Copy patterns by state

| State | Japanese pattern | English pattern | Rule: WHAT / WHY / WHEN / WHEN NOT |
|---|---|---|---|
| Helper | `何を確認/入力するか。制約があれば続けて説明。` | `What to check or enter. State the constraint next.` | WHAT: orient before input. WHY: prevents hidden prerequisites. WHEN: forms, wizard steps, preview, settings. WHEN NOT: do not repeat the label or describe implementation internals. |
| Empty | `まだ[対象]はありません。次に[安全な操作]。` | `No [items] yet. [Safe next action].` | WHAT: distinguish no data from failure. WHY: empty is a valid state. WHEN: lists, activity, assignments, diagnoses. WHEN NOT: do not call it `Error` or imply deletion. |
| Warning/blocked | `[状態]。[影響/未実行]。[解消 action]。` | `[State]. [Impact/not performed]. [Resolution action].` | WHAT: make safety boundary explicit. WHY: operators must know whether writes occurred. WHEN: stale, conflict, unavailable, disconnected, not ready. WHEN NOT: do not soften a blocked write as generic advice. |
| Error | `[失敗対象]に失敗しました。[保持/未実行/一部変更]。[次の確認]。` | `Could not [action] [object]. [What did/did not change]. [Next check].` | WHAT: name action and consequence. WHY: “failed” alone cannot guide recovery. WHEN: API, persistence, validation, cleanup, rollback. WHEN NOT: do not place raw error text or secrets in the headline. |
| Success | `[完了状態]。[保存/作成/再利用]の結果。[次の確認]。` | `[Completed state]. [Saved/created/reused result]. [Next check].` | WHAT: report durable result and remaining verification. WHY: setup success is not necessarily end-to-end delivery. WHEN: setup complete, routing save, test send, repair. WHEN NOT: do not say `Verified` until the specified verification evidence exists. |
| Destructive | `[対象]に対する[結果]を明記。確認文は結果を言い換える。` | `Name the [object] and exact [result]. Confirmation repeats the consequence.` | WHAT: name scope, permanence, and boundary. WHY: “Delete”/“Remove” can mean different stores or external resources. WHEN: disconnect, delete Discord resources, remove association/role. WHEN NOT: do not use vague `Confirm` or hide whether KitsuSync data, Discord resources, or both are affected. |

## 6. Buttons and control language

### 6.1 Button verbs (future rule)

WHAT: use a direct verb plus object or consequence where ambiguity is possible. WHY: button text is the operator’s final prediction of what will happen. WHEN: every primary/secondary action. WHEN NOT: do not use vague labels such as `OK`, `Submit`, `Apply`, `Run`, `Manage`, or `Continue` when the target/result is known.

Preferred pairs:

| Action | Japanese | English | Boundary |
|---|---|---|---|
| Move wizard forward | `次へ` | `Next` | Use only when the next step is obvious; otherwise name it, e.g. `内容を確認`. |
| Read-only preview | `送信せずに確認` | `Check without sending` | Never label a no-write action `Send` or `Test`. |
| Save routing | `保存して有効化` | `Save and enable` | Use only when the save actually enables routing (`src/setup/i18n_catalog.go:49-57`). |
| Pause/resume | `通知を一時停止` / `通知を再開` | `Pause notifications` / `Resume notifications` | Name the notification consequence; do not shorten to `Pause`/`Resume`. |
| Setup execution | `接続する` | `Connect` | Place after explicit review/confirmation; pair with a consequence-first review sentence (`src/setup/i18n_catalog.go:11`; `docs/SETUP_WIZARD.md:106-114`). |
| Destructive resource action | `Discord側のリソースを削除` | `Delete Discord resources` | Always name the external boundary; do not use `Delete` alone. |

### 6.2 Confirmation and cancel

WHAT: confirmation text must describe the exact write or deletion, and the confirm button must repeat the irreversible or external consequence. WHY: a checkbox or modal title alone is not durable consent. WHEN: setup execution, Discord resource deletion, disconnect, and association removal. WHEN NOT: do not ask for confirmation for a read-only refresh, details expansion, or dry-run.

Examples: `この内容でDiscordに変更を行います。` / `This will make the listed changes in Discord.`; `Discord側のチャンネルと関連リソースを削除します。` / `Delete the Discord channels and related resources for this Production.` The actual affected resource scope must be derived from the operation, not invented by copy.

## 7. JP/EN parity, punctuation, and capitalization

### 7.1 Parity invariants (future rule)

WHAT: every state has the same semantic slots, order, primary action, consequence, and disclosure depth in JP and EN. WHY: locale switching must not change the operator’s decision or safety boundary. WHEN: all Current IA routes and all states, especially setup, diagnostics, empty, partial failure, and destructive dialogs. WHEN NOT: do not force identical character count or word order; Japanese grammar may be shorter, but it must not omit meaning (`docs/CURRENT-IA-UI-SPEC.md:113-118`).

Required parity checks:

- State/status pair exists in both locales and means the same thing.
- The same object and scope are named; `Production`, destination channel, and affected external service are not dropped in Japanese.
- “No write occurred”, “routing remains disabled”, “rollback completed”, and “arrival still requires confirmation” survive translation.
- A missing/failed API or unavailable dataset is not translated as a healthy empty state.
- Locale switching preserves route, selected Production, selected tab/step, and the same primary action (`src/setup/i18n.go:19-29`; `docs/CURRENT-IA-UI-SPEC.md:113-118`).

### 7.2 Punctuation and capitalization (future rule)

WHAT: use sentence case in English prose and button labels; capitalize fixed product names and technical identifiers exactly as their systems do. WHY: sentence case is easier to scan and avoids applying English casing rules to Japanese. WHEN: headings, helper text, status explanations, buttons, tables, and alerts. WHEN NOT: do not uppercase Japanese strings or use wide English tracking as a substitute for hierarchy (`docs/design-work/design-agent3-typography.md`; `src/setup/ui.go:269-284`).

Rules:

- English: `Connected`, `Needs review`, `Check without sending`, `Production users`; avoid title-case every word.
- Japanese: no terminal period for compact labels/badges; use `。` for complete helper, warning, error, and success sentences. Do not add a space between Japanese and a product term unless the chosen glossary explicitly requires it; keep one consistent policy.
- Use one punctuation style in a message: do not mix ASCII `:` with Japanese full-width sentence punctuation arbitrarily. For technical key/value diagnostics, ASCII `:` is acceptable; for Japanese prose, prefer `。` and `：` consistently within the component.
- Keep `JP`/`EN` as language-control labels, not `Japanese`/`English`, unless the control has room and the product decision explicitly prefers full names (`src/setup/ui.go:552-559`).
- Do not apply uppercase or letter-spacing to long Japanese labels; the typography audit notes this as a parity risk (`docs/design-work/design-agent3-typography.md`; `src/setup/ui.go:269-284`).

## 8. Acceptance checklist for future implementation

- Every user-facing state identifies object, state, consequence, and next action where relevant.
- Empty is distinct from error; blocked is distinct from unavailable; `Connected` is distinct from notification-ready.
- Test Notification reports send acceptance separately from operator-confirmed arrival (`src/setup/diagnostics.go:962-990,1023-1031`).
- Setup partial failure states report rollback/cleanup outcome and retry eligibility before offering retry (`src/setup/first_time_connection.go:339-418`; `src/setup/channel_plan_handler.go:80-151`).
- Destructive copy names the exact scope: Production association, Discord resources, or both; no generic `Delete`/`Remove` labels.
- `Production`, `Task Type`, Discord server/channel, routing, destination, User Linking, and local Production association use one glossary per context.
- JP and EN retain equal state/order/action/consequence meaning, with no untranslated UI scaffolding or mojibake (`docs/CURRENT-IA-UI-SPEC.md:113-118`).
- Buttons use direct verbs and consequences; read-only actions never look like sends or writes.
- No content renders secrets, tokens, webhook URLs, raw bodies, or unnecessary internal IDs (`docs/CURRENT-IA-UI-SPEC.md:111-118`).
- English uses sentence case; Japanese uses natural sentence punctuation and does not inherit English uppercase/tracking behavior.
- Authenticated JP/EN states, including empty/error/stale/recovery states, remain a required evidence pass because they are not covered by the current runtime evidence (`docs/UI-UX-RUNTIME-EVIDENCE.md:40-44,70-80`).

## 9. Source index

The canonical content and safety sources are `docs/CURRENT-IA-UI-SPEC.md:43-80,91-118,120-155`, `docs/SETUP_WIZARD.md:18-34,36-54,67-89,106-114`, and `docs/NOTIFICATION_SAFETY.md:3-7`. The current implementation sources are `src/setup/i18n_catalog.go:10-110`, `src/setup/i18n.go:8-17,31-246`, `src/setup/ia_views.go`, `src/setup/diagnostics.go:671-745,907-1032`, `src/setup/production_routing.go:102-175,217-290`, `src/setup/first_time_connection.go:245-285,339-420`, `src/setup/channel_plan_handler.go:15-155,288-290`, `src/setup/project_discord_health.go:112-131`, and `src/setup/workflow_diagnosis.go:217-240,411-453`. The higher-level intent and typography constraints are `docs/UI-UX-DISTRIBUTED-AUDIT.md:18-23,38-42,150-168` and `docs/design-work/design-agent3-typography.md`.
