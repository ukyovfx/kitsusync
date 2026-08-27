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
	if !data.IsAssignNotification && (preset == "rich" || preset == "eng") {
		title = notificationCardTitle(data)
		author = strings.TrimSpace(data.TaskType)
		if strings.TrimSpace(data.CardContext) != "" {
			footer = strings.TrimSpace(data.CardContext)
		}
		embed.Title = truncate.TruncateString(title, 256)
		embed.Author = EmbedAuthor{Name: truncate.TruncateString(author, 256)}
		embed.Footer = EmbedFooter{Text: truncate.TruncateString(footer, 2048)}
		embed.Description = truncate.TruncateString(notificationCardDescription(data), 4096)
		embed.Fields = notificationCardFields(data)
	} else if fieldsRaw := parseTaskTemplate("tpl/"+preset+"/fields.tpl", data); strings.TrimSpace(fieldsRaw) != "" {
		var fields []EmbedField
		if json.Unmarshal([]byte(fieldsRaw), &fields) == nil {
			for i := range fields {
				fields[i].Name = truncate.TruncateString(fields[i].Name, 256)
				fields[i].Value = truncate.TruncateString(fields[i].Value, 1024)
			}
			embed.Fields = fields
		}
	}
	fitEmbedTextLimits(&embed)

	return Payload{
		Content: data.MentionContent,
		Embeds:  []Embed{embed},
		AllowedMentions: &AllowedMentions{
			Users: uniqueDiscordIDs(data.AllowedUserIDs),
		},
	}
}

func notificationCardDescription(data Template) string {
	status := strings.TrimSpace(strings.Join([]string{data.StatusEmoji, data.StatusUpper}, " "))
	message := strings.TrimSpace(data.StatusMessage)
	if data.IsCommentOnly && strings.TrimSpace(data.CommentOnlyMessage) != "" {
		message = strings.TrimSpace(data.CommentOnlyMessage)
	}
	parts := make([]string, 0, 2)
	if status != "" {
		parts = append(parts, status)
	}
	if message != "" {
		parts = append(parts, message)
	}
	return strings.Join(parts, "\n\n")
}

func notificationCardTitle(data Template) string {
	entityType := strings.TrimSpace(data.EntityType)
	if entityType == "" {
		entityType = strings.TrimSpace(data.GroupName)
	}
	parent := strings.TrimSpace(data.ParentName)
	task := strings.TrimSpace(data.TaskName)
	switch {
	case entityType != "" && parent != "" && task != "":
		return entityType + " / " + parent + " - " + task
	case entityType != "" && task != "":
		return entityType + " / " + task
	case parent != "" && task != "":
		return parent + " / " + task
	case entityType != "":
		return entityType
	case parent != "":
		return parent
	default:
		return task
	}
}

func notificationCardFields(data Template) []EmbedField {
	fields := make([]EmbedField, 0, 4)
	if strings.TrimSpace(data.CommentContent) != "" {
		comment := "> " + strings.TrimSpace(data.CommentContent)
		if author := strings.TrimSpace(data.CommentAuthor); author != "" {
			comment += "\n— " + author
		}
		fields = append(fields, EmbedField{
			Name:   truncate.TruncateString(data.CommentLabel, 256),
			Value:  truncate.TruncateString(comment, 1024),
			Inline: false,
		})
	}
	links := make([]string, 0, 2)
	if driveURL := safeNotificationURL(data.GoogleDriveURL); driveURL != "" {
		links = append(links, "📁 [Drive]("+driveURL+")")
	}
	if kitsuURL := safeNotificationURL(data.TaskURL); kitsuURL != "" {
		links = append(links, "🦊 [Kitsu]("+kitsuURL+")")
	}
	if len(links) > 0 {
		fields = append(fields, EmbedField{Name: "🔗", Value: truncate.TruncateString(strings.Join(links, " · "), 1024), Inline: false})
	}
	metadata := make([]EmbedField, 0, 2)
	statusLabel := "📊 Status"
	if normalizeNotificationLang(data.NotificationLanguage) == "ja" {
		statusLabel = "📊 ステータス"
	}
	statusValue := notificationCardStatusValue(data)
	metadata = append(metadata, EmbedField{Name: truncate.TruncateString(statusLabel, 256), Value: truncate.TruncateString(statusValue, 1024), Inline: true})
	assigneeLabel := strings.TrimSpace(data.AssigneeLabel)
	if assigneeLabel == "" {
		assigneeLabel = "Assignee"
	}
	metadata = append(metadata, EmbedField{Name: truncate.TruncateString("👤 "+assigneeLabel, 256), Value: truncate.TruncateString(data.AssigneesStr, 1024), Inline: true})
	fields = append(fields, metadata...)
	return fields
}

func notificationCardStatusValue(data Template) string {
	current := strings.ToLower(strings.TrimSpace(data.StatusUpper))
	if current == "" {
		current = strings.ToLower(strings.TrimSpace(data.CurrentStatus))
	}
	previous := strings.ToLower(strings.TrimSpace(data.PreviousStatus))
	if data.IsCommentOnly || previous == "" || previous == current {
		return current
	}
	return previous + " → " + current
}

const maxDiscordEmbedText = 6000

func truncateNotificationText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func embedTextLength(embed Embed) int {
	length := len([]rune(embed.Title)) + len([]rune(embed.Description)) + len([]rune(embed.Footer.Text)) + len([]rune(embed.Author.Name))
	for _, field := range embed.Fields {
		length += len([]rune(field.Name)) + len([]rune(field.Value))
	}
	return length
}

func fitEmbedTextLimits(embed *Embed) {
	if embed == nil {
		return
	}
	over := embedTextLength(*embed) - maxDiscordEmbedText
	if over <= 0 {
		return
	}
	if descriptionLength := len([]rune(embed.Description)); descriptionLength > 0 {
		embed.Description = truncateNotificationText(embed.Description, maxInt(0, descriptionLength-over))
		over = embedTextLength(*embed) - maxDiscordEmbedText
	}
	for over > 0 && len(embed.Fields) > 0 {
		last := len(embed.Fields) - 1
		valueLength := len([]rune(embed.Fields[last].Value))
		if valueLength == 0 {
			embed.Fields = embed.Fields[:last]
		} else {
			embed.Fields[last].Value = truncateNotificationText(embed.Fields[last].Value, maxInt(0, valueLength-over))
		}
		over = embedTextLength(*embed) - maxDiscordEmbedText
	}
	if over > 0 {
		embed.Footer.Text = truncateNotificationText(embed.Footer.Text, maxInt(0, len([]rune(embed.Footer.Text))-over))
		over = embedTextLength(*embed) - maxDiscordEmbedText
	}
	if over > 0 {
		embed.Author.Name = truncateNotificationText(embed.Author.Name, maxInt(0, len([]rune(embed.Author.Name))-over))
		over = embedTextLength(*embed) - maxDiscordEmbedText
	}
	if over > 0 {
		embed.Title = truncateNotificationText(embed.Title, maxInt(0, len([]rune(embed.Title))-over))
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
