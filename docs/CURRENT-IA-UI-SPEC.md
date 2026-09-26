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

API response status contains separate Kitsu API and Discord API peer cards with equal card and graph dimensions. Each card shows the latest safe response-time value, status, one horizontal `Current response time` / `Last updated HH:MM:SS` metadata row on desktop (stacked on mobile), and a chronological bar visualization. Sample counts and selected-window prose remain secondary diagnostic data, not normal-card copy. Bar x positions are derived from observation timestamps across the selected window, so sparse observations remain sparse rather than stretching to the sample count. Each service uses its own zero-based Y scale selected from stable stepped ceilings, so low Kitsu latency remains readable without changing the exact value above the graph. Each graph has exactly three Y ticks at ceiling, midpoint, and 0ms, plus an optional horizontal midpoint guide. The 60-second graph labels its x positions `60s`, `30s`, and `0s`; the 5-minute graph uses `5m`, `2.5m`, and `0s`, independent of UI language. A failure is red; a successful observation is green. No invented latency threshold or yellow pseudo-metric is used. Both graphs share the same viewBox, plot bounds, tick positions, time-label positions, and metadata slots.
Each chart uses a 466×104 viewBox matching the desktop rendered chart aspect. Its Y tick column occupies x=0..34 outside the data plot; the plot spans x=34 through x=464 and the midpoint label is at x=233. The SVG uses its full responsive width and must not create horizontal pillarboxing through `preserveAspectRatio`.

System Status typography uses a compact operational step: page title 28px; major section titles 20px; API and operational card titles 16px; response values 24px; helper/body text 14px; metadata 13px; chart axis/time labels 12px; and Details triggers 14px.

Chart time labels are compact, language-independent units: `60s`, `30s`, `0s` and `5m`, `2.5m`, `0s`.

Visual acceptance is measured in browser pixels, not viewBox percentages: the baseline/grid must leave no more than 6px on either side of the rendered SVG, and the graph surface itself is full-width within the API card. Y-axis labels remain over the plot coordinate system; no large external Y-axis gutter is reserved.

The selector supports exactly `60s` / `直近60秒` and `5m` / `直近5分`. Changing it updates the snapshot and graph without a full-page navigation. The UI refreshes the bounded snapshot every 5 seconds with overlap protection and recovers after a transient refresh failure. The runtime records bounded read-only Kitsu and Discord observations every 20 seconds. At most 20 observations per service are retained in the snapshot window.

Operational cards expose expandable, safe diagnostic details when data exists. Details may include last observation, duration, counts, route/configuration counts, and processing state; they must not expose secrets, tokens, raw response bodies, or unnecessary internal IDs. Recent system issues are shown only when there are issues.

KitsuSync operational status uses one readiness-derived section-level next-action treatment under the section heading when a blocker exists. Setup required links to Kitsu connection settings; Discord Bot setup required links to Discord Bot settings; Production required links to New Production Connection; Routing required links to notification settings/Productions. Event and Notification rows retain concise state-and-cause guidance without repeating the same readiness CTA. An event runtime failure may retain a separate safe Recent issues or diagnostics action. When prerequisites are ready but no event has been observed, the row says that it is waiting for the first observation without a misleading action.

## JP/EN parity and security

- Every Current IA route has JP and EN equivalents with the same state, order, actions, and information density.
- No unintended language leakage or mojibake is accepted.
- Credentials, Authorization headers, JWTs, webhook URLs, response bodies, and secret keys are never rendered, logged, or included in telemetry snapshots.
- Observability requests are read-only and bounded.

## Production detail final rules

The Overview uses four equal summary cards for Production state, Discord state, notification routing, and users/participants. The issue summary is a separate full-width card with a real count, such as `Current issues (0)` / `現在の問題 (0)`, and a healthy `No current issues` / `問題なし` value.

Notifications contains a status row followed by two distinct sections: Notification routing and Notification preview. Routing is an editable one-to-one `Kitsu Task Type → Discord Channel` mapping. Preview is read-only and identifies the selected Task Type, destination channel, Production notification language, mention behavior, and the deterministic rendered Discord message/embed. Preview never sends a message.

Production Users reads the current Kitsu Production Team on each page load and displays each member's global User Linking state. KitsuSync does not add, remove, or persist Production membership. Global User Linking is the normal place to link a Kitsu person to a Discord user; stable Kitsu Person ID is preferred, with the existing safe identity fallback and read-only legacy ProjectUserMap lookup retained only for compatibility. Bot identities are excluded. Reviewer User candidates are limited to currently linked humans in the live Kitsu Production Team; unlinked members remain visible but cannot be selected. Kitsu read failure is shown separately from an empty team.

Troubleshooting exposes compact, real diagnostics for Kitsu, Discord, routing integrity, participant retrieval, User Linking, and recent notification processing. Details information is read-only and uses localized identifiers: `プロダクションID`, `DiscordサーバーID`, and `カテゴリID` in Japanese.

## Production detail current overrides

- Overview renders one current-issues representation: `現在の問題` / `Current issues` with its single status value. It does not render a duplicate count label alongside `問題なし` / `No current issues`.
- The default Notifications view is read-only. It shows a compact `Kitsu Task Type → Discord Channel` summary and one explicit `編集` / `Edit` entry point. The editor is available only through the explicit edit query state; the preview remains read-only and never sends.
- Production Users membership is sourced from the live Kitsu Production Team. Page reads do not write `ProjectUserMap`; legacy rows remain stored and may be read only as a compatibility fallback. Global User Linking remains the normal Kitsu-to-Discord mapping surface, and current linked Production Team members are the only Reviewer User candidates.
- Multi-target Reviewer overrides use an additive table keyed by Production + stable Task Type ID; legacy `ProjectCheckerMap` rows remain readable and are not removed during migration.

## Production Users simple flow

Production detail Users shows the current Kitsu `Production Team`, followed by the existing `Reviewer` controls. Each member row compactly shows the person name, Discord linked/unlinked state, and Kitsu role. For a Supervisor, the page derives the readable supervised Department and current-Production Task Types by matching `Person.departments` against Task Type `DepartmentID`; it must not infer a direct Task Type-to-Supervisor relation. Missing Department or Task Type metadata is omitted rather than guessed. An unlinked member has a direct User Linking action. Search, status filters, manual Production membership forms, and local membership writes are not part of this workflow.

Reviewer targets are selected per stable Kitsu Task Type ID. The section stays concise: a Task Type selector, the automatic Reviewer (name, linked state, and Department Supervisor reason) when applicable, and an `Overrides` list or `None` state. While an override is present, the automatic section states that it is inactive without making an additional Supervisor lookup. The section omits implementation explanations duplicated by the Production Team summary. The explicit User candidate list contains only globally linked humans who are in the current live Kitsu Production Team; no manual Production assignment is required. The automatic list shows eligible Department Supervisors in the Production Team with their User Linking state. An explicit KitsuSync override may contain multiple linked Discord Users and mentionable Discord guild Roles. Adding, removing one target, or resetting the Task Type override does not change notification channels. Reset removes only new explicit targets; a preserved legacy Production mapping resumes until it is explicitly removed. Legacy `ProjectCheckerMap` rows remain readable; no migration or reset deletes them. Discord roles are discovered read-only from the linked Production guild; `@everyone` and non-mentionable roles are unavailable. At most 20 explicit targets are mentioned per notification.

## User Linking readiness states

`/bot/admin/users` links human Kitsu users to human Discord users. Missing Kitsu or Discord Bot configuration is a setup-readiness state: show one concise cause and one `接続設定` / `Connection settings` action, without a Discord server selector, mapping table, failure copy, or diagnostic disclosure. Configured lookups keep genuine request/auth/network failures distinct from successful empty Kitsu users, zero joined Discord servers, and a selected server with zero selectable human members.

The Discord server selector appears only when joined guild data is usable. Multiple joined servers require an explicit selection before members are loaded. The four-column mapping table (`Kitsu user | Discord user | state | action`) appears only when active non-bot Kitsu users and selectable non-bot Discord members are both available. Save starts disabled until the Discord selection changes; existing mappings expose their linked state and an Unlink action. Raw Discord IDs are not normal visible identity text. JP and EN preserve equivalent state, order, actions, and information density on desktop and mobile.

## Source references

The primary implementation is in `src/setup/ia_views.go`, `src/setup/ui.go`, `src/setup/observability.go`, `src/setup/runtime_observation.go`, and the route registration in `src/main.go`.
The current API chart model supersedes the earlier shared-scale wording above: Kitsu and Discord use independent zero-based scales selected from stable stepped ceilings (10, 25, 50, 100, 250, 500, 1000, and 2000ms, extending safely when needed). Each chart keeps its Y tick column outside the data plot, shows ceiling/midpoint/0ms, and uses timestamps for X positions. Exact current response values above the graphs are the direct cross-service comparison; no technical timeout is rendered as a latency target.
## Final System Status observability rules

The normal Kitsu API and Discord API cards show the current response value, the service health badge, and one `Last updated HH:MM:SS` line. Sample counts and selected-window prose are not rendered in the normal card; bounded observation counts belong to diagnostic details only.

Every real telemetry bar is a timestamped observation. It has a native SVG tooltip and a keyboard-reachable accessible name. Successful observations expose the viewer-local time, measured milliseconds, and `Healthy` / the Japanese equivalent. Failed observations expose the viewer-local time and `Request failed` / the Japanese equivalent without inventing a latency value. Tooltips never contain tokens, headers, URLs, bodies, or internal identifiers.

Telemetry timestamps and generated snapshot timestamps are UTC RFC3339 values. The browser converts them with its local `Intl`/IANA timezone rules; language selection never changes timezone conversion, and daylight-saving transitions follow the browser. Audit Log timestamps use the same viewer-local conversion and include the active IANA timezone context.

The only chart time labels are `60s`, `30s`, `0s` and `5m`, `2.5m`, `0s` in every language. Charts retain independent zero-based scales, timestamp-based x positions, full-width plot geometry, orthogonal axes, and green success/red failure bars. No fixed latency threshold, adaptive yellow health line, or fabricated observation is displayed. Auto-refresh remains in-memory and does not persist telemetry to SQLite.
