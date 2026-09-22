# Current state

## Verification basis

Verified against upstream `master` at commit `c1440d7a258611e7c338d4823a849a98214fdb17` on 2026-09-22.

Repository version: `0.4.7`.
Latest GitHub Release: `v0.4.7`.

## Confirmed default-branch state

- KitsuSync is a Go Kitsu-to-Discord pipeline bridge with browser setup and administration surfaces.
- `master` is the accepted default-branch implementation state; code, tests, CI, configuration, PR state, and runtime evidence outrank this summary when they disagree.
- The repo-local AI knowledge entry points are `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and `docs/agent/plans/`.
- Current IA UI decisions are documented in `docs/CURRENT-IA-UI-SPEC.md`; browser-rendered output remains the final visual acceptance source of truth.
- PR #197 is merged for the current IA UI work represented on `master`.
- PR #200 is merged and adds Dependabot configuration for Go Modules, Docker, and GitHub Actions.
- PR #201 is merged and establishes evidence-first community debugging/contribution rules, structured issue forms, reproduction reporting, and public PR validation guidance.
- PR #199 is open as Draft and is not accepted default-branch state. Its current diff is broader than its stated focused User Linking UI scope and must be reconciled before merge.

## Current active work

- Reconcile PR #199 into reviewable scopes before any merge: focused User Linking readiness/presentation, persisted Kitsu runtime credential/live-data behavior, and Connections token-editing UX must not be treated as one already-accepted change.
- Rebuild/rebase any retained #199 work from current `master`, then rerun required checks and browser acceptance where UI behavior is involved.
- Keep repository-local continuity documentation synchronized with accepted `master` state.

## Production state boundary

- The previous active production-verification plan was specific to v0.4.6 and is historical, not a current deployment instruction.
- No current production deployment or live runtime state is verified from repository evidence alone.
- Before any production write, re-derive the deployment plan from the current release/repository state and verify the target runtime with live operator evidence.

## Known boundaries

- Local worktree/dirty state is not visible from GitHub and must be checked locally before local implementation work.
- Live KitsuSync/Kitsu/Zou/Discord runtime health cannot be inferred from GitHub state alone.
- Do not present open PR behavior as accepted implementation until it is merged into `master`.
