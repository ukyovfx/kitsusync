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

### Project Management-first setup flow
- `/bot/setup` now acts as the main operator setup surface.
- The operator flow is organized around:
  - Step 1: Bot Settings
  - Step 2: Project Routing
  - Step 3: Guild Assignment
  - Step 4: Test Notification
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

## Validation
- Source/runtime alignment and operator flow were reviewed through the setup/admin redesign PR sequence.
- Docs were updated so public setup guidance reflects the current Project Management-first flow.
- `go test` validation was not rerun as part of this docs/version bump slice.

## Upgrade Notes
- No DB schema change is required for the version bump itself.
- Operators should use `/bot/setup` as the normal setup entry point after upgrade.
- Existing project data remains compatible.
- Existing shared fallback Guild configuration can remain in place for compatibility, but new project routing should use project-level Guild ID.
