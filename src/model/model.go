package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gookit/slog"
	"gorm.io/gorm"
)

type AuditLog struct {
	ID                 uint      `gorm:"primaryKey"`
	CreatedAt          time.Time `gorm:"index"`
	TaskID             string    `gorm:"index"`
	ProjectID          string    `gorm:"index"`
	ProjectName        string
	GuildID            string `gorm:"index"`
	EntityName         string
	TaskType           string
	OldStatus          string
	NewStatus          string
	DiscordMsgID       string
	PreviousMsgID      string
	WebhookURL         string
	PreviousWebhookURL string
	Success            bool
	ErrorMessage       string
	RetryCount         int
}

func WriteAuditLog(db *gorm.DB, log AuditLog) {
	if db == nil {
		return
	}
	// Webhook URLs are credentials. Audit evidence keeps the outcome and
	// destination IDs, never the secret-bearing URL itself.
	log.WebhookURL = ""
	log.PreviousWebhookURL = ""
	db.Create(&log)
}

func ListAuditLogs(db *gorm.DB, limit int) []AuditLog {
	var logs []AuditLog
	db.Order("created_at desc").Limit(limit).Find(&logs)
	return logs
}

func CountAuditLogs(db *gorm.DB) int64 {
	if db == nil {
		return 0
	}
	var count int64
	db.Model(&AuditLog{}).Count(&count)
	return count
}

type AuditHealth string

const (
	AuditHealthNormal      AuditHealth = "normal"
	AuditHealthNeedsReview AuditHealth = "needs_review"
	AuditHealthAbnormal    AuditHealth = "abnormal"
	AuditHealthNoRecords   AuditHealth = "no_records"
)

// RecentAuditHealth summarizes persisted audit outcomes in the canonical
// recent window. Severity is evaluated across all events.
func RecentAuditHealth(db *gorm.DB, now time.Time) AuditHealth {
	if db == nil {
		return AuditHealthNoRecords
	}
	var logs []AuditLog
	if err := db.Where("created_at >= ?", now.Add(-24*time.Hour)).Find(&logs).Error; err != nil || len(logs) == 0 {
		return AuditHealthNoRecords
	}
	health := AuditHealthNoRecords
	for _, log := range logs {
		text := strings.ToLower(strings.Join([]string{log.ErrorMessage, log.OldStatus, log.NewStatus, log.EntityName}, " "))
		if !log.Success || strings.Contains(text, "failure") || strings.Contains(text, "failed") || strings.Contains(text, "error") {
			return AuditHealthAbnormal
		}
		if log.RetryCount > 0 || strings.Contains(text, "warning") || strings.Contains(text, "retry") || strings.Contains(text, "partial") || strings.Contains(text, "degraded") {
			health = AuditHealthNeedsReview
			continue
		}
		if log.Success && health == AuditHealthNoRecords {
			health = AuditHealthNormal
		}
	}
	return health
}

func PurgeOldAuditLogs(db *gorm.DB, keepDays int) int64 {
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	return db.Where("created_at < ?", cutoff).Delete(&AuditLog{}).RowsAffected
}

type Task struct {
	ID               uint `gorm:"primaryKey"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        gorm.DeletedAt `gorm:"index"`
	TaskID           string         `gorm:"index"`
	TaskUpdatedAt    string
	TaskStatus       string `gorm:"index"`
	CommentID        string
	CommentUpdatedAt string
	DiscordMessageID string
	WebhookURL       string
	DiscordThreadID  string
}

func CreateTask(db *gorm.DB, taskID, taskUpdatedAt, taskStatus, commentID, commentUpdatedAt string) {
	db.Create(&Task{
		TaskID:           taskID,
		TaskUpdatedAt:    taskUpdatedAt,
		TaskStatus:       taskStatus,
		CommentID:        commentID,
		CommentUpdatedAt: commentUpdatedAt,
	})
}

func UpdateTask(db *gorm.DB, taskID, taskUpdatedAt, taskStatus, commentID, commentUpdatedAt string) {
	db.Model(&Task{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
		"task_updated_at":    taskUpdatedAt,
		"task_status":        taskStatus,
		"comment_id":         commentID,
		"comment_updated_at": commentUpdatedAt,
	})
}

// MarkTaskObserved records a non-deliverable or permanently failed event
// without clearing a previously delivered Discord message.
func MarkTaskObserved(db *gorm.DB, taskID, taskUpdatedAt, taskStatus, commentID, commentUpdatedAt string) {
	updates := map[string]interface{}{
		"task_updated_at":    taskUpdatedAt,
		"task_status":        taskStatus,
		"comment_id":         commentID,
		"comment_updated_at": commentUpdatedAt,
	}
	result := db.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates)
	if result.RowsAffected == 0 {
		db.Create(&Task{TaskID: taskID, TaskUpdatedAt: taskUpdatedAt, TaskStatus: taskStatus, CommentID: commentID, CommentUpdatedAt: commentUpdatedAt})
	}
}

func UpdateTaskWithDiscord(db *gorm.DB, taskID, taskUpdatedAt, taskStatus, commentID, commentUpdatedAt, discordMessageID, webhookURL, threadID string) {
	updates := map[string]interface{}{
		"task_updated_at":    taskUpdatedAt,
		"task_status":        taskStatus,
		"comment_id":         commentID,
		"comment_updated_at": commentUpdatedAt,
		"discord_message_id": discordMessageID,
		"webhook_url":        "",
	}
	if threadID != "" {
		updates["discord_thread_id"] = threadID
	}
	result := db.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates)
	if result.RowsAffected == 0 {
		db.Create(&Task{
			TaskID:           taskID,
			TaskUpdatedAt:    taskUpdatedAt,
			TaskStatus:       taskStatus,
			CommentID:        commentID,
			CommentUpdatedAt: commentUpdatedAt,
			DiscordMessageID: discordMessageID,
			DiscordThreadID:  threadID,
		})
	}
}

func ClearMessageID(db *gorm.DB, taskID string) {
	db.Model(&Task{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
		"discord_message_id": "",
		"discord_thread_id":  "",
	})
}

type StatusCount struct {
	TaskStatus string
	Count      int
}

func GetStatusCounts(db *gorm.DB) []StatusCount {
	var results []StatusCount
	db.Model(&Task{}).
		Select("task_status, count(*) as count").
		Where("deleted_at IS NULL").
		Group("task_status").
		Order("count desc").
		Scan(&results)
	return results
}

func FindTask(db *gorm.DB, taskID string) Task {
	var task Task
	db.First(&task, "task_id = ?", taskID)
	return task
}

type Project struct {
	ID                 uint   `gorm:"primaryKey"`
	KitsuProjectID     string `gorm:"uniqueIndex"`
	Name               string
	ProjectType        string
	DiscordGuildID     string `gorm:"index"`
	DiscordCategoryID  string
	Language           string
	StorageURL         string
	ValidationOnly     bool   `gorm:"index"`
	ValidationDataJSON string `gorm:"type:text"`
	ReadOnlyPreview    bool   `gorm:"-"`
}

// ValidationKitsuData is read-only Kitsu metadata captured for an isolated
// validation profile. It deliberately contains no Discord identifiers.
type ValidationKitsuData struct {
	TaskTypes    []ValidationTaskType `json:"task_types,omitempty"`
	Participants []ValidationPerson   `json:"participants,omitempty"`
}

type ValidationTaskType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ValidationPerson struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Email    string `json:"email,omitempty"`
}

func (p Project) ValidationData() ValidationKitsuData {
	var data ValidationKitsuData
	if strings.TrimSpace(p.ValidationDataJSON) == "" {
		return data
	}
	if err := json.Unmarshal([]byte(p.ValidationDataJSON), &data); err != nil {
		return ValidationKitsuData{}
	}
	return data
}

func IsValidationOnlyProject(db *gorm.DB, kitsuProjectID string) bool {
	var project Project
	if db == nil || db.Where("kitsu_project_id = ?", strings.TrimSpace(kitsuProjectID)).First(&project).Error != nil {
		return false
	}
	return project.ValidationOnly
}

type ProjectWebhook struct {
	ID               uint   `gorm:"primaryKey"`
	KitsuProjectID   string `gorm:"index"`
	ChannelName      string
	TaskType         string
	WebhookURL       string
	DiscordChannelID string
}

// ProductionChannelMapping is the stable-ID mapping for the current
// Production -> Discord Guild -> Kitsu Task Type channel model. Display names
// are retained as metadata; routing must use the IDs.
type ProductionChannelMapping struct {
	ID             uint   `gorm:"primaryKey"`
	ProductionID   string `gorm:"not null;uniqueIndex:idx_production_task_channel"`
	GuildID        string `gorm:"not null;index"`
	TaskTypeID     string `gorm:"not null;uniqueIndex:idx_production_task_channel"`
	TaskTypeName   string
	ChannelID      string `gorm:"not null;index"`
	ChannelName    string
	Active         bool
	MigrationState string `gorm:"not null;default:'current'"`
	OperationID    string `gorm:"index"`
	State          string `gorm:"not null;default:'current'"`
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const (
	ChannelMappingStateCurrent        = "current"
	ChannelMappingStatePending        = "pending"
	ChannelMappingStatePartial        = "partial"
	ChannelMappingStateReviewRequired = "review_required"
)

func ValidateProductionChannelMappings(productionID, guildID string, mappings []ProductionChannelMapping) []string {
	issues := []string{}
	productionID = strings.TrimSpace(productionID)
	guildID = strings.TrimSpace(guildID)
	if productionID == "" || guildID == "" {
		issues = append(issues, "Production and linked Guild are required")
	}
	seenTaskTypes := map[string]bool{}
	seenChannels := map[string]bool{}
	for _, mapping := range mappings {
		if strings.TrimSpace(mapping.ProductionID) != productionID || strings.TrimSpace(mapping.GuildID) != guildID {
			issues = append(issues, "mapping belongs to a different Production or Guild")
		}
		if strings.TrimSpace(mapping.TaskTypeID) == "" || strings.TrimSpace(mapping.ChannelID) == "" {
			issues = append(issues, "Task Type and channel IDs are required")
		}
		if seenTaskTypes[mapping.TaskTypeID] || seenChannels[mapping.ChannelID] {
			issues = append(issues, "Task Type and channel mappings must be unique")
		}
		seenTaskTypes[mapping.TaskTypeID] = true
		seenChannels[mapping.ChannelID] = true
		if !mapping.Active || strings.TrimSpace(mapping.MigrationState) != "current" {
			issues = append(issues, "paused or migration-required mappings are not ready")
		}
	}
	return compactUniqueStrings(issues)
}

func ListProductionChannelMappings(db *gorm.DB, productionID string) []ProductionChannelMapping {
	var rows []ProductionChannelMapping
	if db == nil {
		return rows
	}
	db.Where("production_id = ?", strings.TrimSpace(productionID)).Order("task_type_id asc").Find(&rows)
	return rows
}

func SaveProductionChannelMappings(db *gorm.DB, productionID, guildID string, mappings []ProductionChannelMapping) error {
	if db == nil || strings.TrimSpace(productionID) == "" || strings.TrimSpace(guildID) == "" {
		return gorm.ErrInvalidData
	}
	if issues := ValidateProductionChannelMappings(productionID, guildID, mappings); len(issues) > 0 {
		return fmt.Errorf("invalid production channel mappings: %s", strings.Join(issues, "; "))
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, mapping := range mappings {
			var existing ProductionChannelMapping
			err := tx.Where("production_id = ? AND task_type_id = ?", productionID, mapping.TaskTypeID).First(&existing).Error
			if err == nil {
				mapping.ID = existing.ID
				mapping.CreatedAt = existing.CreatedAt
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			mapping.ProductionID = strings.TrimSpace(productionID)
			mapping.GuildID = strings.TrimSpace(guildID)
			if err := tx.Save(&mapping).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SavePendingProductionChannelMapping records a verified Discord result before
// the complete channel plan is ready. Pending rows are deliberately inactive;
// they make retries and manual recovery possible without enabling partial
// notification routing.
func SavePendingProductionChannelMapping(db *gorm.DB, mapping ProductionChannelMapping) error {
	if db == nil || strings.TrimSpace(mapping.ProductionID) == "" || strings.TrimSpace(mapping.GuildID) == "" || strings.TrimSpace(mapping.TaskTypeID) == "" || strings.TrimSpace(mapping.ChannelID) == "" {
		return gorm.ErrInvalidData
	}
	mapping.Active = false
	mapping.State = ChannelMappingStatePending
	mapping.MigrationState = ChannelMappingStatePending
	var existing ProductionChannelMapping
	if err := db.Where("production_id = ? AND task_type_id = ?", mapping.ProductionID, mapping.TaskTypeID).First(&existing).Error; err == nil {
		mapping.ID = existing.ID
		mapping.CreatedAt = existing.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Save(&mapping).Error
}

func MarkProductionChannelMappingsReviewRequired(db *gorm.DB, productionID, operationID string, keep map[string]bool, reason string) error {
	if db == nil {
		return gorm.ErrInvalidData
	}
	var rows []ProductionChannelMapping
	if err := db.Where("production_id = ?", strings.TrimSpace(productionID)).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if keep[row.TaskTypeID] {
			continue
		}
		row.Active = false
		row.State = ChannelMappingStateReviewRequired
		row.MigrationState = ChannelMappingStateReviewRequired
		row.OperationID = operationID
		row.LastError = reason
		if err := db.Save(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

// ActivateProductionRoutingFromMappings makes the new routing model the
// dispatch source of truth after a confirmed channel plan. Legacy
// ProjectWebhook rows remain compatibility data and provide the webhook
// destination required by the current dispatcher.
func ActivateProductionRoutingFromMappings(db *gorm.DB, productionID, guildID string, mappings []ProductionChannelMapping) error {
	slog.Debug("Production routing activation started", "production_id", strings.TrimSpace(productionID), "guild_id", strings.TrimSpace(guildID), "mapping_count", len(mappings))
	if db == nil || strings.TrimSpace(productionID) == "" || strings.TrimSpace(guildID) == "" {
		slog.Warn("Production routing activation rejected", "stage", "input_validation", "error_class", "missing_production_or_guild", "mapping_count", len(mappings))
		return gorm.ErrInvalidData
	}
	project := FindProjectByKitsuID(db, productionID)
	if project == nil {
		slog.Warn("Production routing activation rejected", "stage", "project_lookup", "error_class", "production_not_connected", "production_id", strings.TrimSpace(productionID))
		return fmt.Errorf("production is not connected locally")
	}
	if issues := ValidateProductionChannelMappings(productionID, guildID, mappings); len(issues) > 0 {
		slog.Warn("Production routing activation rejected", "stage", "mapping_validation", "error_class", "invalid_mapping", "production_id", strings.TrimSpace(productionID), "mapping_count", len(mappings), "issues", issues)
		return fmt.Errorf("invalid production channel mappings: %s", strings.Join(issues, "; "))
	}
	webhooks := ListProjectWebhooks(db, productionID)
	slog.Debug("Production routing activation destinations loaded", "production_id", strings.TrimSpace(productionID), "mapping_count", len(mappings), "webhook_record_count", len(webhooks))
	routes := make([]ProductionNotificationRoute, 0, len(mappings))
	for _, mapping := range mappings {
		var selected *ProjectWebhook
		webhookURL := ""
		for i := range webhooks {
			webhook := &webhooks[i]
			if strings.TrimSpace(webhook.DiscordChannelID) != strings.TrimSpace(mapping.ChannelID) || strings.TrimSpace(webhook.WebhookURL) == "" {
				continue
			}
			if selected == nil {
				selected = webhook
				webhookURL = strings.TrimSpace(webhook.WebhookURL)
				continue
			}
			if strings.TrimSpace(webhook.WebhookURL) != webhookURL {
				slog.Warn("Production routing activation rejected", "stage", "destination_validation", "error_class", "ambiguous_webhook_destination", "task_type_id", mapping.TaskTypeID, "channel_id", mapping.ChannelID)
				return fmt.Errorf("ambiguous webhook destinations for channel mapping %s", mapping.TaskTypeID)
			}
		}
		if selected == nil {
			slog.Warn("Production routing activation rejected", "stage", "destination_validation", "error_class", "missing_webhook_destination", "task_type_id", mapping.TaskTypeID, "channel_id", mapping.ChannelID)
			return fmt.Errorf("no valid legacy webhook destination for channel mapping %s", mapping.TaskTypeID)
		}
		routes = append(routes, ProductionNotificationRoute{
			ProductionID:           productionID,
			TaskTypeID:             mapping.TaskTypeID,
			TaskTypeName:           mapping.TaskTypeName,
			DestinationWebhookID:   selected.ID,
			DestinationChannelID:   mapping.ChannelID,
			DestinationChannelName: mapping.ChannelName,
		})
	}
	if issues := ValidateProductionNotificationConfig(db, productionID, routes); len(issues) > 0 {
		slog.Warn("Production routing activation rejected", "stage", "route_validation", "error_class", "invalid_notification_route", "production_id", strings.TrimSpace(productionID), "route_count", len(routes), "issues", issues)
		return fmt.Errorf("notification routing is not valid: %s", strings.Join(issues, "; "))
	}
	slog.Debug("Production routing activation transaction begin", "production_id", strings.TrimSpace(productionID), "mapping_count", len(mappings), "route_count", len(routes))
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Project{}).Where("kitsu_project_id = ?", productionID).Updates(map[string]interface{}{"discord_guild_id": strings.TrimSpace(guildID)}).Error; err != nil {
			return err
		}
		for _, mapping := range mappings {
			var existing ProductionChannelMapping
			err := tx.Where("production_id = ? AND task_type_id = ?", productionID, mapping.TaskTypeID).First(&existing).Error
			if err == nil {
				mapping.ID = existing.ID
				mapping.CreatedAt = existing.CreatedAt
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			mapping.ProductionID = strings.TrimSpace(productionID)
			mapping.GuildID = strings.TrimSpace(guildID)
			mapping.Active = true
			mapping.State = ChannelMappingStateCurrent
			mapping.MigrationState = ChannelMappingStateCurrent
			mapping.LastError = ""
			if err := tx.Save(&mapping).Error; err != nil {
				return err
			}
		}
		keep := make(map[string]bool, len(mappings))
		for _, mapping := range mappings {
			keep[strings.TrimSpace(mapping.TaskTypeID)] = true
		}
		var existingMappings []ProductionChannelMapping
		if err := tx.Where("production_id = ?", productionID).Find(&existingMappings).Error; err != nil {
			return err
		}
		for _, existingMapping := range existingMappings {
			if keep[existingMapping.TaskTypeID] {
				continue
			}
			existingMapping.Active = false
			existingMapping.State = ChannelMappingStateReviewRequired
			existingMapping.MigrationState = ChannelMappingStateReviewRequired
			existingMapping.LastError = "Task Type omitted from the confirmed plan; review before reuse"
			if err := tx.Save(&existingMapping).Error; err != nil {
				return err
			}
		}
		var config ProductionNotificationConfig
		err := tx.Where("production_id = ?", productionID).First(&config).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			config = ProductionNotificationConfig{ProductionID: productionID, ProductionName: project.Name}
		} else if err != nil {
			return err
		}
		config.ProductionName = project.Name
		config.Enabled = true
		if err := tx.Save(&config).Error; err != nil {
			return err
		}
		if err := tx.Where("production_id = ?", productionID).Delete(&ProductionNotificationRoute{}).Error; err != nil {
			return err
		}
		for _, route := range routes {
			if err := tx.Create(&route).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("Production routing activation rolled back", "stage", "database_transaction", "error_class", "transaction_failed", "production_id", strings.TrimSpace(productionID), "mapping_count", len(mappings), "route_count", len(routes))
		return err
	}
	slog.Debug("Production routing activation committed", "production_id", strings.TrimSpace(productionID), "mapping_count", len(mappings), "route_count", len(routes), "committed", true)
	return nil
}

func compactUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

// ProductionNotificationConfig is the explicit opt-in boundary for the
// production-scoped notification router. ProductionName is display metadata;
// ProductionID is the only routing identity.
type ProductionNotificationConfig struct {
	ID             uint   `gorm:"primaryKey"`
	ProductionID   string `gorm:"uniqueIndex;not null"`
	ProductionName string
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ProductionNotificationRoute struct {
	ID                     uint   `gorm:"primaryKey"`
	ProductionID           string `gorm:"index;not null;uniqueIndex:idx_production_task_type"`
	TaskTypeID             string `gorm:"not null;uniqueIndex:idx_production_task_type"`
	TaskTypeName           string
	DestinationWebhookID   uint `gorm:"not null"`
	DestinationChannelID   string
	DestinationChannelName string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type NotificationRoutingDiagnosis struct {
	ID           uint      `gorm:"primaryKey"`
	CreatedAt    time.Time `gorm:"index"`
	TaskID       string    `gorm:"index"`
	ProductionID string    `gorm:"index"`
	TaskTypeID   string    `gorm:"index"`
	Reason       string
	Detail       string
}

func FindProductionNotificationConfig(db *gorm.DB, productionID string) *ProductionNotificationConfig {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return nil
	}
	var row ProductionNotificationConfig
	if err := db.Where("production_id = ?", strings.TrimSpace(productionID)).First(&row).Error; err != nil {
		return nil
	}
	return &row
}

func ListProductionNotificationRoutes(db *gorm.DB, productionID string) []ProductionNotificationRoute {
	var rows []ProductionNotificationRoute
	if db == nil {
		return rows
	}
	db.Where("production_id = ?", strings.TrimSpace(productionID)).Order("id asc").Find(&rows)
	return rows
}

func SaveProductionNotificationConfig(db *gorm.DB, config *ProductionNotificationConfig, routes []ProductionNotificationRoute) error {
	if db == nil || config == nil {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var existing ProductionNotificationConfig
		if err := tx.Where("production_id = ?", config.ProductionID).First(&existing).Error; err == nil {
			config.ID = existing.ID
			config.CreatedAt = existing.CreatedAt
		}
		if err := tx.Save(config).Error; err != nil {
			return err
		}
		if err := tx.Where("production_id = ?", config.ProductionID).Delete(&ProductionNotificationRoute{}).Error; err != nil {
			return err
		}
		for i := range routes {
			routes[i].ID = 0
			routes[i].ProductionID = config.ProductionID
			if err := tx.Create(&routes[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteProductionOperationalState removes current routing state owned by a
// KitsuSync Production connection. Audit history is intentionally retained.
func DeleteProductionOperationalState(db *gorm.DB, productionID string) error {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	for _, table := range []interface{}{
		&ProductionNotificationRoute{},
		&ProductionNotificationConfig{},
		&ProductionChannelMapping{},
	} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if err := db.Where("production_id = ?", productionID).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}

// HasProductionOperationalState reports whether a Production has any local
// routing state without requiring a connected Project row.
func HasProductionOperationalState(db *gorm.DB, productionID string) bool {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return false
	}
	productionID = strings.TrimSpace(productionID)
	var count int64
	for _, table := range []interface{}{
		&ProductionNotificationRoute{},
		&ProductionNotificationConfig{},
		&ProductionChannelMapping{},
	} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if err := db.Model(table).Where("production_id = ?", productionID).Count(&count).Error; err == nil && count > 0 {
			return true
		}
	}
	return false
}

func RecordNotificationRoutingDiagnosis(db *gorm.DB, diagnosis NotificationRoutingDiagnosis) {
	if db != nil {
		db.Create(&diagnosis)
	}
}

func ListNotificationRoutingDiagnoses(db *gorm.DB, productionID string, limit int) []NotificationRoutingDiagnosis {
	var rows []NotificationRoutingDiagnosis
	if db == nil {
		return rows
	}
	query := db.Where("production_id = ?", strings.TrimSpace(productionID)).Order("created_at desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	query.Find(&rows)
	return rows
}

func ValidateProductionNotificationConfig(db *gorm.DB, productionID string, routes []ProductionNotificationRoute) []string {
	var issues []string
	productionID = strings.TrimSpace(productionID)
	if productionID == "" {
		issues = append(issues, "production ID is required")
	}
	if FindProjectByKitsuID(db, productionID) == nil {
		issues = append(issues, "selected Production is not connected locally")
	}
	if len(routes) == 0 {
		issues = append(issues, "at least one routing destination is required")
	}
	seen := make(map[string]struct{})
	for _, route := range routes {
		taskTypeID := strings.TrimSpace(route.TaskTypeID)
		if taskTypeID == "" {
			issues = append(issues, "Task Type ID is required")
			continue
		}
		if _, ok := seen[taskTypeID]; ok {
			issues = append(issues, "Task Type IDs must be unique")
		} else {
			seen[taskTypeID] = struct{}{}
		}
		webhook := FindProjectWebhookByID(db, route.DestinationWebhookID)
		if webhook == nil || webhook.KitsuProjectID != productionID || strings.TrimSpace(webhook.WebhookURL) == "" || strings.TrimSpace(webhook.DiscordChannelID) == "" {
			issues = append(issues, "each destination must be an existing configured webhook for the selected Production")
		}
	}
	return issues
}

func CreateProject(db *gorm.DB, kitsuProjectID, name, projectType, guildID, categoryID, language string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if FindProjectByKitsuID(tx, kitsuProjectID) != nil {
			return errors.New("project is already connected")
		}
		if err := DeleteProductionOperationalState(tx, kitsuProjectID); err != nil {
			return err
		}
		return tx.Create(&Project{
			KitsuProjectID:    kitsuProjectID,
			Name:              name,
			ProjectType:       projectType,
			DiscordGuildID:    guildID,
			DiscordCategoryID: categoryID,
			Language:          language,
		}).Error
	})
}

func UpdateProjectGuildID(db *gorm.DB, kitsuProjectID, guildID string) error {
	return db.Model(&Project{}).Where("kitsu_project_id = ?", kitsuProjectID).Update("discord_guild_id", guildID).Error
}

func ResolveProjectGuildID(db *gorm.DB, kitsuProjectID, fallbackGuildID string) string {
	if p := FindProjectByKitsuID(db, kitsuProjectID); p != nil && strings.TrimSpace(p.DiscordGuildID) != "" {
		return strings.TrimSpace(p.DiscordGuildID)
	}
	return strings.TrimSpace(fallbackGuildID)
}

// SeedProjectGuildFallback copies legacy global guild ID into existing projects
// that do not have project-scoped guild IDs yet.
func SeedProjectGuildFallback(db *gorm.DB, fallbackGuildID string) {
	fallbackGuildID = strings.TrimSpace(fallbackGuildID)
	if fallbackGuildID == "" || db == nil {
		return
	}
	_ = db.Model(&Project{}).
		Where("discord_guild_id = '' OR discord_guild_id IS NULL").
		Update("discord_guild_id", fallbackGuildID).Error
}

func FindProjectByKitsuID(db *gorm.DB, kitsuProjectID string) *Project {
	var p Project
	if err := db.Where("kitsu_project_id = ?", kitsuProjectID).First(&p).Error; err != nil {
		return nil
	}
	return &p
}

func CreateProjectWebhook(db *gorm.DB, kitsuProjectID, channelName, taskType, webhookURL, channelID string) error {
	return db.Create(&ProjectWebhook{
		KitsuProjectID:   kitsuProjectID,
		ChannelName:      channelName,
		TaskType:         taskType,
		WebhookURL:       webhookURL,
		DiscordChannelID: channelID,
	}).Error
}

// DeleteWebhooksByProjectID deletes all project_webhooks rows for the given Kitsu project ID.
func DeleteWebhooksByProjectID(db *gorm.DB, kitsuProjectID string) error {
	return db.Where("kitsu_project_id = ?", kitsuProjectID).Delete(&ProjectWebhook{}).Error
}

func ListProjectWebhooks(db *gorm.DB, kitsuProjectID string) []ProjectWebhook {
	var rows []ProjectWebhook
	db.Where("kitsu_project_id = ?", kitsuProjectID).Order("id asc").Find(&rows)
	return rows
}

func FindProjectWebhookByID(db *gorm.DB, id uint) *ProjectWebhook {
	var wh ProjectWebhook
	if err := db.First(&wh, id).Error; err != nil {
		return nil
	}
	return &wh
}

func DeleteProjectWebhookByID(db *gorm.DB, id uint) {
	db.Delete(&ProjectWebhook{}, id)
}

// DeleteProjectWebhooksByChannelName deletes all webhook records that share the same
// channel name within a project (used when deleting a hierarchical channel group).
func DeleteProjectWebhooksByChannelName(db *gorm.DB, kitsuProjectID, channelName string) error {
	return db.Where("kitsu_project_id = ? AND channel_name = ?", kitsuProjectID, channelName).Delete(&ProjectWebhook{}).Error
}

// UpdateProjectWebhookURL replaces the webhook URL for an existing ProjectWebhook record.
// Used by the reconnect flow when a stale/broken webhook is replaced with a new one.
func UpdateProjectWebhookURL(db *gorm.DB, id uint, newURL string) error {
	return db.Model(&ProjectWebhook{}).Where("id = ?", id).Update("webhook_url", newURL).Error
}

// FindPendingChannel returns the task_type="" record for a channel created but not yet assigned.
// Returns nil if no pending record exists for the given project+channel combination.
func FindPendingChannel(db *gorm.DB, kitsuProjectID, channelName string) *ProjectWebhook {
	var wh ProjectWebhook
	if err := db.Where("kitsu_project_id = ? AND channel_name = ? AND task_type = ?",
		kitsuProjectID, channelName, "").First(&wh).Error; err != nil {
		return nil
	}
	return &wh
}

func ListProjects(db *gorm.DB) []Project {
	var rows []Project
	db.Order("name asc").Find(&rows)
	return rows
}

func ListProjectChannelNames(db *gorm.DB, kitsuProjectID string) []string {
	var webhooks []ProjectWebhook
	db.Where("kitsu_project_id = ?", kitsuProjectID).Find(&webhooks)
	names := make([]string, 0, len(webhooks))
	for _, wh := range webhooks {
		names = append(names, wh.ChannelName)
	}
	return names
}

// ListAllProjectWebhooks returns every webhook row across all projects.
func ListAllProjectWebhooks(db *gorm.DB) []ProjectWebhook {
	var rows []ProjectWebhook
	db.Order("kitsu_project_id asc, id asc").Find(&rows)
	return rows
}

func FindWebhookForTask(db *gorm.DB, kitsuProjectID, taskType string) string {
	var wh ProjectWebhook
	if err := db.Where("kitsu_project_id = ? AND task_type = ?", kitsuProjectID, taskType).First(&wh).Error; err == nil {
		return wh.WebhookURL
	}
	if err := db.Where("kitsu_project_id = ? AND task_type = ?", kitsuProjectID, "*").First(&wh).Error; err == nil {
		return wh.WebhookURL
	}
	return ""
}

// FindChannelNameByWebhookURL returns the channel name associated with the given webhook URL.
func FindChannelNameByWebhookURL(db *gorm.DB, webhookURL string) string {
	var wh ProjectWebhook
	if err := db.Where("webhook_url = ?", webhookURL).First(&wh).Error; err == nil {
		return wh.ChannelName
	}
	return ""
}

func FindWebhookURLForChannel(db *gorm.DB, kitsuProjectID, channelName string) string {
	var wh ProjectWebhook
	if err := db.Where("kitsu_project_id = ? AND channel_name = ?", kitsuProjectID, channelName).First(&wh).Error; err == nil {
		return wh.WebhookURL
	}
	return ""
}

// FindChannelRecord returns any webhook record for the given project+channel.
// Used when adding a second task type to an existing channel to copy webhook URL and channel ID.
func FindChannelRecord(db *gorm.DB, kitsuProjectID, channelName string) *ProjectWebhook {
	var wh ProjectWebhook
	if err := db.Where("kitsu_project_id = ? AND channel_name = ?", kitsuProjectID, channelName).First(&wh).Error; err != nil {
		return nil
	}
	return &wh
}

func GetProjectWebhook(db *gorm.DB, kitsuProjectID string) string {
	var wh ProjectWebhook
	if err := db.Where("kitsu_project_id = ? AND task_type = ?", kitsuProjectID, "*").First(&wh).Error; err == nil {
		return wh.WebhookURL
	}
	if err := db.Where("kitsu_project_id = ?", kitsuProjectID).First(&wh).Error; err == nil {
		return wh.WebhookURL
	}
	return ""
}

func SetProjectWebhook(db *gorm.DB, kitsuProjectID, webhookURL, channelID string) {
	var wh ProjectWebhook
	if err := db.Where("kitsu_project_id = ? AND task_type = ?", kitsuProjectID, "*").First(&wh).Error; err == nil {
		wh.WebhookURL = webhookURL
		if channelID != "" {
			wh.DiscordChannelID = channelID
		}
		db.Save(&wh)
		return
	}
	db.Create(&ProjectWebhook{
		KitsuProjectID:   kitsuProjectID,
		ChannelName:      "general",
		TaskType:         "*",
		WebhookURL:       webhookURL,
		DiscordChannelID: channelID,
	})
}

type UserMap struct {
	ID                 uint   `gorm:"primaryKey"`
	KitsuID            string `gorm:"index"`
	KitsuName          string `gorm:"index"`
	KitsuEmail         string
	DiscordGuildID     string `gorm:"index"`
	DiscordID          string
	DiscordDisplayName string
}

type CheckerMap struct {
	ID                uint   `gorm:"primaryKey"`
	TaskType          string `gorm:"index"`
	KitsuName         string
	KitsuEmail        string `gorm:"index"`
	DiscordID         string
	OverrideDiscordID string
}

type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

// AdminSession stores only the server-side session identity and lifetime.
// The opaque browser token is stored as a digest; Kitsu access tokens are
// intentionally never persisted in this table.
type AdminSession struct {
	ID           uint   `gorm:"primaryKey"`
	TokenHash    string `gorm:"uniqueIndex;not null"`
	Email        string
	Role         string
	Expiry       time.Time `gorm:"index"`
	BotEditUntil time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func ListUserMap(db *gorm.DB) []UserMap {
	var rows []UserMap
	db.Order("kitsu_name asc").Find(&rows)
	return rows
}

func FindDiscordIDByKitsuName(db *gorm.DB, kitsuName string) string {
	var u UserMap
	if err := db.Where("kitsu_name = ?", kitsuName).First(&u).Error; err == nil {
		return u.DiscordID
	}
	return ""
}

func FindUserMapByID(db *gorm.DB, id uint) *UserMap {
	var u UserMap
	if err := db.First(&u, id).Error; err != nil {
		return nil
	}
	return &u
}

func FindProjectUserMapByID(db *gorm.DB, id uint) *ProjectUserMap {
	var row ProjectUserMap
	if err := db.First(&row, id).Error; err != nil {
		return nil
	}
	return &row
}

func UpdateUserMap(db *gorm.DB, id uint, kitsuName, kitsuEmail, discordID string) {
	db.Model(&UserMap{}).Where("id = ?", id).Updates(map[string]interface{}{
		"kitsu_name":  kitsuName,
		"kitsu_email": kitsuEmail,
		"discord_id":  discordID,
	})
}

func UpdateUserMapDisplayName(db *gorm.DB, id uint, displayName string) {
	db.Model(&UserMap{}).Where("id = ?", id).Update("discord_display_name", strings.TrimSpace(displayName))
}

func UpsertUserMapWithIdentity(db *gorm.DB, kitsuID, kitsuName, kitsuEmail, discordGuildID, discordID, displayName string) *UserMap {
	var user UserMap
	query := db
	if strings.TrimSpace(kitsuID) != "" {
		query = query.Where("kitsu_id = ?", strings.TrimSpace(kitsuID))
	} else if strings.TrimSpace(kitsuEmail) != "" {
		query = query.Where("kitsu_email = ?", strings.TrimSpace(kitsuEmail))
	} else {
		query = query.Where("kitsu_name = ?", strings.TrimSpace(kitsuName))
	}
	if query.First(&user).Error != nil {
		user = UserMap{}
	}
	user.KitsuID = strings.TrimSpace(kitsuID)
	user.KitsuName = strings.TrimSpace(kitsuName)
	user.KitsuEmail = strings.TrimSpace(kitsuEmail)
	user.DiscordGuildID = strings.TrimSpace(discordGuildID)
	user.DiscordID = strings.TrimSpace(discordID)
	user.DiscordDisplayName = strings.TrimSpace(displayName)
	if user.ID == 0 {
		db.Create(&user)
	} else {
		db.Save(&user)
	}
	return &user
}

func DeleteUserMapByID(db *gorm.DB, id uint) {
	db.Delete(&UserMap{}, id)
}

func UpdateProjectUserMap(db *gorm.DB, id uint, kitsuName, kitsuEmail, discordUserID string) {
	db.Model(&ProjectUserMap{}).Where("id = ?", id).Updates(map[string]interface{}{
		"kitsu_name":      kitsuName,
		"kitsu_email":     kitsuEmail,
		"discord_user_id": discordUserID,
	})
}

func DeleteProjectUserMapByID(db *gorm.DB, id uint) {
	db.Delete(&ProjectUserMap{}, id)
}

func FindDiscordIDByKitsuNameOrEmail(db *gorm.DB, kitsuName, kitsuEmail string) string {
	var u UserMap
	if kitsuEmail != "" {
		if err := db.Where("kitsu_email = ?", kitsuEmail).First(&u).Error; err == nil {
			if kitsuName != "" && u.KitsuName != kitsuName {
				db.Model(&u).Update("kitsu_name", kitsuName)
			}
			return u.DiscordID
		}
	}
	if err := db.Where("kitsu_name = ?", kitsuName).First(&u).Error; err == nil {
		return u.DiscordID
	}
	return ""
}

func UpsertUserMap(db *gorm.DB, kitsuName, discordID string) {
	UpsertUserMapWithEmail(db, kitsuName, "", discordID)
}

func UpsertUserMapWithEmail(db *gorm.DB, kitsuName, kitsuEmail, discordID string) {
	var u UserMap
	found := false
	if kitsuEmail != "" {
		if err := db.Where("kitsu_email = ?", kitsuEmail).First(&u).Error; err == nil {
			found = true
		}
	}
	if !found {
		if err := db.Where("kitsu_name = ?", kitsuName).First(&u).Error; err == nil {
			found = true
		}
	}
	if found {
		u.KitsuName = kitsuName
		if kitsuEmail != "" {
			u.KitsuEmail = kitsuEmail
		}
		u.DiscordID = discordID
		db.Save(&u)
		return
	}
	db.Create(&UserMap{KitsuName: kitsuName, KitsuEmail: kitsuEmail, DiscordID: discordID})
}

func DeleteUserMap(db *gorm.DB, kitsuName string) {
	db.Where("kitsu_name = ?", kitsuName).Delete(&UserMap{})
}

func ListCheckerMap(db *gorm.DB) []CheckerMap {
	var rows []CheckerMap
	db.Order("task_type asc").Find(&rows)
	return rows
}

func FindCheckersByTaskType(db *gorm.DB, taskType string) []string {
	var rows []CheckerMap
	db.Where("task_type = ?", taskType).Find(&rows)
	ids := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, c := range rows {
		discordID := ResolveCheckerDiscordID(db, c)
		if discordID == "" || seen[discordID] {
			continue
		}
		seen[discordID] = true
		ids = append(ids, discordID)
	}
	return ids
}

func ResolveCheckerDiscordID(db *gorm.DB, row CheckerMap) string {
	if strings.TrimSpace(row.OverrideDiscordID) != "" {
		return strings.TrimSpace(row.OverrideDiscordID)
	}
	if row.KitsuName != "" || row.KitsuEmail != "" {
		if resolved := FindDiscordIDByKitsuNameOrEmail(db, row.KitsuName, row.KitsuEmail); resolved != "" {
			return resolved
		}
	}
	return strings.TrimSpace(row.DiscordID)
}

func AddCheckerMap(db *gorm.DB, taskType, discordID string) {
	var c CheckerMap
	if err := db.Where("task_type = ? AND discord_id = ?", taskType, discordID).First(&c).Error; err != nil {
		db.Create(&CheckerMap{TaskType: taskType, DiscordID: discordID})
	}
}

func AddCheckerMapByKitsuName(db *gorm.DB, taskType, kitsuName string) {
	AddCheckerMapByUser(db, taskType, kitsuName, "")
}

func AddCheckerMapByUser(db *gorm.DB, taskType, kitsuName, kitsuEmail string) {
	AddCheckerMapByUserWithOverride(db, taskType, kitsuName, kitsuEmail, "")
}

func AddCheckerMapByUserWithOverride(db *gorm.DB, taskType, kitsuName, kitsuEmail, overrideDiscordID string) {
	if strings.TrimSpace(taskType) == "" || strings.TrimSpace(kitsuName) == "" {
		return
	}
	discordID := FindDiscordIDByKitsuNameOrEmail(db, kitsuName, kitsuEmail)
	overrideDiscordID = strings.TrimSpace(overrideDiscordID)
	var c CheckerMap
	query := db.Where("task_type = ?", taskType)
	if kitsuEmail != "" {
		query = query.Where("kitsu_email = ?", kitsuEmail)
	} else {
		query = query.Where("kitsu_name = ?", kitsuName)
	}
	if err := query.First(&c).Error; err == nil {
		c.KitsuName = kitsuName
		c.KitsuEmail = kitsuEmail
		c.DiscordID = discordID
		c.OverrideDiscordID = overrideDiscordID
		db.Save(&c)
		return
	}
	db.Create(&CheckerMap{
		TaskType:          taskType,
		KitsuName:         kitsuName,
		KitsuEmail:        kitsuEmail,
		DiscordID:         discordID,
		OverrideDiscordID: overrideDiscordID,
	})
}

func UpdateCheckerMap(db *gorm.DB, id uint, taskType, kitsuName, kitsuEmail string) {
	UpdateCheckerMapWithOverride(db, id, taskType, kitsuName, kitsuEmail, "")
}

func UpdateCheckerMapWithOverride(db *gorm.DB, id uint, taskType, kitsuName, kitsuEmail, overrideDiscordID string) {
	if strings.TrimSpace(taskType) == "" || strings.TrimSpace(kitsuName) == "" {
		return
	}
	discordID := FindDiscordIDByKitsuNameOrEmail(db, kitsuName, kitsuEmail)
	overrideDiscordID = strings.TrimSpace(overrideDiscordID)
	db.Model(&CheckerMap{}).Where("id = ?", id).Updates(map[string]interface{}{
		"task_type":           taskType,
		"kitsu_name":          kitsuName,
		"kitsu_email":         kitsuEmail,
		"discord_id":          discordID,
		"override_discord_id": overrideDiscordID,
	})
}

func DeleteCheckerEntry(db *gorm.DB, taskType, discordID string) {
	db.Where("task_type = ? AND discord_id = ?", taskType, discordID).Delete(&CheckerMap{})
}

func DeleteCheckerEntryByKitsuName(db *gorm.DB, taskType, kitsuName string) {
	db.Where("task_type = ? AND kitsu_name = ?", taskType, kitsuName).Delete(&CheckerMap{})
}

func DeleteCheckerEntryByID(db *gorm.DB, id uint) {
	db.Delete(&CheckerMap{}, id)
}

func DeleteCheckerMap(db *gorm.DB, taskType string) {
	db.Where("task_type = ?", taskType).Delete(&CheckerMap{})
}

// ProjectUserMap stores project-scoped Kitsu → Discord user mappings.
// Falls back to the global UserMap when no project-scoped entry exists.
type ProjectUserMap struct {
	ID            uint   `gorm:"primaryKey"`
	ProjectID     uint   `gorm:"uniqueIndex:idx_projusermap;not null"`
	KitsuName     string `gorm:"uniqueIndex:idx_projusermap"`
	KitsuEmail    string `gorm:"index"`
	DiscordUserID string
	CreatedAt     time.Time
}

// ProjectCheckerMap stores project-scoped task type → Discord reviewer mappings.
// Falls back to the global CheckerMap when no project-scoped entry exists.
type ProjectCheckerMap struct {
	ID                uint   `gorm:"primaryKey"`
	ProjectID         uint   `gorm:"uniqueIndex:idx_projcheckermap;not null"`
	TaskType          string `gorm:"uniqueIndex:idx_projcheckermap;not null"`
	KitsuName         string
	KitsuEmail        string
	DiscordUserID     string
	OverrideDiscordID string
	CreatedAt         time.Time
}

// ProjectSetting stores per-project key-value settings.
type ProjectSetting struct {
	ID        uint   `gorm:"primaryKey"`
	ProjectID uint   `gorm:"uniqueIndex:idx_projsetting;not null"`
	Key       string `gorm:"uniqueIndex:idx_projsetting;not null"`
	Value     string
	CreatedAt time.Time
}

// GetUserMapForProject resolves a Kitsu user to a Discord ID.
// Checks the project-scoped mapping first, then falls back to the global UserMap.
func GetUserMapForProject(db *gorm.DB, kitsuProjectID, kitsuName, kitsuEmail string) string {
	if p := FindProjectByKitsuID(db, kitsuProjectID); p != nil {
		var row ProjectUserMap
		if kitsuEmail != "" {
			if err := db.Where("project_id = ? AND kitsu_email = ?", p.ID, kitsuEmail).First(&row).Error; err == nil {
				return row.DiscordUserID
			}
		}
		if err := db.Where("project_id = ? AND kitsu_name = ?", p.ID, kitsuName).First(&row).Error; err == nil {
			return row.DiscordUserID
		}
	}
	return FindDiscordIDByKitsuNameOrEmail(db, kitsuName, kitsuEmail)
}

// GetCheckerForProject resolves checker Discord IDs for a task type.
// Checks the project-scoped mapping first, then falls back to the global CheckerMap.
// Returns nil (not empty slice) when no match is found, so callers can distinguish "no project entry" from "empty list".
func GetCheckerForProject(db *gorm.DB, kitsuProjectID, taskType string) []string {
	if p := FindProjectByKitsuID(db, kitsuProjectID); p != nil {
		var rows []ProjectCheckerMap
		db.Where("project_id = ? AND task_type = ?", p.ID, taskType).Find(&rows)
		if len(rows) > 0 {
			ids := make([]string, 0, len(rows))
			seen := map[string]bool{}
			for _, c := range rows {
				discordID := resolveProjectCheckerDiscordID(db, c)
				if discordID == "" || seen[discordID] {
					continue
				}
				seen[discordID] = true
				ids = append(ids, discordID)
			}
			if len(ids) > 0 {
				return ids
			}
		}
	}
	return FindCheckersByTaskType(db, taskType)
}

func resolveProjectCheckerDiscordID(db *gorm.DB, row ProjectCheckerMap) string {
	if strings.TrimSpace(row.OverrideDiscordID) != "" {
		return strings.TrimSpace(row.OverrideDiscordID)
	}
	if row.KitsuName != "" || row.KitsuEmail != "" {
		if resolved := FindDiscordIDByKitsuNameOrEmail(db, row.KitsuName, row.KitsuEmail); resolved != "" {
			return resolved
		}
	}
	return strings.TrimSpace(row.DiscordUserID)
}

// DeleteProjectScopedData removes all project-scoped mapping rows for the given Project row ID.
// Call this before deleting the Project record itself.
func DeleteProjectScopedData(db *gorm.DB, projectRowID uint) error {
	if db == nil || projectRowID == 0 {
		return gorm.ErrInvalidData
	}
	for _, table := range []interface{}{
		&ProjectUserMap{},
		&ProjectCheckerMap{},
		&ProjectSetting{},
	} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if err := db.Where("project_id = ?", projectRowID).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}

// ListProjectUserMaps returns all user mappings for the given project row ID.
func ListProjectUserMaps(db *gorm.DB, projectRowID uint) []ProjectUserMap {
	var rows []ProjectUserMap
	db.Where("project_id = ?", projectRowID).Order("kitsu_name").Find(&rows)
	return rows
}

// ListProjectCheckerMaps returns all checker mappings for the given project row ID.
func ListProjectCheckerMaps(db *gorm.DB, projectRowID uint) []ProjectCheckerMap {
	var rows []ProjectCheckerMap
	db.Where("project_id = ?", projectRowID).Order("task_type").Find(&rows)
	return rows
}

// UpsertProjectUserMap creates or updates a project-scoped user mapping.
func UpsertProjectUserMap(db *gorm.DB, projectRowID uint, kitsuName, kitsuEmail, discordUserID string) {
	var row ProjectUserMap
	err := db.Where("project_id = ? AND kitsu_name = ?", projectRowID, kitsuName).First(&row).Error
	if err == nil {
		db.Model(&row).Updates(map[string]interface{}{
			"kitsu_email":     kitsuEmail,
			"discord_user_id": discordUserID,
		})
		return
	}
	db.Create(&ProjectUserMap{
		ProjectID:     projectRowID,
		KitsuName:     kitsuName,
		KitsuEmail:    kitsuEmail,
		DiscordUserID: discordUserID,
	})
}

// UpsertProjectCheckerMap creates or updates a project-scoped checker mapping.
func UpsertProjectCheckerMap(db *gorm.DB, projectRowID uint, taskType, discordUserID string) {
	var row ProjectCheckerMap
	err := db.Where("project_id = ? AND task_type = ?", projectRowID, taskType).First(&row).Error
	if err == nil {
		db.Model(&row).Update("discord_user_id", discordUserID)
		return
	}
	db.Create(&ProjectCheckerMap{
		ProjectID:     projectRowID,
		TaskType:      taskType,
		DiscordUserID: discordUserID,
	})
}

// DeleteProjectUserMapByName removes a project-scoped user mapping by Kitsu name.
func DeleteProjectUserMapByName(db *gorm.DB, projectRowID uint, kitsuName string) {
	db.Where("project_id = ? AND kitsu_name = ?", projectRowID, kitsuName).Delete(&ProjectUserMap{})
}

// DeleteProjectCheckerMapByTaskType removes a project-scoped checker mapping by task type.
func DeleteProjectCheckerMapByTaskType(db *gorm.DB, projectRowID uint, taskType string) {
	db.Where("project_id = ? AND task_type = ?", projectRowID, taskType).Delete(&ProjectCheckerMap{})
}

func FindProjectCheckerMapByID(db *gorm.DB, id uint) *ProjectCheckerMap {
	var row ProjectCheckerMap
	if err := db.First(&row, id).Error; err != nil {
		return nil
	}
	return &row
}

func UpsertProjectCheckerMapWithUser(db *gorm.DB, projectRowID uint, taskType, kitsuName, kitsuEmail, discordUserID, overrideDiscordID string) {
	var row ProjectCheckerMap
	err := db.Where("project_id = ? AND task_type = ?", projectRowID, taskType).First(&row).Error
	if err == nil {
		db.Model(&row).Updates(map[string]interface{}{
			"kitsu_name":          kitsuName,
			"kitsu_email":         kitsuEmail,
			"discord_user_id":     discordUserID,
			"override_discord_id": overrideDiscordID,
		})
		return
	}
	db.Create(&ProjectCheckerMap{
		ProjectID:         projectRowID,
		TaskType:          taskType,
		KitsuName:         kitsuName,
		KitsuEmail:        kitsuEmail,
		DiscordUserID:     discordUserID,
		OverrideDiscordID: overrideDiscordID,
	})
}

func UpdateProjectCheckerMapWithUser(db *gorm.DB, id uint, taskType, kitsuName, kitsuEmail, discordUserID, overrideDiscordID string) {
	db.Model(&ProjectCheckerMap{}).Where("id = ?", id).Updates(map[string]interface{}{
		"task_type":           taskType,
		"kitsu_name":          kitsuName,
		"kitsu_email":         kitsuEmail,
		"discord_user_id":     discordUserID,
		"override_discord_id": overrideDiscordID,
	})
}

func DeleteProjectCheckerMapByID(db *gorm.DB, id uint) {
	db.Delete(&ProjectCheckerMap{}, id)
}

func SetProjectStorageURL(db *gorm.DB, kitsuProjectID, storageURL string) {
	db.Model(&Project{}).Where("kitsu_project_id = ?", kitsuProjectID).Update("storage_url", storageURL)
}

func GetProjectStorageURL(db *gorm.DB, kitsuProjectID string) string {
	p := FindProjectByKitsuID(db, kitsuProjectID)
	if p == nil {
		return ""
	}
	return p.StorageURL
}

func GetSetting(db *gorm.DB, key string) string {
	var s Setting
	if err := db.Where("key = ?", key).First(&s).Error; err != nil {
		return ""
	}
	return s.Value
}

func SetSetting(db *gorm.DB, key, value string) {
	if IsSecretSettingKey(key) {
		return
	}
	var s Setting
	if err := db.Where("key = ?", key).First(&s).Error; err != nil {
		db.Create(&Setting{Key: key, Value: value})
		return
	}
	s.Value = value
	db.Save(&s)
}

func SetSecretSetting(db *gorm.DB, key, value string) {
	_ = SetSecretSettingWithError(db, key, value)
}

func SetSecretSettingWithError(db *gorm.DB, key, value string) error {
	var s Setting
	if err := db.Where("key = ?", key).First(&s).Error; err != nil {
		return db.Create(&Setting{Key: key, Value: value}).Error
	}
	s.Value = value
	return db.Save(&s).Error
}

func DeleteSetting(db *gorm.DB, key string) {
	db.Where("key = ?", key).Delete(&Setting{})
}

func IsSecretSettingKey(key string) bool {
	switch key {
	case "kitsu.password", "kitsu.runtime_password_encrypted", "kitsu.runtime_token_encrypted", "discord.botToken", "discord.webhookURL", "discord.runtime_bot_token":
		return true
	default:
		return false
	}
}

func PurgeLegacySensitiveData(db *gorm.DB) {
	for _, key := range []string{"kitsu.password", "discord.botToken", "discord.webhookURL"} {
		DeleteSetting(db, key)
	}
	db.Model(&Task{}).Where("webhook_url <> ''").Update("webhook_url", "")
}
