package discord

import (
	"encoding/json"
	"strings"

	"app/src/utils/truncate"
)

// NotificationEventKind is the bounded set of events emitted by the current
// polling pipeline. Unknown values are never rendered as notifications.
type NotificationEventKind string

const (
	NotificationEventStatusChange  NotificationEventKind = "status_change"
	NotificationEventCommentUpdate NotificationEventKind = "comment_update"
	NotificationEventAssignment    NotificationEventKind = "assignment"
)

// SupportedNotificationEvents is intentionally small and derived from the
// current FilterTasks policy. Keep this list in sync with isNotifiableStatus.
func SupportedNotificationEvents() []NotificationEventKind {
	return []NotificationEventKind{
		NotificationEventStatusChange,
		NotificationEventCommentUpdate,
		NotificationEventAssignment,
	}
}

// NotificationLanguage normalizes the Production-level language setting.
// Admin UI language is deliberately not an input to this function.
func NotificationLanguage(projectLanguage string) string {
	if strings.EqualFold(strings.TrimSpace(projectLanguage), "en") {
		return "en"
	}
	return "ja"
}

// RenderNotificationPayload is the network-free rendering boundary used by
// delivery. Template data is already normalized and secret-safe by the caller;
// this function only reads local template files and builds a Discord payload.
func RenderNotificationPayload(data Template, preset string) Payload {
	preset = localizedTemplatePreset(preset, data.NotificationLanguage)
	author := parseTaskTemplate("tpl/"+preset+"/author.tpl", data)
	title := parseTaskTemplate("tpl/"+preset+"/title.tpl", data)
	description := parseTaskTemplate("tpl/"+preset+"/description.tpl", data)
	footer := parseTaskTemplate("tpl/"+preset+"/footer.tpl", data)

	embed := Embed{
		Title:       truncate.TruncateString(title, 256),
		Description: truncate.TruncateString(description, 4096),
		Color:       data.Color,
		Url:         truncate.TruncateString(data.TaskURL, 2000),
		Author:      EmbedAuthor{Name: truncate.TruncateString(author, 256)},
		Footer:      EmbedFooter{Text: truncate.TruncateString(footer, 2048)},
	}
	if data.PreviewImageURL != "" {
		embed.Image = &EmbedImage{URL: data.PreviewImageURL}
	}
	if fieldsRaw := parseTaskTemplate("tpl/"+preset+"/fields.tpl", data); strings.TrimSpace(fieldsRaw) != "" {
		var fields []EmbedField
		if json.Unmarshal([]byte(fieldsRaw), &fields) == nil {
			for i := range fields {
				fields[i].Name = truncate.TruncateString(fields[i].Name, 256)
				fields[i].Value = truncate.TruncateString(fields[i].Value, 1024)
			}
			embed.Fields = fields
		}
	}

	return Payload{
		Content: data.MentionContent,
		Embeds:  []Embed{embed},
		AllowedMentions: &AllowedMentions{
			Users: uniqueDiscordIDs(data.AllowedUserIDs),
		},
	}
}
