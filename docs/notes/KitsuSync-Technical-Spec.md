---
title: "KitsuSync Technical Spec"
status: release-candidate
updated: 2026-05-19
release_gate: pending-verification
---

# KitsuSync Technical Spec

Historical note: this document preserves the v0.1.0 release-candidate technical scope. For the current v0.4.0 operator-facing setup flow, use `docs/SETUP_WIZARD.md` and `docs/notes/KitsuSync-CURRENT-STATE.md`.

## v0.1.0 Scope Vocabulary

- `done`: in scope and implemented
- `partial`: implemented but still needs verification or release proof
- `v0.2.0`: intentionally deferred to roadmap
- `human-check`: must be confirmed against real infrastructure

## Scope Status

- auth/session flow: `partial`
- `/bot/login`: `done`
- `/bot/setup-wizard`: `partial`
- `/bot/setup`: `partial`
- `/bot/admin/users`: `partial`
- `/bot/admin/checkers`: `partial`
- notification routing core: `partial`
- logging redaction: `partial`
- release documentation: `partial`
- admin/audit browser UI: `v0.2.0`

## Release-Critical Constraints

- no secrets or real credentials in git
- no production DB access during release verification
- no destructive migration scripts
- no scope expansion beyond v0.1.0 surfaces
- any missing admin/audit workflow goes to `v0.2.0`, not to this branch
