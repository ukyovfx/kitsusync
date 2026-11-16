package main

import (
	"app/src/api/discord"
	"app/src/utils/config"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"
)

// The morning digest is a daily 09:00 JST DM card per configured reviewer.
// It only records one line per production in the audit trail; the delivery
// ledger remains the source of truth for individual notifications. Markers
// are per recipient so a partial failure retries only the failed destinations.
const (
	digestHourJST            = 9
	digestMaxTasksPerSection = 10
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
	if _, err := sendForumMorningDigests(conf, db, now, now.Format("2006-01-02")); err != nil {
		slog.Error("Forum morning digest partially failed", "error_class", "forum_digest_send_failed", "err", err)
	}
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
