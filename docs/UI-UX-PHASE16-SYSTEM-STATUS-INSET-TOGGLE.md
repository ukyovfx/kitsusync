# Phase 16 — System Status inset and Details toggle

Date: 2026-08-28

## Result

The System Status surface now keeps full-width structural dividers while using one shared `--system-status-content-inset` token for headings, API peer content, processing row content, and the recent-issues empty state. Processing rows retain stable label/reason, status, and Details columns; expanded details occupy the row width below the header.

Details is a native button with one delegated click handler. It toggles `aria-expanded`, the controlled panel's `hidden` state, and the visible label in both directions. Native keyboard activation supports Enter and Space, and focus remains on the button.

## Browser evidence

- Authenticated `http://127.0.0.1:8090/bot/admin/health?lang=en` and `lang=ja` verified.
- Tested 1440×900, 1024×768, 768×768, and 375×812.
- No horizontal overflow; scrollbar remained available.
- All four rows were opened, closed, reopened, and closed with mouse, Enter, and Space.
- The first row remained expanded through approximately 26 seconds, covering more than five 5-second polling intervals; it remained expanded and then closed successfully.
- Desktop row geometry remained stable: structural row width exceeded its inset content, and status/Details controls stayed in consistent right-side columns.
- Mobile wrapped long Japanese/English row content without clipping or horizontal overflow.
- Browser console contained no error entries.

## Validation

- Focused non-database System Status tests: PASS.
- `go vet ./src/setup`: PASS.
- `gofmt`: PASS.
- `git diff --check`: PASS.
- `docker compose config --quiet`: PASS (existing unset optional FB variables only emitted warnings).
- One `docker compose up -d --build --force-recreate app`: PASS; CGO-enabled Docker build succeeded.
- `/health`: HTTP 200; runtime healthy and loopback-only `127.0.0.1:8090`.
- Full host test suite and CGO-enabled host test could not execute because this Windows host has no `gcc`; the failures are the existing go-sqlite3 CGO stub/compiler absence, not Phase 16 assertions.

## Contract status

| Contract | Result |
| --- | --- |
| STRUCTURAL_WIDTH | PASS |
| CONTENT_INSET | PASS |
| USER_LINKING_GRAMMAR_MATCH | PASS |
| DETAILS_OPEN | PASS |
| DETAILS_CLOSE | PASS |
| DETAILS_REPEAT_TOGGLE | PASS |
| POLLING_STATE | PASS |
| ARIA_STATE | PASS |
| JP_EN | PASS |
| RESPONSIVE | PASS |

## Changed files

- `src/setup/ia_views.go`
- `src/setup/ia_views_test.go`
- `src/setup/ui.go`
- `docs/UI-UX-PHASE16-SYSTEM-STATUS-INSET-TOGGLE.md`
