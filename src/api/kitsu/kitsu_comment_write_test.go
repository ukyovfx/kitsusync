package kitsu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A written comment goes to the task's comments endpoint with the bot
// bearer token, carrying the text and the task status it was written
// against.
func TestPostTaskCommentWithContextWritesTextAndStatus(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody commentWriteRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request body is not the comment payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"comment-new","text":"OK","task_status_id":"status-1"}`))
	}))
	defer server.Close()
	t.Setenv("KITSU_API_BASE_URL", server.URL+"/api/")
	t.Setenv("KITSU_HOSTNAME", "")
	t.Setenv("KitsuJWTToken", "write-token")
	configureTestOrigin(t, server.URL)

	comment, err := PostTaskCommentWithContext(context.Background(), "task-1", "OK — received.", "status-1")
	if err != nil {
		t.Fatalf("post comment: %v", err)
	}
	if comment.ID != "comment-new" {
		t.Fatalf("comment = %+v", comment)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/data/tasks/task-1/comments" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer write-token" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotBody.Text != "OK — received." || gotBody.TaskStatusID != "status-1" {
		t.Fatalf("body = %+v", gotBody)
	}
}

// GetTaskWithContext reads one task, including the status the reply write
// must carry.
func TestGetTaskWithContextReadsOneTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/data/tasks/task-9" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"task-9","task_status_id":"status-wfa"}`))
	}))
	defer server.Close()
	t.Setenv("KITSU_API_BASE_URL", server.URL+"/api/")
	t.Setenv("KITSU_HOSTNAME", "")
	t.Setenv("KitsuJWTToken", "read-token")
	configureTestOrigin(t, server.URL)

	task, err := GetTaskWithContext(context.Background(), "task-9")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.ID != "task-9" || task.TaskStatusID != "status-wfa" {
		t.Fatalf("task = %+v", task)
	}
	if !strings.HasPrefix(server.URL, "http://127.0.0.1") {
		t.Fatalf("test server URL = %s", server.URL)
	}
}
