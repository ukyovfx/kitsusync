# KitsuSync UI/UX Phase 20 — System Status Vertical Rhythm

## Scope

This phase changes only the vertical rhythm of the System Status page. Routes, polling, adaptive Y scaling, sparkline interaction, horizontal inset, status semantics, and external integrations are unchanged.

## Contract

System Status keeps three semantic groups in source order:

1. API response status
2. KitsuSync operational status
3. Recent system issues

The page now uses explicit shared tokens:

| Token | Value | Use |
| --- | --- | --- |
| `--system-status-page-title-to-section` | `var(--space-5)` / 24px | H1 to API section |
| `--system-status-heading-to-content` | `var(--space-3)` / 12px | Section heading to content |
| `--system-status-internal-item-gap` | `var(--space-2)` / 8px | Peer/detail internal rhythm |
| `--system-status-section-to-section` | `var(--space-5)` / 24px | Major section separation |

This preserves the intended hierarchy: internal item gap < heading/content gap < major section gap.

## Implemented changes

- Kept the existing shared page surface and approved horizontal content inset.
- Applied the 24px page-title-to-first-section rhythm to System Status.
- Applied 24px separation between API, operational, and issues groups.
- Reduced API peer/detail internal rhythm to the explicit 8px token without changing chart geometry.
- Set operational rows to compact 12px vertical padding and kept Details content below the row header.
- Set processing and recent-issues heading-to-content spacing to 12px.
- Kept the recent-issues empty state as quiet text without adding chrome.
- Preserved the API control's accessible target size; its control height is included in the API heading block.

## Browser evidence

Authenticated browser verification used the rebuilt `KitsuSync-clean` runtime at `127.0.0.1:8090`.

At 1440 CSS px (Chrome client width 1425px because the scrollbar remained available), JP measurements were:

| Measurement | Result |
| --- | ---: |
| H1 bottom → API section top | 24px |
| API section bottom → operational section top | 24px |
| Operational section bottom → issues section top | 24px |
| API heading block bottom → API peer content | 0px; controls are part of the heading block |
| Operational H2 bottom → first row | 13px including the structural top divider |
| Issues H2 bottom → empty state top | 12px |
| Page surface | x=176.5, width=1072px |
| System Status content section | x=193.5, width=1038px |

JP and EN were checked at 1440, 1024, 768, and 375px. The page retained the same structural order, no horizontal overflow, and no clipping. At narrow widths API peers stack naturally and processing rows remain readable.

## Validation result

- Focused System Status/telemetry tests: pass
- `gofmt`: pass
- `go vet ./src/setup`: pass
- `git diff --check`: pass
- `docker compose config --quiet`: pass
- Docker rebuild/recreate: pass
- `/health`: HTTP 200

## Deferred scope

No deferred System Status design work was opened. Connection Map, Connected Production redesign, and external Kitsu/Discord E2E remain outside this phase.
