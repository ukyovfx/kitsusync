# KitsuSync UI/UX Phase 18 — External Axis Labels

## Scope

This change is limited to System Status content inset and API sparkline axis geometry. Routes, polling, adaptive domains, equal sample spacing, hover inspection, keyboard inspection, and notification behavior are unchanged.

## Implementation

- System Status keeps the shared page surface and uses the existing `--system-status-content-inset` contract for API, processing, and issue content.
- Each API chart now renders as `label column + plot SVG` siblings. The label column is fixed at 56px and the plot uses a 394×104 viewBox with a 392-unit drawing span.
- Persistent Y labels are max/min only and are outside the dark plot SVG. The SVG contains the line, guide, and temporary interaction layers only.
- Server-rendered and polling-rendered charts use the same geometry. Longer labels therefore cannot reduce the Discord plot width relative to Kitsu.

## Browser evidence

Authenticated Chrome verification at 1440, 1024, 768, and 375px, in English and Japanese:

- no horizontal overflow
- both chart rows reserve 56px for labels and have equal plot widths/heights at each viewport
- the plot SVG contains no persistent Y-axis text; only the hidden interaction tooltip text node remains
- max/min labels are rendered in the sibling label column
- Kitsu and Discord plot paths use the same left/right bounds
- tooltip and temporary guide/marker remain functional after the split
- tooltip exit clears both interaction layers

## Validation

- focused sparkline and refresh tests: PASS
- `go vet ./src/setup`: PASS
- `gofmt`: PASS
- `git diff --check`: PASS
- `docker compose config --quiet`: PASS
- Docker CGO-enabled rebuild/recreate: PASS
- runtime `/health`: HTTP 200

Host-side full CGO tests remain dependent on the unavailable host `gcc`; the Docker build compiled the affected package with CGO enabled.
