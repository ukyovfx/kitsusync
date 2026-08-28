# Agent 2 — Color, surface, spacing, and density token report

## Scope and conclusion

This is a documentation-only proposal based on the current source and supplied audit evidence. No UI code, routes, behavior, or `DESIGN.md` was changed.

The current UI already has a usable dark/orange vocabulary and a partial spacing scale. The main problem is not lack of values; it is that values are repeated as raw literals, surfaces are over-layered, and the documentation page carries a separate CSS history. The recommended direction is to preserve the observed values, give them primitive → semantic → component names, and use surface/border/spacing semantics to reduce visual noise. No new color is proposed.

## Evidence: observed source values

### Color and surface primitives

The canonical admin stylesheet defines these root values at `src/setup/ui.go:10-30`:

| Observed value | Current name | Candidate primitive name | Notes |
|---|---|---|---|
| `#070707` | `--bg` | `--color-neutral-950` | Global dark background. |
| `#0d0d0f` | `--bg2` | `--color-neutral-925` | Secondary dark background. |
| `rgba(20,21,24,.76)` | `--panel` | `--surface-panel-76` | Existing translucent panel value. |
| `rgba(14,15,18,.92)` | `--panel-strong` | `--surface-panel-92` | Existing strong panel value. |
| `rgba(255,255,255,.05)` | `--panel-soft` | `--surface-white-05` | Existing soft white overlay. |
| `rgba(255,255,255,.11)` | `--line` | `--border-white-11` | Default rule/border. |
| `rgba(255,255,255,.2)` | `--line-strong` | `--border-white-20` | Strong rule/border. |
| `#f7f7f4` | `--text` | `--color-neutral-050` | Primary text. |
| `#b8b5ae` | `--muted` | `--color-neutral-300` | Secondary text. |
| `#8f8a83` | `--muted-2` | `--color-neutral-500` | Metadata/tertiary text. |
| `#e85a1a` | `--accent` | `--color-orange-600` | Primary orange accent. |
| `#ff8d48` | `--accent-2` | `--color-orange-400` | Accent highlight/text. |
| `rgba(232,90,26,.34)` | `--accent-glow` | `--color-orange-600-a34` | Existing glow value; should be exceptional. |
| `#ff6a50` | `--danger` | `--color-danger-500` | Destructive/error color. |
| `#8ecf8b` | `--success` | `--color-success-400` | Success color. |
| `#6dc3ff` | focus outline literal | `--color-focus-400` | Focus outline used at `src/setup/ui.go:352`. |

Additional observed state colors are in `src/setup/ui.go:138-143`, `src/setup/ui.go:319`, `src/setup/ui.go:367-369`, and `src/setup/ui.go:387`: success text `#d7f4d4` / `#b8e6b2` / `#a6e0a2`, warning text `#fff1c4` / `#ffe09a` / `#ffd978`, danger text `#ffd3ca` / `#ffb8aa` / `#ffb4a7`, blocked text `#ffe0b3`, and dark accent text `#140904` / `#120804`. These should be preserved as state foreground primitives if the implementation needs those exact contrast variants; they should not be replaced by guessed colors.

`src/setup/ui.go:39-42`, `:50-65`, `:291`, `:299`, and `:386` also contain exact orange/white/black gradients and alpha overlays. The visual audits identify the repeated gradient/glow treatment as a hierarchy problem, not a reason to add more colors: `docs/audits/uiux-agent8-anti-ai.md:56-66`, `docs/UI-UX-DISTRIBUTED-AUDIT.md:28-42`, and `docs/audits/uiux-agent12-adversarial.md:67-83`.

### Spacing, radius, border, and elevation values

The source exposes a partial spacing scale at `src/setup/ui.go:490`: `--space-1:4px`, `--space-2:8px`, `--space-3:12px`, `--space-4:16px`, `--space-5:24px`, `--space-6:32px`, and `--space-action-section:24px`. Other repeated observed values include:

| Category | Observed values and locations |
|---|---|
| Spacing | `2px`, `3px`, `4px`, `5px`, `6px`, `7px`, `8px`, `9px`, `10px`, `12px`, `14px`, `16px`, `18px`, `20px`, `22px`, `24px`, `28px`, `32px`, `48px`; examples: `ui.go:285-290`, `:306-317`, `:328-336`, `:346`, `:356`, `:378-385`, `:385-386`, `:490-491`. |
| Radius | `--radius-sm:10px`, `--radius-md:14px`, `--radius-lg:17px`, `--radius-xl:24px` at `ui.go:27-30`; also `8px`, `11px`, `12px`, `16px`, `18px`, `20px`, `999px` at `ui.go:99-102`, `:285-302`, `:332`, `:349`, `:357`, `:365`, `:378`, `:401`. |
| Elevation | `--shadow:0 24px 80px rgba(0,0,0,.46)` at `ui.go:26`; component shadows include `0 14px 30px rgba(232,90,26,.24)` (`:361`), `0 10px 26px rgba(232,90,26,.1)` (`:386`), `0 12px 28px rgba(0,0,0,.28)` (`:156`), and inset top highlights at `:208`, `:302`, `:320`. |
| Border | `--line` and `--line-strong` at `ui.go:16-17`; recurring raw alpha borders include white `.05`, `.07`, `.08`, `.1`, `.12`, `.14`, `.16`, `.18`, `.25`, `.3`, `.34`, `.4`, `.42`, `.44`, `.45`, `.5`, `.62`, `.72` in the component/state rules. |

The current IA specification explicitly states 24px section-to-section spacing, 12px action-row gap, 24px peer gap for service status groups, and 8px label-to-badge gap at `docs/CURRENT-IA-UI-SPEC.md:82-86`. Those values should be semantic anchors rather than replaced by a new scale.

### Widths and responsive modes

Observed layout widths are `1100px` shell max and `20px 14px 48px` shell padding at `ui.go:83-89`; `260px` navigation width in the shared docs/admin theme at `docs.html:78-82`; `680px` brand-sub max at `ui.go:184-188`; `210px` minimum form column at `:346`; `680px` minimum Production users table at `:146`; `650px` wizard plan table at `:490`; and `420px` delete dialog max at `:156`. The mobile breakpoint is primarily `760px` (`ui.go:158-161`, `:370-377`), with `640px` and `480px` rules elsewhere in the stylesheet as recorded by `docs/audits/uiux-agent0-inventory.md:135`.

The source already contains a compact Connections variant: outer page-card padding `16px`, radius `18px`, card padding `14px`, input minimum height `38px`, action minimum height `38px`, field-row minimum height `28px`, and reduced field gaps at `ui.go:320-343`. The normal form/control baseline is `44px` at `ui.go:349-357`. This supports two semantic modes: standard for ordinary forms and dense for summaries, diagnostic matrices, and compact connection cards. It does not justify a third ad-hoc density mode.

## Proposed token architecture

The following names are candidates, not an implementation patch. Values must resolve only to the observed values above.

### Primitive layer — WHAT / WHY / WHEN / WHEN NOT

WHAT: retain raw values under stable category names: `color-*`, `surface-*`, `border-*`, `space-*`, `radius-*`, `shadow-*`, and `size-*`.

WHY: primitives make the existing palette measurable without assigning meaning too early; they also expose exact duplicates and near-duplicates for later consolidation.

WHEN: change a primitive only when the visual foundation or an audited accessibility requirement changes.

WHEN NOT: do not add a new hex/rgba value merely to make one component look distinctive, and do not encode a component purpose in a primitive name.

Suggested observed-value primitives:

```text
--color-neutral-950: #070707
--color-neutral-925: #0d0d0f
--color-neutral-050: #f7f7f4
--color-neutral-300: #b8b5ae
--color-neutral-500: #8f8a83
--color-orange-600: #e85a1a
--color-orange-400: #ff8d48
--color-danger-500: #ff6a50
--color-success-400: #8ecf8b
--color-focus-400: #6dc3ff
--surface-panel-76: rgba(20,21,24,.76)
--surface-panel-92: rgba(14,15,18,.92)
--surface-white-03: rgba(255,255,255,.03)
--surface-white-04: rgba(255,255,255,.04)
--surface-white-05: rgba(255,255,255,.05)
--surface-white-06: rgba(255,255,255,.06)
--border-white-08: rgba(255,255,255,.08)
--border-white-11: rgba(255,255,255,.11)
--border-white-20: rgba(255,255,255,.2)
--space-1: 4px; --space-2: 8px; --space-3: 12px
--space-4: 16px; --space-5: 24px; --space-6: 32px
--radius-sm: 10px; --radius-md: 14px; --radius-lg: 17px; --radius-xl: 24px; --radius-pill: 999px
--shadow-panel: 0 24px 80px rgba(0,0,0,.46)
```

The `surface-white-*` and `border-white-*` entries above are aliases for values already present in `ui.go`; they are not a request to invent a new alpha palette. Remaining observed literals should either receive an exact primitive name or be removed during a later, separately authorized cleanup.

### Semantic layer — WHAT / WHY / WHEN / WHEN NOT

WHAT: map purpose to primitives, for example:

```text
--color-canvas: var(--color-neutral-950)
--color-content: var(--color-neutral-050)
--color-content-muted: var(--color-neutral-300)
--color-content-subtle: var(--color-neutral-500)
--color-action: var(--color-orange-600)
--color-action-emphasis: var(--color-orange-400)
--color-status-success: var(--color-success-400)
--color-status-danger: var(--color-danger-500)
--surface-page: var(--surface-panel-76)
--surface-control: var(--surface-white-03)
--border-default: var(--border-white-11)
--border-emphasis: var(--border-white-20)
--space-section: var(--space-5)
--space-action: var(--space-3)
--space-peer: var(--space-5)
--space-label-badge: var(--space-2)
--content-max: 1100px
--content-reading-max: 680px
```

WHY: semantic names let a theme or hierarchy decision change in one place and make the intent of status, risk, and action styling reviewable.

WHEN: use semantic tokens whenever a value communicates role: page background, content text, action, success, warning, danger, selected state, section gap, or reading width.

WHEN NOT: do not create semantic aliases for every one-off measurement; do not use `--color-action` for status, and do not use accent color as a substitute for a missing status label.

Note: `--surface-white-025` is intentionally marked as unresolved because the source uses `rgba(255,255,255,.025)` but no primitive is currently named for it. Define it from the exact observed literal only if implementation work later needs the alias. Do not approximate it.

### Component layer — WHAT / WHY / WHEN / WHEN NOT

WHAT: expose only recurring component contracts, with component tokens pointing to semantic tokens:

```text
--page-surface: var(--surface-page)
--page-padding-standard: 20px
--page-padding-dense: 16px
--section-gap-standard: var(--space-section)
--section-gap-dense: var(--space-4)
--card-radius: var(--radius-lg)
--card-border: var(--border-default)
--card-shadow: var(--shadow-panel)
--control-height-standard: 44px
--control-height-dense: 38px
--control-radius: var(--radius-md)
--status-pill-radius: var(--radius-pill)
--status-pill-gap: var(--space-1)
--action-row-gap: var(--space-action)
--table-cell-y-standard: 11px
--table-cell-x-standard: 12px
--table-cell-y-dense: 9px
--table-cell-x-dense: 10px
```

WHY: component contracts prevent page-specific raw values from multiplying while preserving the current standard/dense distinction.

WHEN: use component tokens when the same component appears in more than one route or has a documented standard/dense variant.

WHEN NOT: do not create component tokens for a single decorative gradient, one-off illustration, or a route-specific exception; do not let a component token bypass status semantics.

## Surface and elevation rules

1. WHAT: one primary page surface, flat internal regions, and explicit separators for ordinary data.
   WHY: repeated `page-card glass` plus nested `section-card glass` is documented as card soup and weakens information boundaries (`docs/audits/uiux-agent8-anti-ai.md:28-38`).
   WHEN: use a surface when it represents ownership, an independent task, a modal/overlay, or a high-risk action.
   WHEN NOT: do not wrap every table, metric, status row, or explanation in another elevated card.

2. WHAT: reserve `--shadow-panel` and orange glow shadows for the primary overlay/action level.
   WHY: the adversarial audit says to keep dark + restrained orange, make orange either primary action or selection, and not use glow as a hierarchy substitute (`docs/audits/uiux-agent12-adversarial.md:67-83`).
   WHEN: use elevation for a page shell, modal, or one intentional primary CTA.
   WHEN NOT: do not apply the same shadow to tiles, buttons, cards, and nested panels simultaneously.

3. WHAT: use `--border-default` for quiet structure and `--border-emphasis` only for selection, focus, or risk.
   WHY: borders should disclose semantic boundaries instead of turning every grouping into a competing object.
   WHEN: use a border for a table frame, section rule, control boundary, or selected state.
   WHEN NOT: do not combine strong border + glow + gradient for a normal informational row.

## Spacing rhythm, sections, widths, and density

WHAT: use a 4px base with the observed anchors 8/12/16/24/32; use 24px for section-to-section and peer-group separation, 12px for action rows, and 8px for label-to-badge relationships.

WHY: these are already specified by `CURRENT-IA-UI-SPEC.md:82-86` and recur in the source. Naming them prevents the current 9/10/14/18/20/22px drift from becoming the default rhythm.

WHEN: standard mode is for page shells, ordinary forms, and readable detail views; dense mode is for status summaries, operational matrices, compact connection cards, and tables where the source already uses reduced dimensions.

WHEN NOT: do not use dense mode to fit more content into a novice-facing setup decision, and do not use standard card padding inside a dense summary merely because both are called “card.”

Recommended mode contract:

| Token | Standard | Dense | Source basis |
|---|---:|---:|---|
| Page padding | `20px` | `16px` | `ui.go:285`, `:320` |
| Control min-height | `44px` | `38px` | `ui.go:349-350`, `:332`, `:336` |
| Section gap | `24px` | `16px` | `ui.go:490-491`; IA spec `:84` |
| Internal card padding | `18px` or `20px` | `12px` or `14px` | `ui.go:285`, `:301-302`, `:329` |
| Field row minimum | `38px` | `28px` | `ui.go:310`, `:343` |
| Table cell padding | `11px 12px` | `9px 10px` | `ui.go:380`, `:146`; `:490` |

WHAT: cap the main content at `1100px` and use `680px` as the reading/description maximum; retain the observed `260px` nav and `210px` minimum form column where those structures apply.

WHY: these values exist in the current shell and forms and avoid inventing a new width system (`ui.go:83-89`, `:184-188`, `:346`; `docs.html:78-82`).

WHEN: use full available width for tables, status grids, and two-column operational comparisons; use the reading max for explanatory copy.

WHEN NOT: do not make dense operational tables narrower than their observed minimums (`650px` wizard plan, `680px` users table) without an explicit responsive redesign; at mobile widths, stack or provide the existing horizontal table fallback rather than shrinking text indefinitely.

## Source consistency risks to resolve in a later implementation task

- `src/setup/ui.go:10-30` is the admin theme source, while `docs.html:11-19`, `:23-74`, and `:78-103` contain multiple historical style blocks. The final docs block says “Shared KitsuSync visual tokens” at `docs.html:77`, but it omits at least `--danger` and `--shadow` from the admin root set. Treat this as observed drift, not as permission to edit it in this documentation task.
- `src/setup/ui.go:156` references `var(--surface)` and `var(--border)`, while the root declarations shown at `:10-30` do not define those names. A later implementation audit should resolve these references to semantic tokens before relying on them for routing menus/dialogs. Do not infer fallback colors here.
- The audit evidence supports reducing repeated glass/glow and nested surfaces, but it does not authorize changing the current UI in this report: `docs/UI-UX-DISTRIBUTED-AUDIT.md:155`, `docs/audits/uiux-agent8-anti-ai.md:38`, and `docs/audits/uiux-agent12-adversarial.md:83`.

## Sources

- `src/setup/ui.go:8-103, 128-161, 180-220, 285-391, 490-491`
- `docs.html:10-103`
- `docs/CURRENT-IA-UI-SPEC.md:65-86, 122-155`
- `docs/UI-UX-DISTRIBUTED-AUDIT.md:28-42, 74-78, 108, 155`
- `docs/audits/uiux-agent8-anti-ai.md:28-38, 56-66, 142-151`
- `docs/audits/uiux-agent12-adversarial.md:67-83, 88`
- `docs/audits/uiux-agent0-inventory.md:135`
- `docs/UI-UX-RUNTIME-EVIDENCE.md:50-56, 118-133, 156`
