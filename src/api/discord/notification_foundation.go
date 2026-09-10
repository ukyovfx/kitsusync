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
	// Current WFA / RETAKE / DONE delivery always uses this card renderer,
	// regardless of the selected legacy preset. Do not read legacy template
	// files first: normal status delivery must not depend on them.
	if !data.IsAssignNotification && isCurrentStatusNotification(data) {
		return renderCurrentNotificationCardPayload(data)
	}

	return renderTemplateNotificationPayload(data, preset)
}

func isCurrentStatusNotification(data Template) bool {
	status := strings.ToUpper(strings.TrimSpace(data.StatusUpper))
	if status == "" {
		status = strings.ToUpper(strings.TrimSpace(data.CurrentStatus))
	}
	switch status {
	case "WFA", "RETAKE", "DONE":
		return true
	default:
		return false
	}
}

func renderCurrentNotificationCardPayload(data Template) Payload {
	author := strings.TrimSpace(data.TaskType)
	footer := strings.TrimSpace(data.CardContext)
	embed := Embed{
		Description: truncate.TruncateString(notificationCardDescription(data), 4096),
		Color:       data.Color,
		Author:      EmbedAuthor{Name: truncate.TruncateString(author, 256)},
		Footer:      EmbedFooter{Text: truncate.TruncateString(footer, 2048)},
	}
	if data.PreviewImageURL != "" {
		embed.Image = &EmbedImage{URL: data.PreviewImageURL}
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

func renderTemplateNotificationPayload(data Template, preset string) Payload {
	author := parseTaskTemplate("tpl/"+preset+"/author.tpl", data)
	title := parseTaskTemplate("tpl/"+preset+"/title.tpl", data)
	description := parseTaskTemplate("tpl/"+preset+"/description.tpl", data)
	footer := parseTaskTemplate("tpl/"+preset+"/footer.tpl", data)

	embed := Embed{
		Title:       truncate.TruncateString(title, 256),
		Description: truncate.TruncateString(description, 4096),
		Color:       data.Color,
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
	parts := make([]string, 0, 3)
	if title := strings.TrimSpace(notificationCardTitle(data)); title != "" {
		parts = append(parts, "## "+title)
	}
	if status != "" || message != "" {
		statusBlock := ""
		if status != "" {
			statusBlock = "### " + status
		}
		if message != "" {
			if statusBlock != "" {
				statusBlock += "\n"
			}
			statusBlock += message
		}
		parts = append(parts, statusBlock)
	}
	if strings.TrimSpace(data.CommentContent) != "" {
		commentLabel := strings.TrimSpace(data.CommentLabel)
		if commentLabel == "" {
			commentLabel = "Comment"
			if normalizeNotificationLang(data.NotificationLanguage) == "ja" {
				commentLabel = "コメント"
			}
		}
		comment := "**" + commentLabel + "**\n> " + strings.TrimSpace(data.CommentContent)
		if author := strings.TrimSpace(data.CommentAuthor); author != "" {
			comment += "\nby " + author
		}
		parts = append(parts, comment)
	}

	links := make([]string, 0, 2)
	if kitsuURL := safeNotificationURL(data.TaskURL); kitsuURL != "" {
		links = append(links, "[🦊 Kitsu]("+kitsuURL+")")
	}
	if driveURL := safeNotificationURL(data.GoogleDriveURL); driveURL != "" {
		links = append(links, "[📁 Drive]("+driveURL+")")
	}
	if len(links) > 0 {
		parts = append(parts, strings.Join(links, "　　"))
	}

	statusLabel := "Status"
	if normalizeNotificationLang(data.NotificationLanguage) == "ja" {
		statusLabel = "ステータス"
	}
	assigneeLabel := strings.TrimSpace(data.AssigneeLabel)
	if assigneeLabel == "" {
		assigneeLabel = "Assignee"
	}
	parts = append(parts, "**📊 "+statusLabel+"**　　　**👤 "+assigneeLabel+"**\n"+notificationCardStatusValue(data)+"　　　　"+strings.TrimSpace(data.AssigneesStr))
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
