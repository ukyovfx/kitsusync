# Current state

## Verification basis

Verified against accepted `master` at commit `78869850c372dc7758a90272d951c303db17fb3d` on 2026-09-24.

Repository version: `0.4.8`.
Latest published GitHub Release: `v0.4.8`, targeting the same accepted commit.

## Confirmed default-branch state

- KitsuSync is a Go Kitsu-to-Discord pipeline bridge with browser setup and administration surfaces.
- `master` is the accepted default-branch implementation state; code, tests, CI, configuration, PR state, and runtime evidence outrank this summary when they disagree.
- Open PR behavior is not accepted implementation until merged into `master`.
- The repo-local AI knowledge entry points are `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and `docs/agent/plans/`.
- Current IA UI decisions are documented in `docs/CURRENT-IA-UI-SPEC.md`; browser-rendered output remains the final visual acceptance source of truth.
- The accepted v0.4.8 commit includes the persisted-runtime Kitsu credential work, reversible Connections token replacement, persisted-credential Production Setup/execution revalidation, and User Linking readiness/mapping behavior merged through PRs #204, #205, #207, and #206.
- Post-merge master CI #532 and Security Audit #114 both passed on `78869850c372dc7758a90272d951c303db17fb3d`.

## Release and deployment evidence

- GitHub Release `v0.4.8` is published, non-draft, non-prerelease, and targets `78869850c372dc7758a90272d951c303db17fb3d`.
- Release deployment bundle workflow run #17 (`35975690039`) completed successfully from that exact accepted commit with `deployment_mode=normal`.
- The canonical artifact is `kitsusync-v0.4.8-deployment` (artifact id `10798142172`) with GitHub digest `sha256:bb0407f1543da1f9e503147a79ce976c41c55fce6a832bf382c5ba14dbfac9d4`.
- Artifact verification established release provenance for v0.4.8/source commit `78869850c372dc7758a90272d951c303db17fb3d`, image `kitsusync:v0.4.8`, the expected image archive digest, exact deployment/control tool digests, and no symlinks or unexpected archive entries.
- Artifact existence, staging, or CI success does not by itself establish that v0.4.8 is running in production.

## Current vfxstudio runtime evidence

- The installed production inspector is a no-argument helper; `kitsusync-inspect all` is rejected.
- The latest read-only inspection still reports image/version `kitsusync:v0.4.7` / `0.4.7`, revision `4879c7f3adb966a4760438f502db1929e76ea51e`, container running and healthy, `/health` HTTP 200, `/ready` HTTP 200 `ready`, and loopback-only binding `127.0.0.1:8090`.
- The last credential-specific observation reported runtime auth mode `bot_token_failed` with error class `BOT_TOKEN_EXPIRED_OR_INACTIVE`. The current no-argument inspector does not report Kitsu auth state, so credential validity must be re-established separately rather than assumed current.
- Live Projects/Persons visibility and the production bot principal's actual authorization/visibility remain independently unverified.
- The current v0.4.7 deployment cannot establish production behavior accepted only in v0.4.8.

## Current active work

- The verified v0.4.8 bundle has been uploaded, re-verified on-host, and staged at the canonical root stage; no `kitsusync-deploy` transaction has executed yet.
- The approved production deployment remains pending a supported unattended operator path, a fresh no-argument production preflight, and exactly one supported `kitsusync-deploy` transaction.
- After deployment, validate runtime identity, health/readiness, loopback binding, backup/rollback evidence, then restore or validate the Kitsu runtime credential through the supported application path before live Kitsu acceptance.
- PR #199 remains an old mixed-scope Draft and must not be merged as-is. PR #203 lifecycle cleanup remains a separate human-approved maintenance action.

## Production state boundary

- Historical v0.4.6 production-verification material is not a current deployment instruction.
- Before any production write, verify the exact release artifact/provenance and current target runtime, then use only the root-installed supported deployment wrapper under explicit production approval.
- Direct production Docker Compose execution is unsupported.
- Host infrastructure such as SSH, firewall, Tailscale, nginx, Kitsu/Zou, PostgreSQL, and Redis remains outside normal KitsuSync product-change scope unless explicitly authorized as cross-project work.

## Known boundaries

- Local worktree/dirty state is not visible from GitHub and must be checked locally before local implementation work.
- The original local checkout is intentionally dirty and must not be reset, stashed, cleaned, or overwritten from GitHub assumptions.
- Live KitsuSync/Kitsu/Zou/Discord runtime health cannot be inferred from GitHub state alone.
- Environment-variable presence is not evidence of credential validity or authoritative credential-source selection.
