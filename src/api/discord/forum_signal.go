package discord

import (
	"fmt"
	"strings"
	"time"

	"app/src/model"
	"app/src/utils/truncate"
)

// Forum signal messages. The first message of a thread is the card; every later
// status change is announced with one of these short messages so the thread
// reads as a timeline. Stage E will aggregate them behind the same renderer.

// ForumSignal is one rendered signal message.
type ForumSignal struct {
	Content        string
	Embeds         []Embed
	AllowedUserIDs []string
	AllowedRoleIDs []string
}

// ForumCardInput carries everything the card-shaped signal renderer needs.
// EntityType / ParentName / EntityName / TaskType come from the planned
// payload; LegacyTitle is the task row's space-joined title, the fallback for
// rows queued before the payload carried display fields. Language is the
// Production's notification language ("ja"/"en"); empty falls back to "ja".
type ForumCardInput struct {
	Intent      string
	TaskType    string
	EntityType  string
	ParentName  string
	EntityName  string
	LegacyTitle string
	Quote       string
	Submitter   string
	PublishLine string
	TaskURL     string
	StatusName  string
	Assignees   []string
	UserIDs     []string
	RoleIDs     []string
	Language    string
}

// footerLabels returns the localized footer prefixes so the forum signal
// cards and the notification card footer never drift apart.
func footerLabels(lang string) (status, assignees string) {
	if normalizeNotificationLang(lang) == "en" {
		return "Status: ", "Assignees: "
	}
	return "ステータス：", "担当："
}

// submitLabel is the WFA submitter line's localized prefix.
func submitLabel(lang string) string {
	if normalizeNotificationLang(lang) == "en" {
		return "Submitted by: "
	}
	return "提出："
}

// forumCardHeading builds the embed title in the card shape
// ("Shot / sq01 - sh002"), falling back to the legacy thread title.
func forumCardHeading(entityType, parent, task, legacyTitle string) string {
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
	case task != "":
		return task
	default:
		return legacyTitle
	}
}

// forumStatusLine renders the 状態行: emoji + STATUS + action.
func forumStatusLine(intent, lang string) string {
	switch intent {
	case model.ForumIntentStatusWFA, model.ForumIntentStatusRetake, model.ForumIntentStatusDone:
		message, emoji := localizedStatusMessageInfo(strings.ToUpper(intent), lang)
		return emoji + " " + strings.ToUpper(intent) + " " + message
	case model.ForumIntentAssign:
		_, emoji := localizedStatusMessageInfo("TODO", lang)
		if normalizeNotificationLang(lang) == "en" {
			return emoji + " You have been assigned to this task."
		}
		return emoji + " このタスクの担当になりました。"
	default:
		return ""
	}
}

// forumCardDescription assembles the embed description lines.
func forumCardDescription(in ForumCardInput) string {
	var parts []string
	switch in.Intent {
	case model.ForumIntentComment:
		if in.Quote != "" {
			parts = append(parts, "💬 "+in.Quote)
		}
	case model.ForumIntentPublish:
		if in.PublishLine != "" {
			parts = append(parts, sanitizeDiscordText(in.PublishLine))
		}
	default:
		if line := forumStatusLine(in.Intent, in.Language); line != "" {
			parts = append(parts, line)
		}
	}
	if in.Intent == model.ForumIntentStatusWFA {
		if trimmed := strings.TrimSpace(in.Submitter); trimmed != "" {
			parts = append(parts, submitLabel(in.Language)+sanitizeDiscordText(trimmed))
		}
	}
	if in.Intent == model.ForumIntentStatusRetake {
		if trimmed := strings.TrimSpace(in.Quote); trimmed != "" {
			parts = append(parts, "> "+sanitizeDiscordText(trimmed))
		}
	}
	if in.TaskURL != "" {
		parts = append(parts, "[🦊 Kitsu]("+in.TaskURL+")")
	}
	footer := forumCardFooter(in.StatusName, in.Assignees, in.Language)
	if footer != "" {
		parts = append(parts, footer)
	}
	return strings.Join(parts, "\n\n")
}

// forumCardFooter renders the footer block: status / assignees. Japanese keeps
// the full-width colon; English uses the plain one.
func forumCardFooter(statusName string, assignees []string, lang string) string {
	statusLabel, assigneeLabel := footerLabels(lang)
	var lines []string
	if status := strings.ToLower(strings.TrimSpace(statusName)); status != "" {
		lines = append(lines, statusLabel+status)
	}
	if names := sanitizeAssigneeNames(assignees); len(names) > 0 {
		lines = append(lines, assigneeLabel+strings.Join(names, " "))
	}
	return strings.Join(lines, "\n")
}

func sanitizeAssigneeNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if trimmed := sanitizeDiscordText(strings.TrimSpace(name)); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// RenderForumSignalCard renders a signal as a card-shaped embed. Content
// carries the mention line only; description, links and the status/assignee
// footer live in the embed. Comment and publish signals are neutral: only
// WFA / RETAKE / DONE use the status colors.
func RenderForumSignalCard(in ForumCardInput) ForumSignal {
	title := truncate.TruncateString(forumCardHeading(in.EntityType, in.ParentName, in.EntityName, in.LegacyTitle), 256)
	color := neutralEmbedColor
	if in.Intent == model.ForumIntentStatusWFA || in.Intent == model.ForumIntentStatusRetake || in.Intent == model.ForumIntentStatusDone {
		color = notificationStatusColor(in.Intent)
	}
	embed := Embed{
		Description: truncate.TruncateString(forumCardDescription(in), 4096),
		Title:       title,
		Color:       color,
		Author:      EmbedAuthor{Name: truncate.TruncateString(strings.TrimSpace(in.TaskType), 256)},
	}
	return ForumSignal{
		Content:        forumMentionText(in.UserIDs, in.RoleIDs),
		Embeds:         []Embed{embed},
		AllowedUserIDs: uniqueDiscordIDs(in.UserIDs),
		AllowedRoleIDs: uniqueDiscordRoleIDs(in.RoleIDs),
	}
}

// ForumFlapWarning renders the "state is flapping" warning. It carries no
// mentions on purpose: the signal messages still ping, the warning only
// informs. lang is the Production's notification language; empty means "ja".
func ForumFlapWarning(taskURL string, transitions int, span time.Duration, lang string) ForumSignal {
	var line string
	if normalizeNotificationLang(lang) == "en" {
		line = fmt.Sprintf("⚠️ The status changed %d times in %s. The tag shows the latest status.", transitions, forumSpanText(span, lang))
	} else {
		line = fmt.Sprintf("⚠️ 状態が短時間に何度も変わっています（%d 回 / %s）。タグは最新の状態を表しています。", transitions, forumSpanText(span, lang))
	}
	parts := []string{line}
	if taskURL != "" {
		parts = append(parts, taskURL)
	}
	return ForumSignal{Content: strings.Join(parts, "\n")}
}

func forumSpanText(span time.Duration, lang string) string {
	if normalizeNotificationLang(lang) == "en" {
		if span < time.Minute {
			return "under a minute"
		}
		minutes := int(span.Round(time.Minute) / time.Minute)
		if minutes >= 60 {
			return fmt.Sprintf("%dh", minutes/60)
		}
		return fmt.Sprintf("%dm", minutes)
	}
	if span < time.Minute {
		return "1分以内"
	}
	minutes := int(span.Round(time.Minute) / time.Minute)
	if minutes >= 60 {
		return fmt.Sprintf("%d時間", minutes/60)
	}
	return fmt.Sprintf("%d分", minutes)
}

// forumMentionText builds the leading mention text for a signal.
func forumMentionText(userIDs, roleIDs []string) string {
	users := uniqueDiscordIDs(userIDs)
	roles := uniqueDiscordRoleIDs(roleIDs)
	parts := make([]string, 0, len(users)+len(roles))
	for _, id := range users {
		parts = append(parts, "<@"+id+">")
	}
	for _, id := range roles {
		parts = append(parts, "<@&"+id+">")
	}
	return strings.Join(parts, " ")
}

// ForumFirstMessage builds the starter message of a new thread from an existing
// card payload: the card is unchanged, only the content is emptied and mentions
// are suppressed (D-04).
func ForumFirstMessage(card Payload) ForumThreadMessage {
	return ForumThreadMessage{
		Content:         "",
		Embeds:          card.Embeds,
		AllowedMentions: &AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}},
	}
}

// ForumSignalMessage builds the request body for a signal message.
func ForumSignalMessage(signal ForumSignal, nonce string) ForumMessageCreate {
	return ForumMessageCreate{
		Content: signal.Content,
		Embeds:  signal.Embeds,
		AllowedMentions: &AllowedMentions{
			Parse: []string{},
			Users: signal.AllowedUserIDs,
			Roles: signal.AllowedRoleIDs,
		},
		Flags:        4,
		Nonce:        nonce,
		EnforceNonce: nonce != "",
	}
}

// forumNonceMaxLen is Discord's limit for message nonces.
const forumNonceMaxLen = 25

// ForumNonce derives a deterministic nonce so a retried send is deduplicated by
// Discord instead of creating a second message. Discord rejects nonces longer
// than 25 characters, so the outbox id carries the uniqueness and the task id is
// only a readable suffix.
func ForumNonce(taskID string, outboxID uint) string {
	nonce := fmt.Sprintf("kitsusync-%d", outboxID)
	if taskID == "" {
		return nonce
	}
	room := forumNonceMaxLen - len(nonce) - 1
	if room <= 0 {
		return nonce
	}
	trimmed := taskID
	if len(trimmed) > room {
		trimmed = trimmed[len(trimmed)-room:]
	}
	return nonce + "-" + trimmed
}

// ForumArchiveMinutes returns the archive duration for a role.
func ForumArchiveMinutes(role string) int {
	if role == "done" {
		return 4320
	}
	return 10080
}

// ForumSyncPlan is the ordered set of thread changes for a status.
type ForumSyncPlan struct {
	NeedUnarchive  bool
	ArchiveMinutes int
	NeedRetag      bool
	AppliedTags    []string
}

// PlanForumSync computes the thread changes for the current role. Managed tags
// are replaced; human tags are kept. Unarchive and retag are separate calls
// because a combined PATCH hits the rate limit and archived threads reject tag
// changes.
func PlanForumSync(current ForumThread, managedTags []string, roleTag string, role string, extraTags []string) ForumSyncPlan {
	archiveMinutes := ForumArchiveMinutes(role)
	plan := ForumSyncPlan{ArchiveMinutes: archiveMinutes}
	if current.ThreadMetadata != nil && current.ThreadMetadata.Archived {
		plan.NeedUnarchive = true
	}
	managed := map[string]struct{}{}
	for _, tag := range managedTags {
		if tag != "" {
			managed[tag] = struct{}{}
		}
	}
	next := make([]string, 0, len(current.AppliedTags)+1)
	for _, tag := range current.AppliedTags {
		if _, isManaged := managed[tag]; isManaged {
			continue
		}
		next = append(next, tag)
	}
	for _, tag := range extraTags {
		if tag != "" {
			next = append(next, tag)
		}
	}
	if roleTag != "" {
		next = append(next, roleTag)
	}
	next = dedupeStrings(next)
	// Discord allows at most 5 applied tags; drop extra unmanaged tags last-in
	// first so the role tag survives.
	if len(next) > 5 {
		next = next[len(next)-5:]
		if roleTag != "" && !containsString(next, roleTag) {
			next = append(next[:4], roleTag)
		}
	}
	plan.AppliedTags = next
	plan.NeedRetag = !sameStringSet(current.AppliedTags, next)
	if current.ThreadMetadata != nil && current.ThreadMetadata.AutoArchiveDuration != archiveMinutes {
		plan.NeedRetag = true
	}
	return plan
}

// ForumSyncPatch is the patch for the retag step.
func ForumSyncPatch(plan ForumSyncPlan) ForumThreadPatch {
	appliedTags := plan.AppliedTags
	if appliedTags == nil {
		appliedTags = []string{}
	}
	return ForumThreadPatch{
		AutoArchiveDuration: &plan.ArchiveMinutes,
		AppliedTags:         &appliedTags,
	}
}

// ForumUnarchivePatch is the patch for the unarchive step.
func ForumUnarchivePatch() ForumThreadPatch {
	archived := false
	return ForumThreadPatch{Archived: &archived}
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	set := map[string]struct{}{}
	for _, value := range left {
		set[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
