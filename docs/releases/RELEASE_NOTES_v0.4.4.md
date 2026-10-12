# KitsuSync v0.4.4

## Summary

v0.4.4 improves normal Discord notification cards and makes the notification language an explicit Production setting. Existing routing, transition behavior, safe mentions, and assignment notifications are preserved.

## Added

- Production setup and Production notification settings now support Japanese or English Discord notification text independently of the admin UI language.
- Compact status-aware cards for WFA, RETAKE, and DONE with transition-aware and comment-only wording.
- Optional Kitsu, Google Drive, and preview content when safely available.
- Test Notification uses the shared notification-card renderer, and cards preserve safe Kitsu Task and Google Drive task links when available.
- Admin sessions are retained in the existing SQLite runtime database across a normal container replacement; browser cookies remain opaque and session records contain no service credentials.
- UI/UX work is frozen after visual consolidation across the admin surfaces, responsive/mobile navigation refinement, Connections/User Linking consistency, and System Status restructuring.
- System Status includes adaptive live sparklines with external Y labels, hover/focus tooltip inspection, and keyboard sample navigation.
- Setup Wizard Discord channel-name fields use an attached non-editable `#` prefix while preserving the submitted channel value.
- Obsolete UI implementation paths and legacy chart/dead-code branches were removed after regression checks.

## Changed

- Valid Kitsu Task Status colors are used as the Discord embed accent, with the existing neutral fallback for invalid values.
- Task Type remains plain text and the redundant Status metadata field is omitted.
- WFA uses Checker mentions, RETAKE uses assignee mentions, and DONE has no mention by default. Recipient IDs remain validated, deduplicated, bounded, and aligned with AllowedMentions.
- User-provided names and comments remain bounded to Discord limits and missing optional data is omitted safely.

## Compatibility

- Existing `Project.Language` values remain supported; missing or unsupported values resolve deterministically to Japanese.
- Assignment notification rendering is unchanged.
- The existing startup migration creates the admin-session table when needed; existing settings and runtime secrets remain compatible.
- Discord setup requires the `bot` OAuth2 scope plus `Manage Channels` and `Manage Webhooks`; Administrator is not required. Presence Intent and Message Content Intent remain off. Server Members Intent is required only when User Linking needs to enumerate Guild members.

## Remaining release gate

- Controlled Discord E2E
- Real notification-card verification
- Human Discord visual approval

This candidate does not claim Discord E2E completion. Merge, tag, and release publication remain intentionally unperformed.
