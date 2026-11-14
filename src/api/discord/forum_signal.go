package discord

import (
	"fmt"
	"strings"
	"time"
)

// Forum signal messages. The first message of a thread is the card; every later
// status change is announced with one of these short messages so the thread
// reads as a timeline. Stage E will aggregate them behind the same renderer.

// ForumSignal is one rendered signal message.
type ForumSignal struct {
	Content        string
	AllowedUserIDs []string
	AllowedRoleIDs []string
}

// RenderForumSignal renders the signal for a forum intent.
//   - wfa:     reviewer mention + チェックをお願いします。
//   - retake:  assignee mention + リテイクです。 (+ optional quote)
//   - done:    assignee mention + 完了しました。
//   - assign:  new assignee mention + このタスクの担当になりました。
//   - comment: mention + コメントがあります。 (+ optional quote)
func RenderForumSignal(intent, taskURL, quote string, userIDs, roleIDs []string) ForumSignal {
	mentions := forumMentionText(userIDs, roleIDs)
	var line string
	switch intent {
	case "wfa":
		line = "チェックをお願いします。"
	case "retake":
		line = "リテイクです。"
	case "done":
		line = "完了しました。"
	case "assign":
		line = "このタスクの担当になりました。"
	case "comment":
		line = "コメントがあります。"
	default:
		line = "更新されました。"
	}
	parts := []string{}
	if mentions != "" {
		parts = append(parts, mentions)
	}
	parts = append(parts, line)
	if intent == "retake" || intent == "comment" {
		if trimmed := strings.TrimSpace(quote); trimmed != "" {
			parts = append(parts, "> "+sanitizeDiscordText(trimmed))
		}
	}
	if taskURL != "" {
		parts = append(parts, taskURL)
	}
	return ForumSignal{
		Content:        strings.Join(parts, "\n"),
		AllowedUserIDs: uniqueDiscordIDs(userIDs),
		AllowedRoleIDs: uniqueDiscordRoleIDs(roleIDs),
	}
}

// ForumFlapWarning renders the "state is flapping" warning. It carries no
// mentions on purpose: the signal messages still ping, the warning only informs.
func ForumFlapWarning(taskURL string, transitions int, span time.Duration) ForumSignal {
	parts := []string{fmt.Sprintf("⚠️ 状態が短時間に何度も変わっています（%d 回 / %s）。タグは最新の状態を表しています。", transitions, forumSpanText(span))}
	if taskURL != "" {
		parts = append(parts, taskURL)
	}
	return ForumSignal{Content: strings.Join(parts, "\n")}
}

func forumSpanText(span time.Duration) string {
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

// ForumNonce derives a deterministic nonce so a retried send is deduplicated by
// Discord instead of creating a second message.
func ForumNonce(taskID string, outboxID uint) string {
	if taskID == "" {
		return fmt.Sprintf("kitsusync-%d", outboxID)
	}
	trimmed := taskID
	if len(trimmed) > 20 {
		trimmed = trimmed[len(trimmed)-20:]
	}
	return fmt.Sprintf("kitsusync-%s-%d", trimmed, outboxID)
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
	return ForumThreadPatch{
		AutoArchiveDuration: &plan.ArchiveMinutes,
		AppliedTags:         plan.AppliedTags,
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
