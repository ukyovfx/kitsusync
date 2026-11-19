package setup

import (
	"app/src/api/kitsu"
)

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

