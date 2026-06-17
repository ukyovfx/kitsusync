# Quick Start

Get KitsuSync running in about 5 minutes.

---

## Prerequisites checklist

Before you begin, confirm you have:

- [ ] Docker and Docker Compose installed on the server
- [ ] Kitsu running and reachable from the server (test: `curl http://YOUR_KITSU_HOST/api/`)
- [ ] Discord server where you are an administrator
- [ ] Discord **Bot Token** — create one at [discord.com/developers/applications](https://discord.com/developers/applications) (Bot tab → Reset Token)
- [ ] One or more Discord **Guild IDs** — enable Developer Mode in Discord settings, then right-click each target server → Copy Server ID
- [ ] A dedicated Kitsu **runtime account** (any role ≥ CG Artist) for the bot to poll with — do not reuse a personal account

> **Bot permissions required:** Manage Channels, Manage Webhooks.
> Generate the invite URL from OAuth2 → URL Generator with scope `bot` and those two permissions.

---

## Step 1 — Clone and configure

```bash
git clone https://github.com/ukyovfx/kitsusync.git
cd kitsusync
cp .env.example .env.local
```

Open `.env.local` and fill in the minimum required values:

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
Connected to Kitsu in ...
HTTP server listening on :8090
```

Verify health:

```bash
curl http://localhost:8090/health
# Expected: {"status":"ok"}
```

If you get a 502 or connection refused, the app is still starting — wait a moment and retry.

---

## Step 3 — Open Project Management

1. Open `http://YOUR_SERVER:8090/bot/login` in a browser.
2. Sign in with your **personal** Kitsu manager or admin account.
3. Review shared bot/runtime prerequisites in `/bot/admin/bot` if needed.
4. Open `/bot/setup`.
5. Use the Project Management flow to create routing for one project at a time.

The normal operator flow now moves through these stages:

| Stage | What it does |
|------|-------------|
| Bot Settings | Reviews shared bot/runtime prerequisites |
| Project Routing | Selects the Kitsu project and routing template |
| Guild Assignment | Sets the Discord Server / Guild ID for that project |
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
| Create/manage project routing | `/bot/setup` |
| Review/edit guild per project | `/bot/admin/projects` |
| Add more user/checker mappings | `/bot/admin/users`, `/bot/admin/checkers` |
| Set per-project storage links | `/bot/admin/drive` |
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
