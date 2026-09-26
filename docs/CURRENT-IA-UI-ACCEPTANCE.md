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
- [ ] Normal edit mode shows the resolved Kitsu host as a quiet read-only value, says it was detected/configured automatically, and offers compact `Change manually` / `Use automatic endpoint` controls without a page reload.
- [ ] Manual endpoint mode preserves the existing validation and persistence contract and does not expose a display placeholder as a submitted host.
- [ ] External Kitsu URL is the only normal Advanced setting; its Check link control stays compact beside the field and wraps cleanly on mobile.
- [ ] Internal Kitsu URL and API Base URL appear only in one expert disclosure after manual endpoint mode, except that a saved API Base URL keeps the disclosure visible/open for editing; saved values are never cleared implicitly.
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
- [ ] Production Users reads the live Kitsu Production Team, shows linked/unlinked global User Linking state, distinguishes read failure from an empty team, and excludes bots.
- [ ] Troubleshooting exposes real connection, routing, participant, linking, and recent-notification diagnostics.
- [ ] Details is read-only and uses localized Production/Discord/category ID labels.

## User Linking — `/bot/admin/users`

- [ ] The page describes and renders human Kitsu-to-Discord linking.
- [ ] Missing Kitsu or Discord Bot configuration shows one concise setup cause and one Connection settings action only; no server selector, mapping table, failure copy, or diagnostic disclosure appears.
- [ ] A genuine Kitsu lookup failure is distinct from a successful zero-user result; neither state renders blocked Discord mapping controls.
- [ ] A genuine Discord lookup failure is distinct from zero joined servers and from a selected server with zero selectable human members.
- [ ] The Discord server selector appears only when joined guild data is meaningful; multiple joined servers wait for an explicit selection before mapping rows appear.
- [ ] Ready state shows exactly four mapping columns: Kitsu user, Discord user, state, and action. Real display names are used and raw Discord IDs are not visible as identity text.
- [ ] Save starts disabled and becomes actionable only after the Discord selection changes; existing mappings show their linked state and Unlink action, and save/unlink persist across refresh.
- [ ] Kitsu Bot and Discord bot identities are excluded from normal human linking.
- [ ] JP and EN have equivalent states, order, actions, and information density at desktop and mobile widths, with no mojibake or page overflow.

## Production Users Kitsu Team checks

- [ ] Normal Users view shows the live Kitsu Production Team before the Reviewer controls; no manual add/remove membership workflow appears.
- [ ] Each Team row compactly shows name, Discord link state, and Kitsu role; Supervisor Department/Task Type display is derived only by matching Person Department IDs to Task Type Department IDs, and missing metadata is omitted safely.
- [ ] Each current Team member resolves through global User Linking by stable Kitsu Person ID first; a clear User Linking action appears for unlinked people.
- [ ] Reviewer User candidates include only globally linked human users in the current Kitsu Production Team; unlinked Team members and linked non-Team users are not selectable.
- [ ] Page reads do not create/update `ProjectUserMap`; existing legacy rows remain intact and legacy Reviewer rows remain readable.
- [ ] A successful empty Team and a failed Kitsu Team read have distinct visible states, and a failed read disables User Reviewer selection.
- [ ] Team membership is fetched afresh on each page render; removing a person from Kitsu removes them from the next rendered Team without local cleanup.
- [ ] Reviewer uses a stable Kitsu Task Type ID and stays concise: Task Type selector, automatic Reviewer name/reason, and an `Overrides` list or `None`; redundant implementation explanations are absent.
- [ ] Automatic Reviewer view distinguishes eligible Department Supervisors in the Production team and their linked/unlinked state.
- [ ] Explicit Reviewer overrides support multiple linked Discord Users and mentionable guild Roles, individual removal, and reset without mixing notification routing; reset preserves legacy Production mappings and returns to them when present.
- [ ] Role choices exclude `@everyone` and non-mentionable roles; stored target IDs and outgoing allowed-mention lists are exact and capped at 20.
- [ ] Empty states explain the next action when the Kitsu Team or linked Reviewer candidates are empty.
- [ ] JP and EN have equivalent structure, no unintended language leakage, and no page overflow.

## System Status — `/bot/admin/health`

- [ ] The page begins with API response status after the page title, followed by KitsuSync operational status and a Recent issues section only when issues exist; no redundant page-level Overall/System summary is rendered.
- [ ] Kitsu API and Discord API appear as separate cards; no duplicate API status presentation exists.
- [ ] Kitsu and Discord API cards have equal peer widths/heights and equal graph plot regions.
- [ ] Graph outer containers and plotting regions have equal widths and heights.
- [ ] Graph x positions use observation timestamps; sparse observations do not stretch to fill the sample count.
- [ ] Both graphs use independent zero-based stepped ceilings, exactly three readable Y ticks outside the plot, shared timestamp positioning, metadata slots, and fixed 60s/5m geometry.
- [ ] Each service plot uses x=34 through x=464 in the 466×104 viewBox, with midpoint x=233; the Y tick column is outside the plot and the browser-measured right gap is at most 6px.
- [ ] Browser measurement, not viewBox ratio alone, proves the rendered baseline/grid leaves at most 6px on each side of the SVG and the graph surface has no unnecessary side padding or Y-axis gutter.
- [ ] Computed System Status typography uses the compact operational hierarchy: 28px page title, 20px major titles, 16px card titles, 24px response values, 14px body/helper, 13px metadata, and 12px chart labels.
- [ ] Exact chart labels are language-independent `60s`, `30s`, `0s` and `5m`, `2.5m`, `0s`.
- [ ] Both graphs show exactly three Y ticks at the same positions: maximum, midpoint, and 0, with an optional subtle midpoint guide.
- [ ] Both graphs use the same x-label positions: 60s uses `60s`, `30s`, `0s`; 5m uses `5m`, `2.5m`, `0s`.
- [ ] Kitsu and Discord each use independent zero-based Y scales so low Kitsu latency remains visibly readable; exact current values remain the cross-service comparison.
- [ ] The current response-time value is visually primary and readable above the graph.
- [ ] The 60s / 5m selector changes the visible window without full-page reload or URL navigation.
- [ ] API graphs use chronological bars, green for success, red for failure, and explain response time in ms.
- [ ] Kitsu and Discord receive real read-only observations; unavailable data is not fabricated.
- [ ] The auto-refresh indicator remains visible while snapshot updates occur without overlapping requests.
- [ ] A transient refresh failure is visibly recoverable on the next refresh without a full-page reload.
- [ ] Expandable operational details work and contain materially useful, secret-safe data.
- [ ] Recent system issues are omitted when there are no issues.
- [ ] A non-ready readiness state exposes one compact section-level next action: Kitsu setup → Connection settings, Discord Bot setup → Discord Bot settings, Production required → New Production Connection, and Routing required → notification settings/Productions. Readiness actions are not repeated on every operational row.
- [ ] Event runtime failures may expose one independent safe Recent issues/diagnostics action; waiting for a first observation has concise guidance without a misleading action.
- [ ] JP and EN status labels and explanatory text are semantically equivalent.
- [ ] Console errors/warnings are zero, major GET requests succeed, and page-level horizontal overflow is absent.
## Final System Status observability checks

- [ ] Each API card shows the current value, health badge, and one local `Last updated` line only; no normal-card `15 / 20` count or duplicate selected-window sentence is visible.
- [ ] Every real bar has a native tooltip and keyboard-reachable accessible name containing only its local timestamp, measured duration for success, and the localized success/failure status.
- [ ] Failure tooltips say `Request failed` / the Japanese equivalent and do not fabricate a duration.
- [ ] API snapshot timestamps are UTC RFC3339 and displayed in the viewer's IANA timezone; changing language does not change the timezone, and Audit Log times show the timezone context.
- [ ] Chart labels are exactly `60s / 30s / 0s` and `5m / 2.5m / 0s` in both JP and EN.
- [ ] The browser confirms the bars remain timestamp-positioned, full-width, zero-based, independently scaled, orthogonal, and auto-refreshed without a page reload.
- [ ] No telemetry tooltip, HTML attribute, log, or API response exposes credentials, authorization headers, response bodies, URLs containing secrets, or internal IDs.

## Current Production detail integration checks

- [ ] Overview shows one current-issues representation, not both a count label and a healthy-value label.
- [ ] Default Notifications is read-only and shows `Kitsu Task Type → Discord Channel`; only explicit `Edit` exposes routing controls.
- [ ] Production Users reflects current Kitsu Production Team membership without a local membership write.
- [ ] A globally linked Team member is selectable for Reviewer / Checker without a separate Production association; a linked non-Team user is not offered.
- [ ] Existing `ProjectUserMap` membership rows remain untouched, and bot identities never appear as human Reviewer candidates.
