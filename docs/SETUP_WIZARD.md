# Setup Flow Reference

This document describes the current operator-facing setup flow for KitsuSync v0.4.0.

The normal setup path is now:

1. Open `/bot/login`
2. Review shared prerequisites in `/bot/admin/bot`
3. Create or manage project routing in `/bot/setup`
4. Use `/bot/admin/projects` only for review/edit of existing project guild assignment
5. Use `/bot/admin/health` for troubleshooting and verification when needed

`/bot/setup-wizard`, Manual Setup, and Setup Status are no longer normal user-facing setup paths.

---

## Main Setup Surface

`/bot/setup` is the primary operator setup and management page.

The current flow is organized as:

| Step | Purpose |
|------|---------|
| Step 1: Bot Settings | Review shared bot/runtime prerequisites |
| Step 2: Project Routing | Select a Kitsu project, routing template, and project-level Discord Server / Guild ID |
| Step 3: Resource Creation | Create the Discord categories, channels, and webhooks for that project |
| Step 4: Test Notification | Confirm delivery after Discord resources are created |

This is the only normal first-time setup path operators should follow.

---

## Shared vs Project-level Settings

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

## What Diagnostics Is For

Use `/bot/admin/health` when you need deeper troubleshooting or verification detail.

Examples:

- confirm the current runtime token/guild/project health snapshot
- inspect setup blockers
- verify Kitsu/Discord connectivity beyond the main flow

Diagnostics is intentionally secondary to `/bot/setup`.

---

## Discord Resource Creation Notes

Project setup in `/bot/setup` creates Discord categories, channels, and webhooks only after the create/confirm step.

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

Those belong to the older setup architecture and should not be treated as the current recommended operator flow for v0.4.0.
