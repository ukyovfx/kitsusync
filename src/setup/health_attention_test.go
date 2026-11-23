package setup

import (
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"app/src/model"
)

// The liveness threshold is ten poll intervals, floored at ten minutes.
func TestLivenessThreshold(t *testing.T) {
	if got := livenessThreshold(10 * time.Second); got != 10*time.Minute {
		t.Fatalf("10s interval: threshold = %v, want 10m", got)
	}
	if got := livenessThreshold(30 * time.Second); got != 10*time.Minute {
		t.Fatalf("30s interval: threshold = %v, want 10m", got)
	}
	if got := livenessThreshold(2 * time.Minute); got != 20*time.Minute {
		t.Fatalf("2m interval: threshold = %v, want 20m", got)
	}
}

func TestPollStale(t *testing.T) {
	now := time.Now()
	if !pollStale(now.Add(-11*time.Minute), now, 10*time.Second) {
		t.Fatal("11 minutes of silence at a 10s interval must be stale")
	}
	if pollStale(now.Add(-5*time.Minute), now, 10*time.Second) {
		t.Fatal("5 minutes of silence must not be stale yet")
	}
	if pollStale(time.Time{}, now, 10*time.Second) {
		t.Fatal("a poll that never ran is not stale")
	}
}

// A stale poll marks the liveness section as needing review; a fresh one
// does not.
func TestRenderLivenessSectionMarksStalePoll(t *testing.T) {
	stale := renderLivenessSection("ja", RuntimeSnapshot{LastPollTime: time.Now().Add(-time.Hour)}, 10*time.Second)
	if !strings.Contains(stale, `<span class="status-pill bad"`) || !strings.Contains(stale, "要確認") {
		t.Fatal("stale poll must be marked as needing review")
	}
	fresh := renderLivenessSection("ja", RuntimeSnapshot{LastPollTime: time.Now()}, 10*time.Second)
	if strings.Contains(fresh, `<span class="status-pill bad"`) {
		t.Fatal("fresh poll must not be marked as needing review")
	}
}

func newAttentionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:health-attention-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Project{}, &model.Setting{}, &model.HandoffEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// The attention section shows the admin fallback count and renders nothing
// when there is nothing to review.
func TestRenderHandoffAttentionSection(t *testing.T) {
	db := newAttentionDB(t)
	if got := renderHandoffAttentionSection("ja", db); got != "" {
		t.Fatalf("empty state must render nothing, got %q", got)
	}
	if err := model.CreateProject(db, "prod-1", "Prod One", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.HandoffEvent{
		CommentID: "comment-1", ProductionID: "prod-1", SourceTaskID: "task-1",
		SourceTaskTypeName: "Matchmove", EntityName: "sq010_sh001",
		ProductType: "matchmove", NotifyTaskTypeName: "Compositing",
		FallbackUsed: model.HandoffFallbackAdmin, State: model.HandoffEventPending,
	}).Error; err != nil {
		t.Fatal(err)
	}
	got := renderHandoffAttentionSection("ja", db)
	if !strings.Contains(got, "受け渡しの要確認") {
		t.Fatalf("section must show the handoff review heading, got %q", got)
	}
	if !strings.Contains(got, "1 件") {
		t.Fatalf("section must show the fallback count, got %q", got)
	}
}
