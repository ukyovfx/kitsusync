# Agent 5 — Component grammar

Status: documentation-only design-system guidance. This report does not change UI code, routes, behavior, or `DESIGN.md`.

## Scope and evidence boundary

This report defines a future semantic component grammar for the KitsuSync admin UI. It does not claim that these components already exist as a shared implementation. Current facts are grounded in the current renderer and styles; future rules are explicitly recommendations. Protected connected-Production states, some edit states, and some dialog/keyboard states are runtime-limited as recorded in `docs/UI-UX-RUNTIME-EVIDENCE.md:140-158`. No secret values are reproduced.

Primary evidence:

- `src/setup/ui.go:83-102,128-161,170-284,285-401,408-480,515-690` — shell, navigation, surfaces, status rows, controls, responsive rules, accordion/dialog behavior, and reduced-motion policy.
- `src/setup/ia_views.go:549-603,1238-1321,2691-2857,2860-2932` — Production-local navigation, diagnostic/status surfaces, wizard states, dashboard, empty/disconnected Production state, and completion state.
- `src/setup/admin.go:1086-1123,1328-1662,2051-2328` — notification controls, validation/danger blocks, health tables, diagnostics, and advanced disclosure.
- `src/setup/current_routing.go:225-312` — routing editor, table rows, details menu, and deletion dialog.
- `src/setup/responsive_accessibility_test.go:9-17` — source-level navigation containment and target-size checks.
- `docs/CURRENT-IA-UI-SPEC.md:5-19,69-80,100-155` — route ownership, page hierarchy, language parity, and Production content contract.
- `docs/UI-UX-RUNTIME-EVIDENCE.md:116-140,146-164` — authenticated coverage, runtime-confirmed states, and deferred evidence.
- `docs/design-work/design-agent0-current-tokens.md`, `design-agent2-tokens.md`, `design-agent3-typography.md`, and `design-agent4-navigation.md` — preceding token, typography, and navigation decisions.

## Semantic-card rule

WHAT: A card is a semantic container with one identifiable purpose, one owning heading, and one coherent content/action boundary. Use `section` when the content is a titled region of the current page, `article` when the item can stand alone or be repeated, `form` when the boundary owns submission, `dl` for label/value facts, `ul`/`ol` for lists, and `table` for row/column relationships. A visual surface must not decide the HTML element by itself.

WHY: The current UI has nested `.section-card`, dashboard tiles, status cards, summary cards, inline panels, and danger blocks (`src/setup/ui.go:105-123,137,147,152-154`; `src/setup/ia_views.go:2864-2925`). Without a semantic boundary, nested containers make hierarchy ambiguous, duplicate headings, and encourage treating every grouping as a card.

WHEN: Introduce a surface only when the user needs a distinct task, state, decision, or scan boundary. Give every titled `section`/`article` an accessible name through a real heading or an explicit label. Keep related content in the same surface when separating it would add an extra visual and cognitive boundary.

WHEN NOT: Do not wrap every field, row, badge, or paragraph in a card. Do not use a card to create hierarchy that should be expressed by heading levels, whitespace, a divider, or a list. Do not nest an elevated card inside an equally elevated card unless the inner block is a separately actionable or independently understandable region. Nested surfaces should step down in fill, border, shadow, and spacing rather than repeat the same emphasis.

Recommended surface hierarchy:

```text
AppShell
└─ page header / local navigation
   └─ page section (section)
      ├─ task group (section or form)
      │  └─ rows, controls, lists, or table
      └─ exceptional state (alert, empty, danger, or dialog)
```

Base spacing follows the existing evidence until implementation validation: shell padding `20px 14px 48px`, top-bar and navigation separation `18px`, main page card padding `20px`, section card padding `14px`, common group gap `8–16px`, and action-to-section separation up to `24px` (`src/setup/ui.go:83-102,285-300,146-157`; `design-agent0-current-tokens.md`). These are source-observed starting points, not permission to add a new literal scale.

## Component grammar

Each component rule below states WHAT / WHY / WHEN / WHEN NOT. Variants are semantic, not merely color choices.

### AppShell

WHAT: Own the global page frame: skip link, brand/home control, global navigation, language utility, authenticated content region, and one main landmark. The current shell is a centered `max-width:1100px` container with a top bar and primary navigation (`src/setup/ui.go:83-99,197-228,746-791`). The future grammar may adopt Agent 4’s bounded desktop rail only after implementation and runtime validation (`design-agent4-navigation.md`).

WHY: A single frame establishes persistent orientation and prevents each route from inventing its own header, content width, and responsive behavior. Runtime evidence confirms one main landmark, one h1, and the current horizontal navigation/disclosure in reached states (`docs/UI-UX-RUNTIME-EVIDENCE.md:128,157`).

WHEN: Use on authenticated admin pages. Keep login and the linear setup wizard visually related but workflow-owned; the wizard should not inherit a competing admin sidebar (`design-agent4-navigation.md`).

WHEN NOT: Do not render duplicate global navigation owners, duplicate mobile menus, or more than one main landmark. Do not expose secrets, tokens, or internal identifiers in shell utilities.

### Global navigation and Production-local navigation

WHAT: Global navigation owns Dashboard/home plus the five peer destinations in the current order: Productions, User Linking, Connections, System Status, Audit Log (`src/setup/ia_views.go:23-34`; `docs/CURRENT-IA-UI-SPEC.md:5-19`). Production-local navigation owns the selected Production’s sections; future grammar groups routine management separately from Operations and Danger Zone (`design-agent4-navigation.md`).

WHY: Global and object-local scope are different mental models. The current selected Production renders a `role="tablist"` with eight local items (`src/setup/ia_views.go:555-575`), while the IA evidence identifies the equal-peer problem (`design-agent4-navigation.md`).

WHEN: Mark the exact current page with an accessible current marker. Use the current mobile `Menu` / `メニュー` disclosure at narrow widths; current CSS switches at `760px` and keeps the section rail scrollable (`src/setup/ui.go:158,217-228,487-489`). Preserve targets of at least `44px` where the existing source contract requires it (`src/setup/ui.go:224-234`; `src/setup/responsive_accessibility_test.go:9-17`).

WHEN NOT: Do not add compatibility routes, setup, diagnostics aliases, or global User Linking as Production-local items (`src/main.go:899-943`; `docs/CURRENT-IA-UI-SPEC.md:120-155`). Do not show both a persistent desktop rail and a second mobile rail.

### Page headers and section headers

WHAT: A page header names the page and supplies the immediate context or primary action. A section header names one semantic region and may carry one state indicator or local action. The current pattern is `.page-heading` with a heading, hint, status pill, and sometimes action controls (`src/setup/current_routing.go:278-279`; `src/setup/admin.go:1123`; `src/setup/ia_views.go:2691,2788`).

WHY: Header content is the first scan target and prevents actions from appearing detached from their owner. The current IA contract separates page purpose and Production context (`docs/CURRENT-IA-UI-SPEC.md:69-80,120-155`).

WHEN: Use one page-level `h1`; descend headings by structure rather than visual size. Put the primary action in the page header only when it applies to the whole page. Use section-level actions for one section’s state or form.

WHEN NOT: Do not repeat the page title as a card title. Do not place unrelated status, filters, and destructive actions in one undifferentiated header row. Do not use an eyebrow, badge, or color as a substitute for a heading.

### Settings and service groups

WHAT: SettingsGroup is a titled semantic region for related configuration; ServiceGroup is a named service boundary showing service identity, status, relevant fields/actions, and help. Use `section`/`fieldset` as appropriate, and `legend` for a control group. Current examples include `.settings-block`, separate Kitsu/Discord connection sections, and notification-language forms (`src/setup/ui.go:148`; `src/setup/admin.go:727-924,1122-1123`; `docs/UI-UX-RUNTIME-EVIDENCE.md:126,154`).

WHY: Grouping by responsibility makes it clear which setting affects admin UI, a Production, or future Discord notifications. Runtime evidence confirms Kitsu and Discord are separate named sections (`docs/UI-UX-RUNTIME-EVIDENCE.md:126`).

WHEN: Use one group per service or coherent setting domain; include scope and effect in supporting text. Keep secret fields behind the existing change-token flow and never render secret contents (`docs/UI-UX-RUNTIME-EVIDENCE.md:154`).

WHEN NOT: Do not combine Kitsu and Discord into one ambiguous connection card. Do not make a visual group for a single unrelated field. Do not show a token, header, URL credential, or secret value in help, status, audit, or tooltip content.

### Definition rows and status rows

WHAT: DefinitionRow is a label/value pair for facts; render as `dl`/`dt`/`dd`. StatusRow adds state, explanation, and an optional next action while keeping the label/value relationship intact. The current `.detail-list` and `.status-row` structures establish these roles (`src/setup/ui.go:130-135,150-151`; `src/setup/ia_views.go:2788`).

WHY: Operational screens need fast comparison without turning every fact into a tile. A dedicated row supports variable text and a clear action owner.

WHEN: Use DefinitionRow for stable metadata such as Production, server, category, timestamps, or counts. Use StatusRow when a fact has a meaningful state and the user may need an explanation or recovery action. Allow long values to wrap (`src/setup/ui.go:134,151`).

WHEN NOT: Do not use a status row for arbitrary prose or a button-only toolbar. Do not use position, icon, or color as the only status signal; pair status text with a semantic label. Do not hide essential explanation on narrow screens.

### Status, health metric, and audit event

WHAT: StatusBadge is a compact state label with semantic variants: success, warning, danger, blocked, and neutral, matching current status classes (`src/setup/ui.go:138-143`). HealthMetric is a measurable service/telemetry item with metric name, current value, health interpretation, update time, and an accessible non-visual equivalent. AuditEvent is a chronological event row with viewer-local timestamp, actor/action context, target scope, outcome, and safe detail.

WHY: Status tells the user what state means; a health metric tells what was measured; an audit event tells what happened. The current source already distinguishes status pills, pipeline health items, webhook health, activity rows, and audit log screens (`src/setup/ui.go:137-147`; `src/setup/admin.go:2051-2328`; `docs/UI-UX-RUNTIME-EVIDENCE.md:151-153`).

WHEN: Use badges inside the owning header/row, not as standalone decoration. Use HealthMetric for actual observations only; failed observations must say request failed rather than inventing latency, and telemetry must not expose sensitive request data (`docs/UI-UX-RUNTIME-EVIDENCE.md:149-155`). Use AuditEvent for recorded actions or notification events and preserve the common viewer-local timestamp rule (`docs/UI-UX-RUNTIME-EVIDENCE.md:153`).

WHEN NOT: Do not encode health only with green/red. Do not label a send response as end-to-end delivery confirmation without an explicit distinction (`docs/audits/uiux-agent9-operations.md:17`). Do not place sample counts, technical thresholds, tokens, headers, URLs, bodies, or internal identifiers in normal health cards or tooltips.

### Alerts and notices

WHAT: Alert is an exceptional, actionable condition with severity, concise explanation, consequence, and next action. Use `role="alert"` for newly relevant urgent content and `role="status"`/polite status for non-urgent results. Current examples include the Discord resource notice and wizard blocked/error messages (`src/setup/project_discord_health.go:112-130`; `src/setup/ia_views.go:2695-2699,2754,2790`; `src/setup/ia_views.go:2857`).

WHY: Alerts interrupt the normal reading order only when the condition warrants it. Explicit semantics support screen-reader announcement and prevent a border/color-only warning.

WHEN: Use danger for destructive consequence or failed recovery, warning for attention/review, info for guidance, and success for completed operation. State the scope and whether the action is reversible. Keep the next action adjacent to the message when a safe action exists.

WHEN NOT: Do not use alerts for ordinary helper text, every status badge, or decoration. Do not announce static page content as a live alert. Do not imply a successful external side effect when only a local/API response is known.

### Empty and disconnected states

WHAT: EmptyState explains what is absent, why it matters, and one safe next action. DisconnectedState is a specific empty/blocked variant that names the missing connection and its recovery path. Current examples include `.empty-state`, the disconnected Production card, and the empty Audit Log/runtime states (`src/setup/ui.go:147`; `src/setup/ia_views.go:2924-2925`; `docs/UI-UX-RUNTIME-EVIDENCE.md:140`).

WHY: Empty data and failed prerequisites require different user decisions. A clear state prevents a blank card from being mistaken for a loading failure or healthy zero count.

WHEN: Use for no records, no configured Production, unavailable prerequisite, or safely withheld data. Include a primary action only when the user can resolve the condition from the current scope.

WHEN NOT: Do not use empty state for loading, permission denial, or an error. Do not show a generic “nothing here” message without scope. Do not invent connected-Production content when runtime evidence is unavailable.

### Form controls and buttons

WHAT: FieldControl is a labeled input/select/textarea with help, required/error state, and an associated value. Button variants are primary submit/action, secondary or ghost non-primary action, and danger/destructive action. Current controls inherit font, use `44px` minimum height, and have visible focus rules; current forms use labels and field help (`src/setup/ui.go:82,343-363`; `src/setup/ia_views.go:2734-2752`; `docs/UI-UX-RUNTIME-EVIDENCE.md:157`).

WHY: Consistent control grammar reduces guessing and protects high-risk operations. The current wizard and routing editors already use staged forms, disabled execution until confirmation, and labeled controls (`src/setup/ia_views.go:2759-2792`; `src/setup/current_routing.go:272-312`).

WHEN: Give every control a visible or programmatic label, preserve keyboard focus, keep actions in a predictable row, and switch stacked at narrow widths (`src/setup/ui.go:158-161,370-377`). Use disabled state only when the reason is explained and the user can identify how to proceed.

WHEN NOT: Do not use a link styled as a button when navigation is the action. Do not use icon-only controls without an accessible name. Do not put destructive operations beside routine actions without distinct labeling and confirmation. Do not expose or log secret values.

### Table, list, tabs, and accordion

WHAT: Use Table for relational columns and repeated records, List for ordered/unordered items or activity, Tabs only for peer views within one object, and Accordion/`details` for optional detail that can be independently expanded. Current evidence includes tables, activity lists, `role="tablist"`, and native `details`/`summary` (`src/setup/ia_views.go:555-575,2691-2756`; `src/setup/admin.go:969-998,2283-2308`; `src/setup/current_routing.go:275-278`).

WHY: The element communicates the user’s relationship to the content. Tables support comparison; lists support sequence; tabs switch peer panels; disclosure hides secondary detail without creating a new route.

WHEN: Keep table headers, row labels, and actions aligned. On narrow screens, either preserve a scrollable table with context or transform rows into labeled blocks; current user-linking evidence uses both horizontal wrapping and `data-label` mobile rows (`src/setup/ui.go:149,161`). Use native `details` when disclosure semantics are sufficient. For tabs, expose selected state and keep panel ownership explicit.

WHEN NOT: Do not use a table for a single definition list or card grid. Do not use tabs for unrelated global destinations or to hide a required next step. Do not put critical destructive controls only inside a collapsed disclosure. Do not create a custom accordion when native `details` can provide the required behavior.

### Modal and confirmation

WHAT: Modal is a temporary task surface; Confirmation is the destructive/high-consequence modal variant. Use native `dialog` where possible, with accessible name/description, explicit Cancel and Continue/Delete actions, safe default focus, Escape/Cancel close, and focus return. Current routing deletion uses `dialog`, exact-name confirmation, disabled submit, and a backdrop; the canonical confirmation behavior is documented in runtime evidence (`src/setup/current_routing.go:272-278`; `src/setup/ui.go:600-687`; `docs/UI-UX-RUNTIME-EVIDENCE.md:152-157`).

WHY: Confirmation must make target, scope, reversibility, and required proof visible before mutation. The evidence records a prior immediate-unlink bug and the post-fix canonical dialog behavior (`docs/UI-UX-RUNTIME-EVIDENCE.md:135,152-153`).

WHEN: Use for destructive or context-breaking actions that cannot be safely undone inline. State what KitsuSync changes and what it does not change; exact-name/type confirmation is appropriate for stronger deletion. Trap focus within an open modal and return it to the trigger.

WHEN NOT: Do not use a modal for routine explanation, navigation, or a form that can fit in the page. Do not allow submission before required confirmation. Do not claim connected-Production dialog coverage while that state remains deferred (`docs/UI-UX-RUNTIME-EVIDENCE.md:155`).

### Wizard

WHAT: Wizard is a linear, staged workflow with progress, prerequisites, editable plan, review, explicit execution confirmation, and completion/recovery state. Current source renders seven named steps, gated progress, plan tables, review summaries, a required confirmation checkbox, disabled execution, and a polite completion status (`src/setup/ia_views.go:2390-2416,2691-2857`).

WHY: Setup has external side effects and must expose readiness, plan, scope, and recovery before execution. Operational evidence identifies the need for durable running/partial-failure state and retry eligibility (`docs/audits/uiux-agent9-operations.md:13-15`).

WHEN: Use when the task has ordered prerequisites or a review-before-write boundary. Keep Back/Next labels and current step semantics stable across JP/EN. Treat Execute as a distinct high-consequence boundary, not as an ordinary navigation button.

WHEN NOT: Do not use a wizard for independent settings or a short one-step edit. Do not auto-submit external work without a durable operation state. Do not make the admin sidebar a second wizard navigation system.

### Loading and operation state

WHAT: LoadingState communicates that work is in progress; OperationState communicates stage, elapsed time, refresh/retry safety, and completion/failure outcome for a consequential operation. Current evidence has source-side setup execution and rollback paths but no fully exercised durable runtime operation view (`src/setup/first_time_connection.go:263-282,339-418`; `docs/audits/uiux-agent9-operations.md:13-15`; `docs/UI-UX-RUNTIME-EVIDENCE.md:91`).

WHY: A spinner alone does not tell an operator whether a slow external action is still safe to wait for or whether retry could duplicate work.

WHEN: Use loading only while the UI is waiting for a known response; preserve the page’s heading and scope. Use OperationState for setup, repair, deletion, or other multi-stage external work; expose a stable result/recovery destination and announce state changes politely.

WHEN NOT: Do not use indefinite animation without a status message. Do not replace content with a spinner when stale/read-only data can remain useful. Respect reduced motion; the source contains a shared `prefers-reduced-motion` override for ambient animation, entrances, transitions, and hover transforms (`src/setup/ui.go:515-529`; `docs/UI-UX-RUNTIME-EVIDENCE.md:156`).

## Responsive and accessibility invariants

WHAT: Components reflow by semantic priority: navigation becomes one disclosure, row actions stack, forms become one column, tables preserve row/field context, and content wraps rather than clips. Current breakpoints are `960px`, `760px`, `640px`, and `480px` with component-specific behavior (`src/setup/ui.go:124-127,158-161,370-377,482-489`).

WHY: Runtime evidence confirms no horizontal overflow in the sampled authenticated matrix and confirms the narrow Menu disclosure, but protected connected-Production density remains incomplete (`docs/UI-UX-RUNTIME-EVIDENCE.md:118,128,133,140`).

WHEN: Preserve one main landmark and one h1, visible focus, keyboard order, accessible names/descriptions, and minimum touch targets in every variant. Test JP and EN because labels, wrapping, and status text length differ (`docs/CURRENT-IA-UI-SPEC.md:100-118`; `docs/UI-UX-RUNTIME-EVIDENCE.md:134,157`).

WHEN NOT: Do not infer protected-screen acceptance from Login or empty/disconnected screenshots. Do not hide labels merely to fit a narrow viewport. Do not remove focus or status explanation to preserve a visual grid.

## Component acceptance checklist

- Every surface has one semantic owner and passes the semantic-card rule.
- Each page has one AppShell, one main landmark, one page heading, and one global navigation owner.
- Global and Production-local navigation remain separate; mobile has one disclosure.
- Status, health, alert, empty, loading, and success states name their meaning in text, not color alone.
- Destructive actions use the canonical confirmation behavior and identify exact target/scope.
- Forms have associated labels, help/error text, keyboard access, focus visibility, and `44px`-class targets where required.
- Tables, lists, tabs, and disclosures match their information relationship and retain context when responsive.
- Wizard execution and external operations expose review, confirmation, progress, retry safety, and recovery.
- JP/EN preserve hierarchy and action semantics; no secret or internal sensitive value appears in any component output.
- Acceptance remains open for the connected-Production detail and reduced-motion/keyboard evidence boundaries recorded in `docs/UI-UX-RUNTIME-EVIDENCE.md:155-158`.

## Source index

- Shell, layout, surfaces, controls, responsiveness, dialog, and motion: `src/setup/ui.go:83-102,128-161,170-284,285-401,408-480,515-690,746-791`.
- IA and Production-local structure: `src/setup/ia_views.go:23-34,549-603,1238-1321,2860-2932`.
- Wizard grammar and completion: `src/setup/ia_views.go:2390-2416,2691-2857`.
- Notification, validation, danger, health, and diagnostics: `src/setup/admin.go:1086-1123,1328-1662,2051-2328`.
- Routing table/menu/dialog: `src/setup/current_routing.go:225-312`.
- Interaction and confirmation findings: `docs/audits/uiux-agent4-interaction.md:24-31,65-70`.
- Operations and durable state findings: `docs/audits/uiux-agent9-operations.md:13-19,23-27`.
- Runtime coverage and explicit limits: `docs/UI-UX-RUNTIME-EVIDENCE.md:116-140,146-164`.
