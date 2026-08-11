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

- Kitsu host and Kitsu Bot API Token fields
- Discord Bot Token field
- fixed secret-mask note: saved secrets are never rendered; a fixed-length bullet mask is used for a configured secret
- one save action per service

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

1. overall system health
2. API response status
3. KitsuSync operational status
4. recent system issues

API response status contains separate Kitsu API and Discord API peer cards with equal card and graph dimensions. Each card shows the latest safe response-time value as the primary metric, status, chronological bar visualization, observation count, window, and last update. Bar x positions are derived from observation timestamps across the selected window, so sparse observations remain sparse rather than stretching to the sample count. Each service uses its own zero-based Y scale selected from stable stepped ceilings, so low Kitsu latency remains readable without changing the exact value above the graph. Each graph has exactly three Y ticks at ceiling, midpoint, and 0ms, plus an optional horizontal midpoint guide. The 60-second graph labels its x positions `60秒` / `60s`, `30秒` / `30s`, and `今` / `Now`; the 5-minute graph uses `5分` / `5m`, `2分30秒` / `2m30s`, and `今` / `Now`. A failure is red; a successful observation is green. No invented latency threshold or yellow pseudo-metric is used. Both graphs share the same viewBox, plot bounds, tick positions, time-label positions, and metadata slots.
Each chart uses a 466×104 viewBox matching the desktop rendered chart aspect. Its Y tick column occupies x=0..34 outside the data plot; the plot spans x=34 through x=464 and the midpoint label is at x=233. The SVG uses its full responsive width and must not create horizontal pillarboxing through `preserveAspectRatio`.

System Status typography uses a visibly readable step: page title 32px; major section titles 24px; API and operational card titles 18px; response values 26px; helper/body text 15px; metadata 14px; chart axis/time labels 12px; and Details triggers 14px.

Chart time labels are compact: JP uses `60秒`, `30秒`, `5分`, `2分30秒`, `今`; EN uses `60s`, `30s`, `5m`, `2m30s`, `Now`.

Visual acceptance is measured in browser pixels, not viewBox percentages: the baseline/grid must leave no more than 6px on either side of the rendered SVG, and the graph surface itself is full-width within the API card. Y-axis labels remain over the plot coordinate system; no large external Y-axis gutter is reserved.

The selector supports exactly `60s` / `直近60秒` and `5m` / `直近5分`. Changing it updates the snapshot and graph without a full-page navigation. The UI refreshes the bounded snapshot every 5 seconds with overlap protection and recovers after a transient refresh failure. The runtime records bounded read-only Kitsu and Discord observations every 20 seconds. At most 20 observations per service are retained in the snapshot window.

Operational cards expose expandable, safe diagnostic details when data exists. Details may include last observation, duration, counts, route/configuration counts, and processing state; they must not expose secrets, tokens, raw response bodies, or unnecessary internal IDs. Recent system issues are shown only when there are issues.

## JP/EN parity and security

- Every Current IA route has JP and EN equivalents with the same state, order, actions, and information density.
- No unintended language leakage or mojibake is accepted.
- Credentials, Authorization headers, JWTs, webhook URLs, response bodies, and secret keys are never rendered, logged, or included in telemetry snapshots.
- Observability requests are read-only and bounded.

## Production detail final rules

The Overview uses four equal summary cards for Production state, Discord state, notification routing, and users/participants. The issue summary is a separate full-width card with a real count, such as `Current issues (0)` / `現在の問題 (0)`, and a healthy `No current issues` / `問題なし` value.

Notifications contains a status row followed by two distinct sections: Notification routing and Notification preview. Routing is an editable one-to-one `Kitsu Task Type → Discord Channel` mapping. Preview is read-only and identifies the selected Task Type, destination channel, Production notification language, mention behavior, and the deterministic rendered Discord message/embed. Preview never sends a message.

Production Users distinguishes Kitsu Production participants from globally linked human users. A globally linked human may be displayed even when the Kitsu team endpoint returns no participant; Reviewer/Checker eligibility must say when Production membership is required. Bot identities remain excluded from human linking and assignment.

Troubleshooting exposes compact, real diagnostics for Kitsu, Discord, routing integrity, participant retrieval, User Linking, and recent notification processing. Details information is read-only and uses localized identifiers: `プロダクションID`, `DiscordサーバーID`, and `カテゴリID` in Japanese.

## Source references

The primary implementation is in `src/setup/ia_views.go`, `src/setup/ui.go`, `src/setup/observability.go`, `src/setup/runtime_observation.go`, and the route registration in `src/main.go`.
The current API chart model supersedes the earlier shared-scale wording above: Kitsu and Discord use independent zero-based scales selected from stable stepped ceilings (10, 25, 50, 100, 250, 500, 1000, and 2000ms, extending safely when needed). Each chart keeps its Y tick column outside the data plot, shows ceiling/midpoint/0ms, and uses timestamps for X positions. Exact current response values above the graphs are the direct cross-service comparison; no technical timeout is rendered as a latency target.
## Final System Status observability rules

The normal Kitsu API and Discord API cards show the current response value, the service health badge, and one `Last updated HH:MM:SS` line. Sample counts and selected-window prose are not rendered in the normal card; bounded observation counts belong to diagnostic details only.

Every real telemetry bar is a timestamped observation. It has a native SVG tooltip and a keyboard-reachable accessible name. Successful observations expose the viewer-local time, measured milliseconds, and `Healthy` / the Japanese equivalent. Failed observations expose the viewer-local time and `Request failed` / the Japanese equivalent without inventing a latency value. Tooltips never contain tokens, headers, URLs, bodies, or internal identifiers.

Telemetry timestamps and generated snapshot timestamps are UTC RFC3339 values. The browser converts them with its local `Intl`/IANA timezone rules; language selection never changes timezone conversion, and daylight-saving transitions follow the browser. Audit Log timestamps use the same viewer-local conversion and include the active IANA timezone context.

The only chart time labels are `60s`, `30s`, `Now` and `5m`, `2m30s`, `Now` in English, and `60秒`, `30秒`, `今` and `5分`, `2分30秒`, `今` in Japanese. Charts retain independent zero-based scales, timestamp-based x positions, full-width plot geometry, orthogonal axes, and green success/red failure bars. No fixed latency threshold, adaptive yellow health line, or fabricated observation is displayed. Auto-refresh remains in-memory and does not persist telemetry to SQLite.
