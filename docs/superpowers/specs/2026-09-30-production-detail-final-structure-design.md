# Production Detail Final Structure Design

## Goal

Adopt the approved Gemini Production Detail prototype's information architecture and interaction flow in KitsuSync while retaining the accepted KitsuSync visual system and current Production, routing, WFA, Team, settings, and global User Linking semantics.

## Authority and scope

- Accepted implementation base: `f57409c2858afc666146ab5758965c7258154c5a`.
- The final prototype at `C:\AI-Workspace\artifacts\kitsusync-ui-reference\production-detail-final\kitsusync_production_detail_prototype.html` is authoritative for the four-tab structure, section order, table/list shape, interaction flow, and visible/hidden priority.
- `docs/CURRENT-IA-UI-SPEC.md` remains authoritative for KitsuSync status meaning, behavior, routes, and safety semantics. This feature updates its Production Detail presentation/interaction clauses only when implementation is accepted.
- `docs/superpowers/specs/2026-09-29-kitsu-ui-baseline-design.md` and the existing accepted KitsuSync UI remain authoritative for all visual styling.
- Scope is the connected Production Detail screen and the minimal handler/model glue needed for its unified edit and Team linking interactions. No whole-app redesign, unrelated setup/auth change, deployment/release change, or Production operation is included.

## Visual inheritance

Keep the accepted KitsuSync shell and authenticated dot-grid unchanged. Reuse its dark surfaces, current type hierarchy, spacing rhythm, burnt-orange active/primary emphasis, semantic green/yellow/red, border treatment, radius family, controls, buttons, and responsive patterns. Do not copy Gemini colors, fonts, gradients, shadows, glow, radii, or arbitrary spacing. The four tabs remain plain `Overview / Notifications / Team / Settings` labels on the current shared hairline with the current active underline.

## Production Detail structure

### Header and tabs

Preserve the existing global shell. Render one compact Production identity header with eyebrow, Production name, and the existing semantic connection badge. The primary navigation contains exactly four tabs: Overview, Notifications, Team, Settings. Existing legacy query routes continue to map to their corresponding section and focus/open the intended subsection; no fifth Reviewers tab is introduced.

### Overview

Show connection status, then Current Issues only when unresolved issues exist, then Recent Activity only when relevant exact-Production audit records exist. Do not render an empty/healthy issues card. Aggregate repeated causes, render at most three issue rows, and summarize additional rows as localized “他 N 件” / equivalent. Each issue CTA remains in the same tab set and navigates in the current window: routing/WFA/destination to Notifications, user-link/member to Team, and connection/diagnostic/resource to Settings. An unlinked ordinary Artist is not an issue unless the missing link blocks an actual KitsuSync function such as WFA delivery.

Recent Activity is exact-Production only, newest first, at most five rows, and has no arbitrary age cutoff. Include important configuration/operational changes already represented by audit records; exclude routine task updates, successful notifications, polling, health checks, page views, and background refreshes. Hide the section if no qualifying records exist. The global Audit Log remains the detailed history.

### Notifications view

Use a compact table with `Task Type | Discord Channel | WFA recipients`. Each WFA cell is the effective summary only, combining read-only automatic Kitsu Department Supervisor recipients and persisted additional User/Role targets. There is no separate large WFA editor in view mode and no Notification Preview.

### Notifications edit

One page-global Edit mode contains a selectable routing table and one WFA detail panel following the selected surviving Task Type row. Selection uses a subtle selected surface and thin KitsuSync orange marker. The panel has no separate Task Type selector. Automatic recipients remain informational and cannot be edited, removed, toggled, or overridden. Additional Discord User/Role targets are editable through a compact Add control and small modal/popover; no permanently visible selector fields.

Route additions use only current Kitsu Production Task Types not already routed and verified Production-owned Discord Channels. Route changes, additions, and removals remain browser-side pending until one global Apply or Cancel. An omitted Task Type from the submitted complete routing snapshot means remove its KitsuSync route; it never mutates Kitsu. A pending removal marks its row, disables its WFA edits, and offers Undo. Because the existing canonical route validator requires at least one destination, the final remaining route cannot be removed; the UI must make this constraint clear and server validation remains authoritative. Deleting a route also deletes only that Task Type's persisted additional WFA User/Role targets in the same database update.

## Apply request contract

The browser keeps a baseline plus pending deltas; it does not create server-side draft rows or mutate data while editing. One Apply POST carries:

```json
{
  "project_id": "<Kitsu Production ID>",
  "expected_revision": "<opaque SHA-256 of canonical current route + additional-target state>",
  "routes": [
    {"task_type_id": "<stable Kitsu Task Type ID>", "destination_webhook_id": 123}
  ],
  "reviewer_changes": [
    {
      "task_type_id": "<stable Kitsu Task Type ID>",
      "add_user_ids": ["<Discord user ID>"],
      "remove_user_ids": ["<Discord user ID>"],
      "add_role_ids": ["<Discord role ID>"],
      "remove_role_ids": ["<Discord role ID>"]
    }
  ]
}
```

`routes` is the complete desired ordered route snapshot. Comparing it to the server's current route snapshot represents additions, channel changes, removals, and retained ordering. The four reviewer ID arrays are deltas for additional targets only; automatic recipients are never serialized as writable state. Changes for a route removed in the same request are ignored/rejected as appropriate, and the server removes all persisted additional targets for that removed Task Type. The server resolves stable Task Type IDs and destination ownership against current KitsuSync/Discord state; client labels and names are not trusted identities.

## Validation and atomicity boundary

Before writing, validate the connected/writable Production; exact current Kitsu Task Type IDs; at least one route; unique Task Type routes; every destination webhook's Production ownership; live managed-Guild channel/category ownership; and every added User/Role target using the existing fail-closed rules. User targets must still resolve through global User Linking to a current eligible human Production Team member in the linked Guild. Role targets must still belong to that Guild, be mentionable, and not be `@everyone`. Removal deltas may remove only existing additional targets for the stated Task Type. Automatic Supervisor data is never persisted.

The database transaction is the atomicity boundary for the notification config, ordered route rows, additional Reviewer target changes, and deletion of targets attached to removed routes. A failure in that transaction changes none of those rows. No Kitsu Task Type, Kitsu team membership, or unrelated Production mapping is written.

Discord channel ordering is an external side effect and cannot join the SQLite transaction. First perform read-only Discord permission/ownership preflight and capture the existing relevant order. Then commit the validated database transaction and apply the existing managed-channel ordering operation. If ordering fails, restore the previous route/reviewer database snapshot transactionally and attempt a compensating Discord reorder to the captured prior order. Report a failed compensation as an explicit recovery/diagnostic error; never report success or silently claim remote state was restored. Do not delete Discord channels as part of route removal; channel deletion remains its separate existing confirmed destructive operation.

## Stale/concurrent edit handling

There is no existing optimistic revision mechanism covering both routes and reviewer targets. Use a deterministic revision hash over the canonical ordered route snapshot plus sorted additional User/Role targets for the Production. Render it with the editor; compare it again inside the database transaction before any write. A mismatch returns HTTP 409 with a localized stale-edit message, no writes, and the browser retains its pending values for review/reload. It must not silently overwrite concurrent changes. A transaction/SQLite conflict also fails closed with no success response.

## Team linking modal

Team remains a fresh, read-only live Kitsu Production Team with read-only Kitsu fields. An unlinked person may open an in-place modal containing that Kitsu identity and eligible Discord users from the Production's linked Guild. Saving revalidates the person against the live Team and the selected Discord identity against the current directory, then calls the same global `UserMap` validation/persistence path as `/bot/admin/users`. It does not create `ProjectUserMap` or any Production-local mapping. After save, the Team and global User Linking views read the same record, and other Productions containing that person resolve the same global link. Cancel has no write.

## Settings

Keep Storage, Technical details, Diagnostics, Danger Zone in that order, as vertical sections using existing KitsuSync treatment. Technical details and Diagnostics retain their current read-only/collapsed safety behavior; Storage save behavior, copy controls, and Danger Zone confirmations remain unchanged. Only destructive controls use destructive red treatment. All four tabs remain safe at mobile width without page-level horizontal overflow.

## Compatibility and non-goals

- Preserve WFA delivery precedence, automatic eligibility, additive User/Role semantics, exact allowed mentions, and route identity.
- Preserve no-preview behavior and current notification/channel safety.
- Preserve live Team semantics, stable Task Type IDs, global User Linking, legacy tab mappings, and all current save/confirmation safeguards.
- Do not add schema, Kitsu writes, Production-local user mapping, new notification recipient sources, route-management Discord channel deletion, or Production deploy/release work.

## Acceptance

Focused automated tests cover four tabs/legacy deep links; Overview issue aggregation/navigation and exact-Production activity; Notifications read summaries, selected-row/WFA linkage, read-only automatic recipients, pending add/change/remove/undo, combined Apply/Cancel, stale revision rejection, rollback, and route-removal target cleanup; Team global-link modal validation/persistence/reflection; Settings section order and existing safety; JP/EN; keyboard/focus; and mobile layout. Repository Chromium/Playwright acceptance covers JP/EN desktop/mobile, no overflow/mojibake/console errors, tab and selection semantics, Notifications view/edit behavior and pending state, Team link modal, Settings disclosures, and unchanged visual system. Final runtime acceptance uses isolated Staging only; Production is not deployed.
