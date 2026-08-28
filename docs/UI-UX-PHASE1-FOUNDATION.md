# KitsuSync UI/UX Phase 1 — Design Foundations

Date: 2026-08-28 JST
Scope: foundation-only implementation. Screen-specific redesign, permanent desktop sidebar, serif/mincho, Connection Map, route changes, behavior changes, and notification-semantic changes were not performed.

Canonical contract: [`DESIGN.md`](../DESIGN.md)
Evidence: [`UI-UX-DISTRIBUTED-AUDIT.md`](UI-UX-DISTRIBUTED-AUDIT.md), [`UI-UX-RUNTIME-EVIDENCE.md`](UI-UX-RUNTIME-EVIDENCE.md), and [`design-work/`](design-work/).

## Implemented foundation

### Token authority

`src/setup/ui.go` now exposes primitive, semantic, and component custom properties while preserving the observed KitsuSync identity:

- canvas/content: `#070707`, `#f7f7f4`
- action: `#e85a1a`, emphasis: `#ff8d48`
- danger/success: `#ff6a50`, `#8ecf8b`
- focus/info: `#6dc3ff`
- warning: `#ffd978`
- spacing: 4/8/12/16/24/32/48px
- radius: 10/14/17/24px and pill
- controls: 44px standard, 38px dense only for existing compact connection summaries

Ambiguous literals were intentionally retained. No replacement colors were invented and no secret values were added.

### Shared visual foundation

- Added `public-surface` and `admin-surface` body ownership classes in the shared shell.
- Authenticated operational pages now use a static, lower-contrast dot field; Login retains the stronger existing identity treatment.
- `.glass` is now a quiet surface primitive without default blur or shared heavy shadow.
- Page and section surfaces use semantic surface variables; nested screen-specific rules were not comprehensively migrated.
- Existing horizontal navigation remains the standard. Route-owned nav links now emit `aria-current="page"` and the active class where ownership is unambiguous.
- Shared controls retain the 44px target and visible focus outline. Checkbox/radio exclusions remain in text-input sizing selectors.

### Typography

- Existing Outfit + Noto Sans JP body stack remains.
- Space Grotesk remains limited to short UI chrome.
- Japanese labels, table headers, status labels, and buttons no longer inherit English uppercase/tracking treatment through a locale-scoped override.
- Serif/mincho was not introduced.

### Motion

- Removed the shared card/page `riseIn` default.
- Removed `transition: all`; shared navigation transitions are property-specific.
- Added/retained reduced-motion rules to stop particle movement, nonessential entrance motion, hover travel, and unnecessary transitions while preserving state changes.
- System Status polling was not changed.

## Representative runtime evidence

Final image: `kitsusync:v0.4.4-current`, revision label `1a070c403a0bddeabe63086f037c6bb4f02fd8ca`, source-id `KitsuSync-clean`, dirty label `true`. The retained runtime data/config bind mounts were used; secret contents were not read. `/health` returned HTTP 200.

Screenshots:

- [`phase1_login_en_1440x900.png`](audits/runtime-evidence/phase1_login_en_1440x900.png)
- [`phase1_dashboard_en_1440x900.png`](audits/runtime-evidence/phase1_dashboard_en_1440x900.png)
- [`phase1_connections_read_en_1440x900.png`](audits/runtime-evidence/phase1_connections_read_en_1440x900.png)
- [`phase1_connections_edit_en_1440x900.png`](audits/runtime-evidence/phase1_connections_edit_en_1440x900.png)
- [`phase1_connections_edit_ja_375x812.png`](audits/runtime-evidence/phase1_connections_edit_ja_375x812.png)

Observed after the final rebuild:

- Dashboard, Productions, Connections, User Linking, System Status, Audit Log, and Setup reached successfully.
- All sampled 1440×900 and 375×812 routes retained one main landmark, one h1, and no horizontal overflow.
- Authenticated admin body class was `admin-surface`; the rendered dot layer reported opacity `.08` and no animation.
- Final JP Connections edit reported localized technical labels (`Kitsu Bot APIトークン`, `Discord Botトークン`), `text-transform: none`, normal letter spacing, no horizontal overflow, and 44px edit controls. The 38px dense rule remains limited to read-only connection summaries.
- Connections edit remained step-up protected; the saved-login flow reached the edit renderer without exposing credential contents.
- Chrome console warnings were limited to the known extension message-channel error; no application stack error was observed.

## Intentionally retained or deferred

- Current screen-specific gradients, status colors, and some direct rgba/radius literals remain where ownership is not yet proven.
- Connection Map remains `DEFER`.
- Connected Production detail remains deferred because the retained data has no connected Production.
- Font file/glyph provenance and offline fallback remain deferred.
- Full reduced-motion emulation on protected pages remains deferred because the connected Chrome surface does not expose media emulation.
- The documentation page's independent CSS history was not changed.

## Validation

- Focused Go tests: passed (`TestAdminHeaderKeepsGlobalActionsInPrimaryHeader`, `TestLanguageToggleCentersBothLabels`, `TestBaseAdminJSGuardsUserUnlinkActions`).
- Compile-only Go test: passed (`go test ./src/setup -run '^$' -count=1 -timeout=240s`).
- `go vet ./src/setup`: passed.
- `docker compose -f docker-compose.yml config --quiet`: passed, with existing unset `FB_USERNAME`/`FB_PASSWORD` warnings only.
- `git diff --check`: passed; existing line-ending warnings only.
- `transition:all` source scan: absent.

## Post-implementation adversarial review

Four fresh independent reviews were run after implementation:

- Visual/Anti-AI: shared blur/shadow and global card entrance were correctly reduced. Remaining nested cards, dashboard emphasis, and orange competition are screen-specific `REFINE` items for Phase 2, not Phase 1 regressions.
- JP/EN typography: identified and fixed the JP technical-label and edit-control-size issues. Locale-scoped uppercase/tracking rules now render normally in the final runtime.
- Accessibility/responsive: landmarks, active navigation, mobile disclosure, status text, overflow, and dialog foundation were retained. The language toggle remains a single locale-switch action rather than a tab selection. Compact legacy controls outside the representative migration remain future audit items.
- Workflow regression: Productions, User Linking, System Status, Audit Log, and Setup retained their routes and observed behavior. Connected Production detail and Setup URL-language parity remain deferred evidence boundaries.

No review identified a Phase 1 blocker after repair. The final review findings and boundaries are recorded in `docs/design-work/` and the runtime evidence log.

The host's ordinary DB-backed Go tests remain unavailable with its `CGO_ENABLED=0` environment; the Docker builder compiles with CGO enabled and the rebuilt runtime is healthy.

## Phase 2 boundary

Phase 2 may address screen-specific surface migration, connected Production detail, table/list density, Setup structure, and deeper font delivery only after separate review. This phase does not authorize those changes.
