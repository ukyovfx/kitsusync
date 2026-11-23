package forum

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"

	"app/src/model"
)

// An en Production's signal cards render in English; the language comes
// from the Production row, not the admin UI.

func seedForumProjectLanguage(t *testing.T, db *gorm.DB, language string) {
	t.Helper()
	if err := db.AutoMigrate(&model.Project{}); err != nil {
		t.Fatalf("migrate projects: %v", err)
	}
	if err := model.CreateProject(db, "prod-1", "Production", "", "guild-1", "category-1", language); err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

func TestForumSignalFollowsProductionLanguageEndToEnd(t *testing.T) {
	db := newForumDB(t)
	// CreateProject wipes a Production's operational state, so the settings
	// must be seeded after the project row exists.
	seedForumProjectLanguage(t, db, "en")
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)

	payload, _ := json.Marshal(model.ForumPostMessagePayload{
		Intent:         model.ForumIntentStatusWFA,
		WindowOpenedAt: executor.Now(),
	})
	enqueue(t, db, model.ForumOutbox{
		TaskID:      "task-1",
		Kind:        model.ForumOutboxPostMessage,
		DedupeKey:   model.ForumPostMessageCoalesceKey(model.ForumIntentStatusWFA, "task-1") + ":1",
		CoalesceKey: model.ForumPostMessageCoalesceKey(model.ForumIntentStatusWFA, "task-1"),
		Payload:     string(payload),
		State:       model.ForumOutboxPending,
		NotBefore:   executor.Now(),
		CreatedAt:   executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	for _, request := range fake.snapshot() {
		if request.Method != http.MethodPost || !strings.HasSuffix(request.Path, "/messages") {
			continue
		}
		bodyEmbeds, _ := request.Body["embeds"].([]any)
		bodyEmbed, _ := bodyEmbeds[0].(map[string]any)
		description, _ := bodyEmbed["description"].(string)
		if !strings.Contains(description, "### 👀 WFA") || !strings.Contains(description, "Please review") || !strings.Contains(description, "Status: wfa") {
			t.Errorf("description = %q, want the English signal", description)
		}
		if strings.Contains(description, "チェックをお願いします") || strings.Contains(description, "ステータス：") {
			t.Errorf("description = %q, want no Japanese", description)
		}
		return
	}
	t.Fatalf("no message posted: %+v", fake.snapshot())
}

// The flap warning was abolished (owner ruling, 2026-11-24): a warn row
// queued before the change is finished without posting anything.
func TestForumWarningRowIsDroppedWithoutPosting(t *testing.T) {
	db := newForumDB(t)
	seedForumProjectLanguage(t, db, "en")
	seedForumSettings(t, db)
	seedForumTask(t, db, "thread-1", "s-wfa")
	executor, fake := newExecutor(t, db)

	payload, _ := json.Marshal(map[string]int{"transitions": 4, "span_seconds": 720})
	row := enqueue(t, db, model.ForumOutbox{
		TaskID:      "task-1",
		Kind:        model.ForumOutboxWarnMessage,
		DedupeKey:   "warn:task-1:1",
		CoalesceKey: "warn:task-1",
		Payload:     string(payload),
		State:       model.ForumOutboxPending,
		NotBefore:   executor.Now(),
		CreatedAt:   executor.Now(),
	})

	if _, err := executor.Drain(10); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if got := fake.snapshot(); len(got) != 0 {
		t.Fatalf("requests = %+v, want none for an abolished warning", got)
	}
	done, err := loadOutbox(db, row.ID)
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if done.State != model.ForumOutboxDone || done.Result != "flap_warning_abolished" {
		t.Errorf("row = %q/%q, want done/flap_warning_abolished", done.State, done.Result)
	}
}
