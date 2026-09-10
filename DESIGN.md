# KitsuSync Design System

Status: canonical design direction for the next UI/UX refresh. Documentation only; it does not change the current renderer, routes, product behavior, or notification semantics.

Evidence baseline: `docs/UI-UX-DISTRIBUTED-AUDIT.md`, `docs/UI-UX-RUNTIME-EVIDENCE.md`, `docs/audits/`, `docs/design-work/`, and the current admin theme in `src/setup/ui.go`. Runtime facts and future rules are intentionally separated.

## 1. Purpose

KitsuSync is an operational bridge between Kitsu and Discord. The interface must help an operator understand connection state, choose the next safe action, configure a Production, and recover from a problem without visual noise.

This document defines the future system's vocabulary, hierarchy, components, accessibility contract, and screen rules. It is not permission to implement a redesign.

## 2. Design intent

The working identity is Editorial Systems Interface: quiet, precise, technical, editorial, spatial, restrained, human, and distinctive without being flashy.

The identity comes from typography, composition, spacing, state language, and Kitsu↔Discord operational context. It must not depend on decorative excess, imitation of another product, or generic “premium SaaS” styling.

### Product character

- Meaning before container: expose the operational fact before surrounding it with structure.
- State before explanation: show whether something is connected, blocked, pending, or failed before the supporting copy.
- Next safe action before dashboard theatre: the first focal point answers “what should I do now?”
- Quiet density: expert users should scan rows, statuses, and diagnostics quickly; dense does not mean cramped.
- Spatial editorial rhythm: use deliberate alignment, section breaks, and type scale rather than a field of equal cards.

## 3. Anti-AI principles

1. Every visual element pays rent. It must disclose state, ownership, risk, action, or orientation. Remove decoration that does none of these.
2. One state, one signal. Do not repeat the same status in a badge, glow, icon, border, and paragraph unless each adds different information.
3. Familiar interaction, distinctive expression. Keep buttons, tables, disclosure, and confirmation behavior recognizable; express KitsuSync through hierarchy, type, spacing, and language.
4. Consequences before mechanics. A destructive action says what changes and what does not change; it does not merely say “click Continue.”
5. Semantic asymmetry is allowed. High-risk actions may receive stronger separation than ordinary settings; uniform card treatment is not neutrality.
6. Motion must communicate. Use motion for focus, state change, or completion; never use continuous movement to make an idle screen feel alive.
7. No card soup. A card exists only for an ownership boundary, independent task, independent state, or risk boundary.
8. No metric theatre. Do not add an Overview or metric tile only to make a dashboard appear complete.
9. No dark/orange cliché. Orange is action, selection, or KitsuSync identity; it is not a universal highlight and never replaces semantic status.
10. Preserve human scale. JP and EN must remain readable, names must wrap safely, and dense expert views must not turn into tiny labels.

## 4. Design tokens

Use three layers in future implementation: primitive values, semantic aliases, then component contracts. The values below are extracted from the current admin source; aliases are design candidates, not CSS changes.

### Primitive values

| Token candidate | Current value | Source / purpose |
|---|---|---|
| `--color-neutral-950` | `#070707` | Current `--bg`; canvas |
| `--color-neutral-925` | `#0d0d0f` | Current `--bg2`; secondary dark value, usage unresolved |
| `--color-neutral-050` | `#f7f7f4` | Current `--text`; primary content |
| `--color-neutral-300` | `#b8b5ae` | Current `--muted`; secondary content |
| `--color-neutral-500` | `#8f8a83` | Current `--muted-2`; metadata/helper |
| `--color-orange-600` | `#e85a1a` | Current `--accent`; primary action/selection |
| `--color-orange-400` | `#ff8d48` | Current `--accent-2`; readable accent emphasis |
| `--color-danger-500` | `#ff6a50` | Current `--danger`; destructive/error |
| `--color-success-400` | `#8ecf8b` | Current `--success`; healthy/completed |
| `--color-focus-400` | `#6dc3ff` | Current focus outline |
| `--surface-panel-76` | `rgba(20,21,24,.76)` | Current `--panel`; owned elevated surface |
| `--surface-panel-92` | `rgba(14,15,18,.92)` | Current `--panel-strong`; usage unresolved |
| `--surface-white-03/04/05/06` | exact current rgba values | Subtle internal regions |
| `--border-white-08` | `rgba(255,255,255,.08)` | Quiet component separation |
| `--border-white-11` | `rgba(255,255,255,.11)` | Current `--line`; standard rule |
| `--border-white-20` | `rgba(255,255,255,.2)` | Current `--line-strong`; emphasis |
| `--space-1` through `--space-6` | `4/8/12/16/24/32px` | Existing scale in `ui.go` |
| `--radius-sm/md/lg/xl/pill` | `10/14/17/24/999px` | Existing scale and pill controls |
| `--shadow-panel` | `0 24px 80px rgba(0,0,0,.46)` | Current shared elevation |

### Semantic aliases

```text
--color-canvas: var(--color-neutral-950)
--color-content: var(--color-neutral-050)
--color-content-muted: var(--color-neutral-300)
--color-content-subtle: var(--color-neutral-500)
--color-action: var(--color-orange-600)
--color-action-emphasis: var(--color-orange-400)
--color-status-success: var(--color-success-400)
--color-status-warning: #ffd978 (observed warning foreground; contrast-check each component)
--color-status-danger: var(--color-danger-500)
--color-status-info: #6dc3ff (observed focus/current-step blue; not a competing primary action)
--surface-page: var(--surface-panel-76)
--surface-control: var(--surface-white-03)
--border-default: var(--border-white-11)
--border-emphasis: var(--border-white-20)
--space-section: var(--space-5)
--space-action: var(--space-3)
--space-peer: var(--space-5)
--content-max: 1100px
--content-reading-max: 680px
--control-height-standard: 44px
--control-height-dense: 38px
--touch-target-min: 44px
--space-major: 48px
--status-label-healthy: "Healthy" / "正常"
--status-label-warning: "Warning" / "警告"
--status-label-failed: "Failed" / "失敗"
--status-label-blocked: "Blocked" / "ブロック"
--status-label-pending: "Pending" / "保留中"
--status-label-disconnected: "Disconnected" / "未接続"
--status-label-not-checked: "Not checked" / "未確認"
```

### Component contracts

```text
--page-padding-standard: 20px
--page-padding-dense: 16px
--section-gap-standard: var(--space-section)
--section-gap-dense: 16px
--action-row-gap: var(--space-action)
--card-radius: var(--radius-lg)
--card-border: var(--border-default)
--card-shadow: var(--shadow-panel)
--control-radius: var(--radius-md)
--status-radius: var(--radius-pill)
```

Status implementation must map each vocabulary state to a label, foreground, surface, non-color indicator, and live-region policy. `warning` uses the observed `#ffd978` foreground family; `info` uses the observed `#6dc3ff` family only for information/current-step/focus roles. `blocked`, `pending`, `disconnected`, and `not-checked` are distinct labels and must not collapse into a generic warning.

Do not invent a new orange, dark gray, or arbitrary alpha. Consolidate repeated current literals only after selector ownership and contrast are verified. Keep documentation-page tokens separate until its conflicting style blocks are deliberately resolved.

## 5. Color, surfaces, and elevation

The dark/orange identity is preserved. Orange means action, active selection, or brand emphasis. Green, warning, danger, info, and neutral states must have text or icon meaning in addition to color. Each status contract includes label, foreground, surface, icon/text indicator, and live-region policy; surface alpha is selected from the observed family and contrast-tested per component.

Use one primary page surface, mostly flat internal data regions, and explicit separators. A surface is justified when it expresses ownership, an independent task, an independent state, an overlay, or a risk boundary. Do not wrap every row, metric, explanation, or table in a nested elevated card.

Use structural shadow for major owned surfaces and overlays. Keep orange glow exceptional and action-bound. Never combine strong border, glow, gradient, and saturated fill for an ordinary information row.

The current gradients and dot field are evidence of identity, not a mandate to add more. Operational screens should be calmer than Login/loading.

## 6. Layout and spacing

The standard rhythm is `4 / 8 / 12 / 16 / 24 / 32px`, with `48px` reserved for major breathing room. Use 24px between major sections, 12px between action controls, and 8px between a label and its status/value.

Use `1100px` as the current content-width reference and `680px` as a reading/form reference. Preserve `--touch-target-min:44px`. A future dense mode may use `--control-height-dense:38px` only for read-only, non-primary connection-summary rows after keyboard, adjacent-spacing, zoom, and text-spacing checks pass. It must not be used for primary actions, ordinary forms, or destructive controls.

Responsive behavior is content-driven:

- Above the desktop boundary, provide stable orientation and generous reading width.
- Around 960px, reduce grids and allow action groups to wrap.
- At 760px and below, use disclosure/drawer navigation, stack rows and forms, and reflow tables without horizontal page overflow.
- At 640px/480px, reduce density only where readability remains intact; never sacrifice focus visibility, labels, or target size.

## 7. Typography

Operational UI remains sans-first. Runtime evidence observed `Outfit, "Noto Sans JP", sans-serif`; source also contains Space Grotesk roles. Keep the visual role now and move font delivery toward self-hosted assets in a later implementation pass so offline/error behavior is deterministic. Do not distribute font files in this documentation run.

| Role | Contract |
|---|---|
| Brand / title | Space Grotesk or the existing display role; distinctive but short |
| Page title | Strong sans, compact tracking, one clear focal point |
| Section / service heading | Sans, visibly below page title, above body |
| Body | Outfit with JP fallback; readable line height, no display tracking |
| Helper / metadata | Muted, smaller, never used for required instructions |
| Label / table header | Compact and scannable; uppercase only for short English labels |
| Status | Text-first, concise, never color-only |
| Code / ID | Monospace only for identifiers and copyable technical values |

English uppercase and letter spacing must not leak into Japanese. Use normal Japanese casing and modest tracking. Mixed terms retain canonical product names (`Kitsu`, `Discord`, `Production`) rather than ad-hoc translations. Long Production and user names wrap with `overflow-wrap:anywhere`; truncate only when a full-value affordance exists.

If external fonts fail, preserve layout and readable system fallbacks; never hide content or create FOUT/FOIT-dependent hierarchy. Actual glyph provenance remains unresolved in the runtime evidence.

Serif/mincho is not part of the canonical system and is not an implementation option for this phase. Operational UI is sans-first; any future title experiment requires a separate decision record.

## 8. Navigation and information architecture

Top-level IA remains shallow. The current desktop standard is the horizontal primary navigation observed in runtime. A bounded sidebar is a future desktop candidate only; if adopted later, it must replace rather than coexist with the horizontal primary navigation. Its candidate order is:

1. Productions
2. User Linking
3. Connections
4. System Status
5. Audit Log

The sidebar decision is `ADOPT WITH LIMITS`: it may improve durable orientation on desktop, but it is not part of this implementation phase and must not become a permanent narrow rail on mobile. A collapsed state must preserve labels through an accessible expansion mechanism; icon-only navigation is not the default.

Active state uses text, position, and a restrained accent rule/background. Language and version belong in a quiet utility area. Diagnostics and history must not visually outweigh everyday Production operations.

Production is durable context. `Productions`, `User Linking`, `Connections`, `System Status`, and `Audit Log` are top-level routes. `Notifications`, `Users`, `Troubleshooting`, and `Danger Zone` are Production-local candidates, not implementation requirements until connected-Production runtime evidence exists. Dashboard is an attention surface at `/bot/admin`, not a required navigation item. Setup is a contextual action, not a generic global destination. Do not add Overview merely to complete a dashboard pattern.

Mobile uses a disclosure/drawer, not a compressed permanent sidebar. Production-local navigation follows the same disclosure rule and preserves the current Production name.

## 9. Component grammar

All components must state purpose, ownership, state, and responsive behavior. The semantic-card rule applies everywhere: no surface exists for decoration alone.

| Component | Use | Do not use when |
|---|---|---|
| AppShell | Global landmarks, background, navigation, content frame | A page needs a second competing shell |
| Sidebar / Navigation | Durable route orientation on desktop | Mobile would become a narrow permanent rail |
| PageHeader | Page title, short purpose, one primary action | Repeating title inside every card |
| Section / SettingsGroup | One ownership or task boundary | A simple adjacent row needs a box |
| ServiceGroup | Independent Kitsu/Discord peer state | Merging services into one ambiguous status |
| DefinitionRow | Label/value or status explanation | Long narrative content |
| StatusPill | Short state label with text/icon | Color-only status or arbitrary emphasis |
| Alert / EmptyState | Actionable warning or absence explanation | Obvious helper text |
| Input / Select / Checkbox / Radio | Labeled, keyboard-operable form control | Placeholder-only labeling |
| Primary / Secondary / Quiet button | Action hierarchy | Several competing primary actions |
| DestructiveButton | Irreversible/high-risk action | Ordinary navigation or reversible edit |
| Table / List | Repeated operational records | A decorative metric grid |
| Tabs | Peer views within one Production context | Global navigation or an absent resource |
| Accordion | Progressive disclosure of secondary detail | Hiding the primary state or required action |
| Modal / Confirmation | Focused decision at a risk boundary | Routine informational copy |
| WizardStep | Ordered setup decision with recoverable progress | A short one-screen form |
| AuditEvent / HealthMetric | Dense trace or operational observation | Generic dashboard decoration |
| LoadingState | Meaningful delay with current operation | Fast loads where a flash adds noise |

Component contracts must include visible focus, disabled/read-only distinction, localized accessible names, and state text. Modals must identify the action and consequence, focus Cancel or the safest non-destructive choice by default, support Escape, and return focus to the trigger.

## 10. Forms and actions

Forms group fields by service or task ownership. Labels are persistent and programmatically associated. Helper text explains consequence, format, or recovery; it does not restate the label.

Button verbs are explicit: `Save`, `Recheck connection`, `Change token`, `Continue`, `Cancel`, `Back`, `Remove`, `Unlink`. A destructive verb is visually distinct and must never submit immediately when a confirmation boundary is required.

Secret fields are never echoed in rendered content, screenshots, logs, or evidence. “Change token” is a deliberate reveal/rotation path, not a reason to expose stored values.

## 11. Status and feedback

Every status has a canonical text label and, where useful, an icon or supporting explanation. Healthy, warning, failed, blocked, pending, disconnected, and not-checked are distinct states. Do not use “Connected” to imply semantic health if only process reachability was checked.

Success feedback names what changed. Error feedback names the failed operation, likely cause when known, and the next safe recovery action. Live regions are reserved for meaningful asynchronous changes and must not repeat polling noise.

## 12. Tables, lists, and diagnostics

Tables are for scanning repeated records. Keep headers stable, align values by type, allow long names to wrap, and provide a mobile row/list representation when columns no longer fit. Horizontal scrolling may be local to a genuinely wide data table, never the entire page.

Audit Log is dense and quiet: timestamp, actor, action, target, result, and expandable detail. System Status prioritizes service state, semantic health, latency/observation context, and recovery actions over decoration. Charts require a text alternative and a meaningful label.

## 13. Motion

Motion taxonomy:

- Functional: focus, pressed state, disclosure, modal entry/exit, save feedback, and meaningful status transition.
- Brand: restrained dot or loading identity used only where it supports orientation.
- Decorative: ambient gradients or idle motion; minimize on operational screens and never rely on it.
- Forbidden: cinematic scroll effects, parallax, constant movement beside dense data, bouncing controls, or motion as status encoding.

Use property-specific transitions around the existing short interaction scale (`~180ms`) and the existing `ease` family; `transition: all` is prohibited. Entrance translation must remain small (current evidence: 10px). No staggered spectacle. Loading should not show a splash for fast work; for meaningful delay, say what is being checked or prepared.

| Motion | Trigger | Default | Reduced motion | Completion/cancel rule |
|---|---|---|---|---|
| Focus/hover | Pointer or keyboard enters control | Property-specific, ~180ms | Immediate state | Never hide focus |
| Press | Activation begins | Immediate scale/color state | Immediate state | Returns on release/cancel |
| Disclosure | User opens/closes accordion/nav | ~180ms height/opacity | Instant open/close | Content remains in DOM order |
| Modal | Confirmation opens/closes | ~180ms opacity/translate | Instant visibility change | Escape/Cancel closes; focus returns |
| Save feedback | Successful operation | Immediate status + optional ~180ms emphasis | Immediate status | Announcement carries result |
| Status transition | Health/result changes | Opacity/color transition only | Instant value change | Text remains authoritative |
| Loading | Meaningful async wait | Restrained indicator + operation text | Static indicator + text | Replace with result/error/recovery |

`prefers-reduced-motion: reduce` is a formal contract: disable particle drift, card entrance animation, nonessential transitions, and hover transforms; preserve the state change and focus visibility. Functional data refresh may continue, but visual interpolation must not be required to understand it.

## 14. Dot / spatial motif

The dot motif is `ADOPT WITH LIMITS`. It is a subtle brand texture, never data encoding, never a health signal, and never a substitute for hierarchy. Login/loading may use it more visibly. Operational screens should be nearly static and low contrast. Do not add a map or graph because dots are present.

## 15. Loading

Use a restrained KitsuSync loader only for a meaningful wait. Pair it with a plain-language operation label such as checking Kitsu or preparing the Discord plan. If the wait exceeds the expected operation time, expose recovery or retry guidance. Fast page loads should render directly.

## 16. Accessibility contract

Accessibility is a design acceptance condition, not polish.

- One `main` landmark per page, one logical h1, and ordered headings.
- Every control has a visible or programmatic localized name; labels are associated with inputs.
- Keyboard order follows visual task order; skip link and navigation are reachable.
- Focus is visible with a high-contrast outline and is never removed for styling.
- Touch targets remain at least 44px unless a measured, documented exception is approved.
- Status is not color-only; text/icon/shape must carry meaning.
- Async changes use appropriate status/live regions without repeated announcements.
- Dialogs have an accessible name and description, trap focus while open, support Escape where safe, and restore focus.
- Destructive actions require consequence-first confirmation, explicit target, cancel path, and no immediate submit.
- Reflow must work at narrow widths, zoom, and increased text spacing without hiding essential content.
- Reduced motion disables nonessential animation while preserving function.
- SVG/charts provide a text alternative, data label, or equivalent table.

## 17. Responsive behavior

Desktop may use a sidebar candidate and peer service groups. Tablet wraps actions and reduces grids. Mobile uses disclosure navigation, stacked forms, readable rows, local table overflow only when unavoidable, and full-width primary actions. Never solve narrowness by shrinking text, hiding labels, or preserving a permanent rail.

The current runtime evidence confirmed no horizontal overflow for reached Login and Connections edit states at the requested matrix, but connected Production detail remains unavailable. Future QA must test real connected data, JP/EN, 375/768/1024/1440, zoom, text spacing, and focus.

## 18. Content and JP/EN rules

Write state first, consequence second, next action third. Explain what changes and what remains unchanged. Remove helper copy that only repeats an obvious mechanic; retain safety, format, recovery, and operational context.

Japanese and English must have equivalent information, not necessarily identical word count. Japanese uses natural punctuation and avoids inherited English uppercase/tracking. English labels use sentence case except short canonical status or product conventions. Keep terminology stable: `Kitsu`, `Discord`, `Production`, `User Linking`, `Connections`, `System Status`, `Audit Log`.

Confirmation pattern:

- JP: `Kitsuユーザー「{name}」とDiscordユーザー「{identity}」の紐づけを解除します。`
- EN: `Unlink the Kitsu user "{name}" from Discord user "{identity}".`

Use `Cancel`/`キャンセル` for the safe exit and a consequence-specific submit verb (`Unlink`/`解除`, `Delete`/`削除`). Success messages state the completed operation; errors state the next recovery step.

### Canonical terminology table

| Meaning | English | Japanese rule |
|---|---|---|
| Product | KitsuSync | Keep product name unchanged |
| Service | Kitsu / Discord | Keep product names unchanged |
| Durable context | Production | Use `Production`; do not alternate with Project in UI copy |
| Identity mapping | User Linking | Explain the mapping in JP; do not shorten to an unexplained technical term |
| Connection state | Connected / Disconnected / Not checked | 接続済み / 未接続 / 未確認; distinguish reachability from health |
| Safe exit | Cancel | キャンセル; never use a vague close label for a risky decision |
| Destructive unlink | Unlink | 解除; include the target and consequence |

JP/EN component rules: labels may wrap; do not force uppercase or wide tracking on Japanese; mixed Latin/Japanese keeps one normal text space only where the established copy convention requires it; names and identifiers use `overflow-wrap:anywhere`; buttons must remain readable at the 44px standard height. Equivalent information is mandatory even when sentence length differs.

## 19. Screen-specific rules

| Surface | Primary goal / first priority | Primary action | Avoid | Density / responsive rule |
|---|---|---|---|---|
| Login | Understand identity and sign in | Login | Card-on-card excess, meaningless dead space, distracting motion | Calm composition; dot may be stronger; preserve JP/EN readability |
| Dashboard | See attention and next safe action | Contextual next action | Generic metric dashboard, Overview theatre | Sparse focal hierarchy; no forced tiles |
| Productions | Choose or inspect a Production | Open / connect relevant Production | Equal-weight diagnostics and everyday work | List-first; preserve long names |
| Production detail | Operate within durable Production context | Contextual tab/action | Tabs for unavailable resources, global nav duplication | Local nav; mobile disclosure |
| Notifications | Review and manage delivery state | Relevant notification action | Decorative cards around every row | Dense, readable records |
| Users | Understand participants and roles | Add/edit participant when authorized | Exposing secrets or ambiguous ownership | Table/list with safe wrapping |
| Connections | Compare Kitsu and Discord independently | Recheck or change token | Merged service state, secret echo | Peer service groups; stack on mobile |
| User Linking | Resolve Kitsu↔Discord identity mapping | Save / unlink with confirmation | Immediate unlink, opaque target, color-only state | Row/list reflow; dialog accessible |
| System Status | Diagnose service health and recover | Recheck / recovery action | Decorative charts, health claims from HTTP 200 alone | Operational data first; text alternatives |
| Audit Log | Trace who did what and outcome | Filter/expand detail | Loud cards, unexplained technical noise | Dense and quiet; local overflow only |
| Setup Wizard | Make a safe ordered configuration decision | Continue / save plan | Step 4 as an undifferentiated wall of choices | Progressive disclosure; preserve recovery |
| Diagnostics / Troubleshooting | Explain failure and next safe step | Recheck / repair / inspect | Raw logs without interpretation | Evidence and action adjacent |
| Danger Zone | Make irreversible risk explicit | Destructive action after confirmation | Same visual weight as routine settings | Isolated risk boundary, consequence-first modal |

## 20. Deferred concepts

Connected Production runtime evidence is deferred because the available target has no connected Production detail. The Production-local candidates above are provisional only; do not implement or accept unseen tabs from assumption alone. Their acceptance criteria begin only after connected-Production runtime evidence is recorded.

Connection Map is `DEFER`. If revisited, constraints are: one Production context, Task Type → Discord Channel relationship, read-only first, list remains canonical, and no graph editing requirement. It is not part of the core system.

Serif/mincho is rejected for the canonical system. Keep any future identity experiment outside this document and outside implementation scope. Sidebar remains `ADOPT WITH LIMITS`; Dot and Motion remain `ADOPT WITH LIMITS`. No new sidebar, map, serif, or notification semantics are implemented by this document.

## 21. Do / Don't

Do use one clear focal point, semantic surfaces, stable terms, consequence-first safety copy, text-backed statuses, and restrained state motion.

Don't add an Overview for appearance, turn every row into a card, use orange for every status, hide labels on mobile, echo secrets, rely on font loading, or make decorative movement compete with diagnostics.

## 22. Visual QA checklist

Before accepting an implementation, verify:

- First focal point answers the operator's next question.
- Card/surface count is justified by ownership, task, state, or risk.
- No state is duplicated without added meaning.
- Spacing and alignment follow the token rhythm.
- Typography hierarchy survives JP/EN and long names.
- JP/EN copy has equivalent consequence and recovery information.
- No page-level overflow at required viewport sizes.
- Mobile navigation is a disclosure/drawer, not a permanent compressed rail.
- Focus, labels, landmarks, dialogs, and focus return work by keyboard.
- Status remains understandable without color.
- Reduced-motion disables nonessential movement.
- Charts and SVGs have text alternatives.
- Console errors are classified by application versus browser extension.
- Screenshot comparison checks hierarchy, wrapping, and state changes.
- The result does not resemble a generic AI-generated dashboard template.
- KitsuSync's individuality comes from operational language, type, space, and disciplined warmth.

## 23. Evidence and implementation boundary

This document is ready for a later implementation plan, not implementation itself. The next implementation phase must first convert the candidate token layers into an audited CSS authority, then implement shell/navigation, typography delivery, semantic surfaces, status/forms/dialogs, screen-specific density, and finally runtime accessibility/visual QA. Each phase must preserve the current route and behavior boundary unless separately authorized.

Supporting specialist reports: `docs/design-work/design-agent0-current-tokens.md` through `design-agent9-content.md`.
