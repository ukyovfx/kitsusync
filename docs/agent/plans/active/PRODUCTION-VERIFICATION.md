# Production verification after v0.4.6 hardening

## Goal

Prepare the next KitsuSync production deployment from the immutable v0.4.6 release while keeping all production writes and operator-only actions out of this task.

## Verified basis

- Upstream `master`: `cc9dea22daba83dbc04503f3d87e5fc402d04bd7`
- Release: `v0.4.6`, source commit `b7b30157cb90c4500e8b00d3c26ac7038f5c8c10`
- Merged hardening: PRs #164, #165, #166, and #167
- Latest relevant hardening: PR #167, merged into `master`

## Required off-production work

- Run the repository Security Gate and the focused deployment/bootstrap/rollback contract tests.
- Build the release deployment bundle using the documented workflow from the immutable v0.4.6 source and trusted `master` tooling.
- Verify bundle provenance, portable image identity, Compose/tool digests, release identity, and load-back behavior.
- Preserve the generated artifact and verification evidence for operator handoff.

## Remaining production gate

An authorized operator must stage the verified bundle, inspect the target runtime, and perform the production deployment/rollback verification. Do not perform those actions here.

## Verification status

- Upstream `master` CI reports successful `build`, `bundle`, and `deployment-transaction` jobs for `cc9dea2`.
- The retained upstream bundle is `kitsusync-v0.4.6-deployment`; it is not a GitHub Release asset and has not been downloaded or deployed here.
- Local Gitleaks scans pass for the working tree and staged content.
- Local staged and tracked-tree privacy scans pass after removing the user-specific Docker path from `scripts/validate-v045-rc.ps1`.
- `go vet ./src/...` passes.
- Go tests cannot run successfully in this environment: CGO-off runs fail on the SQLite stub, and CGO-on runs cannot find `gcc`.
- Docker/WSL is unavailable, so local Compose validation, image build/load-back, bundle reconstruction, and Docker deployment transaction tests remain unrun locally.

## Intentional deferrals

- F02 remains deferred pending production evidence. It is not an implementation task in this plan.
- PR #162 remains open and is not a blocker for the v0.4.6 deployment path.

## Next action

Run the remaining local checks in a Linux/Docker/CGO-capable environment, or use the retained successful upstream bundle workflow evidence. Then hand the verified artifact to an authorized operator for production staging and runtime inspection; stop before production deployment.
