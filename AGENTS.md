# KitsuSync AGENTS.md

## Agent knowledge entry point
- Read `docs/agent/START-HERE.md` before repository work.
- Use `docs/agent/CURRENT-STATE.md` for accepted default-branch state and `docs/agent/plans/active/` for durable active-task state.
- Verify technical claims against the current repository, tests, configuration, CI, PRs, and runtime evidence before relying on chat history or memory.

## Workspace scope
- This workspace is exclusively for KitsuSync: its Go application, backend/frontend, setup wizard, Discord integration, Kitsu/Zou integration required by KitsuSync, Docker/Compose, tests, CI, releases, and documentation.
- Vatler, general Kitsu host/server administration, host firewall/network/SSH/Tailscale administration, Kitsu Server Console, unrelated repositories, and unrelated infrastructure are out of scope. Do not inspect, modify, or operate on them; respond `OUT_OF_SCOPE_FOR_KITSUSYNC` with the best matching workspace (or `unknown`).
- Kitsu/Zou server behavior may be inspected when necessary to diagnose KitsuSync integration, but host-level changes belong to the Kitsu Server workspace unless explicitly authorized as cross-project work.

## Repository truth
- The authoritative repository is `ukyovfx/kitsusync`.
- The current checked-out branch/worktree is authoritative for the task being executed; `master` is authoritative for accepted default-branch state.
- Code, tests, CI, configuration, PR state, and runtime evidence outrank summaries in `docs/agent/` when they disagree.
- Historical local paths, old branch conventions, old Obsidian logs, and chat history are not authoritative project state.

## Purpose
This repo prioritizes runtime safety and beginner onboarding clarity over clever cleanup. Keep changes small, explicit, and reviewable.

## Highest Risk Areas
- Beginner setup flow under `/bot/setup-wizard`, `/bot/setup`, `/bot/admin/setup`
- Auth/session flow under `/bot/login`, `/bot/logout`, `RequireSession`
- Setup APIs under `/api/setup/*`
- Runtime startup and token/bootstrap logic in `src/main.go`

## Hard Guardrails
- Do not change setup flow unless the task explicitly asks for it.
- Do not change auth flow, DB schema, API contracts, Docker structure, or pipeline behavior in cleanup work.
- Do not do massive refactors, broad renames, or autonomous rewrites.
- Do not remove unused setup code unless references, runtime entry points, and tests are checked first.
- If a change affects runtime behavior, call that out explicitly before implementation.

## Preferred Change Style
- Prefer the smallest safe diff.
- Keep changes focused on one purpose.
- Cleanup is incremental, not architectural.
- Preserve beginner UX even when code could be cleaner.

## Required Verification
Run these after meaningful repo changes when applicable:
- `go test ./src/... -count=1 -timeout=120s`
- `go vet ./src/...`
- `docker compose config -q`

Never claim verification was run if it was not.

## Knowledge write-back
- Do not write back merely because something was discussed.
- Record durable active-task state, blockers, verification results, and next actions in `docs/agent/plans/active/` when they must survive the current session.
- Update `docs/agent/CURRENT-STATE.md` only when accepted default-branch project state materially changes or its verification basis needs refresh.
- Move completed durable task plans to `docs/agent/plans/archive/` when useful.
- Keep reusable cross-project AI workflow rules in the separate `AI-Knowledge` repository; do not duplicate them here.
- Do not store secrets, credentials, private keys, session tokens, raw logs, or full chat transcripts in project knowledge files.

## Not Yet
These remain intentionally out of scope unless explicitly requested:
- fully autonomous agents
- auto-merge
- automatic setup rewrites
- autonomous cleanup passes
- self-modifying prompts

## Current IA UI Guardrails
- Before changing Current IA UI, read `docs/CURRENT-IA-UI-SPEC.md`.
- Browser-rendered output is the final source of truth for visual acceptance; tests/build success alone cannot produce a UI PASS.
- Use: implement → focused tests → update/preview 8090 → authenticated browser inspect → repair → re-inspect until acceptance items pass.
- Run expensive full validation only after browser visual acceptance is green.
- When a final UI decision changes, update the canonical spec instead of accumulating conflicting chat-only requirements.

Preferred loop: PLAN → IMPLEMENT → FOCUSED TESTS → UPDATE/PREVIEW 8090 AS NEEDED → AUTHENTICATED BROWSER REVIEW → REPAIR VISUAL FAILURES → REPEAT → FINAL FULL VALIDATION ONCE.

During browser/runtime validation, proactively repair obvious in-scope UI regressions after detecting and inspecting their cause: detect → inspect → fix → focused validation → browser re-check. Keep repairs behavior-preserving and bounded; do not broaden them into redesigns or semantic changes without authorization.

Before handing off any user action, self-audit and self-repair the command, paths, environment assumptions, ordering, security, and rollback behavior. Batch unavoidable user actions; do not use the user as the debugging loop.

## Durable execution rules
- Keep changes bounded to the requested scope and preserve the current dirty worktree and recovery path.
- Continue safe in-scope inspection, implementation, focused validation, and repair without waiting for an intermediate approval.
- Stop only for secrets/passwords, authenticated control-plane input, destructive risk, material architecture/security choices, or a genuine runtime-only blocker.
- Reuse the architecture and validation commands documented in the repository and linked `docs/` specifications; do not duplicate them here.
