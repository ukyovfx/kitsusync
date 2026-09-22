# PR #199 Scope Split Design

## Goal

Replace the mixed-scope PR #199 with three reviewable changes rebuilt from current `master`, preserving only behavior that is still justified by current repository evidence and Current IA rules.

## Verification basis

- Accepted default branch: `master`
- Repository version: `0.4.7`
- PR #199 is Draft and is not accepted implementation state.
- PR #199 currently mixes User Linking readiness/presentation, persisted Kitsu runtime credential/live-data behavior, and Connections token-editing UX.
- Current IA UI changes must follow `docs/CURRENT-IA-UI-SPEC.md` and browser-rendered validation.

## Design principles

- Rebuild from current `master`; do not merge or rebase the mixed PR #199 branch into accepted state.
- Keep runtime semantics, User Linking presentation, and Connections edit UX in separate reviewable scopes.
- Preserve existing setup/auth/DB/API contracts unless a scope explicitly requires a bounded change.
- Keep persisted secrets non-renderable and fail closed when persisted runtime credentials exist but cannot be used safely.
- Distinguish successful empty data from request/auth/network failure.
- Required Status Checks apply independently to every retained scope.
- UI scopes require browser-rendered acceptance after focused tests.

## Scope A — Persisted Kitsu runtime live-data foundation

### Purpose

Make read-only live Kitsu views use the already-persisted validated runtime endpoint/token instead of depending on the legacy `KitsuJWTToken` environment variable, while preserving safe failure outcomes.

### Required behavior

- A decryptable persisted runtime Kitsu token is authoritative for live read-only views.
- If a persisted runtime token exists but cannot be decrypted or resolved safely, fail closed instead of silently falling back to a stale environment token.
- Environment credentials remain a compatibility fallback only when no persisted runtime token exists.
- Kitsu person, project, and project task-type reads accept explicit runtime endpoint/token inputs.
- Empty successful responses remain empty successful responses.
- 4xx, 5xx, invalid responses, and transport failures remain distinguishable failures for callers that need to render safe diagnostics.
- 5xx retry exhaustion must preserve a non-nil error instead of collapsing to an uninformative failure.
- Production-list live lookup failure may show a safe warning while preserving locally saved Productions.
- No credential, Authorization header, response body, or sensitive endpoint detail is exposed to rendered UI or durable logs.

### Expected implementation area

- `src/api/kitsu/kitsu.go`
- `src/api/kitsu/kitsu_test.go`
- `src/setup/runtime_credentials.go`
- `src/setup/runtime_credentials_test.go`
- `src/setup/helpers.go`
- `src/setup/ia_views.go` only where live Production reads consume the runtime source
- `src/setup/ia_views_test.go`
- `src/utils/request/request.go`
- `src/utils/request/request_security_test.go`

### Acceptance

- Persisted-token reads work with `KitsuJWTToken` unset.
- Unreadable persisted credentials fail closed.
- Empty and failure outcomes are distinct.
- Existing verified-origin / pinned-IP / redirect protections remain intact.
- Required tests, vet, Compose validation, CI, and Security Audit pass.

## Scope B — User Linking readiness and failure-state UI

### Dependency

Scope B is based on Scope A so the UI can consume truthful live Kitsu outcomes without re-implementing credential semantics.

### Purpose

Make `/bot/admin/users` render prerequisite, empty, failure, and ready states as distinct user-facing states instead of presenting blocked lookups as empty data or generic API failure.

### Required behavior

- Missing Kitsu and/or Discord Bot configuration is a readiness state, not a diagnostic error.
- Missing configuration shows one concise cause and one Connection settings action.
- Blocked prerequisite state does not render the guild selector, mapping table, diagnostic disclosure, or misleading lookup-failure copy.
- Configured Kitsu lookup failure is distinct from a successful zero-person result.
- Discord directory failure is distinct from zero joined guilds and zero selectable human members.
- The Discord guild selector appears only when guild data is meaningfully available.
- Mapping rows appear only when both Kitsu people and Discord member data are usable.
- Raw Discord IDs are not used as the normal human-facing identity.
- JP and EN have equivalent state, order, actions, and information density.
- User-controlled query values are canonicalized/escaped before affecting rendered output.

### Canonical state model

1. prerequisite missing
2. configured lookup / waiting boundary
3. genuine Kitsu lookup failure
4. genuine Discord lookup failure
5. successful zero guilds / zero people / zero selectable members
6. multiple guilds awaiting selection
7. ready populated mapping table

The exact normal-page copy remains governed by `docs/CURRENT-IA-UI-SPEC.md` and must stay concise.

### Expected implementation area

- `docs/CURRENT-IA-UI-SPEC.md`
- `docs/CURRENT-IA-UI-ACCEPTANCE.md`
- `src/setup/ia_views.go`
- `src/setup/ia_views_test.go`
- `src/setup/ui.go` only for bounded User Linking presentation styles

### Acceptance

- Focused state tests cover every canonical state above.
- JP/EN parity tests pass.
- No secret/raw-ID regression appears in rendered output.
- Authenticated browser review at the Current IA preview verifies the visual state transitions and responsive behavior.
- Required Status Checks pass.

## Scope C — Connections saved-token editing UX

### Independence

Scope C is rebuilt directly from current `master`. It does not depend on Scope A or Scope B.

### Purpose

Keep configured secret fields visible as safe masked controls and make token replacement reversible before submit, without changing token persistence or validation semantics.

### Required behavior

- Saved Kitsu and Discord tokens are never rendered.
- A configured secret is represented by the existing fixed-length mask convention.
- `Change token` enables a fresh empty password field for replacement.
- `Cancel` restores the configured masked state without submitting or revealing the saved value.
- One save/recheck action remains per service.
- Existing server-side save, validation, and secret persistence semantics remain unchanged.
- External Kitsu URL explanatory copy appears once; the compact Check link remains available.
- Existing manual endpoint / expert network controls retain their current behavior.

### Expected implementation area

- `src/setup/admin.go`
- `src/setup/kitsu_connection_test.go`
- `src/setup/ia_views_test.go` only if shared Connections rendering assertions belong there

### Acceptance

- Rendered HTML contains the safe mask and change/cancel controls but not stored secrets.
- Change/cancel interactions are verified in browser-rendered UI.
- Existing endpoint/manual/expert settings behavior is unchanged.
- Required Status Checks pass.

## PR and branch strategy

- Scope A: new branch from current `master`; focused PR.
- Scope B: new branch based on Scope A only after Scope A is stable enough to provide the required runtime interface; rebase/rebuild onto accepted `master` after Scope A merges.
- Scope C: new branch from current `master`; may proceed independently of A/B.
- Do not cherry-pick the full PR #199 history. Re-implement or selectively port only the minimal justified changes per scope.
- Preserve small commits so each scope can be reviewed and reverted independently.

## Legacy PR #199 disposition

Keep PR #199 Draft while replacement work is being created. Its branch remains historical evidence only and must not be merged as-is.

After replacement PRs exist and their scope coverage is verified, close PR #199 with links to the replacement PRs and mark it superseded. Do not delete the branch until the replacement work no longer needs source comparison.

## Non-goals

- No DB schema migration.
- No auth/session redesign.
- No setup-wizard rewrite.
- No broad Current IA redesign outside the affected User Linking / Connections states.
- No new secret-storage mechanism.
- No production deployment as part of this split.

## Completion criteria

This split is complete when:

1. Scope A, B, and C each exist as independently reviewable work from current accepted state.
2. Each retained scope passes its required automated checks.
3. B and C pass browser-rendered acceptance for affected UI behavior.
4. PR #199 is closed as superseded only after replacement coverage is verified.
5. `docs/agent/CURRENT-STATE.md` and the durable KitsuSync Project Hub are updated only after accepted default-branch state actually changes.
