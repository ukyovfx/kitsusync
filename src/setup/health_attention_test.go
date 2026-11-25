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

func TestHealthTablesShareAlignedColumnLayout(t *testing.T) {
	liveness := renderLivenessSection("ja", RuntimeSnapshot{LastPollTime: time.Now()}, 10*time.Second)
	if !strings.Contains(liveness, `class="data-table mobile-card-table health-status-table"`) {
		t.Fatalf("liveness table must preserve mobile-card-table and share health-status-table: %s", liveness)
	}

	db, err := gorm.Open(sqlite.Open("file:health-issues-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	model.WriteAuditLog(db, model.AuditLog{
		ProjectName:  "Prod One",
		ErrorMessage: strings.Repeat("long-error-detail-", 8),
		Success:      false,
	})
	issues := recentSystemIssues("ja", nil, db)
	if !strings.Contains(issues, `class="data-table system-issues-table health-status-table"`) {
		t.Fatalf("system issues table must share health-status-table: %s", issues)
	}

	for _, want := range []string{
		`.system-status-sections>.section-stack>.system-issues>.data-table{margin-inline:var(--system-status-content-inset);margin-top:var(--system-status-heading-to-content)}`,
		`.system-status-sections .health-status-table{width:calc(100% - (var(--system-status-content-inset) * 2))}`,
		`.system-status-sections .health-status-table th,.system-status-sections .health-status-table td{text-align:left;overflow-wrap:anywhere}`,
		`@media(min-width:761px){.system-status-sections .health-status-table{table-layout:fixed}`,
		`.system-status-sections .health-status-table th:nth-child(1),.system-status-sections .health-status-table td:nth-child(1){width:34%}`,
		`.system-status-sections .health-status-table th:nth-child(2),.system-status-sections .health-status-table td:nth-child(2){width:33%}`,
		`.system-status-sections .health-status-table th:nth-child(3),.system-status-sections .health-status-table td:nth-child(3){width:33%}`,
	} {
		if !strings.Contains(adminThemeCSS, want) {
			t.Fatalf("adminThemeCSS missing shared health table rule %q", want)
		}
	}
}
