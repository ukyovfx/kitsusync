# Changelog

All notable changes to this project should be recorded here.

## Unreleased

- No unreleased changes yet.

## v0.5.4

First release that ships the verified release-ZIP deployment coordinator
(`kitsusync-release-apply`, PR #272).

### Added

- Global user linking can save every changed row in one submit; unchanged rows
  are not sent, and each submitted row keeps the existing single-row validation.
  The Discord directory is read once per save, and every row is resolved before
  any row is written, so a rejected row cannot leave a partial apply behind.

### Changed

- Opening the Connections settings page and ordinary edits (hostname, Guild,
  display settings) no longer require re-authentication. A step-up is asked for
  only when a credential (token or password) is actually submitted or removed.
- Removing a task type from the production-connection channel plan now updates
  the page in place instead of reloading the whole wizard. The add-task-type
  prompt stays in place and the removed type is preselected for re-adding.
- Go 1.26.11 (from 1.26.10) for GO-2026-6859 in `net/http`.

### Fixed

- Connecting a Production no longer floods Discord with notifications for tasks
  that were already waiting (for example in WFA). The state at connection time —
  including each task's latest comment — is checkpointed silently, so only later
  changes notify. A poll that races the connection stays silent for that
  Production until the checkpoint has finished.
- Unlinking a Kitsu user from Discord now asks for confirmation before it is
  submitted; the bulk Save button is addressed explicitly, so it no longer
  collides with the per-row Unlink buttons.
- The race-detector job no longer fails on the initial-connection checkpoint
  test: the checkpoint now runs to completion inside the connection request
  instead of in a goroutine that outlived the test.

## v0.5.3

### Fixed

- Comments-list fetch failures no longer abort the whole polling round. On Zou 1.1.1 the `GET /api/data/comments` LIST endpoint requires admin privileges and returns 403 for the non-admin bot token, so every poll discarded all its data and no notifications fired at all. `MakeKitsuResponse` now logs a distinguished warning (`error_class: kitsu_comments_fetch_failed`) and continues the round with comment data treated as unavailable; all other endpoint failures still fail the poll. During a comments-unavailable round, comment notifications are suppressed and each task's stored delivered-comment checkpoint (ID/UpdatedAt) is preserved unchanged, so the next successful comments round produces a correct diff with no false delete-all or all-new wave.

## v0.5.2

### Fixed

- Kitsu API base URL normalization: `kitsuBase()` now guarantees exactly one trailing slash on every configuration path (KITSU_API_BASE_URL, KITSU_HOSTNAME, and the config file). Previously a KITSU_HOSTNAME without a trailing slash produced invalid request URLs such as `http://host:8080api/data/tasks`, so every production poll failed with `invalid URL`.

## v0.5.1

### Added

- Production-aware WFA Reviewer resolution from the live Kitsu Production Team, with additive Discord User and Role overrides.
- A redesigned Production Detail operator surface for Overview, Notifications, Team, and Settings.
- Durable notification-delivery tracking and recovery diagnostics for retries, uncertain outcomes, and route changes.
- A static reactive plus-grid authenticated background, a silk Login background, and aligned System Status monitoring charts.

### Changed

- User Linking displays verified Discord identity while keeping immutable Discord User IDs as the mapping identity.
- Staging candidate deployment and release artifacts use exact-source provenance and isolated deployment verification.
- Tag CI passes the validated immutable release tag into source-image provenance verification, matching the deployment-bundle provenance gate.

### Validation note

- Real Discord message and mention delivery was not exercised as part of this release acceptance. No live Discord E2E is claimed.

## v0.5.0 (tagged, not published)

- The immutable `v0.5.0` tag points to `78c6fe86b002ec320ad03ea3b56fd7d1e38b6a2d`, but no GitHub Release or deployment bundle was published for it.
- Tag CI failed because the source-image provenance step did not receive `RELEASE_TAG`; the accepted v0.5.0 product changes are carried forward in v0.5.1.

## v0.4.4

### Added

- Production-level Discord notification language selection during setup and from Production notifications settings.
- Compact status-aware Discord cards with transition-aware Japanese and English messages, safe mentions, optional links/previews, and deterministic truncation.
- Persistent opaque admin sessions in the existing SQLite runtime database, with no service credentials stored in session records.
- Test Notification uses the same bounded notification-card renderer as normal delivery, including safe Kitsu Task and Google Drive task links when available.
- UI/UX visual consolidation across the admin surfaces, including responsive/mobile navigation and consistent Connections/User Linking workflows.
- System Status redesign with adaptive live sparklines, external Y labels, hover/focus inspection, and keyboard sample navigation.
- Setup Wizard Discord `#` channel-name prefix presentation and cleanup of obsolete UI implementation paths.

### Changed

- Normal notification cards use Kitsu status colors as their embed accent, keep Task Type as plain text, and omit the duplicate Status metadata field.
- WFA mentions the configured Checker, RETAKE mentions the assignee, and DONE remains unmentioned by default; recipient deduplication and AllowedMentions remain bounded and exact.
- Discord setup requires the `bot` OAuth2 scope plus `Manage Channels` and `Manage Webhooks`; Administrator is not required. Presence Intent and Message Content Intent remain off, while Server Members Intent is required when User Linking enumerates Guild members.

### Remaining release gate

- Controlled Discord E2E, real notification-card verification, and human Discord visual approval remain pending. No Discord E2E completion is claimed by this candidate.

## v0.4.3

### Fixed

- Fresh deployments no longer fall back to a placeholder Kitsu hostname during administrator login.
- Kitsu endpoint resolution now validates explicit, saved, supported local, and operator-supplied endpoints in a deterministic order.

### Changed

- When supported local Kitsu discovery is unavailable, the fresh-login page provides a validated Kitsu base URL field and saves it only after successful manager/admin authentication.
- Discovery diagnostics identify the safe endpoint source without exposing credentials or tokens.

## v0.4.2

### Added

- Status-aware WFA, RETAKE, and DONE Discord notification cards with transition-aware Japanese and English messages.
- User Linking-based mentions with Production-scoped Checker and assignee resolution.
- Deterministic card fixtures and focused coverage for links, previews, comments, truncation, and safe mention behavior.

### Changed

- Discord embed accents now follow the Kitsu Task Status color, with a neutral fallback for missing or invalid colors.
- Task Type is rendered as plain text and the duplicate Status metadata field is removed.
- WFA mentions the configured Checker, RETAKE mentions the assignee, and DONE has no mention by default.
- Kitsu and Google Drive links, preview images, and comment/assignee details are included only when safely available.
- Recipient IDs are validated, deduplicated, bounded, and used consistently for visible mentions and AllowedMentions.

## v0.4.1

### Changed

- Documentation and release metadata now identify the current candidate as v0.4.1 while preserving the historical v0.4.0 entry.
- Documentation asset cache identity is refreshed so updated operator guidance is served after deployment.

### Fixed

- Clean-clone and runtime documentation loading now avoid reusing an older same-origin docs asset.

## v0.4.0

### Changed

- `/bot/setup` is now the main operator setup and project routing surface
- Guided Setup, Manual Setup, and Setup Status were removed from the normal operator flow
- Discord Guild ID became project-level and is required for new classic project setup
- Bot Settings now focuses on shared bot/runtime prerequisites instead of project destination setup
- Discord Bot Token updates from Bot Settings now persist across restarts instead of applying only to the current process
- Production notification routing is managed by stable Production + Task Type identities and supports staged reorder, destination changes, and fail-closed stale-resource handling
- Current Production routing diagnostics verify the linked Guild, managed channels, ownership, and required Discord permissions before mutation
- Public operator documentation and route inventory now match the current seven-step setup and Production-scoped User Linking flow

### Added

- Historical release detail: [`docs/releases/RELEASE_NOTES_v0.4.0.md`](docs/releases/RELEASE_NOTES_v0.4.0.md)
- Deterministic JP/EN Discord notification rendering with safe mention and audit behavior
- Current IA routing editor regression coverage and Discord permission/readiness diagnostics

### Fixed

- Bot Settings token rotation no longer appears lost after normal container recreate/redeploy when the live DB is preserved
- Fully validated Kitsu/Discord/routing state now reports `overall_notification_readiness: "ready"` instead of remaining in a Discord-pending state
- Routing deletion confirmation no longer blocks the surrounding staged-save form through nested native form validation
- Restored the documented read-only `/bot/docs` and `/bot/docs/site.jsx` aliases and included the static docs assets in the runtime image

## v0.1.0

### Added

- OSS onboarding files: `.env.example`, `conf.toml.example`, `CONTRIBUTING.md`, `SECURITY.md`
- GitHub issue templates for bug reports, feature requests, and security redirect guidance
- Pull request template, CODEOWNERS, label guidance, and release checklist
- First public-ready release notes for the v0.1.0 release
- Historical screenshot guidance is preserved in `docs/archive/screenshots/README.md`.

### Changed

- README rewritten for clearer onboarding, setup flow, debug vs production guidance, and v0.1.0 release presentation
- Notification routing now emits explicit observability logs for route dispatch, send result, and drop visibility
- Setup flow now reports partial failure clearly and attempts best-effort rollback
- SQLite startup now configures WAL, busy timeout, and graceful shutdown logging
- README now presents the project as a release-ready OSS with clearer hero section and release focus
- Release checklist now reflects the v0.1.0 sanity checks and verification flow

### Fixed

- Setup no longer reports false success after Discord channel or webhook provisioning failures
- Unmatched notifications no longer disappear silently when no fallback webhook is configured
- Initial repository onboarding path is now understandable without prior operator context

### Security

- Auth cookie handling hardened for trusted reverse proxy deployments
- Runtime credentials separated from admin login credentials
- FileBrowser restricted to an explicit debug profile with secrets excluded from mounts
