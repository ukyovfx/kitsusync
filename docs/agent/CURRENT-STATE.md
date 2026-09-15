# Current state

## Verification basis

Verified against upstream `master` at commit `cc9dea22daba83dbc04503f3d87e5fc402d04bd7` on 2026-09-15.

## Confirmed stable areas

- KitsuSync is a Go Kitsu-to-Discord pipeline bridge with browser setup and administration surfaces.
- The documented runtime uses Docker Compose, SQLite change tracking, Kitsu polling, and Discord webhook delivery.
- The repository contains application code under `src/`, templates under `tpl/`, deployment/configuration examples, documentation under `docs/`, and CI under `.github/workflows/`.
- The CI workflow targets `master` and defines Go, Docker Compose configuration, and Docker build checks.
- The repo-local AI knowledge entry points are present on `master`: `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and `docs/agent/plans/`.
- PR #159 is merged, and v0.4.6 is released from commit `b7b30157cb90c4500e8b00d3c26ac7038f5c8c10`.
- PRs #164, #165, #166, and #167 are merged into `master`; #167 is the latest relevant production deploy/rollback hardening change.
- Production deployment is governed by the root-installed `kitsusync-deploy` wrapper and a provenance-checked deployment bundle. Direct production Compose execution is unsupported.

## Known confirmed limitations

- SQLite is intended for lightweight/small deployments rather than large multi-node scale-out.
- Discord setup rollback is documented as best-effort; setup depends on correct Discord permissions and Kitsu reachability.
- Production deployment is documented behind a trusted reverse proxy, and the FileBrowser service is debug-only.

## Active work

- The next production deployment is prepared off-production from the immutable v0.4.6 release artifact and current trusted deployment tooling in `master`.
- Remaining work is controlled production verification: stage and inspect the bundle, verify the target runtime and rollback evidence, then obtain operator approval before the first production write.
- F02 remains intentionally deferred pending production evidence; it is not converted into an implementation task.
- PR #162 is open and is not a production blocker for this deployment path.
