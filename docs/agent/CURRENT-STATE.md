# Current state

## Verification basis

Verified against GitHub accepted `master` at `0f2e346f00a9f3b794c2a6243d39f681e661d68a` on 2026-11-18. The forum-notification renovation and its follow-up fix batches are merged (see below); each PR head passed the full CI and Security Audit check suites before merge. Those are PR-head checks, not a claim that a live deployment of this code exists.

Repository, Git, CI, tests, configuration, and verified live runtime evidence override this summary when they disagree. An open PR or release candidate is not accepted implementation.

## Accepted repository and release state

- `master` at `0f2e346f00a9f3b794c2a6243d39f681e661d68a` is the accepted code state. `VERSION` is `0.7.1`.
- The latest published GitHub release is `v0.7.1`. Production runs v0.7.1 (read-only inspect on 2026-11-17: OCI version `0.7.1`, image revision `7b6e7679`). No later code is deployed to Production.
- The forum-notification renovation (the current system kept as the base; notifications moved from text-channel lanes to per-task-type forum channels, one post per task, status as forum tags, signals as mention messages inside the post, DM reduced to a morning digest) is fully merged:
  - Stage B (Kitsu fetch swap) #279, Stage C (forum settings + admin UI) #280, Stage D (forum sending + ledger + forum/tag auto-create) #282, Stage E (aggregation, flap warnings, comments, digest) #283, Stage F (lane removal) #284.
  - Acceptance coverage #285; submitter-marker reading side #286 (the Web Publisher writer side shipped separately in that product's 5.31).
  - Forum zero-touch auto-setup #292; per-forum tag-assignment settings abolished #295 (tags stay fixed/common, bound to roles by name).
- Follow-up fixes merged on top: staging deploy script file list #287, browser-acceptance stabilization #288, staging script UTF-8 pinning #289, System Status button wrap #290, connection-wizard step-4 button #291, wizard/URL fixes #293/#298/#299, scale-audit pre-release fixes #294/#296/#297/#300/#301/#303, log rotation and configured log level #302.
- Current IA UI decisions remain in `docs/CURRENT-IA-UI-SPEC.md`; browser-rendered output is the final source of truth for visual acceptance.

## Staging boundary

- The documented Staging contract is `staging-v10`: the helper, installer and bootstrap read the operator and emergency account names from the root-owned `/etc/kitsusync-staging/operators.conf` and fail closed when it is missing or invalid. The installed helper was upgraded on 2026-11-17 (`STAGING_HELPER_UPGRADE=PASS`, source `7045ddb1`).
- Staging is a separate Compose project on `127.0.0.1:8091` with isolated state. Production follows the separate release-only path on `127.0.0.1:8090`.
- `scripts/deploy-kitsusync-staging-candidate.ps1 -CommitSha <sha>` is the documented candidate path; the candidate must be a merged PR head with successful pull-request-event CI and Security Audit runs and an unexpired deployment artifact.
- Staging is currently used for the forum-notification smoke (production connection, forum auto-setup, user mapping, status cycle) against the latest merged candidate.

## Production status and blocking gates

- Production still runs the pre-renovation v0.7.1 lane model. The forum model reaches Production only through the release path: VERSION bump, release tag, release bundle, and the fixed-path deployment contract. Direct Compose changes are prohibited.
- Blocking gates before the forum-model production release:
  - Staging smoke completion (user mapping, baseline observation, a full WIP → WFA → RETAKE → WFA → DONE cycle, card/thumbnail/submitter-marker checks).
  - Poll-interval decision: `kitsu_fetch_timing` measurements from staging decide between the current 10 s default and the designed 60 s.
  - Gate A remainder: comment-fetch verification against the production Kitsu with the production account (team-membership check) is still open.
- Known production behaviour of v0.7.1 that the renovation removes: the comments list endpoint returns 403 for the service account, so comment/mention detection is ineffective in v0.7.1; lane settings are abolished by the forum model.

## Current / Next / Pending

- Current: staging smoke on the latest master candidate; small pre-release fix batches (scale-audit remainder, admin-UI findings from the smoke) land as separate PRs.
- Next: finish the smoke and the poll-interval measurement, then cut the forum-model release (working version number 0.8.0) and deploy to Production under the contract, including the Kitsu preview-thumbnail proxy for Discord cards.
- Pending: security-disposition implementation from the 2026-11-10 penetration-test rounds (Medium 8 / Low 10) is a separate track, not on the release critical path.

## Repository guidance

- `docs/agent/START-HERE.md` points here for accepted `master` state and to `docs/agent/plans/active/` for durable active-task plans. Do not create duplicate plans when no further active repository implementation is needed.
- Follow `AGENTS.md`, `docs/PRODUCTION_BOOTSTRAP.md`, and the relevant in-repo deployment contracts before scoped operations.
- Keep Production, Staging, and server administration acceptance boundaries distinct.
