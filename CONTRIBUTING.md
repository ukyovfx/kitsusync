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

## Community Debugging and Evidence-First Bug Reporting

Bug reports and debugging results must distinguish observed facts from guesses.

When reporting or reproducing a bug, provide:

- the affected KitsuSync version or commit
- operating system
- Docker / Docker Compose environment
- browser when UI behavior is involved
- reverse proxy category when relevant
- exact reproduction steps
- expected behavior
- observed behavior
- reproduction rate
- relevant logs with secrets removed
- screenshots or screen recordings for visual/UI issues
- the source for claims about intended behavior

Valid sources include:

- KitsuSync documentation
- repository code or configuration
- an existing GitHub issue or pull request
- Kitsu / Zou upstream documentation
- visible UI text or an explicit product specification

If no authoritative source exists for an expected-behavior claim, say so. Do not present an assumption as a confirmed bug.

### Hypotheses

Possible causes may be included, but must be clearly marked as a hypothesis. A hypothesis is not evidence and must not be reported as a confirmed cause.

### Sensitive information

Never upload:

- Discord webhook URLs
- Discord bot tokens
- Kitsu credentials
- passwords
- session cookies
- API keys
- `.env`
- `conf.toml`
- SQLite databases containing real data

Sanitize logs, screenshots, videos, and configuration excerpts before posting.

## Issue Triage and Taking Work

- `needs-triage` means the report has not yet been accepted for implementation. Do not start broad or behavior-changing implementation work while this label is present.
- `needs-reproduction` means an independent reproduction is requested.
- `confirmed` means the behavior has been reproduced with sufficient evidence.
- `help wanted` means outside contributions are welcome.
- `good first issue` marks a bounded issue suitable for a first contribution.
- Before substantial work, comment on the issue so contributors do not duplicate effort. Assignment is not required unless a maintainer explicitly requests it.
- External contributors should work from a fork or contributor branch and submit a pull request. Keep one pull request focused on one purpose.

## Pull Request Guidance

- Keep changes focused.
- Link the issue the pull request addresses.
- Explain operator impact, especially if you touch setup, auth, routing, credentials, or runtime behavior.
- Include evidence for the behavior before and after the change.
- Include screenshots or recordings for UI changes.
- State remaining uncertainty instead of guessing.
- Include verification notes:
  - focused tests
  - `go test ./src/... -count=1 -timeout=120s`
  - `go vet ./src/...`
  - `docker compose config -q`
  - browser checks when UI behavior is involved

## Bug Reports

Use the Bug report issue form for a new problem. Use the Reproduction report form when independently checking an existing bug report.

## Security-Sensitive Areas

Be extra careful around:

- `/bot/login`, `/bot/setup`, `/bot/admin`
- runtime credential handling
- Discord webhook routing
- setup rollback behavior
- reverse proxy and cookie assumptions

Do not disclose suspected vulnerabilities in a public issue. Follow `SECURITY.md` and the repository security reporting path instead.

## Debug Notes

- `editor` is debug-only:

```bash
docker compose --profile debug up -d editor
```

- Do not change debug mounts to include `.env`, `conf.toml`, `sqlite.db`, or runtime secrets.
