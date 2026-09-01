# KitsuSync

[![Build](https://img.shields.io/badge/build-GitHub%20Actions-blue)](#)
[![Docker](https://img.shields.io/badge/runtime-Docker-2496ED)](#quick-start)
[![License](https://img.shields.io/badge/license-Apache--2.0-green)](#license)
[![Security Policy](https://img.shields.io/badge/security-policy-important)](#contributing-and-security)
[![Release](https://img.shields.io/badge/release-v0.4.4-orange)](#current-baseline-v044-candidate)

KitsuSync is a Kitsu x Discord pipeline bridge for VFX and animation teams. It polls Kitsu, detects task changes, and posts structured Discord notifications with setup and admin tools in the browser.

**Current baseline: v0.4.4 candidate.** KitsuSync is currently focused on small/mid-size CG/VFX and indie animation team workflows. It is not intended to replace enterprise production tracking systems.

## Why This Repo Exists

KitsuSync helps teams that already track production in Kitsu but need Discord to act like an operator-friendly notification surface instead of a manual status relay.

- Poll Kitsu and post only meaningful task changes
- Create and manage Discord routing from the browser
- Keep setup, login, and admin tooling in one service
- Stay deployable with Docker Compose and SQLite

## What It Solves

- Sends Kitsu task updates to Discord without manual copy/paste.
- Routes notifications by project, task type, or the default webhook.
- Helps operators create and manage project-specific Discord channels and webhooks from the browser.
- Keeps lightweight change state in SQLite so only new changes are posted.

## Architecture

```text
Kitsu API
  -> KitsuSync poller
  -> SQLite change tracking
  -> Discord webhook delivery

Browser admin UI
  -> /bot/login
  -> /bot/setup
  -> /bot/admin
```

## Current Baseline (v0.4.4 candidate)

- v0.2.0: Setup Wizard/operator clarity and setup-surface role consistency
- v0.2.1: repository rename/public URL alignment to `ukyovfx/kitsusync`
- v0.3.0: operational hardening (log redaction, side-effect-free Discord test endpoint, partial-failure cleanup hardening)
- v0.3.1: Discord notification message UX refinement
- v0.4.1: release-candidate maintenance update for the v0.4.0 setup/admin flow, documentation alignment, and release asset cache refresh
- v0.4.2: status-aware Discord notification cards, safe User Linking-based mentions, and bounded card rendering
- v0.4.3: safe Kitsu endpoint auto-discovery and validated fresh-initialization URL fallback
- v0.4.4: status-aware notification cards and Production-level Discord notification language selection

For release history, see `CHANGELOG.md`. Per-version release notes should live in GitHub Releases going forward. The current candidate notes are in `RELEASE_NOTES_v0.4.4.md`.

## Limitations (Current)

Current limitations are intentionally conservative:

- Not an enterprise pipeline platform or ShotGrid replacement.
- Discord resource rollback during setup is best-effort, not full orchestration.
- Admin review and System Status surfaces are still required for some recovery paths.
- Setup depends on correct Discord bot permissions and Kitsu reachability.
- Notification routing remains webhook-based.
- Discord channel mutations are restricted to verified KitsuSync-owned Production resources and fail closed on stale or ambiguous ownership.
- SQLite is suitable for lightweight/small deployments, not large multi-node scale-out.

For production use, see `docs/SETUP_FOR_STUDIOS.md` and verify routing and operational load against your expected notification volume.

## Roadmap
The following items are tracked as future improvement areas:

- Project-scoped multi-project admin management
- Additional audit and diagnostics improvements
- Safer delete-and-recreate handling for existing Discord channel layouts
- Direct Kitsu task deep links in notifications
- UI controls for `@here` routing and broader mention policies
- More explicit setup dry-run and confirmation workflows
- Screenshot asset completion for public marketing/release pages

If you need these capabilities, keep them in roadmap planning as explicit scoped changes rather than ad-hoc local patches.

## UI Surfaces

- `/bot/login` — admin sign-in
- `/bot/setup` — primary setup and project routing surface
- `/bot/admin/bot` — shared bot/runtime prerequisites and token rotation
- `/bot/admin/projects` — review/edit for existing project-to-guild assignment
- `/bot/admin/health` — single System Status surface for status, verification, and diagnostics
- `/bot/admin` — operational dashboard: system health, active projects, warnings
- `/bot/admin/users` — map Kitsu users to Discord IDs for @mentions

Screenshot placeholders live in `screenshots/README.md`. Capture guidance lives in `screenshots/CAPTURE_GUIDE.md`.

## Getting Started

See `docs/QUICK_START.md` for a 5-minute startup guide.
See `docs/SETUP_WIZARD.md` for the current Project Management-first setup flow reference.

## Repository Layout

```text
docker-compose.yml        App + optional FileBrowser(debug profile only)
.env.example              Environment variable template
conf.toml.example         App configuration template
src/                      Go application source
tpl/                      Discord message templates
docs/                     Current development, operations, and release procedures
```

## Requirements

- Docker
- Docker Compose
- A reachable Kitsu server
- A Discord server where you can create webhooks
- A reverse proxy in production if you want `/bot/*` under the same public host

For the Discord bot, use the `bot` OAuth2 scope with `Manage Channels` and `Manage Webhooks`. Administrator is not required. Keep Presence Intent and Message Content Intent off. Enable Server Members Intent when using User Linking, because that screen reads the Guild member list to offer Discord users for linking.

## Quick Start

### 1. Clone and prepare config

```bash
git clone https://github.com/ukyovfx/kitsusync.git
cd kitsusync
cp .env.example .env.local
cp conf.toml.example conf.toml
mkdir -p data
```

### 2. Fill in `.env.local`

KitsuSync can start without Kitsu or Discord credentials. In that case `/health` stays available and the Web UI runs in setup-required mode with polling and notifications paused.

For browser-first setup:

1. Start the app with secret fields empty.
2. Open `/bot/login` and enter the Kitsu URL plus a Kitsu manager/admin account.
3. Open `/bot/setup` and configure the dedicated Kitsu runtime connection.
4. Configure the Discord Bot Token in Bot Settings, then assign Guild IDs during production setup.

Existing installations may continue supplying `KITSU_HOSTNAME`, `KITSU_RUNTIME_EMAIL`, and `KITSU_RUNTIME_PASSWORD` through environment/config files. Browser admin sessions remain separate from background runtime authentication.

If you want a default catch-all Discord route before project setup, also set:

- `DISCORD_WEBHOOK_URL`

### 3. Review `conf.toml`

`conf.toml.example` already points secret values at environment variables. Common values to review:

- `kitsu.hostname`
- `discord.useThreads`
- `mention.checkerStatuses`
- `mention.artistStatuses`
- `mention.hereStatuses`

### 4. Start the app

```bash
docker compose up -d --build
docker compose ps
docker compose logs -f app
```

Health check:

```bash
curl http://localhost:8090/health
```

Expected response:

```json
{"status":"ok"}
```

The response also reports `runtime.mode` as `setup_required`, `configured`, or `degraded`. Do not expose KitsuSync directly to the public internet; place production deployments behind the documented authenticated reverse-proxy boundary.

If you want a quick release-readiness sanity check after boot:

```bash
docker compose ps
docker compose logs --tail=50 app
curl http://localhost:8090/health
```

For the temporary GCP maintenance stack, use a stricter deploy order: `docker compose build app` first, then `docker compose up -d --force-recreate app`. Avoid overlapping build and recreate steps. See `docs/ENVIRONMENTS.md` for the current backup-first verification flow.

## First-Time Setup Flow

### Direct local access

- Login page: `http://localhost:8090/bot/login`
- Project Management: `http://localhost:8090/bot/setup`
- Bot Settings: `http://localhost:8090/bot/admin/bot`
- System Status: `http://localhost:8090/bot/admin/health`
- Admin: `http://localhost:8090/bot/admin`

### Behind a reverse proxy

Use the public `/bot/*` paths exposed by your proxy, for example:

- `/bot/login`
- `/bot/setup`
- `/bot/admin/bot`
- `/bot/admin/health`
- `/bot/admin`

### Operator checklist

1. Open `/bot/login` and sign in with your Kitsu manager or admin account.
2. Review shared bot/runtime prerequisites in `/bot/admin/bot`.
3. Open `/bot/setup` and complete the seven-step Production connection flow: Prerequisites, Production, Discord Server, Channel Plan, Review, Execute, and Complete.
4. Enter a Discord Server / Guild ID per project during `/bot/setup` Step 2. This is required for new project routing.
5. Use `/bot/admin/projects` only for review/edit of existing project guild assignment.
6. If setup fails after partial Discord provisioning, rollback is best-effort and manual cleanup may still be required before retrying.
7. Review routing and user mappings in `/bot/admin`, use `/bot/admin/health` for status and remediation details, then watch polling logs such as `Connected to Kitsu`, `Got tasks`, and `Done FilterTasks`.

## Environment Variables

See `.env.example` for the full template. Copy it to `.env.local` (development) or `.env.production` (production) — never commit these files to git.

### Required for most installs

- `KITSU_HOSTNAME`
- `DISCORD_BOT_TOKEN`

### Required if you want polling to work immediately on first boot

- `KITSU_RUNTIME_EMAIL`
- `KITSU_RUNTIME_PASSWORD`

### Optional

- `DISCORD_GUILD_ID` (legacy/shared fallback only; new project setup uses project-level Guild ID)
- `DISCORD_WEBHOOK_URL`
- `FB_USERNAME` (debug profile only)
- `FB_PASSWORD` (debug profile only — generate with: `openssl rand -base64 20`)

## Debug vs Production

### Production defaults

- `app` starts by default.
- `editor` does not start by default.
- Runtime secrets must stay in `.env.production` or your deployment secret store. Never commit secret files to git.
- The app is expected to run behind a trusted reverse proxy if you expose `/bot/*` publicly.

### Debug profile

Start FileBrowser only when you explicitly need it:

```bash
docker compose --profile debug up -d editor
```

FileBrowser is for local/debug inspection only.

- It is not recommended for production.
- It does not mount `.env`.
- It does not mount `conf.toml`.
- It does not mount `sqlite.db`.
- It does not mount runtime credential storage.

## Routing Behavior

Notification delivery uses the Production-centered model. A normal operator manages one selected Production from Connected Productions (`/bot/admin/projects?project=<production-id>`); the older routing URL is compatibility-only.

The notification route identity is the stable Production ID plus stable Kitsu Task Type ID. A connected Production alone is not ready: every enabled route must resolve to a verified Discord text channel destination in that Production's linked Discord server. Unmatched, paused, stale, incomplete, cross-server, or ambiguous routes fail closed and are diagnosed without dispatch.

Legacy notification fallback priority is:

1. Project/task-type webhook records created by `/bot/setup`
2. `[[discord.productions]]`
3. `[[discord.taskTypeWebhooks]]`
4. `discord.webhookURL` / `DISCORD_WEBHOOK_URL` as the main fallback

If no fallback webhook is configured for unmatched tasks, the app now logs the drop explicitly instead of silently swallowing it.

## Production Notes

- Cookies are hardened for HTTPS and trusted reverse proxy operation.
- `SameSite=Lax` is intentionally used to preserve login redirect flows.
- `X-Forwarded-Proto` is trusted only when your reverse proxy overwrites it.
- Do not expose the app directly to the internet without a properly configured reverse proxy.
- Large first-time syncs can temporarily hit Discord rate limits. For an existing Kitsu with many tasks, start with `ignoreMessagesDaysOld = 7` and widen it after setup is stable.
- If nginx proxies `/bot/`, set `proxy_read_timeout 300;` so long Discord setup calls do not appear as false browser-side 504 failures.

Example `/bot/` nginx snippet:

```nginx
location /bot/ {
    proxy_pass http://127.0.0.1:8090;
    proxy_read_timeout 300;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

## Preview Images

Discord preview thumbnails require Kitsu preview files to be reachable without interactive browser auth. If your Kitsu deployment protects preview files differently, add a reverse-proxy rule for the preview endpoint.

Example nginx snippet:

```nginx
location ~ ^/api/pictures/thumbnails/preview-files/ {
    proxy_pass http://localhost:5000;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

## Troubleshooting

### `Initial Kitsu authentication failed`

- Check `KITSU_HOSTNAME`
- Check `KITSU_RUNTIME_EMAIL`
- Check `KITSU_RUNTIME_PASSWORD`
- Or complete shared bot/runtime setup from `/bot/admin/bot`

### Notifications are not arriving

- Check `docker compose logs -f app`
- Check `DISCORD_WEBHOOK_URL`
- Check `/bot/admin` routing
- Check the new notification routing warnings in the logs

### Setup failed partway through

- The setup screen now reports failure explicitly.
- Partial Discord resources are rolled back on a best-effort basis.
- Re-run setup only after reading the returned `FAIL:` / `WARN:` lines.

### FileBrowser is not reachable

- This is expected unless you started the debug profile explicitly.

## Documentation

User documentation is maintained at https://rigoo.jp/.

- Template variables and custom preset guide: `docs/TEMPLATES.md`
- Dev vs production environment setup: `docs/ENVIRONMENTS.md`
- First-time studio setup walkthrough: `docs/SETUP_FOR_STUDIOS.md`
- Error messages and System Status troubleshooting: `docs/TROUBLESHOOTING.md`
- Authenticated release E2E: `docs/V044-AUTHENTICATED-E2E.md`

## Contributing and Security

- Contributor guide: `CONTRIBUTING.md`
- Security reporting: `SECURITY.md`
- Changelog: `CHANGELOG.md`
- Latest release notes: `RELEASE_NOTES_v0.4.4.md`
- Screenshot guidance: `screenshots/CAPTURE_GUIDE.md`
- Future per-version release notes: GitHub Releases

## License

Apache License 2.0. See [`LICENSE`](LICENSE).

This project is a fork of [keshon/kitsu-to-discord-task-notification](https://github.com/keshon/kitsu-to-discord-task-notification) (Apache 2.0). Keep the upstream copyright and `NOTICE` (if any) when redistributing.
