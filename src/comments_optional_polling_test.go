package main

import (
	"app/src/api/kitsu"
	"app/src/model"
	"app/src/utils/config"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
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

// forumBKitsuMock serves the metadata endpoints plus per-project tasks and
// per-task comments, counting the comment reads.
func forumBKitsuMock(t *testing.T, tasksByProject map[string]string, commentStatus func(taskID string) (int, string), commentReads *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == "/api/data/task-status/":
			_, _ = w.Write([]byte(`[{"id":"ts1","short_name":"WFA"}]`))
		case path == "/api/data/entities/":
			_, _ = w.Write([]byte(`[{"id":"entity-1","entity_type_id":"et1","project_id":"p1"}]`))
		case path == "/api/data/entity-types/":
			_, _ = w.Write([]byte(`[{"id":"et1"}]`))
		case path == "/api/data/projects/":
			ids := make([]string, 0, len(tasksByProject))
			for id := range tasksByProject {
				ids = append(ids, `{"id":"`+id+`","name":"Production"}`)
			}
			_, _ = w.Write([]byte("[" + strings.Join(ids, ",") + "]"))
		case path == "/api/data/task-types/":
			_, _ = w.Write([]byte(`[{"id":"tt1","name":"Animation"}]`))
		case path == "/api/data/persons/":
			_, _ = w.Write([]byte(`[{"id":"person-1","full_name":"Artist A"}]`))
		case strings.HasPrefix(path, "/api/data/projects/") && strings.HasSuffix(path, "/tasks"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/data/projects/"), "/tasks")
			body, ok := tasksByProject[id]
			if !ok || body == "" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
				return
			}
			_, _ = w.Write([]byte(body))
		case strings.HasPrefix(path, "/api/data/tasks/") && strings.HasSuffix(path, "/comments"):
			*commentReads++
			taskID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/data/tasks/"), "/comments")
			status, body := commentStatus(taskID)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Setenv("KITSU_API_BASE_URL", server.URL+"/api/")
	t.Setenv("KitsuJWTToken", "test-token")
	configureMainTestOrigin(t, server.URL)
	replaceTaskCommentCache(map[string]cachedTaskComments{})
	t.Cleanup(func() { replaceTaskCommentCache(map[string]cachedTaskComments{}) })
	return server
}

const forumBTaskWithComment = `[{"id":"task-1","updated_at":"2026-08-07T00:00:00","entity_id":"entity-1","task_type_id":"tt1","task_status_id":"ts1","project_id":"p1","assignees":["person-1"],"last_comment_date":"2026-08-07T00:00:00"}]`

func forumBConfig() config.Config {
	var conf config.Config
	conf.Threads = 1
	conf.Notification.NotifyOnAssign = true
	return conf
}

func TestMakeKitsuResponseContinuesWhenTaskCommentsFail(t *testing.T) {
	reads := 0
	forumBKitsuMock(t, map[string]string{"p1": forumBTaskWithComment}, func(string) (int, string) {
		return http.StatusForbidden, `{"message":"Forbidden"}`
	}, &reads)

	payloads, err := MakeKitsuResponse(forumBConfig())
	if err != nil {
		t.Fatalf("a task comments 403 must not fail the whole poll, got error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}
	if !payloads[0].CommentsUnavailable {
		t.Fatal("payload must be flagged CommentsUnavailable when its comments read fails")
	}
	if payloads[0].Task.ID != "task-1" || payloads[0].TaskStatus.ShortName != "WFA" || len(payloads[0].Assignees) != 1 {
		t.Fatalf("task data must survive a comments fetch failure: %+v", payloads[0])
	}
}

func TestMakeKitsuResponseReadsCommentsOnlyWhenLastCommentDateMoves(t *testing.T) {
	reads := 0
	forumBKitsuMock(t, map[string]string{"p1": forumBTaskWithComment}, func(string) (int, string) {
		return http.StatusOK, `[{"id":"c1","object_id":"task-1","person_id":"person-1","text":"hello @Artist A","created_at":"2026-08-07T00:00:00","updated_at":"2026-08-07T00:00:00","mentions":["person-1"]}]`
	}, &reads)

	first, err := MakeKitsuResponse(forumBConfig())
	if err != nil || len(first) != 1 {
		t.Fatalf("first poll: %v, %d payloads", err, len(first))
	}
	if first[0].CommentsUnavailable || first[0].LatestComment.Comment.ID != "c1" || first[0].LatestComment.Author.ID != "person-1" {
		t.Fatalf("first poll must carry the latest comment: %+v", first[0].LatestComment)
	}
	if got := first[0].LatestComment.Comment.Mentions; len(got) != 1 || got[0] != "person-1" {
		t.Fatalf("comment mentions must be decoded, got %v", got)
	}
	second, err := MakeKitsuResponse(forumBConfig())
	if err != nil || len(second) != 1 {
		t.Fatalf("second poll: %v, %d payloads", err, len(second))
	}
	if reads != 1 {
		t.Fatalf("unchanged last_comment_date must not re-read comments, got %d reads", reads)
	}
	if second[0].LatestComment.Comment.ID != "c1" {
		t.Fatalf("cached comment must still reach the payload: %+v", second[0].LatestComment)
	}
}

func TestMakeKitsuResponseSkipsOnlyTheFailedProject(t *testing.T) {
	reads := 0
	forumBKitsuMock(t, map[string]string{"p1": forumBTaskWithComment, "p2": ""}, func(string) (int, string) {
		return http.StatusOK, `[]`
	}, &reads)

	payloads, err := MakeKitsuResponse(forumBConfig())
	if err != nil {
		t.Fatalf("one failed project must not fail the poll: %v", err)
	}
	if len(payloads) != 1 || payloads[0].Task.ID != "task-1" {
		t.Fatalf("tasks of the healthy project must still arrive, got %d payloads", len(payloads))
	}
	if _, failed := lastPollFailedProjects["p2"]; !failed {
		t.Fatal("failed project must be recorded so the liveness monitor skips it")
	}
}

func TestMakeKitsuResponseFailsWhenEveryProjectFails(t *testing.T) {
	reads := 0
	forumBKitsuMock(t, map[string]string{"p1": ""}, func(string) (int, string) { return http.StatusOK, `[]` }, &reads)
	if _, err := MakeKitsuResponse(forumBConfig()); err == nil {
		t.Fatal("poll must fail when every project read fails")
	}
}

func TestFilterTasksSkipsCommentDiffWhenCommentsUnavailable(t *testing.T) {
	db := newNotificationPipelineDB(t)
	if err := model.MarkTaskObserved(db, "task-1", "2026-08-07T00:00:00", "WFA", "c1", "2026-08-01T00:00:00", "", ""); err != nil {
		t.Fatal(err)
	}
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	dispatched := 0
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		dispatched += len(data)
		for _, payload := range data {
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, "", "", "discord-message", "", "", "", "")
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
	if err := model.MarkTaskObserved(db, "task-1", "2026-08-07T00:00:00", "WFA", "c1", "2026-08-01T00:00:00", "", ""); err != nil {
		t.Fatal(err)
	}
	configurePipelineRoute(t, db)
	original := notificationDispatch
	t.Cleanup(func() { notificationDispatch = original })
	dispatched := 0
	notificationDispatch = func(data []kitsu.MessagePayload, _ config.Config, _ string, _ *gorm.DB, _ string, _ []string) []kitsu.MessagePayload {
		dispatched += len(data)
		for _, payload := range data {
			model.UpdateTaskWithDiscord(db, payload.Task.ID, payload.Task.UpdatedAt, payload.TaskStatus.ShortName, "", "", "discord-message", "", "", "", "")
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
