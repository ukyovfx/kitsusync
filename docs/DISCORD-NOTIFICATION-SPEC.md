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

New Productions choose this setting in the channel-plan step of the setup wizard. Existing Productions can change it from the notification settings section. A change applies to future notifications and does not change the admin UI language.

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

Kitsu links are included only when an authoritative, reliable task/entity URL is supplied by the notification data. KitsuSync does not construct a task link from a host plus IDs or display names. Optional Drive links are included only when the configured resolver returns a valid URL. Missing or invalid links are omitted; no URL is guessed.

The renderer is deterministic for the same normalized data, language, preset, and template files. It does not perform network access. Discord delivery remains in `SendMessage`/`SendMessageBunch` and is not exercised by rendering tests.

## Final notification card rules

These rules apply to normal `WFA`, `RETAKE`, and `DONE` cards. Assignment
notifications remain outside this redesign.

The card hierarchy is:

1. Task Type as plain text
2. Entity / Task name
3. Status icon and current status (`👀 WFA`, `🔄 RETAKE`, or `✅ DONE`)
4. Transition-aware short message
5. Previous → current, only for a real status transition
6. Latest comment, when present
7. Comment author, when present
8. Assignee state
9. Individually available action links (`Kitsu`, `Google Drive`)
10. Preview image, when available

The current status must not be repeated in a lower metadata field. In
particular, the compact metadata block must not contain a redundant
`Status` / `ステータス` field. Production is secondary metadata only when it
adds useful context; assignee information remains because it is not otherwise
duplicated.

The embed accent/left border is the current Kitsu Task Status `color`. Valid
Kitsu colors are never replaced with hard-coded WFA, RETAKE, or DONE colors.
Missing, empty, malformed, or unsafe colors use the defined neutral KitsuSync
fallback color. The status icon, short name, transition message, and Kitsu
color together provide the primary state cue.

Task Type names remain plain text. Process or decorative emoji must not be
placed before a Task Type name.

### Final embed schema

The rendered payload has one compact embed plus optional message content for
explicit user mentions:

```text
Payload
  content: "<@user-id> ..." or empty
  allowed_mentions.users: exactly the resolved recipient IDs, deduplicated
  allowed_mentions.parse: empty
  allowed_mentions.roles: empty
  embeds[0]
    title: entity/task title
    description: status line and action message
    color: current Kitsu Task Status color or neutral fallback
    url: reliable Kitsu link, when supplied
    fields:
      Comment: latest comment and author, when available
      Links: Drive/Kitsu action links, when available
      Status: current or previous → current status
      Assignee: human-readable assignee state
    image: preview image, only when available
    footer: secondary Production/channel context, when useful
```

There is no `Status` / `ステータス` field in the compact metadata block.
The status is represented only by the status line, icon, short name, and
embed color. Empty optional fields are omitted rather than rendered as
placeholders.

The message content is the only place where mentions are emitted. The embed
shows names and assignment information, never raw mention markup.

### Final JP examples

```text
Compositing

Shot / SC02 - cut009

🔄 RETAKE
レビュー結果により修正が必要です

WFA → RETAKE

コメント
キャラクターに入る赤みをもう少し抑えてください
— コメント投稿者

担当
UKYO M

リンク
Kitsu · Google Drive
```

```text
Animation

Asset / Character

👀 WFA
チェックをお願いします

担当
未割り当て
```

```text
Compositing

Shot / SC02 - cut009

✅ DONE
レビューが完了しました

WFA → DONE
```

Comment-only updates keep the current status line and omit a transition line:

```text
🔄 RETAKE
修正内容が更新されました

コメント
この部分を再調整してください
```

### Final EN examples

```text
Compositing

Shot / SC02 - cut009

🔄 RETAKE
A revision is required after review

WFA → RETAKE

Comment
Please reduce the red tint
— Comment by

Assignee
UKYO M

Links
Kitsu · Google Drive
```

```text
Animation

Asset / Character

👀 WFA
Please review

Assignee
Unassigned
```

```text
Compositing

Shot / SC02 - cut009

✅ DONE
Review completed

WFA → DONE
```

For a comment-only update:

```text
🔄 RETAKE
Revision details were updated

Comment
Please adjust this area again
```

Mention examples:

```text
WFA:     <@checker-id>          + embed with Assignee: UKYO M
RETAKE:  <@artist-a> <@artist-b> + embed with both assignee names
DONE:    empty content by default + embed only
missing mapping: empty content   + embed with Kitsu assignee name
```

When a DONE notification has an explicitly configured operational recipient,
the same payload may contain one deduplicated user mention. A comment-only
WFA update follows the WFA recipient rule; a comment-only RETAKE update
follows the RETAKE assignee rule. A comment-only DONE update is Embed-only by
default.

## External-write boundary

Polling reads Kitsu. Notification delivery is the only Discord message write in this path. This specification and its renderer tests do not send Discord messages, create channels/webhooks, modify Kitsu, or modify Production routing state.

## Release readiness boundary

`/health` reports `overall_notification_readiness: "ready"` only when Kitsu is runtime-ready, the Discord Bot is configured and API-validated, and at least one Production routing configuration is valid. A configured Bot or connected Production alone remains a blocked or pending state.
