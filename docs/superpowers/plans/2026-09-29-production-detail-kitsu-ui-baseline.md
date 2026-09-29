# Production Detail Kitsu UI Baseline Adoption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

## Goal

Apply the approved reusable KitsuSync UI baseline to the Production detail screen in PR #229 only. Keep screen semantics, routes, data sources, auth, persistence, mutations, WFA delivery, and all non-target pages unchanged.

## Architecture

- Reusable visual rules come from the approved Kitsu UI baseline.
- `docs/CURRENT-IA-UI-SPEC.md` remains authoritative for screen semantics, content, state vocabulary/meaning, routes, and behavior.
- The Production detail renderer and scoped styles are the implementation surface; existing handlers and data contracts remain intact.
- Focused Go and Playwright/Chromium acceptance protect behavior and visual structure. Actual authenticated Staging rendering is the final deployed visual check.
- Adoption is incremental and scoped to Production detail; no shared-shell or whole-app restyle.

## Tech Stack

Go server-rendered HTML, embedded/shared CSS in `src/setup/ui.go`, Go `httptest` and setup package tests, repository-supported Playwright/Chromium acceptance in `tools/reviewer-browser-acceptance/browser.cjs`, GitHub Actions CI/Security, and the established isolated Staging routine deployment path.

## Spec

- Approved reusable design: `docs/superpowers/specs/2026-09-29-kitsu-ui-baseline-design.md`
- Screen semantics and behavior: `docs/CURRENT-IA-UI-SPEC.md`
- Browser checklist: `docs/CURRENT-IA-UI-ACCEPTANCE.md`
- Target: Production detail only, at PR #229 head `16aa38af765e90c18f743a9702aa084eea88c208` when this plan was prepared.

## Global Constraints

- Visual adoption only; no backend semantic redesign, schema/API changes, or notification recipient/delivery changes.
- Keep exactly four tabs: Overview, Notifications, Team, Settings. Preserve all legacy route aliases and focus/open behavior.
- Preserve exact-Production audit scoping, live Kitsu Team semantics, role/Department/Supervisor derivation, read-only membership, and global User Linking.
- Preserve WFA routing, Automatic and Additional recipients, all User/Role override mutations, mention safety, and delivery behavior.
- Preserve External Kitsu URL Save, Check-link no-save, reload, failed-save, and clear/fallback behavior.
- Dashboard and Production list remain unchanged.
- Login fabric and authenticated static dot-grid remain unchanged.
- Reuse existing KitsuSync visual values where present. Do not invent one-off tokens or normalize shared tokens in this change.
- Orange is limited to brand/active/primary emphasis. Status vocabulary and semantic colors remain owned by `CURRENT-IA-UI-SPEC.md`.
- Production deployment and release are out of scope. Staging means isolated 8091 only.
- Do not use RDC, Computer Use, or GUI automation.

## Review Focus

Each item is mapped to its owning task/check:

- Long Japanese and English tab, section, and action labels at mobile width — Task 6 browser acceptance.
- Team with several members and long Department/Supervisor text — Task 4 fixture/regression test and Task 6 mobile browser check.
- Successfully empty Team versus failed Team read — Task 4 Go/browser regression checks.
- Notifications read state and explicit edit state — Task 3 Go/browser regression checks.
- Legacy deep links focusing or opening Storage, Diagnostics, Technical details, and Danger Zone — Task 5 existing alias tests plus Task 6 browser route checks.

## Task 1: Production-detail visual acceptance contract

**Files:** `docs/CURRENT-IA-UI-ACCEPTANCE.md`, `src/setup/production_detail_ia_test.go`, `src/setup/ia_views_test.go`, and only if coverage is missing `src/setup/reviewer_browser_acceptance_test.go` / `tools/reviewer-browser-acceptance/browser.cjs`.

- [x] Read the existing Production-detail acceptance section and tests; identify only missing visual checks. Keep the semantic requirements unchanged.
- [x] Add a concise acceptance checklist for the baseline: compact entity header; exactly four label tabs on a shared hairline/underline; sections over summary-card grids; compact Team rows; vertical Settings; JP/EN desktop/mobile; no overflow, mojibake, or console errors.
- [x] Retain focused semantic guard assertions for four-tab order, legacy routes, real routing/WFA placement, read-only Team, Settings order, and scoped activity. Existing tests already prove the contract, so no redundant test was added.
- [x] Run the focused baseline tests and confirm PASS before any UI changes: the Windows attempt was blocked by CGO-disabled SQLite; exact-head Linux/CGO CI #691 passed `go test ./src/...`, including these named tests.
  `go test ./src/setup -run 'TestProductionDetailUsesFourSectionsAndMapsLegacyTabs|TestProductionTabsHaveEquivalentJapaneseLabels|TestProductionNotificationsHasWFARecipientsAndNoPreview|TestProductionTeamUsesGuildDisplayNameAndFallsBackToUserLinking|TestProductionDiagnosticsAuditCountsAreProductionScoped' -count=1`
- [x] Run `git diff --check -- docs/CURRENT-IA-UI-ACCEPTANCE.md src/setup/production_detail_ia_test.go src/setup/ia_views_test.go src/setup/reviewer_browser_acceptance_test.go tools/reviewer-browser-acceptance/browser.cjs`.
- [x] Commit this acceptance-contract boundary as `test: define Production detail baseline acceptance` (`ad010b72`).

## Task 2: Production identity header + Kitsu hairline tabs

**Files:** `src/setup/ia_views.go`, `src/setup/ui.go`, `src/setup/production_detail_ia_test.go`, `src/setup/ia_views_test.go`.

- [ ] Add a focused regression test named `TestProductionHeaderAndKitsuTabs` for the compact Production eyebrow/name/status grouping and four plain-label tabs with shared hairline and active underline.
- [ ] Run the focused test before changing markup/styles and confirm it fails for the intended visual mismatch only:  
  `go test ./src/setup -run '^TestProductionHeaderAndKitsuTabs$' -count=1`
- [ ] Make the minimal scoped markup/CSS change. Reuse existing status data, colors, and style values; do not change routes, status meaning, page shell, or other pages.
- [ ] Run the focused test and the existing tab/alias tests; confirm PASS:  
  `go test ./src/setup -run 'TestProduction(HeaderAndKitsuTabs|DetailUsesFourSectionsAndMapsLegacyTabs|TabsHaveEquivalentJapaneseLabels)$' -count=1`
- [ ] Run `git diff --check -- src/setup/ia_views.go src/setup/ui.go src/setup/production_detail_ia_test.go src/setup/ia_views_test.go`.
- [ ] Commit as `ui: apply Kitsu identity header and tab treatment`.

## Task 3: Overview + Notifications section hierarchy

**Files:** `src/setup/ia_views.go`, `src/setup/ui.go`, `src/setup/production_detail_ia_test.go`, `src/setup/ia_views_test.go`, and `tools/reviewer-browser-acceptance/browser.cjs` only if a rendered check is missing.

- [ ] Add focused regression coverage named `TestProductionOverviewAndNotificationsSectionHierarchy`. Assert compact Overview sections and distinct Routing/WFA sections without changing their content contract.
- [ ] Run it before implementation and confirm the expected hierarchy assertion fails:  
  `go test ./src/setup -run '^TestProductionOverviewAndNotificationsSectionHierarchy$' -count=1`
- [ ] Replace only the Production detail summary-card grouping with compact sections/dividers. Keep current issue/activity truth and exact Production scoping.
- [ ] Structure Routing as Kitsu Task Type → Discord Channel and WFA recipients as a separate section. Preserve read/edit states and all existing Edit/Apply/Cancel and recipient operations.
- [ ] Run focused regression tests and confirm PASS:  
  `go test ./src/setup -run 'TestProduction(OverviewAndNotificationsSectionHierarchy|NotificationsHasWFARecipientsAndNoPreview|OverviewOmitsActivityWhenNoScopedRecordsExist|OverviewDoesNotCallUnconfiguredRoutingHealthy|DiagnosticsAuditCountsAreProductionScoped)$' -count=1`
- [ ] Run `git diff --check -- src/setup/ia_views.go src/setup/ui.go src/setup/production_detail_ia_test.go src/setup/ia_views_test.go tools/reviewer-browser-acceptance/browser.cjs`.
- [ ] Commit as `ui: structure Production overview and notifications`.

## Task 4: Team compact Kitsu-style table/list

**Files:** `src/setup/ia_views.go`, `src/setup/ui.go`, `src/setup/production_detail_ia_test.go`, `src/setup/ia_views_test.go`, and `src/setup/reviewer-browser-acceptance_test.go` only if fixture coverage needs extension.

- [ ] Add `TestProductionTeamCompactRows` using multiple members, long Department names, and long Supervisor scope text. Assert readable repeated rows and no per-person large-card structure.
- [ ] Include both a successful empty Team response and a failed Team read in the owning regression/browser checks; assert they remain distinct.
- [ ] Run the focused test before the change and confirm it fails for the intended presentation mismatch:  
  `go test ./src/setup -run 'TestProductionTeam(CompactRows|UsesGuildDisplayNameAndFallsBackToUserLinking)$' -count=1`
- [ ] Make the smallest Team markup/style change to compact aligned table/list rows. Keep unknown metadata omitted, retain current-human/read-only rules, and do not infer assignments or add membership mutation.
- [ ] Run focused Team tests and confirm PASS:  
  `go test ./src/setup -run 'TestProductionTeam(CompactRows|UsesGuildDisplayNameAndFallsBackToUserLinking)$|TestCurrentProductionUsersDistinguishKitsuEmptyAndReadFailure|TestReviewerBrowserAcceptance' -count=1`
- [ ] Run `git diff --check -- src/setup/ia_views.go src/setup/ui.go src/setup/production_detail_ia_test.go src/setup/ia_views_test.go src/setup/reviewer_browser_acceptance_test.go`.
- [ ] Commit as `ui: compact Production Team rows`.

## Task 5: Settings vertical section/disclosure treatment

**Files:** `src/setup/ia_views.go`, `src/setup/ui.go`, `src/setup/production_detail_ia_test.go`, `src/setup/ia_views_test.go`, and `tools/reviewer-browser-acceptance/browser.cjs` only if a browser check is missing.

- [ ] Add `TestProductionSettingsVerticalSections` for Storage, Technical details, Diagnostics, and Danger Zone order and one-column section hierarchy.
- [ ] Extend or retain regression checks that legacy deep links focus/open Storage, Diagnostics, Technical details, and Danger Zone.
- [ ] Run the focused tests before the style change and confirm the intended visual assertion fails:  
  `go test ./src/setup -run 'TestProduction(SettingsVerticalSections|SettingsGroupsExistingSectionsWithoutChangingForms)$|TestSelectedProductionTabNormalizesLegacyDestinations|TestLegacyActivityDeepLinkFallsBackToOverviewWithoutRecords' -count=1`
- [ ] Make the smallest scoped markup/style adjustment. Preserve disclosure defaults, saves, copy actions, diagnostic behavior, and Danger Zone confirmation safeguards.
- [ ] Run focused tests and confirm PASS:  
  `go test ./src/setup -run 'TestProduction(SettingsVerticalSections|SettingsGroupsExistingSectionsWithoutChangingForms)$|TestSelectedProductionTabNormalizesLegacyDestinations|TestLegacyActivityDeepLinkFallsBackToOverviewWithoutRecords|TestReviewerBrowserAcceptance' -count=1`
- [ ] Run `git diff --check -- src/setup/ia_views.go src/setup/ui.go src/setup/production_detail_ia_test.go src/setup/ia_views_test.go tools/reviewer-browser-acceptance/browser.cjs`.
- [ ] Commit as `ui: align Production settings sections`.

## Task 6: JP/EN desktop/mobile browser acceptance and repairs

**Files:** `tools/reviewer-browser-acceptance/browser.cjs`, `src/setup/reviewer-browser-acceptance_test.go`, plus only the Production detail files owning a confirmed visual defect.

- [ ] Run synthetic browser acceptance through the existing repository Chromium/Playwright path:  
  `go test ./src/setup -run '^TestReviewerBrowserAcceptance$' -count=1`
- [ ] Cover JP desktop, EN desktop, JP mobile, and EN mobile. Verify tab hairline/underline, long localized labels, compact Team rows with long Department/Supervisor values, empty versus failed Team states, Notifications read/edit states, Settings vertical hierarchy, and legacy deep links opening/focusing each Settings subsection.
- [ ] Verify no page overflow, clipped controls/text, mojibake, console/runtime errors, or visual regressions to Login/app backgrounds, Dashboard, or Production list.
- [ ] Repair each confirmed in-scope visual failure minimally, rerun its owning focused Go/browser test, and recheck the affected viewport/locale. Do not alter semantics to satisfy a visual assertion.
- [ ] For real authenticated Staging review, do **not** retry the previously failed managed headed-browser process launch (`CreateProcessWithLogonW error 1909`). Use the already-established interactive Chrome + loopback tunnel/CDP path when human authentication is required.
- [ ] Never inspect or print credentials, cookies, tokens, or session values. Do not use RDC, Computer Use, or GUI automation. Browser-rendered output remains the final visual source of truth.
- [ ] Run `git diff --check` after browser-driven repairs and before committing.
- [ ] Commit any bounded repair as `ui: refine Production detail browser acceptance`; if no repair is needed, do not create an empty commit.

## Task 7: Final full validation + exact-head isolated Staging verification

**Files:** no planned new files. If a failure requires source/test edits, return to the owning Task 2–6 regression loop and then repeat the applicable gates.

- [ ] After synthetic visual acceptance is green, run the full local gate once:  
  `go test ./src/... -count=1 -timeout=120s`  
  `go vet ./src/...`  
  `docker compose config -q`  
  `git diff --check`
- [ ] Push the exact PR #229 head. Wait for exact-head CI and Security Audit to report PASS; verify both correspond to the same head SHA.
- [ ] Deploy only that exact SHA to isolated Staging 8091 using the established candidate path:  
  `pwsh -NoProfile -File scripts/deploy-kitsusync-staging-candidate.ps1 -CommitSha <exact-full-sha>`
- [ ] Verify Staging runtime/source SHA, image identity, `/health`, `/ready`, and loopback-only 8091 binding. Confirm Production 8090 is unchanged using read-only evidence.
- [ ] Run authenticated real-Staging Production-detail acceptance in JP/EN desktop/mobile through the established interactive Chrome + loopback tunnel/CDP path. Do not use the failed managed headed-browser launch or inspect/print any credential/session values.
- [ ] If Staging exposes a mismatch, repair only the owning task's files, repeat focused tests and browser acceptance, then rerun full validation, exact-head CI/Security, and exact-SHA Staging deployment.
- [ ] Stop with PR #229 unmerged. Production deployment and release remain out of scope.

## Expected implementation files

- `src/setup/ia_views.go` — Production detail markup only.
- `src/setup/ui.go` — scoped Production detail styles only.
- `src/setup/production_detail_ia_test.go` and `src/setup/ia_views_test.go` — focused render/route/content regressions.
- `src/setup/reviewer-browser-acceptance_test.go` and `tools/reviewer-browser-acceptance/browser.cjs` — synthetic and rendered browser checks as needed.
- `docs/CURRENT-IA-UI-ACCEPTANCE.md` — only if Task 1 finds a missing visual acceptance criterion.
- `docs/CURRENT-IA-UI-SPEC.md` — unchanged; semantic authority.
- This plan file is the only file changed while preparing the plan.

## Completion gate

All seven tasks are checked, all named focused and full gates pass, the exact candidate passes CI and Security Audit and is verified on isolated Staging, and browser-rendered Production detail passes JP/EN desktop/mobile acceptance. PR #229 remains unmerged; no Production deployment or release occurs.
