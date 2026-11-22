package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"app/src/api/kitsu"
	"app/src/model"
)

// TestHandoffBrowserAcceptance renders the handoff mapping page with 13
// product types and drives it in real Chromium: no horizontal scroll at
// 1280px, recipient summaries on the hand-off chips, per-cell editing, and a
// save/reload round trip. It shares the dedicated browser job's gate and
// pinned SHA so it never runs in the normal test suite.
func TestHandoffBrowserAcceptance(t *testing.T) {
	if os.Getenv(reviewerBrowserRunEnv) != "1" {
		t.Skip("dedicated authenticated browser acceptance job")
	}
	workDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal("resolve repository root")
	}
	want := strings.TrimSpace(os.Getenv("KITSUSYNC_REVIEWER_BROWSER_SHA"))
	if want == "" {
		t.Fatal("exact browser candidate SHA is required")
	}
	got, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(got)) != want {
		t.Fatalf("browser candidate mismatch: HEAD=%q expected=%q", strings.TrimSpace(string(got)), want)
	}
	outputDir := strings.TrimSpace(os.Getenv("KITSUSYNC_REVIEWER_BROWSER_OUTPUT"))
	if outputDir == "" {
		outputDir = filepath.Join(t.TempDir(), "evidence")
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		t.Fatal("create private browser evidence directory")
	}

	stubHandoffTaskTypes(t, []kitsu.TaskType{
		{ID: "tt-import", Name: "素材取り込み"},
		{ID: "tt-lay", Name: "Layout"},
		{ID: "tt-model", Name: "Modeling"},
		{ID: "tt-rig", Name: "Rigging"},
		{ID: "tt-anim", Name: "Animation"},
		{ID: "tt-fx", Name: "FX"},
		{ID: "tt-light", Name: "Lighting"},
		{ID: "tt-render", Name: "Rendering"},
		{ID: "tt-comp", Name: "Compositing"},
		{ID: "tt-sb", Name: "Storyboard"},
		{ID: "tt-concept", Name: "Concept"},
	})
	db := newForumPageDB(t)
	if err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID: "p1",
			NotifyTiming: model.HandoffTimingMixed,
			NoAssignee:   model.HandoffFallbackAdmin,
		},
		ProductTypes: []model.ProductionHandoffProductType{
			{Name: "plate"}, {Name: "matchmove"}, {Name: "camera"}, {Name: "layout"},
			{Name: "model"}, {Name: "rig"}, {Name: "pointcache"}, {Name: "fxcache"},
			{Name: "lighting"}, {Name: "renderPass"}, {Name: "render"},
			{Name: "storyboard"}, {Name: "concept"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/bot/admin/handoff", HandoffSettingsHandler(db))
	server := httptest.NewServer(mux)
	defer server.Close()

	cmd := exec.Command("node", filepath.Join(workDir, "tools", "handoff-browser-acceptance", "browser.cjs"))
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"HANDOFF_TEST_URL="+server.URL+"/bot/admin/handoff?project=p1&lang=ja",
		"HANDOFF_SCREENSHOTS_DIR="+outputDir,
	)
	output, err := cmd.CombinedOutput()
	t.Logf("Node verify output:\n%s", string(output))
	if err != nil {
		t.Fatalf("Chromium verification failed: %v", err)
	}
}
