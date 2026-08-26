# KitsuSync v0.4.0

## Summary
v0.4.0 is an operator-flow release. It promotes Project Management in `/bot/setup` to the main setup path and aligns the setup/admin architecture around project-level routing instead of the older wizard/manual/status split.

## Highlights
- `/bot/setup` is now the primary setup path.
- Guided Setup, Manual Setup, and Setup Status were removed from the normal operator flow.
- Discord Server / Guild ID is now project-level and required for new classic project routing.
- Bot Settings responsibilities were narrowed to shared bot/runtime prerequisites.
- Discord Bot Token updates from Bot Settings now persist across restarts.

## Changes

### Production connection setup flow
- `/bot/setup` now acts as the main operator setup surface.
- The operator flow is organized around seven steps:
  - Step 1: Prerequisites
  - Step 2: Production
  - Step 3: Discord Server
  - Step 4: Channel Plan
  - Step 5: Review
  - Step 6: Execute
  - Step 7: Complete
- `/bot/admin/projects` remains available for review/edit, but is no longer the primary first-time setup destination.

### Legacy setup surface removal
- `/bot/setup-wizard`, Manual Setup, and Setup Status were removed from the normal user-facing setup flow.
- Related old rendering/route wiring was cleaned up as part of the redesign work.
- Diagnostics remains available as a secondary troubleshooting surface.

### Project-level Guild ID
- Guild ID is no longer presented as a normal shared Bot Settings field.
- New classic project setup requires a project-level Discord Server / Guild ID.
- Shared fallback guild behavior remains only as a compatibility fallback for older data/flows.

### Bot Settings scope and persistence
- Bot Settings now focuses on shared runtime prerequisites rather than project destination configuration.
- Discord Bot Token saves now persist across restarts by using durable app settings in addition to immediate runtime update.

### Production notification routing

- Current Production routing is identified by stable Production and Kitsu Task Type IDs.
- Staged routing changes can reorder managed Discord channels, change destinations, and remove notification routes without deleting the underlying Task Type or channel.
- Destructive channel deletion is separate, ownership-checked, permission-checked, and requires exact-name confirmation.
- Stale or ambiguous Discord resources fail closed and are surfaced through diagnostics instead of being guessed.
- JP/EN notification rendering, human-only mentions, and sanitized delivery audit behavior are covered by focused tests.

### Readiness and observability

- Discord permission/readiness checks use the target Production Guild and verify the required channel/webhook permissions.
- Fully validated Kitsu, Discord, and Production routing state reports `overall_notification_readiness: "ready"`.
- Runtime build metadata is passed by CI so `/health` and image labels can identify the source commit.

### Documentation route

- The read-only `/docs` and `/bot/docs` aliases serve the checked-in `docs.html` entry point and same-origin `site.jsx` asset in both Compose and standalone runtime images.

## Validation
- Source/runtime alignment and operator flow were reviewed through the setup/admin redesign PR sequence.
- Docs were updated so public setup guidance reflects the current Project Management-first flow.
- Release-candidate validation is tracked in `.github/RELEASE_CHECKLIST.md`; the final tag and public release remain intentionally separate.

## Upgrade Notes
- No DB schema change is required for the version bump itself.
- Operators should use `/bot/setup` as the normal setup entry point after upgrade.
- Existing project data remains compatible.
- Existing shared fallback Guild configuration can remain in place for compatibility, but new project routing should use project-level Guild ID.
