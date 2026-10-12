# Current state

## Verification basis

Verified against GitHub accepted `master` at `fe293f1f2b8858cf0625d903e2249fce8c209967` on 2026-10-12. PRs #233, #234, and #235 are merged. PR #235 exact-head `a8d4eb4f2c9c993ff03fda2289bf2e97d2084a36` passed CI run `38211376994` and Security Audit run `38211376990`. Those are PR-head checks, not a claim that post-merge `master` checks or a live deployment passed. Recheck exact-head CI/Security before any further acceptance or promotion.

Repository, Git, CI, tests, configuration, and verified live runtime evidence override this summary when they disagree. An open PR or release candidate is not accepted implementation.

## Accepted repository and release state

- `master` at `fe293f1f2b8858cf0625d903e2249fce8c209967` is the accepted code state. `VERSION` is `0.5.1`.
- The latest published GitHub release is `v0.5.1`, created from `f8a32bbba94d84068d9e96c18554ac4ab7b24540` (annotated release tag). This immutable release predates the merged #233, #234, and #235 changes. Neither a later `master` nor a passing CI run updates the published v0.5.1 bundle or proves a new release.
- Accepted recent changes:
  - #233 — read-only Production preflight collector.
  - #234 — historical repository documentation organization.
  - #235 — Production deployment recovery, fixed-path deployment-helper maintenance support, runtime reconstruction identity checks, and related regressions.
- Previously merged UI and reviewer functionality, including #219 and #225, remains accepted; consult Git history and `docs/CURRENT-IA-UI-SPEC.md` for exact behavior.

## Staging boundary

- The documented Staging contract is `staging-v8`. The previously operator-reported helper SHA-256 `c2e8b1b8dda50357909e2b5f8081560c262cff30e0107958ab85b9366a726490` is historical evidence, not proof of current installed identity.
- Staging is designed as a separate Compose project on `127.0.0.1:8091`, with isolated state and polling disabled. Production follows the separate release-only path on `127.0.0.1:8090`.
- `scripts/deploy-kitsusync-staging-candidate.ps1 -CommitSha <sha>` is the documented candidate path. Confirm actual Staging identity, helper version, and checks when needed; a workflow marked skipped does not establish a deployed candidate.

## Production status and blocking gates

- **No present Production KitsuSync revision, readiness, or deployment is verified by this document.** A historical 2026-09-28 read-only review reported revision `3d4dafc7d75658a5877d2e2332266c8e6145163d`; this is not current-state proof. Merging #235 did not install the helper or deploy any application.
- The preflight collector reports Production capacity as `unknown/BLOCKED` whenever any relevant filesystem's simultaneous peak requirement or available capacity cannot be established. The new deployment wrapper mitigates backup ENOSPC and retains fail-closed rollback protections; **it does not prove sufficient Production capacity**. An administrator must attest the exact installed-wrapper/runtime filesystem requirements and capacity, provision insufficient filesystems, and rerun the check. Old backup sizes and a single free-space snapshot are not substitutes.
- The one-time root-owned, fixed-path deployment-helper maintenance artifact is distinct from the published app release. Installing it requires an exact-predecessor and artifact-identity check plus separate operator authorization. There has been no authorized Production helper installation in this verification.
- Before separately authorizing any Production application deployment, establish current artifact/provenance, live backup/rollback capacity and integrity, and `/ready=ready`; `setup_required`, capacity unknown/insufficient, or a failed check must block deployment. Use the supported installed fixed-path controls; do not bypass gates, directly modify Production Compose, or claim rollback guarantees for unmeasured conditions.
- After an explicitly authorized and successful deployment, verify the actual running version/revision, health/readiness, Kitsu connection scope and Discord integration with runtime evidence. The accepted GitHub code state is not proof of these outcomes.

## Current / Next / Pending

- As checked immediately after #235 merged and before opening this documentation update, GitHub had no other open PRs. PR #220 remains closed as superseded.
- Next: independently verify post-merge master checks; obtain the filesystem-specific Production capacity attestation without exposing secrets; prepare the separately reviewed one-time helper maintenance; then check readiness and request an exact, scoped deployment approval only when all gates pass.
- Pending: current Production KitsuSync runtime identity, Production backup/capacity attestation, helper maintenance operation, `/ready` status and authorized deployment. None of these is implied complete by #235's merge.
- The original dirty local checkout and the #235 PR worktree's test-generated `__pycache__` were not modified by the GitHub merge or this documentation work.

## Repository guidance

- `docs/agent/START-HERE.md` points here for accepted `master` state and to `docs/agent/plans/active/` for durable active-task plans. Do not create duplicate plans when no further active repository implementation is needed.
- Follow `AGENTS.md`, `docs/PRODUCTION_BOOTSTRAP.md`, and the relevant in-repo deployment contracts before scoped operations.
- Current IA UI decisions remain in `docs/CURRENT-IA-UI-SPEC.md`; browser-rendered output is the final source of truth for visual acceptance.
- Keep Production, Staging, server administration, and the separate VFX Files Plugin repository's acceptance boundaries distinct.
