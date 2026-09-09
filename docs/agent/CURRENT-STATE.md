# Current state

## Verification basis

Verified against `master` at commit `92569e4b7f4623ba19a7760404b5bbb4a11fa385` (`v0.4.3`). The working tree was clean when inspected.

## Confirmed stable areas

- KitsuSync is a Go Kitsu-to-Discord pipeline bridge with browser setup and administration surfaces.
- The documented runtime uses Docker Compose, SQLite change tracking, Kitsu polling, and Discord webhook delivery.
- The repository contains application code under `src/`, templates under `tpl/`, deployment/configuration examples, documentation under `docs/`, and CI under `.github/workflows/`.
- The CI workflow targets `master` and defines Go, Docker Compose configuration, and Docker build checks.

## Known confirmed limitations

- The README describes the current baseline as a v0.4.3 candidate and states that SQLite is intended for lightweight/small deployments rather than large multi-node scale-out.
- Discord setup rollback is documented as best-effort; setup depends on correct Discord permissions and Kitsu reachability.
- Production deployment is documented behind a trusted reverse proxy, and the FileBrowser service is debug-only.

## Active work

No active task files are present in `docs/agent/plans/active/`.
