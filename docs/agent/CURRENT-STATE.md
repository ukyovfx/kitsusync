# Current state

## Verification basis

Verified against the accepted `master` commit `78869850c372dc7758a90272d951c303db17fb3d` on 2026-09-24.

Repository version: `0.4.8` (`VERSION` and the `v0.4.8` tag at the accepted commit).
GitHub Release publication status was not verified as part of this repository refresh.

## Confirmed default-branch state

- KitsuSync is a Go Kitsu-to-Discord pipeline bridge with browser setup and administration surfaces.
- `master` is the accepted default-branch implementation state; code, tests, CI, configuration, PR state, and runtime evidence outrank this summary when they disagree.
- The repo-local AI knowledge entry points are `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and `docs/agent/plans/`.
- Current IA UI decisions are documented in `docs/CURRENT-IA-UI-SPEC.md`; browser-rendered output remains the final visual acceptance source of truth.
The accepted commit includes the v0.4.8 release-version change. This file does not restate feature or CI claims from earlier candidate snapshots; verify implementation and checks against the accepted commit and its current CI records.

## Historical vfxstudio runtime observation

- A read-only helper observation on 2026-09-24 identified image `kitsusync:v0.4.7`, deployed revision/source-id `4879c7f3adb966a4760438f502db1929e76ea51e`, runtime auth mode `bot_token_failed`, and error class `BOT_TOKEN_EXPIRED_OR_INACTIVE`.
- That observation predates accepted v0.4.8 and is historical only. It does not establish the current deployed version, credential validity, live Kitsu visibility, or current runtime behavior.

## Deployment evidence

- This repository snapshot does not verify a deployment artifact or its CI provenance for `78869850c372dc7758a90272d951c303db17fb3d`.
- A source tag, repository version, or artifact publication alone does not establish that accepted master is deployed to production.

## Production state boundary

- The previous v0.4.6 production-verification plan is historical and is not a current deployment instruction.
- This documentation refresh did not inspect production, so the live deployed version and credential state are unverified here.
- Before any production write, verify the exact artifact/provenance and target runtime, then use only the supported deployment wrapper under explicit production approval.
- After approved deployment, validate the live behaviors required for that release through supported application paths.

## Known boundaries

- Local worktree/dirty state is not visible from GitHub and must be checked locally before local implementation work.
- Live KitsuSync/Kitsu/Zou/Discord runtime health cannot be inferred from GitHub state alone.
- Open PR behavior is not accepted implementation until merged into `master`.
