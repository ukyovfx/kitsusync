# Studio Setup Guide

This guide walks through the complete setup of KitsuSync for a studio that already uses Kitsu and Discord. No deep engineering knowledge required.

## What You Need Before Starting

- **Kitsu** running and reachable from the server
- A **Discord server** where the bot can be granted the required permissions
- A **server or VM** with Docker and Docker Compose installed
- A **Kitsu account** with `manager` or `admin` role (for logging into KitsuSync)
- A **Kitsu bot and its API token** (created in Part 1)

---

## Part 1: Create a Bot in Kitsu

KitsuSync uses a dedicated Kitsu bot and its API token to poll for changes. Bots have no password (the API token is their only credential) and do not count as active users.

1. Log into Kitsu as an admin.
2. Open the **Main Menu**, go to the **Admin** section, and open the **Bots** page.
3. Click **New Bot**.
4. Fill in the bot's name, set its role to **Admin** (so it can read every production, task, and comment, and post comments), and set departments or an expiration date if needed.
5. Click **Create user**.
6. Copy the **API token** shown in the pop-up. You will enter this token in the browser during Part 4 — do not put it in `.env.local` or any config file.

If you need to rotate the credential later, you can regenerate the token from the **Bots** page in Kitsu, which immediately revokes the old token.

> Using a dedicated bot keeps KitsuSync's API activity separate from real users in Kitsu audit logs, and lets you rotate the token without affecting personal accounts.

---

## Part 2: Add the Bot to Your Discord Server

1. Go to [Discord Developer Portal](https://discord.com/developers/applications).
2. Create a new application, then go to the **Bot** tab.
3. Click **Reset Token** and copy the bot token — save it securely.
4. Under **Privileged Gateway Intents**, leave Presence Intent and Message Content Intent turned off. Enable Server Members Intent if you will use User Linking; KitsuSync reads the Guild member list for that screen.
5. Go to **OAuth2 → URL Generator**.
6. Select scopes: `bot`
7. Select the bot permissions `Manage Channels`, `View Channel`, `Send Messages`, `Send Messages in Threads`, `Manage Threads`, `Embed Links`, and `Read Message History`. Administrator is not required.
8. Copy the generated URL and open it in a browser to add the bot to your server.

Note your **Discord Guild IDs** (Server IDs) for each production server:

- In Discord, go to Settings → Advanced → enable Developer Mode.
- Right-click your server name → Copy Server ID.

---

## Part 3: Local development setup (not production)

### On a development machine

```bash
git clone https://github.com/ukyovfx/kitsusync.git
cd kitsusync

# Copy and fill in the config files
cp .env.example .env.local
cp conf.toml.example conf.toml
mkdir -p data
```

### Fill in `.env.local`

Open `.env.local` in a text editor and fill in:

```env
KITSU_HOSTNAME=http://YOUR_KITSU_HOST/      # Include http:// and trailing slash

DISCORD_BOT_TOKEN=your-bot-token            # From Discord Developer Portal (Part 2)
DISCORD_GUILD_ID=optional-fallback-server-id  # Optional fallback only

DISCORD_WEBHOOK_URL=                        # Leave blank for now; set up later via project management if needed
```

> The Kitsu credential is not an environment variable — you will enter the bot API token in the browser in Part 4, where KitsuSync stores it encrypted.

### Review `conf.toml`

The most important settings at first:

```toml
tplPreset = "rich"            # "rich" (Japanese), "eng" (English), or "rus" (Russian)
ignoreMessagesDaysOld = 5     # How many days back to pick up missed changes on first boot

[discord]
  botToken = "${DISCORD_BOT_TOKEN}"
```

Leave other settings at their defaults for now.

### Start the app locally

<!-- LOCAL DEVELOPMENT ONLY -->
```bash
export KITSUSYNC_APP_VERSION="$(tr -d '\r\n' < VERSION)"
docker compose up -d --build
docker compose logs -f app
```
<!-- END LOCAL DEVELOPMENT ONLY -->

Wait until you see:

```
Connected to Kitsu in ...
HTTP server listening on :8090
```

Verify it is healthy:

```bash
curl http://localhost:8090/health
```

Expected: `{"status":"ok"}`

---

## Part 4: First-Time Setup in the Browser

Open a browser and go to `http://YOUR_SERVER:8090/bot/login`.

If you are using a reverse proxy (nginx, Traefik), use your public URL instead.
The proxy must overwrite `X-Forwarded-Proto` and `X-Forwarded-Host` with the
validated external request values; do not append client-supplied forwarded
headers. KitsuSync uses these headers to infer secure-cookie/origin and public
Kitsu URL schemes. Keep the application listener private/loopback-only behind
the proxy.

### Step 1: Log in

Sign in with your **personal** Kitsu manager or admin account. This is not the bot — it's your own login used to operate the admin UI.

Sessions expire after 8 hours.

### Step 2: Review Bot Settings and open Project Management

For first-time setup, use `/bot/admin/bot` for shared prerequisites, then continue into `/bot/setup` for project routing.

1. Open `/bot/admin/bot`.
2. In the Kitsu card, paste the bot API token you copied in Part 1 into the **Kitsu Bot API token** field and save. The screen validates the token and shows the connection state.
3. Open `/bot/setup`.
4. Keep `/bot/admin/projects` for later review/edit of existing project guild assignment.

### Step 3: Project Setup (`/bot/setup`)

This stage handles Discord resource creation for one Kitsu project and is now the main operator setup path.

1. Select a Kitsu project from the dropdown.
2. Enter and confirm the target Discord Server / Guild ID for that project.
3. Review the preview before creation.
4. Confirm creation only after the preview looks correct.
5. Send one test notification before treating the project as complete.

Connection testing happens before this stage. This is the point where the Discord category and forum channels may actually be created.

If setup fails partway through, read the `FAIL:` / `WARN:` lines in the output carefully. Rollback is best-effort, and manual Discord cleanup may still be required before retrying.

### Step 4: Optional Post-setup Review (`/bot/admin`)

After project setup, open `/bot/admin` and review:

- **Connections** (`/bot/admin/bot`): Kitsu and Discord connection state. Its sub-pages assign Kitsu task statuses to notification roles and edit the handoff wiring between task types.
- **User Linking** (`/bot/admin/users`): Link Kitsu people to Discord accounts so notifications can @mention them. Who receives a Waiting For Approval notification is resolved from Kitsu positions (Supervisor), not configured here.
- **System Status** (`/bot/admin/health`): Current problems and the latest delivery checks.

---

## Part 5: Verify Notifications Are Working

1. In Kitsu, change a task status on a project you set up.
2. Watch the Docker logs: `docker compose logs -f app`
3. You should see `Notification route dispatch` followed by `SendMessage`.
4. Check the Discord channel — the notification should appear within one poll cycle (a few seconds).

If notifications are not appearing, see `docs/TROUBLESHOOTING.md`.

---

## Part 6: Production Deployment

Use only the root-installed `/usr/local/sbin/kitsusync-deploy` boundary with the
repository-root Compose model. It requires a root-owned approved image,
immutable image ID, Compose digest, provenance manifest, and explicit
deployment mode. The protected `.env.local` is the canonical wrapper input and
the wrapper forces `APP_ENV=production`. Direct production Compose commands and
the former alternate Compose file under `deploy/` are retired. See
`docs/PRODUCTION_BOOTSTRAP.md` and `docs/ENVIRONMENTS.md`.

---

## Updating KitsuSync

Production updates use the same `/usr/local/sbin/kitsusync-deploy` wrapper as the initial
deployment. Stage the approved immutable image, Compose digest, provenance
manifest, runtime configuration, and deployment-mode policy, then invoke the
wrapper with no alternate Compose file or ad-hoc environment file.

The wrapper-managed config files (`conf.toml`, protected `.env.local`) are not
replaced by an image update; keep them under the deployment policy and backup
process.

Back up the data directory as one unit: the SQLite database and `runtime-secret.key` belong together. Without the key, the credentials stored in the database cannot be read back.

---

## Common First-Setup Issues

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Login fails immediately | Kitsu credentials or URL wrong | Check `KITSU_HOSTNAME` and verify that the bot API token saved on the Connections screen (`/bot/admin/bot`) is valid |
| Bot Setup fails | Bot token or Guild ID wrong | Double-check both in `.env.local` |
| Project Setup fails | Bot lacks Discord permissions | Verify the bot has the permissions listed in Part 2 |
| Notifications not arriving | Project forum missing or production not connected | Complete project setup so the forum is created, and confirm the production shows as connected on the admin screen |
| `/health` returns 502 | App not started | For production, inspect the wrapper-managed service and readiness state; use direct Compose only for local development |

For more detailed troubleshooting, see `docs/TROUBLESHOOTING.md`.
