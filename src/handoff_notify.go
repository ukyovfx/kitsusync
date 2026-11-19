package main

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/model"
	"app/src/setup"
	"app/src/utils/config"
)

// A handoff is reported immediately when the receiving task has a concrete
// assignee. A handoff that fell back to the admin role, or one nobody has
// answered yet, is only listed in the morning digest: the mixed rule the
// owner chose. The Production's notification timing biases the whole rule.
const handoffDigestMaxItems = 5

// handoffAssignees lists the person IDs of a handoff's receiving task. The
// event holds the assignees K2 resolved, or the chosen fallback user; the
// admin fallback deliberately holds nobody and is resolved here instead.
func handoffAssignees(db *gorm.DB, event model.HandoffEvent) []string {
	if ids := model.SplitHandoffPersonIDs(event.NotifyPersonIDs); len(ids) > 0 {
		return ids
	}
	if event.FallbackUsed == model.HandoffFallbackAdmin {
		return resolveHandoffAdminKitsuPersonIDsFor(db, event.ProductionID)
	}
	return nil
}

// handoffTiming returns the Production's notification timing, defaulting to the
// mixed rule when the Production is not configured yet.
func handoffTiming(db *gorm.DB, productionID string) string {
	settings, ok, err := model.LoadProductionHandoffSettings(db, productionID)
	if err != nil || !ok || !model.ValidHandoffTiming(settings.Config.NotifyTiming) {
		return model.HandoffTimingMixed
	}
	return settings.Config.NotifyTiming
}

// handoffImmediate reports whether a handoff with these recipients is sent now
// rather than kept for the morning digest.
func handoffImmediate(timing string, fallbackUsed string, recipientIDs []string) bool {
	if len(recipientIDs) == 0 {
		return false
	}
	switch timing {
	case model.HandoffTimingImmediate:
		return true
	case model.HandoffTimingDigest:
		return false
	}
	return strings.TrimSpace(fallbackUsed) == model.HandoffFallbackNone
}

// sendImmediateHandoffs DMs every pending handoff whose recipient is a concrete
// assignee, and marks it notified so the same delivery is never sent twice.
func sendImmediateHandoffs(conf config.Config, db *gorm.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	pending, err := model.PendingHandoffEvents(db)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	botToken, _, _ := getDiscordSettings(db, conf)
	if strings.TrimSpace(botToken) == "" {
		return 0, nil
	}
	publicURL := setup.PublicKitsuURL(db)
	timings := map[string]string{}
	sent := 0
	var errs []string
	for _, event := range pending {
		timing, known := timings[event.ProductionID]
		if !known {
			timing = handoffTiming(db, event.ProductionID)
			timings[event.ProductionID] = timing
		}
		recipients := handoffRecipients(db, event)
		if !handoffImmediate(timing, event.FallbackUsed, recipients) {
			continue
		}
		// Claim before sending: another poll or instance may have reported the
		// same delivery already, and the event must not be sent twice.
		claimed, claimErr := model.MarkHandoffNotified(db, event.ID)
		if claimErr != nil {
			errs = append(errs, fmt.Sprintf("%d: %v", event.ID, claimErr))
			continue
		}
		if !claimed {
			continue
		}
		for _, recipient := range recipients {
			if err := sendDigestDirectMessage(botToken, recipient, renderHandoffCard(handoffTaskURL(publicURL, event), event)); err != nil {
				errs = append(errs, fmt.Sprintf("%d/%s: %v", event.ID, recipient, err))
			}
		}
		sent++
	}
	if len(errs) > 0 {
		return sent, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return sent, nil
}

// handoffRecipients resolves the Discord recipients of one handoff event.
func handoffRecipients(db *gorm.DB, event model.HandoffEvent) []string {
	return discord.ResolveAssigneeDiscordIDsWithIdentity(event.ProductionID, handoffAssignees(db, event), forumAssigneeIdentity)
}

// handoffCardLine reads as one row of the DM: which product of which shot came
// from whose task, with the link to the receiving task.
func handoffCardLine(event model.HandoffEvent, taskURL string) string {
	product := strings.TrimSpace(event.ProductName + " " + event.ProductVersion)
	line := fmt.Sprintf("%s: %s ← %s の %s", event.EntityName, product, event.SourceAuthorName, event.SourceTaskTypeName)
	if taskURL != "" {
		line += " " + taskURL
	}
	return line
}

// renderHandoffCard is the immediate DM for one handoff.
func renderHandoffCard(taskURL string, event model.HandoffEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔁 素材の受け渡し — %s\n", event.ProductName)
	if version := strings.TrimSpace(event.ProductVersion); version != "" {
		fmt.Fprintf(&b, "版: %s\n", version)
	}
	fmt.Fprintf(&b, "%s の %s から届きました。\n", event.SourceAuthorName, event.SourceTaskTypeName)
	if taskURL != "" {
		fmt.Fprintf(&b, "Kitsu: %s\n", taskURL)
	}
	return b.String()
}

// handoffTaskURL links the receiving task of a handoff in Kitsu.
func handoffTaskURL(publicURL string, event model.HandoffEvent) string {
	return discord.KitsuTaskURL(publicURL, event.ProductionID, event.EntityKind, event.NotifyTaskID)
}

// resolveHandoffAdminKitsuPersonIDs maps a Guild to the Kitsu person IDs its
// admins resolve through, so the admin fallback reaches real people.
var resolveHandoffAdminKitsuPersonIDs = resolveHandoffAdminIDs

// resolveHandoffAdminIDs reads the Guild's admins as Kitsu person IDs, the
// fallback K2 recorded for a receiving task without an assignee.
func resolveHandoffAdminIDs(db *gorm.DB, guildID string) []string {
	if db == nil || strings.TrimSpace(guildID) == "" {
		return nil
	}
	var users []model.UserMap
	if err := db.Where("discord_guild_id = ?", guildID).Find(&users).Error; err != nil {
		return nil
	}
	ids := make([]string, 0, len(users))
	for _, user := range users {
		if id := strings.TrimSpace(user.KitsuID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// resolveHandoffAdminKitsuPersonIDsFor reads the admins of the Production's Guild,
// the fallback K2 recorded for a receiving task without an assignee.
func resolveHandoffAdminKitsuPersonIDsFor(db *gorm.DB, productionID string) []string {
	var project model.Project
	if err := db.Where("kitsu_project_id = ?", productionID).First(&project).Error; err != nil {
		return nil
	}
	return resolveHandoffAdminKitsuPersonIDs(db, project.DiscordGuildID)
}
