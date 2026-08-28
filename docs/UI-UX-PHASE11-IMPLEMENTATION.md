# KitsuSync UI/UX Phase 11 — Connections and System Status Implementation

Status: complete

## Scope

This implementation is limited to Connections and System Status. Routes, navigation, polling, notification semantics, charts, dot animation, and external Kitsu/Discord state were not changed.

## Implemented

### Connections

- Preserved independent Kitsu and Discord peer cards because they represent separate state, diagnostics, and actions.
- Kept Production-level connection actions outside the peer cards, so neither peer visually owns the Production action.
- Preserved equal peer-card geometry without equal-height or filler content hacks.
- Kept the page-level outer surface visually flat; the peer cards remain the only bordered configuration surfaces.

### System Status

- Flattened the outer page card while retaining the shared page shell and readable section boundaries.
- Kept the hierarchy as API response status → KitsuSync processing → Recent system issues.
- Kept Kitsu API and Discord API as equal peer blocks with shared geometry and aligned content.
- Kept processing as four structured rows with one divider model, status badges, and keyboard-accessible Details disclosures.
- Removed redundant API, processing, and Recent issues helper sentences and the duplicated processing-level readiness pill.
- Preserved consequence and safety explanations, including blocked notifications until setup and missing route guidance.
- Empty Recent system issues remains a quiet inline state; populated issues retain their risk-oriented list treatment.

## Browser evidence

Real Chrome QA was run against `http://127.0.0.1:8090` after rebuild/recreate.

- Connections and System Status were checked in JP and EN at 1440×900, 1024px, 768px, and 375×812.
- Connections peer cards remained two equal-width peers on desktop and stacked cleanly on mobile.
- System Status API peers measured equal widths at desktop/tablet sizes and equal single-column widths on mobile.
- The System Status outer surface computed as transparent after the flattening rule.
- All six requested admin screens were smoke-checked in JP/EN: Dashboard, Productions, User Linking, Connections, System Status, and Audit Log.
- Each checked screen rendered one H1 and no horizontal document overflow.
- Pointer movement and keyboard focus were exercised; Details opened from the keyboard and focus remained visibly outlined.
- A browser console entry observed during QA was a browser-extension asynchronous-channel warning, not an application error.

## Validation

- `CGO_ENABLED=1 go test ./src/setup`: PASS
- `go vet ./src/setup`: PASS
- `gofmt`: PASS
- `git diff --check`: PASS
- `docker compose config --quiet`: PASS; only existing unset optional Filebrowser variables and an orphan mock-container warning were reported
- One Docker rebuild/recreate from `KitsuSync-clean`: PASS
- `GET http://127.0.0.1:8090/health`: 200
- Published port remained loopback-only: `127.0.0.1:8090->8090/tcp`

## Deferred

Connected Production redesign, Connection Map, System Status metric redesign, sparkline logic changes, external E2E, merge, and release remain outside this phase.
