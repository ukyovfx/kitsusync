# KitsuSync UI Baseline

**Status:** Design baseline proposal  
**Upstream visual baseline:** `cgwire/kitsu` commit `52eb3029a4835f45c9dde06154e859403816ee07`  
**Initial adoption target:** Production detail in PR #229, after this baseline is reviewed.

## 1. Purpose and scope

This document defines reusable visual-system rules for KitsuSync screens. Its purpose is to make KitsuSync feel like a natural companion to Kitsu while keeping KitsuSync's own identity and product-specific concepts clear.

It governs presentation patterns: hierarchy, density, surfaces, navigation, tables, forms, actions, status, responsive behavior, and accessibility. It does not redefine screen content, route semantics, permissions, data sources, or behavior. Those remain in the screen-specific information architecture and product contracts.

Use current KitsuSync source and rendered behavior as the implementation truth. Use the pinned upstream Kitsu revision as the visual reference, not as a mandate to copy its theme or product semantics.

## 2. Upstream authority and review basis

The visual reference is `cgwire/kitsu` at commit `52eb3029a4835f45c9dde06154e859403816ee07`. Relevant patterns were reviewed in:

- `src/variables.scss` — surface and semantic color roles, including dark theme.
- `src/styles/shared.scss` — compact data tables and row separators.
- `src/components/widgets/RouteTabs.vue` and `RouteSectionTabs.vue` — label-based route tabs.
- `src/components/widgets/ListPageHeader.vue` — shared tab hairline and active underline.
- `src/components/widgets/ProductionTeamList.vue` — compact team table.
- `src/components/pages/ProductionSettings.vue` — settings navigation.
- `src/components/widgets/Card.vue` — justified bounded surfaces.
- `src/components/widgets/ButtonSimple.vue` and `TextField.vue` — action and form patterns.
- `src/App.vue` — theme, focus, responsive table behavior.

These references show patterns rather than fixed measurements. KitsuSync may use different tokens where its dark branded interface requires them.

## 3. Inheritance and KitsuSync exceptions

### Inherit from Kitsu by default

Use Kitsu's established visual language for:

- typography hierarchy and restrained scale
- compact layout and spacing rhythm
- label tabs on a shared hairline with an active underline
- sections and dividers as the normal content structure
- data-oriented tables and list rows
- form labels, field grouping, validation, and action placement
- consistent button hierarchy and state treatment
- small semantic status indicators
- responsive table/list and navigation behavior
- hover, selected, disabled, and focus states

Prefer a familiar Kitsu pattern over a one-off KitsuSync component when it fits the task.

### KitsuSync-specific exceptions

Keep exceptions limited to:

- KitsuSync logo and product identity
- burnt-orange brand accent
- Login particle-fabric background
- authenticated pages' static reactive dot grid
- System Status telemetry charts
- concepts specific to Kitsu and Discord integration

Orange communicates brand, active selection, and primary action. Green, yellow, and red retain semantic meaning for healthy, warning, and error states. Do not recolor semantic states as brand decoration.

## 4. Typography

Use a clear, modest hierarchy:

1. page title or entity name
2. section title
3. row/item title
4. field label and primary value
5. supporting explanation and metadata

Keep operational values easy to scan without turning them into oversized dashboard metrics. Supporting copy should be shorter and lower contrast than the item it explains, while remaining readable.

Use the existing system font stack and Japanese-capable fallbacks. Preserve natural Japanese line wrapping. Avoid all-uppercase styling as a general-purpose hierarchy tool; use it only for short metadata labels when it remains legible. Maintain comfortable line height for mixed Japanese and Latin text.

## 5. Color and semantic status

Retain KitsuSync's dark page surfaces and restrained burnt-orange identity. Upstream Kitsu's light theme colors are not a requirement to recolor KitsuSync. Its dark theme and role-based surface separation are useful references.

Use color by role:

- page background: deepest neutral surface
- content surface: slightly distinct neutral surface where a boundary is needed
- primary text: high-contrast neutral
- supporting text: quieter neutral with sufficient contrast
- borders/dividers: low-contrast hairlines
- orange: KitsuSync identity, active tab/selection, and meaningful primary action
- green/yellow/red: healthy/warning/error only

Do not use color alone to convey status. Pair it with a concise text label or accessible equivalent. Keep decorative saturation and glow low.

## 6. Spacing and density

Use the shared spacing scale already present in KitsuSync as the first choice (4, 8, 12, 16, 24, 32, 48 px). Apply it consistently rather than tuning each page independently.

Prefer compact, data-oriented spacing:

- related label/value pairs stay close
- rows use consistent vertical padding
- section spacing distinguishes groups without large blank bands
- page gutters remain comfortable on narrow screens
- dense content may tighten, but controls remain easy to operate

Avoid large empty hero areas, oversized cards, and inconsistent one-off gaps.

## 7. Page and container hierarchy

A normal page should read in this order:

1. shared application shell and page navigation
2. page title or entity header
3. primary tabs or page-level actions, when present
4. compact content sections
5. supporting metadata or secondary actions

Use the available page width for useful information, with a readable maximum where long-form content needs it. Keep alignment consistent across sibling pages. Content card size or position must not define viewport-level backgrounds.

## 8. Tabs and navigation

Tabs are plain labels on one shared hairline:

- a thin line spans the tab group
- the active tab is identified by an underline on that same line
- inactive tabs remain text links, not pills or filled buttons
- selected state is also available to assistive technology
- keyboard focus is visible and distinct from active selection

Keep primary tabs in a stable order and avoid adding a tab for a secondary action. On mobile, preserve access to all tabs through wrapping or a bounded horizontal tab scroller; do not let the page itself overflow.

## 9. Sections and dividers

Sections and thin dividers are the default way to organize related information. Give each section a clear heading and place actions close to the section they affect.

Use one meaningful boundary between adjacent groups. Avoid stacked borders, redundant separator lines, and dividers after empty states where no content follows. Do not wrap every section in its own bordered container.

## 10. Cards and surfaces

Kitsu uses cards where a bounded, independent surface helps distinguish a discrete unit. KitsuSync may do the same. A card is justified when it represents a distinct resource, a self-contained form/action group, or information that must be visually separated from surrounding content.

Prefer a flat section for content that is part of one page flow. Avoid nested card stacks, a card around every metric, and decorative cards that add no grouping or interaction value. Keep radii and shadows restrained and consistent with the shared shell. On Login, the foreground card and form panel must remain opaque enough to occlude the animated fabric.

## 11. Tables and list rows

Use a compact table when users compare repeated records across shared fields. Use a list when records have a small number of primary facts and a table would add empty columns.

- align headers and cell values consistently
- keep row heights and separators regular
- use subtle hover/selected feedback
- show primary identity before secondary metadata
- keep row actions aligned in a predictable trailing area
- preserve readable column labels and empty states

Team-like data should use compact table/list patterns, not a set of large profile cards. On narrow screens, stack row values with clear labels or use a bounded horizontal scroller for genuinely comparative tables. Never cause document-level horizontal overflow.

## 12. Forms, labels, and inputs

Keep persistent labels visible above or beside fields; placeholders are examples, not substitutes for labels. Group related fields and place concise help or validation text adjacent to the relevant field.

Use consistent control heights, padding, focus rings, disabled treatment, and error styling. Preserve typed values after a failed save. Distinguish validation/check actions from persistence actions, and label a committing action explicitly. Do not introduce save-on-blur or implicit autosave unless the screen contract explicitly requires it.

## 13. Buttons and actions

Maintain a small hierarchy:

- primary: one meaningful commit/continue action for the current context
- secondary: neutral supporting action
- text/link action: navigation or low-emphasis operation
- destructive: clearly identified and semantically red

Use orange sparingly for the primary action or active state. Do not style every control as a large gray block. Keep labels specific, hit areas usable, and button placement consistent. Disabled/loading states must explain or visibly communicate why interaction is unavailable or underway.

## 14. Badges and status

Badges are compact labels, not large panels. Use semantic colors consistently:

- green for healthy/connected
- yellow for attention/warning
- red for failure/disconnected only when the product meaning calls for an error
- neutral for informational or unknown state

Do not make brand orange stand in for health. Keep status near the entity or row it describes, and do not add actions to healthy rows without a real reason.

## 15. Settings and disclosures

Settings should read as vertical sections with a heading, concise description where useful, and the related controls or values. Keep sections aligned on one content column rather than creating a dashboard mosaic.

Use disclosure controls only for secondary details that benefit from being hidden initially. The summary must name the content, expanded content must stay concise, and primary status/actions must remain visible outside the disclosure. Do not hide essential configuration, warnings, or next steps behind disclosure.

## 16. Empty, error, and loading states

Each state should explain what is happening and the next useful step, if one exists:

- empty: identify the missing data and offer a real action only when one exists
- error: state what failed in plain language, preserve recoverable user input, and provide a real retry or destination when safe
- loading: use restrained progress feedback near the affected content and avoid layout jumps

Do not use fake buttons, fabricated values, unexplained blank panels, or decorative status-only cards. Keep the surrounding page hierarchy stable as data loads or errors.

## 17. Responsive and mobile behavior

Desktop density should adapt rather than simply shrink:

- preserve essential labels and actions
- move secondary metadata below primary values when space is limited
- stack form controls and row details as needed
- keep primary actions reachable without placing them mid-content
- retain tab access and visible focus
- avoid clipped content and document-level horizontal scrolling

Mobile should use the same visual system and content semantics. It may change arrangement to fit the viewport, not invent a separate interaction model.

## 18. Accessibility and focus

Maintain keyboard access for navigation, forms, tables with actions, and disclosures. Focus indication must be visible against dark surfaces and must not rely on color alone. Use semantic headings, buttons, links, labels, and table headers. Ensure selected tabs, status text, and errors have accessible names or state attributes.

Respect contrast, text zoom, and reduced-motion preferences. Interactive targets should be practical on touch screens. Hover must not be the only way to discover an action.

## 19. Motion and background exceptions

Background behavior is deliberately separate by page class:

- Login may use the approved animated horizontal silk particle fabric on desktop. Phone-width Login hides the fabric canvas. The centered card remains opaque and readable.
- Authenticated app pages use only the static viewport-level reactive dot grid: fixed dot positions, subtle local brightness/size response near the pointer, no translation, waves, flow field, drift, or global animation.
- System Status charts are a product-specific data visualization exception, not a general page background or decorative motion system.

Do not add other global motion. Local transitions should be short and restrained. Honor reduced motion; the Login background must become static or hidden according to the accepted screen contract, and the authenticated grid should remain visually almost unchanged.

## 20. Forbidden visual patterns

Avoid:

- oversized KPI typography or giant metric tiles
- excessive nested cards or a card around every section
- pill/button-style primary tabs
- huge whitespace that obscures operational density
- broad orange surfaces or orange used for semantic health
- heavy glow, blur, gradients, or decorative texture competing with content
- floating actions detached from the content they affect
- fake links/buttons or invented destinations
- hidden essential information
- mobile layouts that clip controls or overflow horizontally
- one-off visual inventions where an established Kitsu pattern fits

## 21. Browser acceptance checklist

Review the rendered browser output at desktop and mobile sizes, in Japanese and English where localized:

- hierarchy and density feel consistent with a Kitsu operator screen
- shared shell, page title, sections, and tabs align
- tab hairline and active underline are clear and keyboard accessible
- tables/lists remain scannable and actions stay aligned
- forms have visible labels, sensible focus, and clear save/check semantics
- status color and wording agree
- empty/error/loading states are understandable and do not invent actions
- no clipped text, overlap, mojibake, or document-level horizontal overflow
- focus is visible, and reduced-motion behavior is respected
- no console/runtime errors
- no regressions to route, permissions, or screen behavior

## 22. Upstream refresh procedure

When refreshing this baseline:

1. Record the new upstream Kitsu commit SHA and keep the prior SHA for comparison.
2. Inspect the same relevant source areas: theme variables, shared tables, route tabs, page header, team list, settings, cards, buttons, and form controls.
3. Compare the new revision against the current baseline and identify concrete pattern changes; do not assume a visual redesign from a changed version number.
4. Check whether each change is compatible with KitsuSync's dark identity, semantic contracts, and current IA.
5. Update only rules supported by the reviewed upstream evidence. Keep KitsuSync exceptions explicit.
6. Recheck representative browser screens before changing reusable recommendations.

Do not copy upstream assets, source, or product behavior wholesale.

## 23. Migration strategy

Adopt this baseline incrementally:

1. Keep the baseline as a reusable design reference; do not bulk-convert all screens.
2. Choose one screen, preserve its behavior and data semantics, and identify only its visual deviations from this baseline.
3. Make a small screen-scoped change and review the browser-rendered result in JP/EN and desktop/mobile as appropriate.
4. Update screen-specific acceptance criteria only when user-visible semantics change.
5. Repeat after the page is accepted; do not accumulate speculative cleanup in the same change.

## 24. Documentation ownership

- **KITSU-UI-BASELINE:** reusable visual-system rules in this document.
- **CURRENT-IA-UI-SPEC:** screen semantics, content, data meaning, routes, and behavior.
- **Page-specific exceptions:** recorded only when necessary for a screen, with a reason and explicit scope.

This change does not migrate or rewrite `CURRENT-IA-UI-SPEC.md`. Existing screen requirements remain authoritative there. If a visual recommendation conflicts with a screen-specific behavior contract, preserve the behavior and resolve the documentation conflict explicitly before implementation.

## 25. First adoption target: PR #229 Production detail

PR #229 is the first future adoption target. The following is intended structure only; it is not an implementation instruction.

### Header

- Production eyebrow
- Production name
- connection status

### Tabs

- Overview
- Notifications
- Team
- Settings

Use Kitsu-style label tabs on a shared hairline with an active underline.

### Overview

Use compact operational sections and dividers rather than dashboard cards.

### Notifications

Show Routing and WFA recipients as distinct structured sections.

### Team

Use a compact, read-only Kitsu-style table/list.

### Settings

Present Storage, Technical details, Diagnostics, and Danger Zone as vertical settings sections.

Any page-specific departure should be justified by concrete content or behavior needs and documented locally without changing the reusable baseline.

## 26. Self-review and open questions

This baseline does not ban cards: upstream Kitsu has a Card component, and the rule is to use bounded surfaces only when they clarify a distinct unit. It does not import Kitsu's light palette into KitsuSync. It keeps the upstream tab treatment, compact tables, and semantic state colors while preserving KitsuSync-specific brand and background exceptions.

No blocking design question remains for writing or using this baseline. During future adoption, review whether existing shared token values need normalization and whether a specific page has a justified exception; those decisions belong to that page's implementation review, not to speculative changes in this document.
