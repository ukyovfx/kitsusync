# Discord Notification Templates

KitsuSync uses Go templates to render Discord embeds. Templates live in `tpl/` and are selected by `tplPreset` in `conf.toml`.

## Available Presets

| Preset | Directory | Description |
|--------|-----------|-------------|
| `rich` | `tpl/rich/` | Japanese-first layout with status icons and optional Drive/Kitsu links (default) |
| `eng` | `tpl/eng/` | English-only plain layout |
| `rus` | `tpl/rus/` | Russian layout (no `fields.tpl`) |

Set the active preset in `conf.toml`:

```toml
tplPreset = "rich"
```

## Template Files

Each preset directory contains up to four files. All four are required for `rich`; `eng` and `rus` use three.

| File | Discord embed field | Purpose |
|------|---------------------|---------|
| `title.tpl` | Embed title | Short identifier shown at the top of the embed |
| `description.tpl` | Embed description | Main body text |
| `author.tpl` | Author line | Task type name shown as the embed author |
| `footer.tpl` | Footer | Routing/channel name shown at the bottom |
| `fields.tpl` | Inline fields | Structured data columns (rich preset only) |

## Template Variables

These variables are available in every template file.

### Task Identity

| Variable | Type | Example | Description |
|----------|------|---------|-------------|
| `.ProjectName` | string | `"PRODUCTION-A"` | Kitsu project name |
| `.GroupName` | string | `"Shot"` | Entity type name (Shot, Asset, Sequence) |
| `.ParentName` | string | `"cut008"` | Sequence or episode name the entity belongs to |
| `.TaskName` | string | `"Compositing"` | Kitsu task name |
| `.TaskType` | string | `"Compositing"` | Kitsu task type name |
| `.EntityType` | string | `"Shot"` | Entity type (same as GroupName in most cases) |
| `.TaskURL` | string | `"https://kitsu.example.com/productions/.../shots/tasks/..."` | Verified Kitsu task deep link; empty when the entity route is not known |

### Status

| Variable | Type | Example | Description |
|----------|------|---------|-------------|
| `.CurrentStatus` | string | `"DONE"` | New status after the change |
| `.PreviousStatus` | string | `"WFA"` | Status before the change (empty on first notification) |
| `.StatusUpper` | string | `"DONE"` | Same as CurrentStatus, always uppercased |
| `.StatusEmoji` | string | `"✅"` | Emoji mapped to CurrentStatus |
| `.StatusMessage` | string | `"Approved"` | Human-readable label for CurrentStatus |
| `.StatusTransitionMessage` | string | `"WFA → DONE"` | Transition summary string |
| `.IsCommentOnly` | bool | `false` | True when status did not change (comment-only update) |

### People

| Variable | Type | Example | Description |
|----------|------|---------|-------------|
| `.CommentAuthor` | string | `"Taro Yamada"` | Name of the person who triggered the change |
| `.CommentContent` | string | `"Approved!"` | Comment text (empty when no comment was added) |
| `.Assignees` | `[]Assignee` | — | Slice of assignee structs (see below) |
| `.AssigneesStr` | string | `"Taro Yamada, Hanako"` | Comma-separated assignee names |
| `.MentionContent` | string | `"<@123456>"` | Discord mention string (empty when no mapping exists) |

**Assignee struct fields** (use with `range .Assignees`):

| Field | Type | Description |
|-------|------|-------------|
| `.Fullname` | string | Full name of the assignee |
| `.Email` | string | Kitsu email address |

### Metadata and Routing

| Variable | Type | Example | Description |
|----------|------|---------|-------------|
| `.ProcessEmoji` | string | `"🎬"` | Emoji representing the task type |
| `.GoogleDriveURL` | string | `"https://drive.google.com/..."` | Validated project-level Drive link (empty if not configured) |
| `.PreviewImageURL` | string | `"https://kitsu.example.com/..."` | Kitsu preview thumbnail URL (empty if no preview) |
| `.ChannelName` | string | `"kitsu-fx-lighting-comp"` | Discord channel name the notification was sent to |
| `.IsAssignNotification` | bool | `true` | True when this is a new task assignment (status = TODO) |

## Writing a Custom Template

1. Copy an existing preset directory:

```bash
cp -r tpl/eng tpl/studio
```

2. Edit the `.tpl` files. Standard Go template syntax applies (`{{if}}`, `{{range}}`, `{{.Field}}`).

3. Set the preset in `conf.toml`:

```toml
tplPreset = "studio"
```

4. Restart the container.

### Example: Minimal `title.tpl`

```
{{.ParentName}} / {{.TaskName}}
```

### Example: Status arrow in `description.tpl`

```
Status: {{if .PreviousStatus}}{{.PreviousStatus}} → {{end}}{{.CurrentStatus}}
```

### Example: Range over assignees in `description.tpl`

```
Assignees:
{{- range .Assignees}}
- {{.Fullname}}
{{- end}}
```

## Notes

- Templates are loaded at startup. Restart the container after editing.
- Syntax errors in templates cause the embed field to be empty (the rest of the notification still sends).
- The `fields.tpl` file must be a valid JSON array of Discord field objects:

```json
[
  { "name": "Label", "value": "Content", "inline": true }
]
```
