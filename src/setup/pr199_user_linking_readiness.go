package setup

import (
	"app/src/api/kitsu"
	"app/src/model"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// availableProjectsWithError merges live Kitsu projects with persisted local
// connection rows while preserving the live lookup outcome for callers that
// need to distinguish a successful empty response from an API/auth/network
// failure. It never mutates persisted project state.
func availableProjectsWithError(db *gorm.DB) ([]model.Project, error) {
	local := model.ListProjects(db)
	baseURL, token, ok := runtimeKitsuDataSource(db)
	if !ok {
		return local, nil
	}

	live, err := ListKitsuProjectsWithCredentials(baseURL, token)
	if err != nil {
		return local, err
	}
	if len(live) == 0 {
		return local, nil
	}

	localByID := make(map[string]model.Project, len(local))
	for _, project := range local {
		localByID[strings.TrimSpace(project.KitsuProjectID)] = project
	}

	merged := make([]model.Project, 0, len(live)+len(local))
	for _, liveProject := range live {
		id := strings.TrimSpace(liveProject.ID)
		if id == "" {
			continue
		}
		if project, found := localByID[id]; found {
			merged = append(merged, project)
			delete(localByID, id)
			continue
		}

		preview := model.Project{
			KitsuProjectID: id,
			Name:           strings.TrimSpace(liveProject.Name),
			ProjectType:    "live",
			ReadOnlyPreview: true,
		}
		data := model.ValidationKitsuData{}
		for _, taskType := range kitsu.GetProjectTaskTypesWithCredentials(baseURL, token, id).Each {
			if taskType.Archived || taskType.IsArchived {
				continue
			}
			if strings.TrimSpace(taskType.ID) != "" && strings.TrimSpace(taskType.Name) != "" {
				data.TaskTypes = append(data.TaskTypes, model.ValidationTaskType{
					ID:   strings.TrimSpace(taskType.ID),
					Name: strings.TrimSpace(taskType.Name),
				})
			}
		}
		if encoded, marshalErr := json.Marshal(data); marshalErr == nil {
			preview.ValidationDataJSON = string(encoded)
		}
		merged = append(merged, preview)
	}

	for _, project := range localByID {
		merged = append(merged, project)
	}
	sort.Slice(merged, func(i, j int) bool {
		return strings.ToLower(merged[i].Name) < strings.ToLower(merged[j].Name)
	})
	return merged, nil
}

// canonicalDiscordGuildQuery accepts only Discord snowflake-shaped guild IDs
// from the query string. Invalid user-controlled values are treated as no
// selection and never reach Discord lookup/rendering paths.
func canonicalDiscordGuildQuery(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	guildID := strings.TrimSpace(r.URL.Query().Get("discord_guild_id"))
	if !discordIDRegexp.MatchString(guildID) {
		return ""
	}
	return guildID
}
