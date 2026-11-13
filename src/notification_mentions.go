package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/setup"
	stdslog "log/slog"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

// Mention DMs notify a user when a new comment mentions them. The capability
// probe is the poll's own comment fetch: a 403 on the comments endpoint marks
// comments unreadable, disables mention DMs and records the state durably.
const (
	commentsReadableSettingKey       = "comments.readable"
	mentionDailyCap                  = 5
	mentionCountSettingPrefix        = "mention.dm.count."
	mentionExcessSettingPrefix       = "mention.excess.count."
	mentionExcessDigestSectionLabel  = "メンション（超過分）"
)

// mentionCapabilityState tracks the last probe outcome so the durable Setting
// is only written when the capability flips.
var mentionCapabilityState = -1 // -1 unknown, 0 unreadable, 1 readable

// recordCommentReadCapability durably records whether the bot could read
// comments this poll. State changes are logged; mention DMs consult the
// latest payload flag directly.
func recordCommentReadCapability(db *gorm.DB, readable bool) {
	state := 0
	if readable {
		state = 1
	}
	if mentionCapabilityState == state {
		return
	}
	mentionCapabilityState = state
	if db == nil {
		return
	}
	model.SetSetting(db, commentsReadableSettingKey, strconv.FormatBool(readable))
	if readable {
		stdslog.Info("Comment reading available; mention DMs enabled")
	} else {
		stdslog.Warn("Comment reading unavailable (403 or fetch failure); mention DMs disabled", "error_class", "comments_unreadable")
	}
}

// dispatchMentionDMs extracts @mentions from a new comment and DMs the
// mentioned users through the bot transport. The observed comment checkpoint
// deduplicates re-offers; the daily cap keeps a single user from being flooded,
// with the excess counted for the morning digest.
func dispatchMentionDMs(db *gorm.DB, payload kitsu.MessagePayload) {
	if db == nil || payload.CommentsUnavailable {
		return
	}
	recordCommentReadCapability(db, true)
	commentText := payload.LatestComment.Comment.Text
	if strings.TrimSpace(commentText) == "" {
		return
	}
	mentioned := mentionedDiscordIDs(db, commentText)
	if len(mentioned) == 0 {
		return
	}
	botToken := setup.RuntimeDiscordBotToken(db)
	if strings.TrimSpace(botToken) == "" {
		stdslog.Warn("Mention DM skipped: bot token missing", "error_class", "dm_bot_token_missing")
		return
	}
	today := time.Now().In(jst).Format("2006-01-02")
	taskName := payload.Entity.Name
	if strings.TrimSpace(taskName) == "" {
		taskName = payload.Task.ID
	}
	content := fmt.Sprintf("💬 %s — %s さんのコメントでメンションされました:\n%s",
		taskName, strings.TrimSpace(payload.LatestComment.Author.FullName), excerpt(commentText, 200))
	if link := productionTaskLink(payload); !strings.Contains(link, "not supplied") {
		content += "\n" + link
	}
	for _, recipient := range mentioned {
		countKey := mentionCountSettingPrefix + recipient + "." + today
		count, err := strconv.Atoi(model.GetSetting(db, countKey))
		if err != nil {
			count = 0
		}
		if count >= mentionDailyCap {
			excessKey := mentionExcessSettingPrefix + recipient + "." + today
			excess, err := strconv.Atoi(model.GetSetting(db, excessKey))
			if err != nil {
				excess = 0
			}
			model.SetSetting(db, excessKey, strconv.Itoa(excess+1))
			stdslog.Info("Mention DM deferred to digest (daily cap)", "recipient", recipient, "taskID", payload.Task.ID)
			continue
		}
		// The cap counts DELIVERED mentions only: a failed send must not
		// consume quota, so the next mention of this user retries.
		if err := sendDigestDirectMessage(botToken, recipient, content); err != nil {
			stdslog.Warn("Mention DM failed; quota not consumed", "error_class", "mention_dm_failed", "recipient", recipient, "err", err)
			continue
		}
		model.SetSetting(db, countKey, strconv.Itoa(count+1))
	}
}

// mentionedDiscordIDs matches @<kitsu name> tokens in the comment text against
// the user map and returns the mentioned users' Discord IDs. Matching requires
// the complete name: the characters around the match must not continue a name
// (or longer name), so @ゆい never matches @ゆいと.
func mentionedDiscordIDs(db *gorm.DB, commentText string) []string {
	normalized := strings.ToLower(commentText)
	seen := map[string]bool{}
	var ids []string
	for _, user := range model.ListUserMap(db) {
		name := strings.ToLower(strings.TrimSpace(user.KitsuName))
		id := strings.TrimSpace(user.DiscordID)
		if name == "" || id == "" || seen[id] {
			continue
		}
		if containsCompleteMention(normalized, name) {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// containsCompleteMention reports whether text contains @name as a complete
// token. A match is rejected when the character directly after the name (or
// directly before the @) continues a name, which is how short names would
// otherwise swallow longer ones.
func containsCompleteMention(text, name string) bool {
	needle := "@" + name
	for start := 0; ; {
		at := strings.Index(text[start:], needle)
		if at < 0 {
			return false
		}
		start += at
		end := start + len(needle)
		beforeOK := start == 0 || !continuesName(text, start-1)
		afterOK := end >= len(text) || !continuesName(text, end)
		if beforeOK && afterOK {
			return true
		}
		start++
	}
}

// continuesName reports whether the rune at the byte offset extends a name
// (letters and digits, which covers CJK names too).
func continuesName(text string, offset int) bool {
	r := []rune(text[offset:])[0]
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// mentionExcessForToday returns the recipient's excess-mention count for the
// digest card.
func mentionExcessForToday(db *gorm.DB, recipientID string, now time.Time) int {
	key := mentionExcessSettingPrefix + recipientID + "." + now.In(jst).Format("2006-01-02")
	count, err := strconv.Atoi(model.GetSetting(db, key))
	if err != nil {
		return 0
	}
	return count
}

func excerpt(text string, max int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r", ""))
	if len(text) <= max {
		return text
	}
	return text[:max] + "…"
}
