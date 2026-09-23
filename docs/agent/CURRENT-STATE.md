# Current state

## Verification basis

Verified against upstream `master` at commit `84407c582bb0efd0eafd9b0c329c3e3955690b2d` on 2026-09-23.

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
- PR #202 is merged and refreshed repository continuity documentation.
- PR #204 is merged and makes the persisted runtime Kitsu credential source authoritative for accepted live Kitsu reads.
- PRs #199, #203, #205, #206, and #207 are open Draft work and are not accepted default-branch implementation.

## Current active work

- PR #206 contains the focused User Linking readiness/failure-state scope rebuilt on accepted `master`; GitHub CI and Security Audit pass, while authenticated browser acceptance and the real Kitsu Bot visibility/runtime condition remain unresolved.
- PR #207 contains the Production Setup runtime-source fix; GitHub CI and Security Audit pass, while authenticated browser/runtime acceptance of Production and Task Type reads remains unresolved.
- PR #205 contains the focused Connections saved-token editing UX; its existing checks pass, but its branch diverges from current `master` and must be realigned/rebuilt before merge acceptance, then browser-tested.
- Keep PR #199 Draft until replacement scopes are accepted; then close it as superseded. PR #203 remains the split-design documentation until that cleanup is complete.
- Keep repository-local continuity documentation synchronized with accepted `master` state.

## Production state boundary

- The previous active production-verification plan was specific to v0.4.6 and is historical, not a current deployment instruction.
- No current production deployment or live runtime state is verified from repository evidence alone.
- Before any production write, re-derive the deployment plan from the current release/repository state and verify the target runtime with live operator evidence.

## Known boundaries

- Local worktree/dirty state is not visible from GitHub and must be checked locally before local implementation work.
- Live KitsuSync/Kitsu/Zou/Discord runtime health cannot be inferred from GitHub state alone.
- Do not present open PR behavior as accepted implementation until it is merged into `master`.
