# KitsuSync UI/UX Phase 15 — System Status Geometry

## Scope

This phase fixes only the System Status processing-row geometry. Routes, polling, sparkline behavior, notification semantics, and external Kitsu/Discord state were not changed.

## Implemented contract

- Every processing row uses one three-column header: label/reason, status, Details.
- The status column is column 2 and the Details button is column 3.
- Details content is a direct second-row grid item spanning `grid-column: 1 / -1`.
- Closed panels use `hidden` and do not contribute layout height.
- Details uses a native button with `aria-expanded` and `aria-controls`; Enter/click toggles the panel while preserving focus.
- Expanded metadata keeps the existing compact key/value list and stacks naturally on mobile.
- System Status uses the existing spacing tokens for a 24px inner horizontal gutter; API, processing, and recent-issues content share that gutter.

## Browser evidence

Authenticated Chrome QA was run against `http://127.0.0.1:8090/bot/admin/health` after Docker rebuild/recreate.

At the desktop CSS viewport of 1440px, the processing rows measured 990px wide. The Details right edge was 1767.5px for all four rows, and the status right edge was 1674.6px for all four rows in both collapsed and expanded states. The expanded content measured 990px, equal to the row width; closed content measured 0px high.

At 1024px, rows measured 899px wide and all four status right edges were 861.1px; all Details right edges were 954px. At 768px, rows measured 643px wide and all four status right edges were 607.3px; all Details right edges were 698px. At 375px, the four rows retained a common Details right edge of 321px with no horizontal overflow.

The 375px JP check expanded the first row with keyboard Enter. Focus remained on the toggle, `aria-expanded` changed to `true`, the panel became visible at 282px wide, and its metadata stacked naturally. English and Japanese checks at 1440px, 1024px, 768px, and 375px reported no horizontal overflow.

Interaction checks covered all four rows collapsed, all four expanded, multiple rows open, keyboard toggle, and focus retention. The visible toggle pattern is `Details ▾` / `Hide details ▴`.

## Validation

- Focused render test: PASS
- `go vet ./src/setup`: PASS
- `gofmt`: PASS
- `git diff --check`: PASS
- `docker compose config --quiet`: PASS
- Docker CGO-enabled rebuild/recreate: PASS
- `GET http://127.0.0.1:8090/health`: 200
- Browser overflow check: PASS at 1440, 1024, 768, and 375 CSS widths

The host-side CGO test command was attempted with `CGO_ENABLED=1`, but this Windows environment has no `gcc` executable. The Docker build compiled the affected Go package with CGO enabled successfully.

## Changed files

- `src/setup/ia_views.go`
- `src/setup/ia_views_test.go`
- `src/setup/ui.go`
- `docs/UI-UX-PHASE15-SYSTEM-STATUS-GEOMETRY.md`

## Deferred scope

No further System Status redesign, sparkline redesign, Connection Map work, Connected Production work, merge, or release was started.
