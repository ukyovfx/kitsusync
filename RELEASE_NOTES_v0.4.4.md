# KitsuSync v0.4.4

## Summary

v0.4.4 improves normal Discord notification cards and makes the notification language an explicit Production setting. Existing routing, transition behavior, safe mentions, and assignment notifications are preserved.

## Added

- Production setup and Production notification settings now support Japanese or English Discord notification text independently of the admin UI language.
- Compact status-aware cards for WFA, RETAKE, and DONE with transition-aware and comment-only wording.
- Optional Kitsu, Google Drive, and preview content when safely available.
- Admin sessions are retained in the existing SQLite runtime database across a normal container replacement; browser cookies remain opaque and session records contain no service credentials.

## Changed

- Valid Kitsu Task Status colors are used as the Discord embed accent, with the existing neutral fallback for invalid values.
- Task Type remains plain text and the redundant Status metadata field is omitted.
- WFA uses Checker mentions, RETAKE uses assignee mentions, and DONE has no mention by default. Recipient IDs remain validated, deduplicated, bounded, and aligned with AllowedMentions.
- User-provided names and comments remain bounded to Discord limits and missing optional data is omitted safely.

## Compatibility

- Existing `Project.Language` values remain supported; missing or unsupported values resolve deterministically to Japanese.
- Assignment notification rendering is unchanged.
- The existing startup migration creates the admin-session table when needed; existing settings and runtime secrets remain compatible.
- Discord setup requires the `bot` OAuth2 scope plus `Manage Channels` and `Manage Webhooks`. Administrator and privileged Gateway Intents are not required.
