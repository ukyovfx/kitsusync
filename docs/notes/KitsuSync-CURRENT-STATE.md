---
title: "KitsuSync Current State"
status: release-candidate
updated: 2026-06-17
release_gate: pending-verification
---

# KitsuSync Current State

## Release Focus

- target release: `v0.1.0`
- current mode: release candidate hardening
- scope rule: do not expand beyond the existing setup, routing, and operator surfaces

## Implemented Surface

- `/bot/login`: `done`
- `/bot/setup-wizard`: `partial`
- `/bot/setup`: `partial`
- `/bot/admin`: `partial`
- `/bot/admin/users`: `partial`
- `/bot/admin/checkers`: `partial`
- `/api/setup/status`: `partial`
- `/bot/admin/audit`: `v0.2.0`

## Main Risks

- release evidence still needs manual infrastructure checks
- clean-clone bring-up cannot be fully proven without real Kitsu and Discord credentials
- screenshot assets are still manual TODO items
- `sanitized_kitsu_schema.sql` and `sanitized_kitsu_sample.json` were not present in this repo-local review copy

## Next Gate

Release passes only after `docs/notes/KitsuSync-v0.1.0-Release-Gate.md` is marked pass for all must-pass items.

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
