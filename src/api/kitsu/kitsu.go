// Package kitsu provides methods for Kitsu task management software
package kitsu

import (
	"app/src/utils/config"
	"app/src/utils/request"
	"net/http"
	"net/url"
	"os"
)

// kitsuBase は Kitsu ベース URL を返す。
// 環境変数 KITSU_HOSTNAME が設定されている場合はそちらを優先する。
// これにより管理画面（/bot/admin/bot）で設定した Hostname が反映される。
func kitsuBase() string {
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
	ID                        string `json:"id,omitempty"`
	CreatedAt                 string `json:"created_at,omitempty"`
	UpdatedAt                 string `json:"updated_at,omitempty"`
	FirstName                 string `json:"first_name,omitempty"`
	LastName                  string `json:"last_name,omitempty"`
	Email                     string `json:"email,omitempty"`
	Phone                     string `json:"phone,omitempty"`
	Active                    bool   `json:"active,omitempty"`
	Archived                  bool   `json:"archived,omitempty"`
	IsBot                     bool   `json:"is_bot,omitempty"`
	LastPresence              string `json:"last_presence,omitempty"`
	DesktopLogin              string `json:"desktop_login,omitempty"`
	ShotgunID                 string `json:"shotgun_id,omitempty"`
	Timezone                  string `json:"timezone,omitempty"`
	Locale                    string `json:"locale,omitempty"`
	Data                      string `json:"data,omitempty"`
	Role                      string `json:"role,omitempty"`
	HasAvatar                 bool   `json:"has_avatar,omitempty"`
	NotificationsEnabled      bool   `json:"notifications_enabled,omitempty"`
	NotificationsSlackEnabled bool   `json:"notifications_slack_enabled,omitempty"`
	NotificationsSlackUserid  string `json:"notifications_slack_userid,omitempty"`
	Type                      string `json:"type,omitempty"`
	FullName                  string `json:"full_name,omitempty"`
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
	ID        string      `json:"id,omitempty"`
	CreatedAt string      `json:"created_at,omitempty"`
	UpdatedAt string      `json:"updated_at,omitempty"`
	ShotgunID interface{} `json:"shotgun_id,omitempty"`
	ObjectID  string      `json:"object_id,omitempty"`
	PersonID  string      `json:"person_id,omitempty"`
	Text      string      `json:"text,omitempty"`
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
	Assignees []Person
}

func GetComments() Comments {
	path := kitsuBase() + "api/data/comments"
	response := Comments{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

func GetComment(objectID string) Comments {
	path := kitsuBase() + "api/data/comments?object_id=" + objectID
	response := Comments{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

func GetTasks() Tasks {
	path := kitsuBase() + "api/data/tasks?relations=true"
	response := Tasks{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
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
	path := kitsuBase() + "api/data/persons/"
	response := Persons{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

func GetEntities() Entities {
	path := kitsuBase() + "api/data/entities/"
	response := Entities{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

func GetEntity(EntityID string) Entity {
	path := kitsuBase() + "api/data/entities/" + EntityID
	response := Entity{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetEntityTypes() EntityTypes {
	path := kitsuBase() + "api/data/entity-types/"
	response := EntityTypes{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

func GetEntityType(entityTypeID string) EntityType {
	path := kitsuBase() + "api/data/entity-types/" + entityTypeID
	response := EntityType{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}

func GetTaskStatuses() TaskStatuses {
	path := kitsuBase() + "api/data/task-status/"
	response := TaskStatuses{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
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
	path := kitsuBase() + "api/data/task-types/"
	response := TaskTypes{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

// GetProjectTaskTypes returns only the Task Types associated with a selected
// Production. The project-scoped endpoint is authoritative for setup; the
// global Task Type list is not a safe substitute because it can contain
// records unrelated to the selected Production.
func GetProjectTaskTypes(projectID string) TaskTypes {
	response := TaskTypes{}
	if projectID == "" {
		return response
	}
	path := kitsuBase() + "api/data/projects/" + url.PathEscape(projectID) + "/task-types"
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)
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

func GetProjects() Projects {
	path := kitsuBase() + "api/data/projects/"
	response := Projects{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response.Each)

	return response
}

func GetProjectStatus(projectStatusID string) ProjectStatus {
	path := kitsuBase() + "api/data/project-status/" + projectStatusID
	response := ProjectStatus{}
	request.Do(os.Getenv("KitsuJWTToken"), http.MethodGet, path, nil, &response)

	return response
}
