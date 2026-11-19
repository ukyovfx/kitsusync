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
			payload := discord.Payload{Content: renderHandoffCard(publicURL, event), Components: handoffReplyRows([]model.HandoffEvent{event})}
			if err := sendDigestDirectMessage(botToken, recipient, payload); err != nil {
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

// handoffCardLine reads as one row of the DM and, with a task route, links the
// receiving task in Kitsu.
func handoffCardLine(event model.HandoffEvent, taskURL string) string {
	line := fmt.Sprintf("%s: %s ← %s の %s", event.EntityName, handoffProduct(event), event.SourceAuthorName, event.SourceTaskTypeName)
	if taskURL != "" {
		line += " " + handoffTaskURL(taskURL, event)
	}
	return line
}

func handoffProduct(event model.HandoffEvent) string {
	return strings.TrimSpace(event.ProductName + " " + event.ProductVersion)
}

// renderHandoffCard is the immediate DM for one handoff, with the receiver's
// reply buttons under it.
func renderHandoffCard(taskURL string, event model.HandoffEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔁 素材の受け渡し — %s / %s\n", strings.TrimSpace(event.EntityName), event.ProductName)
	if version := strings.TrimSpace(event.ProductVersion); version != "" {
		fmt.Fprintf(&b, "版: %s\n", version)
	}
	fmt.Fprintf(&b, "%s の %s から届きました。\n", event.SourceAuthorName, event.SourceTaskTypeName)
	if taskURL != "" {
		fmt.Fprintf(&b, "Kitsu: %s\n", handoffTaskURL(taskURL, event))
	}
	return b.String()
}

// handoffReplyRows is the receiver's reply for one handoff: one row with OK and
// リテイク. Discords' five-rows-per-message limit therefore caps a digest card
// at handoffDigestMaxItems handoffs. Without a CustomID the buttons are inert,
// which is what the bot sends while nothing listens for button presses yet.
func handoffReplyRows(events []model.HandoffEvent) []discord.ActionRow {
	rows := make([]discord.ActionRow, 0, len(events))
	for _, event := range events {
		id := event.CommentID
		rows = append(rows, discord.ActionRow{Type: 1, Components: []discord.Button{
			{Type: 2, Style: discord.ButtonStyleSuccess, Label: "OK", CustomID: handoffReplyCustomID("ok", id)},
			{Type: 2, Style: discord.ButtonStyleDanger, Label: "リテイク", CustomID: handoffReplyCustomID("retake", id)},
		}})
	}
	return rows
}

// handoffReplyCustomID names the reply of one handoff. It is empty while the
// runtime has no Discord interaction endpoint to receive the press, so the
// button still tells the receiver what the options are without pretending the
// press does something. Flip handoffRepliesWired when that endpoint exists.
func handoffReplyCustomID(action, commentID string) string {
	if !handoffRepliesWired() || strings.TrimSpace(commentID) == "" {
		return ""
	}
	return "handoff:" + action + ":" + commentID
}

// handoffRepliesWired reports whether button presses are received. KitsuSync
// has no Discord gateway and no public interaction endpoint, so today it is
// always false: replies are written by hand in Kitsu.
func handoffRepliesWired() bool { return false }

// handoffTaskURL links the receiving task of a handoff in Kitsu.
func handoffTaskURL(publicURL string, event model.HandoffEvent) string {
	return discord.KitsuTaskURL(publicURL, event.ProductionID, event.EntityKind, event.NotifyTaskID)
}

// resolveHandoffAdminKitsuPersonIDs maps a Guild to the Kitsu person IDs its
// admins resolve through, so the admin fallback reaches real people.
var resolveHandoffAdminKitsuPersonIDs = resolveHandoffAdminIDs

// resolveHandoffAdminIDs reads the Guild's admins as Kitsu person IDs, the
// fallback K2 recorded for a receiving task without an assignee. Per the
// owner's 2026-11-19 decision only members holding a role with the Discord
// Administrator permission resolve through, and zero administrators resolves
// to no destination instead of an error.
func resolveHandoffAdminIDs(db *gorm.DB, guildID string) []string {
	if db == nil || strings.TrimSpace(guildID) == "" {
		return nil
	}
	admins := setup.GuildAdministratorDiscordIDs(guildID, setup.RuntimeDiscordBotToken(db))
	if len(admins) == 0 {
		return nil
	}
	resolved := make(map[string]struct{}, len(admins))
	for _, id := range admins {
		resolved[id] = struct{}{}
	}
	var users []model.UserMap
	if err := db.Where("discord_guild_id = ?", guildID).Find(&users).Error; err != nil {
		return nil
	}
	ids := make([]string, 0, len(users))
	for _, user := range users {
		if _, ok := resolved[strings.TrimSpace(user.DiscordID)]; !ok {
			continue
		}
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
