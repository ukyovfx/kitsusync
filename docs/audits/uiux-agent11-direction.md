# Agent 11 — Editorial Systems Interface direction audit

Date: 2026-08-28
Scope: visual direction and differentiation only
Decision: REFINE

## Executive conclusion

The intended Editorial Systems Interface should be refined into a quiet, operational editorial system. Keep the production-first information architecture, explicit service separation, bilingual parity, and readable status hierarchy. Reduce the current visual theater: the animated particle field, repeated orange glow, translucent glass treatment, large rounded containers, and display-font styling currently compete with the state and action the operator came to inspect.

This is a direction audit, not a redesign specification. No UI, CSS, route, behavior, external-system, or Git change was made.

## Method and evidence boundary

Evidence means a directly observed repository fact, cited to a file/line or current IA contract. Inference means a design judgment derived from those facts and the supplied comparison references. “Accept / Refine / Reject” below is a directional judgment, not proof of user preference.

I did not read or use other agents’ audit conclusions. I inspected the implementation and the canonical Current IA UI spec only. No browser-rendered screen or authenticated runtime was available during this audit.

## Current visual evidence

| Evidence | Route/screen | Severity | Confidence |
|---|---|---:|---:|
| The global theme imports Outfit and Space Grotesk, sets a 13px body size, and uses near-black surfaces with orange accent tokens. | Shared shell; all `/bot/*` screens | High | High |
| The body has fixed radial gradients, a dot field, and an 18-second `particleDrift` animation. | Shared shell; all routes | High | High |
| `.glass` combines translucent panels, 18px backdrop blur, inset highlight, and a large shadow; tiles and cards add 18–24px radii and gradients. | Shared cards; Dashboard, Connections, Production detail, Status | High | High |
| Dashboard management is a five-column equal-width card grid, with a separate orange-accented New Connection CTA. | `/bot/admin` | Medium | High |
| Production overview uses four summary cards plus a full-width current-issues card; routing separates summary, editor, and preview. | `/bot/admin/projects?project=<id>` | Low/positive | High |
| System Status uses peer API cards with charts and auto-refresh; chart geometry and typography are defined in the Current IA contract. | `/bot/admin/health` | Medium | High |
| Current IA requires content-sized badges, 24px section spacing, independent Kitsu/Discord service groups, equal-height peers, and no normal desktop horizontal overflow. | All Current IA routes | High | High |
| The source implementation includes responsive breakpoints at 960px, 760px, and 640px, but the visual result was not rendered here. | Shared shell and responsive grids | Medium | Medium |

Primary code evidence: `src/setup/ui.go:8-70, 77-96, 197-203, 285-293, 355-380`; Dashboard construction: `src/setup/ia_views.go:2864-2895`; route and state contract: `docs/CURRENT-IA-UI-SPEC.md`.

## Reference comparison: ACCEPT / REFINE / REJECT

### Linear — REFINE, do not imitate

Accept the reference principle of dense, predictable work surfaces: stable hierarchy, compact navigation, and emphasis on the object being operated on. This aligns with Productions, routing, System Status, and Audit Log.

Refine the current implementation toward that principle by making the active route and primary action visually quieter but more explicit. The five equal dashboard cards currently give every destination comparable weight; a production/operator workspace should make “what needs attention” and the selected Production the strongest reading path. Do not copy Linear’s product vocabulary, purple/black palette, or interaction patterns; differentiation must come from Kitsu Production state, Kitsu→Discord routing, and safe preview-before-send workflows.

Reject using Linear as a claim that all screens should become compact, monochrome, or keyboard-first. The setup wizard and bilingual status explanations need more breathing room than an issue tracker.

Evidence: the Current IA fixes Dashboard order and gives Productions a dedicated detail route; `src/setup/ui.go:380` defines equal dashboard cards. Inference: the present card parity is visually democratic but not yet editorially directional.

### Apple Human Interface Guidelines — REFINE selectively

Accept legibility, hierarchy, adaptable layout, and clear separation of content from controls. Those principles support the existing 44px mobile navigation/control treatment and the Current IA requirements for readable type and responsive peers.

Refine the material language. Translucency should clarify layers, not become the brand’s primary visual event. Keep one restrained surface model, reserve accent for actionable or semantic emphasis, and make state readable through label, value, and placement rather than glow.

Reject platform mimicry. This is a browser-based operations console, not an Apple-platform application; adopting system metaphors, Liquid Glass styling, or platform-specific controls would dilute the Kitsu/Discord operational identity.

Reference: [Apple HIG — Typography](https://developer.apple.com/design/human-interface-guidelines/typography) and [Apple HIG — Layout](https://developer.apple.com/design/human-interface-guidelines/layout). These support the principles above; they do not prescribe this product’s visual brand.

### Restrained developer tools — ACCEPT as the operational baseline

Accept compact metadata, explicit status vocabulary, predictable navigation, low-decoration surfaces, and visible causality between a state and its next action. This is the strongest fit for Connections, System Status, Audit Log, routing, and troubleshooting.

Refine rather than flatten: retain editorial signifiers in page titles, section labels, and occasional empty/complete states, while keeping tables, logs, credentials, charts, and badges utilitarian. A quiet interface can still have a recognizable accent and typographic voice.

Reject “developer tool” as permission for dense unexplained IDs, raw diagnostics, or dark-mode-only ambiguity. The Current IA explicitly excludes unnecessary internal IDs and secrets from normal views.

Evidence: `docs/CURRENT-IA-UI-SPEC.md` defines safe diagnostics, status vocabulary, and compact chart metadata. Inference: restrained density is compatible with the product’s safety boundary only when human-readable labels remain primary.

### Editorial / mincho — REFINE to a narrow identity layer

Accept editorial contrast and mincho/serif as a limited signal of authorship, record, and considered review. A serif can differentiate the page title or a production-context heading from generic SaaS chrome.

Refine with a strict usage boundary: do not use mincho for navigation, form labels, tables, charts, badges, timestamps, or dense bilingual metadata. Test Japanese glyph coverage, mixed Latin/Japanese baselines, long Production names, and fallback behavior before adoption. The current implementation uses Outfit with Noto Sans JP fallback, so a mincho direction is not currently evidenced as implemented.

Reject an editorial-magazine treatment across the whole console. It would make operational rows feel ornamental, increase bilingual width risk, and weaken rapid scanning.

### Neuform — REJECT as a literal visual system; retain only its discipline

Reject copying a named reference style without a repository-provided definition or approved visual artifact. No local Neuform reference, token set, or component source was found in the inspected implementation.

Retain only the useful abstraction: a disciplined, authored system with deliberate geometry, controlled contrast, and a small number of repeated rules. That discipline should be expressed through KitsuSync’s own production/routing language, not through an externally recognizable skin.

## Route and screen direction

| Route/screen | Direction | Severity | Confidence |
|---|---|---:|---:|
| `/bot/admin` Dashboard | Keep fixed order and five management destinations. Refine hierarchy so the attention queue and New Production Connection are primary; management cards become quieter navigation. | High | High |
| `/bot/admin/projects` Productions | Use a calm list/table surface with name → state → action rhythm. Keep `Disconnected` visibly warning yellow and content-sized. | High | High |
| `/bot/admin/projects?project=<id>` Production detail | Keep four equal summary cards, issue summary, and tab/section structure. Reduce nested glass-on-glass contrast and let the Production name/context carry the editorial identity. | Medium | High |
| `/bot/admin/bot` and `?edit=1` Connections | Treat Kitsu and Discord as two equal service records. Make credential forms quiet and high-trust; no decorative emphasis around secrets. | High | High |
| `/bot/admin/users` User Linking | Favor readable people/identity rows over card spectacle. Keep human-vs-bot distinction and bilingual labels visible. | Medium | High |
| `/bot/admin/health` System Status | Keep peer Kitsu/Discord charts, exact response values, and safe details. Use accent only for current state and selected window; remove decorative chart treatment if it competes with the value. | High | High |
| `/bot/admin/audit` Audit Log | Use restrained developer-tool density: timestamp, operation, result, and expandable safe detail. Editorial styling belongs in the page heading only. | Medium | High |
| `/bot/setup` New Production setup | Keep a warmer editorial cue than admin/status screens, but preserve a single linear task path. Avoid making the wizard look like a marketing landing page. | High | Medium |

## Directional decisions

1. Keep: production-first IA, explicit Kitsu/Discord peer model, content-sized semantic badges, bilingual parity, chart exactness, and visible next actions.
2. Refine: surface hierarchy, type hierarchy, dashboard emphasis, and the boundary between identity styling and operational data.
3. Remove from the intended direction: persistent particle motion, repeated orange glow, glass as the default container identity, large uniform radii on every object, and display-font emphasis in dense operational content.
4. Do not introduce: new routes, new interaction models, decorative connection maps, fabricated metrics, or reference-specific visual copying.

## Unverified scope

- No browser screenshot, live 8090 preview, or authenticated state was inspected; pixel-level judgments about actual contrast, clipping, chart legibility, hover/focus states, and responsive wrapping remain untested.
- No light-theme, OS font fallback, zoom/text-scaling, reduced-motion, or high-contrast rendering was tested.
- No real data set was used to test long Japanese/Latin Production names, zero/large counts, error states, empty states, or mixed-language screens.
- Linear, Apple HIG, and the supplied “editorial/mincho” and “Neuform” labels were used as directional comparison axes, not as a usability benchmark or user research substitute. No approved Neuform artifact was present locally.

## Sources

- Repository implementation: `src/setup/ui.go`, `src/setup/ia_views.go`, `src/setup/admin.go`, `src/setup/root_route.go`.
- Canonical product contract: `docs/CURRENT-IA-UI-SPEC.md`.
- Linear: [Welcome to the new Linear — design refresh](https://linear.app/changelog/2024-03-20-new-linear-ui) and [A calmer interface for a product in motion](https://linear.app/now/behind-the-latest-design-refresh).
- Apple: [Human Interface Guidelines — Typography](https://developer.apple.com/design/human-interface-guidelines/typography) and [Human Interface Guidelines — Layout](https://developer.apple.com/design/human-interface-guidelines/layout).
