package setup

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/model"
)

var errCheckpointUnavailable = errors.New("kitsu unavailable")

// A production connected while tasks already wait for review must not notify
// for that pre-existing work; only later changes notify.
func TestFirstTimeConnectionCheckpointsExistingTasksSilently(t *testing.T) {
	db := newIAViewDB(t)
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	previousOps := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) { return "channel-" + name, nil })
	defer func() { firstTimeOps = previousOps }()
	statusesFetched := make(chan struct{}, 1)
	previousFetch := firstTimeFetchTasks
	firstTimeFetchTasks = func() (kitsu.Tasks, error) {
		return kitsu.Tasks{Each: []kitsu.Task{
			{ID: "waiting", ProjectID: "p1", UpdatedAt: "2026-01-01T00:00:00", TaskStatusID: "s-wfa"},
			{ID: "other-production", ProjectID: "p2", UpdatedAt: "2026-01-01T00:00:00", TaskStatusID: "s-wfa"},
		}}, nil
	}
	defer func() { firstTimeFetchTasks = previousFetch }()
	previousStatuses := firstTimeFetchTaskStatuses
	firstTimeFetchTaskStatuses = func() (kitsu.TaskStatuses, error) {
		defer func() { statusesFetched <- struct{}{} }()
		return kitsu.TaskStatuses{Each: []kitsu.TaskStatus{{ID: "s-wfa", ShortName: "wfa"}, {ID: "s-todo", ShortName: "todo"}}}, nil
	}
	defer func() { firstTimeFetchTaskStatuses = previousStatuses }()

	w := httptest.NewRecorder()
	if !handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", firstTimeTestPlan()), "en", "https://kitsu.invalid/", "bot-token", db) {
		t.Fatal("first-time execution was not handled")
	}
	<-statusesFetched
	// The checkpoint runs after the response; give the write a moment to land.
	deadline := time.Now().Add(2 * time.Second)
	var checkpoint model.Task
	for time.Now().Before(deadline) {
		checkpoint, _ = model.FindTaskWithError(db, "waiting")
		if checkpoint.TaskID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if checkpoint.TaskID == "" || checkpoint.DeliveredTaskStatus != "wfa" || checkpoint.DeliveredTaskUpdatedAt != "2026-01-01T00:00:00" {
		t.Fatalf("pre-existing task was not silently checkpointed: %#v", checkpoint)
	}
	if other, _ := model.FindTaskWithError(db, "other-production"); other.TaskID != "" {
		t.Fatal("checkpoint touched a task outside the connected production")
	}

	// The first poll after connecting sees no change, so nothing is notified.
	if previous := model.PreviousTaskStatus(checkpoint, "wfa"); previous != "" {
		t.Fatalf("checkpoint did not suppress the initial notification: previous=%q", previous)
	}
	// A later change to wfa still reports the transition.
	if previous := model.PreviousTaskStatus(checkpoint, "todo"); previous != "wfa" {
		t.Fatalf("post-connection change lost its previous status: %q", previous)
	}
}

func TestFirstTimeConnectionSurvivesCheckpointFetchFailure(t *testing.T) {
	db := newIAViewDB(t)
	previousOps := firstTimeOps
	firstTimeOps = firstTimeTestOps(func(name string) (string, error) { return "channel-" + name, nil })
	defer func() { firstTimeOps = previousOps }()
	previousFetch := firstTimeFetchTasks
	firstTimeFetchTasks = func() (kitsu.Tasks, error) { return kitsu.Tasks{}, errCheckpointUnavailable }
	defer func() { firstTimeFetchTasks = previousFetch }()

	w := httptest.NewRecorder()
	if !handleFirstTimeConnectionAction(w, firstTimeTestRequest("execute_production_connection", firstTimeTestPlan()), "en", "https://kitsu.invalid/", "bot-token", db) {
		t.Fatal("first-time execution was not handled")
	}
	if model.FindProjectByKitsuID(db, "p1") == nil {
		t.Fatal("checkpoint failure blocked the connection")
	}
}
