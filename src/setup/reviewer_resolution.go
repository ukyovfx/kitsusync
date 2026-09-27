package setup

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"app/src/api/kitsu"
	"app/src/model"
	"gorm.io/gorm"
)

var reviewerProductionResolutionReader = kitsu.GetProjectTaskTypeSupervisorResolutionWithCredentials
var reviewerGuildMembersReader = ListGuildMembers
var reviewerGuildRolesReader = ListGuildRoles

// ResolveProductionWFAReviewerTargets combines current automatic Supervisors
// with explicit targets, validating every Discord target against the live
// Production Team and linked Guild immediately before delivery.
func ResolveProductionWFAReviewerTargets(db *gorm.DB, kitsuBaseURL, kitsuToken, discordToken, kitsuProjectID, taskTypeID string) ([]model.ProjectReviewerTarget, error) {
	if db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var project model.Project
	if err := db.Where("kitsu_project_id = ?", strings.TrimSpace(kitsuProjectID)).First(&project).Error; err != nil {
		return nil, fmt.Errorf("load Reviewer Production: %w", err)
	}
	if !isDiscordSnowflake(project.DiscordGuildID) {
		return nil, errors.New("Reviewer Production has no valid linked Discord Guild")
	}
	if strings.TrimSpace(kitsuBaseURL) == "" || strings.TrimSpace(kitsuToken) == "" {
		return nil, errors.New("Reviewer Kitsu connection is unavailable")
	}
	resolution, err := reviewerProductionResolutionReader(kitsuBaseURL, kitsuToken, project.KitsuProjectID, taskTypeID)
	if err != nil {
		return nil, fmt.Errorf("resolve live Kitsu Reviewer team: %w", err)
	}
	if strings.TrimSpace(resolution.TaskType.ID) != strings.TrimSpace(taskTypeID) {
		return nil, errors.New("Kitsu returned incomplete Task Type Reviewer data")
	}
	if strings.TrimSpace(discordToken) == "" {
		return nil, errors.New("Reviewer Discord connection is unavailable")
	}
	members, err := reviewerGuildMembersReader(project.DiscordGuildID, discordToken)
	if err != nil {
		return nil, fmt.Errorf("read linked Discord Guild members: %w", err)
	}
	memberByID := make(map[string]DiscordGuildMember, len(members))
	for _, member := range members {
		id := strings.TrimSpace(member.User.ID)
		if isDiscordSnowflake(id) && !member.User.Bot {
			memberByID[id] = member
		}
	}
	var globalUsers []model.UserMap
	if err := db.Order("id asc").Find(&globalUsers).Error; err != nil {
		return nil, fmt.Errorf("read global Kitsu User Linking: %w", err)
	}
	linkedTeam := make(map[string]string)
	for _, person := range resolution.Team {
		if person.IsBot || strings.TrimSpace(person.ID) == "" {
			continue
		}
		user := globalUserForKitsuPerson(globalUsers, person)
		if user == nil {
			continue
		}
		discordID := strings.TrimSpace(user.DiscordID)
		if _, isMember := memberByID[discordID]; !isDiscordSnowflake(discordID) || !isMember {
			continue
		}
		linkedTeam[strings.TrimSpace(person.ID)] = discordID
	}

	var explicit []model.ProjectReviewerTarget
	if err := db.Where("project_id = ? AND task_type_id = ?", project.ID, strings.TrimSpace(taskTypeID)).
		Order("target_kind asc, discord_id asc, id asc").Find(&explicit).Error; err != nil {
		return nil, fmt.Errorf("read Production Reviewer overrides: %w", err)
	}
	roleTargetsExist := false
	for _, target := range explicit {
		if target.TargetKind == model.ReviewerTargetRole && isDiscordSnowflake(target.DiscordID) {
			roleTargetsExist = true
			break
		}
	}
	allowedRoles := map[string]struct{}{}
	if roleTargetsExist {
		roles, err := reviewerGuildRolesReader(project.DiscordGuildID, discordToken)
		if err != nil {
			return nil, fmt.Errorf("read linked Discord Guild roles: %w", err)
		}
		for _, role := range mentionableReviewerRoles(project.DiscordGuildID, roles) {
			allowedRoles[strings.TrimSpace(role.ID)] = struct{}{}
		}
	}

	resultByKey := make(map[string]model.ProjectReviewerTarget)
	add := func(kind, discordID string) {
		if !isDiscordSnowflake(discordID) || (kind != model.ReviewerTargetUser && kind != model.ReviewerTargetRole) {
			return
		}
		discordID = strings.TrimSpace(discordID)
		resultByKey[kind+":"+discordID] = model.ProjectReviewerTarget{
			ProjectID: project.ID, TaskTypeID: strings.TrimSpace(taskTypeID),
			TargetKind: kind, DiscordID: discordID,
		}
	}
	for _, person := range resolution.Supervisors {
		teamPerson, isMember := reviewerTeamPerson(resolution.Team, person.ID)
		if isMember && teamPerson.Active && !teamPerson.Archived && !teamPerson.IsBot && kitsu.EffectiveProductionRole(teamPerson) == "supervisor" && containsReviewerDepartment(person.Departments, resolution.TaskType.DepartmentID) {
			if discordID := linkedTeam[strings.TrimSpace(person.ID)]; discordID != "" {
				add(model.ReviewerTargetUser, discordID)
			}
		}
	}
	for _, target := range explicit {
		discordID := strings.TrimSpace(target.DiscordID)
		switch target.TargetKind {
		case model.ReviewerTargetUser:
			valid := false
			for _, linkedID := range linkedTeam {
				if linkedID == discordID {
					valid = true
					break
				}
			}
			if valid {
				add(model.ReviewerTargetUser, discordID)
			}
		case model.ReviewerTargetRole:
			if _, ok := allowedRoles[discordID]; ok && discordID != strings.TrimSpace(project.DiscordGuildID) {
				add(model.ReviewerTargetRole, discordID)
			}
		}
	}
	result := make([]model.ProjectReviewerTarget, 0, len(resultByKey))
	for _, target := range resultByKey {
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TargetKind != result[j].TargetKind {
			return result[i].TargetKind < result[j].TargetKind
		}
		return result[i].DiscordID < result[j].DiscordID
	})
	return result, nil
}

func reviewerTeamPerson(team []kitsu.Person, personID string) (kitsu.Person, bool) {
	personID = strings.TrimSpace(personID)
	for _, person := range team {
		if strings.TrimSpace(person.ID) == personID {
			return person, true
		}
	}
	return kitsu.Person{}, false
}

func containsReviewerDepartment(departments []string, departmentID string) bool {
	departmentID = strings.TrimSpace(departmentID)
	if departmentID == "" {
		return false
	}
	for _, id := range departments {
		if strings.TrimSpace(id) == departmentID {
			return true
		}
	}
	return false
}
