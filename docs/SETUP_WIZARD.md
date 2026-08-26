# Setup Flow Reference

This document describes the current operator-facing setup flow for KitsuSync v0.4.1.

The normal setup path is now:

1. Start KitsuSync; missing runtime credentials enter setup-required mode instead of stopping the process
2. Open `/bot/login`, enter the Kitsu URL when requested, and authenticate as a Kitsu manager/admin
3. Configure the dedicated Kitsu runtime connection in `/bot/setup`
4. Configure Discord in `/bot/admin/bot`, then create or manage each Production's linked Discord Guild and Task Type channels in `/bot/admin/projects` (Connected Productions)
5. Use `/bot/admin/projects` only for review/edit of existing project guild assignment
6. Use `/bot/admin/health` for troubleshooting and verification when needed

`/bot/setup-wizard`, Manual Setup, and Setup Status are no longer normal user-facing setup paths.

---

## Main Setup Surface

`/bot/setup` is the primary operator setup and management page.

The current flow is organized as:

| Step | Purpose |
|------|---------|
| Step 1: Prerequisites | Review shared Kitsu and Discord Bot readiness |
| Step 2: Production | Select the Kitsu Production to connect |
| Step 3: Discord Server | Select the project-level Discord Server / Guild ID |
| Step 4: Channel Plan | Review Task Type channels, destinations, and order |
| Step 5: Review | Confirm the complete plan before execution |
| Step 6: Execute | Create or reuse Discord resources and save routing |
| Step 7: Complete | Verify the saved connection and open the Production |

This is the only normal first-time setup path operators should follow.

## Runtime states

- `setup_required`: the Web UI and `/health` are available, Kitsu is disconnected, and polling/notifications are paused.
- `configured`: the dedicated runtime credentials were validated and polling may run; this does not by itself mean Discord notifications are ready.
- `degraded`: a refresh or temporary Kitsu connection attempt failed; the process and saved configuration remain available for recovery.

The Kitsu manager/admin browser session is held in process memory and authorizes setup pages. Background polling uses a separate dedicated runtime account and never reuses the browser session JWT. The runtime password is encrypted before SQLite storage; the encryption key is stored separately under the ignored `data/` runtime directory. Keep both files protected and backed up together.

---

## Shared vs Project-level Settings

Notification readiness is evaluated independently: Kitsu configured, Kitsu connected, Kitsu ready, Discord bot configured, Discord API validated, Production linked to one Guild, Task Type channels configured, and routing enabled. Overall notification readiness is available only when all required checks pass. A connected Production alone is not configured routing.

Each Production maps to one Discord Guild. Kitsu Task Types are the stable routing identities and are proposed as deterministic text-channel names (`lowercase`, separator normalization, Discord-safe characters, repeated-separator collapse, trimming, and length limiting). Existing channels are reused only when they are exact valid matches in the selected Guild. Collisions, stale references, cross-Guild mappings, and ambiguous ownership fail closed and require review.

### Shared Bot / Runtime settings

Use `/bot/admin/bot` for shared prerequisites:

- Discord Bot Token
- Kitsu hostname
- Kitsu runtime email
- Kitsu runtime password

These settings are shared across projects.

### Project-level destination settings

Use `/bot/setup` for project destination setup:

- Kitsu project selection
- project template/type
- notification language
- Discord Server / Guild ID

Guild ID is now treated as a project-level destination setting for new project routing and is handled in `/bot/setup` Step 2.

---

## What `/bot/admin/projects` Is For

`/bot/admin/projects` remains available, but its role is narrower:

- review existing project-to-guild assignment
- edit existing project guild assignment
- inspect already-created project routing

It is no longer the main required place to enter Guild ID for new project setup.

---

## What System Status Is For

Use `/bot/admin/health` when you need deeper troubleshooting or verification detail.

Examples:

- confirm the current runtime token/guild/project health snapshot
- inspect setup blockers
- verify Kitsu/Discord connectivity beyond the main flow

System Status is intentionally secondary to `/bot/setup`.

---

## Discord Resource Creation Notes

New Connection Setup fetches the selected Production's Task Types and shows a complete create/reuse/conflict plan before any Discord write. Discord channel creation is limited to the selected Guild and only missing channels listed in the confirmed plan. Connected Productions is the normal surface for reviewing mappings, previewing missing channels, pausing/resuming notifications, dry-run, and diagnosis. Compatibility route `/bot/admin/production-routing` redirects there and remains available only for old bookmarks.

If setup fails partway through:

- read the returned `FAIL:` and `WARN:` lines carefully
- treat rollback as best-effort
- verify partial Discord resources manually before retrying when necessary

---

## Legacy Route Note

Historical docs and older releases may reference:

- `/bot/setup-wizard`
- Manual Setup
- Setup Status

Those belong to the older setup architecture and should not be treated as the current recommended operator flow for v0.4.1.
