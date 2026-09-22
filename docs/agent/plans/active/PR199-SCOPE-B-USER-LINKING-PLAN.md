# Scope B User Linking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate Scope A into Current IA live reads and make User Linking render truthful prerequisite, empty, failure, selection, and ready states.

**Architecture:** Current IA views consume the runtime/API interfaces from Scope A. Production and User Linking rendering own only presentation/state orchestration; credential resolution and raw request semantics remain in Scope A.

**Tech Stack:** Go server-rendered HTML, existing Current IA CSS/JS, httptest, authenticated browser acceptance.

**Spec:** `docs/agent/plans/active/PR199-SCOPE-SPLIT-DESIGN.md`

## Global Constraints

- Start from accepted `master` after Scope A merges, or temporarily stack on the exact reviewed Scope A head.
- Follow `docs/CURRENT-IA-UI-SPEC.md`; browser output is final UI acceptance.
- No DB schema, auth/session, setup-wizard, or Connections token-editing changes.
- Never render secrets, raw Authorization data, response bodies, or unnecessary internal IDs.
- Use TDD for every state transition.

## Review Focus

- Missing configuration must not look like an API failure.
- Successful empty Kitsu/Discord data must not look like request failure.
- Multiple guilds with no selection must not render mapping rows.
- Untrusted/invalid guild query input must not be reflected unsafely.
- JP/EN state order/actions must remain equivalent.

---

### Task 1: Wire live Production reads to Scope A

**Files:**
- Modify: `src/setup/ia_views.go`
- Test: `src/setup/ia_views_test.go` or a focused new test file

**Interfaces:**
- Consumes: `runtimeKitsuDataSource`, `ListKitsuProjectsWithCredentials`, `GetProjectTaskTypesWithCredentials`.
- Produces: `availableProjectsWithError(db *gorm.DB) ([]model.Project, error)` while preserving `availableProjects` compatibility.

- [ ] **Step 1: Write failing tests**

Cover persisted runtime token with `KitsuJWTToken` unset, successful empty live Projects, and live lookup failure that preserves local saved Productions plus a non-nil safe error.

- [ ] **Step 2: Verify RED**

```bash
go test ./src/setup -run 'TestPR199AvailableProjects' -count=1
```
Expected: FAIL because current view code depends on env credentials and collapses live failure.

- [ ] **Step 3: Implement minimal integration**

Keep `availableProjects` as a compatibility wrapper. Use Scope A source/API interfaces, merge local/live data exactly as before, and return the live lookup error separately.

- [ ] **Step 4: Verify GREEN**

Run focused tests and relevant Production view tests.

- [ ] **Step 5: Commit**

```bash
git add src/setup/ia_views.go src/setup/*test.go
git commit -m "feat: use persisted runtime source for Current IA reads"
```

### Task 2: Define User Linking state tests before rendering changes

**Files:**
- Test: `src/setup/ia_views_test.go` or focused new `src/setup/pr199_user_linking_test.go`

**Interfaces:**
- Consumes: Scope A lookup helpers.
- Produces: state expectations for `/bot/admin/users`.

- [ ] **Step 1: Write failing tests for the canonical states**

Test at minimum:
1. missing Kitsu + Discord prerequisites;
2. Kitsu lookup failure;
3. Discord lookup failure;
4. zero joined guilds;
5. multiple guilds with no selection;
6. selected guild with zero human members;
7. zero Kitsu users;
8. populated ready table;
9. invalid `discord_guild_id` query is ignored/canonicalized;
10. JP/EN readiness parity.

Assertions for prerequisite-missing state must ensure no guild selector, mapping table, failure diagnostics, or misleading API-error copy is rendered.

- [ ] **Step 2: Verify RED**

```bash
go test ./src/setup -run 'TestPR199UserLinking' -count=1
```
Expected: FAIL against current rendering behavior.

### Task 3: Implement bounded state orchestration

**Files:**
- Modify: `src/setup/ia_views.go`
- Modify only if needed: `src/setup/ui.go`

**Interfaces:**
- Produces: canonicalized guild query helper and bounded rendering helpers for setup notice, selector, failure, empty, and populated states.

- [ ] **Step 1: Implement the minimum rendering logic required by Task 2 tests**

Rules:
- prerequisites gate all live directory rendering;
- Kitsu failure takes precedence over Discord state because mapping cannot proceed;
- selector only when guilds exist and selection is meaningful;
- table only when both directories are usable;
- human-facing Discord identity uses safe display name, never raw ID.

- [ ] **Step 2: Verify GREEN**

```bash
go test ./src/setup -run 'TestPR199UserLinking|TestGlobalUserLinking' -count=1
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add src/setup/ia_views.go src/setup/ui.go src/setup/*test.go
git commit -m "feat: separate User Linking readiness states"
```

### Task 4: Update canonical Current IA contract

**Files:**
- Modify: `docs/CURRENT-IA-UI-SPEC.md`
- Modify: `docs/CURRENT-IA-UI-ACCEPTANCE.md`

- [ ] **Step 1: Document only the final accepted behavior implemented above**

Add the User Linking readiness/failure-state contract and acceptance checks. Do not document intermediate implementation details.

- [ ] **Step 2: Verify docs match rendered state names and JP/EN actions.**

- [ ] **Step 3: Commit**

```bash
git add docs/CURRENT-IA-UI-SPEC.md docs/CURRENT-IA-UI-ACCEPTANCE.md
git commit -m "docs: define User Linking readiness states"
```

### Task 5: Browser and full verification

- [ ] **Step 1: Run focused/full automated checks**

```bash
go test ./src/... -count=1 -timeout=120s
go vet ./src/...
docker compose config -q
git diff --check
```
Expected: PASS.

- [ ] **Step 2: Authenticated browser acceptance**

At preview/runtime 8090, inspect JP and EN for prerequisite missing, multiple guild selection, empty data, genuine failure, and populated ready table. Verify responsive behavior and absence of raw IDs/secrets.

- [ ] **Step 3: Repair bounded UI regressions and re-run focused tests/browser review until green.**

- [ ] **Step 4: Require CI + Security Audit green before merge review.**
