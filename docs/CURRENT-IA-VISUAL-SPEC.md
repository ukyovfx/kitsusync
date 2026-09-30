# Current IA visual specification

This document records the shared visual language already used by KitsuSync's non-Production-Detail Current IA pages. It is a visual reference for implementation and review, not a source of screen semantics.

## Authority and references

- `docs/CURRENT-IA-UI-SPEC.md` remains authoritative for screen meaning, content, state, routes, actions, and behavior.
- This document is authoritative for shared visual presentation across Current IA pages.
- Use the Dashboard and Production list for the overall page surface and operational summary/list treatment.
- Use System Status for density, operational sections, status rows, concise causes, and contained empty states.
- Use User Linking and the Production Team table for compact comparative table headers, row dividers, and readable data cells.
- Use Connections edit for forms, grouped settings, field labels, controls, and action hierarchy.
- Use Audit Log for compact history tables and empty table rows.
- Production Detail itself is not a visual reference for adopting this specification.

## Page shell and content width

Authenticated pages share the KitsuSync header/navigation and a centered content shell. The shell is bounded by the existing 1100px maximum width and uses the existing page padding. The page is presented on the fixed authenticated dot-grid background. A Current IA page normally has one primary workbench surface with its title and content inside it; content should not be wrapped in arbitrary layers of secondary cards.

Keep the shared workbench width and padding unless a page-specific content constraint is already established. Tables may use their existing responsive treatment; normal page scrolling is preferred over nested scroll regions.

## Typography

Use the existing Outfit / Noto Sans JP stack and current shared text tokens. The page title is the strongest heading (shared 28px title rhythm); section headings follow the shared 20px hierarchy; row titles and form labels use the existing compact 13–16px range. Muted text explains state or cause and stays short. Eyebrows and table headings are small, subdued, and consistently letter-spaced where the current component uses that treatment. Do not use oversized metrics or decorative text treatments to simulate hierarchy.

## Color, borders, and surfaces

Use the existing dark canvas, panel, subtle surface, text, muted text, line, and focus tokens in `src/setup/ui.go`. Use burnt orange for active navigation, primary action, and focus emphasis. Green, yellow, and red retain their semantic status meanings as defined in `docs/CURRENT-IA-UI-SPEC.md`; orange does not replace a status color.

The normal page hierarchy is one workbench surface, then sections separated by quiet hairlines. A compact secondary surface is justified when it groups a distinct operational unit, such as the two API response graphs on System Status, a selected form target, or a dangerous action. Secondary surfaces use the existing subtle background, border, and restrained radius; avoid a card inside a card when a row divider communicates the same grouping.

## Spacing and density

Reuse the shared spacing tokens (`--space-1` through `--space-6`, `--space-major`, `--space-section`, and `--space-action`) defined by the current stylesheet. The established rhythm is compact: small gaps within a row, a section-heading-to-content gap, and a larger shared gap between major sections. Do not add page-specific spacing values when an existing token expresses the same relationship.

Operational screens favor information density without crowding. A heading, explanation, status, and action should align into a small number of clear rows. Empty or warning states occupy a compact row in the section they describe instead of floating between separators.

## Sections and operational rows

System Status is the reference for operational sections: a clear section heading, then content aligned to a shared inset, with rows separated by hairlines. Each row keeps its title and concise cause on the left; a semantic badge and optional action align in a consistent right rail. Disclosures remain secondary and compact. Keep one operational concept per row and do not add decorative metric blocks.

## Tables and lists

Team and User Linking establish the table pattern: small quiet column labels, stable columns on wide screens, compact row padding, top or bottom hairline separators, and no heavy outer frame around every row. Keep one entity per row. Text wraps safely; controls remain dark, compact, and aligned with their row. At narrow widths use the component's existing responsive layout or horizontal table treatment, never page-level overflow.

Production list uses compact production entries with identity, connection state, and a clear action. Keep connection state distinct from health or attention state.

## Forms and controls

Connections edit is the form reference. Use visible labels, short field help only when it prevents ambiguity, dark inputs/selects with shared border and radius tokens, and the established 44px standard / 38px dense control heights. Keep fields within one clear editing context. Primary save/apply is visually strongest; validation/read-only/secondary navigation actions use the shared secondary button treatment. Native browser white select, dialog, or other control surfaces must not leak into the dark interface; explicitly style these controls with the existing dark surface and `color-scheme` conventions where needed.

## Disclosures, dialogs, and actions

Disclosures use an obvious but restrained summary row and reveal compact key/value or diagnostic content. Use the existing disclosure affordance and focus behavior. Dialogs use the same dark panel, border, radius, text, field, and button system as the page, with a dark backdrop; native default white dialog surfaces are not acceptable. Keep primary and secondary actions grouped in one predictable action row and preserve confirmation safeguards for destructive actions.

## Badges and states

Use the current shared status-pill/status-badge shapes, dimensions, and semantic color classes. Status labels remain concise and understandable without color alone. Empty states are subdued, compact, and placed inside the relevant section/table row. Loading and failure states must retain the same page hierarchy while distinguishing unavailable data from successful empty data.

## Responsive behavior and accessibility

At desktop widths, keep compact multi-column summary/table layouts where established. At mobile widths, let content and explanatory copy wrap, stack only when necessary, and retain a predictable relationship between title, status, and action. Page-level horizontal overflow is not accepted. Maintain semantic headings, table associations, labels, keyboard focus visibility, dialog labeling, `details/summary` keyboard behavior, and status announcements.

## Motion and backgrounds

Authenticated pages use the existing static reactive dot grid. It is a viewport-level page background; it does not move, recenter, or depend on content-card bounds. Login fabric is a separate login-only treatment. Production Detail must not introduce its own background animation, glow, or decorative gradient.

## Review checklist

- The page reads as one primary workbench with clear section hierarchy.
- Section spacing and row density follow the shared tokens and System Status rhythm.
- Tables follow Team/User Linking density, with one entity per row.
- Forms and editing states follow Connections edit.
- Dialogs, selects, and other controls remain dark and use shared tokens.
- Badges use screen-semantic colors and labels from the Current IA UI spec.
- Empty, warning, and failed states remain compact and inside the content they describe.
- JP and EN have equivalent information hierarchy; mobile has no page overflow.
- Authenticated dot-grid positions and behavior remain unchanged.
