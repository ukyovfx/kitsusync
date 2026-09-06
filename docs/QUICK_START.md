# Quick Start

Get KitsuSync running in about 5 minutes.

---

## Prerequisites checklist

Before you begin, confirm you have:

- [ ] Docker and Docker Compose installed on the server
- [ ] Kitsu running and reachable from the server (test: `curl http://YOUR_KITSU_HOST/api/`)
- [ ] Discord server where the bot can be granted the required permissions
- [ ] Discord **Bot Token** — create one at [discord.com/developers/applications](https://discord.com/developers/applications) (Bot tab → Reset Token)
- [ ] One or more Discord **Guild IDs** — enable Developer Mode in Discord settings, then right-click each target server → Copy Server ID
- [ ] A Kitsu manager/admin account for the browser setup flow

> **Bot permissions required:** Manage Channels and Manage Webhooks. Administrator is not required. Keep Presence Intent and Message Content Intent off. Enable Server Members Intent when using User Linking, because KitsuSync reads the Guild member list to offer Discord users for linking.
> Generate the invite URL from OAuth2 → URL Generator with scope `bot` and those two permissions.

---

## Step 1 — Clone and configure

```bash
git clone https://github.com/ukyovfx/kitsusync.git
cd kitsusync
cp .env.example .env.local
```

For browser-first setup, secret values may remain empty. Existing configured deployments may continue providing:

```env
KITSU_HOSTNAME=http://YOUR_KITSU_HOST/        # include http:// and trailing slash
KITSU_RUNTIME_EMAIL=bot@yourstudio.com
KITSU_RUNTIME_PASSWORD=your-runtime-password

DISCORD_BOT_TOKEN=your-bot-token
DISCORD_GUILD_ID=optional-fallback-server-id
```

Leave `DISCORD_WEBHOOK_URL` empty for now — Project Management sets up routing in the browser.

---

## Step 2 — Start the app

```bash
docker compose up -d --build
```

Wait for the app to be ready:

```bash
docker compose logs -f app
```

You should see within a few seconds:

```
HTTP server listening on :8090
```

Verify health:

```bash
curl http://localhost:8090/health
# Expected status: 200 OK with runtime.mode set to setup_required or configured
```

If you get a 502 or connection refused, the app is still starting — wait a moment and retry.

---

## Step 3 — Open Project Management

1. Open `http://YOUR_SERVER:8090/bot/login` in a browser. For local development use `http://127.0.0.1:8090/bot/login`.
2. If no Kitsu host is configured, enter the Kitsu URL.
3. Sign in with your Kitsu manager or admin account.
4. Complete the dedicated Kitsu runtime connection in `/bot/setup`. Until it succeeds, polling and notifications remain paused.
5. Configure Discord in Bot Settings, then manage each selected Production from `/bot/admin/projects`. Notifications, Task Type channel settings, pause/resume, and `Check without sending` remain on that single Production surface.

Invalid credentials or a temporary Kitsu outage do not stop the Web UI. Reauthenticate from the setup flow; saved configuration is retained. Browser session tokens are not reused by background polling. Do not expose port 8090 directly to the public internet.

The normal operator flow now moves through these stages:

| Stage | What it does |
|------|-------------|
| Bot Settings | Reviews shared bot/runtime prerequisites |
| Production selection | Selects the Kitsu Production and Discord server |
| Channel settings | Reviews deterministic Task Type channels and exact reuse/create results |
| Test Notification | Confirms delivery after channel/webhook creation |

Connection tests do not create Discord resources. Project setup creates Discord categories, channels, and webhooks only after the confirmation step.

---

## Step 4 — Verify notifications

If Project Setup fails after partial Discord provisioning, rollback is best-effort. If the UI does not show **Safe to retry**, check the setup output and `docs/TROUBLESHOOTING.md` before running it again.


1. In Kitsu, change a task status to **WFA**, **Retake**, or **Done**.
2. Watch the logs: `docker compose logs -f app`
3. Look for `Notification route dispatch` followed by a send result.
4. Check the Discord channel — the notification should appear within one poll cycle.

---

## What's next

| Task | Where |
|------|-------|
| Monitor system health | `/bot/admin` (dashboard) |
| Create a new Production connection | `/bot/setup` |
| Manage an existing Production | `/bot/admin/projects?project=<production-id>` |
| Review/edit guild per project | `/bot/admin/projects` |
| Add more user/checker mappings | `/bot/admin/users` |
| Set per-project storage links | `/bot/admin/projects?project=<production-id>&tab=storage-settings` |
| Detailed setup flow reference | `docs/SETUP_WIZARD.md` |
| Troubleshooting | `docs/TROUBLESHOOTING.md` |

---

## Production deployment (Traefik + HTTPS)

For a production server using the Traefik stack in `deploy/`:

```bash
cp .env.local .env.production
# Edit .env.production: set PUBLIC_HOST and ALIAS
cd deploy
docker compose up -d
```

HTTPS and rate limiting are handled automatically by the Traefik labels in `deploy/docker-compose.yml`.

See `docs/ENVIRONMENTS.md` for full environment configuration details.
