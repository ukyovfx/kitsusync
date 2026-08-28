# KitsuSync UI/UX Phase 14 — Adaptive Sparkline

## Scope

Only the System Status API sparklines were changed. Polling cadence, API payloads, sample-index X spacing, route behavior, status semantics, and other admin screens were not changed.

## Implementation

- Kitsu and Discord now calculate independent Y domains from the visible legitimate samples.
- Domains use rounded readable bounds, 10 ms minimum useful span, and bounded headroom above and below the observed values.
- Upper-domain expansion is immediate so a real latency spike remains visible.
- Downward contraction is held for 15 seconds, avoiding visible scale jitter during normal polling.
- Labels are rendered outside the plot area at a shared left gutter: upper, midpoint, and lower values in rounded `ms` units.
- Both charts retain the same 466×104 viewBox, plot bounds, line style, and sample-index X slots. No point markers or visible X-axis labels are used.
- The live refresh script applies the same domain and stability rules as the initial server-rendered chart.

## Evidence

Browser checks against `http://127.0.0.1:8090/bot/admin/health`:

- 1440×900, 1024×768, 768×768, and 375×812: two charts rendered with no document horizontal overflow.
- Mobile JP check: chart width 314 CSS px, labels remained outside the plot, no clipping or console errors.
- Ten 5-second refresh observations: chart geometry remained stable; sample paths progressed through equal X slots (`54`, `259`, `464`) as samples accumulated.
- Live examples showed independent ranges such as Kitsu `100/50/0ms` and Discord `400/275/150ms`; the differing domains preserved readable trend amplitude.

## Validation

- Focused adaptive-sparkline tests: PASS
- `go vet ./src/setup`: PASS
- `gofmt`: PASS
- `git diff --check`: PASS
- `docker compose config --quiet`: PASS (existing unset optional `FB_USERNAME` / `FB_PASSWORD` warnings only)
- One Docker build/recreate with `CGO_ENABLED=1`: PASS
- `/health`: HTTP 200
- Browser console error check: PASS
- Host full CGO test command could not start because the host has no `gcc`; the Docker CGO-enabled application build succeeded.

## Result

`DYNAMIC_Y_DOMAIN = PASS`

`Y_SCALE_STABILITY = PASS`

`AXIS_LABEL_PLACEMENT = PASS`

`OUTLIER_HANDLING = PASS`

`X_EQUAL_SPACING = PASS`

`LIVE_UPDATE = PASS`

`RESPONSIVE = PASS`
