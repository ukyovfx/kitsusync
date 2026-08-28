# KitsuSync UI/UX Refresh Phase 2 — Screen Evidence

Date: 2026-08-28 JST
Scope: screen-level hierarchy and density refinement after Phase 1 foundation.

## Outcome

Phase 2 implemented bounded screen refinements without changing routes, handlers, POST actions, notification semantics, polling cadence, or Setup execution behavior. The changes keep the existing KitsuSync dark/orange identity while making ownership, state, action, and risk more explicit.

## Screen changes

| Screen | Result |
|---|---|
| Dashboard | Source order is now state/connection action → attention queue → quiet summary → management navigation. KPI tiles are flattened into a restrained summary strip; the Setup CTA remains the single prominent action. |
| Productions | List rows use scan order: Production name → truthful state → short reason → Open Production. Rows flatten to a quiet list and stack safely at mobile width. |
| Connections | Read/edit peer services remain independent. Peer sections use a flat top-rule treatment, edit controls retain 44px standard sizing, and the existing hidden-secret behavior is unchanged. |
| User Linking | Server selection is a secondary setup surface; the Kitsu User ↔ Discord User mapping table is a separate, quieter surface. Existing unlink confirmation remains unchanged. |
| System Status | Overall health, API peer telemetry, operational state, and recent issues remain in that order. Telemetry remains real runtime data and the existing refresh script/cadence is unchanged. |
| Audit Log | The chronological table is quieter and denser; decorative event-card treatment is avoided. Existing local-time enhancement remains unchanged. |
| Setup Wizard | Existing seven-step semantics and Review → Execute boundary are preserved. The plan/review surfaces use clearer grouping and the confirmation boundary is visually explicit. No write action was submitted during QA. |
| Login | Phase 1 organization was lightly refined: one-task form emphasis, reduced nested-surface treatment, and preserved public identity. No marketing hero or authentication behavior change. |

## Runtime evidence

The post-change screenshots were captured from the authenticated Chrome session against the rebuilt local runtime. Before evidence remains in the Phase 1/runtime evidence set.

Desktop 1440×900:

- [Dashboard](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_dashboard_en_1440x900.png)
- [Productions](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_productions_en_1440x900.png)
- [Connections](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_connections_en_1440x900.png)
- [User Linking](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_user_linking_en_1440x900.png)
- [System Status](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_system_status_en_1440x900.png)
- [Audit Log](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_audit_log_en_1440x900.png)
- [Setup](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_setup_en_1440x900.png)
- [Login](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_login_en_1440x900.png)

Mobile 375×812 JP evidence:

- [Dashboard](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_dashboard_ja_375x812.png)
- [Productions](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_productions_ja_375x812.png)
- [Connections](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_connections_ja_375x812.png)
- [User Linking](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_user_linking_ja_375x812.png)
- [System Status](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_system_status_ja_375x812.png)
- [Audit Log](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_audit_log_ja_375x812.png)
- [Setup](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_setup_ja_375x812.png)
- [Login](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_login_ja_375x812.png)

Additional dense checks:

- [Connections 768×1024](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_connections_en_768x1024.png)
- [System Status 1024×768](C:/Users/mynti/Documents/KitsuSync-clean/docs/audits/runtime-evidence/phase2_system_status_en_1024x768.png)

Browser smoke at 375×812 found one `main`, one `h1`, `body=admin-surface` for authenticated screens, `body=public-surface` for Login, and no document horizontal overflow across all eight routes. Setup was inspected at the initial selection step only; no POST or write-producing form was submitted.

## Validation

- `gofmt -w src/setup/ia_views.go src/setup/ui.go`: passed.
- `git diff --check`: passed; only existing CRLF conversion warnings were reported by Git.
- `docker compose config --quiet`: passed; existing unset `FB_USERNAME`/`FB_PASSWORD` warnings remain.
- Docker build: passed with `CGO_ENABLED=1`; rebuilt `kitsusync:v0.4.4-current` from this checkout.
- Runtime recreate: passed with the existing read-only `conf.toml` and writable `data` bind mounts preserved.
- `GET http://127.0.0.1:8090/health`: HTTP 200.
- Docker focused tests: `CGO_ENABLED=1 go test ./src/setup -run "TestDashboard|TestHeader|TestConfirmation|TestResponsive|TestAccessibility" -count=1`: passed.
- Docker `CGO_ENABLED=1 go vet ./src/setup`: passed.
- Host focused Go tests: not runnable in this Windows environment because the default toolchain uses `CGO_ENABLED=0` and `go-sqlite3` is a stub; `CGO_ENABLED=1` then reports no `gcc`. Docker compilation, focused tests, and vet passed.

## Review and deferred boundary

Fresh Phase 2 reviewers A–D were dispatched after implementation for visual/anti-AI, operator cognition, accessibility/responsive behavior, and JP/EN content. Valid reviews converged on `REFINE` without `REVERT`: Dashboard management navigation and System Status surface layering remain the main future refinement candidates; the bounded Phase 2 flattening pass has already been applied and rechecked. One reviewer additionally flagged existing technical diagnostic strings that may remain English in deep error details; no such detail was visible in the current healthy runtime evidence, so no unrelated diagnostic/content rewrite was introduced in this screen-only phase.

Connected Production detail remains deferred because the current validation runtime has no connected Production. No fake connected state was created and no external Kitsu/Discord write was attempted. Sidebar, Connection Map, serif/mincho typography, palette changes, and notification semantics remain explicitly out of scope.

Acceptance marker: `KITSUSYNC_UIUX_PHASE2_SCREENS_READY`
