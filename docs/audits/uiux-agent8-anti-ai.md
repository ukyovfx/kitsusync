# Agent 8 — Anti-AI pattern audit

## Conclusion

The strongest harmful pattern is not one isolated component. It is the repeated combination of glass panels, warm gradients/glows, rounded containers, and pill controls across operational screens. That combination makes a server-management product resemble a generic AI/SaaS template and weakens the visual distinction between content, status, and action.

This audit is based on the target repository's source-rendered HTML builders and CSS. It does not use conclusions from other agent reports. No UI behavior, route, CSS, or application code was changed.

## Route and screen coverage

| Route / screen | Source evidence inspected | Screen-level pattern relevant to this audit |
|---|---|---|
| `/bot/admin` Dashboard | `src/setup/ia_views.go:245-331`, `src/setup/ui.go:378-381,484-489` | Four summary metrics, attention queue, five management cards, and a global glass page shell. |
| `/bot/admin/projects` Production list | `src/setup/ia_views.go:488-499` | Repeated `section-card glass` rows with status pill and action. |
| `/bot/admin/projects?project=<id>` Production detail | `src/setup/ia_views.go:549-576,707-716,837-837` | Tab navigation plus nested glass sections, summary cards, status pills, and detail panels. |
| `/bot/admin/health` System Status | `src/setup/ia_views.go:1338-1411`, `src/setup/admin.go:2056-2336` | Peer API cards, expandable runtime details, nested cards/tables, and the same global visual treatment. |
| `/bot/setup` New Production setup | `src/setup/ia_views.go:2362-2842` | Wizard step cards, plan/review cards, rounded status indicators, and gradient/glow emphasis. |
| `/bot/admin/bot` and `?edit=1` Connections | `src/setup/admin.go:2911-3150`, `src/setup/ui.go:296-337` | Connection cards intentionally separate Kitsu and Discord, but still nested in the shared glass/page-card system. |
| `/bot/admin/users` User Linking | `src/setup/ia_views.go:1973-1973,2195-2195`, `src/setup/admin.go:2340-2745` | Table/content regions wrapped by the same `section-card glass` surface. |
| `/bot/docs`, `/bot/docs/`, `/bot/docs/site.jsx` Documentation | `src/docs_routes.go:13-37`, `docs.html:11-80` | Separate static screen; its final CSS removes the initial particle layer but reintroduces the shared warm radial background at `docs.html:78-80`. |

## Findings by classification

### Harmful / AI-like

#### Major — Glassmorphism used as the default semantic container

Evidence: the global `.glass` class applies translucent background, `backdrop-filter`, border, and a large shadow at `src/setup/ui.go:197-203`. `adminPage` wraps every normal admin body in `page-card glass` at `src/setup/admin.go:4080-4082`; many child sections then repeat `section-card glass`, for example Production detail at `src/setup/ia_views.go:707-716` and System Status runtime details at `src/setup/admin.go:2293-2309`.

Judgment: the visual treatment is acceptable for a deliberately layered overlay, but harmful here because it is the default for ordinary tables, forms, diagnostics, and status summaries. The repeated containment creates “card soup” and reduces the perceived difference between a live operational surface and a decorative panel.

Human impact: operators must visually parse surface styling before they can identify the actual information boundary; nested panels also make high-risk actions and diagnostic details less visually distinct.

Severity: major. Confidence: high.

Evidence vs inference: the CSS and nesting are directly observed. The scan-cost and semantic-confusion impact is an inference from that repeated structure and should be validated with an operator walkthrough.

Concrete correction: reserve glass/elevation for one intentional layer per screen and render ordinary data groups as flat regions, rules, or table sections.

#### Major — Global ambient particle field and radial glow without a screen-specific role

Evidence: the admin body paints multiple radial gradients at `src/setup/ui.go:34-43`; `body::before` adds a fixed dotted field and animates it for every admin page at `src/setup/ui.go:45-70`; `body::after` adds another fixed orange radial glow at `src/setup/ui.go:58-66`. The same warm radial background is restored in the documentation token layer at `docs.html:78-80`.

Judgment: this is an AI-like ambient decoration because it is global rather than tied to a meaningful state, workflow stage, or visualization. It is not inherently harmful on an identity/loading screen, but it is harmful on Dashboard, Production, Connections, Users, and System Status screens where persistent movement and glow compete with operational content.

Human impact: visual noise behind tables, forms, and status decisions can make the console feel less stable and less trustworthy; motion may also distract users who are monitoring failures.

Severity: major. Confidence: high for presence; medium for the exact perceptual impact because no runtime visual measurement was performed.

Evidence vs inference: fixed pseudo-elements, opacity, and animation are direct evidence. Distraction and trust impact are inferences requiring browser testing with realistic data and reduced-motion settings.

Concrete correction: remove the ambient layer from operational screens; retain a static, low-contrast identity treatment only where it carries product meaning.

#### Major — Gradient/glow styling is attached to primary actions and ordinary cards

Evidence: `.tile` and `.section-card` use layered gradients at `src/setup/ui.go:285-293`; `.dashboard-cta` uses a warm gradient and orange shadow at `src/setup/ui.go:380`; `.btn` and `.btn-sm` use two-stop gradients and glow shadows at `src/setup/ui.go:351-358`.

Judgment: the warm color is not the specific purple-gradient cliché, but the pattern is still AI-like when every major surface and primary action receives the same “premium” treatment. The repeated glow makes action hierarchy less specific: a card, CTA, and submit button all speak with similar visual intensity.

Human impact: users may over-read decoration as importance and under-read the actual operational priority encoded by status and action labels.

Severity: major. Confidence: high for the pattern; medium for the resulting priority confusion.

Evidence vs inference: gradient declarations and their selectors are direct evidence. The hierarchy impact is an inference from their reuse across unrelated screen roles.

Concrete correction: keep one accent treatment for the single primary action; use flat surfaces and restrained rules for information containers.

#### Major — `transition: all` on navigation controls

Evidence: `.nav-chip`, `.home-link`, and `.action-link` use `transition:all .18s ease` at `src/setup/ui.go:218-228`.

Judgment: this matches the named `transition-all` anti-pattern. It allows unrelated properties to animate and makes the interaction contract depend on future CSS changes rather than on an explicit set of properties.

Human impact: state changes can feel less predictable, and focus/interaction behavior is harder to reason about during keyboard and responsive use.

Severity: major. Confidence: high.

Evidence vs inference: the declaration is direct evidence. The maintainability and predictability impact follows from the unspecified property set; actual focus timing remains untested.

Concrete correction: transition only the intended visual properties, or use no transition for navigation state changes.

### Acceptable AI-adjacent patterns

#### Status pills — acceptable when they encode a real state

Evidence: status classes map to explicit states such as success, warning, danger, blocked, and neutral at `src/setup/ui.go:359-364,485`; Production list rows render a state pill with an action at `src/setup/ia_views.go:488-499`; System Status renders service health badges at `src/setup/ia_views.go:1411-1411`.

Judgment: pills are a familiar AI/SaaS visual pattern, but they are acceptable here because they encode connection, readiness, and failure states. They become harmful only when used for non-state decoration or when the label is too vague to support a decision.

Severity: minor watch item. Confidence: high.

Concrete correction: keep pills for bounded states, pair them with a specific label, and do not use them as generic emphasis badges.

#### Equal peer grids — acceptable where the information is genuinely peer-level

Evidence: the Dashboard renders four summary metrics at `src/setup/ia_views.go:320-320` and five distinct management destinations through `renderDashboardMenuRefined` at `src/setup/ia_views.go:317-323,2864-2890`; responsive grid rules are at `src/setup/ui.go:477-478`.

Judgment: this is not automatically the “3-column feature grid” problem. The items represent distinct operational destinations or decision-linked counts, not invented marketing features. The pattern becomes harmful if all peers are visually equal despite different urgency or if the grid is used to avoid choosing a next action.

Severity: not a problem by source evidence; confidence: medium because actual operator prioritization was not tested.

Concrete correction: preserve peer grids only where the peer relationship is real; make urgency/action priority explicit in content and ordering.

#### Eyebrows and step labels — acceptable when ordinal or workflow-related

Evidence: selected Production uses an eyebrow for context at `src/setup/ia_views.go:573-574`; the setup wizard exposes explicit progress steps at `src/setup/ia_views.go:2474-2474,2842-2842`; the CSS styles these labels at `src/setup/ui.go:164-170`.

Judgment: repeated uppercase labels can become an AI/editorial tic, but these instances have contextual or ordinal meaning. They are not a finding by themselves.

Severity: not a problem when limited to context/progress; confidence: high.

Concrete correction: keep them tied to context or order; do not add them to every ordinary section as decoration.

### Not a problem

#### Font pairing in the admin console

Evidence: the admin theme loads Outfit and Space Grotesk at `src/setup/ui.go:8-10` and uses them in separate body/display/control roles at `src/setup/ui.go:37,169,276,342,351`.

Judgment: this does not match “Inter-everywhere” or a one-font template. The pairing is structurally intentional. The static docs layer contains an earlier Inter declaration at `docs.html:11`, but its later official/shared treatment sets the rendered family to Outfit at `docs.html:77-80`; this is a consistency concern, not an anti-AI finding.

Severity: not a problem. Confidence: high for source declarations; medium for final browser cascade without runtime inspection.

#### Decision-linked dashboard metrics

Evidence: the Dashboard metrics are named Connected Productions, Needs attention, Notification failures, and System status at `src/setup/ia_views.go:320-320`; the attention queue explains why notifications are unavailable and provides the next action at `src/setup/ia_views.go:321-321`.

Judgment: numeric cards are not inherently AI slop when they answer an operator question and connect to a next step. The current source provides that relationship, so this audit does not flag the metrics merely for being cards.

Severity: not a problem. Confidence: high for content linkage; medium for whether the order is optimal in real use.

## Untested scope and confidence limits

- No authenticated browser session, screenshot, computed-style capture, or operator task observation was run in this audit.
- The route evidence is source-level: route registration is visible at `src/main.go:859-940`, and the documentation aliases are visible at `src/docs_routes.go:13-37`. Runtime redirects, authentication states, and data-dependent empty/error states were not exercised.
- The audit did not validate 320/375/414/768px rendering, reduced-motion behavior, focus timing, contrast, or whether any clickable label wraps.
- The report does not determine whether the current visual density causes measurable task delay; that requires a browser walkthrough using representative healthy, disconnected, and failing states.
- `docs.html` includes layered style blocks, so final cascade behavior should be confirmed in a browser before treating every initial declaration as visually active.

## Source references

- `src/main.go:819-940` — HTTP route registration for login, setup, admin, and diagnostics surfaces.
- `src/docs_routes.go:10-50` — active documentation routes and static asset serving.
- `src/setup/ui.go:8-70,164-170,197-228,285-358,378-385,476-517` — shared admin theme, backgrounds, controls, cards, metrics, and responsive rules.
- `src/setup/ia_views.go:245-331,488-499,549-576,707-716,837-837,1338-1411,2362-2842,2864-2890` — Current IA screen markup and route-specific builders.
- `src/setup/admin.go:2056-2336,2911-3150,4070-4085` — legacy/compatibility screen builders and shared admin shell.
- `docs.html:11-80` — documentation screen CSS layers and final shared treatment.

Summary — 4 major harmful · 1 acceptable/watch item · 4 not a problem

Verdict — reads as AI-generated in its shared operational surface language; the main remediation target is repetition of decorative glass/glow treatment, not removal of meaningful status or decision structures.
