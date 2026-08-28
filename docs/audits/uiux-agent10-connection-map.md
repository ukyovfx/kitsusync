# Agent 10 — Connection Map Feasibility Audit

Date: 2026-08-28
Scope: Connection Map cognitive value, graph density, scope/filtering, inspector, editing boundary, and verdict.
Method: independent repository-grounded design audit. No other agent audit or conclusion was used.

## Executive verdict

Connection Map is feasible only as a bounded, read-only visualization inside one Production's routing context. It should supplement the existing exact mapping list, not replace it, become a cross-Production dashboard, or become an in-graph editor.

Recommended placement: `/bot/admin/projects?project=<id>&tab=notifications`, adjacent to or below the existing `Kitsu Task Type → Discord Channel` summary. The map should be opt-in or secondary within that tab. The existing list remains the authoritative inspection and fallback surface.

Verdict: `FEASIBLE WITH LIMITS`
Confidence: High for the current implementation boundary; Medium for operator value because no browser session, representative production data, or user testing was available.

## Evidence and inference convention

- Evidence means directly observable in the repository: route registration, render functions, stored models, validation rules, or existing copy.
- Inference means a design judgment derived from that evidence. It is not a claim that the proposed map already exists or has been validated with users.

## Current route and screen

### Evidence

- The normal Production detail route is `/bot/admin/projects?project=<id>`. Its section navigation includes `overview`, `notifications`, `users`, `storage-settings`, `activity`, `troubleshooting`, `advanced`, and `danger-zone`; there is no `connection-map` tab. Evidence: `src/setup/ia_views.go:555-575`.
- The Notifications screen renders a `Notification routing` section and a read-only `Kitsu Task Type → Discord Channel` mapping. Evidence: `src/setup/ia_views.go:1057-1071` and `src/setup/current_routing.go:281-295`.
- The editor is entered through the explicit `edit_routing=1` query state on the same Production detail route. Evidence: `src/setup/ia_views.go:1067-1070`.
- `/bot/admin/production-routing` is registered as a compatibility route; GET is sent to a compatibility handler while POST uses the routing handler. It is not a normal Current IA map destination. Evidence: `src/main.go:915-920`.

### Inference

The map has a clear semantic home only when the user is already inspecting one Production's Notifications. A new top-level map route would make a secondary diagnostic abstraction compete with the existing Production-first information architecture.

## 1. Cognitive value

### Positive value

Evidence: the current routing model is a Production-scoped relationship from a Kitsu Task Type to one Discord destination. `ProductionNotificationRoute` stores `ProductionID`, `TaskTypeID`, and `DestinationWebhookID`; the current summary renders each route as a left-to-right mapping. Evidence: `src/model/model.go:511-520` and `src/setup/current_routing.go:281-295`.

Inference: a restrained map can make three things faster to perceive than a plain list:

1. whether every visible Task Type has a destination;
2. whether one Discord channel appears to receive multiple Task Types;
3. which side of the integration owns the missing or broken relationship.

These are diagnostic questions, not monitoring metrics. The map's cognitive value is therefore highest during route review, correction, or troubleshooting, and low during routine dashboard scanning.

### Cognitive risks

Inference: a graph can imply a richer network than the product actually manages. The implemented relation is mostly a one-hop mapping, not a general dependency graph. Drawing extra nodes for credentials, webhooks, categories, guilds, users, tasks, or runtime events would increase interpretation cost and invite users to infer unsupported relationships.

Inference: a graph does not improve exact lookup when the operator needs the Task Type name, destination name, route status, or action. The existing list is more scannable for exact comparison and remains necessary for keyboard and narrow-width fallback.

Severity: Medium
Confidence: High for the one-hop model; Medium for perceived cognitive load without live usability testing.

## 2. Graph density and layout feasibility

### Evidence

- Task Type IDs must be unique within a Production's notification routes. Evidence: `src/model/model.go:653-667`.
- The stored channel mapping model also validates unique Task Type and Channel IDs and requires mappings to belong to the same Production and Guild. Evidence: `src/model/model.go:247-272`.
- The editor renders one table row per route, permits adding a Task Type, and does not declare a maximum route count. Evidence: `src/setup/current_routing.go:264-279`.
- The current route list is ordered by database ID. Evidence: `src/model/model.go:544-550`.

### Inference

The relationship is structurally sparse for a small Production, so a two-column map with one edge per route is technically reasonable. Density is not bounded by the UI contract, however. A map that is readable at 5–12 routes can become a wall of crossings or a vertically excessive canvas as Task Types grow. Shared destinations, missing destinations, stale webhook records, and long localized names further reduce graph readability.

The viable layout is not a free-form force graph. Use fixed semantic columns:

`Kitsu Task Types` → `Discord Channels`

Keep node order deterministic and identical to the exact list. Avoid edge animation, curved-edge decoration, auto-layout drift, and a canvas that requires panning to compare endpoints.

Density policy:

- Low density: show the map and exact list together.
- Medium density: show the map with compact nodes and retain the list immediately available.
- High density or repeated destinations: default to the list; expose the map as an explicitly requested diagnostic view.

Severity: High if a map is made the default or allowed to scale without a fallback; Low-Medium for a bounded supplementary map.
Confidence: Medium, because actual route cardinalities and repeated-destination patterns were not available.

## 3. Scope and filtering

### Evidence

- Production detail is selected by `project=<id>` and all routing reads are filtered by the selected Production ID. Evidence: `src/setup/ia_views.go:549-570`, `src/model/model.go:544-550`, and `src/setup/current_routing.go:281-289`.
- Routing validation rejects routes whose destination webhook belongs to another Production. Evidence: `src/model/model.go:668-671`.
- The current UI exposes no map-specific search, status filter, cross-Production comparison, or graph query controls. Evidence: the route/tab and routing renderers above contain no map/filter surface.

### Inference

The primary scope must be exactly one selected Production. A cross-Production map would mix similarly named Task Types and Discord channels, weaken ownership boundaries, and conflict with the Production-scoped validation model.

Required scope affordance if a map is introduced:

- persistent Production name in the map heading;
- plain-language statement that the map shows notification routing for this Production only;
- no implicit inclusion of other Productions, global User Linking, or Kitsu-wide data;
- a list fallback that preserves the same scope.

Filtering should be minimal and semantic, not a dashboard filter suite. A useful first filter is `All / Incomplete / Needs review`, provided each state has a defined backend source. Do not invent a health state from node color alone. If no reliable route-status classification exists for a filter, omit the filter and use the existing notification status plus troubleshooting details.

Severity: High for cross-Production scope leakage or ambiguous scope; Medium for missing filters at low density.
Confidence: High for the required Production scope; Medium for the proposed filter policy.

## 4. Inspector feasibility

### Evidence

- The current read-only routing summary exposes Task Type display name, Discord channel display name, route status, and an Edit link. Evidence: `src/setup/current_routing.go:281-295`.
- Advanced Production details expose Production ID, Discord server ID, and Category ID. Evidence: `src/setup/ia_views.go:711-716`.
- Troubleshooting exposes routing integrity and related diagnostic information through a separate Production tab. Evidence: `src/setup/ia_views.go:657-658` and the `renderCurrentProductionTroubleshooting` call path.
- The route model retains stable IDs and destination webhook identity, but the normal summary does not render those internal identifiers. Evidence: `src/model/model.go:511-520` and `src/setup/current_routing.go:281-295`.

### Inference

An inspector is feasible and valuable if it is a read-only details panel for the selected node or edge. It should answer “what does this connection mean and why is it safe/current?” without becoming a second admin form.

Minimum useful inspector content:

- human-readable Task Type and Discord channel names;
- route state and the reason for a warning/incomplete state, when available;
- Production context;
- a link to the existing Notifications editor for intentional changes;
- a link to troubleshooting when the issue is diagnostic rather than editable.

Do not expose webhook URLs, tokens, response bodies, or raw internal identifiers as normal map content. The current model has sensitive webhook data and stable IDs, while the existing normal UI intentionally keeps the summary human-readable.

Inspector interaction must have a non-graph fallback. Clicking an edge should not be the only way to inspect it; the exact list or an accessible focusable connection row must provide the same information.

Severity: High if the graph is not inspectable or relies on hover; Medium if it duplicates the existing details without a clear route to action.
Confidence: High for the security and fallback boundary; Medium for the exact inspector presentation.

## 5. Editing boundary

### Evidence

- The normal summary is explicitly read-only and links to Edit. Evidence: `src/setup/current_routing.go:294-295`.
- The editor stages changes in a form and applies them only on submit. Evidence: `src/setup/current_routing.go:278-279`.
- The save handler validates Production ownership, Task Type uniqueness, destination validity, and route configuration before persisting. Evidence: `src/setup/current_routing.go:21-92` and `src/model/model.go:644-673`.
- Removing a route is distinct from deleting a Discord channel. The editor copy states that route removal does not delete Task Types or Discord channels; channel deletion requires a separate confirmation dialog with exact channel-name entry. Evidence: `src/setup/current_routing.go:266-279`.
- Saving can also synchronize Discord channel order and can fail if Discord ownership or permissions cannot be verified. Evidence: `src/setup/current_routing.go:94-166`.

### Inference

The map must not support direct drag-to-connect, drag-to-reorder, inline destination mutation, or delete controls. Those interactions would collapse a safe staged workflow into an ambiguous graph gesture and would make a route edit look equivalent to a Discord resource change.

Permitted boundary:

- map selection/highlight is read-only;
- “Edit routing” opens the existing explicit editor state;
- the existing staged Apply changes flow remains the only route mutation path;
- Discord channel deletion remains outside the map and retains the exact-name confirmation boundary;
- post-save failure and rollback semantics remain owned by the existing route handler.

Severity: Critical if graph gestures can mutate routing or Discord resources; High if the map bypasses the existing staged editor.
Confidence: High.

## 6. Feasibility matrix

| Dimension | Finding | Severity if ignored | Confidence |
| --- | --- | --- | --- |
| Cognitive value | Useful for bounded route-shape comprehension and anomaly localization; weak as a general dashboard | Medium | High/Medium |
| Density | Viable for small one-hop mappings; unbounded route count requires list fallback | High | Medium |
| Scope/filtering | Must remain one selected Production; cross-Production graph is not supported by the current ownership model | High | High |
| Inspector | Feasible as read-only node/edge details with list and keyboard fallback | High | High/Medium |
| Editing boundary | Existing explicit staged editor is the correct mutation boundary; graph editing is unsafe and unnecessary | Critical | High |
| Overall feasibility | Supplementary diagnostic, not primary navigation or canonical editor | High | High |

## Untested scope and limits

The following were not validated in this audit:

- authenticated browser rendering or pixel-level layout at desktop/mobile widths;
- actual route counts, long Task Type/channel names, duplicate destinations, and incomplete/stale production data;
- keyboard, screen-reader, focus, reduced-motion, zoom, or touch behavior of a proposed map;
- operator task completion time or comprehension through user testing;
- whether a canvas/SVG library is already available or permitted by the project runtime;
- visual comparison between the map and the current exact list in Japanese and English;
- live Discord/Kitsu state, permissions, or route synchronization outcomes.

These limitations prevent a visual PASS. They do not change the structural verdict that a map can be considered only as a bounded supplement to the current Production Notifications screen.

## Final decision

Adopt only if all of the following remain true:

1. It lives under one selected Production's Notifications context.
2. It renders the single supported relationship: Kitsu Task Type → Discord Channel.
3. It preserves the exact mapping list as the canonical, accessible fallback.
4. It has deterministic ordering and an explicit density cutoff/fallback.
5. It offers read-only inspection with keyboard-equivalent access.
6. It links to the existing staged editor rather than editing in the graph.
7. It does not expose secrets, raw webhook data, or unnecessary internal IDs.
8. It is not promoted to a top-level dashboard or cross-Production network view.

If these constraints cannot be met, keep the current list/table. The current list already expresses the core relationship with lower cognitive and operational risk.

## Sources

- `src/setup/ia_views.go:549-575, 638-716, 1057-1071`
- `src/setup/current_routing.go:21-166, 250-300`
- `src/main.go:893-930`
- `src/model/model.go:211-238, 247-272, 500-520, 544-576, 644-673`
