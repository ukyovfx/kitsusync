# KitsuSync UI/UX Phase 3 — Navigation & Structural Polish

Date: 2026-08-28 JST
Scope: navigation A/B comparison and bounded Dashboard/System Status polish after Phase 2.

## Decision

`SIDEBAR = KEEP_HORIZONTAL`

The current horizontal navigation remains the desktop pattern. It keeps the content canvas centered and gives System Status more usable width at 1440×900 and 1024×768. The compact sidebar prototype improved persistent location, but reduced the diagnostic canvas, increased wrapping and vertical length, and made the Dashboard management links compete more directly with the primary state and setup action. It also offered little benefit on the existing mobile path, which correctly remains a single Menu disclosure.

Dashboard remains an attention surface; the brand/home link is the Dashboard destination. No extra Dashboard navigation item was added.

## A/B evidence

### A — current horizontal navigation

- [Dashboard A, 1440×900](audits/runtime-evidence/bot_admin_A_1440x900.png)
- [Productions A, 1440×900](audits/runtime-evidence/bot_admin_projects_A_1440x900.png)
- [Connections A, 1440×900](audits/runtime-evidence/bot_admin_bot_A_1440x900.png)
- [User Linking A, 1440×900](audits/runtime-evidence/bot_admin_users_A_1440x900.png)
- [System Status A, 1440×900](audits/runtime-evidence/bot_admin_health_A_1440x900.png)
- [Audit Log A, 1440×900](audits/runtime-evidence/bot_admin_audit_A_1440x900.png)
- [System Status A, 1024×768](audits/runtime-evidence/bot_admin_health_A_1024x768.png)
- [User Linking A, 1024×768](audits/runtime-evidence/bot_admin_users_A_1024x768.png)
- Existing Phase 2 [Dashboard A, JP 375×812](audits/runtime-evidence/phase2_dashboard_ja_375x812.png)

### B — compact desktop sidebar prototype

B used the same rendered routes and content. The prototype was applied only for evidence capture and then removed; it did not change routing, link targets, data, or action behavior.

- [Dashboard B, 1440×900](audits/runtime-evidence/bot_admin_B_1440x900.png)
- [Productions B, 1440×900](audits/runtime-evidence/bot_admin_projects_B_1440x900.png)
- [Connections B, 1440×900](audits/runtime-evidence/bot_admin_bot_B_1440x900.png)
- [User Linking B, 1440×900](audits/runtime-evidence/bot_admin_users_B_1440x900.png)
- [System Status B, 1440×900](audits/runtime-evidence/bot_admin_health_B_1440x900.png)
- [Audit Log B, 1440×900](audits/runtime-evidence/bot_admin_audit_B_1440x900.png)
- [System Status B, 1024×768](audits/runtime-evidence/bot_admin_health_B_1024x768.png)
- [User Linking B, 1024×768](audits/runtime-evidence/bot_admin_users_B_1024x768.png)
- [Sidebar prototype at JP 375×812](audits/runtime-evidence/_bot_admin_B_375x812.png)

At 375×812 the B prototype deliberately falls back to the same existing Menu disclosure. The saved mobile evidence shows one disclosure rather than a permanent rail.

## Review lenses

- IA/operator: A has better state → action scan width; B improves location persistence but makes the main workspace narrower.
- Visual/anti-AI: A keeps the restrained KitsuSync canvas and avoids introducing a generic admin rail; B is visually credible but adds a persistent block that is not required by the observed task flow.
- Responsive/accessibility: both retained one `main`, one `h1`, `aria-current` navigation ownership, and no sampled document overflow; A avoids the B prototype's desktop width penalty, while both retain the mobile disclosure.
- JP/EN: A and B preserve the same localized screen content. Clearly current diagnostic strings were localized only in the bounded deep-diagnostic cases; technical protocol terms and backend terminology were not broadly rewritten.

## Implemented refinements

- Dashboard management navigation stays subordinate to the setup CTA, attention queue, and summary strip. No duplicate destination or new state was added.
- System Status keeps the hierarchy `Overall → API → operational state → issues`, but removes unnecessary nested surface treatment from the observability/pipeline/issues sections. Polling endpoint, cadence, and telemetry data are unchanged.
- Deep diagnostic UI strings now localize the clearly user-facing Discord bot validity and HTTP 401/403 messages in Japanese while retaining the English source copy.
- Desktop navigation remains horizontal. Mobile remains one `Menu` disclosure. Active links retain `aria-current="page"`.

## Validation

- `gofmt` completed for changed Go files.
- `git diff --check` completed; only the repository's existing line-ending warnings remain.
- Docker image rebuilt from `C:\Users\mynti\Documents\KitsuSync-clean` as `kitsusync:v0.4.4-current`.
- Existing bind mounts were preserved and the recreated runtime returned `/health` HTTP 200.
- Browser regression reached Dashboard, Productions, Connections, User Linking, System Status, Audit Log, Setup, and Login at 1440×900 with one `main`, one `h1`, and no document overflow.
- Dashboard and System Status were rechecked at JP 375×812 with the existing mobile Menu disclosure and no document overflow.
- No Setup execution or external Kitsu/Discord write was performed.

## Deferred

- Permanent desktop Sidebar is rejected for this phase and may be reconsidered only if later operator evidence shows a persistent cross-route location problem that outweighs diagnostic canvas loss.
- Connected Production detail remains deferred because no connected Production runtime evidence exists.
- Connection Map, serif/mincho typography, notification semantics, and external writes remain out of scope.

Sources: [DESIGN.md](../DESIGN.md), [UI-UX-PHASE1-FOUNDATION.md](UI-UX-PHASE1-FOUNDATION.md), [UI-UX-PHASE2-SCREENS.md](UI-UX-PHASE2-SCREENS.md), `src/setup/ui.go`, `src/setup/ia_views.go`, and `src/setup/diagnostics.go`.

Acceptance marker: `KITSUSYNC_UIUX_PHASE3_NAVIGATION_READY`
