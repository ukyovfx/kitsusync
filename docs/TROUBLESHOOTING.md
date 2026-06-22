# Troubleshooting

## Quick Diagnostics

Before digging deeper, run these three commands:

```bash
docker compose ps
docker compose logs --tail=50 app
curl http://localhost:8090/health
```

A healthy app returns `{"status":"ok"}` on the health endpoint and shows no ERROR lines in the last 50 log lines at steady state.

On the temporary GCP stack, remember that the compose service name is `app` and the running container name is `app-app-1`. Verify the recreated app container, not an assumed service/container label from an older runbook.

For setup state, the `/api/setup/status` endpoint returns a JSON snapshot of every component:

```bash
curl http://localhost:8090/api/setup/status
```

`/bot/admin/health` shows the same status data visually, including active projects, poller state, and any warnings.

---

## Startup Failures

### `env file .env.local not found`

The app requires an env file to start.

```bash
cp .env.example .env.local
# Fill in the required values
docker compose up -d --build
```

### `Initial Kitsu authentication failed`

The runtime bot account cannot authenticate.

**Check:**

- `KITSU_HOSTNAME` is reachable from inside the container: `curl http://YOUR_KITSU_HOST/api/auth/login`
- `KITSU_RUNTIME_EMAIL` and `KITSU_RUNTIME_PASSWORD` are correct
- The runtime account exists in Kitsu and is active

**If you have not set up a runtime account yet:**

- Leave `KITSU_RUNTIME_EMAIL` and `KITSU_RUNTIME_PASSWORD` blank
- Start the app
- Open `/bot/login` and sign in with a Kitsu manager or admin account
- Complete Bot Setup from `/bot/setup`

### `config validation` FATAL errors at startup

The app logs FATAL validation errors and may exit or continue in degraded mode.

**Common causes:**

| Error | Fix |
|-------|-----|
| `DISCORD_BOT_TOKEN is empty` | Set `DISCORD_BOT_TOKEN` in `.env.local` |
| `DISCORD_GUILD_ID is empty` | Set `DISCORD_GUILD_ID` in `.env.local` |
| `KITSU_HOSTNAME is empty` | Set `KITSU_HOSTNAME` in `.env.local` |
| `conf.toml not found` | Run `cp conf.toml.example conf.toml` |

### Container exits immediately after start

```bash
docker compose logs app
```

Look for the first ERROR or FATAL line — it usually identifies the root cause.

---

## Notifications Not Arriving

### Step 1: Confirm polling is running

```bash
docker compose logs app | grep -E "Got tasks|Done FilterTasks|Done MakeKitsuResponse"
```

If this output appears, polling is running. If not, the app may have failed to authenticate with Kitsu.

### Step 2: Check routing

```bash
docker compose logs app | grep "Notification"
```

- `Notification route dispatch` — a batch is being routed
- `Notification dropped` — tasks were dropped due to no webhook configured
- `Notification routing summary` — shows `mainFallbackSent`, `dropped`, etc.

If `dropped > 0` and `mainFallbackSent = 0`, no fallback webhook is configured. Set `DISCORD_WEBHOOK_URL` in `.env.local`.

### Step 3: Check webhook delivery

```bash
docker compose logs app | grep -E "SendMessage|ERROR"
```

| Error | Cause | Fix |
|-------|-------|-----|
| `Unknown Webhook (code 10015)` | Webhook URL is invalid or deleted | Delete the stale route in `/bot/admin`, re-run setup |
| `Missing Access (code 50013)` | Bot lacks permissions | Check Bot permission scopes in Discord Developer Portal |
| `rate limit wait exceeds cap` | Discord rate limit hit | Normal during large initial sync; tasks are retried next poll |
| `non-2xx response status=401` | Bot token is invalid | Regenerate and update `DISCORD_BOT_TOKEN` |

### Step 4: Confirm task is within the polling window

The `ignoreMessagesDaysOld` setting in `conf.toml` controls how far back changes are considered. If a task change happened before this window, it is silently skipped.

```toml
# Consider changes from the past 14 days
ignoreMessagesDaysOld = 14
```

---

## Setup Flow Issues

### Shared prerequisites are not ready

If `/bot/setup` shows Bot / Runtime as not ready, fix the shared prerequisites first.

Start with:

- `.env.local`
- `/bot/admin/bot`
- `/bot/admin/health` when you need more detail

If you changed `.env.local`, restart the app container before reloading the setup page.

### Step 1 (Kitsu) reports "server not reachable"

The app container cannot reach the Kitsu server.

```bash
# Test from inside the running container
docker compose exec app curl http://YOUR_KITSU_HOST/api/
```

If this fails, the hostname is wrong or Kitsu is not reachable from the container network. Check `KITSU_HOSTNAME` in `.env.local`.

### Step 2 (Discord) reports "guild not accessible"

The bot is not in the server, or the Guild ID is wrong.

1. Confirm the Guild ID via Discord Developer Mode (right-click server → Copy Server ID).
2. If the bot is not yet in the server, generate a new invite URL from Discord Developer Portal → OAuth2 → URL Generator (scope: `bot`, permissions: Manage Channels + Manage Webhooks).

### Step 2 note: test action vs saved settings

- /api/setup/test-discord only validates the submitted token/guild pair and does not save token or guild settings.
- Entering a token in /bot/admin/bot updates the current running process and is also saved in app settings.
- After restart, the saved Bot Settings token is used first.
- `.env.local` / environment variables remain fallback sources.
- Guild ID entered in admin settings is stored as a normal setting (discord.guildID) and persists across restart.

### Step 3 (Project) shows "FAIL:" lines

KitsuSync attempts rollback automatically, but rollback is best-effort. Read the `FAIL:` line carefully:

- `failed to create Discord category` — bot lacks Manage Channels permission
- `failed to create webhook` — bot lacks Manage Webhooks permission
- `Kitsu project was not found` — project was deleted from Kitsu; reload the page

If the output shows `✅ Safe to retry`, fix the reported error and click Create Channels again. If it does not, inspect Discord manually before retrying.

### Step 4 (Mapping) shows "no project configured"

Step 3 did not complete successfully. Return to Step 3 and confirm channels were created before proceeding to Step 4.

### Setup page state looks stale after credential changes

The setup surfaces read from `/api/setup/status`. If state looks stale after changing credentials or environment values, reload the page to re-check.

---

## Setup Failures (/bot/setup)

### Bot Setup fails

- Verify `DISCORD_BOT_TOKEN` and `DISCORD_GUILD_ID` are correct
- Confirm the bot is added to the Discord server at `discord.com/developers/applications` → OAuth2 → URL Generator
- Required bot scopes: `bot`, `webhook.incoming`
- Required bot permissions: `Manage Channels`, `Manage Webhooks`

### Project Setup fails partway through

The setup UI shows `FAIL:` or `WARN:` lines describing what failed. Partial Discord resources (categories, channels, webhooks) are rolled back on a best-effort basis, not a hard guarantee.

**Before re-running setup:**

- Read the `FAIL:` / `WARN:` lines in the UI
- Check `docker compose logs app | grep -E "setup|Setup|FAIL|WARN"` for detail
- If Discord resources were partially created, check and clean them manually from Discord before re-running

### Project already has routes but channels are missing in Discord

Routes exist in the DB but the corresponding Discord channels were deleted.

1. Open `/bot/admin` → routing table
2. Delete the stale routes for the affected project
3. Re-run setup for the project

---

## Login and Session Issues

### Login redirects back to login page

- Ensure `KITSU_HOSTNAME` points to a reachable Kitsu instance
- Ensure the Kitsu account has `manager` or `admin` role — regular users cannot log in
- Session timeout is 15 minutes. If the session expired, log in again.

### Cookie warnings in browser

- The admin UI requires cookies. Ensure cookies are enabled for the host.
- In production: ensure the app is accessed over HTTPS (cookies have `Secure` flag set).
- `SameSite=Lax` is intentional — do not change it; it is required for login redirect flows.

---

## Performance Issues

### Polling is slow / tasks appear late

The poll interval is `requestInterval` in `conf.toml` (seconds between Kitsu API calls).

```toml
[kitsu]
  requestInterval = 1  # 1 second between API calls
```

The `threads` setting controls parallel Discord message sending:

```toml
threads = 12
```

### Initial sync sends too many notifications

On first boot with an existing Kitsu, KitsuSync will catch up on all changes within `ignoreMessagesDaysOld`. Limit the window for the first run:

```toml
ignoreMessagesDaysOld = 3
```

Then widen it once the Discord channels are stable.

### Discord rate limit warnings at startup

Normal during large initial syncs. The app respects Discord rate limits and retries on the next poll cycle. No action needed unless they persist.

---

## Database Issues

### `database is locked` or `SQLITE_BUSY`

SQLite is configured with WAL mode and a 5-second busy timeout. If this error appears:

- Ensure only one container instance is writing to `data/sqlite.db`
- In production, check that the volume mount is not shared between containers

### Database file is missing or empty after restart

The `data/` directory must exist and be writable before startup:

```bash
mkdir -p data
docker compose up -d --build
```

In production, confirm `../data/sqlite.db` is correctly mounted via the volume in `deploy/docker-compose.yml`.

If sqlite hashes differ before and after a recreate, do not assume the bind mount failed from that signal alone. Confirm the post-deploy host and container hashes match each other, confirm the expected sqlite bind mount is still attached, and confirm the app is healthy on `/health`.

---

## Discord Channel and Webhook Issues

### Channels were created but webhooks are missing

The Bot may have channel create permission but not webhook create permission. In Discord server settings, verify:

- Server Settings → Integrations → Webhooks (bot can create)
- Channel permissions for the bot user

### `@here` or user mentions not working

Mentions require mapping configuration in `conf.toml` or `/bot/admin`:

- **User mentions:** Add entries under `[[mention.userMap]]` or set them via `/bot/admin` → User Mappings
- **Checker mentions:** Add entries under `[[mention.checkers]]` or `/bot/admin` → Checker Mappings
- **`@here`:** Add the status to `mention.hereStatuses` in `conf.toml` (use cautiously — this pings all members)

Unregistered Kitsu users generate a `WARN Kitsu user not registered` log line — not an error.

### Preview images not showing in Discord embeds

Discord fetches preview thumbnails from Kitsu. Kitsu must serve preview files without requiring interactive browser auth. If previews require login, add a proxy rule:

```nginx
location ~ ^/api/pictures/thumbnails/preview-files/ {
    proxy_pass http://localhost:5000;
    proxy_set_header Host $host;
}
```

---

## FileBrowser is Not Reachable

This is expected behavior. FileBrowser only starts with the debug profile:

```bash
docker compose --profile debug up -d editor
```

It is not intended for production use. See `docs/ENVIRONMENTS.md`.

---

## Logs Reference

| Log pattern | Meaning |
|-------------|---------|
| `Connected to Kitsu in ...` | Kitsu authentication succeeded |
| `Got tasks: N` | Poll fetched N tasks from Kitsu |
| `Done FilterTasks` | Routing complete for this poll cycle |
| `Notification route dispatch` | Discord messages are being sent |
| `Notification dropped` | Task has no webhook route — configure one |
| `SendMessage: non-2xx` | Discord rejected the message — check error body |
| `rate limit wait exceeds cap` | Discord rate limit; task skipped to next cycle |
| `Kitsu user not registered` | Add this person to user mappings |
| `Previous poll still running` | Kitsu or Discord is slow; previous cycle still active |
| `App started env=development` | App running in development mode (DEBUG logs enabled) |
| `App started env=production` | App running in production mode (INFO logs only) |
