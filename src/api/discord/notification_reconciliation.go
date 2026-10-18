package discord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

const maxWebhookMessageReadbackBytes = 64 << 10

type verifiedWebhookMessage struct {
	ID        string  `json:"id"`
	ChannelID string  `json:"channel_id"`
	WebhookID string  `json:"webhook_id"`
	Content   string  `json:"content"`
	Embeds    []Embed `json:"embeds"`
}

// VerifyUnknownVFXFilesDelivery resolves an interrupted delivery only after
// an administrator supplies the candidate Discord message ID and the stored
// webhook credential reads back a message matching the immutable journal
// payload and destination. It never retries a POST.
func VerifyUnknownVFXFilesDelivery(db *gorm.DB, eventID, attemptID, messageID, threadID, actorEmail string, now time.Time) error {
	if db == nil || strings.TrimSpace(eventID) == "" || strings.TrimSpace(attemptID) == "" || strings.TrimSpace(messageID) == "" || strings.TrimSpace(actorEmail) == "" || now.IsZero() {
		return gorm.ErrInvalidData
	}
	var event model.NotificationDeliveryEvent
	if err := db.Where("event_id = ?", strings.TrimSpace(eventID)).First(&event).Error; err != nil {
		return err
	}
	if event.Disposition == model.DeliveryDelivered {
		var prior model.NotificationDeliveryReconciliationAudit
		if err := db.Where("event_id = ? AND attempt_id = ? AND provider_message_id = ? AND provider_thread_id = ?", event.EventID, attemptID, messageID, threadID).First(&prior).Error; err == nil && prior.Action == model.NotificationReconciliationProviderVerified {
			return nil
		}
		return model.ErrDeliveryReconciliationConflict
	}
	if event.Disposition != model.DeliveryUnknown || event.DeliverySource != "vfx_files" || event.AttemptID != strings.TrimSpace(attemptID) || event.AttemptStartedAt == nil {
		return model.ErrDeliveryReconciliationConflict
	}
	var webhook model.ProjectWebhook
	if err := db.Where("id = ? AND kitsu_project_id = ? AND discord_channel_id = ?", event.WebhookRowID, event.ProductionID, event.DestinationChannelID).First(&webhook).Error; err != nil {
		return model.ErrDeliveryReconciliationConflict
	}
	webhookID, err := DiscordWebhookIDFromURL(webhook.WebhookURL)
	if err != nil || webhookID != event.DestinationWebhookID {
		return model.ErrDeliveryReconciliationConflict
	}
	destination, err := model.ResolveNotificationDestination(db, event.ProductionID, event.TaskTypeID, webhook.WebhookURL, webhookID)
	if err != nil || destination.WebhookRowID != event.WebhookRowID || destination.GuildID != event.DestinationGuildID || destination.ChannelID != event.DestinationChannelID || destination.WebhookID != event.DestinationWebhookID {
		return model.ErrDeliveryReconciliationConflict
	}
	if err := validDiscordSnowflake(messageID); err != nil {
		return model.ErrDeliveryReconciliationConflict
	}
	if threadID != "" {
		if event.ThreadName == "" || validDiscordSnowflake(threadID) != nil {
			return model.ErrDeliveryReconciliationConflict
		}
	}
	messageAt, err := discordSnowflakeTime(messageID)
	if err != nil || messageAt.Before(event.AttemptStartedAt.Add(-2*time.Minute)) || messageAt.After(event.AttemptStartedAt.Add(2*time.Minute)) || messageAt.After(now.Add(2*time.Minute)) {
		return model.ErrDeliveryReconciliationConflict
	}
	messageURL := strings.TrimRight(webhook.WebhookURL, "/") + "/messages/" + url.PathEscape(messageID)
	if threadID != "" {
		messageURL += "?thread_id=" + url.QueryEscape(threadID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, messageURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	expectedChannel := event.DestinationChannelID
	if threadID != "" {
		expectedChannel = threadID
	}
	var expected Payload
	if err := json.Unmarshal([]byte(event.PayloadJSON), &expected); err != nil {
		return errors.New("stored Discord payload is invalid")
	}
	if !hasVFXFilesDeliveryMarker(expected, event.SourceEventID) {
		return errors.New("stored delivery has no exact-event correlation marker")
	}
	providerHash, err := verifyWebhookMessageReadback(ctx, client, request, expected, messageID, event.DestinationWebhookID, expectedChannel)
	if err != nil {
		return err
	}
	webhookURLHash := sha256.Sum256([]byte(webhook.WebhookURL))
	evidence := model.VerifiedNotificationDeliveryEvidence{
		EventID: event.EventID, AttemptID: event.AttemptID, ProductionID: event.ProductionID,
		WebhookRowID: event.WebhookRowID, DestinationGuildID: event.DestinationGuildID,
		DestinationChannelID: event.DestinationChannelID, DestinationWebhookID: event.DestinationWebhookID,
		MessageID: messageID, ThreadID: threadID, PayloadSHA256: event.PayloadSHA256,
		WebhookURLFingerprint:  hex.EncodeToString(webhookURLHash[:]),
		ProviderResponseSHA256: providerHash, ActorEmail: strings.TrimSpace(actorEmail), VerifiedAt: now.UTC(),
	}
	_, err = model.RecordProviderVerifiedUnknownDelivery(db, evidence)
	if err != nil {
		return fmt.Errorf("delivery reconciliation state changed: %w", err)
	}
	return nil
}

func hasVFXFilesDeliveryMarker(payload Payload, sourceEventID string) bool {
	if strings.TrimSpace(sourceEventID) == "" {
		return false
	}
	want := vfxFilesDeliveryMarkerPrefix + sourceEventID
	for _, embed := range payload.Embeds {
		if embed.Footer.Text == want {
			return true
		}
	}
	return false
}

func verifyWebhookMessageReadback(ctx context.Context, client *http.Client, request *http.Request, expected Payload, expectedMessageID, expectedWebhookID, expectedChannelID string) (string, error) {
	if client == nil || request == nil {
		return "", errors.New("Discord message readback is unavailable")
	}
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		return "", errors.New("Discord message readback failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxWebhookMessageReadbackBytes))
		return "", errors.New("Discord message could not be verified")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxWebhookMessageReadbackBytes+1))
	if err != nil || len(body) > maxWebhookMessageReadbackBytes {
		return "", errors.New("Discord message readback response was invalid")
	}
	var message verifiedWebhookMessage
	if err := json.Unmarshal(body, &message); err != nil || message.ID != expectedMessageID || message.WebhookID != expectedWebhookID {
		return "", errors.New("Discord message readback did not match the requested message")
	}
	if message.ChannelID != expectedChannelID {
		return "", errors.New("Discord message readback did not match the destination")
	}
	if message.Content != expected.Content || !reflect.DeepEqual(message.Embeds, expected.Embeds) {
		return "", errors.New("Discord message readback did not match the delivery payload")
	}
	canonical, err := json.Marshal(message)
	if err != nil {
		return "", errors.New("Discord message readback could not be fingerprinted")
	}
	providerHash := sha256.Sum256(canonical)
	return hex.EncodeToString(providerHash[:]), nil
}

func validDiscordSnowflake(value string) error {
	if len(value) < 17 || len(value) > 20 {
		return errors.New("invalid Discord snowflake")
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err
}

func discordSnowflakeTime(value string) (time.Time, error) {
	snowflake, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	millis := int64(snowflake>>22) + 1420070400000
	return time.UnixMilli(millis).UTC(), nil
}
