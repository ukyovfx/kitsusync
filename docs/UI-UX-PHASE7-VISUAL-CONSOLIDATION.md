# KitsuSync UI/UX Phase 7 — Visual Consolidation

Date: 2026-08-28
Workspace: `C:\Users\mynti\Documents\KitsuSync-clean`
Branch: `codex/v0.4.4-notification-card`

## Scope and guardrails

This pass consolidated the current reachable admin UI after Phases 1–6. Routes, authentication, notification semantics, polling cadence, external Kitsu/Discord state, and the deferred Connection Map were not changed. Existing dirty worktree changes were preserved.

## Final decisions

- VISUAL_SYSTEM = KEEP_SHARED_SHELL_WITH_INTENTIONAL_SURFACE_ASYMMETRY
- API_SPARKLINE = ADOPT_COMPACT_CONTINUOUS_LINE
- DOT_IDENTITY = REFINE_RESTRAINED_SLOW_ADMIN_TEXTURE
- MICROCOPY = KEEP_PHASE_5_CLEANUP; no broad rewrite
- RESPONSIVE = PASS_FOR_REACHABLE_STATES
- REDUCED_MOTION = PASS_BY_SOURCE_CONTRACT

## Bounded fixes

### API response chart

The primary Kitsu and Discord response visual is now a compact continuous line with keyboard-focusable observation points. The time-positioned samples retain actual observation timestamps and existing shared polling/data semantics. Unavailable data remains an empty/not-checked state. Primary vertical bars, isolated primary points, bottom `60s`/`30s`/`Now` labels, and axis chrome were removed. Current latency and Last updated remain.

The chart has a quiet midpoint guide, visible failure points, text labels in the point title/ARIA label, and no secret-bearing content.

### Dot identity

The dark background retains the subtle dot texture on admin surfaces at lower opacity with a 58-second drift. There is no glow, cursor-follow, physics, WebGL, canvas, or artificial delay. The existing reduced-motion media contract stops movement and transitions.

### Cross-screen consolidation

The shared shell and horizontal navigation remain canonical. Dashboard action, attention, summary, and management areas retain their existing hierarchy. Surface asymmetry is intentional: Dashboard remains action-oriented; Audit Log remains chronological/table-oriented; System Status keeps the overall/API/processing/detail order; Setup retains its plan/form flow. No arbitrary fixed heights were introduced.

The prior microcopy cleanup remains in place: redundant helper lines and punctuation noise were removed where they only repeated the control label. Safety, consequence, prerequisite, recovery, and Kitsu/Discord scope copy was preserved.

## Browser evidence

Evidence was captured with the real Chrome browser at the clean local runtime. Files are under `docs/audits/runtime-evidence/`.

Desktop JP set at 1440×900:

- `phase7_dashboard-ja_1440x900.png`
- `phase7_productions-ja_1440x900.png`
- `phase7_connections-ja_1440x900.png`
- `phase7_user-linking-ja_1440x900.png`
- `phase7_system-status-ja_1440x900.png`
- `phase7_audit-log-ja_1440x900.png`
- `phase7_setup-ja_1440x900.png`
- `phase7_login-ja_1440x900.png`

Desktop EN comparison:

- `phase7_dashboard-en_1440x900.png`
- `phase7_system-status-en_1440x900.png`

Final post-rebuild render checks:

- `phase7_final_dashboard-ja_1440x900.png`
- `phase7_final_system-status-ja_1440x900.png`
- `phase7_final_system-status-en_1440x900.png`

Responsive breakpoint samples:

- `phase7_dashboard-ja_1024x768.png`, `phase7_system-status-ja_1024x768.png`, `phase7_connections-ja_1024x768.png`
- `phase7_dashboard-ja_768x1024.png`, `phase7_system-status-ja_768x1024.png`, `phase7_connections-ja_768x1024.png`
- `phase7_dashboard-ja-mobile_375x812.png`
- `phase7_system-status-ja-mobile_375x812.png`
- `phase7_connections-ja-mobile_375x812.png`
- `phase7_audit-log-ja-mobile_375x812.png`

The clean runtime currently reports the intentional setup-required state because no Kitsu connection is configured in its preserved local data. Therefore Productions and User Linking render their guarded setup state; no external configuration was entered to fabricate connected data. Login, Dashboard, Connections, System Status, Audit Log, and Setup shell states were still inspected directly.

## Review results

| Review | Verdict | Evidence-based result |
| --- | --- | --- |
| Cross-screen consistency | KEEP | Shared shell, title rhythm, horizontal nav, dark/orange identity remain coherent. |
| Hierarchy | KEEP | Dashboard and System Status preserve their approved hierarchy; no screen restructuring was needed. |
| Anti-AI / brand | KEEP | Restrained texture, flat semantic status treatments, and no decorative graph spectacle. |
| Accessibility / responsive | REFINE → KEEP | Sparkline points are keyboard reachable and status is text-plus-shape; viewport checks found no horizontal document overflow in reachable states. |
| JP/EN | KEEP | Visible route headings and operational labels are paired; no new language leakage or mojibake observed. |
| Reduced motion | KEEP | CSS source contract disables texture animation and transitions under `prefers-reduced-motion: reduce`. |

## Validation

- Focused CGO-enabled `go test ./src/setup` telemetry/status tests: PASS
- `gofmt`: PASS
- `git diff --check`: PASS
- `docker compose config --quiet`: PASS
- Docker app rebuild/recreate from `KitsuSync-clean`: PASS
- Runtime mount verified as `C:\Users\mynti\Documents\KitsuSync-clean`: PASS
- `/health`: HTTP 200
- Browser route smoke: PASS for reachable guarded/authenticated states
- Browser console errors: none observed on the inspected System Status route
- Responsive overflow checks: no document overflow reported at tested viewports
- External Kitsu/Discord writes: none

## Remaining deferred scope

Connected Production detail, Connection Map adoption, external Connected Production + Discord E2E, release/merge, and any broader hierarchy redesign remain deferred. The clean runtime still needs its separately approved Kitsu/Discord configuration and dedicated disposable E2E target before that release gate can be closed.

## Release-readiness assessment

The visual consolidation is ready for the current reachable/guarded UI surface. Full connected-data visual verification remains intentionally blocked by the absent local Kitsu configuration and is not claimed here.
