# Current state

## Verification basis

Verified against GitHub `master` at `5c01e30ff222f332656a5a563e76a4741b0ad9b1` on 2026-09-28. Post-merge CI #648 and Security Audit #231 both completed successfully for that exact commit.

## Accepted repository and release state

- `master` is the accepted implementation state. Repository, GitHub, CI, configuration, tests, and current runtime evidence take precedence over this summary when they disagree.
- `VERSION` is `0.4.9`. The latest GitHub release is `v0.4.9`, published 2026-09-24 and targeting `22b3a3c9400dad4ef012a570816934d57e736a06`, which is older than current `master`.
- Current `master` has not been released or deployed to Production. Production deployments remain behind the supported release wrapper and explicit approval.
- Recent accepted work on `master`:
  - #219 — Production Team / Reviewer behavior (`8ac6972375ba583cbca8084758a79d3e9673dc11`).
  - #216 — isolated Staging infrastructure (`dcae6bc477af6c9b12353840fc9f41ed8b410850`).
  - #215 — Current IA polish (`cfae399cb3c5e432f52b5d269f3b1b8823668af4`).
  - #221 — Login and app backgrounds (`811b52c9ec825c3757923f37ff25d23b87a8fe08`).
  - #224 — favicon fix (`22f174bceb4c4730c22e8ec47b5d56b3f3366372`).
  - #223 — Login fabric balance (`61ab09e84a9cb9cfc557352afdb61ce5f5f8e3e4`).
  - #222 — Staging candidate PR resolution / provenance fix (`3bcf3faea70e9df687c6f46c6859920f944b6c3b`).
  - #225 — horizontal Login ribbon, restrained mobile ribbon, User Linking simplification, and System Status graph/disclosure polish; merged as current `master` (`5c01e30ff222f332656a5a563e76a4741b0ad9b1`).

## Staging boundary

- The accepted Staging helper contract is `staging-v8`. The latest operator-reported installed helper SHA-256 is `c2e8b1b8dda50357909e2b5f8081560c262cff30e0107958ab85b9366a726490`.
- Staging is a separate Compose project and runtime bound to `127.0.0.1:8091`, with isolated data/config/session/runtime state and polling disabled. Production remains on its separate release-only path at `127.0.0.1:8090`.
- The routine candidate path is `scripts/deploy-kitsusync-staging-candidate.ps1 -CommitSha <sha>`. It checks exact-SHA CI/Security and immutable artifact provenance, then uses the installed stable helper; it does not upgrade infrastructure or target Production.
- Repository docs describe optional automated Staging deployment as configuration-gated. This architecture description does not assert which candidate is currently running in Staging; verify the runtime when a deployment decision depends on it.

## Production evidence and boundary

- No current Production deployment identity is asserted by this document. The current master has not been deployed there.
- Historical read-only review evidence reported in the 2026-09-28 work context recorded revision `3d4dafc7d75658a5877d2e2332266c8e6145163d`. This is dated historical context, not proof of the current Production runtime or a deployment of accepted `master`.
- Production deployment and release remain explicitly gated. Verify the live runtime and exact release artifact at the time of any separately authorized operation.

## Current work

- PR #220 is closed as superseded and must not be revived or reused.
- The GitHub open-PR list was checked on 2026-09-28 and contained no open PRs.
- No next product implementation task is currently assigned here. Select future work from an explicit project priority rather than inferring one from this state note.

## Repository guidance

- `docs/agent/START-HERE.md` points to this file for accepted default-branch state and to `plans/active/` for active task plans; both pointers remain current. The active plans directory currently contains only `.gitkeep`.
- Current IA UI decisions remain in `docs/CURRENT-IA-UI-SPEC.md`; browser-rendered output is still the final source of truth for visual acceptance.
- Live Kitsu, Discord, Staging, or Production status must be checked directly when relevant. A merged PR, CI result, or historical runtime observation does not establish present service behavior.
