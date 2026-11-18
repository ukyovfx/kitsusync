# Production admin display path: cached guild directory (stage 1)

## Problem
Opening the Production「通知」tab blocked for minutes while Discord rate limited the bot.
Each render synchronously fetched all guild members, the role list, and per-route (11)
send-eligibility evidence, and `botDo` sleeps up to 15s × 3 retries on 429.

## Stage 1 scope (this change)
Remove every synchronous Discord read from the admin display path. The read side mirrors
the guild channel cache (https://github.com/ukyovfx/kitsusync/pull/306) so a later DB read
model can replace the backside without touching callers.

- `src/setup/guild_directory_cache.go` — raw-body cache for the member list and the role
  list. TTL 5 minutes, one in-flight fetch per guild, `view()` never blocks, the previous
  body is kept with a stale notice when a refresh fails, `invalidate()` for write paths.
- `cachedGuildMembers` / `cachedGuildRoles` return `guildDirectoryPending` when no snapshot
  exists yet, so screens render immediately instead of waiting.
- Display paths switched: team tab, reviewer manager, notification tab / routing editor.
- `forumReviewerRoleEvidenceReader` is no longer called for display. Role send access is
  shown as "verified on apply" and is checked only in the save/apply handlers
  (`ForumReviewerRoles`, `handleCurrentProductionUserMutation`, `validateProductionReviewerDeltas`).
- `botDo` retry behaviour unchanged.

## Verification
- `go test ./src/setup/` targeted groups green, including
  `TestProductionDisplayPathsDoNotBlockOnDiscordRateLimit`, which returns 429 with
  `retry_after: 900` for every guild request and asserts all three screens render in
  under 2 seconds from a stale snapshot.
- `go build ./src/...`, `go vet ./src/...` exit 0. CI (Linux) is the authority for the
  full suite; local Windows full runs hit known SQLite TempDir cleanup failures.

## Next (stage 2, not in this change)
Give the cached directory a DB-backed read model (snapshot table + background refresher)
and swap only the cache's fetch/store side.
