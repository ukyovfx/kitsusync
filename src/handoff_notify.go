package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/forum"
	"app/src/model"
	"app/src/setup"
)

// A handoff is reported immediately when the receiving task has a concrete
// assignee. A handoff that fell back to the admin role, or one nobody has
// answered yet, is only listed in the morning digest. The timing is derived
// from the signal kind (redesign ruling G, 2026-11-23): there is no timing
// setting anymore.
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

// productionLanguage resolves the Production's notification language
// ("ja"/"en"); unknown productions fall back to Japanese.
func productionLanguage(db *gorm.DB, kitsuProjectID string) string {
	if project := model.FindProjectByKitsuID(db, kitsuProjectID); project != nil {
		return discord.NotificationLanguage(project.Language)
	}
	return "ja"
}

// handoffImmediate reports whether a handoff with these recipients is sent now
// rather than kept for the morning digest: named recipients are immediate, a
// handoff with nobody named (admin fallback) waits for the digest.
func handoffImmediate(fallbackUsed string, recipientIDs []string) bool {
	if len(recipientIDs) == 0 {
		return false
	}
	return strings.TrimSpace(fallbackUsed) == model.HandoffFallbackNone
}

// sendImmediateHandoffs places a forum signal for every pending handoff
// whose recipient is a concrete assignee, in the receiving task's own post,
// and marks it notified once the signal is queued. Delivery itself is the
// forum outbox's job, so a queued signal is retried there instead of being
// lost with a failed send. Handoffs that do not qualify stay pending for
// the morning digest.
func sendImmediateHandoffs(db *gorm.DB, poller forum.Poller, payloads []kitsu.MessagePayload) (int, error) {
	if db == nil {
		return 0, nil
	}
	pending, err := model.PendingHandoffEvents(db)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	languages := map[string]string{}
	var items []forum.HandoffDispatch
	for _, event := range pending {
		recipients := handoffAssignees(db, event)
		if !handoffImmediate(event.FallbackUsed, recipients) {
			continue
		}
		language, known := languages[event.ProductionID]
		if !known {
			language = productionLanguage(db, event.ProductionID)
			languages[event.ProductionID] = language
		}
		if event.EpisodeKey != "" {
			// A refired episode says so on the card (fix C-5): the count
			// includes this episode, so the second completion reads 2回目.
			if count, err := model.CountHandoffEpisodes(db, event.SourceTaskID); err == nil && count >= 2 {
				event.EpisodeLabel = fmt.Sprintf("再確定（%d回目）", count)
			}
		}
		components, err := json.Marshal(handoffReplyRows(db, []model.HandoffEvent{event}, language))
		if err != nil {
			return 0, err
		}
		items = append(items, forum.HandoffDispatch{Event: event, Mentions: recipients, Components: components})
	}
	if len(items) == 0 {
		return 0, nil
	}
	queued, err := poller.DispatchHandoffs(payloads, items)
	if err != nil {
		return 0, err
	}
	sent := 0
	var errs []string
	for id := range queued {
		// Claim after queueing: the outbox row is dedupe-keyed by the
		// delivery's comment, so a racing poll cannot place it twice, and
		// claiming only once queued means a failed queue never marks a
		// handoff notified that nobody will deliver.
		claimed, claimErr := model.MarkHandoffNotified(db, id)
		if claimErr != nil {
			errs = append(errs, fmt.Sprintf("%d: %v", id, claimErr))
			continue
		}
		if claimed {
			sent++
		}
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
// receiving task in Kitsu. lang is the Production's notification language.
func handoffCardLine(event model.HandoffEvent, taskURL, lang string) string {
	var line string
	if discord.NotificationLanguage(lang) == "en" {
		line = fmt.Sprintf("%s: %s ← from %s's %s", event.EntityName, handoffProduct(event), event.SourceAuthorName, event.SourceTaskTypeName)
	} else {
		line = fmt.Sprintf("%s: %s ← %s の %s", event.EntityName, handoffProduct(event), event.SourceAuthorName, event.SourceTaskTypeName)
	}
	if taskURL != "" {
		line += " " + handoffTaskURL(taskURL, event)
	}
	return line
}

func handoffProduct(event model.HandoffEvent) string {
	return strings.TrimSpace(event.ProductName + " " + event.ProductVersion)
}

// handoffReplyRows is the receiver's reply for one handoff: a single OK
// （受け取り確認） button (ruling I, 2026-11-23 — no Retake button, no
// modal). The button's custom ID carries the event's row ID, the episode
// era's idempotency key. Discord's five-rows-per-message limit therefore
// caps a digest card at handoffDigestMaxItems handoffs. While nothing
// listens for presses the button is sent disabled: Discord rejects a
// non-link button that has no custom_id, so inert has to mean disabled,
// not ID-less. An already-answered event renders disabled and marked.
func handoffReplyRows(db *gorm.DB, events []model.HandoffEvent, lang string) []discord.ActionRow {
	wired := handoffRepliesWired(db)
	rows := make([]discord.ActionRow, 0, len(events))
	for _, event := range events {
		label := "OK"
		disabled := !wired
		if event.State == model.HandoffEventAnswered || event.State == model.HandoffEventRetaken {
			label = "OK（回答済み）"
			if discord.NotificationLanguage(lang) == "en" {
				label = "OK (answered)"
			}
			disabled = true
		}
		rows = append(rows, discord.ActionRow{Type: 1, Components: []discord.Button{
			{Type: 2, Style: discord.ButtonStyleSuccess, Label: label, CustomID: handoffReplyCustomID(event.ID), Disabled: disabled},
		}})
	}
	return rows
}

// handoffReplyCustomID names the reply button of one handoff event:
// handoff:ok:<event row ID>. The ID is always carried, even while replies
// are unwired: Discord requires a custom_id on every non-link button, so
// an unwired button is disabled instead of ID-less.
func handoffReplyCustomID(eventID uint) string {
	if eventID == 0 {
		return "handoff:ok:pending"
	}
	return fmt.Sprintf("handoff:ok:%d", eventID)
}

// handoffRepliesWired reports whether button presses are received. The
// /interactions endpoint verifies callbacks with the Discord application's
// public key, so replies are wired exactly when that key is configured:
// without it the endpoint rejects every callback and an enabled button
// would be a dead button.
func handoffRepliesWired(db *gorm.DB) bool {
	return setup.DiscordApplicationPublicKey(db) != ""
}

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
