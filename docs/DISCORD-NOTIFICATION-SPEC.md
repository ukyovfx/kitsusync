# KitsuSync Discord Notification Specification

This document describes the current notification path and its safety contract. It is intentionally limited to behavior supported by the repository today.

## Delivery path

```text
Kitsu read-only poll
  -> MessagePayload normalization
  -> FilterTasks event policy
  -> Production ID + stable Task Type ID route lookup
  -> ProductionNotificationConfig / ProductionNotificationRoute
  -> ProjectWebhook destination
  -> deterministic JP/EN payload renderer
  -> Discord webhook delivery
  -> AuditLog and Task delivery state
```

`FilterTasks` is the only current task-notification entry point. An event is fail-closed when the Production is not connected, the notification config is absent/disabled, the stable Task Type ID has no route, or the route points to a missing/incomplete ProjectWebhook. No name-only or global fallback is used by the current router.

## Supported event inventory

The current polling implementation supports these normalized event kinds:

| Event kind | Current trigger | Delivery policy |
| --- | --- | --- |
| `status_change` | A task reaches `WFA`, `RETAKE`, or `DONE`, and its stored status differs | Delivered through the Production + Task Type route |
| `comment_update` | A task's latest comment/timestamp changes while the task remains in a notifiable status | Delivered through the same route; the payload marks it as comment-only |
| `assignment` | A task is observed with status `none` and `notification.notifyOnAssign=true` | Delivered through the same route; otherwise the observation is stored without delivery |

Other Kitsu task statuses are observed but are not notification events in the current policy. Unknown event shapes are not rendered or sent.

## Language

Notification language is a Production-level setting stored in `Project.Language`. `en` selects English; every other value currently fails closed to Japanese. The administrator's page language does not affect notification language. Missing or unsupported values therefore have deterministic Japanese output.

The pure renderer is `src/api/discord/notification_foundation.go`. `RenderNotificationPayload` is used by the delivery path and can be tested without a Discord request. Template files remain the content source for the existing `rich` and `eng` presets.

## Mentions

- Only explicitly resolved Discord user IDs are placed in `AllowedMentions.Users`.
- IDs are deduplicated before payload construction.
- Missing Kitsu-to-Discord links leave the notification deliverable and simply produce no user mention.
- Kitsu assignees with `is_bot=true` are excluded from artist mentions.
- `@here` and `@everyone` are never emitted by KitsuSync, regardless of legacy configuration values.
- Kitsu-originated text is sanitized before it is used in Discord content/templates.
- User Linking uses the assignable-person filter, which excludes Kitsu Bot identities from normal human linking.

## Routing and missing resources

Routing identity is `ProductionID + TaskTypeID`. Display names are metadata only. Archived/stale Task Types, missing routes, missing channels, incomplete webhook rows, and ambiguous destinations fail closed and are recorded as routing diagnoses. A notification is not sent to a guessed or legacy destination.

## Idempotency and audit

The `Task` row stores the last observed task/comment state and the delivered Discord message/thread identifiers. An unchanged observation is skipped. A failed or unknown delivery does not claim success and preserves the previous delivered state; it can be retried on a later poll. Successful delivery writes a sanitized `AuditLog` entry and updates the task delivery state. Webhook URLs are removed before audit persistence.

## Links and rendering

Kitsu task links are constructed only when the configured Kitsu host and stable Production/task IDs are available. Optional storage links are included only when the configured resolver returns one. Missing links are omitted or reported as unavailable; no URL is guessed from a display name.

The renderer is deterministic for the same normalized data, language, preset, and template files. It does not perform network access. Discord delivery remains in `SendMessage`/`SendMessageBunch` and is not exercised by rendering tests.

## External-write boundary

Polling reads Kitsu. Notification delivery is the only Discord message write in this path. This specification and its renderer tests do not send Discord messages, create channels/webhooks, modify Kitsu, or modify Production routing state.
