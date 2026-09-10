# Development and Production Environments

## Environment Files

KitsuSync uses two environment files that are never committed to git:

| File | Purpose |
|------|---------|
| `.env.local` | Local development — created from `.env.example` |
| `.env.production` | Production server — managed on the server only |

```bash
# Development setup
cp .env.example .env.local
# Edit .env.local with your values

# Production setup (on the server)
cp .env.example .env.production
# Edit .env.production with production values
```

Both files are listed in `.gitignore`. Never commit them to version control.

## APP_ENV

The `APP_ENV` environment variable controls log verbosity.

| Value | Log level | Set by |
|-------|-----------|--------|
| `development` | DEBUG (all logs) | `docker-compose.yml` |
| `production` | INFO (no debug) | root `docker-compose.yml` through the hardened wrapper |

This variable is set by the supported Compose model; production operators must
not invoke Compose directly.

## Development: docker-compose.yml

For local use. Builds the image from source.

Before a local Compose build, set `KITSUSYNC_APP_VERSION` from the tracked
`VERSION` file. This is a build input, not a second version source:

```bash
export KITSUSYNC_APP_VERSION="$(tr -d '\r\n' < VERSION)"
```

```bash
# Start
docker compose up -d --build

# Logs
docker compose logs -f app

# Stop
docker compose down
```

The `editor` service (FileBrowser) is disabled by default. Start it explicitly only when needed:

```bash
docker compose --profile debug up -d editor
```

FileBrowser mounts only the active docs/template files. It does not have access to `.env`, `conf.toml`, or the database.

## Production: hardened deployment wrapper

The only supported production path is the root-installed
`deploy/kitsusync-deploy` wrapper using the repository-root
`docker-compose.yml`. Direct production `docker compose up`, the former
`deploy/docker-compose.yml`, mutable tags, and ad-hoc server build/recreate
commands are retired.

The wrapper accepts no arguments and consumes root-owned approved image,
provenance, Compose-digest, and deployment-mode policy. `normal` mode requires
`/ready` to report `ready`. `recovery` mode deliberately permits
`setup_required`; neither mode accepts `degraded`.

### Historical retired deployment procedure — do not execute

The remainder of this historical section is retained only as audit evidence for
the retired alternate Compose path. It is not a supported operator runbook.

### Retired: deploy/docker-compose.yml

For VPS/server deployment via Traefik. Uses a pre-built image.

```bash
# Deploy from the deploy/ directory
cd deploy
docker compose pull
docker compose up -d
```

**Before deploying:**

1. Create `.env.production` next to `deploy/docker-compose.yml` (one level up from `deploy/`).
2. Set `PUBLIC_HOST` and `ALIAS` in `.env.production`.
3. Ensure Traefik is running with the `proxy` network.

### Temporary GCP deploy note

For the current temporary GCP stack used during maintenance validation, treat container recreation as a backup-first operation.

- Live stack path: `/home/ukyovfx/kitsu-discord-custom/app/docker-compose.yml`
- Workdir: `/home/ukyovfx/kitsu-discord-custom/app`
- Compose service to rebuild/recreate: `app`
- Live container: `app-app-1`
- After the explicit sqlite bind-mount change is deployed, the live DB is expected to be host-backed at `./data/sqlite.db` -> `/app/sqlite.db`
- During migration, treat the current live container `/app/sqlite.db` as authoritative
- Do not assume any pre-existing host `data/sqlite.db` is already authoritative

Before recreating the container, back up these paths from the live container when present:

- `/app/sqlite.db`
- `/app/logs`
- `/app/dump`

If `/app/logs` or `/app/dump` is missing, record that as "not present" rather than treating it as an automatic deploy failure. `sqlite.db` is the required backup artifact. Logs and dump backups are still useful for rollback evidence when they exist.

Before the first recreate after adding the explicit sqlite bind mount, export the current live `/app/sqlite.db` from the running container and copy that exact file to the host-side `./data/sqlite.db`.

`/app/logs/all-levels.log` can now be recreated automatically by the app if missing, but logs backup is still useful for rollback and deploy evidence.

### Temporary GCP safe deploy sequence

Run the temporary GCP deploy in this order. Do not overlap the build and recreate steps.

1. Update the workdir to the target `master` commit and record the previous live commit/image first.
2. Back up `./data/sqlite.db` and, when present, `/app/logs` and `/app/dump`.
3. Build only the app service:

```bash
cd /home/ukyovfx/kitsu-discord-custom/app
docker compose build app
```

4. Recreate only the app service after the build finishes:

```bash
docker compose up -d --force-recreate app
```

5. Verify the recreated container, not just the image cache:

```bash
docker compose ps app
docker inspect app-app-1 --format '{{.Image}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}'
curl http://localhost:8090/health
```

6. Confirm anonymous route behavior:

```bash
curl -I http://localhost:8090/bot/admin
curl -I http://localhost:8090/bot/admin/health
curl -I http://localhost:8090/bot/admin/health
```

For the current stack, the expected compose service name is `app` and the expected running container name is `app-app-1`. Avoid checking a non-existent service label when verifying the deploy.

### Interpreting sqlite hashes after recreate

Do not treat pre-deploy vs post-deploy sqlite hash drift by itself as proof of a bind-mount failure. The more reliable checks are:

- the post-deploy host `./data/sqlite.db` hash matches the post-deploy container `/app/sqlite.db` hash
- the expected bind mount `./data/sqlite.db -> /app/sqlite.db` is still attached
- the recreated container is healthy and `/health` returns `{"status":"ok"}`

If those post-deploy checks all pass, hash drift across the recreate can still be explained by normal runtime writes.

### Path layout expected by deploy/docker-compose.yml

```text
/your-deployment-dir/
├── .env.production          ← required
├── conf.toml                ← required
├── tpl/
├── data/
│   └── sqlite.db
└── deploy/
    └── docker-compose.yml
```

## conf.toml vs .env

**conf.toml** holds operational settings that can be changed without rebuilding the image:

- Polling behavior (`ignoreMessagesDaysOld`, `requestInterval`, `threads`)
- Discord message layout (`tplPreset`, `useThreads`, `embedsPerRequests`)
- Routing rules (`[[discord.productions]]`, `[[discord.taskTypeWebhooks]]`)
- Mention configuration (`checkerStatuses`, `artistStatuses`, `hereStatuses`)
- User and checker mappings

**env file** holds secrets and per-environment values:

- `DISCORD_BOT_TOKEN`
- `DISCORD_GUILD_ID`
- `KITSU_HOSTNAME`
- `KITSU_RUNTIME_EMAIL`
- `KITSU_RUNTIME_PASSWORD` (bootstrap/compatibility only; token-based runtime recovery is preferred)
- `DISCORD_WEBHOOK_URL`

`conf.toml` reads secret values from env via `${VAR_NAME}` syntax. The actual secrets never live in `conf.toml`.

## Updating conf.toml in Production

`conf.toml` is mounted read-only. Any production restart or recreate must go
through the same approved-image, provenance, readiness, and rollback wrapper;
do not bypass it with direct Compose commands.

## Rotating Secrets

When rotating `DISCORD_BOT_TOKEN` or other secrets:

Update the protected production environment source, stage the approved
Compose/environment digests, and use the hardened wrapper. Secret rotation
does not authorize a direct Compose recreate.

## Runtime recovery

Do not recover a production runtime by copying or re-entering a saved password
into a host script. Open the authenticated KitsuSync Connections/setup surface
and use the validated Kitsu bot-token flow. `/health` proves process/local
health only. `/ready` distinguishes `ready`, `setup_required`, and `degraded`;
the root-owned deployment mode determines whether setup recovery is deliberate.
The wrapper also verifies authenticated setup/admin entry points during both
deployment and rollback.

## Release provenance policy

The root-installed deployment boundary consumes a staged image archive plus a
root-owned policy set: archive digest, immutable image ID, Compose digest,
explicit deployment mode, and the source/build/image manifest
(`artifact_kind`, `merge_test_commit`, `source_commit`, `source_id`,
`release_commit`, `version`). It
refuses mutable tags, missing metadata, symlinked policy files, or an image
whose OCI labels do not match the manifest. The previous image is retained by
immutable ID before recreation; rollback must restore that ID and normalized
runtime-significant environment, labels, command, healthcheck, mounts,
host-config, network IDs, and explicit aliases.

## Host-loopback Kitsu with zero-input discovery

When Kitsu/Zou is intentionally bound to host loopback and KitsuSync runs in
Docker, the deployment layer must provide a narrowly scoped internal endpoint
for the app and inject that validated URL as `KITSU_HOSTNAME`. KitsuSync itself
does not change Zou, nginx, public/Tailscale listeners, or shared databases.
Deployments that cannot provide a safe internal endpoint retain the validated
operator-entered URL fallback on `/bot/login`.
