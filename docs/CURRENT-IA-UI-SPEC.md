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

The two statuses are independent and vertically stacked as equal label/badge rows. Production count, routing, notification readiness, and User Linking do not change either connection status. The Dashboard Production summary shows the total visible Production count plus separate Connected and Disconnected counts; its Management Production card repeats the Connected and Disconnected counts. The Production list summary uses the same connection-state definition and shows Connected and Disconnected counts; it does not replace the two service groups. Both service groups remain contained inside the Connections card at desktop widths. ValidationOnly records are excluded from normal Production totals.

## Production list and detail

Each Production row presents the human-readable Production name, a compact content-sized status badge, and its action. `Connected` / `接続済` is positive green. `Disconnected` / `未接続` is warning yellow, not neutral gray. A visible Kitsu Production without a KitsuSync connection is included in the total and disconnected count, but not in the connected count or Needs attention count. Dashboard and Production list derive these counts from the same live-plus-local Production set and connection-state definition; ValidationOnly records are excluded from normal summary counts.

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

Advanced settings show External Kitsu URL as the normal optional setting with a separate explicit Save action. Save is disabled until the value changes; it persists only the External Kitsu URL, and clearing then saving restores the normal Kitsu URL fallback. Check link opens the currently saved effective URL and never saves the edited value. Failed saves retain the typed value and show an inline error; there is no save-on-blur or automatic save while typing. Internal Kitsu URL and API Base URL are specialist network overrides: the single expert disclosure is hidden until manual endpoint mode, except that a saved API Base URL makes it visible and open so the saved value remains editable. Internal Kitsu URL is only visible while manual endpoint mode and the disclosure are open. Existing saved settings are preserved.

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

Production detail has exactly four primary sections: `Overview` / `概要`, `Notifications` / `通知`, `Team` / `チーム`, and `Settings` / `設定`. The compact identity header contains a small Production eyebrow, Production name, and connection badge. It does not repeat “Selected Production”. Storage, Activity, Troubleshooting, Technical details, and Danger Zone are not primary tabs. Reviewers is not a primary section.

Overview always preserves three compact operational blocks in this order: Status, Current Issues, and Recent Activity, including when the underlying real data is sparse. Current Issues shows a concise healthy/empty state when clear rather than disappearing. Repeated identical causes are aggregated, no more than three issue rows are shown, and overflow is summarized as `他 N 件` / `N other issues`. Issue actions stay in the current window and target Notifications for routing/WFA/destination issues, Team for an actual blocking User Linking/member issue, and Settings for connection/diagnostic/resource issues. An ordinary unlinked Artist is not an issue. Do not add persistent state or unnecessary external reads; reuse existing read-only Team/WFA resolution only when necessary to establish a real WFA-blocking condition. Recent Activity is exact-Production, newest-first, at most five rows, with no arbitrary age cutoff; it omits ordinary task updates, successful notification sends, polling, health checks, page views, and background refreshes while retaining relevant configuration/operational changes and delivery failures. When no qualifying activity exists, preserve the section and show a compact localized empty state. Overview does not invent metrics or recipient readiness.

Notifications read mode shows a compact `Kitsu Task Type | Discord Channel | WFA recipients` table. Each routed Task Type row summarizes read-only Automatic recipients and Additional User/Role recipients. One explicit Edit action enters the global edit state. In edit state, route rows are selectable and the selected row controls one WFA panel; Automatic recipients remain read-only while Additional User/Role changes are pending. Route additions, channel changes, ordering, route removals, and Additional recipient deltas are held only in browser memory and submitted together through one Apply. Cancel, refresh, or leaving the page before Apply discards them. Apply is asynchronous: an HTTP 409 reports that the saved state changed and retains the current pending edits without a full-page reload. The client omits reviewer deltas for routes marked for removal, and the server rejects any crafted request that combines a removed route with its reviewer delta. Removing a route also removes that route's existing Additional User/Role targets inside the same database transaction; Kitsu Task Types and Discord channels are unaffected. At least one route must remain, enforced by the UI and server. The database transaction commits before Discord channel reordering; if reordering fails, KitsuSync attempts to restore the prior database snapshot and prior Discord order independently. Either compensation failure produces a non-success recovery diagnostic; the response never claims success. There is no Notification Preview, synthetic task/message, preview selector, or substitute example. The language is “WFA recipients”, “Automatic recipients”, and “Additional recipients” (JP: `WFA通知先`, `自動通知先`, `追加通知先`).

Team is a read-only view of the live Kitsu Production Team. It shows each current human member, effective Kitsu Production role, Departments only when their names can be resolved from current Production Task Type metadata, and global User Linking state. A compact in-place User Linking modal is available for unlinked people only after the live Production Team and linked Discord Guild membership are verified. It submits through the same canonical global `UserMap` validation and persistence path as `/bot/admin/users`; it stores no Production-local mapping and uses current Kitsu identity fields rather than browser-supplied names or email. After saving, Team and global User Linking show the same mapping. KitsuSync does not add/remove team members or edit Kitsu roles or Departments. A Supervisor’s displayed scope is derived by matching that person’s Department IDs to this Production’s Task Type Department IDs. Department membership does not mean the person is assigned to a Task or Shot; this view does not fetch assignment data. Unknown or missing metadata is omitted rather than guessed. A failed Kitsu Team read is distinct from a successfully empty Team. Bots are excluded.

Reviewer recipient behavior and its storage/mutation model are unchanged. Automatic targets, explicit User overrides, explicit Role overrides, delivery-time validation, mention safety, and routing remain as implemented. User-facing configuration now lives only under Notifications. Team does not provide recipient or membership mutation controls.

Settings contains Storage, Technical details, Diagnostics, and Danger Zone in that order. Storage retains its existing form and feedback behavior. Technical details, Diagnostics, and Danger Zone start collapsed; technical identifiers remain read-only with explicit copy controls. Diagnostics remains a compact vertical list. Danger Zone retains its confirmation safeguards and visual separation.

Historical direct links remain coherent: `overview` and `activity` → Overview (`activity` focuses the always-present Recent Activity section); `notifications` → Notifications; `users` and `user-settings` → Team; `reviewers` → Notifications focused on WFA recipients; `storage-settings` → Settings at Storage; `troubleshooting` → Settings with Diagnostics expanded; `advanced` → Settings with Technical details expanded; and `danger-zone` → Settings with Danger Zone expanded. Reviewer mutation redirects return to Notifications and the WFA recipients area. These compatibility routes do not create extra visible tabs.

Current IA browser acceptance covers rich and sparse Production Detail fixtures for Overview, Notifications routing and its global pending edit state, Team and its canonical global User Linking modal, and Settings in JP and EN at desktop and mobile widths. The sparse fixture contains one Team member, one notification route, no current issues, and no qualifying recent activity; all required section/table scaffolding remains visible with compact empty states. It confirms that no primary Reviewers tab or Notification Preview remains, Team membership is read-only and Kitsu-backed, Supervisor scope is derived truthfully, and normal members are not shown as having inferred task assignments. It also verifies add/remove/reorder and channel changes stay pending until the single Apply, Cancel discards them, a stale HTTP 409 leaves the browser in the same edit state, deleted routes carry no client reviewer delta, and the server's route/target transaction and explicit Discord compensation diagnostics preserve consistency.

## Production detail current overrides

- Overview always renders Status, Current Issues, and Recent Activity in order. Healthy Current Issues and empty Recent Activity use concise localized empty states without fabricated counts or activity.
- Notifications keeps fixed Task Type / Discord Channel / WFA column proportions when only one route exists; Automatic and Additional recipient summaries remain distinct, readable sub-rows.
- Team retains its five-column widths and row rhythm with one member as well as with a full team.
- Settings retains its four-section vertical hierarchy and readable disclosure rows regardless of data density.
- Notifications read mode shows the routing and per-Task-Type WFA summary table; the explicit global Edit state stages route and Additional User/Role changes in one browser-side pending transaction. Automatic recipients stay read-only. A stale 409 preserves pending edits, route deletion atomically removes that route's persisted Additional targets, and failed Discord reorder compensation is an explicit non-success recovery diagnostic.
- Team's optional User Linking modal uses the existing global UserMap path and live Team/Guild validation; no Production-local User mapping is created.
- Production Team is live from Kitsu. Team display uses global User Linking by stable Kitsu Person ID first, with the existing safe identity fallback; it does not use local ProjectUserMap rows as a substitute for global links. Automatic WFA eligibility and override delivery validation retain their existing current-Team/current-Guild rules.
- Multi-target Reviewer overrides remain additive and keyed by Production + stable Task Type ID; legacy `ProjectCheckerMap` and `CheckerMap` data remains stored and is not used as a WFA fallback.
- The External Kitsu URL has a separate explicit Save action. Check link remains read-only; clearing and saving returns to the normal Kitsu URL fallback. This Connection Settings contract is unaffected by the Production detail IA.## User Linking readiness states

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
