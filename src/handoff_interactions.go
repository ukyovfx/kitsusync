package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"app/src/api/discord"
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/setup"
)

// Discord interaction types and response types used by the handoff reply
// endpoint. See the Discord developer docs for the full tables.
const (
	interactionTypePing             = 1
	interactionTypeMessageComponent = 3

	interactionResponsePong          = 1
	interactionResponseMessage       = 4
	interactionResponseUpdateMessage = 7

	interactionEphemeralFlag = 64

	// handoffReplierMarkerPrefix starts the attribution line of a
	// ghostwritten reply comment, mirroring the kitsusync-submitter marker
	// the Web Publisher writes into its comments (see forum/planner.go).
	// Zou records the bot account as the comment's author, so the human who
	// actually answered is named on this line instead.
	handoffReplierMarkerPrefix = "kitsusync-replier:"
)

// discordInteraction is the subset of Discord's interaction callback body
// the handoff reply flow reads.
type discordInteraction struct {
	ID        string `json:"id"`
	Type      int    `json:"type"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	Data      *struct {
		CustomID string `json:"custom_id"`
	} `json:"data"`
	Member *struct {
		User discordInteractionUser `json:"user"`
	} `json:"member"`
	User    *discordInteractionUser `json:"user"`
	Message *struct {
		Content    string              `json:"content"`
		Embeds     []discord.Embed     `json:"embeds"`
		Components []discord.ActionRow `json:"components"`
	} `json:"message"`
}

type discordInteractionUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

// clicker returns the Discord user who pressed, from the guild member when
// present and from the top-level user in DMs.
func (in discordInteraction) clicker() discordInteractionUser {
	if in.Member != nil && strings.TrimSpace(in.Member.User.ID) != "" {
		return in.Member.User
	}
	if in.User != nil {
		return *in.User
	}
	return discordInteractionUser{}
}

type interactionResponse struct {
	Type int                    `json:"type"`
	Data map[string]interface{} `json:"data,omitempty"`
}

// verifyDiscordInteractionSignature checks Discord's Ed25519 signature over
// timestamp+body against the application's public key (hex).
func verifyDiscordInteractionSignature(publicKeyHex, signatureHex, timestamp string, body []byte) bool {
	publicKey, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	signature, err := hex.DecodeString(strings.TrimSpace(signatureHex))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	message := make([]byte, 0, len(timestamp)+len(body))
	message = append(message, timestamp...)
	message = append(message, body...)
	return ed25519.Verify(ed25519.PublicKey(publicKey), message, signature)
}

// handoffInteractionsHandler serves POST /interactions, the callback
// Discord invokes for the handoff OK button. It is deliberately not behind
// the admin session: Discord's servers call it, and the Ed25519 signature
// is the authentication. Per ruling I (2026-11-23) the only component this
// endpoint serves is the OK （受け取り確認） button — there is no Retake
// button and no modal.
func handoffInteractionsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "unreadable request body", http.StatusBadRequest)
			return
		}
		publicKey := setup.DiscordApplicationPublicKey(db)
		if publicKey == "" || !verifyDiscordInteractionSignature(publicKey,
			r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"), body) {
			// Fail closed: without the key nothing can be verified, and a
			// bad signature is not a press kitsusync may act on.
			http.Error(w, "invalid request signature", http.StatusUnauthorized)
			return
		}
		var interaction discordInteraction
		if err := json.Unmarshal(body, &interaction); err != nil {
			http.Error(w, "invalid interaction payload", http.StatusBadRequest)
			return
		}
		switch interaction.Type {
		case interactionTypePing:
			writeInteractionResponse(w, interactionResponse{Type: interactionResponsePong})
		case interactionTypeMessageComponent:
			handleHandoffButton(db, w, r, interaction)
		default:
			http.Error(w, "unsupported interaction type", http.StatusBadRequest)
		}
	}
}

func writeInteractionResponse(w http.ResponseWriter, response interactionResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Warn("handoff interaction response write failed", "err", err)
	}
}

// ephemeralInteractionMessage answers only the pressing user.
func ephemeralInteractionMessage(w http.ResponseWriter, text string) {
	writeInteractionResponse(w, interactionResponse{
		Type: interactionResponseMessage,
		Data: map[string]interface{}{"content": text, "flags": interactionEphemeralFlag},
	})
}

// parseHandoffButtonCustomID splits handoff:ok:<event row ID> (core v2 §5:
// buttons are keyed by the episode-era event ID, not the delivery comment).
func parseHandoffButtonCustomID(customID string) (uint, bool) {
	parts := strings.SplitN(strings.TrimSpace(customID), ":", 3)
	if len(parts) != 3 || parts[0] != "handoff" || parts[1] != "ok" {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

// handoffForInteraction resolves the event a press refers to and checks the
// press may act on it: the event must exist, still wait for a reply, and be
// addressed to the pressing user. A non-empty refusal is the ephemeral text
// to show the presser; the caller sends it and stops.
func handoffForInteraction(db *gorm.DB, interaction discordInteraction, eventID uint) (model.HandoffEvent, string, string) {
	event, err := model.FindHandoffEventByID(db, eventID)
	if err != nil {
		return model.HandoffEvent{}, "ja", handoffInteractionText("ja", "not_found")
	}
	lang := productionLanguage(db, event.ProductionID)
	if event.State != model.HandoffEventPending && event.State != model.HandoffEventNotified {
		return model.HandoffEvent{}, lang, handoffInteractionText(lang, "already_answered")
	}
	clicker := interaction.clicker()
	for _, recipientID := range handoffRecipients(db, event) {
		if recipientID == strings.TrimSpace(clicker.ID) {
			return event, lang, ""
		}
	}
	return model.HandoffEvent{}, lang, handoffInteractionText(lang, "not_recipient")
}

// handleHandoffButton answers an OK press.
func handleHandoffButton(db *gorm.DB, w http.ResponseWriter, r *http.Request, interaction discordInteraction) {
	if interaction.Data == nil {
		http.Error(w, "invalid interaction payload", http.StatusBadRequest)
		return
	}
	eventID, ok := parseHandoffButtonCustomID(interaction.Data.CustomID)
	if !ok {
		http.Error(w, "unknown component", http.StatusBadRequest)
		return
	}
	event, lang, refusal := handoffForInteraction(db, interaction, eventID)
	if refusal != "" {
		ephemeralInteractionMessage(w, refusal)
		return
	}
	answerHandoff(db, w, r, interaction, event, lang)
}

// answerHandoff claims the reply, ghostwrites it to Kitsu, and updates the
// pressed message. The claim (MarkHandoffReplied's CAS) comes first, as the
// model intends: a double press never writes a second comment. When the
// Kitsu write fails the claim is returned, so the receiver can press again
// instead of believing a reply landed that never did.
func answerHandoff(db *gorm.DB, w http.ResponseWriter, r *http.Request, interaction discordInteraction, event model.HandoffEvent, lang string) {
	claimed, err := model.MarkHandoffReplied(db, event.ID, model.HandoffEventAnswered)
	if err != nil {
		slog.Warn("handoff reply claim failed", "event_id", event.ID, "err", err)
		ephemeralInteractionMessage(w, handoffInteractionText(lang, "failed"))
		return
	}
	if !claimed {
		ephemeralInteractionMessage(w, handoffInteractionText(lang, "already_answered"))
		return
	}
	replierName := handoffReplierName(db, interaction)
	text := handoffReplyCommentText(event, replierName, lang)
	if err := writeHandoffReplyComment(r.Context(), event, text); err != nil {
		slog.Warn("handoff reply comment write failed", "event_id", event.ID, "err", err)
		if revertErr := db.Model(&model.HandoffEvent{}).
			Where("id = ? AND state = ?", event.ID, model.HandoffEventAnswered).
			Update("state", event.State).Error; revertErr != nil {
			slog.Warn("handoff reply claim revert failed", "event_id", event.ID, "err", revertErr)
		}
		model.WriteAuditLog(db, model.AuditLog{
			TaskID:     event.SourceTaskID,
			ProjectID:  event.ProductionID,
			EntityName: event.EntityName,
			TaskType:   event.SourceTaskTypeName,
			NewStatus:  fmt.Sprintf("handoff reply comment write failed: event %d", event.ID),
			Success:    false,
			ActorKind:  model.AuditActorHuman,
			ActorName:  replierName,
		})
		ephemeralInteractionMessage(w, handoffInteractionText(lang, "failed"))
		return
	}
	model.WriteAuditLog(db, model.AuditLog{
		TaskID:     event.SourceTaskID,
		ProjectID:  event.ProductionID,
		EntityName: event.EntityName,
		TaskType:   event.SourceTaskTypeName,
		NewStatus:  fmt.Sprintf("handoff replied answered: event %d", event.ID),
		Success:    true,
		ActorKind:  model.AuditActorHuman,
		ActorName:  replierName,
	})
	data := map[string]interface{}{
		"components":       handoffAnsweredRows(interaction, event.ID, lang),
		"allowed_mentions": map[string]interface{}{"parse": []string{}},
	}
	if interaction.Message != nil {
		data["content"] = interaction.Message.Content
		if interaction.Message.Embeds != nil {
			data["embeds"] = interaction.Message.Embeds
		}
	}
	writeInteractionResponse(w, interactionResponse{Type: interactionResponseUpdateMessage, Data: data})
}

// errHandoffSourceStatusUnknown marks a source task whose current status
// could not be read; the ghostwrite must not guess a status.
var errHandoffSourceStatusUnknown = errors.New("handoff source task status unknown")

// writeHandoffReplyComment posts the ghostwritten reply on the SOURCE task
// (core v2 §5): the thread the source artist reads lives there. The comment
// carries the source task's CURRENT status only — episode-era handoffs
// have no delivery comment to fall back to — so writing it never moves the
// task: kitsusync reports, Kitsu decides.
var writeHandoffReplyComment = func(ctx context.Context, event model.HandoffEvent, text string) error {
	task, err := kitsu.GetTaskWithContext(ctx, event.SourceTaskID)
	if err != nil {
		return err
	}
	statusID := strings.TrimSpace(task.TaskStatusID)
	if statusID == "" {
		return errHandoffSourceStatusUnknown
	}
	_, err = kitsu.PostTaskCommentWithContext(ctx, event.SourceTaskID, text, statusID)
	return err
}

// handoffReplyCommentText builds the ghostwritten comment. The reply itself
// comes first; the replier marker closes it, the same shape the Web
// Publisher's submitter marker uses.
func handoffReplyCommentText(event model.HandoffEvent, replierName, lang string) string {
	body := "OK（受け取りを確認しました）"
	if discord.NotificationLanguage(lang) == "en" {
		body = "OK — received."
	}
	if strings.TrimSpace(replierName) != "" {
		body += "\n" + handoffReplierMarkerPrefix + " " + strings.TrimSpace(replierName)
	}
	return body
}

// handoffReplierName names the pressing human for the ghostwrite marker:
// the Kitsu name recorded in the user map when there is one, then the
// Discord display name, then the username.
func handoffReplierName(db *gorm.DB, interaction discordInteraction) string {
	clicker := interaction.clicker()
	if db != nil && strings.TrimSpace(clicker.ID) != "" {
		query := db.Where("discord_id = ?", strings.TrimSpace(clicker.ID))
		if strings.TrimSpace(interaction.GuildID) != "" {
			query = query.Where("discord_guild_id = ?", strings.TrimSpace(interaction.GuildID))
		}
		var userMap model.UserMap
		if err := query.First(&userMap).Error; err == nil && strings.TrimSpace(userMap.KitsuName) != "" {
			return singleLine(userMap.KitsuName)
		}
	}
	if name := strings.TrimSpace(clicker.GlobalName); name != "" {
		return singleLine(name)
	}
	return singleLine(clicker.Username)
}

// singleLine keeps a display name from breaking the marker line's shape.
func singleLine(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
}

// handoffAnsweredRows returns the pressed message's rows with the answered
// event's OK button disabled and marked （回答済み）. Rows of other handoffs
// on the same message (a digest carries up to five) stay live.
func handoffAnsweredRows(interaction discordInteraction, eventID uint, lang string) []discord.ActionRow {
	if interaction.Message == nil {
		return []discord.ActionRow{}
	}
	rows := make([]discord.ActionRow, len(interaction.Message.Components))
	copy(rows, interaction.Message.Components)
	suffix := "（回答済み）"
	if discord.NotificationLanguage(lang) == "en" {
		suffix = " (answered)"
	}
	target := handoffReplyCustomID(eventID)
	for i, row := range rows {
		matched := false
		components := make([]discord.Button, len(row.Components))
		copy(components, row.Components)
		for j, button := range components {
			if button.CustomID != target {
				continue
			}
			matched = true
			components[j].Disabled = true
			components[j].Label += suffix
		}
		if matched {
			rows[i].Components = components
		}
	}
	return rows
}

// handoffInteractionText localizes the endpoint's own messages.
func handoffInteractionText(lang, key string) string {
	en := discord.NotificationLanguage(lang) == "en"
	switch key {
	case "not_found":
		if en {
			return "This handoff could not be found."
		}
		return "この受け渡しが見つかりませんでした。"
	case "already_answered":
		if en {
			return "This handoff has already been answered."
		}
		return "この受け渡しはすでに回答済みです。"
	case "not_recipient":
		if en {
			return "This handoff is not addressed to you."
		}
		return "この受け渡しの宛先ではありません。"
	case "failed":
		if en {
			return "Writing the reply to Kitsu failed. Please press again."
		}
		return "Kitsuへの書き込みに失敗しました。もう一度押してください。"
	}
	return ""
}
