---
title: "KitsuSync Current State"
status: maintenance
updated: 2026-06-17
release: v0.4.2 candidate
---

# KitsuSync Current State

## Release Focus

- current release: `v0.4.2 candidate`
- current mode: maintenance with small cleanup and hardening PRs
- scope rule: keep changes small, focused, and easy to verify

## Active Setup Surface

- `/bot/setup`: the only normal setup path
- Bot Settings: shared bot/runtime prerequisites
- Guild ID: project-level and required in classic `/bot/setup` Step 2
- Diagnostics: kept for troubleshooting
- legacy setup wizard, Manual Setup, Setup Status, and Guided Setup are removed from the normal setup path

## Current Deployment State

- temporary GCP stack has been deployed and smoke-tested through PR #100 and PR #101
- latest tested deployed commit: `01e5c823f54febe490c21264e2ce0e39d175b92a`
- PR #101 records the PR #100 deploy QA result in this file
- `docs/archive/diagrams/` retains the legacy source fragments, and they are no longer runtime-served or mounted

## Known Caveats

- authenticated visual QA has not been performed
- temporary GCP deploys require careful DB, logs, and dump backup before container recreation
- `docker compose build` showed a `buildx isn't installed` warning during the latest temporary deploy, but the deploy still succeeded

## Next Gate

- keep future work focused on small cleanup and hardening PRs
- keep `docs/archive/diagrams/` as retained legacy source fragments unless a later cleanup explicitly decides otherwise

## 2026-06-17 Maintenance Update

- PR #98 was merged and deployed to the temporary GCP stack.
- deployed `master` commit: `e17843b26581cb6d3986989e2e443352bb80371d`
- previous live commit: `2fd9ba9296564facaa812a64486a0098800225b4`
- smoke QA passed:
  - `/health` -> `{"status":"ok"}`
  - anonymous `/bot/admin` -> `303 See Other` to `/bot/login?lang=ja&next=%2Fbot%2Fadmin`
  - `/bot/docs` -> `200 OK`
  - `/diagrams/` -> `404 Not Found`
- DB backup/restore matched before and after:
  - hash: `039d708304812740b688e1fd97ea29a018f0abb06c731a5eea21c8670a070972`
  - size: `163840` bytes
- backups:
  - `/home/ukyovfx/kitsu-discord-custom/backups/20260617-105124/sqlite.db`
  - `/home/ukyovfx/kitsu-discord-custom/backups/20260617-105124/logs`
  - `/home/ukyovfx/kitsu-discord-custom/backups/20260617-105124/dump`
- known temporary GCP deploy caveat:
  - after container recreation, missing `/app/logs/all-levels.log` caused a restart loop
  - restoring the backed-up runtime logs fixed the live stack
- authenticated visual QA was not performed

## 2026-06-17 Maintenance Update (PR #100)

- PR #100 was merged and deployed to the temporary GCP stack.
- deployed `master` commit: `01e5c823f54febe490c21264e2ce0e39d175b92a`
- previous live commit: `e17843b26581cb6d3986989e2e443352bb80371d`
- expected latest `master` matched the deployed commit
- DB hash/size stayed unchanged before and after deploy:
  - hash: `039d708304812740b688e1fd97ea29a018f0abb06c731a5eea21c8670a070972`
  - size: `163840` bytes
- smoke QA passed:
  - `/health` -> `{"status":"ok"}`
  - anonymous `/bot/admin` -> `303 See Other`
  - `Location` -> `/bot/login?lang=ja&next=%2Fbot%2Fadmin`
  - `/bot/docs` -> `200 OK`
  - `/diagrams/` -> `404 Not Found`
- missing-log startup hardening verified:
  - recreated the container with the new image
  - restored live DB and dump before startup, but did not restore logs
  - container did not restart-loop
  - `/app/logs` and `/app/logs/all-levels.log` were automatically created
  - file status: `LOG_DIR=present`, `LOG_FILE=present`
  - container status: `running|0|healthy`
- warnings:
  - `docker compose build` printed a `buildx isn't installed` warning, but build/deploy succeeded
  - dump backup directory permissions required checking the backup artifact side for size
- authenticated visual QA was not performed

## 2026-06-17 Maintenance Update (PR #107 / #108)

- docs maintenance cleanup continued without changing runtime behavior
- `docs/ENVIRONMENTS.md` now documents the temporary GCP backup-first deploy behavior, including the live container DB/logs/dump backup caveat
- `docs/notes` historical metadata is now clearly marked as historical background rather than active release-state guidance
- current user-facing setup and deploy docs are now broadly aligned with the v0.4.2 candidate state
