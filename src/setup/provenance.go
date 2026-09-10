package setup

import (
	"app/src/model"
	"strings"

	"gorm.io/gorm"
)

type ReadModelProvenance struct {
	Productions   []ProductionProvenance   `json:"productions"`
	Users         []UserProvenance         `json:"users"`
	ExcludedUsers []ExcludedUserProvenance `json:"excluded_users,omitempty"`
}

type ProductionProvenance struct {
	DisplayName      string `json:"display_name"`
	ProductionID     string `json:"production_id"`
	Source           string `json:"source"`
	ConnectedLocally bool   `json:"connected_locally"`
}

type UserProvenance struct {
	DisplayName  string `json:"display_name"`
	Active       bool   `json:"active"`
	Archived     bool   `json:"archived"`
	IsBot        bool   `json:"is_bot"`
	Role         string `json:"role,omitempty"`
	Source       string `json:"source"`
	LocalMapping bool   `json:"local_mapping"`
}

type ExcludedUserProvenance struct {
	DisplayName string `json:"display_name"`
	Active      bool   `json:"active"`
	Archived    bool   `json:"archived"`
	IsBot       bool   `json:"is_bot"`
	Role        string `json:"role,omitempty"`
	Source      string `json:"source"`
	Reason      string `json:"reason"`
}

func shortenedProvenanceID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 10 {
		return value
	}
	return value[:6] + "..." + value[len(value)-4:]
}

func ReadModelProvenanceSnapshot(db *gorm.DB) ReadModelProvenance {
	snapshot := ReadModelProvenance{}
	for _, project := range availableProjects(db) {
		source := "local_sqlite"
		connected := true
		if project.ValidationOnly {
			source = "fixture"
			connected = false
		} else if project.ReadOnlyPreview {
			source = "live_kitsu_api"
			connected = false
		}
		snapshot.Productions = append(snapshot.Productions, ProductionProvenance{
			DisplayName:      project.Name,
			ProductionID:     shortenedProvenanceID(project.KitsuProjectID),
			Source:           source,
			ConnectedLocally: connected,
		})
	}
	people, source := globalUserLinkingPeople(db)
	localMaps := model.ListUserMap(db)
	for _, person := range people {
		mapped := false
		for _, user := range localMaps {
			if strings.TrimSpace(person.ID) != "" && strings.TrimSpace(person.ID) == strings.TrimSpace(user.KitsuID) {
				mapped = strings.TrimSpace(user.DiscordID) != ""
				break
			}
			if strings.TrimSpace(person.Email) != "" && strings.EqualFold(strings.TrimSpace(person.Email), strings.TrimSpace(user.KitsuEmail)) {
				mapped = strings.TrimSpace(user.DiscordID) != ""
				break
			}
			if strings.EqualFold(strings.TrimSpace(person.FullName), strings.TrimSpace(user.KitsuName)) {
				mapped = strings.TrimSpace(user.DiscordID) != ""
				break
			}
		}
		snapshot.Users = append(snapshot.Users, UserProvenance{
			DisplayName:  person.FullName,
			Active:       person.Active,
			Archived:     person.Archived,
			IsBot:        person.IsBot,
			Role:         person.Role,
			Source:       source,
			LocalMapping: mapped,
		})
	}
	if source == "live_kitsu_api" {
		for _, person := range ListKitsuPersons("") {
			if containsKitsuPerson(people, person.ID) {
				continue
			}
			reason := "inactive_or_archived"
			if person.IsBot {
				reason = "service_account"
			}
			snapshot.ExcludedUsers = append(snapshot.ExcludedUsers, ExcludedUserProvenance{
				DisplayName: person.FullName,
				Active:      person.Active,
				Archived:    person.Archived,
				IsBot:       person.IsBot,
				Role:        person.Role,
				Source:      source,
				Reason:      reason,
			})
		}
	}
	return snapshot
}

func containsKitsuPerson(people []KitsuPerson, id string) bool {
	for _, person := range people {
		if strings.TrimSpace(person.ID) == strings.TrimSpace(id) {
			return true
		}
	}
	return false
}
