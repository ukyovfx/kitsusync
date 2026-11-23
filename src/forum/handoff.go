package forum

import (
	"strings"

	"app/src/api/kitsu"
)

// assigneeIDs mirrors the planner: bot accounts are not recipients.
func assigneeIDs(payload kitsu.MessagePayload) []string {
	ids := make([]string, 0, len(payload.Assignees))
	for _, person := range payload.Assignees {
		if person.IsBot || strings.TrimSpace(person.ID) == "" {
			continue
		}
		ids = append(ids, strings.TrimSpace(person.ID))
	}
	return ids
}

func containsID(ids []string, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, candidate := range ids {
		if strings.TrimSpace(candidate) == id {
			return true
		}
	}
	return false
}
