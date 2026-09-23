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
- PRs #199, #203, #205, #206, and #207 are open Draft work and are not accepted default-branch implementation.

## Current active work

- PR #206 contains the focused User Linking readiness/failure-state scope rebuilt on accepted `master`; current head `2dff31c8251c83f0d3df2d464c1bd5a1a91b7e9e` passes GitHub CI #511 and Security Audit #93, including CodeQL and the Go race detector, and all eight prior CodeQL XSS review threads are resolved. Authenticated browser acceptance remains unresolved. Read-only vfxstudio inspection identifies running container `kitsusync-app-1` as healthy on image `kitsusync:v0.4.7`, with configuration/data mounts sourced from `/home/ukyo_vfx/kitsusync`; image revision/source-id is `4879c7f3adb966a4760438f502db1929e76ea51e`. Accepted `master` `84407c582bb0efd0eafd9b0c329c3e3955690b2d` is 46 commits ahead of that deployed revision, so the current deployment must not be used as runtime acceptance evidence for the #204-dependent #206/#207 behavior. The deployed database has an encrypted persisted Kitsu token and saved bot metadata, but its recorded runtime auth state is `bot_token_failed` with error class `BOT_TOKEN_EXPIRED_OR_INACTIVE`; the deployed container also has empty legacy `KitsuJWTToken` and `DISCORD_BOT_TOKEN` environment credentials. The deployed revision's Kitsu API package reads live data through the legacy environment token path, so this runtime provides evidence for the stale credential-source problem addressed by #204/#207, but a valid persisted Kitsu token is still required before current-head browser/runtime acceptance can be completed. Current project/person endpoint results and bot authorization scope remain unverified, and previously inspected Zou 1.0.67 behavior is not an established root cause.
- PR #207 contains the Production Setup runtime-source fix; GitHub CI and Security Audit pass. The current deployed revision still uses an empty legacy `KitsuJWTToken` for its Kitsu API reads while an encrypted persisted token exists in SQLite, directly matching the stale-source failure shape that #207 removes. However, that persisted token is currently recorded as expired or inactive, so authenticated browser/runtime acceptance of Production and Task Type reads remains unresolved until a valid saved credential is available on a deployment containing the accepted runtime-source foundation.
- PR #205 contains the focused Connections saved-token editing UX; it is now aligned with accepted `master` with `behind_by=0`, and GitHub CI #504 / Security Audit #86 pass. Authenticated browser-rendered acceptance remains unresolved.
- PR #203 contains the split-design documentation; it is aligned with accepted `master` with `behind_by=0`, and GitHub CI #506 / Security Audit #88 pass. Its final lifecycle disposition remains pending replacement-scope acceptance.
- Keep PR #199 Draft until replacement scopes are accepted; then close it as superseded.
- Keep repository-local continuity documentation synchronized with accepted `master` state.

## Production state boundary

- The previous active production-verification plan was specific to v0.4.6 and is historical, not a current deployment instruction.
- The current vfxstudio KitsuSync container identity and image revision are partially verified, but the deployed source revision predates accepted `master` by 46 commits and therefore does not establish current repository behavior in production.
- The deployed runtime currently has no usable legacy Kitsu environment token and its persisted Kitsu bot credential is recorded as expired or inactive; do not treat empty or failed live-data reads from this deployment as acceptance evidence for current `master` or open PR behavior.
- No current accepted-master production deployment or live #206/#207 runtime acceptance is verified from the available evidence.
- Before any production write, re-derive the deployment plan from the current release/repository state and verify the target runtime with live operator evidence.

## Known boundaries

- Local worktree/dirty state is not visible from GitHub and must be checked locally before local implementation work.
- Live KitsuSync/Kitsu/Zou/Discord runtime health cannot be inferred from GitHub state alone.
- Do not present open PR behavior as accepted implementation until it is merged into `master`.
