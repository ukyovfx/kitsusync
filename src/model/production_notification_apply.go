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

type forumRouteRevision struct {
	TaskTypeID string `json:"task_type_id"`
	ChannelID  string `json:"channel_id"`
}
type notificationTargetRevision struct {
	TaskTypeID string `json:"task_type_id"`
	Kind       string `json:"kind"`
	DiscordID  string `json:"discord_id"`
}

// productionNotificationRevision hashes the forum routes plus the reviewer
// targets so the reviewer editor can detect stale edits.
func productionNotificationRevision(db *gorm.DB, projectID uint, productionID string) (string, error) {
	if db == nil {
		return "", gorm.ErrInvalidDB
	}
	settings, _, err := LoadProductionForumSettings(db, strings.TrimSpace(productionID))
	if err != nil {
		return "", err
	}
	canonicalRoutes := make([]forumRouteRevision, 0, len(settings.Routes))
	for _, route := range settings.Routes {
		canonicalRoutes = append(canonicalRoutes, forumRouteRevision{strings.TrimSpace(route.TaskTypeID), strings.TrimSpace(route.ForumChannelID)})
	}
	sort.Slice(canonicalRoutes, func(i, j int) bool {
		if canonicalRoutes[i].TaskTypeID != canonicalRoutes[j].TaskTypeID {
			return canonicalRoutes[i].TaskTypeID < canonicalRoutes[j].TaskTypeID
		}
		return canonicalRoutes[i].ChannelID < canonicalRoutes[j].ChannelID
	})
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
		Routes  []forumRouteRevision         `json:"routes"`
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

// ApplyProductionReviewerDeltas applies reviewer add/remove deltas for task
// types that exist as forum routes, under the production write lock and an
// optimistic-concurrency check. Routes themselves are managed by the forum
// settings screen, not here.
func ApplyProductionReviewerDeltas(db *gorm.DB, projectID uint, productionID, expectedRevision string, changes []ProductionReviewerDelta) (string, error) {
	if db == nil || projectID == 0 || strings.TrimSpace(productionID) == "" {
		return "", gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	var newRevision string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := AcquireProductionNotificationWriteLock(tx, productionID); err != nil {
			return err
		}
		if err := EnsureProductionNotificationMutationIdle(tx, productionID, ""); err != nil {
			return err
		}
		got, err := productionNotificationRevision(tx, projectID, productionID)
		if err != nil {
			return err
		}
		if got != expectedRevision {
			return ErrProductionNotificationRevisionConflict
		}
		settings, _, err := LoadProductionForumSettings(tx, productionID)
		if err != nil {
			return err
		}
		live := make(map[string]struct{}, len(settings.Routes))
		for _, route := range settings.Routes {
			live[strings.TrimSpace(route.TaskTypeID)] = struct{}{}
		}
		for _, delta := range changes {
			if _, ok := live[strings.TrimSpace(delta.TaskTypeID)]; !ok {
				return fmt.Errorf("%w: %s", ErrReviewerDeltaForRemovedRoute, strings.TrimSpace(delta.TaskTypeID))
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
