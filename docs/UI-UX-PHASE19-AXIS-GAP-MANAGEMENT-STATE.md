# KitsuSync UI/UX Phase 19 — Axis Gap and Management State

## Scope

Only two visual defects were addressed: external sparkline Y-label spacing and the Dashboard Management grid's one-sided orange divider. Routes, behavior, polling, adaptive scales, tooltip interaction, and notification semantics are unchanged.

## Findings and fixes

- Sparkline labels remain in the shared 56px external label column. The label-to-plot gap is now the shared `--sparkline-axis-gap: 4px` token, applied equally to Kitsu and Discord.
- The Management grid intentionally retains neutral right-side structural dividers. Earlier hover/focus border-color rules were coloring that divider orange because the card's main border was removed later. A shared state override now keeps border, `border-right`, and `border-inline-end` on `var(--line)` for hover, focus, active, and current states. Focus-visible outline remains available.

## Validation evidence

- Focused chart/state tests: PASS
- `go vet ./src/setup`: PASS
- `gofmt`: PASS
- `git diff --check`: PASS
- Browser QA: authenticated Chrome, JP/EN, 1440/1024/768/375px
- Chart label columns and plot widths are shared; no overflow or clipping
- Tooltip, hover guide, temporary marker, and polling rebind remain functional
- Management hover/focus/click states retain neutral dividers with no one-sided orange edge
- No external Kitsu/Discord writes performed
