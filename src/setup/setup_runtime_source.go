package setup

import (
	"app/src/api/kitsu"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// setupKitsuProjects uses the same persisted/runtime credential source as
// other live Kitsu reads. This prevents Setup from silently falling back to
// the legacy process environment when a saved Bot token is authoritative.
func setupKitsuProjects(db *gorm.DB) ([]KitsuProject, error) {
	baseURL, token, ok := runtimeKitsuDataSource(db)
	if !ok {
		return nil, errors.New("Kitsu runtime data source is unavailable")
	}
	return ListKitsuProjectsWithCredentials(baseURL, token)
}

func setupKitsuTaskTypes(db *gorm.DB, productionID string) []kitsu.TaskType {
	baseURL, token, ok := runtimeKitsuDataSource(db)
	if !ok || strings.TrimSpace(productionID) == "" {
		return nil
	}
	return filterActiveTaskTypes(kitsu.GetProjectTaskTypesWithCredentials(baseURL, token, strings.TrimSpace(productionID)).Each)
}
