package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"app/src/utils/request"

	"gorm.io/gorm"
)

func configureMainTestOrigin(t *testing.T, raw string) {
	t.Helper()
	if err := request.ConfigureVerifiedOrigin(request.VerifiedOrigin{BaseURL: raw, PinnedIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}); err != nil {
		t.Fatal(err)
	}
}

func TestMakeKitsuResponseContinuesWhenCommentsListFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("kitsu mock: %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/data/tasks":
			_, _ = w.Write([]byte(`[{"id":"task-1","updated_at":"2026-08-07T00:00:00","entity_id":"entity-1","task_type_id":"tt1","task_status_id":"ts1","project_id":"p1"}]`))
		case "/api/data/task-status/":
			_, _ = w.Write([]byte(`[{"id":"ts1","short_name":"WFA"}]`))
		case "/api/data/entities/":
			_, _ = w.Write([]byte(`[{"id":"entity-1","entity_type_id":"et1","project_id":"p1"}]`))
		case "/api/data/entity-types/":
			_, _ = w.Write([]byte(`[{"id":"et1"}]`))
		case "/api/data/projects/":
			_, _ = w.Write([]byte(`[{"id":"p1","name":"Production"}]`))
		case "/api/data/task-types/":
			_, _ = w.Write([]byte(`[{"id":"tt1","name":"Animation"}]`))
		case "/api/data/persons/":
			_, _ = w.Write([]byte(`[]`))
		case "/api/data/comments":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("KITSU_API_BASE_URL", server.URL+"/api/")
	t.Setenv("KitsuJWTToken", "test-token")
	configureMainTestOrigin(t, server.URL)

	var conf config.Config
	conf.Threads = 1
	conf.Notification.NotifyOnAssign = true

	payloads, err := MakeKitsuResponse(conf)
	if err != nil {
		t.Fatalf("comments LIST 403 must not fail the whole poll, got error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}
	if !payloads[0].CommentsUnavailable {
		t.Fatal("payload must be flagged CommentsUnavailable when comments LIST fails")
	}
	if payloads[0].Task.ID != "task-1" || payloads[0].TaskStatus.ShortName != "WFA" {
		t.Fatalf("task data must survive a comments fetch failure: %+v", payloads[0])
	}
}

func TestFilterTasksSkipsCommentDiffWhenCommentsUnavailable(t *testing.T) {
	db := newNotificationPipelineDB(t)
	if err := model.MarkTaskObserved(db, "task-1", "2026-08-07T00:00:00", "WFA", "c1", "2026-08-01T00:00:00"); err != nil {
		t.Fatal(err)
	}
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	dispatched := 0
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		dispatched += len(data)
		for _, payload := range data {
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, "", "", "discord-message", "", "")
		}
		return data
	}

	// Comments could not be fetched, so the payload arrives with empty comment
	// data even though the delivered snapshot already has comment c1.
	payload := pipelinePayload("WFA")
	payload.CommentsUnavailable = true
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), db)

	if dispatched != 0 {
		t.Fatalf("comment-only change must not dispatch while comments are unavailable, got %d dispatches", dispatched)
	}
	row := model.FindTask(db, "task-1")
	if row.DeliveredCommentID != "c1" || row.DeliveredCommentUpdatedAt != "2026-08-01T00:00:00" {
		t.Fatalf("delivered comment checkpoint must be preserved, got %q / %q", row.DeliveredCommentID, row.DeliveredCommentUpdatedAt)
	}
	if row.CommentID != "c1" || row.CommentUpdatedAt != "2026-08-01T00:00:00" {
		t.Fatalf("observed comment checkpoint must be preserved, got %q / %q", row.CommentID, row.CommentUpdatedAt)
	}
}

func TestFilterTasksDispatchesCommentOnlyWhenCommentsAvailable(t *testing.T) {
	withImmediateAggregation(t)
	db := newNotificationPipelineDB(t)
	if err := model.MarkTaskObserved(db, "task-1", "2026-08-07T00:00:00", "WFA", "c1", "2026-08-01T00:00:00"); err != nil {
		t.Fatal(err)
	}
	configurePipelineRoute(t, db)
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	dispatched := 0
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		dispatched += len(data)
		for _, payload := range data {
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, "", "", "discord-message", "", "")
		}
		return data
	}

	// Same fixture without the outage flag: the (empty) latest comment still
	// differs from the stored checkpoint so a comment-only event dispatches.
	payload := pipelinePayload("WFA")
	FilterTasks([]kitsu.MessagePayload{payload}, pipelineConfig(), db)

	if dispatched != 1 {
		t.Fatalf("expected comment-only event to dispatch when comments are readable, got %d", dispatched)
	}
}
