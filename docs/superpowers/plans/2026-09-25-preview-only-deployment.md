# KitsuSync preview-only deployment implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy one exact Linux CI candidate SHA to vfxstudio through a distinct non-release preview entry point that shares the existing backup/deploy/rollback transaction.

**Architecture:** Keep release and preview provenance policies in separate root entry points. Extract the current deployment transaction into one root-owned core that consumes already validated identity and mode values; candidate policy cannot be selected through the release command. Preview uses the existing Compose project/runtime and adds only a preview readiness mode that permits `ready` or `setup_required`.

**Tech Stack:** Bash, Python 3, Docker Compose, Go, GitHub Actions, PowerShell/SSH.

**Spec:** `docs/superpowers/specs/2026-09-25-preview-only-deployment-design.md`

## Global Constraints

- Preserve release provenance checks and release wrapper behavior.
- Preview requires `artifact_kind=candidate`, exact explicit SHA, `source_id=source_commit`, empty release identity, and `kitsusync:ci-<SHA>`.
- Preserve transaction locking, complete backup, rollback, expected single runtime container, and loopback binding `127.0.0.1:8090`.
- Preview accepts `/ready` only as `ready`/200 or `setup_required`/503 with matching build identity.
- No merge, release, deployment outside vfxstudio, or unrelated host/network/account/secret changes.

## Review Focus

- Wrong or missing expected SHA is rejected before touching runtime state; test candidate provenance validation.
- Candidate bundle submitted to release entry point is rejected; run existing release hardening contract tests unchanged.
- Candidate runtime with mismatched OCI labels or non-loopback port rolls back; cover in transaction integration test.
- `setup_required` is accepted only with valid health/build identity and UI boundary routes; cover in preview transaction fixtures.
- Preview token is a visible safety marker and never treated as a credential; test exact token requirement and ensure no secret storage.

---

### Task 1: Correct stale System Status CI expectations

**Files:**
- Modify: `src/setup/ia_views_test.go`
- Inspect only: `src/setup/ia_views.go`

**Interfaces:**
- Consumes: `renderPipelineHealthItem`, `readinessViewFor`, `renderIAHealth`.
- Produces: CI assertions that require no normal-page diagnostic disclosure, only real state actions, correct compact right rail, and truthful unobserved API semantics.

- [ ] **Step 1: Run the focused tests on Linux/CGO or note local Windows limitation.**
- [ ] **Step 2: Update only obsolete details/action/status-summary assertions; preserve assertions for setup blockers, actual routes, line graph, timestamp X positions, separate scales, no fabricated failed latency, and rail alignment.**
- [ ] **Step 3: Run `CGO_ENABLED=1 go test ./src/setup -run 'TestSystemStatus|TestBotAndSystemStatusUseActualPrerequisiteValues' -count=1 -timeout=120s`.**
- [ ] **Step 4: Commit the test correction.**

### Task 2: Add separate candidate policy and reuse deployment transaction

**Files:**
- Modify: `deploy/kitsusync-deploy` (extract transaction body behind strict existing release gate without changing its policy)
- Create: `deploy/kitsusync-deploy-transaction`
- Create: `deploy/kitsusync-preview-deploy`
- Modify: `scripts/build-deployment-bundle.sh` only if candidate mode needs explicit PREVIEW metadata already not represented
- Modify: `scripts/test-release-hardening.sh`
- Create/modify: `scripts/test-preview-deploy-contract.sh`
- Modify: `.github/workflows/build.yml`

**Interfaces:**
- Release wrapper calls the shared core only after its existing release checks pass.
- Preview wrapper accepts exactly `<expected-source-sha> PREVIEW`, reads the root-owned staged bundle, and calls the shared core only after candidate checks pass.
- Shared core accepts validated identity through a root-owned temporary identity file; it receives no user-controlled Compose path, image ref, runtime root, project, or port.

- [ ] **Step 1: Add contract tests for candidate-only exact identity, explicit confirmation, empty release fields, and the CI image tag.**
- [ ] **Step 2: Run contract tests and verify expected failures before implementation.**
- [ ] **Step 3: Extract the current transaction implementation with no semantic changes; preserve release validation in `kitsusync-deploy`.**
- [ ] **Step 4: Implement preview-only provenance gates and preview readiness mode in the shared validator.**
- [ ] **Step 5: Test that release wrapper rejects candidate inputs and preview rejects release/wrong SHA/wrong tag/nonempty release metadata.**
- [ ] **Step 6: Run existing release hardening tests and new preview contract tests.**
- [ ] **Step 7: Commit preview tooling and tests.**

### Task 3: Exercise the preview deployment transaction in Linux Docker CI

**Files:**
- Modify: `scripts/test-deployment-transaction.sh`
- Modify: `.github/workflows/build.yml`
- Inspect: `scripts/build-deployment-bundle.sh`, `deploy/kitsusync-preview-deploy`

**Interfaces:**
- Integration fixture creates a candidate bundle through the production bundle builder and invokes the actual preview entry point.
- Test asserts container SHA identity, exactly one service container, loopback binding, valid `/health`, setup-required readiness, UI route boundary, rollback on invalid target.

- [ ] **Step 1: Add candidate fixture tests for successful setup-required deployment and failure rollback.**
- [ ] **Step 2: Run the transaction harness and confirm expected failures.**
- [ ] **Step 3: Add minimal preview bundle construction and invocation to the isolated integration harness.**
- [ ] **Step 4: Run `bash scripts/test-deployment-transaction.sh` and all release transaction/hardening tests.**
- [ ] **Step 5: Commit the transaction coverage.**

### Task 4: Validate candidate, push successor, and build immutable artifact

**Files:**
- Modify: `src/setup/ia_views_test.go` only if Linux CI identifies another obsolete assertion.
- Modify: `.github/workflows/build.yml` only for preview artifact production and validation.

**Interfaces:**
- GitHub Actions PR candidate workflow uploads a bundle only for exact PR head SHA and successful tests, vet, Compose, provenance, transaction, and security checks.
- Candidate artifact is named with the exact SHA and remains `artifact_kind=candidate`.

- [ ] **Step 1: Run gofmt, `git diff --check`, Linux CGO Go tests, vet, Compose validation, deployment transaction, and required security checks.**
- [ ] **Step 2: Commit the complete successor candidate.**
- [ ] **Step 3: Push only the explicit branch ref to update PR #213; do not merge.**
- [ ] **Step 4: Wait for every required PR check to complete successfully and download its exact-SHA artifact.**
- [ ] **Step 5: Verify artifact archive digest, provenance fields, image labels/content identity, and candidate SHA before staging.**

### Task 5: Stage candidate on vfxstudio and prepare one privileged command

**Files:**
- No repository changes unless a staging bug is found and fixed through Tasks 2–4.

**Interfaces:**
- Stage immutable bundle under the breakglass user's home via normal SSH.
- Prepare one PowerShell command that uses `vfxstudio-breakglass` and interactive sudo, with candidate SHA and expected staged hashes fixed in the command.

- [ ] **Step 1: Stage candidate artifact and a root runner; verify server-side hashes over normal SSH.**
- [ ] **Step 2: Prepare one exact PowerShell command for breakglass sudo that validates host-side Go/SQLite tests, vet, Compose, artifact/provenance, installs preview wrapper if needed, deploys, and verifies identity/health/routes.**
- [ ] **Step 3: Stop for the user's sudo execution only after all non-root work is complete.**

### Task 6: Deploy and verify after sudo

**Files:**
- No repository changes unless an operational defect requires a new reviewed successor.

- [ ] **Step 1: Continue after the user runs the prepared command.**
- [ ] **Step 2: Verify source SHA, exact single container, loopback port, `/health`, `/ready`, `/bot/admin/users`, and `/bot/admin/health`.**
- [ ] **Step 3: Report preview identity and exact review pages; do not merge or release.**
