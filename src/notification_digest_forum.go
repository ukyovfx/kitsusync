package main

import (
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
		settings, ok, err := model.LoadProductionForumSettings(db, project.KitsuProjectID)
		if err != nil || !ok || settings.Config.Mode != model.NotificationModeForum {
			continue
		}
		recipients, err := forumDigestRecipients(db, project, settings, now)
		if err != nil {
			digestErrs = append(digestErrs, fmt.Sprintf("%s: %v", project.KitsuProjectID, err))
			continue
		}
		// Past the catch-up hour a failed DM is dropped rather than retried
		// all day: the digest is only useful in the morning (stage E spec).
		dropAfter := now.Hour() >= digestCatchupUntilHourJST
		for _, recipient := range recipients {
			marker := digestLastSentDateSettingKey + project.KitsuProjectID + "." + recipient.DiscordID
			if model.GetSetting(db, marker) == today {
				continue
			}
			if err := sendDigestDirectMessage(botToken, recipient.DiscordID, renderForumDigestCard(project.Name, recipient)); err != nil {
				if dropAfter {
					model.SetSetting(db, marker, today)
					continue
				}
				digestErrs = append(digestErrs, fmt.Sprintf("%s/%s: %v", project.KitsuProjectID, recipient.DiscordID, err))
				continue
			}
			model.SetSetting(db, marker, today)
			sent++
		}
		model.WriteAuditLog(db, model.AuditLog{
			TaskID:      "morning-digest",
			ProjectID:   project.KitsuProjectID,
			ProjectName: project.Name,
			GuildID:     project.DiscordGuildID,
			NewStatus:   fmt.Sprintf("mode=forum recipients=%d", len(recipients)),
			Success:     true,
			ActorKind:   "system",
			ActorName:   "morning-digest",
		})
	}
	if len(digestErrs) > 0 {
		return sent, fmt.Errorf("%s", strings.Join(digestErrs, "; "))
	}
	return sent, nil
}

// forumDigestRecipients builds each person's unresolved queue. A WFA item goes
// to the Production's reviewer targets, a RETAKE item to the task's assignees.
// Only items that opened before today and are still open count.
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
	reviewers := forumDigestReviewerIDs(db, project.ID)
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
			for _, id := range reviewers {
				ensure(id).Waiting = append(ensure(id).Waiting, item)
			}
			continue
		}
		for _, id := range discord.ResolveAssigneeDiscordIDs(project.KitsuProjectID, model.ForumAssigneeIDs(task)) {
			ensure(id).Retakes = append(ensure(id).Retakes, item)
		}
	}
	ordered := make([]forumDigestRecipient, 0, len(byPerson))
	for _, recipient := range byPerson {
		ordered = append(ordered, *recipient)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].DiscordID < ordered[j].DiscordID })
	return ordered, nil
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
// thread titles, so the DM reads the same as the post.
func renderForumDigestCard(productionName string, recipient forumDigestRecipient) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 朝のダイジェスト — %s\n", productionName)
	fmt.Fprintf(&b, "・チェック待ち (WFA): %d件", len(recipient.Waiting))
	if items := forumDigestLines(recipient.Waiting); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "・差し戻し (RETAKE): %d件", len(recipient.Retakes))
	if items := forumDigestLines(recipient.Retakes); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
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
