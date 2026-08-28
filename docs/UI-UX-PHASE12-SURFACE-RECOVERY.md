# KitsuSync UI/UX Phase 12 — Surface Recovery

Status: ready

## Result

The shared admin page surface was restored for Connections and System Status. Phase 11's internal section flattening remains, but the page surface is again the readable boundary between the global dot background and page content.

## Changes

- Restored the standard `.page-card` surface contract: `rgba(20, 21, 24, .76)` background, default border, 14px radius, 16px padding, and the existing shared width/margins.
- Kept Connections Kitsu and Discord as independent peer cards inside the shared surface.
- Kept Production-level connection actions at page level; no ambiguous peer ownership was introduced.
- Kept System Status as one shared surface with API peer blocks, structured processing rows, and a quiet Recent issues state.
- Changed processing rows to one consistent anatomy: left label/reason, right status/Details controls.
- Preserved failure, prerequisite, consequence, recovery, and safety copy.
- Reduced API metadata to window and last-updated context; polling and sparkline behavior are unchanged.
- Kept the global animated dot layer outside the page surface and did not reduce its opacity.

## Browser evidence

Real Chrome QA was run against `http://127.0.0.1:8090` in JP and EN at 1440, 1024, 768, and 375×812.

- Productions, User Linking, Connections, and System Status share the same rendered page surface values and H1 top rhythm.
- Connections peer cards are equal width on desktop and stack without horizontal overflow on mobile.
- System Status processing rows keep status and Details in the same right-side control column at all checked widths.
- System Status API peers remain equal width and the metadata no longer shows the sample-count/debug prefix.
- Selected navigation, pointer movement, keyboard focus, buttons, and Details disclosure were exercised.
- Details opened from the keyboard and focus-visible used the existing blue outline.
- All checked pages had no horizontal document overflow and no application console errors were observed.

## Validation

- `CGO_ENABLED=1 go test ./src/setup`: PASS
- `go vet ./src/setup`: PASS
- `gofmt`: PASS
- `git diff --check`: PASS
- `docker compose config --quiet`: PASS; existing optional-variable/orphan warnings only
- One Docker rebuild/recreate from `KitsuSync-clean`: PASS
- `/health`: 200
- Port exposure: loopback-only `127.0.0.1:8090->8090/tcp`

## Deferred

No Connected Production redesign, Connection Map, route/IA changes, polling changes, sparkline redesign, or external Kitsu/Discord writes were performed.
