# Current state

## Verification basis

Verified against `master` at commit `71cdded7b61f11d154826fed5e3e5b3eae540ee8`.

## Confirmed stable areas

- KitsuSync is a Go Kitsu-to-Discord pipeline bridge with browser setup and administration surfaces.
- The documented runtime uses Docker Compose, SQLite change tracking, Kitsu polling, and Discord webhook delivery.
- The repository contains application code under `src/`, templates under `tpl/`, deployment/configuration examples, documentation under `docs/`, and CI under `.github/workflows/`.
- The CI workflow targets `master` and defines Go, Docker Compose configuration, and Docker build checks.
- The repo-local AI knowledge entry points are present on `master`: `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and `docs/agent/plans/`.

## Known confirmed limitations

- SQLite is intended for lightweight/small deployments rather than large multi-node scale-out.
- Discord setup rollback is documented as best-effort; setup depends on correct Discord permissions and Kitsu reachability.
- Production deployment is documented behind a trusted reverse proxy, and the FileBrowser service is debug-only.

## Active work

- v0.4.4 release candidate work remains active in PR #159 (`codex/v0.4.4-notification-card`).
- Durable task state is tracked in `docs/agent/plans/active/V0.4.4-RELEASE.md`.
- The active PR is not accepted default-branch state until it is merged.
