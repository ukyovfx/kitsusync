# KitsuSync Current IA UI Spec

This is the canonical visual and content contract for the accepted Current IA. It describes the rendered UI, not obsolete intermediate screens or legacy management flows.

## Route inventory

| Area | Current route | Required purpose |
| --- | --- | --- |
| Dashboard | `/bot/admin` | Runtime summary, next action, and management navigation |
| Production list | `/bot/admin/projects` | Connected and available Kitsu Productions |
| Production detail | `/bot/admin/projects?project=<id>` | One Production's connection, routing, and resource state |
| Connections | `/bot/admin/bot` | Normal Kitsu and Discord connection summary |
| Connections edit | `/bot/admin/bot?edit=1` | Separate Kitsu and Discord credential forms |
| User Linking | `/bot/admin/users` | Human Kitsu-to-Discord linking |
| System Status | `/bot/admin/health` | Pipeline health, observations, and recent issues |
| Audit Log | `/bot/admin/audit` | Operation and notification history |
| New Production setup | `/bot/setup` | First-time Production connection wizard |

Current IA links stay within these routes. A legacy renderer must not be reached by a normal Current IA action.

## Background motion

The login page keeps its centered card as the composition anchor. On desktop, a Canvas 2D particle fabric follows the supplied continuous-silk prototype baseline: 26 strands at 7px longitudinal sampling, depth-aware particles, continuous inward streaming, separate left/right wave equations, broad slow folds, fine flutter, and the canonical burnt-orange-to-amber tiers. The two sides retain the prototype's intentional silhouette differences; neither may disappear or become unusably dark. The fabric continues beneath the card and the card's normal foreground layer provides occlusion, without a visible moat. Pointer gusts use local 2D proximity to the projected cloth, affect nearby wave/flutter/brightness, ease back, and never repel particles. Phone widths show no Login waves; the centered card remains the visual focus on a quiet dark background. Reduced motion shows a stable composed baseline. The background has no vortex, rings, network lines, bloom, or heavy glow.

Normal authenticated app pages use a static, viewport-level Canvas 2D dot grid. Dots occupy a regular fixed grid independent of content cards, routes, or panel bounds. The dark background uses very low-contrast warm-orange dots. The grid never globally animates, drifts, flows, waves, converges, recenters, repels, or translates dots. Pointer proximity may smoothly brighten and enlarge nearby dots only; dot positions remain fixed. Reduced motion looks nearly identical to the static baseline and may disable pointer activation. The grid remains the same across Dashboard, Production list, User Linking, and System Status. Neither background changes page structure, route behavior, or current IA content.

## Dashboard

The primary content order is fixed:

1. page heading and refresh action
2. summary metrics
3. Productions needing attention
4. New Production Connection CTA
5. Management menu

The management menu uses one shared equal-width responsive grid. At desktop widths all five cards use equal columns; at narrower widths the same grid wraps to balanced equal-width rows and then a single column. The Connections management card contains two explicit peer groups in the same card:

`Kitsu [status]` and `Discord [status]`

The two statuses are independent and vertically stacked as equal label/badge rows. Production count, routing, notification readiness, and User Linking do not change either connection status. The Production list card may show connected Production count; it does not replace the two service groups. Both service groups remain contained inside the Connections card at desktop widths.

## Production list and detail

Each Production row presents the human-readable Production name, a compact content-sized status badge, and its action. `Connected` / `接続済` is positive green. `Disconnected` / `未接続` is warning yellow, not neutral gray. A visible Kitsu Production without a KitsuSync connection is disconnected and is not included in the connected count.

The detail view uses the same connection-state definition as the list and Dashboard count. It may show routing/resource information only when that state exists; it must not invent or duplicate stale resources.

## Connections

Normal view:

- `Kitsu connection [Connected / Disconnected / Not configured / Needs review / Error]`
- Kitsu host
- masked Kitsu Bot API Token
- `Discord Bot connection [Connected / Disconnected / Not configured / Needs review / Error]`
- masked Discord Bot Token
- edit and New Production Connection actions

Kitsu and Discord cards are equal-height, stretch-aligned desktop peers without fake filler content. The cards stack naturally at narrow widths.

Edit view keeps the same two-card structure and independent badges:

- Kitsu host is shown as the resolved safe endpoint with automatic-detection copy and a compact `Manual setup` action. Manual mode reveals the existing validated host input and an `Automatic` action without changing resolution or persistence semantics.
- Kitsu Bot API Token field remains the normal Kitsu credential; saved secrets are never rendered.
- Discord Bot Token field
- fixed secret-mask note: saved secrets are never rendered; a fixed-length bullet mask is used for a configured secret
- one save action per service

Advanced settings show External Kitsu URL as the normal optional setting and keep the compact Check link action beside it. Internal Kitsu URL and API Base URL are specialist network overrides: the single expert disclosure is hidden until manual endpoint mode, except that a saved API Base URL makes it visible and open so the saved value remains editable. Internal Kitsu URL is only visible while manual endpoint mode and the disclosure are open. Existing saved settings are preserved.

Bot identity metadata may remain available to diagnostics, but is not a normal card row.

## Status vocabulary and color

Badges are short semantic states; explanatory sentences remain supporting text.

| Meaning | Japanese | English | Color |
| --- | --- | --- | --- |
| healthy connection | 接続済 | Connected | green |
| disconnected | 未接続 | Disconnected | yellow/warning |
| missing configuration | 未設定 | Not configured | yellow/warning |
| configured but failing validation | 要確認 | Needs review | yellow/warning |
| hard failure | エラー | Error | red/error |
| normal operation | 正常 | Healthy | green |
| waiting | 接続待 | Waiting | yellow/warning |
| unavailable | 利用不可 | Unavailable | yellow/warning |

JP and EN are semantic equivalents. Do not put a full guidance sentence in a badge.

## Spacing and responsive behavior

- Use the shared spacing tokens; section-to-section spacing is 24px.
- Action rows use a 12px control gap and a 24px section offset.
- Service status groups use a 24px peer gap and 8px label-to-badge gap.
- Status badges fit their content and do not flex-grow into empty bars.
- Desktop cards stretch to a balanced row; mobile cards and service groups wrap or stack cleanly.
- Normal desktop layouts must not introduce page-level horizontal overflow.

## System Status

System Status is organized as:

1. API response status
2. KitsuSync operational status
3. recent system issues (only when issues exist)

API response status contains separate Kitsu API and Discord API peer cards with equal card and graph dimensions. The response value or `Request failed` state sits directly below the API title; `Last updated HH:MM:SS` is right-aligned on the same row as secondary metadata, and the health badge stays in the header. The graph is a chronological line plot over one fixed rolling 60-second domain shared by both services. Each successful observation uses its monitoring-cycle timestamp and measured duration. Kitsu and Discord probes run concurrently and share one cycle timestamp. Failed observations break the line without receiving a latency value or a bottom-row X marker. A gap longer than 30 seconds breaks the line; the latest observation is labeled `Stale` / `古いデータ` after 45 seconds without a new cycle. The immediately preceding pre-window success may continue into the left boundary only when the next sample is within the 30-second continuity limit; the out-of-window sample itself is not marked. Each service uses an independent zero-based Y scale selected from stable stepped ceilings. Each graph has exactly three Y ticks at ceiling, midpoint, and 0ms, plus an optional horizontal midpoint guide. The axis and plot bounds are identical across Kitsu and Discord and across initial render/refresh. Successful observations and line segments are green. No invented latency threshold or yellow pseudo-metric is used.
Each chart uses a 496×104 viewBox with a readable Y-axis label column at x=0..40 and a baseline from x=44 through x=452. Timestamped points use x=48 through x=448, leaving symmetric marker inset and a centered midpoint at x=248. Ticks are right-aligned so their labels remain inside the SVG; the midpoint guide is at y=45. The SVG uses its full responsive width and must not create horizontal pillarboxing through `preserveAspectRatio`.

System Status typography uses a compact operational step: page title 28px; major section titles 20px; API and operational card titles 16px; response values 24px; helper/body text 14px; metadata 13px; chart axis/time labels 12px.

Chart time labels are compact and localized: `60s`, `30s`, `Now` / `60秒`, `30秒`, `今`.

Visual acceptance is measured in browser pixels, not viewBox percentages: the baseline/grid must leave no more than 6px on either side of the rendered SVG, and the graph surface itself is full-width within the API card. Y-axis labels remain over the plot coordinate system; no large external Y-axis gutter is reserved.

The graph uses a fixed 60-second rolling domain with no window selector. The UI refreshes the bounded snapshot every 5 seconds with overlap protection and recovers after a transient refresh failure. The runtime records bounded read-only Kitsu and Discord observations concurrently every 20 seconds and marks both with one shared cycle timestamp. At most 20 observations per service are retained in memory; each snapshot includes the current window and at most one preceding sample for left-boundary continuity.

System Status processing rows may show small secondary disclosures under the left-side summary when useful; their concise read-only content excludes secrets and raw sensitive values. Status badges stay top-right, with any real page action below the badge and outside the disclosure. No in-page diagnostic anchor actions are used. Recent system issues are shown only when there are issues. Status badges and any real action link share a right-side rail: Kitsu/Discord issues go to `/bot/admin/bot`, missing Production goes to `/bot/setup`, routing issues go to `/bot/admin/projects` or the affected Production's `?tab=notifications` view, and event/runtime failures go to `/bot/admin/audit`. Healthy rows and internal-data rows have no action. `New Production Connection` stays in the rail with the status that needs it.

KitsuSync operational status keeps concise state-and-cause guidance on each row. Readiness actions sit beside their status in the right rail rather than floating under the section heading. When prerequisites are ready but no event has been observed, the row says that it is waiting for the first observation without a misleading action.

## JP/EN parity and security

- Every Current IA route has JP and EN equivalents with the same state, order, actions, and information density.
- No unintended language leakage or mojibake is accepted.
- Credentials, Authorization headers, JWTs, webhook URLs, response bodies, and secret keys are never rendered, logged, or included in telemetry snapshots.
- Observability requests are read-only and bounded.

## Production detail final rules

Production detail has four primary sections only: `Overview` / `概要`, `Notifications` / `通知`, `Reviewers` / `レビュアー`, and `Settings` / `設定`. The compact identity header contains a small Production eyebrow, the Production name, and its connection badge; it does not repeat “Selected Production” copy. Storage, Activity, Troubleshooting, Technical details, and Danger Zone are not primary tabs.

Overview uses a compact operational status list, a concise Current issues section, and up to five recent audit records when they can be queried by the exact Production ID. The recent activity section is omitted when there are no matching records; the global Audit Log remains the complete history. No activity is fabricated and Overview is not presented as a large metric-card dashboard.

Notifications contains the existing routing summary and a read-only deterministic notification preview. Read mode uses explicit `Kitsu Task Type` and `Discord Channel` columns and one `Edit` / `編集` action. The existing explicit edit state retains one-to-one routing, ordering, add/remove, channel selection, Apply, and Cancel behavior. The preview selector follows the configured Task Type routes and shows each destination, Production notification language, WFA Reviewer mention policy, and an example message rendered by the current Discord card renderer. The example is clearly marked and uses no real task or recipient data. Preview never sends a message or exposes webhook/channel IDs.

Production Users reads the current Kitsu Production Team on each page load and displays each member's global User Linking state and effective Production role. KitsuSync does not add, remove, or persist Production membership. Global User Linking is the normal place to link a Kitsu person to a Discord user; stable Kitsu Person ID is preferred, with the existing safe identity fallback and read-only legacy ProjectUserMap lookup retained only for compatibility. Bot identities are excluded. Reviewer User candidates are limited to currently linked humans in the live Kitsu Production Team who are current members of its linked Discord Guild; unlinked members remain visible but cannot be selected. The Discord display name order is Guild nickname, global display name, then username. Kitsu read failure is shown separately from an empty team.

Reviewers keeps the existing WFA recipient behavior. The Task Type selector, Automatic recipients, additive User/Role Overrides, and mutation actions are unchanged. A collapsed `Reviewer eligibility` disclosure contains only candidate-related information and does not provide Production membership editing.

Settings contains four low-frequency sections in this order: Storage, Technical details, Diagnostics, and Danger Zone. The Storage form preserves its existing endpoint and validation behavior; Save begins disabled and becomes enabled only when the value changes. Technical details, Diagnostics, and Danger Zone are disclosures collapsed by default. Technical identifiers are read-only and may be copied through explicit per-value controls. Diagnostics remains a compact vertical list of current real state. Danger Zone retains the existing disconnect/delete actions and confirmation requirements and is visually separated from normal settings.

Old direct links remain valid and map into the new sections: `overview` → Overview; `notifications` → Notifications; `users` and `user-settings` → Reviewers; `storage-settings` → Settings at Storage; `activity` → Overview at Recent activity; `troubleshooting` → Settings with Diagnostics expanded; `advanced` → Settings with Technical details expanded; and `danger-zone` → Settings with Danger Zone expanded. The destination is brought into view and receives focus where appropriate. These aliases do not create extra visible tabs.

Disclosure summaries on Production Settings and System Status are indented one text level under their parent row; expanded contents are indented one additional level using logical inline properties. Status badges and action rails retain their alignment.

## Production detail current overrides

- Overview renders one current-issues representation: `現在の問題` / `Current issues` with its single status value. It does not render a duplicate count label alongside `問題なし` / `No current issues`.
- The default Notifications view is read-only. It shows a compact `Kitsu Task Type → Discord Channel` summary, one explicit `編集` / `Edit` entry point, and a Task Type selector for the read-only current-renderer example. The editor is available only through the explicit edit query state; the example is clearly identified, contains no real task/recipient data, and never sends.
- Production Users membership is sourced from the live Kitsu Production Team. Page reads do not write `ProjectUserMap`; legacy rows remain stored and may be read only as a compatibility fallback for the Team display. Global User Linking is required for Reviewer targets, and current linked Production Team members who are current linked-Guild members are the only Reviewer User candidates.
- Multi-target Reviewer overrides use an additive table keyed by Production + stable Task Type ID; legacy `ProjectCheckerMap` and `CheckerMap` rows remain stored and are not used as WFA Reviewer fallbacks.

## Production Users simple flow

Production detail Users shows the current Kitsu `Production Team`, followed by the existing `Reviewer` controls. Each member row compactly shows the person name, Discord linked/unlinked state, and effective Kitsu Production role (`project_role` when non-empty, except global `admin` remains `admin`). For a Supervisor, the page derives the readable supervised Department and current-Production Task Types by matching `Person.departments` against Task Type `DepartmentID`; it must not infer a direct Task Type-to-Supervisor relation. Missing Department or Task Type metadata is omitted rather than guessed. An unlinked member has a direct User Linking action. Search, status filters, manual Production membership forms, and local membership writes are not part of this workflow.

Reviewer means a targeted Discord WFA review recipient, not a Kitsu permission role. Targets are selected per stable Kitsu Task Type ID. The section stays concise: a Task Type selector, the automatic Reviewer (name and Department Supervisor reason) when applicable, and an `Overrides` list or `None` state. Automatic remains visible when Overrides exist; overrides add recipients. The section omits implementation explanations duplicated by the Production Team summary. Automatic eligibility requires a current active human in the live Production Team, effective Production role `supervisor`, at least one matching Department, a Task Type Department, global User Linking, and current membership in the linked Discord Guild. Position does not qualify someone. Explicit User overrides are revalidated at delivery against live Production Team membership, global User Linking, and live linked-Guild membership. Explicit Role overrides are revalidated at delivery against the linked Guild and current mentionability; `@everyone` and non-mentionable roles are unavailable. Automatic Users, explicit Users, and explicit Roles are additive and deterministically deduplicated. Legacy `ProjectCheckerMap` and `CheckerMap` rows remain stored but never become WFA Reviewer fallbacks; no migration or reset deletes them. An empty Reviewer set or a lookup failure still posts the normal WFA card without a targeted Reviewer mention. RETAKE, DONE, assignment notifications, and non-WFA legacy Checker behavior remain unchanged. WFA targets are capped at the existing 20-target notification limit.

## User Linking readiness states

`/bot/admin/users` links human Kitsu users to human Discord users. Missing Kitsu or Discord Bot configuration is a setup-readiness state: show one concise cause and one `接続設定` / `Connection settings` action, without a Discord server selector, mapping table, failure copy, or diagnostic disclosure. Configured lookups keep genuine request/auth/network failures distinct from successful empty Kitsu users, zero joined Discord servers, and a selected server with zero selectable human members.

The Discord server selector appears only when joined guild data is usable. Multiple joined servers require an explicit selection before members are loaded; this state uses a compact prompt and no bottom divider. When a server is selected, its identity remains in the selector and the mapping table begins after exactly one separator. The four-column mapping table (`Kitsu user | Discord user | state | action`) appears only when active non-bot Kitsu users and selectable non-bot Discord members are both available. Save starts disabled until the Discord selection changes; existing mappings expose their linked state and an Unlink action. Raw Discord IDs are not normal visible identity text. JP and EN preserve equivalent state, order, actions, and information density on desktop and mobile.

## Source references

The primary implementation is in `src/setup/ia_views.go`, `src/setup/ui.go`, `src/setup/observability.go`, `src/setup/runtime_observation.go`, and the route registration in `src/main.go`.
The current API chart model supersedes the earlier shared-scale wording above: Kitsu and Discord use independent zero-based scales selected from stable stepped ceilings (10, 25, 50, 100, 250, 500, 1000, and 2000ms, extending safely when needed). Each chart keeps its Y tick column outside the data plot, shows ceiling/midpoint/0ms, and uses timestamps for X positions. The graph baseline spans x=44..452 in its 496×104 viewBox; observation points span x=48..448, leaving equal 4px marker inset at both endpoints around midpoint x=248. Exact current response values above the graphs are the direct cross-service comparison; no technical timeout is rendered as a latency target.
## Final System Status observability rules

The normal Kitsu API and Discord API cards show the current response value or failure state directly below the API title, the service health badge in the card header, and one right-aligned secondary `Last updated HH:MM:SS` value on the same row as the response state. Sample counts and selected-window prose are not rendered in the normal card.

Every successful line point is a timestamped observation. It has a native SVG tooltip and a keyboard-reachable accessible name. Successful observations expose the viewer-local time, measured milliseconds, and `Healthy` / the Japanese equivalent. Failed observations break the line without a graph mark or fabricated latency; the latest failure remains explicit in the response state. Tooltips never contain tokens, headers, URLs, bodies, or internal identifiers.

Telemetry timestamps and generated snapshot timestamps are UTC RFC3339 values. The browser converts them with its local `Intl`/IANA timezone rules; language selection never changes timezone conversion, and daylight-saving transitions follow the browser. Audit Log timestamps use the same viewer-local conversion and include the active IANA timezone context.

The chart time labels are `60s`, `30s`, `Now` / `60秒`, `30秒`, `今`. Charts retain independent zero-based scales, a shared timestamp-based 60-second X domain, full-width plot geometry, orthogonal axes, and green successful line segments. Failed observations and gaps longer than 30 seconds break the line without a bottom-row marker or plotted latency. The adjacent pre-window sample may interpolate an entering segment at the left boundary when its gap is within 30 seconds. A latest observation older than 45 seconds is identified as stale. No fixed latency threshold, adaptive yellow health line, or fabricated observation is displayed. Auto-refresh remains in-memory and does not persist telemetry to SQLite.

Processing-status rows keep the title and short cause on the left. Where useful supplemental information exists, a small secondary disclosure appears slightly indented beneath the summary; its contents are indented with it. its content is concise, read-only, and excludes secrets and raw sensitive values. The status badge stays at the top of the right rail, with any real page action directly below it and outside the disclosure.
