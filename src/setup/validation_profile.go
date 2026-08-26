package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"gorm.io/gorm"
)

const validationOnlyEnv = "KITSUSYNC_VALIDATION_ONLY"
const localProfileEnv = "KITSUSYNC_LOCAL_PROFILE"

// LocalDevelopmentKitsuHostname is intentionally opt-in. It is not an OSS
// production default; it only supplies the container-to-host endpoint when
// the explicit local development profile is enabled.
func LocalDevelopmentKitsuHostname() string {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(localProfileEnv)))
	if value != "1" && value != "true" && value != "yes" {
		return ""
	}
	if host := strings.TrimSpace(os.Getenv("KITSUSYNC_LOCAL_KITSU_HOST")); host != "" {
		return host
	}
	return "http://host.docker.internal:8080/"
}

func ValidationOnlyModeEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(validationOnlyEnv)))
	return value == "1" || value == "true" || value == "yes"
}

func FixtureModeEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("KITSUSYNC_FIXTURE_MODE")))
	return value == "1" || value == "true" || value == "yes"
}

func RejectValidationMutation(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ValidationOnlyModeEnabled() && r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "validation-only profile is read-only", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func SeedConfigIfFixture(db *gorm.DB, conf config.Config) {
	if FixtureModeEnabled() {
		SeedFromConfig(db, conf)
	}
}

// SeedValidationOnlyProfile imports only GET results into the isolated local
// database. It intentionally creates no Discord reference, route, webhook,
// channel mapping, or notification configuration.
func SeedValidationOnlyProfile(db *gorm.DB) (int, error) {
	if db == nil {
		return 0, errors.New("validation profile database is nil")
	}
	projects := kitsu.GetProjects().Each
	if len(projects) != 1 {
		return 0, fmt.Errorf("validation-only profile requires exactly one Kitsu Production, got %d", len(projects))
	}
	taskTypes := kitsu.GetTaskTypes().Each
	persons := kitsu.GetPersons().Each
	data := model.ValidationKitsuData{}
	for _, taskType := range taskTypes {
		if strings.TrimSpace(taskType.ID) == "" || strings.TrimSpace(taskType.Name) == "" {
			continue
		}
		data.TaskTypes = append(data.TaskTypes, model.ValidationTaskType{ID: strings.TrimSpace(taskType.ID), Name: strings.TrimSpace(taskType.Name)})
	}
	for _, person := range persons {
		name := strings.TrimSpace(person.FullName)
		if name == "" {
			name = strings.TrimSpace(strings.TrimSpace(person.FirstName) + " " + strings.TrimSpace(person.LastName))
		}
		if strings.TrimSpace(person.ID) == "" || name == "" {
			continue
		}
		data.Participants = append(data.Participants, model.ValidationPerson{ID: strings.TrimSpace(person.ID), FullName: name, Email: strings.TrimSpace(person.Email)})
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	projectData := projects[0]
	var existingProjects []model.Project
	if err := db.Find(&existingProjects).Error; err != nil {
		return 0, err
	}
	for _, existing := range existingProjects {
		if !existing.ValidationOnly && strings.TrimSpace(existing.KitsuProjectID) != strings.TrimSpace(projectData.ID) {
			return 0, errors.New("refusing to mix validation-only data with a normal Production")
		}
	}
	var project model.Project
	if err := db.Where("kitsu_project_id = ?", strings.TrimSpace(projectData.ID)).First(&project).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if project.ID != 0 && !project.ValidationOnly {
		return 0, errors.New("refusing to replace a normal Production with validation-only data")
	}
	project.KitsuProjectID = strings.TrimSpace(projectData.ID)
	project.Name = strings.TrimSpace(projectData.Name)
	project.ProjectType = "validation-only"
	project.DiscordGuildID = ""
	project.DiscordCategoryID = ""
	project.StorageURL = ""
	project.ValidationOnly = true
	project.ValidationDataJSON = string(encoded)
	if err := db.Save(&project).Error; err != nil {
		return 0, err
	}
	for _, person := range data.Participants {
		var user model.UserMap
		query := db.Where("kitsu_name = ?", person.FullName).First(&user)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			if err := db.Create(&model.UserMap{KitsuName: person.FullName, KitsuEmail: person.Email}).Error; err != nil {
				return 0, err
			}
		}
	}
	return 1, nil
}
