# v0.4.4 Legacy and Dead-Code Cleanup Audit

Date: 2026-08-27

This audit covers the KitsuSync runtime and documentation assets in the v0.4.4
worktree. It is intentionally limited to safe cleanup; existing notification,
setup, authentication, compatibility, and recovery behavior is preserved.

## Inventory and classification

### Current (A)

- `/bot/`, `/bot/login`, `/bot/setup`, `/bot/admin`, `/bot/admin/projects`,
  `/bot/admin/users`, `/bot/admin/checkers`, `/bot/admin/drive`, `/bot/admin/bot`,
  `/bot/admin/audit`, `/bot/admin/health`, `/bot/admin/provenance`, and the
  setup API routes are registered in `src/main.go` and used by the current UI.
- `/bot/docs`, `/bot/docs/`, and `/bot/docs/site.jsx` serve the current static
  documentation entry point. `docs.html` and `site.jsx` are copied into the
  runtime image and mounted by Compose.
- `tpl/eng`, `tpl/rich`, and `tpl/rus` are loaded by the Discord notification
  renderer. They remain required, including assignment and compatibility
  rendering.
- Test Notification and read-only notification preview are supported operator
  capabilities and remain in the runtime.

### Compatibility required (B)

- `/login`, `/setup`, `/admin/*`, `/docs`, and `/site.jsx` aliases remain for
  existing bookmarks and supported installations.
- `/bot/admin/production-routing` and `/bot/admin/workflow-diagnosis` retain
  their compatibility behavior while the current project UI is canonical.
- `legacy=1` branches for user, connection, and older setup records remain
  because persisted legacy settings and recovery workflows still use them.
- Legacy user edit helpers and the stopped-state compatibility handling remain
  covered by current tests and persisted-data compatibility requirements.

### Diagnostic required (C)

- `GET /bot/admin/health?legacy=1` is an older but still unique diagnostic
  surface. Its webhook table and `reconnect_webhook` recovery action are not
  present in the current System Status view, logs, audit view, or readiness API.
  It is retained until that recovery capability is moved into the current
  advanced diagnostics surface. The current `/bot/admin/health` remains the
  normal System Status entry point.
- The current health page's observability and webhook diagnostics are retained
  because they expose operational state needed to troubleshoot delivery.

### Test-only (F)

- `src/*_test.go` fixtures and the temporary local E2E helper/state files are
  not runtime assets. The temporary E2E state is intentionally left outside the
  release diff until the pending human Discord visual review is complete.

### Removed (D/E)

- Removed the five unreferenced JSX copies under `src/diagrams/`:
  `12-phase2-2-overview.jsx`, `13-admin-routes.jsx`, `14-db-schema.jsx`,
  `15-webhook-flow.jsx`, and `16-dcc-integration.jsx`.
- Reference analysis found no Go loader, `embed.FS` entry, import, Docker copy,
  runtime route, documentation link, or test dependency for those files.
  Historical diagram source remains deliberately archived under
  `docs/archive/diagrams/`, as documented by the repository's current-state
  notes.

## Not removed

- `docs/archive/diagrams/` is retained as explicitly documented historical
  source, not runtime web content.
- `docs.html` and `site.jsx` are active runtime assets, not duplicate copies.
- Compatibility redirects, legacy persisted-data branches, current diagnostics,
  notification preview/Test Notification, and all Discord templates were
  investigated and retained for the reasons above.
- No route, authentication check, authorization check, database schema, or
  Discord/Kitsu behavior was changed by this cleanup.

## Validation evidence

- Route registration and asset loading were traced through `src/main.go`,
  `src/docs_routes.go`, `Dockerfile`, and `docker-compose.yml`.
- Legacy health behavior was compared with `renderIAHealth`; the legacy view
  still contains unique webhook reconnect functionality.
- `src/diagrams/` had no runtime or test references outside the deleted files.
- Temporary E2E files remain untracked and are excluded from release commits.
- Focused and full validation are run after this bounded cleanup.
