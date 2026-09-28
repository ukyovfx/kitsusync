# Current IA UI Acceptance Checklist

## Reusable authenticated browser gate

The existing CI workflow runs a dedicated `reviewer-browser-acceptance` job for pull requests to `master`, checking out the exact PR head. It launches the normal KitsuSync login and admin handlers against a temporary SQLite database, a loopback-only synthetic Kitsu service, and an intercepted synthetic Discord API. Chromium logs in through `/bot/login`; there is no test authentication bypass and no real Kitsu/Discord credential or outbound service call. The same isolated browser gate also checks the login fabric and authenticated-app static dot grid at 1440×900, 1920×1080, and 390×844, including both languages, pointer activation, and reduced motion.

The job records Japanese and English Production Users/Reviewer, User Linking, and System Status screens at 1440×1000 and 390×844. It checks the live-team Reviewer eligibility and display-name cases, additive User/Role overrides and filtering, empty/error states, layout overflow, mojibake, and browser console errors. It saves synthetic screenshots and a non-secret state summary as a short-retention Actions artifact. A companion synthetic WFA delivery test verifies recipient union/deduplication, exact allowed mentions, zero-Reviewer card delivery, and fail-closed lookup behavior.

This CI evidence validates application behavior with synthetic services only. It does not establish deployed runtime identity, production credential validity, live Kitsu data, or real Discord delivery; those remain separate staging/runtime evidence.

## Login and app backgrounds

- [ ] `/bot/login` retains a horizontally centered card. Desktop uses the prototype Gold Standard baseline (26 strands, 7px sampling, 180px sheet width, canonical 10-tier palette, 36,000-dot tier capacity) with separate side-specific waves and continuous `streamDist` flow. Phone widths hide the Login fabric canvas and show no waves.
- [ ] The login fabric has broad slow folds, fine flutter, a visible center taper, depth-aware size/brightness, and the prototype's burnt-orange → Kitsu-orange → amber palette; it has no vortex, rings, network lines, repulsion, bloom, or heavy glow. Intentional left/right silhouette differences are preserved; no symmetric-density ratio is required.
- [ ] Browser pixel sampling proves mid/bright orange tiers are materially present, and samples immediately outside both desktop card edges show the cloth reaches the card without a visible moat. The card's foreground layer naturally occludes the cloth beneath it.
- [ ] Pointer gust uses projected 2D cloth proximity: a pointer at the cloth increases local wave/flutter/brightness, while the same X position far above or below the cloth does not trigger it. The gust smoothly recovers after leaving and never repels particles. On mobile, pointer activation remains subtle and does not disturb the centered card.
- [ ] Across separated desktop frames, longitudinal stream distance and particle distribution advance; changing wave phase alone does not satisfy this check. Reduced motion holds a stable composed baseline.
- [ ] Normal authenticated app pages use a static, viewport-level regular dot grid, with fixed positions and low-contrast warm-orange color. It does not depend on content-card bounds and remains pixel-stable over time and consistent across Dashboard, Production list, User Linking, and System Status.
- [ ] The authenticated grid has no global animation, waves, silk/fabric, drift, flowing noise/mist, recentering, repulsion, or particle translation. Pointer proximity changes only nearby dot brightness and size with smooth local falloff; reduced motion retains the static baseline and may disable pointer activation.
- [ ] Login and app canvases remain behind the UI and do not alter Current IA layout or content. Login card and form panel are opaque while login cloth geometry continues underneath them without a particle moat.
- [ ] Login fabric respects `prefers-reduced-motion: reduce`; the authenticated dot grid already has no global animation.
- [ ] Browser acceptance covers JP and EN at 1440×900, 1920×1080, and 390×844, with no horizontal overflow, mojibake, or console errors. The Login fabric is absent at 390×844; authenticated pages retain their static reactive dot grid at all three viewport sizes.

Use the repo-supported authenticated preview/browser workflow. Browser-rendered output is the final acceptance evidence. Do not use Production 8090 or submit write-producing forms during this smoke check.

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
- [ ] Normal edit mode shows the resolved Kitsu host as a quiet read-only value, says it was detected/configured automatically, and offers compact `Change manually` / `Use automatic endpoint` controls without a page reload.
- [ ] Manual endpoint mode preserves existing validation and persistence behavior and does not submit a display placeholder as the host.
- [ ] External Kitsu URL is the only normal Advanced setting; its Check link control stays compact beside the field and wraps cleanly on mobile.
- [ ] Internal Kitsu URL and API Base URL appear only in one expert disclosure after manual endpoint mode, except that a saved API Base URL keeps the disclosure visible/open for editing; saved values are never cleared implicitly.
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
- [ ] Only Overview, Notifications, Reviewers, and Settings appear as primary Production sections; the identity header is compact and has no redundant “Selected Production” copy.
- [ ] Overview uses a compact operational status list and Current issues section; recent activity shows at most five exact-Production audit records and is omitted when none exist.
- [ ] Notifications read mode shows an explicit Kitsu Task Type / Discord Channel column structure and one Edit action; the existing explicit edit mode preserves add, remove, ordering, channel selection, Apply, and Cancel.
- [ ] The read-only preview selector follows configured Task Type routes and identifies each destination, Production notification language, and WFA Reviewer mention policy; its clearly marked example uses the current Discord card renderer without real task/recipient data, webhook/channel IDs, or a send control.
- [ ] Production Users reads the live Kitsu Production Team, shows linked/unlinked global User Linking state, distinguishes read failure from an empty team, and excludes bots.
- [ ] Reviewers keeps Automatic and additive User/Role Overrides unchanged; the collapsed eligibility disclosure is secondary and has no membership editing controls.
- [ ] Settings contains Storage, Technical details, Diagnostics, and Danger Zone in that order, with ordinary dividers and no floating-card grid.
- [ ] Storage preserves its existing form and feedback behavior; Save begins disabled and enables only after the link changes.
- [ ] Technical details, Diagnostics, and Danger Zone start collapsed; their legacy direct links map to Settings and open the expected disclosure.
- [ ] Technical IDs remain read-only and localized; no credentials or secrets appear.
- [ ] Diagnostics is a compact vertical list; Danger Zone remains separated at the bottom and keeps both destructive confirmation safeguards.
- [ ] Legacy `users` / `user-settings` routes map to Reviewers; Storage, Activity, Troubleshooting, Details, and Danger Zone routes map to their intended new section and focus/scroll to the destination.
- [ ] Disclosure summaries are indented one level and contents another on desktop and mobile, including System Status processing diagnostics.
- [ ] JP/EN and desktop/mobile have no clipped controls, horizontal overflow, mojibake, or console/runtime errors.

## User Linking — `/bot/admin/users`

- [ ] The page describes and renders human Kitsu-to-Discord linking.
- [ ] Missing Kitsu or Discord Bot configuration shows one concise setup cause and one Connection settings action only; no server selector, mapping table, failure copy, or diagnostic disclosure appears.
- [ ] A genuine Kitsu lookup failure is distinct from a successful zero-user result; neither state renders blocked Discord mapping controls.
- [ ] A genuine Discord lookup failure is distinct from zero joined servers and from a selected server with zero selectable human members.
- [ ] The Discord server selector appears only when joined guild data is meaningful; multiple joined servers wait for an explicit selection before mapping rows appear, with a compact prompt and no empty-state divider.
- [ ] Ready state shows exactly four mapping columns: Kitsu user, Discord user, state, and action. Real display names are used and raw Discord IDs are not visible as identity text. The selected server context is kept in the selector; exactly one separator leads into the table.
- [ ] Save starts disabled and becomes actionable only after the Discord selection changes; existing mappings show their linked state and Unlink action, and save/unlink persist across refresh.
- [ ] Bot identities are excluded from normal human linking.
- [ ] JP and EN have equivalent states, order, actions, and information density at desktop and mobile widths, with no mojibake or page overflow.

## Production Users Kitsu Team checks

- [ ] Normal Users view shows the live Kitsu Production Team before the Reviewer controls; no manual add/remove membership workflow appears.
- [ ] Each Team row compactly shows name, Discord link state, and effective Kitsu Production role (`project_role` overrides except global admin); Supervisor Department/Task Type display is derived only by matching Person Department IDs to Task Type Department IDs, and missing metadata is omitted safely.
- [ ] Each current Team member resolves through global User Linking by stable Kitsu Person ID first; a clear User Linking action appears for unlinked people.
- [ ] Reviewer User candidates include only globally linked human users in the current Kitsu Production Team who are current members of the linked Discord Guild; unlinked Team members, linked non-Team users, and Guild non-members are not selectable.
- [ ] Page reads do not create/update `ProjectUserMap`; existing legacy rows remain intact but legacy Reviewer rows do not become WFA recipients.
- [ ] A successful empty Team and a failed Kitsu Team read have distinct visible states, and a failed read disables User Reviewer selection.
- [ ] Team membership is fetched afresh on each page render; removing a person from Kitsu removes them from the next rendered Team without local cleanup.
- [ ] Reviewer uses a stable Kitsu Task Type ID and stays concise: Task Type selector, automatic Reviewer name/reason, and an `Overrides` list or `None`; Automatic remains visible when Overrides exist and no “inactive while overridden” text appears.
- [ ] Reviewer is presented and delivered as a targeted Discord WFA recipient, not a Kitsu permission Role.
- [ ] Automatic Reviewer eligibility requires current active human Production Team membership, effective Production role `supervisor` (global admin cannot be overridden), matching Department membership, global User Linking, and current linked-Guild membership; Position does not qualify.
- [ ] Explicit User overrides are revalidated at delivery against live Team membership, global User Linking, and live Guild membership; explicit Role overrides are revalidated against the linked Guild and current mentionability.
- [ ] Automatic Users, explicit Users, and explicit Roles are additively combined and deterministically deduplicated; stale User/Role targets are omitted without broadening recipients.
- [ ] WFA has no fallback to Production Manager, Admin, legacy `ProjectCheckerMap`, global `CheckerMap`, or config Checkers. Legacy rows remain stored. Zero Reviewer or lookup failure still posts the normal WFA card with no targeted Reviewer mention.
- [ ] RETAKE, DONE, assignment notifications, and non-WFA legacy Checker behavior remain unchanged.
- [ ] Role choices exclude `@everyone` and non-mentionable roles; outgoing allowed-mention lists contain only the exact eligible users/roles and are capped at 20.
- [ ] Empty states explain the next action when the Kitsu Team or linked Reviewer candidates are empty.
- [ ] JP and EN have equivalent structure, no unintended language leakage, and no page overflow.

## System Status — `/bot/admin/health`

- [ ] The page begins with API response status after the page title, followed by KitsuSync operational status and a Recent issues section only when issues exist; no redundant page-level Overall/System summary is rendered.
- [ ] Kitsu API and Discord API appear as separate cards; no duplicate API status presentation exists.
- [ ] Kitsu and Discord API cards have equal peer widths/heights and equal graph plot regions.
- [ ] Graph outer containers and plotting regions have equal widths and heights.
- [ ] Graph x positions use observation timestamps; sparse observations do not stretch to fill the sample count.
- [ ] Both graphs are lines over the same fixed rolling 60-second domain, use independent zero-based stepped ceilings, exactly three readable Y ticks, and identical X-axis/plot geometry.
- [ ] Successful observations connect as green line segments; failed observations break the line without bottom-row X markers or latency values.
- [ ] Each service plot baseline uses x=44 through x=452 in the 496×104 viewBox; timestamped data uses x=48 through x=448 for equal 4px marker inset and a symmetric midpoint at x=248. Y-axis labels remain fully visible and browser-measured outer gaps remain balanced.
- [ ] Browser measurement, not viewBox ratio alone, proves the rendered baseline/grid has symmetric left/right margins and the graph surface has no unnecessary side padding or oversized Y-axis gutter.
- [ ] Computed System Status typography uses the compact operational hierarchy: 28px page title, 20px major titles, 16px card titles, 24px response values, 14px body/helper, 13px metadata, and 12px chart labels.
- [ ] Exact fixed-domain chart labels are JP `60秒`, `30秒`, `今`; EN `60s`, `30s`, `Now`.
- [ ] Both graphs show exactly three Y ticks at the same positions: maximum, midpoint, and 0, with an optional subtle midpoint guide.
- [ ] Both graphs use matching X-label positions for the fixed rolling 60-second domain and shared current time.
- [ ] Kitsu and Discord each use independent zero-based Y scales so low Kitsu latency remains visibly readable; exact current values remain the cross-service comparison.
- [ ] The current response-time value is visually primary and readable above the graph.
- [ ] The graph has no time-window selector and keeps a rolling 60-second X domain in initial render and refresh.
- [ ] API graphs use chronological green success lines; failed samples and long gaps break the line without red failure marks or fabricated latency.
- [ ] Kitsu and Discord receive real read-only observations; unavailable data is not fabricated.
- [ ] The auto-refresh indicator remains visible while snapshot updates occur without overlapping requests.
- [ ] A transient refresh failure is visibly recoverable on the next refresh without a full-page reload.
- [ ] Useful processing-row diagnostics appear in compact disclosures beneath the left-side summary; status badges and primary actions remain outside disclosures.
- [ ] The response value or failure state sits directly below the API title; `Last updated` is right-aligned on the same row as secondary metadata.
- [ ] Status badges and real actions align in a right-side rail; `New Production Connection` does not float in the content center.
- [ ] Kitsu/Discord issues link to `/bot/admin/bot`; no Production links to `/bot/setup`; routing links to `/bot/admin/projects` or the affected Production `?tab=notifications`; event/runtime failures link to `/bot/admin/audit`.
- [ ] Healthy rows and internal-data rows have no unnecessary action; no diagnostic anchor actions or placeholder buttons appear.
- [ ] Recent system issues are omitted when there are no issues.
- [ ] A non-ready readiness state exposes one compact section-level next action: Kitsu setup → Connection settings, Discord Bot setup → Discord Bot settings, Production required → New Production Connection, and Routing required → notification settings/Productions. Readiness actions are not repeated on every operational row.
- [ ] Event/runtime failures link to Audit Log only when an actual failure is recorded; waiting for a first observation has concise guidance without a misleading action.
- [ ] JP and EN status labels and explanatory text are semantically equivalent.
- [ ] Console errors/warnings are zero, major GET requests succeed, and page-level horizontal overflow is absent.
## Final System Status observability checks

- [ ] Each API card shows the current value, health badge, and one local `Last updated` line only; no normal-card `15 / 20` count or duplicate selected-window sentence is visible.
- [ ] Every successful line point has a native tooltip and keyboard-reachable accessible name containing only its local timestamp, measured duration, and localized success status.
- [ ] Failed observations break the graph line and never fabricate a duration or add X marks along the graph baseline; the main API response state says `Request failed` / the Japanese equivalent when the latest observation failed.
- [ ] API snapshot timestamps are UTC RFC3339 and displayed in the viewer's IANA timezone; changing language does not change the timezone, and Audit Log times show the timezone context.
- [ ] Chart labels use the fixed-domain JP/EN values for `60s`, `30s`, and `Now` in both initial render and refresh.
- [ ] The browser confirms the line graph remains timestamp-positioned, full-width, zero-based, independently scaled, orthogonal, and auto-refreshed without a page reload.
- [ ] Kitsu and Discord probes share one monitoring-cycle timestamp and are started concurrently.
- [ ] A sample gap longer than 30 seconds breaks the line; failed observations never receive latency values; observations older than 45 seconds are labeled stale.
- [ ] The immediately preceding pre-window sample continues the line at the left boundary only when the adjacent interval is within 30 seconds, without rendering an out-of-window point.
- [ ] No telemetry tooltip, HTML attribute, log, or API response exposes credentials, authorization headers, response bodies, URLs containing secrets, or internal IDs.

## Current Production detail integration checks

- [ ] Overview shows one current-issues representation, not both a count label and a healthy-value label.
- [ ] Default Notifications is read-only and shows `Kitsu Task Type → Discord Channel`; only explicit `Edit` exposes routing controls.
- [ ] Production Users reflects current Kitsu Production Team membership without a local membership write.
- [ ] A globally linked Team member who is also a current linked-Guild member is selectable for Reviewer without a separate Production association; a linked non-Team or Guild non-member is not offered.
- [ ] Existing `ProjectUserMap` membership rows remain untouched, and bot identities never appear as human Reviewer candidates.
