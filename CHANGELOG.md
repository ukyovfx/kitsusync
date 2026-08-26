# Changelog

All notable changes to this project should be recorded here.

## Unreleased

- The current v0.4.2 candidate is documented below; v0.4.0 and v0.4.1 remain historical.

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

- Release notes for the setup/admin redesign in `RELEASE_NOTES_v0.4.0.md`
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
- Screenshot guidance in `screenshots/README.md`

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
