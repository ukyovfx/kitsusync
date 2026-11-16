package main

import (
	"app/src/api/kitsu"
	"testing"
)

func TestStatusChangeCommentRequiresZouStatusAndLastCommentMatch(t *testing.T) {
	task := kitsu.Task{ID: "task-1", TaskStatusID: "status-done", LastCommentDate: "2026-09-01T10:00:00Z"}
	comments := []kitsu.Comment{
		{ObjectID: "task-1", TaskStatusID: "status-wfa", PersonID: "wrong-person", CreatedAt: task.LastCommentDate},
		{ObjectID: "task-1", TaskStatusID: "status-done", PersonID: "person-1", CreatedAt: task.LastCommentDate},
	}
	got, ok := statusChangeComment(task, comments)
	if !ok || got.PersonID != "person-1" {
		t.Fatalf("status change comment = %+v/%v, want person-1/true", got, ok)
	}
	comments[1].CreatedAt = "2026-09-01T10:01:00Z"
	if _, ok := statusChangeComment(task, comments); ok {
		t.Fatal("mismatched comment was treated as deterministic status actor")
	}
}
