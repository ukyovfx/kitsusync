# Contributing to KitsuSync

## Before You Start

- Read `README.md` and bring up the app locally with Docker first.
- Keep secrets in `.env` only. Do not commit `.env`, `conf.toml`, or runtime passwords.
- Use the debug FileBrowser profile only when you explicitly need it.

## Local Development Flow

```bash
cp .env.example .env
cp conf.toml.example conf.toml
mkdir -p data
export KITSUSYNC_APP_VERSION="$(tr -d '\r\n' < VERSION)"
docker compose up -d --build
docker compose logs -f app
```

Useful checks:

```bash
docker compose ps
curl http://localhost:8090/health
```

## Pull Request Guidance

- Keep changes focused.
- Explain operator impact, especially if you touch setup, auth, routing, or runtime credentials.
- Include verification notes:
  - routes checked
  - Docker build status
  - polling/log checks
  - any setup/admin UI checks

## Bug Reports

Please include:

- what you expected
- what happened instead
- relevant route or setup step
- `docker compose logs --tail=200 app`
- whether you are using direct `:8090` access or a reverse proxy
- whether debug FileBrowser was enabled

## Security-Sensitive Areas

Be extra careful around:

- `/bot/login`, `/bot/setup`, `/bot/admin`
- runtime credential handling
- Discord webhook routing
- setup rollback behavior
- reverse proxy and cookie assumptions

## Debug Notes

- `editor` is debug-only:

```bash
docker compose --profile debug up -d editor
```

- Do not change debug mounts to include `.env`, `conf.toml`, `sqlite.db`, or runtime secrets.
