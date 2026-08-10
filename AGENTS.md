# KitsuSync AGENTS.md

## Purpose
This repo prioritizes runtime safety and beginner onboarding clarity over clever cleanup. Keep changes small, explicit, and reviewable.

## Repo Truth
- Treat the GCP repo at `/home/ukyovfx/kitsu-discord-custom/app` as the source of truth.
- Keep work split by branch purpose:
  - `feature/v0.1.0-release-gate-final` for release hardening
  - `feature/v0.1.0-uiux-polish` for onboarding and setup UX polish
  - `feature/v0.1.0-cleanup-phase1` for low-risk cleanup only
- Do not mix release gate, UIUX, and cleanup into one PR.

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
Run these after meaningful repo changes:
- `go test ./src/... -count=1 -timeout=120s`
- `go vet ./src/...`
- `docker compose config -q`

## Decision Logging
- Leave rationale for risky or scope-limiting decisions in commit messages, PR text, or the Obsidian knowledge log.
- Continue append-only updates in `Obsidian_ukyo/02_Domains/KitsuSync/log.md` for release, UIUX, and cleanup milestones.

## Not Yet
These are intentionally out of scope for now:
- fully autonomous agents
- auto-merge
- automatic setup rewrites
- autonomous cleanup passes
- self-modifying prompts
- orchestration automation

## Current IA UI Guardrails
- Before changing Current IA UI, read `docs/CURRENT-IA-UI-SPEC.md`.
- Browser-rendered output is the final source of truth for visual acceptance; tests/build success alone cannot produce a UI PASS.
- Use: implement → focused tests → update/preview 8090 → authenticated browser inspect → repair → re-inspect until acceptance items pass.
- Run expensive full validation only after browser visual acceptance is green.
- When a final UI decision changes, update the canonical spec instead of accumulating conflicting chat-only requirements.

Preferred loop: PLAN → IMPLEMENT → FOCUSED TESTS → UPDATE/PREVIEW 8090 AS NEEDED → AUTHENTICATED BROWSER REVIEW → REPAIR VISUAL FAILURES → REPEAT → FINAL FULL VALIDATION ONCE.
