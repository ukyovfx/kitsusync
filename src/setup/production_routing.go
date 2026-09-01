package setup

import (
	"app/src/api/kitsu"
	"os"
	"strings"
)

func routingTaskTypesForProduction(productionID string) []kitsu.TaskType {
	if strings.TrimSpace(os.Getenv("KitsuJWTToken")) == "" || strings.TrimSpace(productionID) == "" {
		return nil
	}
	return filterActiveTaskTypes(kitsu.GetProjectTaskTypes(strings.TrimSpace(productionID)).Each)
}

func filterActiveTaskTypes(taskTypes []kitsu.TaskType) []kitsu.TaskType {
	active := make([]kitsu.TaskType, 0, len(taskTypes))
	for _, taskType := range taskTypes {
		if taskType.Archived || taskType.IsArchived {
			continue
		}
		active = append(active, taskType)
	}
	return active
}

func taskTypeName(taskTypeID string, taskTypes []kitsu.TaskType) string {
	for _, taskType := range taskTypes {
		if taskType.ID == taskTypeID {
			return strings.TrimSpace(taskType.Name)
		}
	}
	return ""
}

func valueAt(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}
