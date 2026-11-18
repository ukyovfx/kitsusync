# Admin UI design system (サイズと配置の統一ルール)

Status: active (implementation + visual iteration in progress)
Base: master c2738e6bf4ac7604dd4b7c4abdb0c8c2b515c4f6
Scope: `src/setup` admin pages (CSS in `ui.go`, markup in `ia_views.go` / `current_routing.go` / `forum_settings.go` / `pr199_user_linking_render.go`).
Not in scope: login page, setup flow semantics, API/DB contracts, copy changes beyond what a pattern requires.
Prior audit (A1-A15 / C1-C16) fixed overflow and wording; this document defines the system itself.

## 1. Buttons

| Variant | Class | Use |
| --- | --- | --- |
| Primary | `.btn` | one per action group, always the last (rightmost) item |
| Secondary | `.btn-ghost` | Edit, Cancel, Back, Add, Open, Copy |
| Danger | `.btn-danger` | Remove / Delete / Disconnect |
| Quiet | `.btn-quiet` | tertiary in-row navigation that used to be an underlined link |

Two sizes only:

| Size | min-height | padding-inline | font | min-width | where |
| --- | --- | --- | --- | --- | --- |
| Regular (default) | 40px | 16px | 14px / 600 | 80px | page, section header, form, dialog, edit footer |
| Compact | 32px | 12px | 13px / 600 | 64px | inside table rows and list rows only |

- Radius 8px, gap between icon/text 8px, no letter-spacing above .02em.
- `Edit`, `Apply`, `Cancel`, `Add`, `Remove` are all Regular unless inside a table/list row (then Compact). Primary is never larger than the others.
- Disabled (every variant): background rgba(255,255,255,.05), border rgba(255,255,255,.12), text #9a958d (>= 4.5:1 on the card surface), no opacity fade, no shadow, `not-allowed`. A disabled button must stay readable.
- Order in any action group: Cancel/Back first, Primary last; groups sit on one row, 8px apart.

## 2. Pills / status

- One component: `.status-pill`, `.status-badge`, `.dashboard-status-chip` share metrics.
- Height 24px, padding-inline 10px, font 12px / 600, radius 999px, 1px border, content-sized, nowrap.
- Colors (meaning): Healthy / Connected / Linked = green; Needs review / Disconnected / Not configured / Waiting / Unavailable = yellow (kept from `docs/CURRENT-IA-UI-SPEC.md`: Disconnected is yellow, not gray); Error / Failed = red; Off / Disabled / no state = gray; Blocked = orange.
- Placement:
  1. Page or section header: right end of the title row, vertically centered with the title.
  2. Table: its own Status column, left-aligned, vertically centered. Never stacked in a cell with a button or link.
  3. List row: fixed-width state column (8rem) between the name and the action.
- A given state uses the same label in view and edit modes of the same section.

## 3. Tables

- Cell padding 12px 12px; header 12px, muted; row min-height 56px.
- Column roles: identity (primary) column `minmax(14rem,1.4fr)` and never below 12rem at >= 761px; status column 8.5rem; action column `max-content`, right-aligned, nowrap; other columns `minmax(8rem,1fr)`.
- Long names: one line, `text-overflow:ellipsis`, full text in `title`. Rows never grow because a name wrapped. Explanatory notes may wrap to at most 2 lines.
- <= 760px: each row becomes a labeled card (label above value); action buttons keep Compact size.

## 4. Spacing scale

Tokens: 4 / 8 / 12 / 16 / 24 / 32 px.

| Where | Value |
| --- | --- |
| icon/text, chip inner | 4 |
| label to its control, control to control in one row, buttons in a group | 8 |
| heading to its content, related list items | 12 |
| form field to field, card padding, content to the action bar inside a card | 16 |
| section to section, page title to first section | 24 |
| page-level group separation | 32 |

- No element may add extra blank space after a table or section (no `min-height` filler); a section ends at its last control + 0.
- Dialog and expanded panel items use the same scale: label to value 12 in a fixed label column of 9rem.

## 5. State and action pattern (every editable section)

1. View mode: header = title (left) + status pill, Edit (Regular secondary) at the right end. Content is read-only.
2. Edit mode: the view table is replaced by the edit UI (never shown twice). Header keeps title + the same status pill; the Edit button is gone.
3. Footer bar (edit mode): pending text on the left (13px muted), `Cancel` (secondary) then `Apply` (primary) on the right. Apply is disabled and readable until something changes.
4. After Apply/Cancel the section returns to view mode.

## 6. Dialogs

- Width `min(560px, 100vw - 32px)`, fixed (never shrink-to-fit); padding 24px; title 18px / 600.
- Title to tabs 16, tabs to content 16, field to field 16, content to action bar 24; action bar right-aligned: Cancel then primary, 8px gap.
- Choice lists: option min-height 40px, single line with ellipsis + `title`; secondary note (e.g. "Not mentionable") is a right-aligned note on the same row, not appended long text; list max-height 280px, scrolls.
- If a tab has no choices, show only the empty message (no empty picker). The confirm button keeps the primary style and uses the disabled style until a value is chosen.

## 7. Adopted numbers and inspection log

Adopted values (CSS: the "Admin UI design system v1" block at the end of `src/setup/ui.go`):

| Item | Value |
| --- | --- |
| Button, regular / compact | 40px / 32px, padding-inline 16px / 12px, font 14px / 13px, 600, radius 8px |
| Exception | a button beside an input (External URL, storage Save) takes the input height, 44px |
| Pill | 24px high, padding-inline 10px, font 12px / 600, one size for `.status-pill`, `.status-badge`, `.dashboard-status-chip` |
| Spacing tokens | 4 / 8 / 12 / 16 / 24 / 32 |
| Section padding in Notifications | 24px top and bottom |
| Dialog | `min(560px, 100vw - 32px)`, padding 24px, opaque `--bg2` background, list max-height 280px, options 40px |
| Production list row | name `1fr`, state column 8rem, action auto (>= 761px); name row then state + action row (<= 760px) |
| Notifications edit table | task column 18rem, forum column next to it (left aligned); stacked cards <= 760px |
| Forum settings table | label column 16rem, select max 32rem |
| Team table | member 13rem, role 6.5rem, department 9rem, Discord flexible, status 8.5rem, action 12rem; linked rows have no action |
| User linking table | grid `minmax(14rem,1.4fr) minmax(10rem,1fr) 8.5rem minmax(0,1.7fr)` |

Decision kept from the existing spec: Disconnected is yellow (not gray); gray is for off / no state.

Inspection rounds (screenshots: ja/en x 1440, 1024, 760, 390, 16 states each, via the reviewer fixture server; audit method: unslop pass order spacing, color, icons, copy, type, layout, details; guidelines from artifact-design and the owner's five points):

- Round 1 differences fixed: dialog showed content through its background (made opaque); open list covered the dialog title (list now opens below the field); large blank under the notifications table (section padding 24px); "Add" button floated mid-row beside Remove (moved under the list, left aligned); notifications heading row jumped between view and edit mode (min-height 40px).
- Round 2 differences fixed: forum column too far right in the edit table (legacy `th:last-child{width:8%}` rule; now task column 18rem, forum next to it); forum settings select started at 50% (label column 16rem); Team table repeated actions twice and overlapped (action cell reset per branch; linked rows have no action); names over-truncated (member column 13rem, `title` for full text).
- Round 3 differences fixed: production list name broke one character per line at 390px (grid made breakpoint-specific); notifications heading centered and stacked on mobile (row layout); 44px and 34px outlier buttons (connections actions to 40px, endpoint reset 32px); route table dividers cut short at <= 760px (width rules removed there); listbox options clipped without full text (added `title`).
- Measured on the final 160 captures: horizontal page scroll 0; pill heights all 24px; button heights 32 / 40 plus the documented 44px input-adjacent exception.
- Acceptance tests updated to the new structure: Team has separate Status and Action columns, Edit button 40px and pill 24px, forum name column is the fixed label column, edit mode shows the directory notice and the same status pill as view mode.
