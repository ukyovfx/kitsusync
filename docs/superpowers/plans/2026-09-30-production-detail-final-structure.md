# Production Detail Final Structure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the final Gemini Production Detail structure in KitsuSync while preserving accepted KitsuSync visual, data, and safety semantics.

**Architecture:** Keep the existing server-rendered Go page and four canonical tabs. Stage notification edits in browser memory and submit routing plus additional WFA-target deltas in one revision-checked POST; use one SQLite transaction for related rows and compensating recovery around the external Discord reorder. Team linking reuses the global UserMap validation/persistence path.

**Tech Stack:** Go `net/http`, server-rendered HTML/CSS/JavaScript, GORM, SQLite, existing Kitsu and Discord clients, repository Chromium/Playwright browser acceptance.

**Spec:** `docs/superpowers/specs/2026-09-30-production-detail-final-structure-design.md`

## Global Constraints

- Gemini prototype owns Production Detail structure and interaction order; `docs/CURRENT-IA-UI-SPEC.md` owns KitsuSync screen semantics.
- Verified structure reference: `C:\AI-Workspace\artifacts\kitsusync-ui-reference\production-detail-final\kitsusync_production_detail_prototype.html` exists. Use that exact file for structure/interaction only; do not substitute another prototype or adopt its styling.
- Existing KitsuSync visual styling, colors, typography, tokens, authenticated dot-grid, and controls remain authoritative.
- Four tabs only: Overview, Notifications, Team, Settings; preserve all legacy route mappings.
- Automatic WFA recipients remain Kitsu-derived, read-only, and never enter the writable Apply payload.
- User Linking remains global `UserMap`; do not add Production-local User mappings or schema.
- Route removal changes KitsuSync routing only, never Kitsu Task Types or Discord channel existence.
- At least one notification route remains required by the existing canonical validator.
- Production deploy/release is out of scope; any runtime acceptance targets isolated Staging only.

## Review Focus

- Long JP labels and narrow mobile width: verify no clipped actions/overflow in the owning UI/browser task.
- Multiple pending route edits with WFA changes on several Task Types: verify all deltas survive row selection and one Apply.
- Stale route/target revision or concurrent SQLite writer: verify the asynchronous Apply receives HTTP 409/transaction conflict without full-page navigation, makes no partial write, and preserves pending browser edits.
- Route removal with a reviewer delta for that same Task Type: client omits the delta; server rejects a crafted conflicting request; existing additional targets are removed atomically with the route.
- Discord reorder request fails after DB commit, including uncertain remote completion: verify both DB snapshot restore and prior-order Discord compensation are attempted and independently report explicit recovery diagnostics if either fails; neither failure returns success.
- Team link selection becomes stale or the same Person is already linked elsewhere: verify shared global validation rejects safely with no project-scoped mapping.
- Empty/failed live Team and no mentionable Discord roles: verify distinct localized fail-closed states.

---

### Task 1: Pin the final four-tab and Overview acceptance contract

**Files:**
- Modify: `src/setup/production_detail_ia_test.go`
- Modify: `src/setup/ia_views_test.go`
- Modify: `docs/CURRENT-IA-UI-SPEC.md`
- Modify: `docs/CURRENT-IA-UI-ACCEPTANCE.md`
- Source under test: `src/setup/ia_views.go`

**Interfaces:** Preserve `renderIASelectedProduction`, `selectedProductionTab`, and `legacyProductionSection`; lock test expectations before changing renderers.

- [ ] Add failing focused tests for four-tab labels, preserved legacy destinations, empty Current Issues omission, issue cap/aggregation/“other N” copy in JP/EN, correct same-window CTA routes, ordinary unlinked Artist exclusion, exact-Production activity newest-first/max-five/no-age-cutoff, exclusion of successful notification sends/routine Task events, delivery-failure inclusion, and empty activity omission.
- [ ] Run `go test ./src/setup -run 'TestProductionDetail|TestProductionOverview|TestLegacyActivity' -count=1` and confirm the new assertions fail for current behavior.
- [ ] Make the minimal Overview renderer changes in `src/setup/ia_views.go`; do not add persistence or unnecessary external reads. If identifying a real WFA-blocking missing link requires it, reuse the existing read-only Team/WFA resolver; an ordinary unlinked Artist is not an issue.
- [ ] Run the focused command again and confirm PASS.
- [ ] Update only the Production Detail clauses in the two Current IA docs to describe the accepted four-tab structure and Overview contract.
- [ ] Run `git diff --check` for the touched scope.
- [ ] Commit as `feat(ui): align production overview with final structure`.

### Task 2: Add compact Notifications view and row-linked WFA panel

**Files:**
- Modify: `src/setup/ia_views.go`
- Modify: `src/setup/ia_views_test.go`
- Modify: `src/setup/production_detail_ia_test.go`
- Read-only fragment endpoint/router in `src/setup/admin.go` or the existing route-registration file only if needed.

**Interfaces:** Keep `renderSelectedProductionNotifications` and `renderCurrentProductionWFARecipients`; add a read-only selected-Task-Type fragment endpoint only if existing SSR cannot switch the panel without losing pending browser state.

- [ ] Add failing tests for the three-column read table, effective WFA summary per stable Task Type ID, no preview, no separate Task Type selector, row selection selecting the matching WFA panel, Automatic read-only semantics, and JP/EN empty/error states.
- [ ] Run `go test ./src/setup -run 'TestProductionNotifications|TestReviewerManager' -count=1` and confirm the intended failures.
- [ ] Implement the compact read renderer and selected-row panel behavior using current routing and WFA resolution functions; if a panel GET is required, keep it read-only and pass only Production/Task Type IDs.
- [ ] Run the focused test command and confirm PASS.
- [ ] Run `git diff --check` for this task's files.
- [ ] Commit as `feat(ui): unify production notification presentation`.

### Task 3: Stage routing rows and additional WFA target edits in browser state

**Files:**
- Modify: `src/setup/current_routing.go`
- Modify: `src/setup/ia_views.go`
- Modify: `src/setup/ui.go` only for scoped Production Detail styles if needed
- Modify: `src/setup/ia_views_test.go`
- Modify: `src/setup/production_detail_ia_test.go`

**Interfaces:** Define the browser Apply request from the spec: `project_id`, `expected_revision`, complete ordered `routes`, and per-Task-Type add/remove User/Role ID deltas. Expose exactly one Apply POST and one Cancel action.

- [ ] Add failing renderer/contract tests for selectable rows, selected-row styling hook, linked WFA panel, compact User/Role add modal, pending route add/channel change/removal/Undo, disabled WFA editing for a pending-removed row, minimum-one-route behavior, client omission of reviewer deltas for pending-removed routes, asynchronous Apply (no full-page form submit), HTTP 409 retaining pending state without reload/navigation, and one global Apply/Cancel with no per-row persistence forms.
- [ ] Run `go test ./src/setup -run 'TestProductionNotifications|TestCurrentIARouting|TestReviewerManager' -count=1` and confirm the pending-state assertions fail.
- [ ] Implement client-side pending state in the existing rendered page script; row changes update the selected WFA panel without discarding pending deltas. Submit Apply with JavaScript `fetch` or equivalent so an HTTP 409 can show an inline stale-edit message while retaining pending state and without reload/navigation. Cancel clears only browser state. Ensure automatic recipient markup is never serialized as editable data and omit `reviewer_changes` for pending-removed Task Types.
- [ ] Run the focused command and confirm PASS.
- [ ] Run `git diff --check` for touched files.
- [ ] Commit as `feat(ui): stage production notification edits`.

### Task 4: Apply notification changes with revision check, one DB transaction, and Discord compensation

**Files:**
- Modify: `src/setup/current_routing.go`
- Modify: `src/setup/ia_views.go` or a focused `src/setup/production_notifications_apply.go`
- Modify: `src/model/model.go` or a focused `src/model/production_notification.go`
- Add/modify: `src/setup/production_notifications_apply_test.go`
- Add/modify: `src/model/production_notification_test.go`

**Interfaces:** Add typed Apply request/delta types and a deterministic revision helper over canonical ordered routes plus sorted additional targets. Handler validates current Kitsu/Discord identities; model transaction atomically replaces route state and applies target deltas/removals.

- [ ] Add failing model/handler tests for route add/change/remove snapshot semantics; one-request User/Role delta application; route removal deleting only same-Task-Type additional targets in the same transaction; reject the whole request when `reviewer_changes` targets a Task Type omitted from submitted `routes`; automatic recipients never writable; invalid Task Type/destination/User/Role rejection; stale revision 409 with no writes; transaction failure with no partial DB state; and one-route minimum.
- [ ] Run `go test ./src/setup ./src/model -run 'Test.*(ProductionNotificationApply|NotificationRevision|RoutingSnapshot|ReviewerDelta)' -count=1` and confirm failures occur at the intended checks.
- [ ] Implement validation and `expected_revision` comparison inside the transaction before writes; return conflict without mutation on stale state. Preserve current webhook ownership, Kitsu membership, Guild membership, mentionable-role, and fail-closed validation.
- [ ] Implement DB snapshot/restore helpers so the config/routes/targets share one transaction; do not add schema or store automatic Supervisor data.
- [ ] Add deterministic tests with Discord function seams for successful external order sync; order failure followed by transactional DB snapshot restore and prior-order compensation; DB restore failure; and Discord prior-order compensation failure. Verify both compensation actions are attempted, each failure is identified in recovery diagnostics, and no failure path returns success.
- [ ] Run the focused tests and confirm PASS.
- [ ] Run `git diff --check`.
- [ ] Commit as `feat(notifications): apply production routing and reviewer edits atomically`.

### Task 5: Reuse global User Linking from the Team modal

**Files:**
- Modify: `src/setup/admin.go`
- Modify: `src/setup/ia_views.go` or add `src/setup/production_team_linking.go`
- Modify: `src/setup/pr199_user_linking_render.go` only if a shared existing helper needs factoring
- Modify: `src/setup/production_detail_ia_test.go`
- Add/modify: `src/setup/production_team_linking_test.go`

**Interfaces:** Factor the existing global link save validation/persistence into one shared helper used by `/bot/admin/users` and the Production Team modal. The modal posts to the same `UserMap` path and returns only to an allowlisted Production Team URL.

- [ ] Add failing tests for modal open/cancel, live Team Person validation, eligible linked-Guild option validation, successful global `UserMap` write, Team/global User Linking readback consistency, stale/non-Team person rejection, Discord conflict rejection, and absence of `ProjectUserMap` writes.
- [ ] Run `go test ./src/setup -run 'TestProductionTeam.*(Link|Modal)|TestGlobalUserLink' -count=1` and confirm failure.
- [ ] Extract/reuse the canonical global User Linking helper; add the in-place modal and safe return path. Keep Kitsu-owned fields read-only and do not accept arbitrary return URLs.
- [ ] Run the focused tests and confirm PASS.
- [ ] Run `git diff --check`.
- [ ] Commit as `feat(ui): link production team members through global user mapping`.

### Task 6: Align Settings structure and run synthetic JP/EN browser acceptance

**Files:**
- Modify: `src/setup/ia_views.go`
- Modify: `src/setup/ui.go` only for Production Detail scoped CSS
- Modify: `src/setup/production_detail_ia_test.go`
- Modify: `src/setup/reviewer_browser_acceptance_test.go` or the existing browser acceptance suite
- Modify: `tools/reviewer-browser-acceptance/browser.cjs`
- Modify: `docs/CURRENT-IA-UI-SPEC.md`
- Modify: `docs/CURRENT-IA-UI-ACCEPTANCE.md`

- [ ] Add failing Settings hierarchy/accessibility tests for Storage → Technical details → Diagnostics → Danger Zone, collapsed-by-default behavior, legacy deep links focusing/opening the correct subsection, safe actions, and no altered form submission.
- [ ] Run `go test ./src/setup -run 'TestProductionSettings|TestProductionDetailUsesFourSections' -count=1` and confirm failure for any missing final structure contract.
- [ ] Make only necessary scoped markup/CSS changes; preserve current KitsuSync styles and ensure controls/disclosures remain usable at 390px.
- [ ] Run focused Go tests and browser script syntax/fixture tests; confirm PASS.
- [ ] Run repository Chromium/Playwright acceptance for JP and EN at desktop 1440×1000 and mobile 390×844: four hairline tabs; Overview issue/activity rules; Notifications read/edit selection, unsaved Apply/Cancel behavior, and a simulated stale Apply response that retains all pending edits without page navigation; Team compact live data and link modal; Settings order/disclosures; no overflow, mojibake, or console/runtime errors; authenticated dot-grid unchanged.
- [ ] Repair only in-scope browser defects, rerun the owning focused check, and repeat browser acceptance until PASS.
- [ ] Update the canonical IA spec and acceptance checklist to remove superseded Production Detail interactions while retaining semantic/API safety requirements.
- [ ] Run `git diff --check`.
- [ ] Commit as `test(ui): accept final production detail structure`.

### Task 7: Final validation and isolated Staging verification

**Files:**
- Verify all scoped files from Tasks 1–6; no new feature scope.

- [ ] Run `gofmt` on changed Go files and verify no formatting diff remains.
- [ ] Run `go test ./src/... -count=1 -timeout=120s`.
- [ ] Run `go vet ./src/...`.
- [ ] Run `docker compose config -q`.
- [ ] Run `git diff --check` and verify the changed-file list contains only Production Detail UI/handler/model/tests/current IA documentation.
- [ ] Push the exact branch head and wait for exact-head Linux CI and Security Audit PASS.
- [ ] Deploy only the exact CI-proven candidate through the established Staging helper to `127.0.0.1:8091`; do not touch Production or release.
- [ ] Verify Staging source SHA, health/readiness, loopback bind, routing/Discord isolation, and Production runtime unchanged.
- [ ] Perform authenticated real-Staging browser acceptance in JP/EN desktop/mobile. If the accepted CDP session requires human login, stop once at that authentication boundary; never inspect credential/session material.
- [ ] Record exact Staging/browser evidence in the PR; keep the PR unmerged and do not mark it ready unless all required visual acceptance passes.

## Handoff

Review this plan before implementation. The implementation steps depend on a shared Apply request contract, transaction/revision boundary, and global mapping helper, so use `superpowers:executing-plans` for a single native execution pass unless the operator requests subagent-driven execution.

## Operator review follow-up (2026-09-30)

The following bounded follow-up was requested after review of the Staging candidate. It preserves the original four-tab and backend semantics except for the explicitly approved pending Discord channel creation at Notifications Apply.

### Task 8: Align Production Detail controls and unconfigured shell

**Files:** `src/setup/ia_views.go`, `src/setup/current_routing.go`, `src/setup/ui.go`, `src/setup/production_detail_ia_test.go`, `src/setup/reviewer_browser_acceptance_test.go`, `tools/reviewer-browser-acceptance/browser.cjs`, and Current IA docs only for the unconfigured shared-shell contract.

- [ ] Add focused renderer/browser regressions for read-mode status/Edit control height, edit spacing/alignment, dark Add recipient dialog and options, row-anchored route actions without overflow, confirmation before staging route removal, compact Settings disclosures, desktop Storage input+Save alignment, and shared configured/unconfigured Production Detail shell.
- [ ] Run the owning focused test/browser assertions and confirm each missing contract fails before changing its implementation.
- [ ] Make the smallest scoped markup/CSS/interaction changes. Route removal remains pending until Apply and never deletes Discord/Kitsu resources; keep the existing separate confirmed Discord-channel deletion behavior distinct.
- [ ] Verify JP/EN desktop/mobile, sparse/rich data, no page overflow, no unintended internal scroll, no mojibake, and no console/runtime errors.
- [ ] Update `docs/CURRENT-IA-UI-SPEC.md` / `docs/CURRENT-IA-UI-ACCEPTANCE.md` only to lock the shared shell for configured and unconfigured Production Detail.
- [ ] Run focused tests and `git diff --check`; commit as `ui: align Production detail controls from staging review`.

### Task 9: Stage new Discord channel creation in Notifications Apply

**Files:** `src/setup/current_routing.go`, `src/setup/production_notifications_apply.go`, `src/model/production_notification_apply.go`, `src/setup/production_notifications_apply_test.go`, `src/model/production_notification_apply_test.go`, and `tools/reviewer-browser-acceptance/browser.cjs`.

- [ ] Add failing model/handler/browser tests for reusing an unassigned existing managed channel, default/custom normalized channel names, pending creation without Discord writes, Cancel/reload creating nothing, Apply creating one channel+webhook and one route, and channel/webhook/persistence failure returning non-success with bounded cleanup diagnostics.
- [ ] Run focused tests and confirm failures are caused by the missing staged-create contract.
- [ ] Add a strict Apply payload for per-route destination choice (`existing` webhook ID or `create` normalized name); reject ambiguous, duplicate, unowned, non-text, wrong-category, invalid-name, and unknown Task Type selections.
- [ ] Keep all new Discord side effects inside final Apply. Create a new text channel and its webhook only after read-only permission/ownership and revision preflight; persist the resulting `ProjectWebhook` and route atomically with reviewer deltas. On any later failure, attempt cleanup of only newly created Discord artifacts and report incomplete recovery without success.
- [ ] Keep existing Discord channels selectable after their routes are removed; route removal itself never deletes a channel or Kitsu Task Type.
- [ ] Run focused model/handler/browser tests and `git diff --check`; commit as `feat(notifications): stage channel creation until apply`.

### Task 10: Final operator-review verification

**Files:** only in-scope files from Tasks 8–9.

- [ ] Run focused Production Detail tests, `go test ./src/... -count=1 -timeout=120s`, `go vet ./src/...`, `docker compose config -q`, browser syntax/acceptance, and `git diff --check`.
- [ ] Capture and visually inspect the requested Notifications, route menu, recipient dialog/dropdown, add-channel choices, Settings, configured/unconfigured, and mobile screenshots.
- [ ] Push the exact PR head and require exact-head CI, Security Audit, and CodeQL (if configured) PASS.
- [ ] Deploy the exact verified head to isolated Staging only; verify runtime identity, health/readiness, 127.0.0.1:8091, and Production unchanged.
- [ ] Stop with PR #230 Draft and unmerged. Do not deploy Production or release.
