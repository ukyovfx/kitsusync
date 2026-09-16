# Production verification after v0.4.6 hardening

## Goal

Prepare the next KitsuSync production deployment from the immutable v0.4.6 release while keeping all production writes and operator-only actions out of this task.

## Verified basis

- Upstream `master`: `0d226f5d2d9383ba6275366783c1f82cc66ee596`
- Release: `v0.4.6`, source commit `b7b30157cb90c4500e8b00d3c26ac7038f5c8c10`
- Merged changes: PR #162 at `0d226f5d2d9383ba6275366783c1f82cc66ee596`, plus PRs #164, #165, #166, and #167
- Latest relevant production deploy/rollback hardening: PR #167 at `cc9dea22daba83dbc04503f3d87e5fc402d04bd7`, merged into `master`

## Required off-production work

- Run the repository Security Gate and the focused deployment/bootstrap/rollback contract tests.
- Build the release deployment bundle using the documented workflow from the immutable v0.4.6 source and trusted `master` tooling.
- Verify bundle provenance, portable image identity, Compose/tool digests, release identity, and load-back behavior.
- Preserve the generated artifact and verification evidence for operator handoff.

## Remaining production gate

An authorized operator must stage the verified bundle, inspect the target runtime, and perform the production deployment/rollback verification. Do not perform those actions here.

## Verification status

- Upstream CI reports successful `build`, `bundle`, and `deployment-transaction` jobs for hardening commit `cc9dea2`; `master` subsequently advanced to `0d226f5` through merged PR #162.
- The retained upstream bundle is `kitsusync-v0.4.6-deployment`; it is not a GitHub Release asset and has not been downloaded or deployed here.
- Current staged Gitleaks and staged/tracked privacy scans pass; earlier tracked-tree scans also passed after removing the user-specific Docker path from `scripts/validate-v045-rc.ps1`.
- `go vet ./src/...` passes.
- Go tests cannot run successfully in this environment: CGO-off runs fail on the SQLite stub, and CGO-on runs cannot find `gcc`.
- Docker/WSL is unavailable, so local Compose validation, image build/load-back, bundle reconstruction, and Docker deployment transaction tests remain unrun locally.
- The release-hardening, release-identity, bootstrap-security, image-identity, and runtime-state contract tests pass locally; runtime-state skips its Docker round-trip.
- Restore-state tests require the POSIX-only Python `pwd` module, and Windows startup-validation cases are collected but skipped.
- The production-path contract still flags a literal `docker compose down -v` safety warning in pre-existing `README.md` content; deployment-behavior tests require Docker and remain unrun.

## Intentional deferrals

- F02 remains deferred pending production evidence. It is not an implementation task in this plan.
- PR #162 is merged and is not a blocker for the v0.4.6 deployment path.

## Documentation cleanup task

- Review stale or contradictory operational claims and remove obsolete completion language while preserving durable evidence.
- Keep `docs/PRODUCTION_BOOTSTRAP.md` as the canonical production deployment procedure and reduce duplicated operational instructions elsewhere by linking or narrowing them.
- Resolve the production-path contract finding in the pre-existing `README.md` recovery warning only after confirming ownership of that dirty change; do not absorb unrelated user work into a cleanup commit.
- Review completed plans for archival only after their work is actually complete; this active plan remains unarchivable while off-production verification and the operator-only production gate are incomplete.

## Next action

Run the remaining local checks in a Linux/Docker/CGO-capable environment, or use the retained successful upstream bundle workflow evidence. Then hand the verified artifact to an authorized operator for production staging and runtime inspection; stop before production deployment.
