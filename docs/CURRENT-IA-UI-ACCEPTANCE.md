# Current IA UI Acceptance Checklist

Use an authenticated 8090 browser session. Browser-rendered output is the final acceptance evidence. Do not submit write-producing forms during this smoke check.

## Dashboard — `/bot/admin`

- [ ] Heading, summary, action-required section, New Production Connection CTA, and Management menu appear in that order.
- [ ] The Management Connections card shows explicit `Kitsu` and `Discord` status groups.
- [ ] Kitsu and Discord badges are independently derived and semantically correct.
- [ ] Kitsu and Discord status groups stay contained inside the Connections card; the Production count remains in the Production card.
- [ ] All five Management cards have equal widths at desktop; no isolated or wider Connections card appears.
- [ ] Connections Kitsu and Discord status rows are vertical, fully contained, and use matching label/badge structure.
- [ ] Production count is shown only as Production state, not as a connection-service badge.
- [ ] No horizontal overflow appears at the target desktop width.
- [ ] JP and EN have equivalent order, meaning, and actions.

## Connections normal — `/bot/admin/bot`

- [ ] Kitsu and Discord are separate named cards with independent status badges.
- [ ] Cards are equal-height and aligned on desktop without fake filler content.
- [ ] Kitsu host and masked Kitsu Bot token are visible as safe metadata.
- [ ] Masked secrets use the fixed bullet mask and never reveal plaintext.
- [ ] Discord Bot token is similarly masked.
- [ ] No Bot identity row appears in the normal card.
- [ ] Normal and edit action rows use the same shared spacing token and control gap.
- [ ] Edit and New Production Connection actions are clear and current-IA routes.

## Connections edit — `/bot/admin/bot?edit=1`

- [ ] Kitsu and Discord edit cards remain independent and equal-height on desktop.
- [ ] Kitsu host/token and Discord token fields retain their labels and secret handling.
- [ ] Saved-secret guidance is supporting text, not a health badge.
- [ ] Each save action is adjacent to its own service form.
- [ ] No credential value, token, Authorization data, or identity secret is rendered.

## Production list — `/bot/admin/projects`

- [ ] Each visible Kitsu Production has a compact content-sized status badge.
- [ ] Connected is green; Disconnected is yellow/warning.
- [ ] A visible Kitsu Production without a KitsuSync connection is Disconnected and is excluded from the connected count.
- [ ] No full-width empty status bar appears.
- [ ] Production names and actions remain aligned without overflow.

## Production detail — `/bot/admin/projects?project=<id>`

- [ ] Detail status agrees with the Production list and Dashboard count.
- [ ] Routing/resource state is shown only when it is real and current.
- [ ] No duplicate or stale resource representation is visible.
- [ ] Normal Current IA actions do not fall into a legacy renderer.
- [ ] Overview has four aligned summary cards and a separate counted current-issues card.
- [ ] Notifications shows a distinct routing section and read-only preview section with visible spacing.
- [ ] Routing rows explicitly label Kitsu Task Type and Discord Channel.
- [ ] Preview identifies Task Type, destination, Production notification language, mention behavior, and deterministic rendered message/embed; no send control exists.
- [ ] Production Users separately shows Kitsu participants, globally linked humans, and truthful Reviewer/Checker eligibility; bots are excluded.
- [ ] Troubleshooting exposes real connection, routing, participant, linking, and recent-notification diagnostics.
- [ ] Details is read-only and uses localized Production/Discord/category ID labels.

## User Linking — `/bot/admin/users`

- [ ] The page describes and renders human Kitsu-to-Discord linking.
- [ ] Bot identities are excluded from normal human linking.
- [ ] JP and EN copy is equivalent and free of mojibake.

## System Status — `/bot/admin/health`

- [ ] The page has Overall system health, API response status, operational status, and recent issues sections in that order.
- [ ] Kitsu API and Discord API appear as separate cards; no duplicate API status presentation exists.
- [ ] Kitsu and Discord API cards have equal peer widths/heights and equal graph plot regions.
- [ ] Graph outer containers and plotting regions have equal widths and heights.
- [ ] Graph x positions use observation timestamps; sparse observations do not stretch to fill the sample count.
- [ ] Both graphs use independent zero-based stepped ceilings, exactly three readable Y ticks outside the plot, shared timestamp positioning, metadata slots, and fixed 60s/5m geometry.
- [ ] Each service plot uses x=34 through x=464 in the 466×104 viewBox, with midpoint x=233; the Y tick column is outside the plot and the browser-measured right gap is at most 6px.
- [ ] Browser measurement, not viewBox ratio alone, proves the rendered baseline/grid leaves at most 6px on each side of the SVG and the graph surface has no unnecessary side padding or Y-axis gutter.
- [ ] Computed System Status typography is visibly stepped up: 32px page title, 24px major titles, 18px card titles, 26px response values, 15px body/helper, 14px metadata/details, and 12px chart labels.
- [ ] Exact chart labels are JP `60秒`, `30秒`, `5分`, `2分30秒`, `今`; EN `60s`, `30s`, `5m`, `2m30s`, `Now`.
- [ ] Both graphs show exactly three Y ticks at the same positions: maximum, midpoint, and 0, with an optional subtle midpoint guide.
- [ ] Both graphs use the same x-label positions: 60s uses `60秒` / `60s`, `30秒` / `30s`, `今` / `Now`; 5m uses `5分` / `5m`, `2分30秒` / `2m30s`, `今` / `Now`.
- [ ] Kitsu and Discord each use independent zero-based Y scales so low Kitsu latency remains visibly readable; exact current values remain the cross-service comparison.
- [ ] The current response-time value is visually primary and readable above the graph.
- [ ] The 60s / 5m selector changes the visible window without full-page reload or URL navigation.
- [ ] API graphs use chronological bars, green for success, red for failure, and explain response time in ms.
- [ ] Kitsu and Discord receive real read-only observations; unavailable data is not fabricated.
- [ ] The auto-refresh indicator remains visible while snapshot updates occur without overlapping requests.
- [ ] A transient refresh failure is visibly recoverable on the next refresh without a full-page reload.
- [ ] Expandable operational details work and contain materially useful, secret-safe data.
- [ ] Recent system issues are omitted when there are no issues.
- [ ] JP and EN status labels and explanatory text are semantically equivalent.
- [ ] Console errors/warnings are zero, major GET requests succeed, and page-level horizontal overflow is absent.
## Final System Status observability checks

- [ ] Each API card shows the current value, health badge, and one local `Last updated` line only; no normal-card `15 / 20` count or duplicate selected-window sentence is visible.
- [ ] Every real bar has a native tooltip and keyboard-reachable accessible name containing only its local timestamp, measured duration for success, and the localized success/failure status.
- [ ] Failure tooltips say `Request failed` / the Japanese equivalent and do not fabricate a duration.
- [ ] API snapshot timestamps are UTC RFC3339 and displayed in the viewer's IANA timezone; changing language does not change the timezone, and Audit Log times show the timezone context.
- [ ] English chart labels are exactly `60s / 30s / Now` and `5m / 2m30s / Now`; Japanese labels are exactly `60秒 / 30秒 / 今` and `5分 / 2分30秒 / 今`.
- [ ] The browser confirms the bars remain timestamp-positioned, full-width, zero-based, independently scaled, orthogonal, and auto-refreshed without a page reload.
- [ ] No telemetry tooltip, HTML attribute, log, or API response exposes credentials, authorization headers, response bodies, URLs containing secrets, or internal IDs.
