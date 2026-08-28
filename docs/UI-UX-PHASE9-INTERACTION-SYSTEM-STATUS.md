# KitsuSync UI/UX Phase 9 — Interaction System & System Status

## Scope

This was a targeted refinement. Routes, information architecture, horizontal navigation, polling semantics, notification behavior, and external Kitsu/Discord behavior were unchanged.

## Interaction-state audit

The shared shell had several local hover grammars: some controls translated on hover, some changed only a border, and some used page-specific backgrounds. Phase 9 formalized one interaction grammar for nav items, buttons, row/disclosure actions, selects, status surfaces, and links:

- hover changes color/background/border/shadow only
- hover and focus do not translate or change width/height
- focus-visible uses the shared 3px focus outline with 3px offset
- selected/current nav retains a semantic accent border
- disabled controls retain their geometry and remove hover emphasis
- status surfaces remain color-independent through text labels and `role="status"`

Browser verification confirmed stable geometry for representative interactive controls and visible keyboard focus.

## Typography and spacing

The Phase 8 title tokens remain canonical. Phase 9 rechecked rendered geometry in JP and EN at 1440, 1024, 768, and 375 widths. Six required admin routes each retained one `main`, one `h1`, and no horizontal document overflow.

## Card and divider rule

Cards are reserved for independent state, independent action, meaningful grouped objects, or a risk/decision boundary. System Status now presents the overall state as a compact summary row and uses whitespace plus shared dividers for API and processing sections. Homogeneous processing rows share one inset, one divider color, and one divider thickness; decorative separators around every text block were not added.

## System Status

The overall healthy state no longer repeats a sentence explaining that healthy observations are healthy. It shows `System` and the status pill; secondary copy appears only for unavailable, unconfirmed, or review-needed states.

API responsiveness remains a two-column desktop peer and stacks naturally on narrow screens. KitsuSync processing is a compact operational list. Healthy rows omit redundant explanations; issue, prerequisite, and recovery explanations remain available, with Details disclosure retained where useful. Recent issues and diagnostics remain accessible without changing polling or action semantics.

## Sparkline time model and live polling

Both charts use the selected current-time window as their X domain: current time minus 60 seconds or five minutes through current time. Sample timestamps remain legitimate and sparse; no interpolation or fabricated sample is introduced. Kitsu and Discord use the same plot geometry and continuous line treatment, with no circles or visible X-axis labels.

In the browser, three successive polling observations were captured at approximately five-second intervals. The live label remained `Auto-refresh`, chart paths remained present, both viewBoxes remained `0 0 466 104`, and both charts contained zero circles. The latest response values changed during the observation window, confirming live re-rendering without page reload.

## Dot background evidence

The admin background now uses a restrained two-scale stipple field plus a low-density brown/orange atmospheric layer. It remains slow (`52s`) and continuous; measured background positions changed over 3.5 seconds while opacity stayed at `0.105`. The dashboard screenshot showed the dot/mist texture in the open background without competing with panels. Login/public retains the stronger public tier.

The reduced-motion source contract remains explicit: `@media (prefers-reduced-motion: reduce)` disables `particleDrift` while retaining the static texture. No entrance animation, cursor following, glow spectacle, or new particle system was introduced.

## Responsive and language results

Dashboard, Productions, User Linking, Connections, System Status, and Audit Log were checked in JP and EN at 1440×900, 1024×768, 768×1024, and 375×812. All 48 combinations had no horizontal document overflow. Long English status labels remained inside their parent surfaces.

## Validation

- `gofmt` — passed
- CGO-enabled `go test ./src/setup` in Go 1.21 Bookworm — passed
- CGO-enabled `go vet ./src/setup` — passed
- `git diff --check` — passed
- `docker compose config --quiet` — passed
- Docker rebuilt/recreated once after the final Phase 9 changes
- `/health` — HTTP 200
- runtime remains loopback-only on `127.0.0.1:8090`
- no application console errors; unrelated browser-extension async-response warnings were observed separately

## Deferred

Connection Map, Connected Production redesign, release/merge, and external Kitsu/Discord writes remain deferred.
