# Agent 6 — Motion / dot / loading

Status: WAVE 2 documentation-only design-system report. This report changes only this file; it does not change UI code, routes, behavior, or `DESIGN.md`.

## Scope and evidence boundary

Current facts are separated from future design rules. Source facts below are from the current worktree. Runtime facts are from the supplied post-fix evidence and are not generalized beyond the states that were actually reached. No secret values are included.

Primary sources:

- `src/setup/ui.go:45-74` — fixed dot field, `particleDrift`, and `riseIn`.
- `src/setup/ui.go:224-266` — navigation, action, and language-control transitions.
- `src/setup/ui.go:285-299` — page/card entry and tile transition.
- `src/setup/ui.go:349-369` — form, button, focus, disabled, and status styling.
- `src/setup/ui.go:392-401` — accordion structure and caret styling.
- `src/setup/ui.go:524-529` — current post-fix reduced-motion override.
- `src/setup/ui.go:562-700,784-787` — modal, submit lock, saving label, and live region behavior.
- `src/setup/ia_views.go:1354-1411` — System Status polling, chart replacement, and static live indicator.
- `src/setup/current_routing.go:275-312` and `src/setup/ia_views.go:2756` — drag/drop, keyboard row movement, and destructive dialog behavior.
- `docs/audits/uiux-agent7-motion.md:15-26,45-73` — pre-fix motion inventory and audit interpretation.
- `docs/audits/uiux-runtime-agentD-motion.md:13-93` — runtime motion evidence and untested boundaries.
- `docs/UI-UX-RUNTIME-EVIDENCE.md:46-58,116-142,144-160` — Login/authenticated coverage, post-fix reduced-motion boundary, and remaining evidence gates.
- `docs/CURRENT-IA-UI-SPEC.md:91-111,147-155` — System Status and telemetry contract.

## Current implementation facts

The shared admin theme currently contains a fixed viewport-wide dot field at `body::before`, with a radial dot pattern, `opacity:.22`, and `animation:particleDrift 18s linear infinite` (`src/setup/ui.go:45-56`). The animation shifts the first background layer from `0 0` to `34px 68px` (`src/setup/ui.go:67-70`). A warm radial glow is separate and static (`src/setup/ui.go:58-65`).

Cards use `riseIn .42s ease both`; `riseIn` starts at `opacity:0` and `translateY(10px)` and ends at the normal position (`src/setup/ui.go:71-74,285-292`). Navigation/action links use `transition:all .18s ease` and hover `translateY(-1px)` (`src/setup/ui.go:224-240`). The language thumb uses `left .18s ease` (`src/setup/ui.go:257-266`). Tiles use `.18s` transform/border transitions and hover `translateY(-2px)` (`src/setup/ui.go:291-294`). Form controls transition border, shadow, and background over `.18s`; buttons transition transform, opacity, and shadow over `.18s` (`src/setup/ui.go:349-358`). The accordion caret has a `.18s` rotation transition (`src/setup/ui.go:392-401`).

The current worktree now contains a post-fix `@media (prefers-reduced-motion: reduce)` rule that stops the dot animation and card entrance, removes transitions from the listed controls, and removes listed hover transforms (`src/setup/ui.go:524-529`). This is a source fact, not a complete runtime PASS: the post-fix runtime evidence says the normal authenticated Connections styles remain active and reduced-motion emulation for protected pages was deferred (`docs/UI-UX-RUNTIME-EVIDENCE.md:154-158`). Earlier Login runtime evidence recorded the pre-fix artifact retaining `particleDrift`, `riseIn`, and `transition: all` under emulated reduce (`docs/UI-UX-RUNTIME-EVIDENCE.md:56-57,88`; `docs/audits/uiux-runtime-agentD-motion.md:15-50`).

System Status performs an in-memory read-only refresh every five seconds with overlap protection, replaces status/details/chart markup, and changes the live label to `Auto-refresh` or `Refresh unavailable` (`src/setup/ia_views.go:1354-1357`). The current IA contract defines the chart as real timestamped observations, green success/red failure bars, and a bounded observation window; it does not define animated chart interpolation (`docs/CURRENT-IA-UI-SPEC.md:100-111,147-155`). The live dot is a static 7px green circle with a 3px ring and adjacent text (`src/setup/ui.go:491`, `src/setup/ia_views.go:1411`).

The shared page shell exposes a visually hidden polite live region (`src/setup/ui.go:784-787`). Non-delete form submission disables submit controls and replaces button text with localized `Saving...` / `保存中...`; duplicate submission is blocked (`src/setup/ui.go:658-673`, `:690-692,739`). The delete confirmation uses an open state, delayed focus, Escape/cancel close, and focus return to the trigger (`src/setup/ui.go:583-595,630-655,696-700`; post-fix runtime confirmation: `docs/UI-UX-RUNTIME-EVIDENCE.md:150-153`). No source evidence establishes a spinner, skeleton, shimmer, page-transition animation, toast animation, or animated status-pill transition in the inspected surfaces; absence of evidence is not proof that no other renderer can add one.

## Motion taxonomy

The following taxonomy is a future design-system contract, not a claim that every rule is already implemented.

| Class | WHAT | WHY | WHEN | WHEN NOT |
|---|---|---|---|---|
| Functional | Short movement or visual interpolation that explains an input, selection, focus, reorder, open/close, save, or refresh state. | It reduces uncertainty about what changed without competing with operational reading. | Directly after a user action or bounded state change. | Do not use it for continuous ambience, decoration, or a state that is already clear from stable text. |
| Brand | A quiet, static or near-static treatment that identifies KitsuSync, especially Login or an explicit identity moment. | It gives the product a recognizable identity without making operations theatrical. | Login, empty/complete identity moments, or an explicitly branded entry surface. | Do not carry brand motion into logs, tables, health monitoring, long forms, or repeated refreshes. |
| Decorative | Non-semantic atmosphere such as the dot motif or warm glow. | It can support tone, but it does not explain task state. | Only when contrast, reading load, and reduced-motion behavior have been verified for that surface. | Never let it compete with status, data, focus, or a destructive decision; never make it continuous by default on operational pages. |
| Forbidden | Pulse, flash, bounce, parallax, attention-seeking count changes, celebratory confetti, shimmer used as fake progress, or movement that is the only status cue. | These patterns can create attention and accessibility risk and can misrepresent system certainty. | None in the admin design system. | Especially forbidden for errors, health, notification delivery, destructive actions, and auto-refresh. |

## Motion budget and primitives

WHAT: use a small motion budget: `0ms` for no motion, `150–180ms` for direct control feedback, `200–240ms` for bounded disclosure/overlay movement, and at most `420ms` for a one-time page-surface entrance. Cap routine translation at `2px`; permit up to `10px` only for a single, non-repeating surface entrance if it improves orientation. WHY: the current source already uses `.18s` interaction transitions and `10px` / `2px` entry-hover translations (`src/setup/ui.go:71-74,224-240,291-294,349-358`), while the motion audit identifies continuous background movement and repeated card entry as the main attention risks (`docs/audits/uiux-agent7-motion.md:19-21`). WHEN: apply these as named component contracts for future implementation. WHEN NOT: do not invent longer durations, larger travel, staggered cascades, spring/bounce physics, or indefinite loops for ordinary admin work.

WHAT: default to `ease-out` for an element entering or opening, `ease-in` for leaving or closing, and `linear` only for a genuinely time-based indicator whose timing is meaningful. WHY: easing should clarify direction and completion rather than add personality. WHEN: use on future explicit motion tokens and evaluate in both JP and EN. WHEN NOT: do not use `linear` for hover/press/focus, and do not use a keyframe loop for a state that can be represented statically.

WHAT: animate only named properties such as `transform`, `opacity`, `background-color`, `border-color`, `box-shadow`, or an explicitly bounded disclosure property. WHY: the current `transition:all` is broad and makes the motion contract difficult to audit (`src/setup/ui.go:224-234`; runtime record: `docs/UI-UX-RUNTIME-EVIDENCE.md:56-57`). WHEN: replace broad transitions during a future implementation pass. WHEN NOT: do not animate layout geometry, text size, page width, scroll position, or secret-bearing content merely to make a change feel smoother.

## Interaction rules

### Hover, focus, and press

WHAT: hover is a low-amplitude pointer cue: a color/border/surface change and, where useful, no more than `2px` upward translation. WHY: it identifies an actionable target without moving the reading surface. WHEN: pointer-capable devices and non-disabled links, tiles, and buttons (`src/setup/ui.go:236-240,291-294,357-360`). WHEN NOT: do not make hover the only indication; do not apply hover movement to disabled controls, status-only elements, data rows, or touch-only flows.

WHAT: focus is immediate and persistent for the duration of focus, using the existing visible `3px` blue outline with `3px` offset; future motion must not delay or animate focus visibility. WHY: keyboard orientation is more important than polish, and the source already defines a clear focus ring (`src/setup/ui.go:351-353`). WHEN: keyboard focus, programmatic focus after modal open, and focus return after modal close. WHEN NOT: never remove, fade, or replace focus with hover styling; never rely on a moving border that can disappear before the user locates it.

WHAT: press feedback is a bounded state change, preferably opacity/background/border, with no more than `1px` scale/translation if a control needs physical confirmation. WHY: a submit or destructive action needs confirmation but must not look like it has already completed. WHEN: while the pointer/keyboard activation is being accepted, before the save result is known. WHEN NOT: do not use a bounce, pulse, or optimistic success motion for an operation that can fail or has not been persisted.

### Navigation

WHAT: navigation changes should be communicated by stable active-state styling; the current responsive navigation may use a small color/border transition and language-thumb movement, but not a page-wide slide. WHY: route changes are orientation changes, not a presentation sequence; the current nav uses `.18s` transitions and the mobile disclosure is evidence-supported (`src/setup/ui.go:224-266,487-489`; `docs/UI-UX-RUNTIME-EVIDENCE.md:116-128`). WHEN: selecting a global/local destination or switching JP/EN. WHEN NOT: do not animate the whole shell, duplicate navigation, or force a user to watch an exit animation before content becomes available.

### Accordion and disclosure

WHAT: an accordion communicates open/closed state through the existing caret rotation and stable summary state; future expansion may use a bounded `200–240ms` `ease-out` reveal only if it does not cause focus or content to jump unexpectedly. WHY: the caret already conveys direction (`src/setup/ui.go:392-401`), and operational details should remain scannable. WHEN: `details`/accordion content is explicitly opened or closed. WHEN NOT: do not auto-open, pulse, repeatedly bounce the caret, or animate a long diagnostic block in a way that moves the trigger away from the keyboard user.

### Modal and destructive confirmation

WHAT: modal opening/closing must prioritize modality, focus, and the confirmation text; a future overlay may fade over `150–200ms`, while the dialog itself may use at most `2px` translation over `200ms`, or remain static. WHY: the current modal contract already handles focus, Escape/cancel, and focus return, and the runtime post-fix confirms those semantics (`src/setup/ui.go:583-595,630-655`; `docs/UI-UX-RUNTIME-EVIDENCE.md:150-153`). WHEN: opening or closing a destructive confirmation. WHEN NOT: do not delay access to the dialog, animate the destructive action as success, obscure the target text, or close only after a decorative sequence. Reduced motion must make the dialog immediate.

### Feedback and status transitions

WHAT: feedback uses text, status role, and semantic color as the source of truth; motion is optional and secondary. WHY: current status pills, notices, and live labels are text-bearing (`src/setup/admin.go:2866-2871,2914-2934`; `src/setup/ia_views.go:1411-1423`), and the design intent says status cannot rely on color alone (`docs/design-work/design-agent1-intent.md:55`). WHEN: save success/failure, connection state, health state, routing state, and refresh availability. WHEN NOT: do not flash a status, pulse a green dot, animate an error into view without a persistent text alternative, or imply “healthy” solely through movement.

WHAT: a status transition should settle to the new stable value; if a transition is animated, use a single `150–180ms` color/border/opacity change and no translation. WHY: state comparison matters more than spectacle. WHEN: a known state changes from pending to success/failure or from unavailable to available. WHEN NOT: do not animate every five-second telemetry replacement as a card entrance, and do not interpolate fabricated intermediate values.

## Loading and operational identity

WHAT: represent loading as “the requested operation is in progress,” with a stable localized label and disabled duplicate-submit controls; use the existing `Saving...` / `保存中...` behavior as the current identity baseline. WHY: source evidence shows submit locking and localized button text, but no verified spinner/skeleton contract (`src/setup/ui.go:658-673,690-692,739`; absence of spinner/skeleton evidence noted above). WHEN: a form submission or confirmation action has been accepted and the result is pending. WHEN NOT: do not add shimmer, indefinite progress percentages, fake latency, or a spinner that replaces the action name; do not expose tokens or request bodies in loading feedback.

WHAT: for read-only System Status refresh, keep the last valid snapshot visible while the request is pending, keep the refresh label stable, and announce only a meaningful failure or completed update through the existing status/live text mechanism. WHY: the source refresh is in-memory, bounded, overlap-protected, and replaces card markup every five seconds (`src/setup/ia_views.go:1354-1357`); the IA contract requires real observations and safe diagnostics (`docs/CURRENT-IA-UI-SPEC.md:100-111,147-155`). WHEN: initial observation load, selector change, periodic refresh, and transient refresh failure. WHEN NOT: do not blank a usable chart for a routine refresh, animate bars as if measurements were continuous, or turn the static dot into a pulse/spinner.

WHAT: define loading identity by operation and scope: form button = localized action-in-progress label; read-only telemetry = stable `Auto-refresh` / `Refresh unavailable`; page navigation = browser/load state without a custom page theater. WHY: these identities match current source semantics (`src/setup/ui.go:564-565,658-673`; `src/setup/ia_views.go:1356-1411`). WHEN: future loading states need a consistent contract across JP and EN. WHEN NOT: do not use generic “Loading...” where the user needs to know which operation is pending, and do not show implementation identifiers or secrets.

## Page transitions

WHAT: treat a route change as immediate content replacement with optional one-time, low-amplitude surface entry; do not add a global page transition. WHY: current evidence confirms route rendering and responsive states but does not establish that page-wide movement improves task performance; the existing `riseIn` applies broadly to `.tile`, `.section-card`, and `.page-card` (`src/setup/ui.go:285-292`), and the audit warns about repeated entry motion (`docs/audits/uiux-agent7-motion.md:20,31-32`). WHEN: only after an authenticated JP/EN runtime comparison shows that a bounded entry improves orientation. WHEN NOT: do not stagger every card, replay entry motion on every five-second refresh, or hide operational content behind an exit/entry delay.

WHAT: if `riseIn` remains a future-approved pattern, cap it at one surface-level entrance, `420ms` maximum, `10px` maximum translation, no stagger, and no opacity-only concealment of essential information. WHY: these limits match the current source amplitude while preventing a cascade. WHEN: a new page or explicit empty/complete moment is first rendered. WHEN NOT: under reduced motion, on repeated refresh, in dense tables/logs, or when the content is needed immediately for a destructive or recovery decision.

## Dot motif and ambient identity

WHAT: treat the current dot field as decorative brand atmosphere, not a live indicator, progress signal, or health state. WHY: source shows a fixed radial pattern with a continuous `particleDrift`, while runtime evidence confirms the motif on Login and authenticated screenshots but classifies its operational use as limited (`src/setup/ui.go:45-70`; `docs/UI-UX-RUNTIME-EVIDENCE.md:131-132`; `docs/audits/uiux-agent7-motion.md:19,25`). WHEN: retain only as a static or explicitly approved Login/identity treatment after contrast and reduced-motion validation. WHEN NOT: do not use dots to encode success/failure, connection, message delivery, latency, or “currently live”; do not add dots to charts, tables, rows, or status badges.

WHAT: in the default operational mode, prefer the motif static or absent; if the decorative layer is retained, keep it behind content, pointer-inert, low contrast, and free of independent movement. WHY: long-lived admin surfaces prioritize reading and monitoring, and the audit identifies continuous particles as distraction risk (`docs/audits/uiux-agent7-motion.md:19,49-55`). WHEN: Login or a bounded brand surface passes visual/readability review. WHEN NOT: when the user has requested reduced motion, when content density is high, when a dialog is open, or when the dot field competes with focus/status contrast.

## Reduced-motion contract

WHAT: `@media (prefers-reduced-motion: reduce)` is a hard contract: decorative animation is removed; page/card entry is immediate; hover/press/accordion/language/nav transitions are removed or reduced to an immediate state change; translation is `0px`; modal opening/closing is immediate; telemetry refresh continues functionally but has no animated visual interpolation. WHY: the current post-fix source now disables the dot animation, card entrance, listed transitions, and listed hover transforms (`src/setup/ui.go:524-529`), while the pre-fix runtime evidence demonstrated the defect this contract addresses (`docs/UI-UX-RUNTIME-EVIDENCE.md:56-57,156`). WHEN: every renderer using the shared theme and every future motion component. WHEN NOT: do not stop required data refresh, form submission, or error announcement merely because visual motion is reduced; reduce visual movement, not the underlying operation.

WHAT: reduced-motion validation must inspect both source and rendered computed behavior for Login and protected screens at JP/EN representative widths, including navigation, focus, accordion, modal, submit/loading, drag/reorder, and System Status refresh. WHY: the current source fix is present, but protected-page reduce emulation remains explicitly deferred (`docs/UI-UX-RUNTIME-EVIDENCE.md:156-158`). WHEN: before motion design acceptance or DESIGN.md readiness. WHEN NOT: do not mark reduced-motion PASS from the presence of a CSS rule alone, from screenshots without interaction, or from the pre-fix Login artifact.

WHAT: reduced motion must preserve semantic visibility and input feedback: active state, focus ring, status text, disabled state, modal focus order, and live announcements remain available without movement. WHY: motion is supplemental and the current UI already relies on text/status roles and focus semantics (`src/setup/ui.go:351-354,365-369,784-787`; `src/setup/ia_views.go:1411-1423`). WHEN: every reduced-motion state and every localized variant. WHEN NOT: do not replace a removed animation with silence, color-only state, or a hidden content change.

## Acceptance matrix for a future implementation pass

| Area | Acceptance rule | Evidence required |
|---|---|---|
| Motion budget | No routine motion exceeds the named duration/translation limits; no broad `transition:all` remains in a new component. | Computed styles and source review against `src/setup/ui.go:224-240,291-294,349-358`. |
| Hover/focus/press | Hover is supplemental; focus is immediate and visible; press never implies unconfirmed success. | Keyboard/pointer interaction trace, including narrow JP/EN states; current focus baseline `src/setup/ui.go:351-353`. |
| Navigation/accordion/modal | No page theater; disclosure direction is clear; modal focus and Escape/cancel behavior remain correct. | Authenticated interaction evidence plus post-fix dialog evidence `docs/UI-UX-RUNTIME-EVIDENCE.md:150-157`. |
| Loading | Saving state identifies the operation, prevents duplicate submit, and does not expose secrets; telemetry retains usable last data during refresh. | Submit/refresh/error traces against `src/setup/ui.go:658-673` and `src/setup/ia_views.go:1354-1357`. |
| Dot identity | Dot is decorative only, static or reduced under the operational contract, and never the sole state cue. | Login and protected screenshots with contrast review; runtime boundary `docs/UI-UX-RUNTIME-EVIDENCE.md:131-132,156`. |
| Status transitions | Text/status role remains stable; no pulse/flash or fabricated interpolation. | Healthy/failure/recovery and refresh-unavailable states; IA contract `docs/CURRENT-IA-UI-SPEC.md:100-111,147-155`. |
| Reduced motion | Decorative and nonessential motion is absent/immediate; required refresh and announcements continue. | Emulated `reduce` computed styles and interaction behavior for Login plus protected screens; current gate is still deferred (`docs/UI-UX-RUNTIME-EVIDENCE.md:156`). |

## Confidence and deferred evidence

High confidence: current source declarations for `particleDrift`, `riseIn`, interaction transitions, static live dot, five-second refresh, submit lock, modal focus behavior, and the post-fix reduced-motion CSS rule.

Medium confidence: authenticated screenshots confirm the ambient dotted treatment and normal authenticated Connections computed styles, but the protected reduced-motion profile was not emulated in the connected Chrome pass (`docs/UI-UX-RUNTIME-EVIDENCE.md:116-118,131-132,156`).

Deferred: frame-by-frame perception, pointer/keyboard activation timing for every component, drag/reorder runtime behavior, connected Production detail, all loading/error variants, and reduced-motion runtime coverage for protected pages. Do not promote these future rules to shipped behavior until those evidence gaps are closed (`docs/UI-UX-RUNTIME-EVIDENCE.md:140,156-158`; `docs/audits/uiux-runtime-agentD-motion.md:60-93`).
