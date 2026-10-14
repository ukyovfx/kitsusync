# KitsuSync ↔ VFX Files Plugin integration contract v1

**Contract version:** `1.0.0`
**Status:** Draft for cross-repository implementation
**Authority:** This directory is the KitsuSync-side contract proposal. It does not claim that the separate Files Plugin implements these endpoints.

## Ownership

- Kitsu/Zou owns the canonical UUIDs, names, hierarchy, membership, Tasks, Task Types, and source metadata. Names are display snapshots only; IDs are identity.
- KitsuSync owns the desired per-Production integration policy: the opaque Drive connection ID, selected verified root ID, template selection/customization, sync mode/scope, activation state, and Discord route policy. `Project.StorageURL` remains the existing optional generic link and is not repurposed.
- The Files Plugin owns Google OAuth execution, provider credential material, Drive object/folder bindings, sync jobs and progress, Output Packages/manifests, and per-recipient delivery state. Plugin-side copies of KitsuSync desired configuration are revisioned execution snapshots; they cannot independently become the desired-state authority.
- KitsuSync owns Discord routing and delivery. The Plugin reports business events; it does not send Discord messages.
- KitsuSync remains the only user-facing configuration surface. It obtains the authoritative Kitsu/Zou object snapshot through its existing server-side Kitsu integration and sends that bounded, read-only snapshot to the Plugin before preview. The Plugin does not get Kitsu credentials and must not independently enumerate or mutate Kitsu/Zou.

## OAuth decision and Drive scope

Use the Plugin as the OAuth confidential client: KitsuSync's authenticated, CSRF-protected administrator action asks the Plugin backend to create a one-use authorization intent, then the KitsuSync server redirects the browser to the returned Google authorization URL. The Plugin owns the fixed callback URI, exchanges the code server-side, and stores refresh credentials in its protected credential store. OAuth state is single-use and bound to the initiating KitsuSync administrator, Production and authorization intent. The browser must never receive the code at a KitsuSync page, access token, refresh token, service identity, or internal `credential_ref`.

Google's web-server OAuth flow is designed for a backend that can securely store confidential information and state, and Google requires long-lived refresh tokens to be stored securely. The existing Plugin drafts do not establish the needed credential store/key lifecycle, so this is an implementation gate before real OAuth credentials are used. First evaluate the narrow `drive.file` scope with Google's Picker for user-selected roots. Do not assume it covers every existing-folder/adoption workflow; validate the exact Shared Drive and folder operations. Any broader scope requires separate scope justification and Google verification review. A saved KitsuSync `StorageURL` stays generic. A recognizable Drive folder URL may be parsed locally as an untrusted candidate ID (the contract accepts only `https://drive.google.com/drive/folders/{id}` and `/drive/u/{n}/folders/{id}` forms) without fetching the supplied URL. The Plugin verifies that ID through authenticated Drive API operations and checks Production scope before it becomes a configured root. Reject userinfo, custom ports, lookalike hosts, other paths and non-HTTPS schemes. A URL alone never authorizes access or starts folder operations.

## Authentication and secrets

KitsuSync's browser authenticates only to KitsuSync. Its same-origin authenticated administrator action starts an OAuth authorization intent through the KitsuSync backend. KitsuSync's server calls the Plugin API and receives an authorization URL; it issues a server redirect. The URL is never returned to page JavaScript. OAuth callback and code exchange terminate at the Plugin backend. The browser never receives a Google access or refresh token, Plugin service credential, credential reference, or Drive API response containing credentials.

KitsuSync-to-Plugin calls use HTTPS and a dedicated service identity with audience and operation scopes. Every request carries `X-KitsuSync-Actor-ID`, derived from the authenticated KitsuSync session by the backend; the Plugin trusts it only when the service identity is valid and records it for audit. Never copy an actor ID from browser form data. Connection/root reads also require a Production scope so the Plugin authorizes the exact Production, connection, root, actor, and config revision on every operation. Plugin-to-KitsuSync events use HTTPS request signing with a separately rotatable integration key, timestamp, nonce, and raw-body SHA-256; replay windows and event IDs are checked before enqueue. Keys are server configuration secrets, never browser settings. A Google OAuth scope alone is not treated as a Production-root boundary.

The Plugin may keep an opaque internal `credential_ref` in its database, but it is not returned to KitsuSync or accepted from the browser. The referenced credential must be stored outside source control, the database, container image, logs, and public static paths in a fixed protected server-side directory. The current referenced Draft PRs do **not** prove that this credential store, OAuth callback handoff, encryption/key management, or API/worker secret boundary is implemented. Before real credentials are used, the Plugin must implement and test: fixed-path non-symlink file access; restrictive owner/mode; atomic write/replace; authenticated encryption with a separately provisioned key; key rotation/revocation; worker access restricted to the trusted Plugin service identity; and no token in IPC/HTTP payloads, exception text, or logs. If those controls cannot be supplied by the existing Plugin host, stop and choose an approved secret manager rather than placing tokens in SQLite or KitsuSync.

OAuth state is single-use, expires, and is bound server-side to the initiating KitsuSync administrator, Production context, and Plugin authorization intent. Google callback URI and the post-consent KitsuSync return URI are fixed in Plugin deployment configuration; caller-controlled redirect URIs are rejected. The return redirect contains only an opaque authorization ID, never an authorization code or token. KitsuSync POST actions require its normal session, CSRF protection, and admin authorization. Disconnect/revoke disables future operations, revokes at Google where supported, and removes the credential only after the operation and audit record are durably committed. Failed revocation remains visible as `revoke_pending` and is not reported as disconnected.

## HTTP and event delivery semantics

- `plugin-api.openapi.json` describes KitsuSync backend → Plugin backend calls.
- `kitsusync-events.openapi.json` describes Plugin backend → KitsuSync event intake. These are separate trust directions, not one shared API surface.
- Both directions use `/v1`; requests and responses carry `contract_version: "1.0"`. Additive fields are compatible within major v1. Unknown major versions fail closed with `unsupported_contract_version`.
- Mutating commands require an `Idempotency-Key`. Repeating a key with the same canonical body returns the original operation; reusing it with a different body returns `idempotency_conflict`. The only non-mutating `POST` is the read-only sync preview operation.
- KitsuSync desired-config changes use `If-Match` with the current monotonically increasing `config_revision`. Stale writes return `409 stale_config` and must be re-read; they are never silently overwritten.
- Desired-config Apply and activation use a strong quoted revision ETag (for example `If-Match: "4"`) plus `Idempotency-Key`. Activation is a distinct explicit administrator action. It increments `notification_epoch`; events are eligible only when that positive epoch equals the current enabled desired config. Disabling/re-enabling advances the epoch so old queued events cannot become newly eligible.
- The KitsuSync backend uploads a bounded read-only Kitsu object snapshot to the Plugin in pages. The snapshot start binds Production, config revision, Kitsu source ETag, object count, page count and full snapshot SHA-256. Each page is idempotent and hash-checked. Completion seals an immutable snapshot only if every page and canonical digest matches. Preview is allowed only against a complete, unexpired snapshot and the matching config revision. Use RFC 8785 JSON Canonicalization Scheme bytes for each page's `page_sha256` and for the full ordered object-array `snapshot_sha256`; pages contain at most 500 objects and the whole snapshot at most 500,000. Objects are ordered by `(object_type, id)`, IDs must be unique, and every non-null parent must resolve within the same snapshot. Include ancestors as `included_in_sync:false` context when needed to render a selected object's path. A changed config/source ETag invalidates the preview; the Plugin does not query Kitsu itself.
- A sync Apply requires a current successful preview ID, matching Production/config revision, selected verified root, and explicit administrator action. A pasted/saved URL, root candidate, or successful OAuth alone never starts folder creation.
- Sync job creation is asynchronous (`202`); the operation ID is stable and status is read through the job resource. Retry and reconcile are distinct idempotent commands. Ambiguous provider outcomes remain `reconcile_required`; the client cannot request blind replay.
- Event intake returns `202` only after each newly accepted event is durably recorded. Per-event outcomes distinguish `accepted`, `duplicate`, and `quarantined`; invalid batch structure is rejected at HTTP level. A duplicate `(producer_id,event_id)` with identical body is idempotent; same identity with different body is a security-visible conflict and is never sent.
- HMAC is calculated in constant time over `timestamp + LF + nonce + LF + lowercase raw-body SHA-256 + LF + Idempotency-Key`; the key ID selects a Plugin integration key that is bound to the producer identity. Enforce a short timestamp window and durable nonce uniqueness. The body hash covers the exact raw UTF-8 request bytes. No browser receives this integration key.
- Every post-activation business event carries `activation_epoch`. KitsuSync compares it with the current enabled Production config before journaling; mismatches and bootstrap discovery are quarantined/suppressed. The Plugin must not emit pre-activation history as new delivery events.
- `delivery_id` is stable for one recipient Task; each recipient gets its own delivery ID and its own monotonically increasing `delivery_revision`. FX and Layout deliveries therefore progress independently. An old/equal revision is not re-applied. Each transition event has a distinct event ID.
- KitsuSync routes only by exact `production_id + recipient_task_type_id` and existing Discord route IDs. Missing/ambiguous route is quarantined with a sanitized diagnostic; no fallback to names or another Production. File-delivery status never changes Kitsu Task WFA/RETAKE/DONE state.
- Initial sync/import is notification-suppressed. Only business transitions created after explicit activation (or an explicitly enabled post-activation setting) are eligible for Discord delivery. Historical discovery never generates a burst of new-delivery messages.
- Event payloads contain IDs and bounded display metadata, never OAuth credentials, webhook URLs/tokens, cookies, public-sharing links, or arbitrary destination URLs. KitsuSync builds Kitsu links from its configured trusted origin and stable IDs. Drive links are emitted only after server-side access-policy checks; public sharing is not part of this contract. Discord content is escaped and uses the existing restricted `allowed_mentions` policy; default is no mentions.
- Event rows and resulting Discord deliveries are durable and independently deduplicated. An HTTP acknowledgment is not a Discord-send acknowledgment. KitsuSync reports its own delivery journal state; it never claims recipient receipt/device push.

## Identity and payload rules

Kitsu identifiers are UUID strings: `production_id`, `episode_id`, `sequence_id`, `entity_id` (`shot` or `asset`), source/recipient `task_id` and `task_type_id`, and `person_id`. Snapshot pages include Task Type metadata rows so every Task's `task_type_id` resolves to a stable Task Type ID. `task_type_name`, entity names/codes, and person display names are descriptive snapshots only. A Task Type rename does not alter route resolution.

Output Packages have a Plugin-owned stable `output_package_id`, positive monotone `version`, `classification`, `manifest_sha256`, bounded manifest reference, and zero or more Drive object IDs with content SHA-256 and byte count. Optional frame range has integer start/end/step and an explicit rate numerator/denominator. Never accept a path, local file name, or URL as object identity. Sender is a Kitsu `person_id` with a display snapshot. A single distribution to multiple Tasks is represented by separate delivery events, not one shared mutable recipient state.

## First implementation flow

1. KitsuSync validates the administrator session/CSRF, checks admin access to the selected Production, and calls `POST /drive-connections/authorizations` with `Idempotency-Key` and `X-KitsuSync-Actor-ID`. It redirects the browser to the returned URL; the URL is not passed to page JavaScript. The Plugin finishes OAuth and redirects only to its configured KitsuSync return URI.
2. KitsuSync polls `GET /drive-connections/{connection_id}` for a sanitized state, lists scoped folders with `GET /drive-connections/{connection_id}/roots?production_id=...`, and posts `RootVerificationRequest` to verify the selected ID or allowlisted Drive folder URL. No writes occur during root verification.
3. KitsuSync reads templates and saves a desired-config update with the strong `If-Match` ETag and idempotency key. The update schema deliberately excludes activation.
4. KitsuSync uploads the scope-filtered hierarchy snapshot using snapshot start, page PUTs, and complete; it then requests read-only preview. KitsuSync renders the Plugin's bounded diff/warnings and does not infer success from a saved URL.
5. Only a separate administrator Apply submits the successful preview to create a sync job. KitsuSync polls job state; a retry is explicit and idempotent, while an ambiguous provider result requires reconcile.
6. After setup/sync is reviewed, a separate explicit activation call turns on new business-event intake and starts a new notification epoch. Deactivation is also explicit and advances the epoch. Plugin-sent events are then durably accepted/quarantined by KitsuSync and independently delivered through the existing notification journal.

## Template materialization semantics

The Plugin template catalog response includes a required `materialization` descriptor (`schema_revision: 1`) for every template. KitsuSync stores and sends only the selected catalog `scope`, `id`, `revision`, and `overrides`; the Plugin resolves those exact immutable catalog IDs and revisions into its execution tree. KitsuSync must not synthesize a folder tree or silently drop an override. An unknown scope/ID/revision, unsupported override key, or unsafe folder component is rejected before the desired configuration is committed.

For the v1 default profile, `root_binding` is exactly `verified_root_is_production_root`: the selected, successfully verified Drive root **is the Production folder**. The Plugin never creates or adopts a second folder named after the Production. All static template folders are children of that verified root. A root must be Production-scoped, belong to the selected StorageConnection, and have provider-confirmed child-create permission before Activation. A future “root is parent of Production folder” mode requires a distinct contract value and must not be inferred from a URL or name.

The default materialization is deterministic and folder-only: `00_Production`, `01_Shared_Assets`, `02_Shots`, `03_Editorial`, and `04_Delivery` are stable template-ID children of the Production root. Selected Asset Tasks resolve under `01_Shared_Assets`; selected Shot Tasks resolve under `02_Shots`, optionally through Episode and Sequence when their scope and resolved Kitsu hierarchy include them. Both Asset and Shot branches may be selected together. Each branch resolves its own Task-role node; Working, Publish, Shared Outputs, and Final Delivery are nested beneath the Task. Dynamic folders use Kitsu UUIDs for binding identity. Names are display snapshots and must never be used to adopt or reconnect provider folders.

KitsuSync may save a verified connection and root with every hierarchy scope disabled while setup is incomplete. That configuration cannot be previewed or activated. Any enabled hierarchy sync requires Task synchronization plus at least one matching Shot or Asset template and scope; Sequence requires Shot, and Episode requires Sequence. Every enabled entity kind must have its matching selected template. This validation is enforced by the Plugin when desired configuration is written and is repeated before Preview, Apply, activation, and Worker execution.

`overrides` uses only the keys advertised by that catalog entry's `materialization.available_overrides`. Values replace one folder component; they cannot change role, parent, root, or stable identity. The Plugin rejects path separators, dot segments, control characters, undeclared placeholders, and values that exceed the contract limit. Catalog revision changes are explicit and invalidate stale Preview/Apply work.

On a successful desired-config write the Plugin atomically stores its derived `SharedTemplateConfiguration` execution snapshot with the same Production, connection, verified root and `config_revision`. It remains disabled unless the separately fenced Activation is enabled. Preview, Apply and the Worker all use that persisted snapshot and recheck configuration revision, activation epoch, connection, root, template version and current Kitsu authorization. Existing reserved provider IDs may be reconciled by exact identity; an unbound same-name folder is never adopted. v1 currently permits only `new_production`; `future_only` and `adopt_existing` remain rejected until their distinct boundary/proof semantics are specified and tested.

See `fixtures/desired-config-adopt-existing.json`, `fixtures/sync-preview.json`, `fixtures/sync-snapshot-start.json`, `fixtures/sync-snapshot-page-0.json`, and the event fixtures for synthetic payload examples. KitsuSync's event endpoint acknowledges durable event intake only; delivery success is read from its own delivery journal.

## Required state vocabulary

- Connection: `not_configured`, `authorizing`, `connected`, `permission_denied`, `expired`, `revoking`, `revoke_pending`, `error`.
- Root verification: `unverified`, `verified`, `permission_denied`, `not_found`, `wrong_drive`, `error`.
- Sync job: `preview_ready`, `queued`, `running`, `completed`, `failed`, `reconcile_required`, `cancelled`.
- Per-recipient delivery: `created`, `ready`, `accepted`, `needs_fix`, `revised`, plus `blocked_no_route` and `quarantined` on the KitsuSync side.
- Errors are stable machine codes with localized client text; raw provider responses and secrets are not returned.

## Files

- `plugin-api.openapi.json`: Plugin API operations called only by KitsuSync's server.
- `kitsusync-events.openapi.json`: signed event intake operations exposed by KitsuSync.
- `schemas/`: JSON Schema Draft 2020-12 desired-config, sync-preview and event structures; request/response DTOs for the Plugin API are defined in its OpenAPI components.
- `fixtures/`: synthetic, non-secret examples for contract tests and a future Fake adapter.
- `../../../../tools/contracts/vfx_files_contract_test.go`: local fixture/schema regression test.

All IDs, names, Drive references, hashes, and links in fixtures are synthetic. Snapshot-page SHA values are computed over canonical JSON as defined above. No Google credentials, real Kitsu IDs, Discord IDs, webhook URLs, or Production URLs are included.

## Cross-repository handoff boundary

The Files Plugin accepted `main` is currently design/documentation-only. Plugin PRs #1/#2/#4/#7/#8 are Draft and are not accepted implementations. PR #1's candidate has a `credential_ref` database column and an injected ephemeral token-provider interface, but that does not prove a protected credential directory or OAuth implementation. PR #8 describes KitsuSync/Drive connectors as contracts/Fakes pending credential approval. The Plugin owner must implement the Plugin API, OAuth callback/vault controls, desired-config mirror revision checks, event emission/outbox, and provider fake against these files before either side enables external operations. Do not infer completion from a file existing in this repository.

## External references and approval boundary

- [Google OAuth web-server flow](https://developers.google.com/identity/protocols/oauth2/web-server) — server-side confidential-client model and callback behavior.
- [Google Drive API scopes](https://developers.google.com/workspace/drive/api/guides/api-specific-auth) — scope selection, `drive.file` and Picker guidance, and secure refresh-token storage.
- [Google OAuth security best practices](https://developers.google.com/identity/protocols/oauth2/resources/best-practices) — credential storage and revocation guidance.

Decision: do not pass Google tokens to KitsuSync or the browser. The Plugin is the one OAuth confidential client. If its existing host cannot provide an encrypted, access-controlled credential store with a separately provisioned key, the integration remains disabled until an approved secret manager is available. That is a security gate, not a reason to copy credentials into KitsuSync SQLite.
