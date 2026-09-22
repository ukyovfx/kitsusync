# Scope C Connections Token UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make saved Kitsu/Discord token replacement visible, masked, and cancellable without changing secret persistence or validation semantics.

**Architecture:** Keep server-side credential handling unchanged. Adjust only the Connections edit renderer and its client-side interaction so configured secrets render as disabled masked password fields, `Change token` switches to a fresh empty editable field, and `Cancel` restores the safe configured state.

**Tech Stack:** Go server-rendered HTML, small inline browser JS, existing setup test fixtures, authenticated browser acceptance.

**Spec:** `docs/agent/plans/active/PR199-SCOPE-SPLIT-DESIGN.md`

## Global Constraints

- Branch directly from current accepted `master`.
- No runtime credential storage, validation, DB, auth/session, setup wizard, or Current IA User Linking changes.
- Saved secret values must never appear in HTML, JS, logs, placeholders, or diagnostics.
- Keep existing fixed mask convention and one service-specific save/recheck action.
- Use TDD before renderer changes.

## Review Focus

- Stored secret strings must never appear in output.
- Cancel must not submit or preserve a typed replacement value.
- Kitsu and Discord controls must remain independent.
- Existing manual endpoint/expert network controls must retain behavior.
- External Kitsu URL helper copy must appear once, with Check link retained.

---

### Task 1: Pin configured-token rendering behavior

**Files:**
- Create test: `src/setup/pr199_connections_token_ux_test.go`
- Existing implementation target: `src/setup/admin.go`

**Interfaces:**
- Consumes: `renderConnectionsEditFormWithIdentityRows` and existing runtime-secret test helpers.
- Produces: exact safe rendering contract for configured Kitsu/Discord token fields.

- [ ] **Step 1: Write failing renderer test**

Create a DB fixture with saved Kitsu and Discord runtime tokens and render the English/Japanese Connections edit form. Assert:
- both token input IDs remain present;
- configured fields use `placeholder="••••••••••••••••••••"` and are disabled;
- `Change token` and `Cancel` controls exist;
- stored secret strings do not appear;
- the External Kitsu URL explanation appears once per language;
- the compact Check link remains present.

- [ ] **Step 2: Verify RED**

```bash
go test ./src/setup -run 'TestPR199ConnectionsSavedTokenControls' -count=1
```
Expected: FAIL because current configured token inputs are hidden rather than visible masked/cancellable controls.

### Task 2: Implement masked Change/Cancel interaction

**Files:**
- Modify: `src/setup/admin.go`
- Test: `src/setup/pr199_connections_token_ux_test.go`

**Interfaces:**
- Produces: configured token input with disabled fixed mask placeholder; `Change token`; `Cancel`; unchanged service submit semantics.

- [ ] **Step 1: Implement minimum renderer changes**

For each configured service:
- render the password input disabled with the fixed mask placeholder;
- do not set its value;
- `Change token` enables it, clears value/placeholder, shows Cancel, and focuses it;
- `Cancel` clears typed value, disables the input, restores fixed mask placeholder, restores Change token, and hides Cancel;
- do not reveal or fetch the saved secret.

- [ ] **Step 2: Consolidate External Kitsu URL explanation**

Keep the label/input/Check link; move the explanatory sentence to the single Advanced settings explanation and remove the duplicate field-level helper.

- [ ] **Step 3: Verify GREEN**

```bash
go test ./src/setup -run 'TestPR199ConnectionsSavedTokenControls|TestConnectionsEdit' -count=1
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add src/setup/admin.go src/setup/pr199_connections_token_ux_test.go
git commit -m "feat: make saved token replacement reversible"
```

### Task 3: Preserve existing Connections behavior

**Files:**
- Modify only if an assertion needs updating: `src/setup/kitsu_connection_test.go`

- [ ] **Step 1: Run existing Connections tests**

```bash
go test ./src/setup -run 'TestConnections|TestKitsuConnection' -count=1
```
Expected: PASS or fail only where old hidden-input assertions now conflict with the accepted masked control contract.

- [ ] **Step 2: If needed, update stale assertions only**

Replace expectations for `hidden style="display:none"` with the fixed mask/disabled control contract. Do not weaken secret-leak checks.

- [ ] **Step 3: Re-run focused tests**

Expected: PASS.

- [ ] **Step 4: Commit assertion-only adjustment separately if required.**

### Task 4: Browser and full verification

- [ ] **Step 1: Run repository verification**

```bash
go test ./src/... -count=1 -timeout=120s
go vet ./src/...
docker compose config -q
git diff --check
```
Expected: PASS.

- [ ] **Step 2: Authenticated browser acceptance**

Verify both services in JP and EN:
- configured field shows only mask;
- Change token opens an empty editable field;
- typing a replacement then Cancel returns to mask and drops typed value;
- changing one service does not alter the other;
- manual endpoint and expert disclosure still work;
- External Kitsu URL copy is singular and Check link remains.

- [ ] **Step 3: Require CI + Security Audit green before merge review.**
