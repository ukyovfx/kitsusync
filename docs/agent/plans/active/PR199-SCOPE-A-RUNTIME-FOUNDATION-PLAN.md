# Scope A Runtime Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a UI-independent persisted Kitsu runtime/live-data foundation with explicit credentials and preserved failure outcomes.

**Architecture:** Resolve one authoritative runtime Kitsu endpoint/token from persisted encrypted settings, falling back to environment credentials only when no persisted token exists. Pass that explicit source into Kitsu read APIs and preserve safe structured lookup failures; Current IA consumption is deferred to Scope B.

**Tech Stack:** Go, GORM/SQLite test fixtures, net/http/httptest, existing request verified-origin layer.

**Spec:** `docs/agent/plans/active/PR199-SCOPE-SPLIT-DESIGN.md`

## Global Constraints

- Rebuild from current accepted `master`; do not merge PR #199 history wholesale.
- No DB schema, auth/session, setup-wizard, or UI rendering changes.
- Persisted secret values must never be rendered or logged.
- Existing verified-origin, pinned-IP, TLS, and redirect protections must remain intact.
- Use TDD: failing test first, then minimal implementation.

## Review Focus

- Persisted token exists but decryption fails: must fail closed without env fallback.
- Explicit endpoint ending in `/api`: generated data paths must still be correct.
- HTTP 5xx after retry handling: caller must receive a non-nil error.
- HTTP 4xx vs successful empty JSON array: failure and empty success must stay distinct.
- Existing verified-origin protections must reject target/origin mismatches exactly as before.

---

### Task 1: Explicit Kitsu read APIs

**Files:**
- Create test: `src/api/kitsu/pr199_runtime_foundation_test.go`
- Modify: `src/api/kitsu/kitsu.go`

**Interfaces:**
- Produces: `GetPersonsWithCredentials(baseURL, token string) (Persons, error)`
- Produces: `GetProjectsWithCredentials(baseURL, token string) (Projects, error)`
- Produces: `GetProjectTaskTypesWithCredentials(baseURL, token, projectID string) TaskTypes`

- [ ] **Step 1: Write failing tests**

Create tests using `httptest.NewServer`, `configureTestOrigin`, and an explicit bearer token. Assert `/api/data/persons/` and `/api/data/projects/` are requested with `Authorization: Bearer persisted-token` while `KitsuJWTToken` is empty.

- [ ] **Step 2: Verify RED**

Run:
```bash
go test ./src/api/kitsu -run 'TestPR199.*WithCredentials' -count=1
```
Expected: FAIL because the explicit-credential functions do not exist.

- [ ] **Step 3: Implement minimal explicit APIs**

Add explicit endpoint/token variants while keeping existing env-based functions as compatibility wrappers. Normalize a provided base URL by trimming trailing `/` and an optional trailing `/api`, then append the existing API paths.

- [ ] **Step 4: Verify GREEN**

Run the focused command above, then:
```bash
go test ./src/api/kitsu -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/api/kitsu/kitsu.go src/api/kitsu/pr199_runtime_foundation_test.go
git commit -m "feat: add explicit Kitsu runtime read APIs"
```

### Task 2: Authoritative persisted runtime source

**Files:**
- Create test: `src/setup/pr199_runtime_source_test.go`
- Modify: `src/setup/runtime_credentials.go`

**Interfaces:**
- Consumes: existing encrypted runtime Kitsu token storage.
- Produces: `runtimeKitsuDataSource(db *gorm.DB) (baseURL, token string, ok bool)`.

- [ ] **Step 1: Write failing tests**

Cover:
- persisted token + persisted API base URL wins even when a stale env token is present;
- unreadable persisted ciphertext returns `ok=false`, empty base/token, and does not fall back to env;
- no persisted token may use the existing env token/host compatibility path.

- [ ] **Step 2: Verify RED**

```bash
go test ./src/setup -run 'TestPR199RuntimeKitsuDataSource' -count=1
```
Expected: FAIL because `runtimeKitsuDataSource` does not exist.

- [ ] **Step 3: Implement minimal source resolution**

Resolve persisted token first. If ciphertext exists, decryption failure is terminal for this source. Resolve API base URL from saved API override, then safe Kitsu UI host, then saved hostname. Only when no persisted token exists may legacy env credentials be considered.

- [ ] **Step 4: Verify GREEN**

Run focused test, then:
```bash
go test ./src/setup -run 'RuntimeKitsu|Credential' -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/setup/runtime_credentials.go src/setup/pr199_runtime_source_test.go
git commit -m "feat: resolve persisted Kitsu runtime source"
```

### Task 3: Safe lookup outcome helpers

**Files:**
- Create test: `src/setup/pr199_kitsu_lookup_test.go`
- Modify: `src/setup/helpers.go`
- Modify: `src/utils/request/request.go`
- Create/modify test: `src/utils/request/pr199_runtime_foundation_test.go`

**Interfaces:**
- Consumes: explicit Kitsu read APIs from Task 1.
- Produces: safe `KitsuLookupError` classification and explicit credential list helpers.

- [ ] **Step 1: Write failing tests**

Use an `httptest` server to verify:
- persons endpoint HTTP 401 produces a non-nil classified error;
- projects endpoint JSON `[]` produces zero projects and nil error;
- a direct 5xx `attemptOnce` result has a non-nil error.

- [ ] **Step 2: Verify RED**

```bash
go test ./src/setup -run 'TestPR199KitsuLookup' -count=1
go test ./src/utils/request -run 'TestPR199Transient5xxPreservesError' -count=1
```
Expected: FAIL because helper APIs/classification are missing and 5xx currently drops the concrete error.

- [ ] **Step 3: Implement minimal failure preservation**

Add explicit credential list helpers for persons/projects, classify safe endpoint-scoped failures without URLs/tokens/bodies, and return `fmt.Errorf("HTTP status %d", resp.StatusCode)` for 5xx transient results.

- [ ] **Step 4: Verify GREEN**

Run both focused commands, then:
```bash
go test ./src/setup ./src/utils/request -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/setup/helpers.go src/setup/pr199_kitsu_lookup_test.go src/utils/request/request.go src/utils/request/pr199_runtime_foundation_test.go
git commit -m "fix: preserve Kitsu live lookup failures"
```

### Task 4: Scope A full verification

**Files:** No new behavior.

- [ ] **Step 1: Run repository verification**

```bash
go test ./src/... -count=1 -timeout=120s
go vet ./src/...
docker compose config -q
git diff --check
```
Expected: PASS.

- [ ] **Step 2: Confirm no Current IA rendering changes**

```bash
git diff master...HEAD -- src/setup/ia_views.go src/setup/ui.go src/setup/admin.go docs/CURRENT-IA-UI-SPEC.md
```
Expected: no Scope A UI diff.

- [ ] **Step 3: Push branch and require CI + Security Audit green before merge review.**
