# Production Detail Kitsu UI Baseline Adoption Plan

**Scope:** PR #229 Production detail visual adoption only  
**Planning basis:** PR head `4c307aa6f5ddafa712c4afd0ec9443ab4856e3c6`  
**Reusable visual authority:** `docs/superpowers/specs/2026-09-29-kitsu-ui-baseline-design.md`  
**Screen semantics authority:** `docs/CURRENT-IA-UI-SPEC.md`

## Goal and guardrails

Apply the approved Kitsu-derived visual baseline to the Production detail screen only. Keep all screen content, source-of-truth rules, routes, authorization, persistence, mutations, and notification behavior as currently specified and implemented.

Do not alter Login or authenticated background rendering, Dashboard, Production list, shared connection semantics, backend behavior, database schema, WFA delivery, or Production deployment/release behavior. Do not add arbitrary visual tokens. Reuse existing style values where available; any shared-token normalization requires a separate explicit decision.

The baseline governs reusable presentation. `CURRENT-IA-UI-SPEC.md` remains authoritative for screen semantics, content, routes, state meanings, and behavior. Do not migrate that spec during this work. Page-specific deviations from the baseline require a concrete Production-detail need and must not change semantics.

## Existing contracts to protect

Before implementation, keep these behaviors visible in the test plan:

- Four primary tabs only: Overview, Notifications, Team, Settings.
- Legacy tab aliases and focus/open behavior remain compatible.
- Production identity and audit activity remain scoped to the exact Production ID.
- Overview omits empty activity and does not invent metrics or readiness.
- Notifications uses real `Kitsu Task Type → Discord Channel` routing; Edit/Apply/Cancel and ordering remain intact.
- WFA Automatic and Additional recipients stay under Notifications; no Notification Preview returns.
- Team remains a live, read-only Kitsu Team view. Effective role, resolvable Department, Supervisor scope, and global Discord linking follow the current IA contract. Do not infer task assignments or add membership mutation.
- Settings preserves Storage save behavior, identifier copy actions, diagnostics, and Danger Zone confirmations.
- External Kitsu URL Save/Check-link/reload/fallback semantics remain unchanged.
- Existing JP/EN meaning and state parity remains intact.

## Regression-first implementation tasks

### 1. Lock visual acceptance expectations and baseline behavior tests

Start by extending focused tests before changing markup or CSS. Run the relevant tests once to establish the current result, then add assertions that fail only for the requested baseline mismatch.

- In `production_detail_ia_test.go` and `ia_views_test.go`, verify four visible primary tabs and order, correct panel/section structure, no primary Reviewers tab, and no Notification Preview.
- Retain and extend legacy alias/focus tests rather than replacing them.
- Add narrowly scoped structural assertions for the intended section hierarchy: Overview and Notifications use compact sections; Team has one repeated-row/table-list structure rather than a per-person card collection; Settings sections retain their order.
- Do not encode status meanings in this reusable baseline test. Existing IA semantics remain the authority.
- In `reviewer-browser-acceptance_test.go` and `tools/reviewer-browser-acceptance/browser.cjs`, add browser assertions for the visible visual contract. Prefer relationships and computed styles over brittle exact pixel snapshots.
- Ensure assertions cover the semantic DOM/content and are useful if class names change; avoid tests that merely duplicate the same assertion in Go and JavaScript.

Expected test additions should verify the requested visual result without asserting new backend behavior. Keep tests for save, copy, disclosures, routing, WFA targets, role/team data, and exact-Production audit scoping as behavioral regression guards.

### 2. Refine the Production header and tabs

In `ia_views.go`, render a compact Production eyebrow, name, and existing connection badge in a single identity header. Keep badge text/state tied to the existing canonical status.

Render exactly Overview / Notifications / Team / Settings as plain label links on one shared hairline. The active tab uses an underline on that line; remove pill, filled, or individually bordered tab treatment. Preserve route query parameters, selected-tab semantics, JP/EN labels, and legacy tab mappings.

In `ui.go`, adjust only Production-detail selectors. Reuse existing color, spacing, border, and typography values; do not affect shared navigation or other pages.

### 3. Reshape Overview and Notifications into compact sections

Overview:
- Replace the summary-card/grid composition with compact operator-console sections and meaningful dividers.
- Keep truthful current connection/resource status and issues.
- Render recent activity only when exact-Production audit records exist.
- Preserve empty-state omission and exact Production scoping.

Notifications:
- Present Routing and WFA recipients as distinct structured sections.
- Make the read-only routing relationship immediately legible as Kitsu Task Type → Discord Channel.
- Keep the single explicit Edit entry point and the existing edit form's add/remove, ordering, channel selection, Apply, and Cancel behavior.
- Keep WFA Task Type, Automatic, and Additional recipients controls and their existing mutation/delivery semantics.
- Keep Notification Preview absent.

Use sections and separators by default. Do not add metric tiles, nested card stacks, new destination links, or new explanatory copy that duplicates the IA.

### 4. Convert Team to a compact Kitsu-style table/list

Present repeated Team members in a compact read-only table/list pattern. Keep identity first, followed by effective Kitsu role, resolvable Department, truthfully derived Supervisor scope, and Discord linking state/action.

- Use a table where the fields compare consistently; use a labeled stacked row layout at narrow widths when needed.
- Keep row separators and actions aligned.
- Continue omitting unknown metadata instead of guessing.
- Preserve the live Kitsu Team source, current-human filtering, role resolution, Department-to-Task-Type scope derivation, and global User Linking contract.
- Do not infer Artist task assignment or add team/role/Department mutation controls.

### 5. Organize Settings as vertical settings sections

Use a single aligned vertical structure for Storage, Technical details, Diagnostics, and Danger Zone, in that order.

Preserve existing disclosure defaults and targets, field values, explicit save semantics, copy feedback, diagnostic behavior, and destructive confirmation safeguards. Keep essential status and actions visible; disclosures remain for secondary detail. Avoid turning each section into a dashboard card.

### 6. Mobile, accessibility, and browser visual acceptance

After focused tests pass, inspect the rendered candidate and refine only Production-detail deviations. Use the repository-supported Chromium/Playwright acceptance path; do not use GUI automation outside that path.

Run and capture JP desktop, EN desktop, JP mobile, and EN mobile. At minimum verify:

- exactly four tabs in the required order; plain labels, shared hairline, active underline
- compact Production eyebrow/name and existing semantic connection badge
- Overview sections/dividers, truthful status/issues, and real exact-Production activity only
- Routing and WFA-recipient hierarchy; edit mode retains existing controls; no Preview
- compact Team rows, correct information hierarchy, no membership controls or inferred task assignments
- vertical Settings order and preserved disclosure/save/copy/destructive interactions
- JP/EN content and state parity
- no page-level horizontal overflow, clipped text, overlap, mojibake, or console/runtime errors
- keyboard-visible focus, semantic tab selection, usable touch targets, and reduced-motion compatibility
- unchanged Login fabric and authenticated static dot-grid appearance when navigating to/from Production detail

Use browser screenshots as the visual acceptance evidence. Do not accept the UI based only on DOM tests or CI screenshots if the approved Staging browser review is part of the candidate gate.

### 7. Final validation and isolated Staging review

Only after browser visual acceptance passes, run once:

- `go test ./src/... -count=1 -timeout=120s`
- `go vet ./src/...`
- `docker compose config -q`
- `git diff --check`

Then push the exact PR head and require exact-head CI and Security Audit PASS. Deploy that exact candidate only to isolated Staging through the established supported path. Confirm the candidate SHA and Staging health/readiness, then repeat the JP/EN desktop/mobile Production-detail browser checks on that exact Staging SHA.

Production deployment and release are out of scope. Stop with PR #229 unmerged; keep its Draft/review state under the separate review decision.

## Expected implementation files

- `src/setup/ia_views.go` — Production detail structure and presentation markup.
- `src/setup/ui.go` — scoped Production detail styles using existing tokens/values.
- `src/setup/production_detail_ia_test.go` — focused rendered structure and route compatibility assertions.
- `src/setup/ia_views_test.go` — focused IA content/state regression assertions where they belong.
- `src/setup/reviewer-browser-acceptance_test.go` — browser test harness assertions only if needed.
- `tools/reviewer-browser-acceptance/browser.cjs` — rendered visual contract checks and JP/EN viewport acceptance.
- `docs/CURRENT-IA-UI-ACCEPTANCE.md` — update only if a visual acceptance criterion is missing; preserve its existing semantic requirements.
- `docs/CURRENT-IA-UI-SPEC.md` — no planned change; it remains the semantic authority.

Avoid changing a listed file if the existing coverage already proves the relevant condition. Do not change backend handlers, schemas, routing, delivery, credentials, or deployment configuration.

## Completion gate

The plan is complete when implementation changes only the Production detail visual presentation and directly related acceptance coverage, all protected behaviors remain covered, browser acceptance passes in JP/EN desktop/mobile, final tests and exact-head CI/Security pass, and the exact candidate is verified on isolated Staging. No Production deployment or release occurs.
