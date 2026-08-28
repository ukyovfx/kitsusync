# KitsuSync UI/UX Phase 10 — Visual Consistency Audit

Status: audit and design-rule definition only
Date: 2026-08-28
Runtime: authenticated Chrome, `http://127.0.0.1:8090`

## Executive finding

The six admin routes already render the same H1 token in the current browser, but the source does not have one clean contract. Earlier page-local selectors remain in `src/setup/ui.go`, and later System Status rules override sizes, spacing, and surfaces. The observed drift is therefore caused by layered selector exceptions, not by a missing global redesign. No source fix was made.

## Browser evidence

Audited real authenticated Chrome UI in JP and EN at 1440×900, 1024×768, and 375×812. Routes: `/bot/admin`, `/bot/admin/projects`, `/bot/admin/users`, `/bot/admin/bot`, `/bot/admin/health`, `/bot/admin/audit`.

| Route | H1 rendered | H1 top / first page surface at 1440 | Key observation |
|---|---|---|---|
| Dashboard | 28px / 650 / 32.2px | 115px / 96px | Route intro and management blocks add local hierarchy |
| Productions | 28px / 650 / 32.2px | 113px / 96px | Bordered intro and bordered object rows compete |
| User Linking | 28px / 650 / 32.2px | 113px / 96px | Selector is a special flattened section |
| Connections | 28px / 650 / 32.2px | 113px / 96px | Peer cards use local zero-radius/zero-side-padding rules |
| System Status | 28px / 650 / 32.2px | 113px / 96px | H2/H3/helper and divider rules remain heavier |
| Audit Log | 28px / 650 / 32.2px | 113px / 96px | Table is wrapped in a second bordered section |

At 375px, route H1s remained 28px / 650 / 32.2px and started around 207.4px inside a page surface beginning around 190.4px. At all three widths, sampled pages had no horizontal document overflow. System Status was the longest page: approximately 1,181px rendered height at 1440px, before scrolling.

### Spacing measurements and proposed rhythm

- Page surface top: 96px desktop; standard rendered page-card padding: 16px.
- H1 to first meaningful block: normally about 42px from H1 top; Dashboard and System Status add route-specific blocks.
- Source stack gaps vary among 12, 16, 18, 24, and 28px; System Status uses 28px while the shared stack uses 24px.
- H2 is 20px / 650 / 24px with 8px bottom margin; H3 is 16px / 650 / 19.2px with 6px bottom margin, but System Status later raises some H3s.
- Surface padding varies among 9, 12, 14, 16, 18, and 20px.

Adopt the existing DESIGN.md rhythm: page padding 16px (`--page-padding-dense`), H1→section 24px (`--space-section`), section→section 24px, heading→helper 8px, helper→content 12px only when helper adds operational information, dense row padding 12px 16px, standard action gap 12px. Normalize by semantic relationship, never by forcing equal heights.

## Root causes and files/classes

Primary stylesheet: `src/setup/ui.go`.

- Base `.page-heading h1` defines 26px/1.03; `.dashboard-intro h1` defines 28px/1.1; connection `:has(...)` rules define 24px; System Status defines 32px.
- Phase 8 lines 608–611 adds canonical `main h1`, H2, and H3 rules with `!important`; these currently win in sampled rendering but leave conflicting declarations in the same stylesheet.
- `.page-card`, `.section-card`, `.connections-card`, `.user-linking-page>.section-card:first-of-type`, `.production-list-item`, and System Status selectors each alter padding/radius/background/border.
- `src/setup/ia_views.go` and `src/setup/admin.go` create the route-specific nested containers and helper paragraphs that trigger these exceptions.

Rule to adopt later: one route H1 contract (`Outfit`, 28px, 650, 1.15, -0.025em), one H2/H3 contract, and no route-local declaration of title metrics. Surface exceptions must be semantic and documented.

## Card/surface classification

| Surface | Classification | Reason |
|---|---|---|
| Productions intro copy section | `REMOVE_REDUNDANT_CONTAINER` | No independent state/action; context can sit above the list. Preserve selection-gating meaning inline if needed. |
| Productions row | `KEEP_CARD` | Independent Production identity, state, and open action. |
| User Linking selector | `FLATTEN_TO_SECTION` | Context switch, not an object; retain a scope boundary. |
| Connections Kitsu/Discord peers | `KEEP_CARD` | Independent connection state/diagnostics. |
| Connections outer page card | `FLATTEN_TO_SECTION` | Shell around two peer sections; avoid card-inside-card. |
| System Status outer page card | `FLATTEN_TO_SECTION` | Sections carry the information hierarchy. |
| System “System / Healthy” summary | `REMOVE_REDUNDANT_CONTAINER` | Duplicates healthy API/pipeline signals; retain aggregate only for a distinct actionable failure. |
| System API peer cards | `FLATTEN_TO_SECTION` | One API section with equal peers; no extra peer chrome. |
| System pipeline rows | `FLATTEN_TO_ROW` | State/action rows need status and dividers, not individual cards. |
| System Recent issues | `KEEP_CARD` when non-empty; `FLATTEN_TO_SECTION` when empty | Non-empty issues are a risk boundary; empty state is not an alert card. |
| Audit Log table wrapper | `KEEP_CARD` | Independent historical/audit boundary; remove only extra explanatory nesting. |

## System Status simplification proposal

Current structure: overall summary → API section → Kitsu/Discord peers → operational pipeline → four rows → recent issues. It feels vertically long and horizontally compressed because the page card, flattened sections, peer rules, grid border, and row borders all coexist.

Use this future structure without changing metrics or polling: page title; one API response section with two equal peers and one boundary; one compact operational status section with status/Details/action rows; recent issues only as a distinct risk section when non-empty. Remove the healthy aggregate summary. Reduce repeated section, peer, grid, and row dividers to one grouping rule.

## Navigation state audit

Pointer hover on the active Productions link showed no transform after the Phase 9 override, but the active item still had an inset white top shadow. Source `.nav-chip.active` retains a gradient and inset shadow; the later rule only changes its border. Tab traversal reached skip link, brand, navigation, language control, and action; focus-visible was a clear 3px blue outline with 3px offset.

Recommended quiet contract: selected = text plus `rgba(255,255,255,.06–.08)` background and low-alpha border, no gradient/shadow; hover = same restrained background/border transition, no transform; focus-visible = retain the existing blue outline and offset.

## Background dot identity audit

Source `src/setup/ui.go:85–112`; current admin computed values: opacity `.105`, `particleDrift 52s linear infinite`. First dot layer moves `(0,0)`→`(32px,64px)`; second `(12px,18px)`→`(-24px,34px)`; atmospheric layer `(0,0)`→`(18px,-12px)`. Browser computed background position changed over time, proving motion. Reduced motion disables animation and retains the static texture.

The diagonal drift exists, but `.105` admin opacity can be hard to perceive on content-heavy pages. Later proposal, not implementation: retain the two dot scales and diagonal motion, tune admin opacity around `.13–.16` only after reducing competing atmosphere, and use a 60–75s duration. Keep login/public stronger and reduced-motion static.

## Production list copy

Current source `src/setup/ia_views.go:527`: 「プロダクション一覧で状態を確認し、選択したプロダクションを開きます。設定は選択後に表示されます。」

Classification: `SHORTEN`, not remove. The first clause repeats list navigation; the second communicates a real selection prerequisite. Suggested later wording: JP「状態を確認してProductionを開きます。設定は選択後に表示します」 / EN “Review a state and open a Production. Settings appear after selection.” Preserve warnings, errors, destructive confirmation, recovery, and consequence copy.

## Sparkline X-spacing audit

Source `src/setup/ia_views.go:1437–1465` plus the readable refresh script. The visible chart is a line sparkline with no point markers; Kitsu and Discord share the same geometry/line width. Server-side path X positions are timestamp-derived. The refresh script source still contains an older index-slot bar function, but the rendered script is replaced by the current line implementation.

Timestamp X preserves elapsed gaps but produces uneven visual spacing. Sample-index X gives equal spacing, improves Kitsu/Discord comparison, and makes a compact sparkline stable; it represents observation order rather than elapsed time. Recommend sample-index spacing, with time window and last-updated timestamp providing temporal context; keep x-axis labels and circles out of the visible chart.

## Browser interaction / responsive result

- Hover: no active-nav jump; selected state remains visually stronger than hover because of the inherited shadow.
- Keyboard: focus is visible and reaches primary navigation/actions.
- EN long labels: readable at 375px; System Status stacks API controls/peers.
- JP/EN: sampled headings, statuses, controls, helpers, and empty states preserve information parity; Kitsu/Discord/Production/Task Type/API are intentional technical terms.
- 1440/1024/375: no horizontal document overflow observed. System Status mobile is the main density risk; reduce hierarchy/chrome, not text size.

## Shared rules and ordered implementation plan

1. Remove obsolete title metric overrides; retain one sans H1/H2/H3 token contract.
2. Normalize page/section/row surfaces using DESIGN.md border, radius, and padding aliases.
3. Simplify System Status summary/divider hierarchy without touching metrics/polling.
4. Quiet selected navigation; re-test hover, focus-visible, and mobile disclosure.
5. Shorten only confirmed repeated helpers and re-check JP/EN parity.
6. Change sparkline X to sample index while preserving live refresh/time context.
7. Calibrate existing dot pseudo-element only; verify normal and reduced-motion states.
8. Re-run six routes × JP/EN × 1440/1024/375 with pointer, keyboard, overflow, and console checks.

## Deferred / out of scope

No source implementation fix was made. IA, routes, navigation model, polling, notification semantics, sans typography, Sidebar, Connection Map, Connected Production redesign, and external Kitsu/Discord behavior remain unchanged.

`KITSUSYNC_UIUX_PHASE10_AUDIT_READY`
