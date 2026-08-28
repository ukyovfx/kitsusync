# Agent 0 — Current source/token inventory

Status: documentation-only inventory. No UI code, route, behavior, or `DESIGN.md` was changed.

## Scope and evidence boundary

The current admin UI stylesheet is the Go raw string `adminThemeCSS` in `src/setup/ui.go:8-530`. The documentation page has its own inline styles in `docs.html:10-101`; it is a separate surface and should not be silently treated as the admin theme. `site.jsx` was searched for CSS declarations and is not a stylesheet source for this inventory. Runtime corroboration comes from `docs/UI-UX-RUNTIME-EVIDENCE.md:145-164` and the motion/typography audits. Values below are source-observed unless explicitly marked recommendation or unresolved.

Classification meanings:

- KEEP — preserve as an intentional current value or semantic token.
- NORMALIZE — current value is valid, but repeated or inconsistent enough to consolidate behind a token/component rule.
- REMOVE — recommendation to remove or disable a decorative/duplicate value; not an implementation claim.
- UNRESOLVED — source value or runtime impact exists, but the evidence is insufficient to decide its final design status.

## 1. Admin theme: canonical variable layer

Observed at `src/setup/ui.go:10-30`:

| Token | Current value | Classification | Rule (WHAT / WHY / WHEN / WHEN NOT) |
|---|---|---|---|
| `--bg` | `#070707` | KEEP | WHAT: global dark base. WHY: establishes the low-luminance admin canvas. WHEN: page/body and shell backgrounds. WHEN NOT: do not use for semantic status or control emphasis. |
| `--bg2` | `#0d0d0f` | UNRESOLVED | WHAT: declared secondary background. WHY: source declaration records intent, but no usage was established in the inspected CSS. WHEN: only after usage is confirmed. WHEN NOT: do not add new usage solely because it exists. |
| `--panel` | `rgba(20,21,24,.76)` | NORMALIZE | WHAT: translucent panel fill. WHY: reused by `.glass` and connection edit card; centralizes surface opacity. WHEN: owned elevated surfaces. WHEN NOT: not for every nested card or status badge. |
| `--panel-strong` | `rgba(14,15,18,.92)` | UNRESOLVED | WHAT: declared strong panel. WHY: no confirmed usage in the inspected declarations. WHEN: retain pending full selector/use search. WHEN NOT: do not infer rendered use from declaration alone. |
| `--panel-soft` | `rgba(255,255,255,.05)` | UNRESOLVED | WHAT: declared soft panel. WHY: usage is not established in the inspected CSS. WHEN: use only if an owning surface is confirmed. WHEN NOT: do not multiply equivalent `.03/.035/.04/.045/.05` fills without a surface rule. |
| `--line` | `rgba(255,255,255,.11)` | KEEP | WHAT: standard divider/border. WHY: shared low-contrast separation. WHEN: section boundaries and shared shell components. WHEN NOT: do not use for high-priority focus or danger boundaries. |
| `--line-strong` | `rgba(255,255,255,.2)` | UNRESOLVED | WHAT: declared strong line. WHY: current selector usage is not established in the inspected CSS. WHEN: retain until full usage census. WHEN NOT: do not assume every `.18` or `.2` direct border is this token. |
| `--text` | `#f7f7f4` | KEEP | WHAT: primary text. WHY: high-contrast operational content. WHEN: headings, values, primary controls. WHEN NOT: do not use as the only status signal. |
| `--muted` | `#b8b5ae` | KEEP | WHAT: secondary text. WHY: hierarchy for help/metadata. WHEN: supporting copy and non-primary labels. WHEN NOT: not for essential instructions or disabled-only text. |
| `--muted-2` | `#8f8a83` | NORMALIZE | WHAT: tertiary text. WHY: repeated metadata/field-help role. WHEN: captions, field help, low-priority metadata. WHEN NOT: not for required actions or long body copy. |
| `--accent` | `#e85a1a` | KEEP | WHAT: warm action accent. WHY: identifies primary action and active state. WHEN: primary controls, active navigation, accent rules. WHEN NOT: not as the sole error/success encoding. |
| `--accent-2` | `#ff8d48` | KEEP | WHAT: lighter warm accent. WHY: readable accent text and gradient endpoint. WHEN: labels, active metadata, button gradients. WHEN NOT: do not treat it as a separate semantic state. |
| `--accent-glow` | `rgba(232,90,26,.34)` | UNRESOLVED | WHAT: declared accent glow. WHY: direct glow values also appear elsewhere, so ownership is unclear. WHEN: after usage and contrast are verified. WHEN NOT: do not broaden glow to operational surfaces. |
| `--danger` | `#ff6a50` | KEEP | WHAT: destructive/error accent. WHY: semantic danger distinction. WHEN: danger buttons, danger borders, failed states. WHEN NOT: not for generic attention or warning. |
| `--success` | `#8ecf8b` | KEEP | WHAT: success accent. WHY: semantic completion/healthy state. WHEN: success badges, completed steps, notices. WHEN NOT: not as decoration unrelated to state. |
| `--shadow` | `0 24px 80px rgba(0,0,0,.46)` | NORMALIZE | WHAT: elevated surface shadow. WHY: shared visual depth, currently used by `.glass`, `.tile`, and connection edit container. WHEN: major elevated surfaces. WHEN NOT: do not stack it on every nested card or small control. |
| `--radius-xl/lg/md/sm` | `24px / 17px / 14px / 10px` | NORMALIZE | WHAT: declared radius scale. WHY: a useful scale exists, but many selectors use direct `12/14/15/16/18/20/24px` and pill `999px`. WHEN: component surfaces and controls. WHEN NOT: do not replace semantic pills or circular icon shapes with a rectangular scale. |

## 2. Admin color, opacity, surface, and shadow families

Observed direct families in `src/setup/ui.go:99-147`, `:203-209`, `:224-265`, `:289-364`, `:378-480`:

| Family | Observed values/usages | Classification | Rule (WHAT / WHY / WHEN / WHEN NOT) |
|---|---|---|---|
| White overlays | `rgba(255,255,255,.025/.03/.035/.04/.045/.05/.06/.065/.08/.1/.12/.14/.16/.18)` across nav, cards, rows, tables, controls, and workflow states | NORMALIZE | WHAT: layered dark-surface fills and borders. WHY: current hierarchy depends on subtle opacity differences, but the family is fragmented. WHEN: consolidate by surface level after runtime comparison. WHEN NOT: do not flatten all overlays to one value; do not introduce glass everywhere. |
| White borders | `rgba(255,255,255,.05/.07/.08/.09/.1/.12/.14/.16/.18/.2)` | NORMALIZE | WHAT: separation and control outlines. WHY: repeated direct values overlap with `--line` and `--line-strong`. WHEN: map to semantic border roles. WHEN NOT: do not normalize focus, danger, or status borders into the neutral line token. |
| Orange overlays | `rgba(232,90,26,.05/.06/.08/.1/.12/.14/.16/.18/.2/.22/.34)` and `rgba(255,141,72,.08/.12/.7/.88/.94)` | NORMALIZE | WHAT: accent backgrounds, glows, dragging/active states, and gradients. WHY: same hue serves several roles. WHEN: split into action, active, highlight, and decorative roles. WHEN NOT: do not use warm fill for success/warning/error semantics. |
| Semantic green | `#8ecf8b`, `#a6e0a2`, `#9ddd99`, `#d7f4d4`, plus rgba `.07/.08/.09/.1/.12/.18/.25/.28/.3/.42` | NORMALIZE | WHAT: success/completed/healthy family. WHY: multiple text and surface intensities are deliberate but un-tokenized. WHEN: map by semantic state and contrast role. WHEN NOT: not for neutral completion-looking decoration. |
| Semantic warning | `#ffd978/#ffe09a/#fff1c4/#ffc850`, `rgba(255,200,80,.1/.14/.3/.34/.44)` | NORMALIZE | WHAT: warning family. WHY: repeated direct values need semantic naming and contrast checks. WHEN: warning badges, labels, and values. WHEN NOT: do not use orange danger values interchangeably. |
| Semantic danger | `#ff6a50/#ffb4a7/#ffb8aa/#ffd3ca/#fff5f2`, `rgba(255,106,80,.035/.08/.13/.18/.25/.28/.3/.34/.4/.44)` | NORMALIZE | WHAT: failure/destructive family. WHY: consistent meaning is present but literal values are scattered. WHEN: danger controls, error values, destructive blocks. WHEN NOT: not for ordinary emphasis. |
| Informational blue | `#6dc3ff/#8bd2ff/#e9f7ff`, `rgba(109,195,255,.06/.08/.1/.12/.15/.18/.2/.22/.3/.32)` | NORMALIZE | WHAT: info/current-step/focus family. WHY: appears in focus outlines, setup state, preview, and workflow emphasis. WHEN: reserve by semantic role and verify contrast. WHEN NOT: do not make blue a competing primary action color. |
| Shadows | `--shadow`; `0 10px 24px rgba(232,90,26,.28)`; `0 14px 30px rgba(232,90,26,.24)`; `0 12px 28px rgba(0,0,0,.28)`; inset white highlights and blue/orange inset rings | NORMALIZE | WHAT: elevation, action glow, menu overlay, and inset emphasis. WHY: shadows have mixed structural and decorative purposes. WHEN: retain elevation on major surfaces and menus; use glow only for action affordance. WHEN NOT: do not use ambient glow to compensate for weak hierarchy. |

Evidence note: the runtime evidence confirms the Outfit stack and source-level motion behavior for reached Login/authenticated states, but does not prove every protected surface visually. `docs/UI-UX-RUNTIME-EVIDENCE.md:154-158` explicitly records the runtime boundary and deferred connected-Production state.

## 3. Admin gradients, decoration, and motion

Observed:

- Body background: two warm radial gradients over a vertical dark gradient at `src/setup/ui.go:39-43`.
- Ambient dot/texture layer: radial dot field plus two linear layers, `opacity:.22`, `particleDrift 18s linear infinite`, at `src/setup/ui.go:45-56`.
- Secondary warm radial glow: `opacity:.55` at `src/setup/ui.go:58-65`.
- `riseIn`: `opacity:0` and `translateY(10px)` to final state at `src/setup/ui.go:71-74`; applied to `.tile`, `.section-card`, `.page-card` for `.42s ease both` at `src/setup/ui.go:291-292`.
- Interaction transitions: nav/action links `all .18s`, language thumb `left .18s`, tiles `.18s`, fields `.18s`, buttons `.18s`, accordion caret `.18s`; see `src/setup/ui.go:224-266`, `:349-363`, `:396-401`.
- Reduced-motion override is present at `src/setup/ui.go:524-529`: ambient/card animations, listed transitions, and listed hover transforms are disabled.

| Item | Classification | Rule (WHAT / WHY / WHEN / WHEN NOT) |
|---|---|---|
| Body warm gradients and dot motif | NORMALIZE | WHAT: global decorative backdrop. WHY: current visual identity uses warm dark atmosphere, while audits note competition with dense operational content. WHEN: retain only where it does not reduce reading hierarchy. WHEN NOT: do not add additional global gradients or particles to data-heavy screens. |
| Continuous `particleDrift` | REMOVE (recommendation) | WHAT: viewport-wide moving decoration. WHY: it is not task feedback and prior audit evidence identified distraction/reduced-motion risk. WHEN: remove from operational surfaces, or confine to a brand/login treatment if explicitly accepted. WHEN NOT: never let it remain active under reduced motion. |
| `riseIn` entrance animation | NORMALIZE | WHAT: short card entrance. WHY: low amplitude but applies broadly to operational cards. WHEN: use for meaningful view entry if runtime confirms it helps orientation. WHEN NOT: no staggered spectacle or repeated animation on every refresh. |
| Hover/field/button transitions | KEEP with policy | WHAT: short interaction feedback. WHY: supports affordance. WHEN: keep at `.18s`-scale for direct interaction and disable under reduced motion. WHEN NOT: do not use `transition: all` when property-specific transitions are possible. |

The earlier audit text at `docs/audits/uiux-runtime-agentD-motion.md:18-50` describes a no-override state, but the current source now contains the override at `src/setup/ui.go:524-529`; the later runtime evidence records this as source fixed while protected reduced-motion emulation remains deferred at `docs/UI-UX-RUNTIME-EVIDENCE.md:156`. This is a source/evidence chronology distinction, not a reason to remove the current policy.

## 4. Admin spacing, layout, radii, and breakpoints

Observed base/layout values:

- Shell: max width `1100px`, centered, padding `20px 14px 48px` at `src/setup/ui.go:83-89`.
- Top bar: gap `12px`, bottom margin `18px`; actions gap `7px` at `src/setup/ui.go:90-99`, `:191-198`.
- Section navigation: gap `8px`, padding `8px`, radius `14px`, bottom margin `18px` at `src/setup/ui.go:99-102`.
- Navigation card: padding `6px`, radius `16px`, gap `4px`; nav chips min-height `44px`, padding `8px 12px` at `src/setup/ui.go:210-235`.
- Main page card: radius `24px`, padding `20px`; section card radius `20px`, padding `14px`; tile min-height `152px`, padding `14px`, radius `20px` at `src/setup/ui.go:285-300`.
- Common grids: dashboard 3 columns/gap `10px`; form min column `210px`/gap `9px`; connection/edit grids 2 columns/gap `12px` at `src/setup/ui.go:290`, `:328`, `:346`.
- Controls: text/select/textarea radius `14px`, padding `10px 12px`, min-height `44px`; buttons radius `999px`, min-height `44px`, padding `8px 14px`; small button padding `6px 10px` at `src/setup/ui.go:349-363`.
- Focus: `outline:3px solid #6dc3ff; outline-offset:3px`; input focus ring `0 0 0 3px rgba(232,90,26,.16)` at `src/setup/ui.go:351-353`.

Breakpoints observed:

| Breakpoint | Current behavior | Classification | Rule (WHAT / WHY / WHEN / WHEN NOT) |
|---|---|---|---|
| `1100px` | Dashboard menu changes to 3 columns at `src/setup/ui.go:482`. | KEEP | WHAT: intermediate density adjustment. WHY: avoids five-card squeeze. WHEN: viewport at or below 1100px. WHEN NOT: do not infer a universal container breakpoint. |
| `960px` | Top actions wrap; dashboard/summary/workflow grids reduce; primary nav scrolls; production overview grids reduce at `src/setup/ui.go:124-127`, `:483`, `:487`. | NORMALIZE | WHAT: tablet/intermediate layout. WHY: many components independently respond here. WHEN: preserve behavior while consolidating shared layout decisions. WHEN NOT: do not assume all 960px rules represent one visual breakpoint. |
| `760px` | Rows stack, nav becomes mobile details, forms and tables reflow, controls stretch at `src/setup/ui.go:158-161`, `:370-377`, `:489`. | KEEP | WHAT: primary mobile transition. WHY: broad narrow-screen adaptation. WHEN: at or below 760px. WHEN NOT: do not remove without replacing keyboard/mobile navigation and table behavior. |
| `640px` | Shell/card typography and grids compact; dashboard/menu/summary become one column; controls stack at `src/setup/ui.go:484`, `:488`. | NORMALIZE | WHAT: small-mobile density step. WHY: combines many local rules. WHEN: keep only where content evidence supports it. WHEN NOT: do not force all surfaces to one-column if a two-column data view remains readable. |
| `480px` | Wizard steps become a vertical grid; cards lose shadows; shell padding reduces to `7px 6px 28px` at `src/setup/ui.go:485-486`. | NORMALIZE | WHAT: very narrow fallback. WHY: protects fit and reduces visual weight. WHEN: verify against real JP/EN content. WHEN NOT: do not trade away essential focus visibility or touch target size. |

Runtime evidence confirms sampled dimensions and no horizontal overflow for reached Login and Connections edit states, while connected-Production detail remains deferred: `docs/UI-UX-RUNTIME-EVIDENCE.md:84`, `:154-158`.

## 5. Typography

Observed at `src/setup/ui.go:9`, `:36-43`, `:180-188`, `:269-284`, `:287-300`, `:348`, `:355`, `:357`:

| Role | Current value | Classification | Rule (WHAT / WHY / WHEN / WHEN NOT) |
|---|---|---|---|
| Body | `"Outfit","Noto Sans JP",sans-serif`; `13px`; letter spacing `.01em` | KEEP / UNRESOLVED loading | WHAT: operational body stack. WHY: current source and runtime computed family agree. WHEN: body, data, help, and forms. WHEN NOT: do not claim webfont glyph provenance; runtime evidence leaves fallback provenance deferred (`docs/UI-UX-RUNTIME-EVIDENCE.md:157-158`). |
| Display/labels/actions | `"Space Grotesk","Outfit",sans-serif`; labels uppercase `.16em`; buttons `.04em`; nav/metadata variants `.08-.14em` | NORMALIZE | WHAT: secondary UI face and tracking system. WHY: consistent source intent, but many local sizes/tracking values exist. WHEN: headings, labels, actions, compact metadata. WHEN NOT: avoid wide uppercase tracking for long JP strings or body prose. |
| Major headings | page h1 `26px`, `line-height:1.03`, `letter-spacing:-.03em`; system status desktop overrides to `32px` at `src/setup/ui.go:287`, `:515-523` | NORMALIZE | WHAT: display hierarchy. WHY: local context overrides are explicit but duplicated. WHEN: map to named heading tiers. WHEN NOT: do not apply display tracking to tables/status values. |
| Help/metadata | `11px/12px`, line heights `1.45-1.7`, `var(--muted-2)` or `var(--muted)` | KEEP | WHAT: supporting text. WHY: clear secondary hierarchy. WHEN: field help, captions, metadata. WHEN NOT: not for required user instructions. |

## 6. Controls, status, and state tokens

Observed:

- Status badges/pills use pill radius, `5px 8-9px` padding, `font-size:.78rem` or `12px`, and semantic green/yellow/red/orange/neutral fills at `src/setup/ui.go:138-143`, `:365-369`.
- Disabled controls use `opacity:.72` and `cursor:not-allowed` at `src/setup/ui.go:354`, `:359-360`.
- Readonly/disabled fields and buttons share the same opacity; this is a current behavior, not proof that all disabled states are visually equivalent.
- Setup step states use green done, blue active, translucent neutral pending/blocked at `src/setup/ui.go:434-445`.
- Dragging rows use `opacity:.45` and warm background at `src/setup/ui.go:454`, `:459`, and `:502`.
- Modal backdrop uses `rgba(0,0,0,.72)` and delete box radius `20px` at `src/setup/ui.go:420-423`.

Classification: NORMALIZE the semantic state family, KEEP the 44px control/focus baseline, and mark disabled-vs-readonly visual distinction UNRESOLVED until the full interaction matrix is reviewed. Rule: WHAT = state must be encoded by text/icon plus color; WHY = operational status cannot rely on color alone; WHEN = badges, setup steps, notices, and destructive controls; WHEN NOT = do not reuse success/warning/danger fills as generic decoration.

## 7. Documentation page (`docs.html`) — separate token inventory

Observed source structure:

1. Initial dark editorial/glass treatment at `docs.html:10-19`: `#050606`, `#f5f5f2`, Inter/system stack, dot and warm gradients, blur, shadows, radius `16/18/20/22/28px`, and `docDrift 20s`.
2. “Official documentation treatment” override at `docs.html:21-74`: flat `#101216` / `#17191e`, `#2b2f36` lines, no body pseudo-elements, no blur, no animation, 244px nav, 960px container, and breakpoints `900/700/560px`.
3. Later root/theme block at `docs.html:78-101`: reintroduces admin-like variables (`--bg:#070707`, `--panel`, `--accent`, radius scale), Outfit/Space Grotesk, warm gradients and interactive nav styling, while retaining later overrides for some documentation selectors.

| Documentation token/cluster | Classification | Rule (WHAT / WHY / WHEN / WHEN NOT) |
|---|---|---|
| Three layered style blocks with conflicting base/override values | REMOVE (recommendation) | WHAT: duplicate style authority. WHY: cascade makes “current value” selector-dependent and hard to audit. WHEN: consolidate after capturing intended rendered docs state. WHEN NOT: do not delete an earlier block before verifying fallback/loading behavior. |
| Flat documentation palette `#101216/#17191e/#2b2f36` | KEEP candidate | WHAT: quiet reading surface from the official treatment at `docs.html:21-74`. WHY: aligns with documentation readability. WHEN: docs-only surface. WHEN NOT: do not assume it is the admin shell palette. |
| Warm/glass first block and reintroduced theme block | NORMALIZE / UNRESOLVED | WHAT: competing editorial and product-theme treatments. WHY: source contains both and cascade interaction needs rendered confirmation. WHEN: decide from current docs runtime/intent. WHEN NOT: do not copy its gradients or glass values into admin screens. |
| Docs breakpoints `900/700/560px` | KEEP for docs, UNRESOLVED cross-surface | WHAT: docs-specific responsive rules. WHY: docs layout differs from admin UI. WHEN: documentation page only. WHEN NOT: do not merge with admin `1100/960/760/640/480px` without a cross-surface decision. |
| Docs motion `docDrift 20s` and initial `rise .42s` | REMOVE (recommendation) | WHAT: decorative docs motion. WHY: duplicates the ambient-motion risk identified for the admin theme. WHEN: disable for reading-oriented documentation, especially reduced motion. WHEN NOT: do not claim it is present in the admin runtime. |

## 8. Consolidation rules and unresolved questions

Recommended design-system rules, based on the observed inventory:

1. WHAT: retain one semantic source for canvas, surface, line, text, muted text, accent, success, warning, danger, info, elevation, and radius. WHY: current variables already establish most of this vocabulary, while direct literals fragment it. WHEN: any future CSS normalization pass. WHEN NOT: do not normalize values whose semantic ownership is not proven.
2. WHAT: keep separate surface roles for canvas, major elevated card, nested card, control, status, and overlay. WHY: the current `.025-.08` white overlays and translucent panels serve different depths. WHEN: mapping repeated literals to tokens. WHEN NOT: do not collapse all translucent surfaces into one “glass” token.
3. WHAT: separate structural shadow, action glow, menu shadow, and inset highlight. WHY: their purposes differ. WHEN: shadow consolidation. WHEN NOT: do not use decorative glow as a substitute for contrast or hierarchy.
4. WHAT: preserve 44px minimum interactive controls and 3px focus outline unless a measured accessibility review authorizes a change. WHY: current source sets these consistently at `src/setup/ui.go:349-353`, and runtime evidence confirms focus/controls for reached Connections edit states at `docs/UI-UX-RUNTIME-EVIDENCE.md:157`. WHEN: all interactive controls. WHEN NOT: do not shrink solely to fit a dense layout.
5. WHAT: preserve the reduced-motion override and extend it only by audited selector ownership. WHY: current source disables ambient/card motion and listed transitions at `src/setup/ui.go:524-529`. WHEN: any motion change. WHEN NOT: do not infer protected-page runtime confirmation beyond the evidence boundary in `docs/UI-UX-RUNTIME-EVIDENCE.md:156`.
6. WHAT: treat `docs.html` tokens as docs-only until a deliberate cross-surface decision. WHY: it contains independent, conflicting style blocks at `docs.html:10-101`. WHEN: documentation redesign or cleanup. WHEN NOT: do not use it as evidence of admin runtime styling.

Unresolved from source/evidence alone: actual use of several declared root variables (`--bg2`, `--panel-strong`, `--panel-soft`, `--line-strong`, `--accent-glow`); final intended authority among the three `docs.html` style blocks; webfont file/glyph provenance and offline fallback; authenticated connected-Production visual states; and whether every direct opacity/radius value is intentionally semantic or historical duplication. The runtime evidence explicitly keeps those boundaries open at `docs/UI-UX-RUNTIME-EVIDENCE.md:154-164`.
