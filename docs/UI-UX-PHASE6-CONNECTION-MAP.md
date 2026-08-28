# KitsuSync UI/UX Phase 6 — Connection Map Prototype

Date: 2026-08-28
Status: prototype evaluation complete
Verdict: `CONNECTION_MAP = DEFER`

## Scope and guardrails

The prototype tests one synthetic Production (`Bluebird`) with a stable two-column relationship: Kitsu Task Type → Discord Channel. It is a standalone local HTML file under `docs/design-work/`; it is not routed from KitsuSync, does not read runtime data, does not call Kitsu or Discord, does not write configuration, and does not persist node positions.

Existing Production lists, Connections, and settings remain canonical. No navigation, route, notification, polling, security, or external state was changed. User mapping was intentionally excluded from this first graph because it would introduce a second relationship model before the primary routing relationship is proven.

Prototype: [phase6-connection-map-prototype.html](design-work/phase6-connection-map-prototype.html)

## Prototype structure

- Stable Kitsu-left / Discord-right layout; no force simulation.
- Small, medium, dense, and empty synthetic samples selected locally.
- Read-only node selection with a keyboard-reachable inspector.
- Local viewport pan, wheel zoom, and reset view; no configuration controls.
- Semantic relationship table parallel to the graph.
- At narrow widths the table is the primary representation; the graph is hidden rather than squeezed into unreadable text.
- Dense mode keeps text size stable and explicitly recommends filtering or the list.

Node dragging was not retained in the evaluated version. The stable semantic columns already provide the useful spatial explanation; free placement would add local state and edge-maintenance cost without improving the operator decision.

## Browser evidence

Captured with Chrome at the requested target sizes using the loopback-served local prototype. The 375×812 captures use the mobile fallback/list representation.

- Small graph, EN, 1440×900: [phase6_map_small_en_1440x900.png](audits/runtime-evidence/phase6_map_small_en_1440x900.png)
- Selected node and inspector, EN, 1440×900: [phase6_map_selected_en_1440x900.png](audits/runtime-evidence/phase6_map_selected_en_1440x900.png)
- Medium graph, EN, 1440×900: [phase6_map_medium_en_1440x900.png](audits/runtime-evidence/phase6_map_medium_en_1440x900.png)
- Dense graph, EN, 1440×900: [phase6_map_dense_en_1440x900.png](audits/runtime-evidence/phase6_map_dense_en_1440x900.png)
- Dense graph, EN, 1024×768: [phase6_map_dense_en_1024x768.png](audits/runtime-evidence/phase6_map_dense_en_1024x768.png)
- Small graph, JP, 1440×900: [phase6_map_small_ja_1440x900.png](audits/runtime-evidence/phase6_map_small_ja_1440x900.png)
- Medium mobile fallback, JP, 375×812: [phase6_map_mobile_ja_375x812.png](audits/runtime-evidence/phase6_map_mobile_ja_375x812.png)
- Dense mobile fallback, JP, 375×812: [phase6_map_dense_ja_375x812.png](audits/runtime-evidence/phase6_map_dense_ja_375x812.png)

## A/B findings

| Lens | A — existing list/settings | B — map prototype | Finding |
|---|---|---|---|
| Operator understanding | Reliable exact rows and state | Faster first-glance answer for three direct routes | B helps orientation only at small density |
| Misrouting detection | Explicit row-by-row comparison | Parallel edges make a small mismatch salient | B loses this advantage as edges accumulate |
| Cognitive load | Predictable and dense | Spatial scan is initially easy | Dense B becomes visual noise and needs filtering |
| Visual / anti-AI | Existing operational surface | Restrained, semantic, no spectacle | B is credible, but visual novelty alone is not a reason to maintain it |
| Accessibility | Canonical semantic controls | Table plus focusable nodes and inspector | Table remains the dependable representation |
| JP/EN | Existing terminology and wrapping | Concise labels; JP fallback stays readable | No material language regression in the prototype |

The map can make a three-route relationship understandable faster than the canonical list. It does not yet prove a durable operational advantage over the list/settings surface, especially for real connected data or dense routing. Therefore adoption is not justified.

## Density limits

- Empty: a clear empty state is possible, but it is not more useful than the canonical empty state.
- Small (3): clearest value; direct left-to-right reading works at desktop sizes.
- Medium (6): still usable with selection and the parallel table, but the graph begins competing with the exact list.
- Dense (12): text can remain legible only by accepting a busy surface; progressive disclosure or filtering would be required. Shrinking nodes was rejected.
- Mobile: the accessible list is clearer and more reliable than a compressed graph. No document-level horizontal overflow was observed in the 375×812 captures.

## Accessibility and language result

The graph is not the only representation. Nodes are native buttons with visible focus behavior; selection updates a named, live inspector; the relationship table has a caption, headers, and state text; relationship meaning is not encoded by color alone. Primary controls meet the 44px target intent. The mobile fallback preserves the relationship columns without requiring a pointer.

JP and EN communicate the same relationship and safety boundary. Product terminology remains `Kitsu`, `Discord`, `Production`, and `Connection Map`; operational copy remains sans and concise. No mojibake or material clipping was observed in the captured JP/EN states.

## User mapping and maintenance decision

User mapping does not belong in this graph yet. Kitsu User ↔ Discord User is a separate identity-resolution task with different privacy, error, and action semantics. Combining it with notification routing would increase cognitive load and make the graph harder to explain.

Node drag is not useful enough for the first prototype. Stable semantic columns answer the core question; local arrangement does not persist and would add edge synchronization, keyboard parity, and test surface.

The maintenance cost is non-trivial: a second representation needs a11y parity, responsive fallback, density policy, focus/selection semantics, and ongoing agreement with the canonical routing model. Without evidence from real connected Production data that operators make materially faster or safer decisions, that cost is not warranted.

## Independent review lenses

Four fresh review prompts were dispatched covering operator understanding, visual/anti-AI, accessibility/responsive, and JP/EN. Those tasks did not return usable review text within this run, so their absence was not treated as approval. The final decision below is based on the captured browser evidence and the structured six-axis review performed here:

- Operator understanding: `KEEP` for small read-only exploration; overall `DEFER` for product adoption.
- Visual / Anti-AI: `KEEP`; restrained and identity-consistent, with no novelty-driven adoption case.
- Accessibility / responsive: `KEEP` for the parallel list and mobile fallback; `DEFER` graph as a canonical surface.
- JP/EN: `KEEP`; terminology and concise labels remain balanced.

No bounded REFINE was required for the retained docs-only evidence prototype after the hidden empty-state and node-placement defects found during browser QA were corrected.

## Deferred scope

- No runtime Production integration.
- No new route or navigation entry.
- No graph editing, route creation/deletion, webhook changes, or persistent layout.
- No Connection Map canonicalization.
- No Connected Production redesign.
- No user-mapping graph layer.
- No release or external end-to-end execution.

## Validation

- Chrome smoke: empty/small/medium/dense, selected inspector, EN/JP, 1440×900, 1024×768, and 375×812.
- Console check: no application errors; only the known browser-extension asynchronous message-channel warning appeared.
- `git diff --check`: passed; existing line-ending warnings only.
- `docker compose config --quiet`: passed; existing unset `FB_USERNAME`/`FB_PASSWORD` warnings only.
- The prototype is docs-only, so no Docker rebuild or runtime health change was required. Existing KitsuSync runtime state was not modified.

Final decision: keep the standalone evidence prototype for future connected-Production research, but defer Connection Map from the product UI until real-data usability evidence demonstrates a clear operator benefit over the canonical list/settings.
