package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrProductionNotificationRevisionConflict = errors.New("production notification revision conflict")
	ErrReviewerDeltaForRemovedRoute           = errors.New("reviewer changes target a removed route")
)

type ProductionReviewerDelta struct {
	TaskTypeID    string   `json:"task_type_id"`
	AddUserIDs    []string `json:"add_user_ids"`
	RemoveUserIDs []string `json:"remove_user_ids"`
	AddRoleIDs    []string `json:"add_role_ids"`
	RemoveRoleIDs []string `json:"remove_role_ids"`
}

type notificationRouteRevision struct {
	TaskTypeID string `json:"task_type_id"`
	WebhookID  uint   `json:"webhook_id"`
}
type notificationTargetRevision struct {
	TaskTypeID string `json:"task_type_id"`
	Kind       string `json:"kind"`
	DiscordID  string `json:"discord_id"`
}

func productionNotificationRevision(db *gorm.DB, projectID uint, productionID string) (string, error) {
	if db == nil {
		return "", gorm.ErrInvalidDB
	}
	var routes []ProductionNotificationRoute
	if err := db.Where("production_id = ?", strings.TrimSpace(productionID)).Order("id asc").Find(&routes).Error; err != nil {
		return "", err
	}
	canonicalRoutes := make([]notificationRouteRevision, 0, len(routes))
	for _, route := range routes {
		canonicalRoutes = append(canonicalRoutes, notificationRouteRevision{strings.TrimSpace(route.TaskTypeID), route.DestinationWebhookID})
	}
	var targets []ProjectReviewerTarget
	if err := db.Where("project_id = ?", projectID).Order("id asc").Find(&targets).Error; err != nil {
		return "", err
	}
	canonicalTargets := make([]notificationTargetRevision, 0, len(targets))
	for _, target := range targets {
		canonicalTargets = append(canonicalTargets, notificationTargetRevision{strings.TrimSpace(target.TaskTypeID), target.TargetKind, strings.TrimSpace(target.DiscordID)})
	}
	sort.Slice(canonicalTargets, func(i, j int) bool {
		a, b := canonicalTargets[i], canonicalTargets[j]
		if a.TaskTypeID != b.TaskTypeID {
			return a.TaskTypeID < b.TaskTypeID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.DiscordID < b.DiscordID
	})
	data, _ := json.Marshal(struct {
		Routes  []notificationRouteRevision  `json:"routes"`
		Targets []notificationTargetRevision `json:"targets"`
	}{canonicalRoutes, canonicalTargets})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func ProductionNotificationRevision(db *gorm.DB, projectID uint, productionID string) string {
	revision, _ := ProductionNotificationRevisionWithError(db, projectID, productionID)
	return revision
}

// ProductionNotificationRevisionWithError computes the canonical routing
// revision and propagates storage failures instead of treating them as an
// empty configuration.
func ProductionNotificationRevisionWithError(db *gorm.DB, projectID uint, productionID string) (string, error) {
	return productionNotificationRevision(db, projectID, productionID)
}

// ApplyProductionNotificationState atomically replaces route state, applies
// additive User/Role deltas, and removes targets belonging to deleted routes.
func ApplyProductionNotificationState(db *gorm.DB, projectID uint, productionID, productionName, expectedRevision string, routes []ProductionNotificationRoute, changes []ProductionReviewerDelta, createdWebhooks ...*ProjectWebhook) (string, error) {
	return applyProductionNotificationState(db, projectID, productionID, productionName, expectedRevision, routes, changes, "", createdWebhooks...)
}

// ApplyProductionNotificationStateForExternalReorder commits the new routing
// revision together with a durable dispatch barrier. The caller must clear or
// quarantine that barrier after Discord ordering/compensation finishes.
func ApplyProductionNotificationStateForExternalReorder(db *gorm.DB, projectID uint, productionID, productionName, expectedRevision string, routes []ProductionNotificationRoute, changes []ProductionReviewerDelta, mutationToken string, createdWebhooks ...*ProjectWebhook) (string, error) {
	if strings.TrimSpace(mutationToken) == "" {
		return "", gorm.ErrInvalidData
	}
	return applyProductionNotificationState(db, projectID, productionID, productionName, expectedRevision, routes, changes, mutationToken, createdWebhooks...)
}

func NewProductionNotificationMutationToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func applyProductionNotificationState(db *gorm.DB, projectID uint, productionID, productionName, expectedRevision string, routes []ProductionNotificationRoute, changes []ProductionReviewerDelta, mutationToken string, createdWebhooks ...*ProjectWebhook) (string, error) {
	if db == nil || projectID == 0 || strings.TrimSpace(productionID) == "" || strings.TrimSpace(expectedRevision) == "" {
		return "", gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	var newRevision string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		if err := EnsureNoUnresolvedNotificationDeliveries(tx, productionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, productionID, ""); err != nil {
			return err
		}
		if err := EnsureNoUnresolvedManagedChannelDeletions(tx, productionID); err != nil {
			return err
		}
		got, err := productionNotificationRevision(tx, projectID, productionID)
		if err != nil {
			return err
		}
		if got != expectedRevision {
			return ErrProductionNotificationRevisionConflict
		}
		if mutationToken != "" {
			now := time.Now().UTC()
			result := tx.Model(&ProductionNotificationWriteLock{}).Where("production_id = ? AND mutation_state = ''", productionID).Updates(map[string]any{"mutation_state": NotificationMutationReordering, "mutation_token": mutationToken, "mutation_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrNotificationMutationRecoveryRequired
			}
		}
		webhooksByTask := make(map[string]*ProjectWebhook, len(createdWebhooks))
		for _, webhook := range createdWebhooks {
			if webhook == nil || strings.TrimSpace(webhook.KitsuProjectID) != productionID || strings.TrimSpace(webhook.TaskType) == "" || strings.TrimSpace(webhook.ChannelName) == "" || strings.TrimSpace(webhook.DiscordChannelID) == "" || strings.TrimSpace(webhook.WebhookURL) == "" {
				return gorm.ErrInvalidData
			}
			taskTypeID := strings.TrimSpace(webhook.TaskType)
			if _, exists := webhooksByTask[taskTypeID]; exists {
				return gorm.ErrInvalidData
			}
			webhook.ID = 0
			webhook.KitsuProjectID = productionID
			webhooksByTask[taskTypeID] = webhook
		}
		live := make(map[string]struct{}, len(routes))
		for i := range routes {
			id := strings.TrimSpace(routes[i].TaskTypeID)
			if id == "" {
				return gorm.ErrInvalidData
			}
			if routes[i].DestinationWebhookID == 0 {
				webhook := webhooksByTask[id]
				if webhook == nil {
					return gorm.ErrInvalidData
				}
				if err := tx.Create(webhook).Error; err != nil {
					return err
				}
				routes[i].DestinationWebhookID = webhook.ID
				delete(webhooksByTask, id)
			}
			if _, ok := live[id]; ok {
				return gorm.ErrInvalidData
			}
			live[id] = struct{}{}
			routes[i].ProductionID = productionID
		}
		if len(webhooksByTask) != 0 {
			return gorm.ErrInvalidData
		}
		if issues := ValidateProductionNotificationConfig(tx, productionID, routes); len(issues) != 0 {
			return gorm.ErrInvalidData
		}
		for _, route := range routes {
			var tombstones int64
			if err := tx.Model(&ManagedChannelDeletion{}).Where("webhook_row_id = ? AND disposition <> ?", route.DestinationWebhookID, ChannelDeletionComplete).Count(&tombstones).Error; err != nil {
				return err
			}
			if tombstones > 0 {
				return ErrChannelDeletionConflict
			}
		}
		for _, delta := range changes {
			if _, ok := live[strings.TrimSpace(delta.TaskTypeID)]; !ok {
				return ErrReviewerDeltaForRemovedRoute
			}
		}
		if len(routes) == 0 {
			return gorm.ErrInvalidData
		}
		var oldRoutes []ProductionNotificationRoute
		if err := tx.Where("production_id = ?", productionID).Order("id asc").Find(&oldRoutes).Error; err != nil {
			return err
		}
		oldByID := make(map[string]struct{}, len(oldRoutes))
		for _, route := range oldRoutes {
			oldByID[route.TaskTypeID] = struct{}{}
		}
		config := ProductionNotificationConfig{ProductionID: productionID, ProductionName: strings.TrimSpace(productionName), Enabled: true}
		var existing ProductionNotificationConfig
		if err := tx.Where("production_id = ?", productionID).First(&existing).Error; err == nil {
			config.ID = existing.ID
			config.CreatedAt = existing.CreatedAt
			config.ProductionName = existing.ProductionName
			config.Enabled = existing.Enabled
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Save(&config).Error; err != nil {
			return err
		}
		if err := tx.Where("production_id = ?", productionID).Delete(&ProductionNotificationRoute{}).Error; err != nil {
			return err
		}
		for i := range routes {
			routes[i].ID = 0
			if err := tx.Create(&routes[i]).Error; err != nil {
				return err
			}
		}
		for id := range oldByID {
			if _, ok := live[id]; !ok {
				if err := tx.Where("project_id = ? AND task_type_id = ?", projectID, id).Delete(&ProjectReviewerTarget{}).Error; err != nil {
					return err
				}
			}
		}
		for _, delta := range changes {
			for _, entry := range []struct {
				kind        string
				add, remove []string
			}{{ReviewerTargetUser, delta.AddUserIDs, delta.RemoveUserIDs}, {ReviewerTargetRole, delta.AddRoleIDs, delta.RemoveRoleIDs}} {
				for _, id := range entry.remove {
					if err := tx.Where("project_id = ? AND task_type_id = ? AND target_kind = ? AND discord_id = ?", projectID, strings.TrimSpace(delta.TaskTypeID), entry.kind, strings.TrimSpace(id)).Delete(&ProjectReviewerTarget{}).Error; err != nil {
						return err
					}
				}
				for _, id := range entry.add {
					id = strings.TrimSpace(id)
					if !validReviewerTarget(entry.kind, id) {
						return gorm.ErrInvalidData
					}
					target := ProjectReviewerTarget{ProjectID: projectID, TaskTypeID: strings.TrimSpace(delta.TaskTypeID), TargetKind: entry.kind, DiscordID: id}
					var found ProjectReviewerTarget
					err := tx.Where("project_id = ? AND task_type_id = ? AND target_kind = ? AND discord_id = ?", target.ProjectID, target.TaskTypeID, target.TargetKind, target.DiscordID).First(&found).Error
					if errors.Is(err, gorm.ErrRecordNotFound) {
						if err := tx.Create(&target).Error; err != nil {
							return err
						}
					} else if err != nil {
						return err
					}
				}
			}
		}
		newRevision, err = productionNotificationRevision(tx, projectID, productionID)
		if err != nil {
			return err
		}
		return nil
	})
	return newRevision, err
}

func SnapshotProductionNotificationState(db *gorm.DB, projectID uint, productionID string) (*ProductionNotificationConfig, []ProductionNotificationRoute, []ProjectReviewerTarget, error) {
	if db == nil {
		return nil, nil, nil, gorm.ErrInvalidDB
	}
	var config *ProductionNotificationConfig
	var row ProductionNotificationConfig
	if err := db.Where("production_id = ?", strings.TrimSpace(productionID)).First(&row).Error; err == nil {
		config = &row
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil, err
	}
	var routes []ProductionNotificationRoute
	if err := db.Where("production_id = ?", strings.TrimSpace(productionID)).Order("id asc").Find(&routes).Error; err != nil {
		return nil, nil, nil, err
	}
	var targets []ProjectReviewerTarget
	if err := db.Where("project_id = ?", projectID).Order("id asc").Find(&targets).Error; err != nil {
		return nil, nil, nil, err
	}
	return config, routes, targets, nil
}

func RestoreProductionNotificationState(db *gorm.DB, projectID uint, productionID, expectedCurrentRevision string, config *ProductionNotificationConfig, routes []ProductionNotificationRoute, targets []ProjectReviewerTarget, cleanupWebhookIDs ...uint) error {
	return restoreProductionNotificationState(db, projectID, productionID, expectedCurrentRevision, config, routes, targets, "", cleanupWebhookIDs...)
}

func RestoreProductionNotificationStateForExternalReorder(db *gorm.DB, projectID uint, productionID, expectedCurrentRevision string, config *ProductionNotificationConfig, routes []ProductionNotificationRoute, targets []ProjectReviewerTarget, mutationToken string, cleanupWebhookIDs ...uint) error {
	if strings.TrimSpace(mutationToken) == "" {
		return gorm.ErrInvalidData
	}
	return restoreProductionNotificationState(db, projectID, productionID, expectedCurrentRevision, config, routes, targets, mutationToken, cleanupWebhookIDs...)
}

func restoreProductionNotificationState(db *gorm.DB, projectID uint, productionID, expectedCurrentRevision string, config *ProductionNotificationConfig, routes []ProductionNotificationRoute, targets []ProjectReviewerTarget, mutationToken string, cleanupWebhookIDs ...uint) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		if mutationToken == "" {
			if err := EnsureProductionNotificationMutationIdle(tx, productionID, ""); err != nil {
				return err
			}
		} else {
			var lock ProductionNotificationWriteLock
			if err := tx.Where("production_id = ? AND mutation_state = ? AND mutation_token = ?", productionID, NotificationMutationReordering, mutationToken).First(&lock).Error; err != nil {
				return ErrNotificationMutationRecoveryRequired
			}
		}
		if err := EnsureNoInFlightNotificationDeliveries(tx, productionID); err != nil {
			return err
		}
		currentRevision, err := productionNotificationRevision(tx, projectID, productionID)
		if err != nil {
			return err
		}
		if expectedCurrentRevision == "" || currentRevision != expectedCurrentRevision {
			return ErrProductionNotificationRevisionConflict
		}
		if err := tx.Where("production_id = ?", productionID).Delete(&ProductionNotificationRoute{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", projectID).Delete(&ProjectReviewerTarget{}).Error; err != nil {
			return err
		}
		if err := tx.Where("production_id = ?", productionID).Delete(&ProductionNotificationConfig{}).Error; err != nil {
			return err
		}
		for _, id := range cleanupWebhookIDs {
			if id == 0 {
				continue
			}
			if err := tx.Where("id = ? AND kitsu_project_id = ?", id, strings.TrimSpace(productionID)).Delete(&ProjectWebhook{}).Error; err != nil {
				return err
			}
		}
		if config != nil {
			copy := *config
			if err := tx.Create(&copy).Error; err != nil {
				return err
			}
		}
		for i := range routes {
			copy := routes[i]
			copy.ID = 0
			if err := tx.Create(&copy).Error; err != nil {
				return err
			}
		}
		for i := range targets {
			copy := targets[i]
			copy.ID = 0
			if err := tx.Create(&copy).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func FinishProductionNotificationExternalMutation(db *gorm.DB, productionID, mutationToken string, recoveryRequired bool) error {
	if db == nil || strings.TrimSpace(productionID) == "" || strings.TrimSpace(mutationToken) == "" {
		return gorm.ErrInvalidData
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		updates := map[string]any{"mutation_state": "", "mutation_token": "", "mutation_at": nil}
		if recoveryRequired {
			updates["mutation_state"] = "reorder_needs_help"
			// Retain the durable attempt identity so the explicit recovery path can
			// claim this exact quarantined mutation with a fresh token.
			updates["mutation_token"] = strings.TrimSpace(mutationToken)
		}
		result := tx.Model(&ProductionNotificationWriteLock{}).Where("production_id = ? AND mutation_state = ? AND mutation_token = ?", strings.TrimSpace(productionID), NotificationMutationReordering, strings.TrimSpace(mutationToken)).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotificationMutationRecoveryRequired
		}
		return nil
	})
}

func ValidateReviewerDeltasForRemovedRoutes(routes []ProductionNotificationRoute, changes []ProductionReviewerDelta) error {
	live := map[string]struct{}{}
	for _, route := range routes {
		live[strings.TrimSpace(route.TaskTypeID)] = struct{}{}
	}
	for _, change := range changes {
		if _, ok := live[strings.TrimSpace(change.TaskTypeID)]; !ok {
			return fmt.Errorf("%w: %s", ErrReviewerDeltaForRemovedRoute, strings.TrimSpace(change.TaskTypeID))
		}
	}
	return nil
}
