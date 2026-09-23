# Current state

## Verification basis

Verified against upstream `master` at commit `84407c582bb0efd0eafd9b0c329c3e3955690b2d` on 2026-09-24.

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
- PRs #199, #203, #205, #206, #207, and #208 are open Draft work and are not accepted default-branch implementation.

## Current active work

- PR #206 contains the focused User Linking readiness/failure-state scope rebuilt on accepted `master`. Its current head is `d3fee3bd0b096563ee7a06133f7653b03b188eb3`; GitHub CI and Security Audit pass on that head, including CodeQL, the Go race detector, and the full Linux/CGO build/test workflow. The CodeQL repair escapes the language-toggle URL in its HTML attribute. Authenticated browser acceptance remains unresolved.
- PR #207 contains the Production Setup runtime-source fix at head `154ea75a6a4327c2c4959a5d3389c107e36c3ff4`; GitHub CI and Security Audit pass. Authenticated browser/runtime acceptance of Production and Task Type reads remains unresolved because the deployed revision predates accepted `master` and the persisted Kitsu token is recorded as expired or inactive.
- PR #205 contains the focused Connections saved-token editing UX at head `f2bfaed4fa06905a080ab8303f2a57c056a2faf0`; GitHub CI and Security Audit pass. Authenticated browser-rendered acceptance remains unresolved.
- PR #203 contains the split-design documentation; it is aligned with accepted `master` with `behind_by=0`, and GitHub CI #506 / Security Audit #88 pass. Its final lifecycle disposition remains pending replacement-scope acceptance.
- PR #208 is this Current State refresh; it remains Draft and does not change accepted `master`.
- Keep PR #199 Draft until replacement scopes are accepted; then close it as superseded.
- Keep repository-local continuity documentation synchronized with accepted `master` state.

## Current vfxstudio runtime evidence

- The approved read-only `/usr/local/sbin/kitsusync-inspect all` helper completed on 2026-09-24. It confirmed image `kitsusync:v0.4.7`, deployed revision/source-id `4879c7f3adb966a4760438f502db1929e76ea51e`, configured Kitsu origin `http://172.17.0.1:8080/`, persisted Kitsu token metadata present, bot metadata present, runtime auth mode `bot_token_failed`, and error class `BOT_TOKEN_EXPIRED_OR_INACTIVE`. The helper masks the bot ID/name values, so principal identity and authorization scope remain unknown.
- The helper output reported `KitsuJWTToken` and `DISCORD_BOT_TOKEN` as present, conflicting with the earlier inspection that reported them empty. No token values were printed; resolve this evidence discrepancy before relying on either environment-variable state.
- This deployment is 46 commits behind accepted `master`. Its deployed Kitsu API read path and runtime behavior are not acceptance evidence for current `master` or open PR behavior. The helper did not establish project/person endpoint row counts, project-team membership, role, or installed Zou 1.0.67 filtering behavior.

## Production state boundary

- The previous active production-verification plan was specific to v0.4.6 and is historical, not a current deployment instruction.
- The current vfxstudio KitsuSync container identity and image revision are partially verified, but the deployed source revision predates accepted `master` by 46 commits and therefore does not establish current repository behavior in production.
- The deployed persisted Kitsu bot credential is recorded as expired or inactive. The latest helper's legacy environment-variable presence fields conflict with earlier evidence; do not infer endpoint authorization or successful reads from presence alone.
- No current accepted-master production deployment or live #206/#207 runtime acceptance is verified from the available evidence.
- Before any production write, re-derive the deployment plan from the current release/repository state and verify the target runtime with live operator evidence.

## Known boundaries

- Local worktree/dirty state is not visible from GitHub and must be checked locally before local implementation work.
- Live KitsuSync/Kitsu/Zou/Discord runtime health cannot be inferred from GitHub state alone.
- Do not present open PR behavior as accepted implementation until it is merged into `master`.
