# Agent 4 — Navigation / IA grammar

Status: final documentation-only recommendation. This report does not change UI code, routes, behavior, or `DESIGN.md`.

## Scope and evidence boundary

Observed facts are separated from recommendations. Source-backed facts come from the current repository. Runtime-backed facts come from the authenticated evidence pass. The protected connected-Production state was not available in the runtime pass, so its local-navigation behavior remains a design rule, not a runtime claim.

Primary sources:

- `src/setup/ia_views.go:23-34` — current global navigation items.
- `src/setup/ui.go:83-101,210-284,487-489,746-791` — shell, navigation CSS, responsive disclosure, language control, and shell composition.
- `src/setup/ia_views.go:549-575,598-603` — selected Production navigation and default tab behavior.
- `src/main.go:892-943` — registered canonical and compatibility routes.
- `docs/CURRENT-IA-UI-SPEC.md:5-19,120-141` — current route ownership and Production content contract.
- `docs/audits/uiux-agent3-ia.md:17-46,50-88,110-147` — route map, scope map, and IA findings.
- `docs/UI-UX-RUNTIME-EVIDENCE.md:116-128` — authenticated navigation and responsive evidence.
- `src/setup/responsive_accessibility_test.go:9-17` — source-level navigation containment and target-size checks.

No secret values are included here.

## Observed current IA

### Global/top navigation

The current global set is five peer links, in this order: Productions, User Linking, Connections, System Status, and Audit Log (`src/setup/ia_views.go:23-34`). Dashboard is the brand/home link in the shell (`src/setup/ui.go:764-767`), not one of the five `iaNav` chips. New Production setup is a contextual CTA from dashboard and connection surfaces, not a global `iaNav` item (`docs/CURRENT-IA-UI-SPEC.md:5-19`; `src/setup/admin.go:393-443`).

The rendered current navigation is a horizontal `.nav-card` inside `primary-nav`; it is not a desktop sidebar. The outer shell is bounded by `max-width:1100px` (`src/setup/ui.go:83-89`). At widths up to 960px, the nav becomes a horizontally scrollable contained rail; at widths up to 760px, the desktop rail is hidden and a single `details.mobile-nav` disclosure is shown (`src/setup/ui.go:487-489`). Each nav target has `min-height:44px` (`src/setup/ui.go:224-234`).

The current `iaNav` links do not emit an active class; `.nav-chip.active` exists in CSS but is not assigned by `iaNav` (`src/setup/ia_views.go:23-34`; `src/setup/ui.go:241-245`). The language control is a JP/EN two-position link in the top action area, with a 104px minimum width and 56px minimum height (`src/setup/ui.go:246-284,552-559`).

Authenticated runtime evidence confirms the horizontal primary navigation at 1440px and the `Menu` / `メニュー` disclosure in the effective narrow state. It did not show a duplicate persistent mobile navigation (`docs/UI-UX-RUNTIME-EVIDENCE.md:116-128`).

### Production-local navigation

A selected connected Production renders a heading, status, and one `role="tablist"` containing eight peers: Overview, Notifications, Users, Storage settings, Activity, Troubleshooting, Advanced, and Danger Zone (`src/setup/ia_views.go:555-575`). The current implementation defaults an absent or unknown `tab` to `overview` (`src/setup/ia_views.go:598-603`). At 760px and below, the local section rail becomes horizontally scrollable (`src/setup/ui.go:158`).

The IA audit identifies the eight-peer list as a hierarchy problem because daily management, diagnostics, metadata, and dangerous actions have equal navigation weight (`docs/audits/uiux-agent3-ia.md:50-67`). It also identifies Storage as Production-owned even though saving currently returns to a global `/bot/admin/drive` surface (`docs/audits/uiux-agent3-ia.md:69-88`). This report treats that as a route-ownership rule for the future grammar; it does not change the route.

Global User Linking and Production-local association are intentionally different scopes and should remain different navigation destinations (`docs/audits/uiux-agent3-ia.md:144-147`; `docs/CURRENT-IA-UI-SPEC.md:126,134-141`).

### Setup and compatibility

The normal new-connection wizard is `/bot/setup`; the dashboard points to it as the new Production action (`docs/CURRENT-IA-UI-SPEC.md:17`; `src/setup/admin.go:393-443`). Setup is therefore contextual to the task of adding or repairing a Production and must not become a permanent top-level peer of everyday administration.

Root and `/bot` routes are both registered, while legacy routing, diagnosis, checker, and diagnostics paths are compatibility or redirect surfaces (`src/main.go:859-943`; `docs/audits/uiux-agent3-ia.md:90-108,144-147`). They must not be additional navigation entries.

## Final navigation grammar

The following is the recommended IA grammar. Every rule is stated as WHAT / WHY / WHEN / WHEN NOT.

### 1. Global frame and bounded desktop sidebar

WHAT: Use one global frame with a bounded desktop sidebar and a fluid content region. Recommended dimensions: expanded sidebar `240px` fixed basis; collapsed sidebar `64px` fixed basis; `16px` sidebar-to-content gap; content `minmax(0, 1fr)`; frame max width `1280px` including both columns. Keep the current shell's centered, bounded character; the `1100px` implementation value is evidence of that intent, not a required final width (`src/setup/ui.go:83-89`).

WHY: A stable left rail gives five global destinations a persistent location, makes the current section explicit, and leaves the Production-local rail to express object context. The bounded frame prevents a wide monitor from turning navigation into a distant, low-density strip.

WHEN: Use the expanded 240px rail on desktop widths where the frame can provide at least `960px` for content after subtracting sidebar and gap. Use the collapsed 64px rail only as an explicit user-controlled state, preserving icon-plus-tooltip/accessibility names and the same route order.

WHEN NOT: Do not show both a persistent sidebar and the existing horizontal global nav. Do not use a sidebar for login or the step-by-step setup wizard. Do not collapse automatically solely because a label is long; switch to the mobile disclosure at the mobile breakpoint instead.

### 2. Global ordering and active state

WHAT: The global order is `Productions → User Linking → Connections → System Status → Audit Log`. The brand/home control is `Dashboard`. Active state is exact and persistent: selected item has a text/icon state, a visible 3:1-or-better contrast change against its rail, and an accessible current marker (`aria-current="page"` for pages). Dashboard is active only on `/bot/admin`.

WHY: This preserves the current route ownership and keeps the five operational destinations shallow (`src/setup/ia_views.go:23-34`; `docs/CURRENT-IA-UI-SPEC.md:5-19`). Explicit active state repairs the current gap where the CSS supports `.active` but `iaNav` does not emit it (`src/setup/ia_views.go:30-32`; `src/setup/ui.go:241-245`).

WHEN: Mark Productions active for the Production collection and selected Production workspace; mark the other four only for their own global surfaces. Keep Dashboard available as home and summary, not as a mandatory step before another destination.

WHEN NOT: Do not mark both a global parent and a Production child as competing active peers. Do not add `/checkers`, `/production-routing`, `/workflow-diagnosis`, `/diagnostics`, `/drive`, or root aliases to the global rail; retain them only as compatibility/deep-link surfaces (`src/main.go:899-943`; `docs/audits/uiux-agent3-ia.md:90-108,144-147`).

### 3. Sidebar collapse and utilities

WHAT: The expanded rail shows labels. The collapsed rail shows the same five destinations as stable icon buttons, plus a clearly labeled expand control. Place utilities in a separate bottom utility group: language, help/docs if exposed, and version/build information. Keep sign-out outside the primary IA group. Recommended utility spacing is `12px` between groups and `8px` between controls; utility controls retain at least `44px` hit height.

WHY: Grouping utilities separately prevents language/version/help from competing with operational destinations. The current language control already has an independent top-action role and JP/EN parity (`src/setup/ui.go:246-284,552-559`).

WHEN: Show language in the global frame on every localized page, preserve the current locale in the destination URL/state, and show version/build information in the utility area or an About/details surface when it is useful for support.

WHEN NOT: Do not put status badges, connection health, Production counts, or “new setup” into the utility group. Do not expose internal identifiers or secret-related metadata as version/build navigation content. Do not make version text a route unless a real, supported destination exists.

### 4. Responsive/mobile disclosure

WHAT: Below the mobile breakpoint, replace the desktop sidebar with exactly one `Menu` / `メニュー` disclosure. Recommended breakpoint: `760px`, matching the current mobile-nav rule (`src/setup/ui.go:489`). The disclosure is full-width, closed by default after navigation, contains the same global order, and exposes the current item. The language utility remains reachable in the header or disclosure, but must appear once.

WHY: The authenticated evidence supports the current single-disclosure pattern and specifically found no duplicate persistent mobile navigation (`docs/UI-UX-RUNTIME-EVIDENCE.md:116-128`). One disclosure preserves route access without consuming the narrow content width.

WHEN: Use the disclosure at narrow widths and when the content area cannot support the 240px desktop rail plus readable content. Keep targets at least 44px high, consistent with source checks (`src/setup/responsive_accessibility_test.go:9-17`).

WHEN NOT: Do not render both a mobile disclosure and a persistent sidebar. Do not make a horizontal global rail the only mobile access method. Do not force users to pass through Dashboard after opening the menu.

### 5. Production-local hybrid navigation

WHAT: Treat a selected Production as a workspace nested under Productions. Keep a compact local navigation adjacent to the Production heading, with two levels only:

```text
Productions
└─ Selected Production: <name>
   ├─ Overview (optional summary)
   ├─ Manage
   │  ├─ Notifications
   │  ├─ Users
   │  └─ Storage
   └─ Operations
      ├─ Activity
      ├─ Troubleshooting
      ├─ Details
      └─ Danger Zone
```

Recommended desktop local rail width is `208px` expanded, with a `44px` compact/collapsed affordance only when the surrounding layout is space-constrained. Recommended content gap is `24px`. On mobile, convert this local rail to one horizontal scroll row or one disclosure, not both; the selected Production name remains visible above it.

WHY: This retains the current Production-centered model while separating primary management from diagnostics and dangerous operations, the exact issue identified by the IA audit (`docs/audits/uiux-agent3-ia.md:50-67,110-134`). It also preserves the intentional distinction between global User Linking and local Production association (`docs/CURRENT-IA-UI-SPEC.md:126,134-141`).

WHEN: Use the local navigation only after a Production is selected. Keep Notifications, Users, and Storage as the primary management group. Put Activity, Troubleshooting, Details, and Danger Zone under Operations, with Danger Zone visually and semantically last. Preserve the selected Production context for every child destination, including Storage save completion.

WHEN NOT: Do not place the eight items as equal peers in one undifferentiated tab row. Do not put global User Linking inside the Production-local group. Do not make Operations or Danger Zone the default landing surface for ordinary entry.

### 6. No forced Overview

WHAT: Overview is an optional summary destination, not a mandatory funnel. A direct link, dashboard action, or post-action result should land on the task-relevant Production section: Notifications after notification setup, Users after association/role work, Storage after storage work, and Operations children after their corresponding diagnostic action. If no task context exists, use the last valid Production-local section; otherwise use Overview as the neutral fallback.

WHY: The current renderer defaults every absent/unknown tab to Overview (`src/setup/ia_views.go:598-603`), while the approved content contract defines Overview as a summary rather than the owner of all Production work (`docs/CURRENT-IA-UI-SPEC.md:120-128`). Avoiding a forced Overview reduces an extra navigation step and makes deep links honest.

WHEN: Use Overview for an explicit summary request, a newly selected Production with no prior task context, or a safe fallback after an invalid/deleted child destination. Preserve a valid deep-link section across refresh and locale changes.

WHEN NOT: Do not redirect every Production action to Overview. Do not duplicate the same status or action links in Overview merely to compensate for missing local navigation. Do not infer a task context when the source link does not provide one.

### 7. Setup contextuality

WHAT: Keep New Production setup as a contextual action reachable from Dashboard, Productions, and relevant connection/empty states. Keep repair setup contextual to the selected Production and its current issue. The wizard owns its own linear step navigation; it does not inherit the admin sidebar as a second competing workflow.

WHY: The current IA contract names `/bot/setup` as the first-time connection wizard and the dashboard already presents it as the next action (`docs/CURRENT-IA-UI-SPEC.md:17`; `src/setup/admin.go:393-443`). Context explains why setup is being opened and preserves the beginner flow.

WHEN: Show New Production setup when no connected Production exists, when the user explicitly chooses Add/New, or when a selected Production has a documented repair action. Return to the relevant Production or collection context after completion.

WHEN NOT: Do not add Setup as a sixth peer in the global sidebar. Do not force setup users through Dashboard or Overview before the wizard. Do not expose legacy route names as setup navigation labels.

### 8. Language and version invariants

WHAT: JP and EN use the same navigation tree, order, grouping, active semantics, widths, and disclosure behavior. Only labels and locale-specific explanatory text change. The language control remains a single JP/EN switch with a visible selected state, based on the current control (`src/setup/ui.go:552-559`).

WHY: The runtime evidence confirms basic JP/EN navigation rendering and the current spec requires equivalent structure and information density (`docs/UI-UX-RUNTIME-EVIDENCE.md:116-128`; `docs/CURRENT-IA-UI-SPEC.md:113-118`).

WHEN: Apply language changes to the current route/context, including selected Production and local section, and keep the same active destination after switching.

WHEN NOT: Do not translate route ownership into a different IA tree. Do not mix JP and EN labels in one navigation state. Do not show a build version, commit identifier, or diagnostic metadata in the primary navigation unless the user explicitly opens the utility/About surface.

## Acceptance checks for this grammar

- Desktop has one global navigation owner: the 240px sidebar; no duplicate horizontal global rail.
- Collapsed desktop sidebar is explicit and reversible; labels remain available to assistive technology.
- Mobile has one `Menu` / `メニュー` disclosure and no second persistent navigation.
- Global order remains Productions, User Linking, Connections, System Status, Audit Log; Dashboard remains home.
- Selected Production is visibly nested under Productions, with a 208px local rail or one mobile local disclosure/row.
- Production local management and Operations are grouped; Danger Zone is not a peer of routine management.
- Overview is available but is not a forced intermediate redirect.
- Setup remains contextual and linear, not a global peer.
- Storage preserves selected Production context after save; global `/drive` is not a competing normal entry.
- JP and EN preserve identical hierarchy, active state, widths, and disclosure behavior.
- Compatibility routes remain deep-link/redirect support and do not appear in normal navigation.

## Confidence and deferred evidence

High confidence: current route ownership, global link order, shell width, mobile disclosure implementation, Production tab list, default Overview behavior, setup route, and compatibility route registration are source-backed.

Medium confidence: the authenticated runtime confirms the current horizontal navigation and narrow disclosure, but the connected-Production detail state was unavailable. The recommended 240px/208px widths and grouped Production grammar are design recommendations, not measurements of a shipped sidebar. They require future visual validation at JP/EN and desktop/mobile target widths before implementation.
