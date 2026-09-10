# KitsuSync v0.4.4 authenticated E2E procedure

This procedure is for the `codex/v0.4.4-notification-card` RC only. Run it against a disposable Kitsu/Zou instance, a disposable Discord Guild, and a fresh KitsuSync SQLite data directory. Never use `vfxstudio`, a real Production, or a shared Discord destination.

## Existing repository tooling

Reuse these existing assets:

- `docker-compose.test.yml` and `tools/test-kitsu-mock` for offline Kitsu/local regression tests.
- `/bot/setup` and the current Connected Productions UI for the real setup flow.
- `src/setup/first_time_connection_test.go` for setup, rollback, and cleanup coverage.
- `src/setup/handler_db_cleanup_test.go` and `src/setup/strong_delete_failure_matrix_test.go` for local cleanup/failure coverage.
- `src/setup/project_discord_health_test.go`, `src/setup/production_routing_test.go`, and `src/setup/test_notification_destination_test.go` for readiness, mapping, and destination safety.

There is no real-Discord E2E runner in the repository. The authenticated portion is therefore a controlled manual run using the existing UI and Discord API behavior.

## Minimum configuration and credentials

Put values only in ignored `.env.local` and local `conf.toml`; evidence must contain names and presence/status only.

Required:

- `KITSU_HOSTNAME` — disposable Kitsu base URL with scheme and trailing slash.
- `KITSU_RUNTIME_EMAIL` and `KITSU_RUNTIME_PASSWORD`, or the setup UI's validated Kitsu bot-token path. The persisted equivalents are `kitsu.runtime_password_encrypted` or `kitsu.runtime_token_encrypted`, protected by `KITSUSYNC_SECRET_KEY_FILE`.
- `DISCORD_BOT_TOKEN` — bot token for the disposable Guild.
- Project-level Discord Guild ID entered during setup. `DISCORD_GUILD_ID` is only the legacy/shared fallback; it is not sufficient as the new project's routing identity.

Optional and only if explicitly covered:

- `DISCORD_WEBHOOK_URL` — catch-all fallback; leave unset for the minimum project-routing E2E.
- `FB_USERNAME` and `FB_PASSWORD` — only for the optional debug FileBrowser profile.
- `KITSUSYNC_LOCAL_PROFILE`, `KITSUSYNC_LOCAL_KITSU_HOST`, `KITSUSYNC_FIXTURE_MODE`, and `KITSUSYNC_VALIDATION_ONLY` — local/mock profiles only; do not enable fixture or validation-only mode for the real Discord run.

Copy non-secret defaults from `conf.toml.example`: `[kitsu]`, `[discord]`, `[notification]`, and `[mention]`. Keep `silentUpdateDB=false`. Do not put webhook URLs, passwords, bot tokens, cookies, or JWTs in logs, screenshots, shell history, or evidence.

## Required services and permissions

Services:

1. Disposable Kitsu/Zou with one test Project, reachable from the KitsuSync container.
2. KitsuSync RC container with fresh SQLite `data/`, local `conf.toml`, and loopback-only port 8090 (use another host port if 8090 is occupied; do not stop an unrelated service).
3. Disposable Discord Guild containing only the E2E bot and E2E resources.
4. Authenticated browser session for the KitsuSync admin UI.

Discord bot:

- OAuth2 `bot` scope.
- `Manage Channels` and `Manage Webhooks` in the disposable Guild. `Administrator` is not required.
- The bot must be able to list the Guild/channels and create/delete the test category, text channels, and webhooks.
- Keep Presence Intent and Message Content Intent disabled. Enable Server Members Intent only if User Linking is included.

Kitsu runtime identity:

- Successful authentication and read access to the endpoints used by validation/polling: authenticated user, projects, persons, task statuses, entities, entity types, task types, tasks, and comments.
- KitsuSync setup and notification delivery require no Kitsu write permission. A separate test operator may need permission to alter a disposable task for transition testing; otherwise pre-seed the task state.

## Isolated test data and snapshots

Prepare one disposable Kitsu Project with at least two distinct Task Types, one Entity, one Task, and statuses covering WFA, RETAKE, and DONE. Include a disposable test person/assignee and checker only if mention behavior is tested. Use one comment-only change only if comment-only card behavior is tested.

Before setup, record sanitized snapshots of:

- Kitsu Project, Task, Task Type, status, assignee, checker, and comment IDs/state.
- Discord categories, channels, and webhooks in the disposable Guild.
- KitsuSync rows/counts for `Project`, `ProjectWebhook`, `ProductionChannelMapping`, `ProductionNotificationConfig`, `ProductionNotificationRoute`, and relevant settings. Never record secret columns or values.

## Exact preparation commands

From the RC checkout:

```sh
cp .env.example .env.local
cp conf.toml.example conf.toml
# Fill only the named local variables; do not print their values.
docker compose config -q
docker compose build app
docker compose -f docker-compose.test.yml up --abort-on-container-exit --exit-code-from test
docker compose up -d app
docker compose ps
curl -fsS http://127.0.0.1:8090/health
```

Confirm `/health` reports build version `0.4.4`, the app container is healthy, and logs contain no secret values. If 8090 is occupied, use an explicit isolated host-port override; do not reconfigure or stop another project merely to claim 8090.

## Exact authenticated E2E sequence

1. Open `/bot/login` and authenticate with the disposable Kitsu account/session. Confirm the browser reaches the setup/admin surface without a redirect loop.
2. Open `/bot/admin/bot` and complete Kitsu connection. Confirm the Kitsu host and runtime account show configured/connected status. Confirm the runtime validation succeeds for the required read endpoints.
3. Configure the Discord Bot Connection with the disposable `DISCORD_BOT_TOKEN`. Select the disposable Guild and run the connection/permission check. Require valid bot identity, valid Guild membership, `Manage Channels`, and `Manage Webhooks`.
4. Open `/bot/setup` and select the disposable Kitsu Project and its project-level Discord Guild. Review the exact Task Type channel plan. Confirm it is a no-write preview and that names are deterministic and IDs are the routing identity.
5. Explicitly confirm the plan. Record the created category ID, every created channel ID/name, and every created webhook ID. Verify each belongs to the selected Guild and disposable Project.
6. In Connected Productions, verify the Project is connected, notification configuration is enabled, notification readiness is `ready`, and every Task Type ID maps to exactly one owned channel/webhook. Verify no cross-Guild, stale, duplicate, or ambiguous mapping exists.
7. Use the existing Test Notification destination selector and choose exactly one newly created owned destination. Verify one expected Discord message/card arrives in the expected channel. Check WFA, RETAKE, and DONE card text/accent/mention behavior only when their corresponding test data is present. Do not use the fallback webhook for the minimum run.
8. If transition delivery is in scope, apply only the pre-authorized disposable Kitsu task changes, wait one poll cycle, and record the sanitized Kitsu before/after state, KitsuSync poll evidence, and Discord message IDs. Verify unrelated Projects receive no message.

## Controlled cleanup and orphan verification

1. Disable/pause the disposable Production or stop polling before deletion.
2. Use the existing Connected Productions delete flow for the exact disposable Project. Delete only resources identified as owned by this run; never delete by name alone.
3. Record the deletion result and any `FAIL:`/`WARN:` lines. A partial result is not PASS; resolve each named owned resource before continuing.
4. Re-list the disposable Guild. The post-run set must contain none of the category/channel/webhook IDs created by this run, while unrelated resources remain unchanged.
5. Re-open `/bot/admin/projects`, `/bot/admin/health`, diagnostics, and routing views. Inspect KitsuSync SQLite read-only. There must be no orphan Project, ProjectWebhook, ProductionChannelMapping, ProductionNotificationConfig, ProductionNotificationRoute, or run-specific setting for the disposable Project. Secret values and webhook URLs must not appear in evidence.
6. Compare Kitsu snapshots. No Kitsu state may differ except the explicitly pre-authorized transition test, which must be restored.
7. Verify KitsuSync logs and Discord message/audit state show no unintended notification. Then run `docker compose down --remove-orphans` and verify no E2E container/network remains.

## PASS criteria and evidence bundle

PASS requires every item below:

- RC branch and commit, runtime build version `0.4.4`, and clean source search for obsolete UI text where relevant.
- `gofmt`, `go test ./...`, `go vet ./...`, `git diff --check`, Compose config, Docker build, and Docker test evidence.
- Authenticated Kitsu connection and all required read-endpoint checks successful.
- Authenticated Discord bot/Guild check successful with required permissions.
- Confirmed no-write plan, created resource IDs, project/Guild ownership, routing/mapping state, and readiness `ready`.
- Sanitized real Discord card evidence for the selected owned destination and any explicitly tested WFA/RETAKE/DONE transitions.
- Cleanup output and post-cleanup Discord/KitsuSync snapshots proving no run-owned resources, mappings, routes, configs, or settings remain.
- Kitsu before/after comparison proving no unintended Kitsu writes.
- KitsuSync/Discord comparison proving no unintended notifications.
- Evidence contains no secret values, webhook URLs, passwords, tokens, JWTs, cookies, or unredacted response bodies.

If any credential, isolated resource, permission, snapshot, cleanup, or evidence item is missing, stop at `CREDENTIALS_REQUIRED` or `INFRA_BLOCKED`; do not call it an E2E PASS.
