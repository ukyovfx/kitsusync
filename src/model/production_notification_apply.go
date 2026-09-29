package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

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

func productionNotificationRevision(db *gorm.DB, projectID uint, productionID string) string {
	routes := ListProductionNotificationRoutes(db, productionID)
	canonicalRoutes := make([]notificationRouteRevision, 0, len(routes))
	for _, route := range routes {
		canonicalRoutes = append(canonicalRoutes, notificationRouteRevision{strings.TrimSpace(route.TaskTypeID), route.DestinationWebhookID})
	}
	targets := ListProjectReviewerTargets(db, projectID)
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
	return hex.EncodeToString(sum[:])
}

func ProductionNotificationRevision(db *gorm.DB, projectID uint, productionID string) string {
	if db == nil {
		return ""
	}
	return productionNotificationRevision(db, projectID, productionID)
}

// ApplyProductionNotificationState atomically replaces route state, applies
// additive User/Role deltas, and removes targets belonging to deleted routes.
func ApplyProductionNotificationState(db *gorm.DB, projectID uint, productionID, productionName, expectedRevision string, routes []ProductionNotificationRoute, changes []ProductionReviewerDelta) (string, error) {
	if db == nil || projectID == 0 || strings.TrimSpace(productionID) == "" || strings.TrimSpace(expectedRevision) == "" {
		return "", gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	var newRevision string
	err := db.Transaction(func(tx *gorm.DB) error {
		if got := productionNotificationRevision(tx, projectID, productionID); got != expectedRevision {
			return ErrProductionNotificationRevisionConflict
		}
		live := make(map[string]struct{}, len(routes))
		for i := range routes {
			id := strings.TrimSpace(routes[i].TaskTypeID)
			if id == "" || routes[i].DestinationWebhookID == 0 {
				return gorm.ErrInvalidData
			}
			if _, ok := live[id]; ok {
				return gorm.ErrInvalidData
			}
			live[id] = struct{}{}
			routes[i].ProductionID = productionID
		}
		for _, delta := range changes {
			if _, ok := live[strings.TrimSpace(delta.TaskTypeID)]; !ok {
				return ErrReviewerDeltaForRemovedRoute
			}
		}
		if len(routes) == 0 {
			return gorm.ErrInvalidData
		}
		oldRoutes := ListProductionNotificationRoutes(tx, productionID)
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
		newRevision = productionNotificationRevision(tx, projectID, productionID)
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

func RestoreProductionNotificationState(db *gorm.DB, projectID uint, productionID, expectedCurrentRevision string, config *ProductionNotificationConfig, routes []ProductionNotificationRoute, targets []ProjectReviewerTarget) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if expectedCurrentRevision == "" || productionNotificationRevision(tx, projectID, productionID) != expectedCurrentRevision {
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
