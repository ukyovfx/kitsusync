package discord

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"app/src/api/vfxfiles"
	"app/src/model"
	"gorm.io/gorm"
)

// ProcessVFXFilesDeliveryEvents walks a fixed inbox high-water mark in bounded
// pages. Deferred rows remain durable but cannot pin the first page ahead of
// later events. It never mentions Discord users/roles or exposes Drive refs.
func ProcessVFXFilesDeliveryEvents(db *gorm.DB, limit int) (processed, deferred int, err error) {
	now := time.Now().UTC()
	highWatermark, err := model.VFXFilesDeliveryEventHighWatermark(db, now)
	if err != nil {
		return 0, 0, err
	}
	var afterID uint
	for afterID < highWatermark {
		rows, pageErr := model.PendingVFXFilesDeliveryEventsPage(db, afterID, highWatermark, limit, now)
		if pageErr != nil {
			return processed, deferred, pageErr
		}
		if len(rows) == 0 {
			break
		}
		afterID = rows[len(rows)-1].ID
		for _, row := range rows {
			result := processVFXFilesDeliveryEvent(db, row)
			if result == "processed" {
				processed++
			} else if result == "deferred" {
				deferred++
			}
		}
	}
	return processed, deferred, nil
}

func processVFXFilesDeliveryEvent(db *gorm.DB, row model.VFXFilesEventInbox) string {
	event, err := vfxfiles.DecodeFileDeliveryNotification([]byte(row.PayloadJSON))
	if err != nil || event.EventID != row.EventID || !strings.EqualFold(event.ProductionID, row.ProductionID) || event.RecipientTaskID != row.RecipientTaskID || event.RecipientTaskTypeID != row.RecipientTaskTypeID {
		_ = model.QuarantineVFXFilesEvent(db, row.EventID, "invalid_delivery_payload", time.Now().UTC())
		return "processed"
	}
	var config model.VFXFilesProductionConfig
	if err := db.Where("production_id = ?", row.ProductionID).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = model.QuarantineVFXFilesEvent(db, row.EventID, "integration_not_configured", time.Now().UTC())
			return "processed"
		}
		return "deferred"
	}
	if !config.Activated || config.ActivationEpoch == 0 || config.ActivationEpoch != row.ActivationEpoch {
		_ = model.QuarantineVFXFilesEvent(db, row.EventID, "activation_epoch_mismatch", time.Now().UTC())
		return "processed"
	}
	project := model.FindProjectByKitsuID(db, row.ProductionID)
	if project == nil || project.ValidationOnly {
		_ = model.QuarantineVFXFilesEvent(db, row.EventID, "production_unavailable", time.Now().UTC())
		return "processed"
	}
	var route model.ProductionNotificationRoute
	if err := db.Where("production_id = ? AND task_type_id = ?", row.ProductionID, event.RecipientTaskTypeID).First(&route).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = model.QuarantineVFXFilesEvent(db, row.EventID, "notification_route_missing", time.Now().UTC())
			return "processed"
		}
		return "deferred"
	}
	webhook := model.FindProjectWebhookByID(db, route.DestinationWebhookID)
	if webhook == nil || webhook.KitsuProjectID != row.ProductionID {
		_ = model.QuarantineVFXFilesEvent(db, row.EventID, "notification_destination_missing", time.Now().UTC())
		return "processed"
	}
	webhookID, err := DiscordWebhookIDFromURL(webhook.WebhookURL)
	if err != nil {
		_ = model.QuarantineVFXFilesEvent(db, row.EventID, "notification_destination_invalid", time.Now().UTC())
		return "processed"
	}
	destination, err := model.ResolveNotificationDestination(db, row.ProductionID, event.RecipientTaskTypeID, webhook.WebhookURL, webhookID)
	if err != nil {
		if errors.Is(err, model.ErrDeliveryUnavailable) || errors.Is(err, gorm.ErrRecordNotFound) {
			_ = model.QuarantineVFXFilesEvent(db, row.EventID, "notification_destination_unavailable", time.Now().UTC())
			return "processed"
		}
		return "deferred"
	}
	revision, err := model.ProductionNotificationRevisionWithError(db, project.ID, row.ProductionID)
	if err != nil {
		return "deferred"
	}
	journalTaskID := vfxFilesJournalTaskID(event.DeliveryID)
	previous, err := model.LatestDeliveredNotificationForDestination(db, journalTaskID, destination)
	if err != nil {
		return "deferred"
	}
	priorMessageID, priorThreadID := "", ""
	if previous != nil {
		priorMessageID, priorThreadID = previous.DiscordMessageID, previous.DiscordThreadID
	}
	payload := fileDeliveryPayload(db, event, row.ProductionID, event.RecipientTaskID)
	encoded, err := json.Marshal(payload)
	if err != nil {
		_ = model.QuarantineVFXFilesEvent(db, row.EventID, "notification_payload_invalid", time.Now().UTC())
		return "processed"
	}
	intent := model.NotificationDeliveryIntent{
		TaskID: journalTaskID, ProductionID: row.ProductionID, TaskTypeID: event.RecipientTaskTypeID,
		TaskStatus:     "FILE_" + strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(event.EventType, "file_delivery."), "-", "_")),
		DeliverySource: "vfx_files", SourceEventID: event.EventID, TaskUpdatedAt: event.OccurredAt,
		Payload: encoded, WebhookRowID: destination.WebhookRowID, GuildID: destination.GuildID, ChannelID: destination.ChannelID,
		DiscordWebhookID: destination.WebhookID, NotificationRevision: revision, ThreadName: "Files · " + sanitizeDiscordText(event.EntityName),
		PriorMessageID: priorMessageID, PriorThreadID: priorThreadID,
	}
	prepared, err := model.PrepareNotificationDelivery(db, intent)
	if errors.Is(err, model.ErrVFXFilesActivationEpochFenced) {
		return "processed"
	}
	if err != nil || prepared == nil {
		if errors.Is(err, model.ErrDeliveryIdentityConflict) {
			_ = model.QuarantineVFXFilesEvent(db, row.EventID, "delivery_identity_conflict", time.Now().UTC())
			return "processed"
		}
		return "deferred"
	}
	sent := dispatchPreparedNotification(db, *prepared, webhook.WebhookURL)
	disposition, dispositionErr := model.NotificationDeliveryDisposition(db, prepared.EventID)
	if dispositionErr != nil {
		return "deferred"
	}
	if sent.Durable || sent.AlreadyDelivered || sent.Unknown || disposition == model.DeliveryDelivered || disposition == model.DeliveryUnknown || disposition == model.DeliveryTerminalFailure || disposition == model.DeliveryLegacyUnknown {
		if _, err := model.MarkVFXFilesEventProcessed(db, row.EventID, time.Now().UTC()); err != nil {
			return "deferred"
		}
		return "processed"
	}
	return "deferred"
}

// vfxFilesJournalTaskID separates each Plugin recipient delivery stream from
// native Kitsu Task notifications. The real recipient Task ID remains in the
// rendered Kitsu link and inbox, but must never share journal message/thread
// replacement state with native status notifications.
func vfxFilesJournalTaskID(deliveryID string) string {
	return "vfx-files-delivery:" + strings.ToLower(strings.TrimSpace(deliveryID))
}

func fileDeliveryPayload(db *gorm.DB, event vfxfiles.FileDeliveryNotification, productionID, taskID string) Payload {
	lang := "en"
	if project := model.FindProjectByKitsuID(db, productionID); project != nil && strings.EqualFold(strings.TrimSpace(project.Language), "ja") {
		lang = "ja"
	}
	status := strings.TrimPrefix(event.EventType, "file_delivery.")
	label := func(ja, en string) string {
		if lang == "ja" {
			return ja
		}
		return en
	}
	fields := []EmbedField{
		{Name: label("Production", "Production"), Value: boundedDiscordField(event.ProductionName, productionID), Inline: true},
		{Name: label("対象", "Entity"), Value: boundedDiscordField(event.EntityName, event.EntityType), Inline: true},
		{Name: label("送信元", "From"), Value: boundedDiscordField(event.SourceTaskTypeName, event.SourceTaskTypeID), Inline: true},
		{Name: label("送信先", "To"), Value: boundedDiscordField(event.RecipientTaskTypeName, event.RecipientTaskTypeID), Inline: true},
		{Name: label("送信者", "Sender"), Value: boundedDiscordField(event.SenderName, "—"), Inline: true},
		{Name: label("出力", "Output"), Value: fmt.Sprintf("%s · v%03d", boundedDiscordField(event.OutputClassification, "Output"), event.OutputVersion), Inline: true},
		{Name: label("配布状態", "Delivery"), Value: boundedDiscordField(status, "updated"), Inline: true},
	}
	link := ""
	if KitsuPublicURLResolver != nil {
		link = KitsuTaskURL(KitsuPublicURLResolver(), productionID, event.EntityType, taskID)
	}
	embed := Embed{Title: label("ファイル配布 · "+strings.ToUpper(status), "File delivery · "+strings.ToUpper(status)), Color: 0x64748B, Fields: fields}
	if link != "" {
		embed.Url = link
	}
	return Payload{Username: "KitsuSync", Content: "", Embeds: []Embed{embed}, AllowedMentions: &AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}}}
}

func boundedDiscordField(value, fallback string) string {
	value = sanitizeDiscordText(strings.TrimSpace(value))
	if value == "" {
		value = fallback
	}
	if len([]rune(value)) > 900 {
		value = string([]rune(value)[:900])
	}
	return value
}
