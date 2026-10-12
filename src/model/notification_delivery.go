package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	ChannelDeletionPending   = "pending"
	ChannelDeletionComplete  = "complete"
	ChannelDeletionNeedsHelp = "needs_help"

	NotificationMutationReordering   = "reordering"
	NotificationMutationDeleting     = "deleting"
	NotificationMutationDeletionHelp = "deletion_needs_help"
	NotificationMutationReorderHelp  = "reorder_needs_help"
)

var (
	ErrDeliveryIdentityConflict             = errors.New("notification delivery identity conflict")
	ErrDeliveryUnavailable                  = errors.New("notification delivery is not safely dispatchable")
	ErrDeliveryAttemptConflict              = errors.New("notification delivery attempt conflict")
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
	DeliverySource         string `gorm:"not null;default:'kitsu';index"`
	SourceEventID          string `gorm:"size:64;index"`
	TaskUpdatedAt          string
	CommentID              string
	CommentUpdatedAt       string
	IsAssignNotification   bool
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
	FailureClass           string
	SupersededByEventID    string `gorm:"size:64;index"`
	CleanupFailureClass    string
	IdentityConflictCount  int        `gorm:"not null;default:0" json:"-"`
	IdentityConflictAt     *time.Time `json:"-"`
	CreatedAt              time.Time  `gorm:"index"`
	UpdatedAt              time.Time
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
func BeginProductionNotificationOrderRecovery(db *gorm.DB, productionID string) (string, error) {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return "", gorm.ErrInvalidData
	}
	token, err := NewProductionNotificationMutationToken()
	if err != nil {
		return "", err
	}
	productionID = strings.TrimSpace(productionID)
	var claimedToken string
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		// Reconciliation changes channel positions only; pending/unknown delivery
		// intents keep their original destinations. An actively executing send is
		// the only delivery state that must block this operation.
		if err := EnsureNoInFlightNotificationDeliveries(tx, productionID); err != nil {
			return err
		}
		var lock ProductionNotificationWriteLock
		if err := tx.Where("production_id = ?", productionID).First(&lock).Error; err != nil {
			return err
		}
		if lock.MutationState != NotificationMutationReorderHelp {
			return ErrNotificationMutationRecoveryRequired
		}
		claim := tx.Model(&ProductionNotificationWriteLock{}).Where("production_id = ? AND mutation_state = ?", productionID, NotificationMutationReorderHelp)
		if strings.TrimSpace(lock.MutationToken) == "" {
			// Older quarantines may have been persisted before the mutation token
			// was retained. Claim only that exact help state with an empty/NULL
			// token, then replace it atomically with a fresh recovery token.
			claim = claim.Where("mutation_token = '' OR mutation_token IS NULL")
		} else {
			claim = claim.Where("mutation_token = ?", lock.MutationToken)
		}
		result := claim.Updates(map[string]any{
			"mutation_state": NotificationMutationReordering,
			"mutation_token": token,
			"mutation_at":    time.Now().UTC(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotificationMutationRecoveryRequired
		}
		claimedToken = token
		return nil
	})
	return claimedToken, err
}

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

func CanDeleteProjectWebhookWithoutRemovingLastRoute(db *gorm.DB, productionID string, webhookRowID uint) (bool, error) {
	if db == nil || strings.TrimSpace(productionID) == "" || webhookRowID == 0 {
		return false, gorm.ErrInvalidData
	}
	var total, affected int64
	if err := db.Model(&ProductionNotificationRoute{}).Where("production_id = ?", strings.TrimSpace(productionID)).Count(&total).Error; err != nil {
		return false, err
	}
	if err := db.Model(&ProductionNotificationRoute{}).Where("production_id = ? AND destination_webhook_id = ?", strings.TrimSpace(productionID), webhookRowID).Count(&affected).Error; err != nil {
		return false, err
	}
	return affected == 0 || total > affected, nil
}

type NotificationDeliveryIntent struct {
	TaskID               string
	ProductionID         string
	TaskTypeID           string
	TaskStatus           string
	DeliverySource       string
	SourceEventID        string
	TaskUpdatedAt        string
	CommentID            string
	CommentUpdatedAt     string
	IsAssignNotification bool
	Payload              []byte
	WebhookRowID         uint
	GuildID              string
	ChannelID            string
	DiscordWebhookID     string
	NotificationRevision string
	ThreadName           string
	PriorMessageID       string
	PriorThreadID        string
}

type DeliveryAttempt struct {
	Event NotificationDeliveryEvent
	ID    string
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

type NotificationDestinationIdentity struct {
	WebhookRowID uint
	GuildID      string
	ChannelID    string
	WebhookID    string
}

func NotificationEventIdentity(intent NotificationDeliveryIntent) string {
	if intent.DeliverySource == "vfx_files" && strings.TrimSpace(intent.SourceEventID) != "" {
		data, _ := json.Marshal([]string{"vfx_files", strings.TrimSpace(intent.SourceEventID)})
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	data, _ := json.Marshal([]string{
		strings.TrimSpace(intent.ProductionID), strings.TrimSpace(intent.TaskID), strings.TrimSpace(intent.TaskTypeID),
		strings.ToUpper(strings.TrimSpace(intent.TaskStatus)), strings.TrimSpace(intent.TaskUpdatedAt), strings.TrimSpace(intent.CommentID),
		strings.TrimSpace(intent.CommentUpdatedAt), fmt.Sprint(intent.IsAssignNotification),
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func notificationEventTime(taskUpdatedAt, commentUpdatedAt string) (time.Time, bool) {
	var latest time.Time
	for _, raw := range []string{taskUpdatedAt, commentUpdatedAt} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var parsed time.Time
		var err error
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999"} {
			parsed, err = time.Parse(layout, strings.TrimSpace(raw))
			if err == nil {
				break
			}
		}
		if err != nil {
			continue
		}
		if latest.IsZero() || parsed.After(latest) {
			latest = parsed
		}
	}
	return latest, !latest.IsZero()
}

func supersedeKnownRejectedNotificationEvents(tx *gorm.DB, intent NotificationDeliveryIntent, now time.Time) (int64, error) {
	if tx == nil || strings.TrimSpace(intent.TaskID) == "" || strings.TrimSpace(intent.ProductionID) == "" {
		return 0, gorm.ErrInvalidData
	}
	observedAt, ok := notificationEventTime(intent.TaskUpdatedAt, intent.CommentUpdatedAt)
	if !ok || intent.DeliverySource != "" && intent.DeliverySource != "kitsu" {
		return 0, nil
	}
	newEventID := NotificationEventIdentity(intent)
	var retryable []NotificationDeliveryEvent
	if err := tx.Where("task_id = ? AND production_id = ? AND event_id <> ? AND disposition = ? AND failure_class = ? AND attempt_count > 0 AND attempt_id <> ? AND discord_message_id = '' AND delivered_at IS NULL AND payload_redacted_at IS NULL",
		strings.TrimSpace(intent.TaskID), strings.TrimSpace(intent.ProductionID), newEventID, DeliveryRetryable, "rate_limited", "").Find(&retryable).Error; err != nil {
		return 0, err
	}
	var count int64
	for _, prior := range retryable {
		priorAt, valid := notificationEventTime(prior.TaskUpdatedAt, prior.CommentUpdatedAt)
		if !valid || !observedAt.After(priorAt) {
			continue
		}
		result := tx.Model(&NotificationDeliveryEvent{}).
			Where("id = ? AND event_id = ? AND disposition = ? AND failure_class = ?", prior.ID, prior.EventID, DeliveryRetryable, "rate_limited").
			UpdateColumns(map[string]any{
				"disposition":            DeliveryTerminalFailure,
				"failure_class":          "superseded_by_newer_kitsu_event",
				"superseded_by_event_id": newEventID,
				"updated_at":             now.UTC(),
			})
		if result.Error != nil {
			return count, result.Error
		}
		count += result.RowsAffected
	}
	return count, nil
}

// SupersedeObsoleteKnownRejectedNotificationDeliveries records a later Kitsu
// event as the explicit terminal successor to any older, known-rejected (429)
// event for that task. It never resends stale content and never discards the
// prior payload or attempt evidence. Unparseable timestamps fail closed.
func SupersedeObsoleteKnownRejectedNotificationDeliveries(db *gorm.DB, intent NotificationDeliveryIntent) (int64, error) {
	if db == nil {
		return 0, gorm.ErrInvalidDB
	}
	var count int64
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, intent.ProductionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, intent.ProductionID, ""); err != nil {
			return err
		}
		var err error
		count, err = supersedeKnownRejectedNotificationEvents(tx, intent, time.Now().UTC())
		return err
	})
	return count, err
}

func notificationDestinationFingerprint(guildID, channelID, webhookID string) string {
	data, _ := json.Marshal([]string{strings.TrimSpace(guildID), strings.TrimSpace(channelID), strings.TrimSpace(webhookID)})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

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

func ResolveNotificationDestination(db *gorm.DB, productionID, taskTypeID, webhookURL, discordWebhookID string) (NotificationDestinationIdentity, error) {
	var identity NotificationDestinationIdentity
	if db == nil || strings.TrimSpace(productionID) == "" || strings.TrimSpace(taskTypeID) == "" || strings.TrimSpace(webhookURL) == "" || strings.TrimSpace(discordWebhookID) == "" {
		return identity, gorm.ErrInvalidData
	}
	var project Project
	if err := db.Where("kitsu_project_id = ?", strings.TrimSpace(productionID)).First(&project).Error; err != nil {
		return identity, err
	}
	var route ProductionNotificationRoute
	if err := db.Where("production_id = ? AND task_type_id = ?", strings.TrimSpace(productionID), strings.TrimSpace(taskTypeID)).First(&route).Error; err != nil {
		return identity, err
	}
	var webhook ProjectWebhook
	if err := db.Where("id = ? AND kitsu_project_id = ? AND webhook_url = ?", route.DestinationWebhookID, strings.TrimSpace(productionID), strings.TrimSpace(webhookURL)).First(&webhook).Error; err != nil {
		return identity, err
	}
	if project.ValidationOnly || strings.TrimSpace(project.DiscordGuildID) == "" || strings.TrimSpace(webhook.DiscordChannelID) == "" ||
		(route.DestinationChannelID != "" && strings.TrimSpace(route.DestinationChannelID) != strings.TrimSpace(webhook.DiscordChannelID)) {
		return identity, ErrDeliveryUnavailable
	}
	var count int64
	if err := db.Model(&ManagedChannelDeletion{}).Where("webhook_row_id = ? AND disposition <> ?", webhook.ID, ChannelDeletionComplete).Count(&count).Error; err != nil {
		return identity, err
	}
	if count != 0 {
		return identity, ErrDeliveryUnavailable
	}
	return NotificationDestinationIdentity{WebhookRowID: webhook.ID, GuildID: strings.TrimSpace(project.DiscordGuildID), ChannelID: strings.TrimSpace(webhook.DiscordChannelID), WebhookID: strings.TrimSpace(discordWebhookID)}, nil
}

func LatestDeliveredNotificationForDestination(db *gorm.DB, taskID string, identity NotificationDestinationIdentity) (*NotificationDeliveryEvent, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var event NotificationDeliveryEvent
	err := db.Where("task_id = ? AND destination_fingerprint = ? AND disposition = ?", strings.TrimSpace(taskID), notificationDestinationFingerprint(identity.GuildID, identity.ChannelID, identity.WebhookID), DeliveryDelivered).Order("delivered_at desc, id desc").First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func NotificationDeliveryDisposition(db *gorm.DB, eventID string) (string, error) {
	if db == nil || strings.TrimSpace(eventID) == "" {
		return "", gorm.ErrInvalidData
	}
	var event NotificationDeliveryEvent
	err := db.Select("disposition").Where("event_id = ?", eventID).First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return event.Disposition, err
}

func ListPendingNotificationDeliveries(db *gorm.DB, limit int) ([]NotificationDeliveryEvent, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []NotificationDeliveryEvent
	query := db.Where("disposition IN ?", []string{DeliveryPending, DeliveryRetryable}).Order("created_at asc, id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// RedactDeliveredNotificationPayloads clears only old, successfully delivered
// payload bodies whose prior-message cleanup has completed. Event identity,
// payload hash, destination identity, disposition and Discord message/thread
// checkpoints remain durable so a repeated Kitsu observation cannot resend.
// The bounded conditional update is safe to run repeatedly and never loads or
// logs the payload text.
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

func FindNotificationDeliveryByID(db *gorm.DB, eventID string) (*NotificationDeliveryEvent, error) {
	if db == nil || strings.TrimSpace(eventID) == "" {
		return nil, gorm.ErrInvalidData
	}
	var row NotificationDeliveryEvent
	err := db.Where("event_id = ?", eventID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func PrepareNotificationDelivery(db *gorm.DB, intent NotificationDeliveryIntent) (*NotificationDeliveryEvent, error) {
	if db == nil || strings.TrimSpace(intent.TaskID) == "" || strings.TrimSpace(intent.ProductionID) == "" ||
		strings.TrimSpace(intent.TaskTypeID) == "" || len(intent.Payload) == 0 || intent.WebhookRowID == 0 ||
		strings.TrimSpace(intent.GuildID) == "" || strings.TrimSpace(intent.ChannelID) == "" || strings.TrimSpace(intent.DiscordWebhookID) == "" {
		return nil, gorm.ErrInvalidData
	}
	var payload any
	if err := json.Unmarshal(intent.Payload, &payload); err != nil {
		return nil, gorm.ErrInvalidData
	}
	eventID := NotificationEventIdentity(intent)
	payloadSum := sha256.Sum256(intent.Payload)
	source := strings.TrimSpace(intent.DeliverySource)
	if source == "" {
		source = "kitsu"
	}
	if source != "kitsu" && source != "vfx_files" || source == "vfx_files" && (strings.TrimSpace(intent.SourceEventID) == "" || !vfxFilesUUIDRE.MatchString(intent.SourceEventID)) {
		return nil, gorm.ErrInvalidData
	}
	row := NotificationDeliveryEvent{
		EventID: eventID, TaskID: strings.TrimSpace(intent.TaskID), ProductionID: strings.TrimSpace(intent.ProductionID),
		TaskTypeID: strings.TrimSpace(intent.TaskTypeID), TaskStatus: strings.TrimSpace(intent.TaskStatus),
		DeliverySource: source, SourceEventID: strings.TrimSpace(intent.SourceEventID),
		TaskUpdatedAt: intent.TaskUpdatedAt, CommentID: intent.CommentID, CommentUpdatedAt: intent.CommentUpdatedAt,
		IsAssignNotification: intent.IsAssignNotification, PayloadJSON: string(intent.Payload), PayloadSHA256: hex.EncodeToString(payloadSum[:]),
		WebhookRowID: intent.WebhookRowID, DestinationGuildID: strings.TrimSpace(intent.GuildID),
		DestinationChannelID: strings.TrimSpace(intent.ChannelID), DestinationWebhookID: strings.TrimSpace(intent.DiscordWebhookID),
		DestinationFingerprint: notificationDestinationFingerprint(intent.GuildID, intent.ChannelID, intent.DiscordWebhookID),
		NotificationRevision:   strings.TrimSpace(intent.NotificationRevision),
		Disposition:            DeliveryPending, ThreadName: intent.ThreadName, PriorMessageID: intent.PriorMessageID, PriorThreadID: intent.PriorThreadID,
	}
	var identityConflict bool
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, intent.ProductionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, intent.ProductionID, ""); err != nil {
			return err
		}
		if err := EnsureNoUnresolvedManagedChannelDeletions(tx, intent.ProductionID); err != nil {
			return err
		}
		if _, err := supersedeKnownRejectedNotificationEvents(tx, intent, time.Now().UTC()); err != nil {
			return err
		}
		var existing NotificationDeliveryEvent
		err := tx.Where("event_id = ?", eventID).First(&existing).Error
		if err == nil {
			if existing.DestinationFingerprint != row.DestinationFingerprint || existing.WebhookRowID != row.WebhookRowID || existing.PayloadSHA256 != row.PayloadSHA256 || existing.NotificationRevision != row.NotificationRevision {
				conflictAt := time.Now().UTC()
				update := tx.Model(&NotificationDeliveryEvent{}).Where("id = ? AND event_id = ?", existing.ID, eventID).
					UpdateColumns(map[string]any{"identity_conflict_count": gorm.Expr("identity_conflict_count + 1"), "identity_conflict_at": conflictAt})
				if update.Error != nil {
					return update.Error
				}
				if update.RowsAffected != 1 {
					return ErrProductionNotificationRevisionConflict
				}
				// A rate-limit response is an explicit Discord rejection. It is safe
				// to refresh that never-accepted intent from the current Kitsu render
				// and currently resolved destination. Unknown, in-flight, delivered,
				// and other terminal records retain their immutable delivery evidence.
				if existing.Disposition == DeliveryRetryable && existing.FailureClass == "rate_limited" &&
					existing.AttemptCount > 0 && existing.AttemptID != "" && existing.DiscordMessageID == "" && existing.DeliveredAt == nil && existing.PayloadRedactedAt == nil {
					result := tx.Model(&NotificationDeliveryEvent{}).
						Where("id = ? AND event_id = ? AND disposition = ? AND failure_class = ? AND discord_message_id = ?", existing.ID, eventID, DeliveryRetryable, "rate_limited", "").
						UpdateColumns(map[string]any{
							"task_id": row.TaskID, "production_id": row.ProductionID, "task_type_id": row.TaskTypeID,
							"task_status": row.TaskStatus, "task_updated_at": row.TaskUpdatedAt, "comment_id": row.CommentID,
							"comment_updated_at": row.CommentUpdatedAt, "is_assign_notification": row.IsAssignNotification,
							"payload_json": row.PayloadJSON, "payload_sha256": row.PayloadSHA256, "payload_redacted_at": nil,
							"webhook_row_id": row.WebhookRowID, "destination_guild_id": row.DestinationGuildID,
							"destination_channel_id": row.DestinationChannelID, "destination_webhook_id": row.DestinationWebhookID,
							"destination_fingerprint": row.DestinationFingerprint, "notification_revision": row.NotificationRevision,
							"thread_name": row.ThreadName, "prior_message_id": row.PriorMessageID, "prior_thread_id": row.PriorThreadID,
							"disposition": DeliveryPending, "failure_class": "fresh_intent_after_known_rejection", "updated_at": conflictAt,
						})
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected != 1 {
						return ErrProductionNotificationRevisionConflict
					}
					if err := tx.Where("id = ?", existing.ID).First(&row).Error; err != nil {
						return err
					}
					return nil
				}
				identityConflict = true
				row = existing
				return nil
			}
			row = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err := tx.Model(&ManagedChannelDeletion{}).Where("guild_id = ? AND channel_id = ? AND discord_webhook_id = ? AND disposition <> ?", row.DestinationGuildID, row.DestinationChannelID, row.DestinationWebhookID, ChannelDeletionComplete).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrDeliveryUnavailable
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	if identityConflict {
		return &row, ErrDeliveryIdentityConflict
	}
	return &row, nil
}

func BeginNotificationDelivery(db *gorm.DB, eventID string, now time.Time) (*DeliveryAttempt, bool, error) {
	return beginNotificationDelivery(db, eventID, nil, now)
}

// BeginNotificationDeliveryForEvent atomically verifies that the event read
// and destination-checked by the caller is still the durable intent being
// claimed. A known-rejected refresh racing the external webhook identity read
// therefore cannot make the caller send a replacement payload to the old URL.
func BeginNotificationDeliveryForEvent(db *gorm.DB, expected NotificationDeliveryEvent, now time.Time) (*DeliveryAttempt, bool, error) {
	return beginNotificationDelivery(db, expected.EventID, &expected, now)
}

func beginNotificationDelivery(db *gorm.DB, eventID string, expected *NotificationDeliveryEvent, now time.Time) (*DeliveryAttempt, bool, error) {
	if db == nil {
		return nil, false, gorm.ErrInvalidDB
	}
	var attempt DeliveryAttempt
	var unavailable bool
	err := db.Transaction(func(tx *gorm.DB) error {
		var row NotificationDeliveryEvent
		if err := tx.Where("event_id = ?", eventID).First(&row).Error; err != nil {
			return err
		}
		if expected != nil && (row.EventID != expected.EventID || row.PayloadSHA256 != expected.PayloadSHA256 ||
			row.DestinationFingerprint != expected.DestinationFingerprint || row.WebhookRowID != expected.WebhookRowID ||
			row.NotificationRevision != expected.NotificationRevision) {
			unavailable = true
			return nil
		}
		if err := AcquireProductionNotificationWriteLock(tx, row.ProductionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, row.ProductionID, ""); err != nil {
			return err
		}
		if row.DeliverySource == "vfx_files" {
			var routing ProductionNotificationConfig
			if err := tx.Where("production_id = ? AND enabled = ?", row.ProductionID, true).First(&routing).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					unavailable = true
					return nil
				}
				return err
			}
			var integration VFXFilesProductionConfig
			if err := tx.Where("production_id = ?", row.ProductionID).First(&integration).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					unavailable = true
					return nil
				}
				return err
			}
			var inbox VFXFilesEventInbox
			if err := tx.Where("producer_id = ? AND event_id = ?", "vfx-files-plugin", row.SourceEventID).First(&inbox).Error; err != nil {
				return err
			}
			if !integration.Activated || integration.ActivationEpoch == 0 || inbox.ActivationEpoch != integration.ActivationEpoch || inbox.Outcome != VFXFilesEventAccepted || inbox.ProcessedAt != nil {
				unavailable = true
				return nil
			}
		}
		if row.Disposition == DeliveryInFlight {
			if row.AttemptStartedAt == nil || now.Sub(*row.AttemptStartedAt) > 2*time.Minute {
				if err := tx.Model(&NotificationDeliveryEvent{}).Where("id = ? AND disposition = ?", row.ID, DeliveryInFlight).Updates(map[string]any{"disposition": DeliveryUnknown, "failure_class": "expired_in_flight"}).Error; err != nil {
					return err
				}
			}
			unavailable = true
			return nil
		}
		if row.Disposition != DeliveryPending && row.Disposition != DeliveryRetryable {
			unavailable = true
			return nil
		}
		if err := EnsureProductionNotificationMutationIdle(tx, row.ProductionID, ""); err != nil {
			if errors.Is(err, ErrNotificationMutationRecoveryRequired) {
				unavailable = true
				return nil
			}
			return err
		}
		if err := EnsureNoInFlightNotificationDeliveries(tx, row.ProductionID); err != nil {
			if errors.Is(err, ErrNotificationDeliveryInFlight) {
				unavailable = true
				return nil
			}
			return err
		}
		var earlierUnresolved int64
		if err := tx.Model(&NotificationDeliveryEvent{}).Where("task_id = ? AND id < ? AND disposition IN ?", row.TaskID, row.ID, []string{DeliveryPending, DeliveryRetryable, DeliveryUnknown, DeliveryInFlight}).Count(&earlierUnresolved).Error; err != nil {
			return err
		}
		if earlierUnresolved != 0 {
			unavailable = true
			return nil
		}
		var count int64
		if err := tx.Model(&ManagedChannelDeletion{}).Where("guild_id = ? AND channel_id = ? AND discord_webhook_id = ? AND disposition <> ?", row.DestinationGuildID, row.DestinationChannelID, row.DestinationWebhookID, ChannelDeletionComplete).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			unavailable = true
			return nil
		}
		var project Project
		if err := tx.Where("kitsu_project_id = ?", row.ProductionID).First(&project).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				unavailable = true
				return tx.Model(&NotificationDeliveryEvent{}).Where("id = ?", row.ID).Updates(map[string]any{"disposition": DeliveryTerminalFailure, "failure_class": "production_removed"}).Error
			}
			return err
		}
		var webhook ProjectWebhook
		if err := tx.Where("id = ? AND kitsu_project_id = ? AND discord_channel_id = ?", row.WebhookRowID, row.ProductionID, row.DestinationChannelID).First(&webhook).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				unavailable = true
				return tx.Model(&NotificationDeliveryEvent{}).Where("id = ?", row.ID).Updates(map[string]any{"disposition": DeliveryTerminalFailure, "failure_class": "destination_removed"}).Error
			}
			return err
		}
		var route ProductionNotificationRoute
		if err := tx.Where("production_id = ? AND task_type_id = ? AND destination_webhook_id = ?", row.ProductionID, row.TaskTypeID, row.WebhookRowID).First(&route).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				unavailable = true
				return tx.Model(&NotificationDeliveryEvent{}).Where("id = ?", row.ID).Updates(map[string]any{"disposition": DeliveryTerminalFailure, "failure_class": "route_removed"}).Error
			}
			return err
		}
		if strings.TrimSpace(project.DiscordGuildID) != row.DestinationGuildID || (route.DestinationChannelID != "" && route.DestinationChannelID != row.DestinationChannelID) {
			unavailable = true
			return nil
		}
		if row.NotificationRevision != "" {
			revision, err := productionNotificationRevision(tx, project.ID, row.ProductionID)
			if err != nil {
				return err
			}
			if revision != row.NotificationRevision {
				unavailable = true
				return tx.Model(&NotificationDeliveryEvent{}).Where("id = ? AND disposition = ?", row.ID, row.Disposition).Updates(map[string]any{"disposition": DeliveryTerminalFailure, "failure_class": "notification_state_changed_before_dispatch"}).Error
			}
		}
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return err
		}
		attempt.ID = hex.EncodeToString(raw[:])
		attempt.Event = row
		result := tx.Model(&NotificationDeliveryEvent{}).Where("id = ? AND disposition IN ?", row.ID, []string{DeliveryPending, DeliveryRetryable}).Updates(map[string]any{
			"disposition": DeliveryInFlight, "attempt_id": attempt.ID, "attempt_count": row.AttemptCount + 1, "attempt_started_at": now,
			"failure_class": "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeliveryAttemptConflict
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if unavailable {
		return nil, false, ErrDeliveryUnavailable
	}
	return &attempt, true, nil
}

func CompleteNotificationDelivery(db *gorm.DB, attempt DeliveryAttempt, messageID, threadID string, now time.Time) error {
	if db == nil || attempt.ID == "" || strings.TrimSpace(messageID) == "" {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, attempt.Event.ProductionID); err != nil {
			return err
		}
		result := tx.Model(&NotificationDeliveryEvent{}).Where("event_id = ? AND attempt_id = ? AND disposition = ?", attempt.Event.EventID, attempt.ID, DeliveryInFlight).Updates(map[string]any{
			"disposition": DeliveryDelivered, "discord_message_id": messageID, "discord_thread_id": threadID, "delivered_at": now,
			"prior_cleanup_pending": attempt.Event.PriorMessageID != "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeliveryAttemptConflict
		}
		if attempt.Event.DeliverySource == "vfx_files" {
			return nil
		}
		return updateTaskDeliveryCheckpoint(tx, attempt.Event, messageID, threadID)
	})
}

func updateTaskDeliveryCheckpoint(tx *gorm.DB, event NotificationDeliveryEvent, messageID, threadID string) error {
	var task Task
	err := tx.Where("task_id = ?", event.TaskID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&Task{TaskID: event.TaskID, TaskUpdatedAt: event.TaskUpdatedAt, TaskStatus: event.TaskStatus, LastObservedStatus: event.TaskStatus, CommentID: event.CommentID, CommentUpdatedAt: event.CommentUpdatedAt, DiscordMessageID: messageID, DiscordThreadID: threadID, DeliveredTaskUpdatedAt: event.TaskUpdatedAt, DeliveredTaskStatus: event.TaskStatus, DeliveredCommentID: event.CommentID, DeliveredCommentUpdatedAt: event.CommentUpdatedAt}).Error
	}
	if err != nil {
		return err
	}
	result := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{
		"task_status": event.TaskStatus, "discord_message_id": messageID, "discord_thread_id": threadID,
		"delivered_task_updated_at": event.TaskUpdatedAt, "delivered_task_status": event.TaskStatus,
		"delivered_comment_id": event.CommentID, "delivered_comment_updated_at": event.CommentUpdatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func SetNotificationDeliveryDisposition(db *gorm.DB, attempt DeliveryAttempt, disposition, failureClass string) error {
	if db == nil || (disposition != DeliveryRetryable && disposition != DeliveryUnknown && disposition != DeliveryTerminalFailure) {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, attempt.Event.ProductionID); err != nil {
			return err
		}
		result := tx.Model(&NotificationDeliveryEvent{}).Where("event_id = ? AND attempt_id = ? AND disposition = ?", attempt.Event.EventID, attempt.ID, DeliveryInFlight).Updates(map[string]any{"disposition": disposition, "failure_class": failureClass})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeliveryAttemptConflict
		}
		return nil
	})
}

// QuarantineAcceptedButUnpersisted retains the durable pre-send attempt while
// recording that Discord may have accepted a message whose checkpoint failed.
// It never makes the event automatically retryable.
func QuarantineAcceptedButUnpersisted(db *gorm.DB, attempt DeliveryAttempt) error {
	if db == nil || attempt.ID == "" {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, attempt.Event.ProductionID); err != nil {
			return err
		}
		result := tx.Model(&NotificationDeliveryEvent{}).Where("event_id = ? AND attempt_id = ? AND disposition = ?", attempt.Event.EventID, attempt.ID, DeliveryInFlight).Updates(map[string]any{"disposition": DeliveryUnknown, "failure_class": "discord_accepted_checkpoint_uncommitted"})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeliveryAttemptConflict
		}
		return nil
	})
}

func SetUnstartedNotificationDeliveryDisposition(db *gorm.DB, eventID, disposition, failureClass string) error {
	if db == nil || strings.TrimSpace(eventID) == "" || disposition != DeliveryTerminalFailure && disposition != DeliveryUnknown {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var event NotificationDeliveryEvent
		if err := tx.Where("event_id = ?", eventID).First(&event).Error; err != nil {
			return err
		}
		if err := AcquireProductionNotificationWriteLock(tx, event.ProductionID); err != nil {
			return err
		}
		result := tx.Model(&NotificationDeliveryEvent{}).Where("id = ? AND disposition IN ?", event.ID, []string{DeliveryPending, DeliveryRetryable}).Updates(map[string]any{"disposition": disposition, "failure_class": failureClass})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeliveryAttemptConflict
		}
		return nil
	})
}

// RecordPriorMessageCleanup tracks best-effort replacement cleanup without
// changing the durable outcome of the newly delivered notification.
func RecordPriorMessageCleanup(db *gorm.DB, eventID string, succeeded bool, failureClass string) error {
	if db == nil || strings.TrimSpace(eventID) == "" {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var event NotificationDeliveryEvent
		if err := tx.Where("event_id = ?", eventID).First(&event).Error; err != nil {
			return err
		}
		if err := AcquireProductionNotificationWriteLock(tx, event.ProductionID); err != nil {
			return err
		}
		updates := map[string]any{"prior_cleanup_pending": !succeeded, "prior_cleanup_in_flight": false, "cleanup_failure_class": ""}
		if !succeeded {
			updates["cleanup_failure_class"] = strings.TrimSpace(failureClass)
		}
		result := tx.Model(&NotificationDeliveryEvent{}).Where("id = ? AND disposition = ? AND prior_cleanup_pending = ?", event.ID, DeliveryDelivered, true).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDeliveryAttemptConflict
		}
		return nil
	})
}

func BeginPriorMessageCleanup(db *gorm.DB, eventID string) (*NotificationDeliveryEvent, bool, error) {
	if db == nil || strings.TrimSpace(eventID) == "" {
		return nil, false, gorm.ErrInvalidData
	}
	var event NotificationDeliveryEvent
	var unavailable bool
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("event_id = ?", strings.TrimSpace(eventID)).First(&event).Error; err != nil {
			return err
		}
		if err := AcquireProductionNotificationWriteLock(tx, event.ProductionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, event.ProductionID, ""); err != nil {
			return err
		}
		if !event.PriorCleanupPending || event.Disposition != DeliveryDelivered || event.PriorMessageID == "" {
			unavailable = true
			return nil
		}
		var tombstoneCount int64
		if err := tx.Model(&ManagedChannelDeletion{}).Where("webhook_row_id = ? AND disposition <> ?", event.WebhookRowID, ChannelDeletionComplete).Count(&tombstoneCount).Error; err != nil {
			return err
		}
		if tombstoneCount != 0 {
			unavailable = true
			return nil
		}
		var webhook ProjectWebhook
		if err := tx.Where("id = ? AND kitsu_project_id = ? AND discord_channel_id = ?", event.WebhookRowID, event.ProductionID, event.DestinationChannelID).First(&webhook).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				unavailable = true
				return nil
			}
			return err
		}
		result := tx.Model(&NotificationDeliveryEvent{}).Where("id = ? AND disposition = ? AND prior_cleanup_pending = ? AND prior_cleanup_in_flight = ?", event.ID, DeliveryDelivered, true, false).Update("prior_cleanup_in_flight", true)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			unavailable = true
			return nil
		}
		event.PriorCleanupInFlight = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if unavailable {
		return nil, false, ErrDeliveryUnavailable
	}
	return &event, true, nil
}

func QuarantineInflightNotificationDeliveries(db *gorm.DB) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	return db.Transaction(func(tx *gorm.DB) error {
		quarantined := tx.Model(&NotificationDeliveryEvent{}).Where("disposition = ?", DeliveryInFlight).Updates(map[string]any{"disposition": DeliveryUnknown, "failure_class": "process_restart_during_delivery"})
		if quarantined.Error != nil {
			return quarantined.Error
		}
		// External operations interrupted by a process restart must stay fenced.
		// A human may explicitly retry a channel teardown from its tombstones, but
		// neither teardown nor Discord reordering resumes automatically.
		if err := tx.Model(&ProductionNotificationWriteLock{}).Where("mutation_state = ?", NotificationMutationDeleting).Updates(map[string]any{"mutation_state": NotificationMutationDeletionHelp, "mutation_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&ProductionNotificationWriteLock{}).Where("mutation_state = ?", NotificationMutationReordering).Updates(map[string]any{"mutation_state": NotificationMutationReorderHelp, "mutation_at": nil}).Error; err != nil {
			return err
		}
		// Message deletion is idempotent for the exact stored message ID; a
		// restart may safely release its claim and retry after re-verifying the
		// original webhook identity.
		return tx.Model(&NotificationDeliveryEvent{}).Where("prior_cleanup_in_flight = ? AND prior_cleanup_pending = ?", true, true).Update("prior_cleanup_in_flight", false).Error
	})
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

type ManagedChannelDeletionTarget struct {
	WebhookRowID     uint
	GuildID          string
	ChannelID        string
	DiscordWebhookID string
}

// BeginManagedChannelDeletionBatch tombstones every exact destination in one
// transaction before any external DELETE. allowLastRoute is reserved for an
// explicit whole-Production teardown and requires the batch to cover every
// current notification route.
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

func FinalizeManagedChannelDeletion(db *gorm.DB, webhookRowID uint, now time.Time) error {
	return finalizeManagedChannelDeletion(db, webhookRowID, now, "")
}

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
