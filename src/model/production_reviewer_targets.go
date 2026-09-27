package model

import (
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
