package model

import (
	"strings"
	"time"

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
	if len(log.WebhookURL) > 40 {
		log.WebhookURL = log.WebhookURL[:40] + "..."
	}
	db.Create(&log)
}

func ListAuditLogs(db *gorm.DB, limit int) []AuditLog {
	var logs []AuditLog
	db.Order("created_at desc").Limit(limit).Find(&logs)
	return logs
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
	db.Model(&Task{}).Where("task_id = ?", taskID).Updates(updates)
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
	ID                uint   `gorm:"primaryKey"`
	KitsuProjectID    string `gorm:"uniqueIndex"`
	Name              string
	ProjectType       string
	DiscordGuildID    string `gorm:"index"`
	DiscordCategoryID string
	Language          string
	StorageURL        string
}

type ProjectWebhook struct {
	ID               uint   `gorm:"primaryKey"`
	KitsuProjectID   string `gorm:"index"`
	ChannelName      string
	TaskType         string
	WebhookURL       string
	DiscordChannelID string
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
	return db.Create(&Project{
		KitsuProjectID:    kitsuProjectID,
		Name:              name,
		ProjectType:       projectType,
		DiscordGuildID:    guildID,
		DiscordCategoryID: categoryID,
		Language:          language,
	}).Error
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
	ID         uint   `gorm:"primaryKey"`
	KitsuName  string `gorm:"index"`
	KitsuEmail string
	DiscordID  string
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
func DeleteProjectScopedData(db *gorm.DB, projectRowID uint) {
	db.Where("project_id = ?", projectRowID).Delete(&ProjectUserMap{})
	db.Where("project_id = ?", projectRowID).Delete(&ProjectCheckerMap{})
	db.Where("project_id = ?", projectRowID).Delete(&ProjectSetting{})
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
