package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
)

// The forum digest reads the forum ledger instead of the lane observation
// table: a forum-mode Production's unresolved work is whatever still sits in a
// trigger role on a task that has a post. Items are grouped per person, so a
// person only hears about their own queue.
const (
	digestCatchupUntilHourJST = 12
	// The forum digest marks its own day: the lane digest runs first for the
	// same person and Production, and a shared marker would suppress this one.
	forumDigestLastSentDateSettingKey = "digest.forum.last_sent_date."
	// digest.forum.done.<project> = today marks the whole day as finished for
	// a Production so a poll after 09:00 neither recomputes the recipients
	// (external reads) nor writes an audit row.
	forumDigestDoneSettingKey = "digest.forum.done."
)

// forumDigestItem is one unresolved item for one person.
type forumDigestItem struct {
	TaskID string
	Title  string
	Role   string
	Since  time.Time
}

// forumDigestRecipient is one person's digest for one Production.
type forumDigestRecipient struct {
	DiscordID string
	Waiting   []forumDigestItem
	Retakes   []forumDigestItem
	// Handoffs carry the product and its version, which no forum thread
	// title holds, and one reply row each.
	Handoffs []model.HandoffEvent
	// HandoffsMore is how many unanswered handoffs the card's cap left out.
	HandoffsMore int
}

// sendForumMorningDigests sends the per-person forum digest for forum-mode
// Productions and returns the number of DMs sent. A failed destination keeps
// its marker unset and retries on the next poll; after the catch-up deadline
// the day is marked done so a stuck DM never repeats forever.
func sendForumMorningDigests(conf config.Config, db *gorm.DB, now time.Time, today string) (int, error) {
	if db == nil {
		return 0, nil
	}
	botToken := setup.RuntimeDiscordBotToken(db)
	var digestErrs []string
	sent := 0
	for _, project := range model.ListProjects(db) {
		if project.KitsuProjectID == "" || project.ValidationOnly || strings.TrimSpace(project.DiscordGuildID) == "" {
			continue
		}
		doneKey := forumDigestDoneSettingKey + project.KitsuProjectID
		if model.GetSetting(db, doneKey) == today {
			continue
		}
		// Past the catch-up hour the day is done without sending: the digest
		// is only useful in the morning (stage E spec).
		if now.Hour() >= digestCatchupUntilHourJST {
			model.SetSetting(db, doneKey, today)
			continue
		}
		settings, ok, err := model.LoadProductionForumSettings(db, project.KitsuProjectID)
		if err != nil || !ok {
			continue
		}
		recipients, err := forumDigestRecipients(db, project, settings, now)
		if err != nil {
			digestErrs = append(digestErrs, fmt.Sprintf("%s: %v", project.KitsuProjectID, err))
			continue
		}
		// Unanswered handoffs are listed every morning until they are
		// answered, so they are merged into the per-person queue.
		recipients = mergeHandoffDigestItems(db, project, recipients)
		language := discord.NotificationLanguage(project.Language)
		projectErrs := 0
		projectSent := 0
		publicURL := setup.PublicKitsuURL(db)
		for _, recipient := range recipients {
			marker := forumDigestLastSentDateSettingKey + project.KitsuProjectID + "." + recipient.DiscordID
			if model.GetSetting(db, marker) == today {
				continue
			}
			content := renderForumDigestCard(project.Name, recipient, publicURL, language)
			if err := sendDigestDirectMessage(botToken, recipient.DiscordID, content, handoffReplyRows(recipient.Handoffs, language)); err != nil {
				projectErrs++
				digestErrs = append(digestErrs, fmt.Sprintf("%s/%s: %v", project.KitsuProjectID, recipient.DiscordID, err))
				continue
			}
			model.SetSetting(db, marker, today)
			projectSent++
		}
		// The day is done when every recipient was served; a failed
		// destination retries on the next poll until the catch-up deadline.
		if projectErrs == 0 {
			model.SetSetting(db, doneKey, today)
		}
		// A quiet poll (nothing sent, nothing failed) writes no audit row.
		if projectSent > 0 || projectErrs > 0 {
			model.WriteAuditLog(db, model.AuditLog{
				TaskID:      "morning-digest",
				ProjectID:   project.KitsuProjectID,
				ProjectName: project.Name,
				GuildID:     project.DiscordGuildID,
				NewStatus:   fmt.Sprintf("mode=forum recipients=%d sent=%d", len(recipients), projectSent),
				Success:     projectErrs == 0,
				ActorKind:   "system",
				ActorName:   "morning-digest",
			})
		}
		sent += projectSent
	}
	if len(digestErrs) > 0 {
		return sent, fmt.Errorf("%s", strings.Join(digestErrs, "; "))
	}
	return sent, nil
}

// forumDigestReviewerResolver returns the WFA reviewers of one Task Type. It is
// a variable so tests can answer without Kitsu and Discord.
var forumDigestReviewerResolver = resolveForumDigestReviewers

// resolveForumDigestReviewers resolves the forum WFA reviewers of a Task Type
// with the same rules as the runtime signal path: explicit targets plus the
// automatic supervisors, with role targets proven against the Bot's forum
// permissions.
func resolveForumDigestReviewers(db *gorm.DB, kitsuBaseURL, kitsuToken, discordToken, kitsuProjectID, taskTypeID string) ([]model.ProjectReviewerTarget, error) {
	return setup.ResolveForumWFAReviewerTargets(db, kitsuBaseURL, kitsuToken, discordToken, kitsuProjectID, taskTypeID)
}

// forumDigestRecipients builds each person's unresolved queue. A WFA item goes
// to the reviewers of the item's Task Type, a RETAKE item to the task's
// assignees. Only items that opened before today and are still open count.
func forumDigestRecipients(db *gorm.DB, project model.Project, settings model.ProductionForumSettings, now time.Time) ([]forumDigestRecipient, error) {
	tasks, err := model.ForumTasksWithPosts(db, project.KitsuProjectID)
	if err != nil {
		return nil, err
	}
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	roleByStatus := map[string]string{}
	for _, row := range settings.StatusRoles {
		roleByStatus[row.StatusID] = row.Role
	}
	// Reviewers are resolved per Task Type: a person only reviews the Task
	// Types they are a reviewer for, and the automatic supervisors of a Task
	// Type must be included. The resolver is memoised because it costs a few
	// Discord and Kitsu reads per Task Type.
	reviewersByTaskType := map[string][]string{}
	reviewersFor := func(taskTypeID string) []string {
		if ids, ok := reviewersByTaskType[taskTypeID]; ok {
			return ids
		}
		ids := forumDigestReviewerIDs(db, project.ID)
		hostname, _, _ := getKitsuCreds(db, config.Config{})
		baseURL := ""
		if connection, connectionErr := setup.ResolveKitsuConnection(context.Background(), hostname, model.GetSetting(db, setup.KitsuAPIBaseURLSettingKey)); connectionErr == nil {
			baseURL = connection.ResolvedAPIBaseURL
		}
		resolved, err := forumDigestReviewerResolver(db, baseURL, setup.StoredRuntimeKitsuToken(db), setup.RuntimeDiscordBotToken(db), project.KitsuProjectID, taskTypeID)
		if err == nil && len(resolved) > 0 {
			ids = nil
			for _, target := range resolved {
				if target.TargetKind == model.ReviewerTargetUser && target.DiscordID != "" {
					ids = append(ids, target.DiscordID)
				}
			}
		}
		reviewersByTaskType[taskTypeID] = ids
		return ids
	}
	byPerson := map[string]*forumDigestRecipient{}
	ensure := func(id string) *forumDigestRecipient {
		if recipient, ok := byPerson[id]; ok {
			return recipient
		}
		recipient := &forumDigestRecipient{DiscordID: id}
		byPerson[id] = recipient
		return recipient
	}
	for _, task := range tasks {
		role := roleByStatus[task.StatusID]
		if role != model.ForumRoleWFA && role != model.ForumRoleRetake {
			continue
		}
		if !task.StatusSince.Before(startOfToday) {
			continue
		}
		item := forumDigestItem{TaskID: task.TaskID, Title: task.Title, Role: role, Since: task.StatusSince}
		if role == model.ForumRoleWFA {
			for _, id := range reviewersFor(task.TaskTypeID) {
				ensure(id).Waiting = append(ensure(id).Waiting, item)
			}
			continue
		}
		for _, id := range discord.ResolveAssigneeDiscordIDsWithIdentity(project.KitsuProjectID, model.ForumAssigneeIDs(task), forumAssigneeIdentity) {
			ensure(id).Retakes = append(ensure(id).Retakes, item)
		}
	}
	// Notification-only recipients (a handoff fallback, say) have no forum
	// queue of their own; they are appended after the sorted ones.
	ordered := make([]forumDigestRecipient, 0, len(byPerson))
	for _, recipient := range byPerson {
		ordered = append(ordered, *recipient)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].DiscordID < ordered[j].DiscordID })
	return ordered, nil
}

// mergeHandoffDigestItems adds each person's unanswered handoffs to their digest
// and creates the recipient when only a handoff concerns them.
func mergeHandoffDigestItems(db *gorm.DB, project model.Project, recipients []forumDigestRecipient) []forumDigestRecipient {
	items, moreHandoffs := handoffDigestItems(db, project)
	if recipients == nil {
		recipients = []forumDigestRecipient{}
	}
	index := make(map[string]int, len(recipients))
	for i, recipient := range recipients {
		index[recipient.DiscordID] = i
	}
	for recipient, events := range items {
		position, known := index[recipient]
		if !known {
			recipients = append(recipients, forumDigestRecipient{DiscordID: recipient})
			position = len(recipients) - 1
			index[recipient] = position
		}
		recipients[position].Handoffs = events
		recipients[position].HandoffsMore = moreHandoffs[recipient]
	}
	return recipients
}

// handoffDigestItems returns each person's unanswered handoffs per Production.
// A pending handoff that the poller will send right away is not a reminder;
// notified handoffs, and pending ones the send failed on, stay listed until
// they are answered. The cap is what keeps a digest card's reply rows inside
// Discord's five-rows-per-message limit; it also returns, per person, how many
// unanswered handoffs the cap left out.
func handoffDigestItems(db *gorm.DB, project model.Project) (map[string][]model.HandoffEvent, map[string]int) {
	unanswered, err := model.UnansweredHandoffEvents(db)
	if err != nil {
		return nil, nil
	}
	byPerson := map[string][]model.HandoffEvent{}
	moreHandoffs := map[string]int{}
	for _, event := range unanswered {
		if event.ProductionID != project.KitsuProjectID {
			continue
		}
		recipients := handoffRecipients(db, event)
		if event.State == model.HandoffEventPending && handoffImmediate(handoffTiming(db, event.ProductionID), event.FallbackUsed, recipients) {
			continue
		}
		for _, recipient := range recipients {
			byPerson[recipient] = append(byPerson[recipient], event)
		}
	}
	for recipient, events := range byPerson {
		if len(events) > handoffDigestMaxItems {
			// A shortened card holds a forgotten row; the count keeps the
			// person aware of it when no button can reach it.
			moreHandoffs[recipient] = len(events) - handoffDigestMaxItems
			byPerson[recipient] = events[:handoffDigestMaxItems]
		}
	}
	return byPerson, moreHandoffs
}

// forumDigestReviewerIDs returns the Production's user-kind reviewer targets.
func forumDigestReviewerIDs(db *gorm.DB, projectID uint) []string {
	var targets []model.ProjectReviewerTarget
	if err := db.Where("project_id = ? AND target_kind = ?", projectID, model.ReviewerTargetUser).Order("id asc").Find(&targets).Error; err != nil {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, target := range targets {
		id := strings.TrimSpace(target.DiscordID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// renderForumDigestCard lists the person's own queue. Titles are the forum
// thread titles, so the DM reads the same as the post. lang is the
// Production's notification language.
func renderForumDigestCard(productionName string, recipient forumDigestRecipient, publicURL, lang string) string {
	en := discord.NotificationLanguage(lang) == "en"
	digestTitle := "📋 朝のダイジェスト — %s\n"
	waitingLabel := "・チェック待ち (WFA): %d件"
	retakeLabel := "・差し戻し (RETAKE): %d件"
	handoffLabel := "・素材の受け渡し: %d件"
	moreLabel := "他に未返事が %d 件あります"
	if en {
		digestTitle = "📋 Morning digest — %s\n"
		waitingLabel = "- Awaiting review (WFA): %d"
		retakeLabel = "- Retakes (RETAKE): %d"
		handoffLabel = "- Asset handoffs: %d"
		moreLabel = "%d more awaiting a reply"
	}
	var b strings.Builder
	fmt.Fprintf(&b, digestTitle, productionName)
	fmt.Fprintf(&b, waitingLabel, len(recipient.Waiting))
	if items := forumDigestLines(recipient.Waiting); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, retakeLabel, len(recipient.Retakes))
	if items := forumDigestLines(recipient.Retakes); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
	// Both lists are always rendered, so the person sees a real 0件 rather
	// than a missing section.
	fmt.Fprintf(&b, handoffLabel, len(recipient.Handoffs))
	if len(recipient.Handoffs) > 0 {
		lines := make([]string, 0, len(recipient.Handoffs))
		for _, event := range recipient.Handoffs {
			lines = append(lines, handoffCardLine(event, publicURL, lang))
		}
		b.WriteString("\n  " + strings.Join(lines, "\n  "))
		if recipient.HandoffsMore > 0 {
			fmt.Fprintf(&b, "\n  "+moreLabel, recipient.HandoffsMore)
		}
	} else {
		b.WriteString("\n")
	}
	return b.String()
}

func forumDigestLines(items []forumDigestItem) []string {
	if len(items) > digestMaxTasksPerSection {
		items = items[:digestMaxTasksPerSection]
	}
	titles := make([]string, 0, len(items))
	for _, item := range items {
		titles = append(titles, item.Title)
	}
	return prefixItems(titles, digestMaxTasksPerSection)
}
