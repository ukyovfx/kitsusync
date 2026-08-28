# KitsuSync Phase 11 — Connections / System Status Audit

Status: audit only; no source changes made

Date: 2026-08-28
Runtime: authenticated Chrome, `http://127.0.0.1:8090`
Scope: Connections and System Status, compared with Productions and User Linking

## Executive finding

The shared shell already gives the four screens a common H1 contract and page inset. Connections is close to the Productions/User Linking grammar: two independent service surfaces, aligned status/value/action anatomy, and a natural mobile stack. Its remaining deviation is a transparent outer `page-card` wrapper and a slightly action-heavy horizontal footer.

System Status remains the larger deviation. It has the right content order, but its outer page card, section-card markup, peer-level rules, row dividers, helper sentences, and long empty-state section combine into a visually segmented and vertically long diagnostic page. The recommended next implementation is a flatter page-level section composition, not a metric or polling redesign.

## Baseline evidence

Fresh authenticated browser inspection covered JP and EN at 1440, 1024, 768, and 375 CSS pixels. The pages were inspected through live navigation and DOM geometry, not source values alone.

At 1440:

| Screen | H1 top / height | Page surface | First meaningful content | Horizontal overflow |
|---|---:|---:|---:|---|
| Productions | 113 / 32px | x184, w1072, inset 17px | y169 intro; y214 Production row | No |
| User Linking | 113 / 32px | x184, w1072, inset 17px | y169 selector section | No |
| Connections | 113 / 32px | x184, w1072, inset 17px | y169 peer cards | No |
| System Status | 113 / 32px | x176.5, w1072, inset 17px | y169 API section | No |

System Status x176.5 is the vertical-scrollbar-adjusted viewport coordinate; its content x193.5 gives the same 17px page inset as the baseline. At 1024 the content inset was 17px; at 375 it was 17px inside the page surface. H1 height remained approximately 32px in every tested language/width.

The baseline rule is therefore not “move the titles.” It is: preserve the shared shell geometry and simplify the content surfaces beneath it.

## Six-dimension review

| Dimension | Score | Finding |
|---|---:|---|
| Visual hierarchy | 14/20 | Connections is clear; System Status has too many equal section boundaries and explanatory lines. |
| Consistency | 13/20 | Connections matches the peer-card grammar; System Status still reads as a separate diagnostic template. |
| Accessibility | 16/20 | H1, labels, text statuses, Details disclosure, and narrow reflow are present; the dense diagnostic sequence still needs clearer grouping and less repeated copy. |
| Usability | 15/20 | Connections supports comparison and action; System Status supports diagnosis but takes longer to scan than the baseline list/section grammar. |
| Responsiveness | 15/20 | No horizontal overflow at 1440/1024/768/375; System Status becomes very tall at 375 and should reduce chrome rather than text size. |
| Performance / stability | 17/20 | No interaction regression observed; polling and live status remain understandable. |

Weighted review score: `15.0 / 20` (75%). This is a bounded consistency issue, not a route or behavior issue.

## Shared geometry comparison

### H1 and page padding

KEEP the shared H1 and shell geometry. All four screens measured the same H1 height and the same 17px inner page inset at desktop/tablet/mobile. Connections and System Status both place their first section at y169, which is 24px after the H1 bottom at y145. User Linking uses the same section start.

Deviation: System Status has a scrollbar-adjusted outer x coordinate and a much taller content surface; neither is an H1 alignment defect.

### H1 to content and section rhythm

The first content relationship is consistent at 24px. The deviation is below that relationship:

- Connections: two peers begin together at y169 and have matching 104px desktop heights.
- System Status: API peers begin at y234 inside a section beginning y169; processing begins around y485; recent issues begins around y977. The 24px major stack rhythm is present, but each section adds its own padding and boundary, so the page feels more segmented than Productions/User Linking.
- At 375, System Status is approximately 1,550px tall versus compact baseline screens. The height is content-driven, but repeated helper text and disclosure/detail spacing contribute avoidable growth.

## Surface inventory and classification

The classification uses the KitsuSync rule that a card needs independent state, independent action, independent object identity, or a meaningful risk/decision boundary. This also agrees with [Atlassian spacing guidance](https://atlassian.design/foundations/spacing), which uses proximity and repeated rhythm to express relationships, [Carbon structured-list guidance](https://carbondesignsystem.com/components/structured-list/usage/), which favors lists for simple repeated data and discourages nesting, and [PatternFly card guidance](https://www.patternfly.org/components/card/design-guidelines/), which reserves cards for distinct information groups rather than dense repetitive content.

### Connections

| Rendered surface / class | Classification | Audit finding |
|---|---|---|
| Outer `.page-card.glass` | `FLATTEN_TO_SECTION` | Its border is transparent in the current render, so it contributes layout ownership but no useful visual boundary. Keep the shell container semantics if needed; remove visual card chrome in a later implementation. |
| Kitsu `.connections-card` | `KEEP_CARD` | Independent service state, host identity, status, and service-specific diagnostic content. |
| Discord `.connections-card` | `KEEP_CARD` | Independent service state and service-specific configuration/diagnostics. |
| `.connections-summary-grid` grouping | `FLATTEN_TO_SECTION` | It is a peer layout, not an additional object surface. Use one shared section heading/boundary and two peer cards. |
| Summary action row | `FLATTEN_TO_ROW` | Actions belong to the Connections task and should use one consistent footer/row alignment. They should not look like a third service card. |
| Connection edit service surfaces, when reachable | `KEEP_CARD` | Each service owns different fields and risk; keep independent service boundaries but use the same header, state/value, field, and action anatomy. |
| Connection edit outer grouping | `FLATTEN_TO_SECTION` | Do not stack a page card around peer service cards. |

Recommendation: retain two independent Kitsu/Discord cards rather than merge them. Merging would weaken ownership and make service-specific state/actions ambiguous. The shared configuration surface should be a section boundary, not a third card.

### System Status

| Rendered surface / class | Classification | Audit finding |
|---|---|---|
| Outer `.page-card.glass` | `FLATTEN_TO_SECTION` | It is the route shell, but its bordered elevated treatment makes the whole diagnostic page one large card around other sections. Productions uses the shell for list ownership; System Status needs flatter diagnostic grouping. |
| `.system-observability` | `FLATTEN_TO_SECTION` | One API peer section is correct; keep one section boundary, not nested card chrome. |
| `.api-observation-card` Kitsu | `FLATTEN_TO_SECTION` | API is a peer metric within the shared API section. Use aligned peer blocks with no second card border. |
| `.api-observation-card` Discord | `FLATTEN_TO_SECTION` | Same rule as Kitsu; geometry and status placement should remain identical. |
| `.pipeline-health` outer surface | `FLATTEN_TO_SECTION` | Processing is a homogeneous operational list, not an independent card. |
| `.pipeline-health-item` rows | `FLATTEN_TO_ROW` | Keep label, optional reason, status, Details, and action in one repeated row grammar with one divider rule. |
| `.pipeline-health-details` | `FLATTEN_TO_ROW` | Disclosure is row detail, not a nested surface. |
| Recent issues section when empty | `FLATTEN_TO_SECTION` | “No recent issues” is a quiet absence state and does not need a bordered alert card. |
| Recent issues section when non-empty | `KEEP_CARD` | A real failure list is a meaningful risk boundary; stronger treatment is justified. |
| `.system-overall-summary` when healthy | `REMOVE` | It repeats the API/processing health and should not render. |
| `.system-overall-summary` when degraded/unavailable/action-required | `KEEP_CARD` only if actionable | Keep only when it names a distinct aggregate condition and next action; otherwise flatten to a semantic alert row. |

This is consistent with PatternFly’s dashboard guidance that each card should communicate a distinct metric or related information group, and its warning against cards for dense repetitive text. Carbon likewise recommends structured lists for simple, scannable repeated rows rather than nested complex content.

## Connections audit

### Current anatomy

At 1440 the two peer cards are equal width (`509px` each), equal height (`104px`), aligned at y169, and use the same 16px padding, 1px border, 14px radius, and subtle surface. Status appears in the peer header; host/state fields follow; actions sit together below the peer content.

KEEP this two-peer model. Kitsu and Discord have independent state and ownership, so two cards communicate comparison faster than one merged configuration card.

### Deviations from baseline

1. The outer `.page-card.glass` remains in the DOM with transparent border/background. It is visually flat but still creates a page-level container around the peer cards; this is a semantic surface mismatch with the intended “section containing peer cards” grammar.
2. The action row contains two horizontally adjacent actions with different labels and widths. This is acceptable at desktop, but the action anatomy should be explicitly normalized: service comparison first, shared Connections actions second, consistent 12px gap, and natural wrapping/stacking at narrow widths.
3. The current disconnected/configuration states can leave visually sparse peer bodies. Empty weight should be reduced by keeping the state/value/action anatomy and avoiding placeholder explanation that does not change the operator’s next action.
4. At 375 the peer cards stack naturally, but their heights differ when English labels and action text wrap. This is content-driven and preferable to fixed heights; the alignment rule should be consistent internal anatomy, not equal card heights.

### Helper copy classification

| Copy | Classification | Reason |
|---|---|---|
| Kitsu/Discord field labels such as `Kitsu host` | `KEEP` | Identifies the value and ownership. |
| Connection state labels such as `Connected` / `Not connected` | `KEEP` | Operational state; must remain text-backed. |
| “Edit connections” / “New Production Connection” | `KEEP` | Distinct actions with different scope. |
| Any repeated “check the connection” sentence adjacent to the same state/action | `REMOVE` | Repeats the status and action without consequence or recovery detail. |
| Secret/configuration consequence or recovery copy in edit flow | `KEEP` | Safety boundary and recovery meaning take priority over compactness. |

## System Status audit

### Current anatomy

The runtime order is already semantically close to the requested structure:

1. `API response status`
2. `KitsuSync operational status`
3. `Recent system issues`

The healthy aggregate is absent in the current render, which is correct. The remaining issue is chrome and copy: a page card contains flat sections; API peers have peer-level padding/boundaries; processing rows have dividers and optional explanation/detail blocks; recent issues keeps a section header and helper even when empty.

### API peer section

KEEP one API peer section. The desktop grid measured equal columns (`503px / 503px` at 1440; `458px / 458px` at 1024; `330px / 330px` at 768). Mobile measured one column (`314px`). This geometry is correct.

Deviation: the visible API section still carries a section-card class and layered legacy rules even though its rendered border is removed. The implementation target should consolidate this into one authoritative flat section rule, preserving the equal peer grid, identical plot bounds, aligned status/value/meta, and existing chart accessibility.

### Processing list

The four required rows are present: Event monitoring, Notification processing, Internal data, and Connection/routing integrity. This is the correct structured-list model. Current row-level dividers are homogeneous, but optional explanation text appears for blocked/waiting states and can make row anatomy uneven. Details are useful for secondary diagnostic facts and should remain disclosure content.

Recommended row anatomy: label + short reason only when it changes the next action + status + Details when secondary facts exist. Keep one divider between homogeneous rows with the same token and inset. Do not wrap each row in a card.

### Recent issues

The empty state renders as `No recent issues.` / `最近の問題はありません。`, which is correctly quiet text rather than a risk card. The surrounding section still carries a heading and helper (`Recent failure and recovery records.`). The helper is useful when actual issue entries exist, but it is low-value when the list is empty.

| Copy | Classification | Reason |
|---|---|---|
| `API response status` / `API応答状態` | `KEEP` | Primary section name and clear scope. |
| `Shows real response times in chronological order.` / `実測できた応答時間を時間順に表示します。` | `SHORTEN` | Useful context, but can be reduced to a compact label such as “Observed response time” / 「実測応答時間」 if the chart label remains accessible. |
| `Review each notification stage. Metrics that are not available are shown as unconfirmed.` / equivalent JP | `SHORTEN` | Preserve the unconfirmed-state meaning, remove the first clause if the section title and rows already establish review context. |
| `Notifications remain blocked until setup is complete.` | `KEEP` | Consequence/safety copy; it explains a real operational boundary. |
| `No valid enabled route is available.` | `KEEP` | Explains why notification processing cannot proceed. |
| `Recent failure and recovery records.` / `直近の失敗と復旧記録を表示します。` | `SHORTEN` when empty, otherwise `KEEP` | Necessary context for populated issues; redundant beside the quiet empty state. |
| `No recent issues.` / `最近の問題はありません。` | `KEEP` | Clear absence state. |

### Status/detail/action placement

- API: status is correctly adjacent to each service heading; value/meta/chart follow in a consistent block.
- Processing: status is correctly in the row heading, but optional explanation can push Details/action lower and make rows uneven. Keep the explanation only for consequence/recovery context.
- Details: correct progressive disclosure mechanism; preserve keyboard access and row association.
- Actions: no primary recovery action is visible in the current healthy/unconfigured sample. If an action appears in a degraded state, align it at the row end and keep it separate from the status label.

## Responsive and interaction findings

| Check | Result | Deviation / implication |
|---|---|---|
| 1440 | PASS | Shared H1 and inset; Connections peers equal; System Status too tall/segmented. |
| 1024 | PASS | API peers remain equal; outer scrollbar reduces available page width but no overflow. |
| 768 | PASS | Connections and API peer layout remain readable; System Status becomes a long single-column diagnostic flow. |
| 375 | PASS | No horizontal document overflow; Connections cards stack; System Status is approximately 1.5kpx tall and needs chrome/copy reduction. |
| JP/EN | PASS with refinement | Information parity is present. System Status English/JP helper sentences are longer than needed for the same operational state. |
| Pointer | PASS | Peer/action controls are discoverable; no route behavior was changed. |
| Keyboard | PASS | Navigation, controls, and Details remain reachable with visible focus; no source change made in this audit. |
| Empty state | PASS with refinement | System Status empty issue message is quiet; its surrounding helper can be conditional. |

## External design-system evidence applied

- [Atlassian — Spacing](https://atlassian.design/foundations/spacing): use proximity to express relatedness, consistent spacing to create predictable list rhythm, and hierarchy through order/size. This supports one API group, one processing list, and fewer nested boundaries.
- [Carbon — Structured list usage](https://carbondesignsystem.com/components/structured-list/usage/): structured lists are for simple, scannable grouped rows; nesting is discouraged for complex repeated data. This supports flattening System Status processing into rows.
- [Carbon — Structured list style](https://carbondesignsystem.com/components/structured-list/style/): documented row padding and consistent row heights reinforce one shared row contract rather than per-row cards.
- [PatternFly — Card design guidelines](https://www.patternfly.org/components/card/design-guidelines/): cards are for distinct information groups, comparison, dashboards, or selectable objects; dense repetitive related content should use a list/data list instead.
- [PatternFly — Dashboard design guidelines](https://v5-archive.patternfly.org/patterns/dashboard/design-guidelines/): cards should communicate a distinct metric or related group and use consistent columns/gutters; this supports keeping Connections’ independent peers and removing System Status card soup.

## Prioritized findings

| # | Severity | Finding | Recommendation | Nielsen heuristic |
|---:|---|---|---|---|
| 1 | Major | System Status still reads as a bordered page containing several flat section-card layers. | Flatten the outer diagnostic surface and consolidate one boundary per semantic section. | H4 Consistency; H8 Minimalist design |
| 2 | Major | System Status processing rows become uneven when optional explanations appear. | Keep only consequence/recovery reasons; move secondary facts into Details; preserve one row divider contract. | H1 Visibility; H8 Minimalist design |
| 3 | Minor | Connections retains a transparent outer page-card wrapper. | Keep it as a layout section only, without card chrome. | H4 Consistency |
| 4 | Minor | System Status API and recent-issues helper sentences add vertical noise in common states. | Conditionally shorten/remove helpers when heading or state already communicates the meaning. | H8 Minimalist design |
| 5 | Minor | Connections actions are a shared footer but visually read as adjacent peer actions. | Normalize as one section-level action row with a stable 12px gap and mobile wrap. | H6 Recognition rather than recall |
| 6 | Enhancement | The System Status page is approximately 1.5kpx tall at 375. | Reduce chrome and redundant copy before considering any density change; do not shrink required text. | H7 Flexibility; H8 Minimalist design |

## Ordered implementation plan

1. System Status: remove visual outer card chrome while retaining the route shell and landmarks.
2. System Status: make API one flat semantic section with two equal peer blocks and one divider/boundary rule.
3. System Status: make processing a structured list with a single row divider and conditional short reasons.
4. System Status: keep non-empty issues as a risk surface; make the empty state a heading plus quiet inline message without redundant helper.
5. Connections: preserve two independent Kitsu/Discord cards; flatten only the outer grouping chrome.
6. Connections: normalize section-level action placement and verify natural wrapping at 768/375.
7. Re-run JP/EN browser geometry, keyboard, Details, overflow, and console checks.

No implementation was performed in this audit.

`KITSUSYNC_UIUX_PHASE11_AUDIT_READY`
