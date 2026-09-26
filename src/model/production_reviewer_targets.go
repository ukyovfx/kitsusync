package model

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	ReviewerTargetUser = "user"
	ReviewerTargetRole = "role"
)

// ProjectReviewerTarget is the additive, multi-target Production override.
// The old single-target ProjectCheckerMap remains readable for compatibility.
type ProjectReviewerTarget struct {
	ID           uint   `gorm:"primaryKey"`
	ProjectID    uint   `gorm:"uniqueIndex:idx_projreviewer_target,priority:1;not null"`
	TaskTypeID   string `gorm:"uniqueIndex:idx_projreviewer_target,priority:2;not null"`
	TaskTypeName string
	TargetKind   string `gorm:"uniqueIndex:idx_projreviewer_target,priority:3;not null"`
	DiscordID    string `gorm:"uniqueIndex:idx_projreviewer_target,priority:4;not null"`
	CreatedAt    time.Time
}

func (ProjectReviewerTarget) TableName() string { return "project_reviewer_targets" }

func validReviewerTarget(kind, id string) bool {
	if kind != ReviewerTargetUser && kind != ReviewerTargetRole {
		return false
	}
	id = strings.TrimSpace(id)
	if len(id) < 17 || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func UpsertProjectReviewerTarget(db *gorm.DB, projectID uint, taskTypeID, taskTypeName, kind, discordID string) error {
	if db == nil || projectID == 0 || strings.TrimSpace(taskTypeID) == "" || !validReviewerTarget(kind, discordID) {
		return gorm.ErrInvalidData
	}
	target := ProjectReviewerTarget{
		ProjectID:    projectID,
		TaskTypeID:   strings.TrimSpace(taskTypeID),
		TaskTypeName: strings.TrimSpace(taskTypeName),
		TargetKind:   kind,
		DiscordID:    strings.TrimSpace(discordID),
	}
	var existing ProjectReviewerTarget
	err := db.Where("project_id = ? AND task_type_id = ? AND target_kind = ? AND discord_id = ?", target.ProjectID, target.TaskTypeID, target.TargetKind, target.DiscordID).First(&existing).Error
	if err == nil {
		return db.Model(&existing).Update("task_type_name", target.TaskTypeName).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return db.Create(&target).Error
}

func ListProjectReviewerTargets(db *gorm.DB, projectID uint) []ProjectReviewerTarget {
	var rows []ProjectReviewerTarget
	if db == nil {
		return rows
	}
	_ = db.Where("project_id = ?", projectID).Order("task_type_id asc, target_kind asc, discord_id asc, id asc").Find(&rows).Error
	return rows
}

func ListProjectReviewerTargetsForTaskType(db *gorm.DB, projectID uint, taskTypeID string) ([]ProjectReviewerTarget, bool, error) {
	var rows []ProjectReviewerTarget
	if db == nil || projectID == 0 || strings.TrimSpace(taskTypeID) == "" {
		return rows, false, gorm.ErrInvalidData
	}
	err := db.Where("project_id = ? AND task_type_id = ?", projectID, strings.TrimSpace(taskTypeID)).
		Order("target_kind asc, discord_id asc, id asc").Find(&rows).Error
	return rows, len(rows) > 0, err
}

func DeleteProjectReviewerTarget(db *gorm.DB, projectID, targetID uint) error {
	if db == nil || projectID == 0 || targetID == 0 {
		return gorm.ErrInvalidData
	}
	return db.Where("project_id = ? AND id = ?", projectID, targetID).Delete(&ProjectReviewerTarget{}).Error
}

func DeleteProjectReviewerTargetsForTaskType(db *gorm.DB, projectID uint, taskTypeID string) error {
	if db == nil || projectID == 0 || strings.TrimSpace(taskTypeID) == "" {
		return gorm.ErrInvalidData
	}
	return db.Where("project_id = ? AND task_type_id = ?", projectID, strings.TrimSpace(taskTypeID)).Delete(&ProjectReviewerTarget{}).Error
}

func DeleteProjectReviewerTargetsForDiscordUser(db *gorm.DB, projectID uint, discordID string) error {
	if db == nil || projectID == 0 || strings.TrimSpace(discordID) == "" {
		return gorm.ErrInvalidData
	}
	return db.Where("project_id = ? AND target_kind = ? AND discord_id = ?", projectID, ReviewerTargetUser, strings.TrimSpace(discordID)).Delete(&ProjectReviewerTarget{}).Error
}

// ResolveProjectReviewerTargets reports both the selected target set and
// whether explicit rows exist. Presence suppresses all automatic/legacy
// fallback even if a stale target is later rejected at delivery time.
func ResolveProjectReviewerTargets(db *gorm.DB, projectID uint, taskTypeID string) ([]ProjectReviewerTarget, bool, error) {
	rows, exists, err := ListProjectReviewerTargetsForTaskType(db, projectID, taskTypeID)
	if err != nil || !exists {
		return nil, exists, err
	}
	valid := rows[:0]
	for _, row := range rows {
		if !validReviewerTarget(row.TargetKind, row.DiscordID) {
			continue
		}
		if row.TargetKind == ReviewerTargetUser {
			var associations int64
			if err := db.Model(&UserMap{}).Where("discord_id = ?", strings.TrimSpace(row.DiscordID)).Count(&associations).Error; err != nil {
				return nil, true, err
			}
			if associations == 0 {
				continue
			}
		}
		valid = append(valid, row)
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].TargetKind != valid[j].TargetKind {
			return valid[i].TargetKind < valid[j].TargetKind
		}
		if valid[i].DiscordID != valid[j].DiscordID {
			return valid[i].DiscordID < valid[j].DiscordID
		}
		return valid[i].ID < valid[j].ID
	})
	if len(valid) == 0 {
		return nil, true, fmt.Errorf("explicit Production Reviewer targets are invalid")
	}
	return valid, true, nil
}

// ResolveReviewerTargetsForProjectWithSupervisors applies WFA precedence.
// The bool is true when new explicit Production targets exist; callers must
// not fall through to a legacy or automatic target when that set is stale.
func ResolveReviewerTargetsForProjectWithSupervisors(db *gorm.DB, baseURL, token, kitsuProjectID, taskTypeID, taskTypeName string) ([]ProjectReviewerTarget, bool, error) {
	if db == nil {
		return nil, false, gorm.ErrInvalidDB
	}
	project := FindProjectByKitsuID(db, kitsuProjectID)
	if project == nil {
		return nil, false, nil
	}
	if targets, explicit, err := ResolveProjectReviewerTargets(db, project.ID, taskTypeID); explicit || err != nil {
		return targets, explicit, err
	}
	userTargets := func(ids []string) []ProjectReviewerTarget {
		result := make([]ProjectReviewerTarget, 0, len(ids))
		for _, id := range ids {
			if validReviewerTarget(ReviewerTargetUser, id) {
				result = append(result, ProjectReviewerTarget{TargetKind: ReviewerTargetUser, DiscordID: strings.TrimSpace(id)})
			}
		}
		return result
	}
	if ids := GetProjectCheckerForTaskTypeID(db, kitsuProjectID, taskTypeID, taskTypeName); len(ids) > 0 {
		return userTargets(ids), false, nil
	}
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" {
		return userTargets(FindCheckersByTaskTypeID(db, taskTypeID, taskTypeName)), false, fmt.Errorf("Kitsu Supervisor resolution requires a runtime endpoint and token")
	}
	supervisors, err := ResolveProjectTaskTypeSupervisorDiscordIDs(db, baseURL, token, kitsuProjectID, taskTypeID)
	if err != nil {
		return userTargets(FindCheckersByTaskTypeID(db, taskTypeID, taskTypeName)), false, err
	}
	if len(supervisors) > 0 {
		return userTargets(supervisors), false, nil
	}
	return userTargets(FindCheckersByTaskTypeID(db, taskTypeID, taskTypeName)), false, nil
}
