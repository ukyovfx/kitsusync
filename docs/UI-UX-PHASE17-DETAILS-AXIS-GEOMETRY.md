# Phase 17 — Details and axis geometry

Date: 2026-08-28

## Changes

- Details labels no longer receive dynamic `hidden` attributes. The button keeps both label slots in the same intrinsic grid geometry, while `aria-expanded` controls visual label visibility.
- The shared chart geometry reserves a 72-unit Y-label column and starts both Kitsu and Discord plots at x=72. Labels are right-aligned at x=66.
- Compact charts show upper and lower Y bounds only. Adaptive domains, hysteresis, polling, equal X sample spacing, and independent service domains are unchanged.
- The polling refresh script applies the same axis geometry, so a refresh cannot restore the prior 54/48 placement.

## Browser evidence

- Authenticated System Status verified after the latest Docker rebuild.
- All four processing rows completed open → close → open → close with mouse. Each returned to the same collapsed geometry; status and Details X positions remained stable.
- Kitsu and Discord charts both used guide x=72, label x=66, and plot paths starting at x=72. No label entered the plot column.
- Verified before and after a polling refresh; no layout shift or one-way toggle behavior.
- JP and EN checked at 1440×900, 1024×768, 768×768, and 375×812.
- No horizontal overflow or clipping observed.

## Validation

- Focused chart and Details regression tests: PASS.
- `go vet ./src/setup`: PASS.
- `gofmt`: PASS.
- `git diff --check`: PASS.
- `docker compose config --quiet`: PASS.
- Latest CGO-enabled Docker build and app recreate: PASS.
- `/health`: HTTP 200.

The host-side CGO test remains unavailable because the Windows environment has no `gcc`; this is an environment limitation, while the Docker CGO build succeeded.

## Result

| Check | Result |
| --- | --- |
| DETAILS_POST_COLLAPSE_ALIGNMENT | PASS |
| HIDDEN_PANEL_ZERO_LAYOUT | PASS |
| STATUS_COLUMN_STABILITY | PASS |
| Y_LABEL_OUTSIDE_PLOT | PASS |
| SHARED_CHART_GEOMETRY | PASS |
| ADAPTIVE_Y_DOMAIN | PASS |
| JP_EN | PASS |
| RESPONSIVE | PASS |
