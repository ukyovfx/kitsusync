// Package kitsu provides methods for Kitsu task management software
package kitsu

import (
	"app/src/utils/config"
	"app/src/utils/request"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
)

// kitsuBase は Kitsu ベース URL を返す。
// 環境変数 KITSU_HOSTNAME が設定されている場合はそちらを優先する。
// これにより管理画面（/bot/admin/bot）で設定した Hostname が反映される。
func kitsuBase() string {
	if h := os.Getenv("KITSU_API_BASE_URL"); h != "" {
		h = strings.TrimRight(h, "/")
		h = strings.TrimSuffix(h, "/api")
		return h + "/"
	}
	if h := os.Getenv("KITSU_HOSTNAME"); h != "" {
		return h
	}
	return config.Read().Kitsu.Hostname
}

type Task struct {
	Assignees       []string    `json:"assignees,omitempty"`
	ID              string      `json:"id,omitempty"`
	CreatedAt       string      `json:"created_at,omitempty"`
	UpdatedAt       string      `json:"updated_at,omitempty"`
	Name            string      `json:"name,omitempty"`
	LastCommentDate string      `json:"last_comment_date,omitempty"`
	Data            interface{} `json:"data,omitempty"`
	ProjectID       string      `json:"project_id,omitempty"`
	TaskTypeID      string      `json:"task_type_id,omitempty"`
	TaskStatusID    string      `json:"task_status_id,omitempty"`
	EntityID        string      `json:"entity_id,omitempty"`
	AssignerID      string      `json:"assigner_id,omitempty"`
	Type            string      `json:"type,omitempty"`
}
type Tasks struct {
	Each []Task
}

type Person struct {
	ID                        string   `json:"id,omitempty"`
	CreatedAt                 string   `json:"created_at,omitempty"`
	UpdatedAt                 string   `json:"updated_at,omitempty"`
	FirstName                 string   `json:"first_name,omitempty"`
	LastName                  string   `json:"last_name,omitempty"`
	Email                     string   `json:"email,omitempty"`
	Phone                     string   `json:"phone,omitempty"`
	Active                    bool     `json:"active,omitempty"`
	Archived                  bool     `json:"archived,omitempty"`
	IsBot                     bool     `json:"is_bot,omitempty"`
	LastPresence              string   `json:"last_presence,omitempty"`
	DesktopLogin              string   `json:"desktop_login,omitempty"`
	ShotgunID                 string   `json:"shotgun_id,omitempty"`
	Timezone                  string   `json:"timezone,omitempty"`
	Locale                    string   `json:"locale,omitempty"`
	Data                      string   `json:"data,omitempty"`
	Role                      string   `json:"role,omitempty"`
	Departments               []string `json:"departments,omitempty"`
	HasAvatar                 bool     `json:"has_avatar,omitempty"`
	NotificationsEnabled      bool     `json:"notifications_enabled,omitempty"`
	NotificationsSlackEnabled bool     `json:"notifications_slack_enabled,omitempty"`
	NotificationsSlackUserid  string   `json:"notifications_slack_userid,omitempty"`
	Type                      string   `json:"type,omitempty"`
	FullName                  string   `json:"full_name,omitempty"`
}

type Persons struct {
	Each []Person
}

type Entity struct {
	EntitiesOut     []interface{} `json:"entities_out,omitempty"`
	InstanceCasting []interface{} `json:"instance_casting,omitempty"`
	CreatedAt       string        `json:"created_at,omitempty"`
	UpdatedAt       string        `json:"updated_at,omitempty"`
	ID              string        `json:"id,omitempty"`
	Name            string        `json:"name,omitempty"`
	Code            interface{}   `json:"code,omitempty"`
	Description     interface{}   `json:"description,omitempty"`
	ShotgunID       interface{}   `json:"shotgun_id,omitempty"`
	Canceled        bool          `json:"canceled,omitempty"`
	NbFrames        interface{}   `json:"nb_frames,omitempty"`
	ProjectID       string        `json:"project_id,omitempty"`
	EntityTypeID    string        `json:"entity_type_id,omitempty"`
	ParentID        string        `json:"parent_id,omitempty"`
	SourceID        interface{}   `json:"source_id,omitempty"`
	PreviewFileID   interface{}   `json:"preview_file_id,omitempty"`
	Data            interface{}   `json:"data,omitempty"`
	EntitiesIn      []interface{} `json:"entities_in,omitempty"`
	Type            string        `json:"type,omitempty"`
}

type Entities struct {
	Each []Entity
}

type EntityType struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type EntityTypes struct {
	Each []EntityType
}

type TaskStatuses struct {
	Each []TaskStatus
}

type TaskStatus struct {
	ID              string      `json:"id,omitempty"`
	CreatedAt       string      `json:"created_at,omitempty"`
	UpdatedAt       string      `json:"updated_at,omitempty"`
	Name            string      `json:"name,omitempty"`
	ShortName       string      `json:"short_name,omitempty"`
	Color           string      `json:"color,omitempty"`
	IsDone          bool        `json:"is_done,omitempty"`
	IsArtistAllowed bool        `json:"is_artist_allowed,omitempty"`
	IsClientAllowed bool        `json:"is_client_allowed,omitempty"`
	IsRetake        bool        `json:"is_retake,omitempty"`
	ShotgunID       interface{} `json:"shotgun_id,omitempty"`
	IsReviewable    bool        `json:"is_reviewable,omitempty"`
	Type            string      `json:"type,omitempty"`
}

type Comment struct {
	ID           string      `json:"id,omitempty"`
	CreatedAt    string      `json:"created_at,omitempty"`
	UpdatedAt    string      `json:"updated_at,omitempty"`
	ShotgunID    interface{} `json:"shotgun_id,omitempty"`
	ObjectID     string      `json:"object_id,omitempty"`
	PersonID     string      `json:"person_id,omitempty"`
	TaskStatusID string      `json:"task_status_id,omitempty"`
	Text         string      `json:"text,omitempty"`
}

type Comments struct {
	Each []Comment
}

type TaskType struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	ShortName      string `json:"short_name,omitempty"`
	ForEntity      string `json:"for_entity,omitempty"`
	DepartmentID   string `json:"department_id,omitempty"`
	DepartmentName string `json:"department_name,omitempty"`
	Active         bool   `json:"active,omitempty"`
	Archived       bool   `json:"archived,omitempty"`
	IsArchived     bool   `json:"is_archived,omitempty"`
}

type TaskTypes struct {
	Each []TaskType
}

type Project struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	ProjectStatusID string `json:"project_status_id,omitempty"`
}

type Projects struct {
	Each []Project
}

type ProjectStatus struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type ProjectStatuses struct {
	Each []ProjectStatus
}

type MessagePayload struct {
	PreviousStatusName   string // we store task status from DB and consider it 'old/prevous'
	IsCommentOnly        bool   // true when only the comment changed (no status/timestamp change)
	IsAssignNotification bool   // true when task status is "none" (TODO) and notifyOnAssign is enabled
	Project              struct {
		Project
	}
	Entity struct {
		Entity
	}
	EntityType struct {
		EntityType
	}
	Parent struct {
		Entity
	}
	Task struct {
		Task
	}
	TaskType struct {
		TaskType
	}
	TaskStatus struct {
		TaskStatus
	}
	LatestComment struct {
		Comment struct {
			Comment
		}
		Author struct {
			Person
		}
	}
	StatusChangeAuthor struct {
		Person
	}
	Assignees []Person
}

func GetComments() Comments {
	response, _ := GetCommentsWithError()
	return response
}

func GetCommentsWithError() (Comments, error) {
	path := kitsuBase() + "api/data/comments"
	response := Comments{}
	_, err := request.DoWithError(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response, err
}

func GetComment(objectID string) Comments {
	path := kitsuBase() + "api/data/comments?object_id=" + objectID
	response := Comments{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

func GetTasks() Tasks {
	response, _ := GetTasksWithError()
	return response
}

func GetTasksWithError() (Tasks, error) {
	path := kitsuBase() + "api/data/tasks?relations=true"
	response := Tasks{}
	_, err := request.DoWithError(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response, err
}

func GetTask(taskID string) Task {
	path := kitsuBase() + "api/data/tasks/" + taskID
	response := Task{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetPerson(personID string) Person {
	path := kitsuBase() + "api/data/persons/" + personID
	response := Person{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetPersons() Persons {
	response, _ := GetPersonsWithError()
	return response
}

func GetPersonsWithError() (Persons, error) {
	return GetPersonsWithCredentials("", os.Getenv("KitsuJWTToken"))
}

// GetPersonsWithCredentials reads the global person directory using the
// caller's already-validated runtime endpoint and credential. This keeps
// persisted runtime credentials authoritative without copying them into the
// legacy KitsuJWTToken environment variable.
func GetPersonsWithCredentials(baseURL, token string) (Persons, error) {
	path := kitsuBaseFor(baseURL) + "api/data/persons/"
	response := Persons{}
	_, err := request.DoWithError(token, http.MethodGet, path, nil, &response.Each)

	return response, err
}

func GetEntities() Entities {
	response, _ := GetEntitiesWithError()
	return response
}

func GetEntitiesWithError() (Entities, error) {
	path := kitsuBase() + "api/data/entities/"
	response := Entities{}
	_, err := request.DoWithError(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response, err
}

func GetEntity(EntityID string) Entity {
	path := kitsuBase() + "api/data/entities/" + EntityID
	response := Entity{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetEntityTypes() EntityTypes {
	response, _ := GetEntityTypesWithError()
	return response
}

func GetEntityTypesWithError() (EntityTypes, error) {
	path := kitsuBase() + "api/data/entity-types/"
	response := EntityTypes{}
	_, err := request.DoWithError(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response, err
}

func GetEntityType(entityTypeID string) EntityType {
	path := kitsuBase() + "api/data/entity-types/" + entityTypeID
	response := EntityType{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetTaskStatuses() TaskStatuses {
	response, _ := GetTaskStatusesWithError()
	return response
}

func GetTaskStatusesWithError() (TaskStatuses, error) {
	path := kitsuBase() + "api/data/task-status/"
	response := TaskStatuses{}
	_, err := request.DoWithError(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response, err
}

func GetTaskStatus(taskStatusID string) TaskStatus {
	path := kitsuBase() + "api/data/task-status/" + taskStatusID
	response := TaskStatus{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetTaskType(taskID string) TaskType {
	path := kitsuBase() + "api/data/task-types/" + taskID
	response := TaskType{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetTaskTypes() TaskTypes {
	response, _ := GetTaskTypesWithError()
	return response
}

func GetTaskTypesWithError() (TaskTypes, error) {
	path := kitsuBase() + "api/data/task-types/"
	response := TaskTypes{}
	_, err := request.DoWithError(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response, err
}

// GetProjectTaskTypes returns only the Task Types associated with a selected
// Production. The project-scoped endpoint is authoritative for setup; the
// global Task Type list is not a safe substitute because it can contain
// records unrelated to the selected Production.
func GetProjectTaskTypes(projectID string) TaskTypes {
	return getProjectTaskTypesWithCredentials("", os.Getenv("KitsuJWTToken"), projectID)
}

// GetProjectTaskTypesWithCredentials reads task types from a selected
// Production using an explicit runtime endpoint and credential.
func GetProjectTaskTypesWithCredentials(baseURL, token, projectID string) TaskTypes {
	return getProjectTaskTypesWithCredentials(baseURL, token, projectID)
}

func getProjectTaskTypesWithCredentials(baseURL, token, projectID string) TaskTypes {
	response := TaskTypes{}
	if projectID == "" {
		return response
	}
	path := kitsuBaseFor(baseURL) + "api/data/projects/" + url.PathEscape(projectID) + "/task-types"
	request.Do(token, http.MethodGet, path, nil, &response.Each)
	return response
}

func GetProject(projectID string) Project {
	path := kitsuBase() + "api/data/projects/" + projectID
	response := Project{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

// GetProjectTeam returns the read-only Production team from Zou.  This is
// distinct from the global person directory: a Production participant is
// scoped to one Production and is the authoritative source for assignment
// candidates on the Production detail page.
func GetProjectTeam(projectID string) []Person {
	if projectID == "" {
		return nil
	}
	var response []Person
	path := kitsuBase() + "api/data/projects/" + url.PathEscape(projectID) + "/team"
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)
	return response
}

// GetProjectTeamWithCredentialsAndError reads one Production's team using the
// caller's validated runtime endpoint and credential.
func GetProjectTeamWithCredentialsAndError(baseURL, token, projectID string) ([]Person, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("Kitsu Production ID is required")
	}
	var response []Person
	path := kitsuBaseFor(baseURL) + "api/data/projects/" + url.PathEscape(projectID) + "/team"
	_, err := request.DoWithError(token, http.MethodGet, path, nil, &response)
	return response, err
}

func GetProjects() Projects {
	response, _ := GetProjectsWithError()
	return response
}

func GetProjectsWithError() (Projects, error) {
	return GetProjectsWithCredentials("", os.Getenv("KitsuJWTToken"))
}

// GetProjectsWithCredentials reads live Productions using the caller's
// already-validated runtime endpoint and credential.
func GetProjectsWithCredentials(baseURL, token string) (Projects, error) {
	path := kitsuBaseFor(baseURL) + "api/data/projects/"
	response := Projects{}
	_, err := request.DoWithError(token, http.MethodGet, path, nil, &response.Each)

	return response, err
}

func kitsuBaseFor(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return kitsuBase()
	}
	h := strings.TrimRight(strings.TrimSpace(raw), "/")
	h = strings.TrimSuffix(h, "/api")
	return h + "/"
}

func GetProjectStatus(projectStatusID string) ProjectStatus {
	path := kitsuBase() + "api/data/project-status/" + projectStatusID
	response := ProjectStatus{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

// GetProjectTaskTypesWithCredentialsAndError reads Task Types for one Production
// with the caller's validated runtime endpoint and credential, returning transport
// and response decoding failures to callers that require a complete resolution.
func GetProjectTaskTypesWithCredentialsAndError(baseURL, token, projectID string) (TaskTypes, error) {
	response := TaskTypes{}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return response, errors.New("Kitsu Production ID is required")
	}
	path := kitsuBaseFor(baseURL) + "api/data/projects/" + url.PathEscape(projectID) + "/task-types"
	_, err := request.DoWithError(token, http.MethodGet, path, nil, &response.Each)
	return response, err
}

// GetPersonWithCredentials reads a person's detail, including Department
// memberships, using the supplied validated runtime endpoint and credential.
func GetPersonWithCredentials(baseURL, token, personID string) (Person, error) {
	person := Person{}
	personID = strings.TrimSpace(personID)
	if personID == "" {
		return person, errors.New("Kitsu person ID is required")
	}
	path := kitsuBaseFor(baseURL) + "api/data/persons/" + url.PathEscape(personID) + "?relations=true"
	_, err := request.DoWithError(token, http.MethodGet, path, nil, &person)
	return person, err
}

// GetProjectTaskTypeSupervisorsWithCredentials resolves the Department
// Supervisors for an exact Task Type ID in one Production. Kitsu remains the
// source of truth; this function does not cache or persist the result.
func GetProjectTaskTypeSupervisorsWithCredentials(baseURL, token, projectID, taskTypeID string) ([]Person, error) {
	projectID = strings.TrimSpace(projectID)
	taskTypeID = strings.TrimSpace(taskTypeID)
	if projectID == "" || taskTypeID == "" {
		return nil, errors.New("Kitsu Production ID and Task Type ID are required")
	}

	taskTypes, err := GetProjectTaskTypesWithCredentialsAndError(baseURL, token, projectID)
	if err != nil {
		return nil, fmt.Errorf("read Kitsu Production Task Types: %w", err)
	}
	var selected *TaskType
	for i := range taskTypes.Each {
		if taskTypes.Each[i].ID != taskTypeID {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("Kitsu Task Type ID %q is ambiguous", taskTypeID)
		}
		selected = &taskTypes.Each[i]
	}
	if selected == nil {
		return nil, fmt.Errorf("Kitsu Task Type ID %q was not found in Production %q", taskTypeID, projectID)
	}
	departmentID := strings.TrimSpace(selected.DepartmentID)
	if departmentID == "" {
		return nil, fmt.Errorf("Kitsu Task Type ID %q has no Department ID", taskTypeID)
	}

	team, err := GetProjectTeamWithCredentialsAndError(baseURL, token, projectID)
	if err != nil {
		return nil, fmt.Errorf("read Kitsu Production team: %w", err)
	}
	teamIDs := make(map[string]struct{}, len(team))
	for _, person := range team {
		if id := strings.TrimSpace(person.ID); id != "" {
			teamIDs[id] = struct{}{}
		}
	}
	if len(teamIDs) == 0 {
		return []Person{}, nil
	}

	people, err := GetPersonsWithCredentials(baseURL, token)
	if err != nil {
		return nil, fmt.Errorf("read Kitsu people: %w", err)
	}
	candidateIDs := make(map[string]struct{})
	for _, person := range people.Each {
		if strings.TrimSpace(person.Role) != "supervisor" {
			continue
		}
		personID := strings.TrimSpace(person.ID)
		if personID == "" {
			return nil, errors.New("Kitsu Supervisor record has no person ID")
		}
		if _, isProductionMember := teamIDs[personID]; !isProductionMember {
			continue
		}
		candidateIDs[personID] = struct{}{}
	}
	orderedIDs := make([]string, 0, len(candidateIDs))
	for personID := range candidateIDs {
		orderedIDs = append(orderedIDs, personID)
	}
	sort.Strings(orderedIDs)

	result := make([]Person, 0, len(orderedIDs))
	for _, personID := range orderedIDs {
		person, err := GetPersonWithCredentials(baseURL, token, personID)
		if err != nil {
			return nil, fmt.Errorf("read Kitsu Supervisor person %q: %w", personID, err)
		}
		if strings.TrimSpace(person.ID) != personID {
			return nil, fmt.Errorf("Kitsu person detail ID did not match requested Supervisor %q", personID)
		}
		if strings.TrimSpace(person.Role) != "supervisor" {
			return nil, fmt.Errorf("Kitsu person %q is not a Supervisor", personID)
		}
		if !containsKitsuDepartment(person.Departments, departmentID) {
			continue
		}
		if strings.TrimSpace(person.FullName) == "" {
			person.FullName = strings.TrimSpace(person.FirstName + " " + person.LastName)
		}
		if strings.TrimSpace(person.FullName) == "" || strings.TrimSpace(person.Email) == "" {
			return nil, fmt.Errorf("Kitsu Supervisor person %q is missing name or email", personID)
		}
		result = append(result, person)
	}
	return result, nil
}

func containsKitsuDepartment(departments []string, departmentID string) bool {
	for _, id := range departments {
		if strings.TrimSpace(id) == departmentID {
			return true
		}
	}
	return false
}
