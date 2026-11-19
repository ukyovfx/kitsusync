package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	NotificationPayloadRedactionBatchSize = 100

	DeliveryPending         = "pending"
	DeliveryInFlight        = "in_flight"
	DeliveryDelivered       = "delivered"
	DeliveryRetryable       = "retryable"
	DeliveryUnknown         = "unknown"
	DeliveryTerminalFailure = "terminal_failure"
	DeliveryLegacyUnknown   = "legacy_unknown"
	DeliveryRecorded        = "recorded"

	ChannelDeletionPending   = "pending"
	ChannelDeletionComplete  = "complete"
	ChannelDeletionNeedsHelp = "needs_help"

	NotificationMutationReordering   = "reordering"
	NotificationMutationDeleting     = "deleting"
	NotificationMutationDeletionHelp = "deletion_needs_help"
	NotificationMutationReorderHelp  = "reorder_needs_help"
)

var (
	ErrChannelDeletionConflict              = errors.New("managed channel deletion conflict")
	ErrNotificationDeliveryInFlight         = errors.New("notification delivery is in flight")
	ErrNotificationMutationRecoveryRequired = errors.New("production notification mutation requires recovery")
)

// NotificationDeliveryEvent is a durable, destination-bound dispatch intent.
// PayloadJSON is the exact Discord request body and may contain user-authored
// content. Webhook URLs/tokens are deliberately never copied into this journal.
type NotificationDeliveryEvent struct {
	ID                     uint   `gorm:"primaryKey"`
	EventID                string `gorm:"not null;uniqueIndex;size:64"`
	TaskID                 string `gorm:"not null;index"`
	ProductionID           string `gorm:"not null;index"`
	TaskTypeID             string `gorm:"not null"`
	TaskStatus             string `gorm:"not null"`
	TaskUpdatedAt          string
	CommentID              string
	CommentUpdatedAt       string
	IsAssignNotification   bool
	LaneType               string `gorm:"not null;default:channel;index"`
	DestinationTarget      string
	PayloadJSON            string `gorm:"type:text;not null"`
	PayloadSHA256          string `gorm:"not null;size:64"`
	PayloadRedactedAt      *time.Time
	WebhookRowID           uint   `gorm:"not null;index"`
	DestinationGuildID     string `gorm:"not null;index"`
	DestinationChannelID   string `gorm:"not null;index"`
	DestinationWebhookID   string `gorm:"not null;index"`
	DestinationFingerprint string `gorm:"not null;size:64"`
	NotificationRevision   string `gorm:"size:64"`
	Disposition            string `gorm:"not null;index"`
	AttemptID              string `gorm:"size:32"`
	AttemptCount           int
	AttemptStartedAt       *time.Time
	DeliveredAt            *time.Time
	DiscordMessageID       string
	DiscordThreadID        string
	ThreadName             string
	PriorMessageID         string
	PriorThreadID          string
	PriorCleanupPending    bool `gorm:"not null;default:false;index"`
	PriorCleanupInFlight   bool `gorm:"not null;default:false;index"`
	// Lifecycle cleanup implements the channel-as-queue rule: when a task
	// leaves WFA, its waiting-for-check post is deleted. The flags mirror the
	// prior-message cleanup machinery so the sweeper can claim work safely.
	LifecycleCleanupPending  bool `gorm:"not null;default:false;index"`
	LifecycleCleanupInFlight bool `gorm:"not null;default:false;index"`
	LifecycleDeletedAt       *time.Time
	FailureClass             string
	SupersededByEventID      string `gorm:"size:64;index"`
	CleanupFailureClass      string
	IdentityConflictCount    int        `gorm:"not null;default:0" json:"-"`
	IdentityConflictAt       *time.Time `json:"-"`
	CreatedAt                time.Time  `gorm:"index"`
	UpdatedAt                time.Time
}

// ManagedChannelDeletion is a recoverable tombstone. A pending row prevents
// dispatch before the external DELETE; completion and local cleanup commit
// together after an idempotently verified external deletion.
type ManagedChannelDeletion struct {
	ID               uint   `gorm:"primaryKey"`
	WebhookRowID     uint   `gorm:"not null;uniqueIndex"`
	ProductionID     string `gorm:"not null;index"`
	GuildID          string `gorm:"not null;index"`
	ChannelID        string `gorm:"not null;index"`
	DiscordWebhookID string `gorm:"not null;index"`
	Disposition      string `gorm:"not null;index"`
	FailureClass     string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

// ProductionNotificationWriteLock is a database-level serialization point
// shared by Apply, direct Reviewer mutation, dispatch claims and deletion.
// SQLite's single-writer transaction semantics make this effective across
// processes, unlike the in-process HTTP mutex.
type ProductionNotificationWriteLock struct {
	ID            uint   `gorm:"primaryKey"`
	ProductionID  string `gorm:"not null;uniqueIndex"`
	Revision      uint64 `gorm:"not null;default:0"`
	MutationState string `gorm:"not null;default:'';index"`
	MutationToken string `gorm:"size:64"`
	MutationAt    *time.Time
	UpdatedAt     time.Time
}

func (ProductionNotificationWriteLock) TableName() string {
	return "production_notification_write_locks"
}

func EnsureNoInFlightNotificationDeliveries(tx *gorm.DB, productionID string) error {
	var count int64
	if err := tx.Model(&NotificationDeliveryEvent{}).Where("production_id = ? AND disposition = ?", strings.TrimSpace(productionID), DeliveryInFlight).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrNotificationDeliveryInFlight
	}
	return nil
}

// notificationWebhookID extracts the webhook snowflake from a trusted
// discord.com webhook URL; empty when the URL is not a canonical webhook URL.
func notificationWebhookID(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "discord.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "webhooks" || parts[2] == "" || parts[3] == "" {
		return ""
	}
	return strings.TrimSpace(parts[2])
}

func AcquireProductionNotificationWriteLock(tx *gorm.DB, productionID string) error {
	productionID = strings.TrimSpace(productionID)
	if tx == nil || productionID == "" {
		return gorm.ErrInvalidData
	}
	row := ProductionNotificationWriteLock{ProductionID: productionID}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "production_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	result := tx.Model(&ProductionNotificationWriteLock{}).Where("production_id = ?", productionID).UpdateColumn("revision", gorm.Expr("revision + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrProductionNotificationRevisionConflict
	}
	return nil
}
func EnsureNoUnresolvedNotificationDeliveries(tx *gorm.DB, productionID string) error {
	var count int64
	if err := tx.Model(&NotificationDeliveryEvent{}).Where("production_id = ? AND disposition IN ?", strings.TrimSpace(productionID), []string{DeliveryPending, DeliveryRetryable, DeliveryInFlight, DeliveryUnknown}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrNotificationDeliveryInFlight
	}
	return nil
}
func EnsureProductionNotificationMutationIdle(tx *gorm.DB, productionID, allowedToken string) error {
	var row ProductionNotificationWriteLock
	err := tx.Where("production_id = ?", strings.TrimSpace(productionID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.MutationState == "" {
		return nil
	}
	if (row.MutationState == NotificationMutationReordering || row.MutationState == NotificationMutationDeleting) && allowedToken != "" && row.MutationToken == allowedToken {
		return nil
	}
	return ErrNotificationMutationRecoveryRequired
}

// BeginProductionDeletionMutation fences polling, Apply, Reviewer writes, and
// other destination changes for the full teardown. A crashed deleting state is
// never resumed automatically; an explicit re-run may resume only a durable
// deletion_needs_help state, with the same operation token.
func BeginProductionDeletionMutation(db *gorm.DB, productionID string) (string, error) {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return "", gorm.ErrInvalidData
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	newToken := hex.EncodeToString(random[:])
	var token string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		if err := EnsureNoUnresolvedNotificationDeliveries(tx, productionID); err != nil {
			return err
		}
		var lock ProductionNotificationWriteLock
		if err := tx.Where("production_id = ?", strings.TrimSpace(productionID)).First(&lock).Error; err != nil {
			return err
		}
		switch lock.MutationState {
		case "":
			token = newToken
		case NotificationMutationDeletionHelp:
			token = lock.MutationToken
			if token == "" {
				return ErrNotificationMutationRecoveryRequired
			}
		default:
			return ErrNotificationMutationRecoveryRequired
		}
		result := tx.Model(&ProductionNotificationWriteLock{}).Where("production_id = ? AND mutation_state = ?", strings.TrimSpace(productionID), lock.MutationState).Updates(map[string]any{
			"mutation_state": NotificationMutationDeleting, "mutation_token": token, "mutation_at": time.Now().UTC(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotificationMutationRecoveryRequired
		}
		return nil
	})
	return token, err
}

// BeginProductionNotificationOrderRecovery explicitly claims a quarantined
// reorder so callers can reconcile Discord to the current durable route order.
// The caller must verify every destination and complete or re-quarantine the
// operation with FinishProductionNotificationExternalMutation.
func FinishProductionDeletionMutation(db *gorm.DB, productionID, token string, needsHelp bool) error {
	if db == nil || strings.TrimSpace(productionID) == "" || strings.TrimSpace(token) == "" {
		return gorm.ErrInvalidData
	}
	state := ""
	if needsHelp {
		state = NotificationMutationDeletionHelp
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		result := tx.Model(&ProductionNotificationWriteLock{}).Where("production_id = ? AND mutation_state = ? AND mutation_token = ?", strings.TrimSpace(productionID), NotificationMutationDeleting, strings.TrimSpace(token)).Updates(map[string]any{
			"mutation_state": state, "mutation_token": map[bool]string{true: token, false: ""}[needsHelp], "mutation_at": nil,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotificationMutationRecoveryRequired
		}
		return nil
	})
}
func EnsureNoUnresolvedManagedChannelDeletions(tx *gorm.DB, productionID string) error {
	var count int64
	if err := tx.Model(&ManagedChannelDeletion{}).Where("production_id = ? AND disposition <> ?", strings.TrimSpace(productionID), ChannelDeletionComplete).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrChannelDeletionConflict
	}
	return nil
}
func FindManagedChannelDeletion(db *gorm.DB, webhookRowID uint) (*ManagedChannelDeletion, error) {
	if db == nil || webhookRowID == 0 {
		return nil, gorm.ErrInvalidData
	}
	var row ManagedChannelDeletion
	err := db.Where("webhook_row_id = ?", webhookRowID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

type DeliveryDiagnostic struct {
	EventID          string    `json:"event_id"`
	SuccessorEventID string    `json:"successor_event_id,omitempty"`
	Disposition      string    `json:"disposition"`
	FailureClass     string    `json:"failure_class,omitempty"`
	ConflictCount    int       `json:"conflict_count,omitempty"`
	Cleanup          bool      `json:"cleanup_pending,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func RedactDeliveredNotificationPayloads(db *gorm.DB, deliveredBefore, redactedAt time.Time, limit int) (int64, error) {
	if db == nil || deliveredBefore.IsZero() || redactedAt.IsZero() || limit <= 0 {
		return 0, gorm.ErrInvalidData
	}
	if limit > NotificationPayloadRedactionBatchSize {
		limit = NotificationPayloadRedactionBatchSize
	}
	var rowsAffected int64
	err := db.Transaction(func(tx *gorm.DB) error {
		eligible := tx.Model(&NotificationDeliveryEvent{}).
			Select("id").
			Where("disposition = ? AND delivered_at IS NOT NULL AND delivered_at < ?", DeliveryDelivered, deliveredBefore).
			Where("prior_cleanup_pending = ? AND prior_cleanup_in_flight = ?", false, false).
			Where("payload_json <> '' OR thread_name <> ''").
			Order("delivered_at ASC, id ASC").
			Limit(limit)
		result := tx.Model(&NotificationDeliveryEvent{}).
			Where("id IN (?)", eligible).
			Where("disposition = ? AND delivered_at IS NOT NULL AND delivered_at < ?", DeliveryDelivered, deliveredBefore).
			Where("prior_cleanup_pending = ? AND prior_cleanup_in_flight = ?", false, false).
			Where("payload_json <> '' OR thread_name <> ''").
			Updates(map[string]any{"payload_json": "", "thread_name": "", "payload_redacted_at": gorm.Expr("COALESCE(payload_redacted_at, ?)", redactedAt.UTC())})
		if result.Error != nil {
			return result.Error
		}
		rowsAffected = result.RowsAffected
		return nil
	})
	return rowsAffected, err
}
func ListUnresolvedNotificationDeliveries(db *gorm.DB, limit int) ([]DeliveryDiagnostic, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	out := make([]DeliveryDiagnostic, 0)
	var conflictCount int
	if err := db.Model(&NotificationDeliveryEvent{}).Select("COALESCE(SUM(identity_conflict_count), 0)").Where("identity_conflict_count > 0").Scan(&conflictCount).Error; err != nil {
		return nil, err
	}
	if conflictCount > 0 {
		var latestConflict NotificationDeliveryEvent
		if err := db.Select("identity_conflict_at", "updated_at").Where("identity_conflict_count > 0").Order("identity_conflict_at desc, id desc").First(&latestConflict).Error; err != nil {
			return nil, err
		}
		updated := time.Time{}
		if latestConflict.IdentityConflictAt != nil {
			updated = *latestConflict.IdentityConflictAt
		} else {
			updated = latestConflict.UpdatedAt
		}
		out = append(out, DeliveryDiagnostic{EventID: "identity-conflicts", Disposition: "identity_conflict", FailureClass: "event_identity_conflict", ConflictCount: conflictCount, UpdatedAt: updated})
	}
	var rows []NotificationDeliveryEvent
	if limit <= 0 || len(out) < limit {
		query := db.Where("disposition IN ? OR prior_cleanup_pending = ?", []string{DeliveryPending, DeliveryInFlight, DeliveryRetryable, DeliveryUnknown, DeliveryTerminalFailure, DeliveryLegacyUnknown}, true).Order("updated_at asc")
		if limit > 0 {
			query = query.Limit(limit - len(out))
		}
		if err := query.Find(&rows).Error; err != nil {
			return nil, err
		}
	}
	for _, row := range rows {
		id := row.EventID
		if len(id) > 12 {
			id = id[:12]
		}
		successorID := row.SupersededByEventID
		if len(successorID) > 12 {
			successorID = successorID[:12]
		}
		out = append(out, DeliveryDiagnostic{EventID: id, SuccessorEventID: successorID, Disposition: row.Disposition, FailureClass: row.FailureClass, Cleanup: row.PriorCleanupPending, UpdatedAt: row.UpdatedAt})
	}
	var deletions []ManagedChannelDeletion
	dq := db.Where("disposition <> ?", ChannelDeletionComplete).Order("updated_at asc")
	if limit > 0 && len(out) < limit {
		dq = dq.Limit(limit - len(out))
	}
	if err := dq.Find(&deletions).Error; err != nil {
		return nil, err
	}
	for _, row := range deletions {
		id := fmt.Sprintf("delete-%d", row.WebhookRowID)
		out = append(out, DeliveryDiagnostic{EventID: id, Disposition: "channel_" + row.Disposition, FailureClass: row.FailureClass, UpdatedAt: row.UpdatedAt})
	}
	var mutations []ProductionNotificationWriteLock
	mq := db.Where("mutation_state <> ''").Order("updated_at asc")
	if limit > 0 && len(out) < limit {
		mq = mq.Limit(limit - len(out))
	}
	if err := mq.Find(&mutations).Error; err != nil {
		return nil, err
	}
	for _, row := range mutations {
		id := row.ProductionID
		if len(id) > 12 {
			id = id[:12]
		}
		out = append(out, DeliveryDiagnostic{EventID: "mutation-" + id, Disposition: "mutation_" + row.MutationState, FailureClass: "external_order_recovery_required", UpdatedAt: row.UpdatedAt})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func BeginManagedChannelDeletion(db *gorm.DB, productionID string, webhookRowID uint, guildID, channelID, discordWebhookID string, now time.Time) error {
	return BeginManagedChannelDeletionBatch(db, productionID, []ManagedChannelDeletionTarget{{WebhookRowID: webhookRowID, GuildID: guildID, ChannelID: channelID, DiscordWebhookID: discordWebhookID}}, now, false)
}

// ManagedChannelDeletionTarget identifies one Discord destination to tombstone.
type ManagedChannelDeletionTarget struct {
	WebhookRowID     uint
	GuildID          string
	ChannelID        string
	DiscordWebhookID string
}

func BeginManagedChannelDeletionBatch(db *gorm.DB, productionID string, targets []ManagedChannelDeletionTarget, now time.Time, allowLastRoute bool) error {
	return beginManagedChannelDeletionBatch(db, productionID, targets, now, allowLastRoute, "")
}

// BeginManagedChannelDeletionBatchForProductionTeardown accepts only the
// durable whole-Production deletion token, keeping all channel tombstones
// within the same cross-process mutation barrier.
func BeginManagedChannelDeletionBatchForProductionTeardown(db *gorm.DB, productionID string, targets []ManagedChannelDeletionTarget, now time.Time, allowLastRoute bool, token string) error {
	if strings.TrimSpace(token) == "" {
		return gorm.ErrInvalidData
	}
	return beginManagedChannelDeletionBatch(db, productionID, targets, now, allowLastRoute, token)
}
func beginManagedChannelDeletionBatch(db *gorm.DB, productionID string, targets []ManagedChannelDeletionTarget, now time.Time, allowLastRoute bool, mutationToken string) error {
	if db == nil || strings.TrimSpace(productionID) == "" || len(targets) == 0 {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		if err := EnsureNoUnresolvedNotificationDeliveries(tx, productionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, productionID, mutationToken); err != nil {
			return err
		}
		var project Project
		if err := tx.Where("kitsu_project_id = ?", strings.TrimSpace(productionID)).First(&project).Error; err != nil {
			return err
		}
		seen := map[uint]bool{}
		rowsByChannel := map[string]int{}
		for _, target := range targets {
			rowsByChannel[strings.TrimSpace(target.ChannelID)]++
		}
		var affectedRouteCount, allRouteCount int64
		if err := tx.Model(&ProductionNotificationRoute{}).Where("production_id = ?", productionID).Count(&allRouteCount).Error; err != nil {
			return err
		}
		for _, target := range targets {
			if target.WebhookRowID == 0 || strings.TrimSpace(target.GuildID) == "" || strings.TrimSpace(target.ChannelID) == "" || strings.TrimSpace(target.DiscordWebhookID) == "" || seen[target.WebhookRowID] || strings.TrimSpace(project.DiscordGuildID) != strings.TrimSpace(target.GuildID) {
				return gorm.ErrInvalidData
			}
			seen[target.WebhookRowID] = true
			var cleanupCount int64
			if err := tx.Model(&NotificationDeliveryEvent{}).Where("webhook_row_id = ? AND prior_cleanup_pending = ?", target.WebhookRowID, true).Count(&cleanupCount).Error; err != nil {
				return err
			}
			if cleanupCount != 0 {
				return ErrChannelDeletionConflict
			}
			var webhook ProjectWebhook
			if err := tx.Where("id = ? AND kitsu_project_id = ? AND discord_channel_id = ?", target.WebhookRowID, strings.TrimSpace(productionID), strings.TrimSpace(target.ChannelID)).First(&webhook).Error; err != nil {
				return err
			}
			if notificationWebhookID(webhook.WebhookURL) != strings.TrimSpace(target.DiscordWebhookID) {
				return ErrChannelDeletionConflict
			}
			var channelRowCount int64
			if err := tx.Model(&ProjectWebhook{}).Where("kitsu_project_id = ? AND discord_channel_id = ?", productionID, target.ChannelID).Count(&channelRowCount).Error; err != nil {
				return err
			}
			if channelRowCount != int64(rowsByChannel[strings.TrimSpace(target.ChannelID)]) {
				return ErrChannelDeletionConflict
			}
			var routeCount int64
			if err := tx.Model(&ProductionNotificationRoute{}).Where("production_id = ? AND destination_webhook_id = ?", productionID, target.WebhookRowID).Count(&routeCount).Error; err != nil {
				return err
			}
			affectedRouteCount += routeCount
		}
		if affectedRouteCount > allRouteCount || (allowLastRoute && affectedRouteCount != allRouteCount) || (!allowLastRoute && affectedRouteCount > 0 && allRouteCount <= affectedRouteCount) {
			return ErrChannelDeletionConflict
		}
		for _, target := range targets {
			var tombstone ManagedChannelDeletion
			err := tx.Where("webhook_row_id = ?", target.WebhookRowID).First(&tombstone).Error
			if err == nil {
				if tombstone.ProductionID != productionID || tombstone.GuildID != target.GuildID || tombstone.ChannelID != target.ChannelID || tombstone.DiscordWebhookID != target.DiscordWebhookID || tombstone.Disposition == ChannelDeletionComplete {
					return ErrChannelDeletionConflict
				}
				if tombstone.Disposition == ChannelDeletionNeedsHelp {
					if err := tx.Model(&ManagedChannelDeletion{}).Where("id = ?", tombstone.ID).Updates(map[string]any{"disposition": ChannelDeletionPending, "failure_class": ""}).Error; err != nil {
						return err
					}
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			row := ManagedChannelDeletion{WebhookRowID: target.WebhookRowID, ProductionID: productionID, GuildID: target.GuildID, ChannelID: target.ChannelID, DiscordWebhookID: target.DiscordWebhookID, Disposition: ChannelDeletionPending, CreatedAt: now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func RecordManagedChannelDeletionFailure(db *gorm.DB, webhookRowID uint, class string) error {
	if db == nil || webhookRowID == 0 || strings.TrimSpace(class) == "" {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var tombstone ManagedChannelDeletion
		if err := tx.Where("webhook_row_id = ?", webhookRowID).First(&tombstone).Error; err != nil {
			return err
		}
		if err := AcquireProductionNotificationWriteLock(tx, tombstone.ProductionID); err != nil {
			return err
		}
		result := tx.Model(&ManagedChannelDeletion{}).Where("webhook_row_id = ? AND disposition IN ?", webhookRowID, []string{ChannelDeletionPending, ChannelDeletionNeedsHelp}).Updates(map[string]any{"disposition": ChannelDeletionNeedsHelp, "failure_class": class})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrChannelDeletionConflict
		}
		return nil
	})
}

// RecordNotificationOnlyDelivery writes a record-only lane entry to the
// delivery journal. Nothing is sent; the row documents that the notification
// was intentionally recorded without delivery.
func FinalizeManagedChannelDeletionForProductionTeardown(db *gorm.DB, webhookRowID uint, now time.Time, token string) error {
	if strings.TrimSpace(token) == "" {
		return gorm.ErrInvalidData
	}
	return finalizeManagedChannelDeletion(db, webhookRowID, now, token)
}
func finalizeManagedChannelDeletion(db *gorm.DB, webhookRowID uint, now time.Time, mutationToken string) error {
	if db == nil || webhookRowID == 0 {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var tombstone ManagedChannelDeletion
		if err := tx.Where("webhook_row_id = ?", webhookRowID).First(&tombstone).Error; err != nil {
			return err
		}
		if err := AcquireProductionNotificationWriteLock(tx, tombstone.ProductionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, tombstone.ProductionID, mutationToken); err != nil {
			return err
		}
		if err := EnsureNoInFlightNotificationDeliveries(tx, tombstone.ProductionID); err != nil {
			return err
		}
		if tombstone.Disposition != ChannelDeletionPending && tombstone.Disposition != ChannelDeletionNeedsHelp {
			return ErrChannelDeletionConflict
		}
		var routes []ProductionNotificationRoute
		if err := tx.Where("production_id = ? AND destination_webhook_id = ?", tombstone.ProductionID, webhookRowID).Find(&routes).Error; err != nil {
			return err
		}
		var project Project
		if err := tx.Where("kitsu_project_id = ?", tombstone.ProductionID).First(&project).Error; err != nil {
			return err
		}
		if strings.TrimSpace(project.DiscordGuildID) != tombstone.GuildID {
			return ErrChannelDeletionConflict
		}
		var webhook ProjectWebhook
		if err := tx.Where("id = ? AND kitsu_project_id = ? AND discord_channel_id = ?", webhookRowID, tombstone.ProductionID, tombstone.ChannelID).First(&webhook).Error; err != nil {
			return err
		}
		if notificationWebhookID(webhook.WebhookURL) != tombstone.DiscordWebhookID {
			return ErrChannelDeletionConflict
		}
		for _, route := range routes {
			if err := tx.Where("project_id = ? AND task_type_id = ?", project.ID, route.TaskTypeID).Delete(&ProjectReviewerTarget{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("production_id = ? AND destination_webhook_id = ?", tombstone.ProductionID, webhookRowID).Delete(&ProductionNotificationRoute{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&ProjectWebhook{}, webhookRowID).Error; err != nil {
			return err
		}
		return tx.Model(&ManagedChannelDeletion{}).Where("id = ?", tombstone.ID).Updates(map[string]any{"disposition": ChannelDeletionComplete, "completed_at": now, "failure_class": ""}).Error
	})
}
