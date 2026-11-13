package main

import (
	"app/src/api/discord"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
	stdslog "log/slog"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// The morning digest is a daily 09:00 JST DM card per configured reviewer.
// It only records one line per production in the audit trail; the delivery
// ledger remains the source of truth for individual notifications. Markers
// are per recipient so a partial failure retries only the failed destinations.
const (
	digestLastSentDateSettingKey = "digest.last_sent_date."
	digestStallNoticeDateKey     = "digest.stall_notice_date."
	digestHourJST                = 9
	digestMaxTasksPerSection     = 10
	stalledTaskAfter             = 24 * time.Hour
)

// jst is a fixed UTC+9 zone: Japan has no DST, so a FixedZone is exact.
var jst = time.FixedZone("JST", 9*60*60)

// runMorningDigestIfDue sends the daily digest DMs once per JST day and
// recipient. A failed destination simply keeps its marker unset and retries on
// the next poll.
func runMorningDigestIfDue(conf config.Config, db *gorm.DB) {
	if db == nil || conf.SilentUpdateDB {
		return
	}
	now := time.Now().In(jst)
	if now.Hour() < digestHourJST {
		return
	}
	if err := sendMorningDigests(conf, db, now, now.Format("2006-01-02")); err != nil {
		stdslog.Error("Morning digest partially failed; failed destinations retry next poll", "error_class", "digest_send_failed", "err", err)
	}
}

// productionDigest is one production's digest card content. Section entries
// are display names (the shot/asset name recorded with the observation).
type productionDigest struct {
	ProductionID   string
	ProductionName string
	WaitingCheck   []string
	Retakes        []string
	Stalled        []string
	DoneYesterday  []string
}

func sendMorningDigests(conf config.Config, db *gorm.DB, now time.Time, today string) error {
	botToken := setup.RuntimeDiscordBotToken(db)
	var digestErrs []string
	for _, project := range model.ListProjects(db) {
		if project.KitsuProjectID == "" || project.ValidationOnly || strings.TrimSpace(project.DiscordGuildID) == "" {
			continue
		}
		digest := buildProductionDigest(db, project, now)
		recipients := digestRecipientIDs(db, project.ID)
		if len(recipients) == 0 {
			continue
		}
		for _, recipient := range recipients {
			// Per-destination daily marker: successful cards are never
			// re-sent; failed ones retry on the next poll.
			marker := digestLastSentDateSettingKey + project.KitsuProjectID + "." + recipient
			if model.GetSetting(db, marker) == today {
				continue
			}
			card := renderDigestCard(digest, mentionExcessForToday(db, recipient, now))
			if err := sendDigestDirectMessage(botToken, recipient, card); err != nil {
				digestErrs = append(digestErrs, fmt.Sprintf("%s/%s: %v", project.KitsuProjectID, recipient, err))
				continue
			}
			model.SetSetting(db, marker, today)
		}
		// Stalled work gets a separate one-line notice to the lead, once per
		// day per production. The lead is the production's first user-kind
		// reviewer target.
		stallMarker := digestStallNoticeDateKey + project.KitsuProjectID
		if len(digest.Stalled) > 0 && model.GetSetting(db, stallMarker) != today {
			notice := fmt.Sprintf("⏰ %s: %d件のタスクがWFAのまま24時間以上経過しています（先頭: %s）",
				digest.ProductionName, len(digest.Stalled), strings.Join(prefixItems(digest.Stalled, 3), ", "))
			if err := sendDigestDirectMessage(botToken, recipients[0], notice); err != nil {
				digestErrs = append(digestErrs, fmt.Sprintf("%s/stall-notice: %v", project.KitsuProjectID, err))
			} else {
				model.SetSetting(db, stallMarker, today)
			}
		}
		model.WriteAuditLog(db, model.AuditLog{
			TaskID:      "morning-digest",
			ProjectID:   project.KitsuProjectID,
			ProjectName: project.Name,
			GuildID:     project.DiscordGuildID,
			NewStatus:   fmt.Sprintf("wfa=%d retake=%d stalled=%d done_yesterday=%d recipients=%d", len(digest.WaitingCheck), len(digest.Retakes), len(digest.Stalled), len(digest.DoneYesterday), len(recipients)),
			Success:     len(digestErrs) == 0,
			ActorKind:   "system",
			ActorName:   "morning-digest",
		})
	}
	if len(digestErrs) > 0 {
		return fmt.Errorf("%s", strings.Join(digestErrs, "; "))
	}
	return nil
}

// buildProductionDigest derives the four digest sections from the task
// observation table, which carries the production membership itself. Tasks
// silently accepted by the initial checkpoint never enter the delivery
// ledger, so the ledger cannot be the membership source.
func buildProductionDigest(db *gorm.DB, project model.Project, now time.Time) productionDigest {
	digest := productionDigest{ProductionID: project.KitsuProjectID, ProductionName: project.Name}
	var tasks []model.Task
	if err := db.Where("production_id = ?", project.KitsuProjectID).Find(&tasks).Error; err != nil {
		return digest
	}
	yesterdayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst).AddDate(0, 0, -1)
	for _, task := range tasks {
		status := strings.ToLower(strings.TrimSpace(task.LastObservedStatus))
		if status == "" {
			status = strings.ToLower(strings.TrimSpace(task.TaskStatus))
		}
		name := taskDisplayItem(task)
		switch status {
		case "wfa":
			digest.WaitingCheck = append(digest.WaitingCheck, name)
			if stalledLongerThan(task, now) {
				digest.Stalled = append(digest.Stalled, name)
			}
		case "retake":
			digest.Retakes = append(digest.Retakes, name)
		case "done":
			if taskCompletedWithin(task, yesterdayStart, 24*time.Hour, now) {
				digest.DoneYesterday = append(digest.DoneYesterday, name)
			}
		}
	}
	return digest
}

// taskDisplayItem prefers the entity name recorded with the observation
// (sh002-style shot names) and falls back to the task ID.
func taskDisplayItem(task model.Task) string {
	if name := strings.TrimSpace(task.EntityName); name != "" {
		return name
	}
	if len(task.TaskID) > 12 {
		return task.TaskID[:12]
	}
	return task.TaskID
}

// taskCompletedWithin reports whether the task's status most recently became
// done inside the given JST window. The status-change timestamp recorded at
// observation time is the completion time; the row timestamp is the fallback
// for observations that predate it.
func taskCompletedWithin(task model.Task, windowStart time.Time, window time.Duration, now time.Time) bool {
	completed := task.StatusChangedAt
	if completed.IsZero() {
		completed = task.UpdatedAt
	}
	completed = completed.In(jst)
	return completed.After(windowStart) && completed.Before(windowStart.Add(window)) && now.After(completed)
}

// stalledLongerThan reports whether a WFA task has been unchanged for over a
// day. The Kitsu updated_at timestamp is authoritative; the row timestamp is
// the fallback for legacy rows.
func stalledLongerThan(task model.Task, now time.Time) bool {
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(task.TaskUpdatedAt)); err == nil {
		return now.Sub(parsed) > stalledTaskAfter
	}
	return now.Sub(task.UpdatedAt) > stalledTaskAfter
}

// digestRecipientIDs returns the production's user-kind reviewer Discord IDs,
// deduplicated, oldest first so the first configured reviewer is the lead.
func digestRecipientIDs(db *gorm.DB, projectID uint) []string {
	var targets []model.ProjectReviewerTarget
	if err := db.Where("project_id = ? AND target_kind = ?", projectID, model.ReviewerTargetUser).Order("id asc").Find(&targets).Error; err != nil {
		return nil
	}
	seen := map[string]bool{}
	var recipients []string
	for _, target := range targets {
		id := strings.TrimSpace(target.DiscordID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		recipients = append(recipients, id)
	}
	return recipients
}

// renderDigestCard renders the fixed sections with counts. No buttons: the
// card is informational, approval DMs remain the primary channel.
func renderDigestCard(digest productionDigest, mentionExcess int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 朝のダイジェスト — %s\n", digest.ProductionName)
	fmt.Fprintf(&b, "・チェック待ち (WFA): %d件", len(digest.WaitingCheck))
	if items := prefixItems(digest.WaitingCheck, digestMaxTasksPerSection); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "・差し戻し (RETAKE): %d件", len(digest.Retakes))
	if items := prefixItems(digest.Retakes, digestMaxTasksPerSection); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "・止まっている物 (24h以上WFA): %d件", len(digest.Stalled))
	if items := prefixItems(digest.Stalled, digestMaxTasksPerSection); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "・昨日の完了 (DONE): %d件", len(digest.DoneYesterday))
	if items := prefixItems(digest.DoneYesterday, digestMaxTasksPerSection); len(items) > 0 {
		b.WriteString("  " + strings.Join(items, ", "))
	}
	b.WriteString("\n")
	if mentionExcess > 0 {
		fmt.Fprintf(&b, "・%s: %d件\n", mentionExcessDigestSectionLabel, mentionExcess)
	}
	return b.String()
}

func prefixItems(items []string, max int) []string {
	if len(items) > max {
		items = items[:max]
	}
	short := make([]string, 0, len(items))
	for _, item := range items {
		if len(item) > 12 {
			item = item[:12]
		}
		short = append(short, item)
	}
	return short
}

// sendDigestDirectMessage delivers one digest card through the existing bot
// DM transport. Failures surface to the caller; they never abort the digest
// for other productions.
func sendDigestDirectMessage(botToken, recipientID, content string) error {
	if strings.TrimSpace(botToken) == "" {
		return fmt.Errorf("dm_bot_token_missing")
	}
	channelID, err := discord.OpenDMChannel(botToken, recipientID)
	if err != nil {
		return fmt.Errorf("dm_channel_unavailable: %w", err)
	}
	result := discord.SendDirectMessage(botToken, channelID, discord.Payload{Content: content})
	if result.MessageID == "" {
		return fmt.Errorf("dm_send_failed: %s", result.FailureCategory)
	}
	return nil
}
