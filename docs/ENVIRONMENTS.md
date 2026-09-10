# Development and Production Environments

## Environment Files

KitsuSync uses two environment files that are never committed to git:

| File | Purpose |
|------|---------|
| `.env.local` | Local development — created from `.env.example` |
| `.env.production` | Unsupported legacy name; not consumed by the production wrapper |

```bash
# Development setup
cp .env.example .env.local
# Edit .env.local with your values

# Production uses the protected .env.local input consumed by the wrapper.
# Do not create or rely on .env.production.
```

Both files are listed in `.gitignore`. Never commit them to version control.

## APP_ENV

The `APP_ENV` environment variable controls log verbosity.

| Value | Log level | Set by |
|-------|-----------|--------|
| `development` | DEBUG (all logs) | `docker-compose.yml` |
| `production` | INFO (no debug) | hardened wrapper override |

The root Compose file remains development-oriented. The supported production
wrapper overrides `APP_ENV=production`; production operators must not bypass it.

## Development: docker-compose.yml

For local use. Builds the image from source.

Before a local Compose build, set `KITSUSYNC_APP_VERSION` from the tracked
`VERSION` file. This is a build input, not a second version source:

<!-- LOCAL DEVELOPMENT ONLY -->
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
<!-- END LOCAL DEVELOPMENT ONLY -->

FileBrowser mounts only the active docs/template files. It does not have access to `.env`, `conf.toml`, or the database.

## Production: hardened deployment wrapper

The only supported production path is the root-installed
`/usr/local/sbin/kitsusync-deploy` wrapper using the cryptographically bound
repository-root `docker-compose.yml`. Direct production `docker compose up`, the former
`deploy/docker-compose.yml`, mutable tags, and ad-hoc server build/recreate
commands are retired.

The wrapper accepts no arguments and consumes the protected root-owned
`/etc/kitsusync-deploy/.env.local` input plus the approved image archive,
provenance, Compose digest, and deployment-mode policy. `normal` mode requires
`/ready` to report `ready`. `recovery` mode deliberately permits
`setup_required`; neither mode accepts `degraded`.

See `docs/PRODUCTION_BOOTSTRAP.md` for the one-time root bootstrap, narrow sudo
boundary, read-only inspection, consistent backup, explicit first legacy
migration, and the normal no-argument deployment command.

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
