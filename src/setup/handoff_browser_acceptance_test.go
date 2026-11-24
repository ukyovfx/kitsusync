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

// TestHandoffBrowserAcceptance renders the handoff wiring editors and
// drives them in real Chromium: the studio routes on the connection
// settings page and the Production overrides on the handoff page render
// as seeded (inheritance included), neither page scrolls horizontally at
// 1280px, and an edit of both layers survives a save/reload round trip.
// It shares the dedicated browser job's gate and pinned SHA so it never
// runs in the normal test suite.
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
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatal("create private browser evidence directory")
	}

	stubHandoffTaskTypes(t, []kitsu.TaskType{
		{ID: "tt-import", Name: "素材取り込み"},
		{ID: "tt-lay", Name: "Layout"},
		{ID: "tt-model", Name: "Modeling"},
		{ID: "tt-rig", Name: "Rigging"},
		{ID: "tt-shading", Name: "Shading"},
		{ID: "tt-anim", Name: "Animation"},
		{ID: "tt-fx", Name: "FX"},
		{ID: "tt-light", Name: "Lighting"},
		{ID: "tt-render", Name: "Rendering"},
		{ID: "tt-comp", Name: "Compositing"},
		{ID: "tt-sb", Name: "Storyboard"},
		{ID: "tt-concept", Name: "Concept"},
	})
	db := newForumPageDB(t)
	// Studio defaults: Rendering -> Compositing, Layout -> Animation +
	// Lighting. This Production overrides Rendering to "no handoff" and
	// inherits everything else.
	if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{
		FromTaskTypeID: "tt-render", FromTaskTypeName: "Rendering",
		NotifyTaskTypeIDs: model.JoinHandoffNotifyIDs([]string{"tt-comp"}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := model.SaveStudioTaskRoute(db, model.StudioTaskRoute{
		FromTaskTypeID: "tt-lay", FromTaskTypeName: "Layout",
		NotifyTaskTypeIDs: model.JoinHandoffNotifyIDs([]string{"tt-anim", "tt-light"}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionTaskRoute(db, model.ProductionTaskRoute{
		ProductionID: "p1", FromTaskTypeID: "tt-render", FromTaskTypeName: "Rendering",
		NotifyTaskTypeIDs: model.TaskRouteNone,
	}); err != nil {
		t.Fatal(err)
	}
	if err := model.SaveProductionHandoffSettings(db, model.ProductionHandoffSettings{
		Config: model.ProductionHandoffConfig{
			ProductionID: "p1",
			NoAssignee:   model.HandoffFallbackAdmin,
		},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/bot/admin/handoff", HandoffSettingsHandler(db))
	botHandler := BotHandlerWithRuntime(db, nil, func() bool { return true })
	mux.HandleFunc("/bot/admin/bot", botHandler)
	mux.HandleFunc("/bot/admin/bot/connections", botHandler)
	mux.HandleFunc("/bot/admin/bot/statuses", botHandler)
	mux.HandleFunc("/bot/admin/bot/wiring", botHandler)
	server := httptest.NewServer(mux)
	defer server.Close()

	cmd := exec.Command("node", filepath.Join(workDir, "tools", "handoff-browser-acceptance", "browser.cjs"))
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"HANDOFF_TEST_URL="+server.URL+"/bot/admin/handoff?project=p1&lang=ja",
		"HANDOFF_BOT_URL="+server.URL+"/bot/admin/bot/wiring?lang=ja",
		"HANDOFF_SCREENSHOTS_DIR="+outputDir,
	)
	output, err := cmd.CombinedOutput()
	t.Logf("Node verify output:\n%s", string(output))
	if err != nil {
		t.Fatalf("Chromium verification failed: %v", err)
	}
}
