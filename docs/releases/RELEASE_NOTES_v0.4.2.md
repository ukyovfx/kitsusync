# KitsuSync v0.4.2

## Summary

v0.4.2 adds compact, status-aware Discord notification cards while preserving the existing Production routing and delivery flow.

## Notification cards

- WFA, RETAKE, and DONE show status icons, transition-aware messages, and the current Kitsu Task Status color as the embed accent.
- Task Type remains plain text; the redundant lower Status field is removed.
- WFA mentions the Production-configured Checker, RETAKE mentions the resolved assignee, and DONE does not mention anyone by default.
- User mentions are validated, deduplicated, bounded, and kept in exact agreement with Discord `AllowedMentions`.
- Comment-only updates use dedicated wording and do not show a fabricated status transition.
- Assignee names, Kitsu links, Google Drive links, and preview images appear only when safely available.
- Long user-provided content is truncated deterministically to remain within Discord limits.

## Compatibility and safety

- Existing new-message delivery followed by old-message deletion is unchanged.
- Missing User Linking, assignee, comment, link, preview, or status-color data degrades safely.
- No database migration is required for this release candidate.
- Preserve SQLite data, the runtime secret key, and operator configuration together during upgrade.

## Validation

- Focused Discord renderer and recipient tests passed.
- Full Go tests and vet passed in the supported CGO-enabled validation environment.
- Controlled Discord E2E passed using disposable resources only; all disposable resources were removed afterward.
