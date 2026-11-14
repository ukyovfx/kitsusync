package model

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Notification modes a Production can run in. Lane (text channel + webhook) is
// the default; forum is opt-in per Production until the lane path is removed.
const (
	NotificationModeLane  = "lane"
	NotificationModeForum = "forum"
)

// Forum roles a Kitsu status can play. Only wfa/retake/done trigger a post or a
// mention; wip only moves the tag. A status without a role is unmanaged.
const (
	ForumRoleWFA    = "wfa"
	ForumRoleRetake = "retake"
	ForumRoleDone   = "done"
	ForumRoleWIP    = "wip"
)

func validForumRole(role string) bool {
	switch role {
	case ForumRoleWFA, ForumRoleRetake, ForumRoleDone, ForumRoleWIP:
		return true
	}
	return false
}

// ProductionForumConfig is the per-Production switch between the lane and the
// forum notification paths. A Production without a row runs in lane mode.
type ProductionForumConfig struct {
	ID           uint   `gorm:"primaryKey"`
	ProductionID string `gorm:"uniqueIndex;not null"`
	Mode         string `gorm:"not null;default:lane"`
	GuildID      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ProductionForumRoute maps one Kitsu Task Type to the Discord forum channel
// its posts are created in. IDs are the identity; names are display metadata.
type ProductionForumRoute struct {
	ID               uint   `gorm:"primaryKey"`
	ProductionID     string `gorm:"not null;uniqueIndex:idx_forum_route_task_type"`
	TaskTypeID       string `gorm:"not null;uniqueIndex:idx_forum_route_task_type"`
	TaskTypeName     string
	ForumChannelID   string `gorm:"not null;index"`
	ForumChannelName string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ProductionForumStatusRole maps a Kitsu task status ID to the role it plays.
type ProductionForumStatusRole struct {
	ID           uint   `gorm:"primaryKey"`
	ProductionID string `gorm:"not null;uniqueIndex:idx_forum_status_role"`
	StatusID     string `gorm:"not null;uniqueIndex:idx_forum_status_role"`
	StatusName   string
	Role         string `gorm:"not null"`
	CreatedAt    time.Time
}

// ProductionForumTag maps a role to a forum's tag ID. Tag IDs differ per forum,
// so the key includes the forum channel.
type ProductionForumTag struct {
	ID             uint   `gorm:"primaryKey"`
	ProductionID   string `gorm:"not null;uniqueIndex:idx_forum_tag"`
	ForumChannelID string `gorm:"not null;uniqueIndex:idx_forum_tag"`
	Role           string `gorm:"not null;uniqueIndex:idx_forum_tag"`
	TagID          string `gorm:"not null"`
	CreatedAt      time.Time
}

// ProductionForumSettings is one Production's complete forum configuration.
type ProductionForumSettings struct {
	Config      ProductionForumConfig
	Routes      []ProductionForumRoute
	StatusRoles []ProductionForumStatusRole
	Tags        []ProductionForumTag
}

// NotificationModeForProduction returns the Production's notification mode.
// Anything other than an explicit forum row is lane, so an unconfigured or
// unreadable Production never switches paths by accident.
func NotificationModeForProduction(db *gorm.DB, productionID string) string {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return NotificationModeLane
	}
	var config ProductionForumConfig
	if err := db.Where("production_id = ?", strings.TrimSpace(productionID)).First(&config).Error; err != nil {
		return NotificationModeLane
	}
	if config.Mode == NotificationModeForum {
		return NotificationModeForum
	}
	return NotificationModeLane
}

// LoadProductionForumSettings reads a Production's forum configuration. ok is
// false when the Production has no forum configuration row.
func LoadProductionForumSettings(db *gorm.DB, productionID string) (settings ProductionForumSettings, ok bool, err error) {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return settings, false, gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	if err := db.Where("production_id = ?", productionID).First(&settings.Config).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return settings, false, nil
		}
		return settings, false, err
	}
	if err := db.Where("production_id = ?", productionID).Order("id asc").Find(&settings.Routes).Error; err != nil {
		return settings, true, err
	}
	if err := db.Where("production_id = ?", productionID).Order("id asc").Find(&settings.StatusRoles).Error; err != nil {
		return settings, true, err
	}
	if err := db.Where("production_id = ?", productionID).Order("id asc").Find(&settings.Tags).Error; err != nil {
		return settings, true, err
	}
	return settings, true, nil
}

// ForumSettingsProblem is one validation failure as a code plus arguments, so
// the admin page can word it in the viewer's language.
type ForumSettingsProblem struct {
	Code string
	Args []string
}

// English renders the problem as a plain English sentence (logs, tests, API).
func (p ForumSettingsProblem) English() string {
	arg := func(i int) string {
		if i < len(p.Args) {
			return p.Args[i]
		}
		return ""
	}
	switch p.Code {
	case ProblemUnknownMode:
		return fmt.Sprintf("unknown notification mode %q", arg(0))
	case ProblemProductionRequired:
		return "production ID is required"
	case ProblemRouteIncomplete:
		return "every forum route needs a Task Type ID and a forum channel ID"
	case ProblemTaskTypeDuplicate:
		return fmt.Sprintf("Task Type %s is routed more than once", arg(0))
	case ProblemStatusNoRole:
		return fmt.Sprintf("status %q has no valid role", arg(0))
	case ProblemStatusDuplicate:
		return fmt.Sprintf("status %s is mapped more than once", arg(0))
	case ProblemTagUnroutedForum:
		return fmt.Sprintf("tag for forum %s, which no Task Type routes to", arg(0))
	case ProblemTagInvalid:
		return fmt.Sprintf("forum %s has an invalid tag mapping", arg(0))
	case ProblemTagRoleDuplicate:
		return fmt.Sprintf("forum %s maps role %s more than once", arg(0), arg(1))
	case ProblemTagReused:
		return fmt.Sprintf("forum %s uses tag %s for more than one role", arg(0), arg(1))
	case ProblemGuildRequired:
		return "forum mode needs a Discord server (guild) ID"
	case ProblemRoutesRequired:
		return "forum mode needs at least one Task Type routed to a forum"
	case ProblemRoleStatusMissing:
		return fmt.Sprintf("forum mode needs a Kitsu status mapped to role %s", arg(0))
	case ProblemForumTagMissing:
		return fmt.Sprintf("forum %s has no tag for role %s", arg(0), arg(1))
	}
	return p.Code
}

// Problem codes of ForumSettingsProblem.
const (
	ProblemUnknownMode        = "unknown_mode"
	ProblemProductionRequired = "production_required"
	ProblemRouteIncomplete    = "route_incomplete"
	ProblemTaskTypeDuplicate  = "task_type_duplicate"
	ProblemStatusNoRole       = "status_no_role"
	ProblemStatusDuplicate    = "status_duplicate"
	ProblemTagUnroutedForum   = "tag_unrouted_forum"
	ProblemTagInvalid         = "tag_invalid"
	ProblemTagRoleDuplicate   = "tag_role_duplicate"
	ProblemTagReused          = "tag_reused"
	ProblemGuildRequired      = "guild_required"
	ProblemRoutesRequired     = "routes_required"
	ProblemRoleStatusMissing  = "role_status_missing"
	ProblemForumTagMissing    = "forum_tag_missing"
)

// ForumSettingsError carries the structured problems of a rejected save.
type ForumSettingsError struct {
	Problems []ForumSettingsProblem
}

func (e *ForumSettingsError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		parts = append(parts, problem.English())
	}
	return "invalid forum settings: " + strings.Join(parts, "; ")
}

// ValidateProductionForumSettings is ForumSettingsProblems rendered in English.
func ValidateProductionForumSettings(settings ProductionForumSettings) []string {
	var messages []string
	for _, problem := range ForumSettingsProblems(settings) {
		messages = append(messages, problem.English())
	}
	return messages
}

// ForumSettingsProblems checks the configuration without touching Discord or
// Kitsu (live ID checks happen at startup validation). A forum-mode Production
// needs a complete configuration; a lane-mode one may be saved half-finished.
func ForumSettingsProblems(settings ProductionForumSettings) []ForumSettingsProblem {
	var problems []ForumSettingsProblem
	add := func(code string, args ...string) {
		problems = append(problems, ForumSettingsProblem{Code: code, Args: args})
	}
	mode := settings.Config.Mode
	if mode != NotificationModeLane && mode != NotificationModeForum {
		add(ProblemUnknownMode, mode)
	}
	if strings.TrimSpace(settings.Config.ProductionID) == "" {
		add(ProblemProductionRequired)
	}

	forums := map[string]struct{}{}
	taskTypes := map[string]struct{}{}
	for _, route := range settings.Routes {
		taskTypeID := strings.TrimSpace(route.TaskTypeID)
		forumID := strings.TrimSpace(route.ForumChannelID)
		if taskTypeID == "" || forumID == "" {
			add(ProblemRouteIncomplete)
			continue
		}
		if _, dup := taskTypes[taskTypeID]; dup {
			add(ProblemTaskTypeDuplicate, taskTypeID)
		}
		taskTypes[taskTypeID] = struct{}{}
		forums[forumID] = struct{}{}
	}

	statusSeen := map[string]struct{}{}
	rolesInUse := map[string]struct{}{}
	for _, row := range settings.StatusRoles {
		statusID := strings.TrimSpace(row.StatusID)
		if statusID == "" || !validForumRole(row.Role) {
			add(ProblemStatusNoRole, row.StatusID)
			continue
		}
		if _, dup := statusSeen[statusID]; dup {
			add(ProblemStatusDuplicate, statusID)
		}
		statusSeen[statusID] = struct{}{}
		rolesInUse[row.Role] = struct{}{}
	}

	tagsByForum := map[string]map[string]string{}
	tagIDs := map[string]map[string]struct{}{}
	for _, tag := range settings.Tags {
		forumID := strings.TrimSpace(tag.ForumChannelID)
		tagID := strings.TrimSpace(tag.TagID)
		if _, known := forums[forumID]; !known {
			add(ProblemTagUnroutedForum, forumID)
			continue
		}
		if !validForumRole(tag.Role) || tagID == "" {
			add(ProblemTagInvalid, forumID)
			continue
		}
		if tagsByForum[forumID] == nil {
			tagsByForum[forumID] = map[string]string{}
			tagIDs[forumID] = map[string]struct{}{}
		}
		if _, dup := tagsByForum[forumID][tag.Role]; dup {
			add(ProblemTagRoleDuplicate, forumID, tag.Role)
		}
		if _, dup := tagIDs[forumID][tagID]; dup {
			add(ProblemTagReused, forumID, tagID)
		}
		tagsByForum[forumID][tag.Role] = tagID
		tagIDs[forumID][tagID] = struct{}{}
	}

	if mode == NotificationModeForum {
		if strings.TrimSpace(settings.Config.GuildID) == "" {
			add(ProblemGuildRequired)
		}
		if len(settings.Routes) == 0 {
			add(ProblemRoutesRequired)
		}
		// Every trigger role must have a status, and a tag in every forum, or a
		// post could be created without a state tag.
		for _, role := range []string{ForumRoleWFA, ForumRoleRetake, ForumRoleDone} {
			if _, ok := rolesInUse[role]; !ok {
				add(ProblemRoleStatusMissing, role)
			}
			for _, forumID := range sortedKeys(forums) {
				if _, ok := tagsByForum[forumID][role]; !ok {
					add(ProblemForumTagMissing, forumID, role)
				}
			}
		}
	}
	return problems
}

// sortedKeys keeps problem lists in a stable order (map iteration is random).
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SaveProductionForumSettings validates and replaces a Production's whole forum
// configuration in one transaction. It only writes the forum tables; lane
// routing state is untouched.
func SaveProductionForumSettings(db *gorm.DB, settings ProductionForumSettings) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	settings.Config.ProductionID = strings.TrimSpace(settings.Config.ProductionID)
	if problems := ForumSettingsProblems(settings); len(problems) > 0 {
		return &ForumSettingsError{Problems: problems}
	}
	productionID := settings.Config.ProductionID
	return db.Transaction(func(tx *gorm.DB) error {
		var existing ProductionForumConfig
		if err := tx.Where("production_id = ?", productionID).First(&existing).Error; err == nil {
			settings.Config.ID = existing.ID
			settings.Config.CreatedAt = existing.CreatedAt
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		settings.Config.ID = existing.ID
		if err := tx.Save(&settings.Config).Error; err != nil {
			return err
		}
		if err := deleteProductionForumChildren(tx, productionID); err != nil {
			return err
		}
		for i := range settings.Routes {
			settings.Routes[i].ID = 0
			settings.Routes[i].ProductionID = productionID
			if err := tx.Create(&settings.Routes[i]).Error; err != nil {
				return err
			}
		}
		for i := range settings.StatusRoles {
			settings.StatusRoles[i].ID = 0
			settings.StatusRoles[i].ProductionID = productionID
			if err := tx.Create(&settings.StatusRoles[i]).Error; err != nil {
				return err
			}
		}
		for i := range settings.Tags {
			settings.Tags[i].ID = 0
			settings.Tags[i].ProductionID = productionID
			if err := tx.Create(&settings.Tags[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func deleteProductionForumChildren(tx *gorm.DB, productionID string) error {
	for _, table := range []any{&ProductionForumRoute{}, &ProductionForumStatusRole{}, &ProductionForumTag{}} {
		if err := tx.Where("production_id = ?", productionID).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}

// DeleteProductionForumSettings removes a Production's forum configuration,
// including its mode row, so it falls back to lane mode.
func DeleteProductionForumSettings(db *gorm.DB, productionID string) error {
	if db == nil || strings.TrimSpace(productionID) == "" {
		return gorm.ErrInvalidData
	}
	productionID = strings.TrimSpace(productionID)
	return db.Transaction(func(tx *gorm.DB) error {
		if err := deleteProductionForumChildren(tx, productionID); err != nil {
			return err
		}
		return tx.Where("production_id = ?", productionID).Delete(&ProductionForumConfig{}).Error
	})
}
