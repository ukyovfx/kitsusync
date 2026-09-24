# Current state

## Verification basis

Verified against upstream `master` at commit `95a898c3068af473700a92acd436c8ebb6a4f768` on 2026-09-24.

Repository version: `0.4.7`.
Latest GitHub Release: `v0.4.7`.

## Confirmed default-branch state

- KitsuSync is a Go Kitsu-to-Discord pipeline bridge with browser setup and administration surfaces.
- `master` is the accepted default-branch implementation state; code, tests, CI, configuration, PR state, and runtime evidence outrank this summary when they disagree.
- The repo-local AI knowledge entry points are `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and `docs/agent/plans/`.
- Current IA UI decisions are documented in `docs/CURRENT-IA-UI-SPEC.md`; browser-rendered output remains the final visual acceptance source of truth.
- PR #204 is merged and makes the persisted runtime Kitsu credential authoritative for accepted live Kitsu reads.
- PR #205 is merged and makes saved Kitsu/Discord token replacement reversible without rendering stored secret values.
- PR #207 is merged and moves Production Setup, Task Type reads, and execution-time revalidation onto the persisted/runtime Kitsu credential source; reviewed `category_id` is preserved into execution revalidation.
- PR #206 is merged and makes User Linking readiness/failure/empty states explicit, requires explicit guild selection when needed, excludes bot identities from human mapping, and preserves Save/Unlink mapping persistence.
- Post-merge master CI #528 and Security Audit #110 both pass on `95a898c3068af473700a92acd436c8ebb6a4f768`.
- The accepted master tree is `5d1bb57ed5fd76665f542e8bc1b87f964a28dc40`, matching the previously browser-tested combined candidate tree.

## Pre-merge acceptance evidence

- GitHub Actions run `35957556307` completed successfully on disposable acceptance branch head `42aa3d0a4a2c559e879aa39b3c6acdb73214bc90`.
- The acceptance candidate was composed from accepted master-at-the-time plus exact #205, #207, and #206 heads and produced the same final tree now present on accepted `master`.
- Linux/CGO validation, `go vet`, Go tests, Compose validation, verified external-egress isolation, authenticated Chromium acceptance, firewall restoration, cleanup, and non-secret artifact upload passed.
- Browser acceptance covered #205 Connections token Change/Cancel/masking behavior, #207 persisted-credential Production Setup/Task Types/execution revalidation, and #206 User Linking JP/EN desktop/mobile readiness states plus Save/refresh/Unlink persistence.
- Synthetic acceptance proves implementation/browser behavior only; it does not establish production credential validity, live Kitsu visibility, or current deployed-runtime behavior.

## Current active work

- PR #208 is this documentation refresh. It remains Draft until its refreshed head passes CI/Security and receives explicit merge approval.
- PR #199 is the old mixed-scope Draft and must not be merged as-is; replacement implementation scopes are now accepted on `master`.
- PR #203 is the split-design Draft. Its lifecycle can be resolved as superseded after explicit human approval.
- The next implementation-facing step is deployment preparation from exact accepted master `95a898c3068af473700a92acd436c8ebb6a4f768`, not further acceptance work on #205/#206/#207.

## Current vfxstudio runtime evidence

- The approved read-only `/usr/local/sbin/kitsusync-inspect all` helper completed on 2026-09-24. It identified image `kitsusync:v0.4.7`, deployed revision/source-id `4879c7f3adb966a4760438f502db1929e76ea51e`, configured Kitsu origin `http://172.17.0.1:8080/`, persisted Kitsu token metadata present, bot metadata present, runtime auth mode `bot_token_failed`, and error class `BOT_TOKEN_EXPIRED_OR_INACTIVE`.
- Git comparison shows accepted `master` is now 75 commits ahead of the deployed revision.
- The latest approved helper observation reported legacy `KitsuJWTToken` and `DISCORD_BOT_TOKEN` environment variables as empty; an earlier observation reported them present. No secret values were printed, and environment presence must not be used as evidence of credential validity or credential-source selection.
- Current Projects/Persons response shapes/row counts and the production bot principal's actual authorization/visibility remain independently unverified.
- The old deployment cannot establish behavior now accepted through #204/#205/#206/#207.

## Deployment evidence

- Master CI #528 produced deployment artifact `kitsusync-deployment-95a898c3068af473700a92acd436c8ebb6a4f768` from exact accepted master.
- Artifact id: `10796346925`.
- GitHub artifact digest: `sha256:54fe8b340bc666d8474d10919487e847d76dd988956be7037daf0ee27092f12a`.
- CI verified source/build/image provenance, bundle load-back behavior, deployment behavior tests, and isolated deployment/rollback transaction behavior before upload.
- Artifact existence and CI success are deployment-preparation evidence only; no production deployment is authorized by this document.

## Production state boundary

- The previous v0.4.6 production-verification plan is historical and is not a current deployment instruction.
- No deployment of accepted master `95a898c3068af473700a92acd436c8ebb6a4f768` to vfxstudio is verified yet.
- The current persisted production Kitsu Bot credential is recorded as expired or inactive.
- Before any production write, verify the exact artifact/provenance and target runtime, then use only the supported deployment wrapper under explicit production approval.
- After approved deployment, restore/validate a usable Kitsu Bot credential through the supported Connections/setup path and perform live Projects/Persons, Production Setup, Task Type/execution revalidation, and User Linking verification.

## Known boundaries

- Local worktree/dirty state is not visible from GitHub and must be checked locally before local implementation work.
- The original local checkout was intentionally preserved at dirty local-only HEAD `a15c52e5d759418bf80b198e7dddbe0e1f80d874`; do not reset or overwrite it from GitHub assumptions.
- Live KitsuSync/Kitsu/Zou/Discord runtime health cannot be inferred from GitHub state alone.
- Open PR behavior is not accepted implementation until merged into `master`.
