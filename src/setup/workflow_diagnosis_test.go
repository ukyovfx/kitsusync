package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/src/model"
)

func TestBuildWorkflowTemplateDiagnosisKeepsExactNameRules(t *testing.T) {
	global := []workflowTaskType{
		{ID: "concept-1", Name: "Concept", ForEntity: "Asset"},
		{ID: "concept-2", Name: "Concept", ForEntity: "Shot"},
		{ID: "shading", Name: "Shading", ForEntity: "Asset"},
		{ID: "rendering", Name: "Rendering", ForEntity: "Shot"},
	}
	production := []workflowTaskType{
		{ID: "concept-1", Name: "Concept", ForEntity: "Asset"},
		{ID: "shading", Name: "Shading", ForEntity: "Asset"},
		{ID: "rendering", Name: "Rendering", ForEntity: "Shot"},
	}
	entries := buildWorkflowTemplateDiagnosis(global, production, nil)
	concept := findWorkflowTemplateEntry(entries, "Concept")
	if concept.Classification != "Ambiguous" {
		t.Fatalf("duplicate global Concept should be ambiguous, got %q", concept.Classification)
	}
	lookdev := findWorkflowTemplateEntry(entries, "LookDev")
	if lookdev.Classification != "Missing" {
		t.Fatalf("LookDev must not be satisfied by Shading, got %q", lookdev.Classification)
	}
	if containsString(lookdev.SimilarCandidates, "Shading") {
		t.Fatalf("Shading should not be presented as a similar LookDev candidate")
	}
	if findWorkflowTemplateEntry(entries, "Rendering").ExpectedTaskType != "" {
		t.Fatalf("Rendering is an actual production type, not a template entry")
	}
}

func TestBuildWorkflowTemplateDiagnosisClassifiesRoutedAndUnrouted(t *testing.T) {
	global := []workflowTaskType{{ID: "concept", Name: "Concept"}}
	production := []workflowTaskType{{ID: "concept", Name: "Concept"}}
	routes := []model.ProjectWebhook{{KitsuProjectID: "p1", TaskType: "Concept", ChannelName: "kitsu-concept"}}
	entries := buildWorkflowTemplateDiagnosis(global, production, routes)
	if got := findWorkflowTemplateEntry(entries, "Concept").Classification; got != "Ready" {
		t.Fatalf("expected routed Concept to be Ready, got %q", got)
	}
	entries = buildWorkflowTemplateDiagnosis(global, production, nil)
	if got := findWorkflowTemplateEntry(entries, "Concept").Classification; got != "Unrouted" {
		t.Fatalf("expected unconfigured Concept to be Unrouted, got %q", got)
	}
}

func TestRouteChannelsShowsSharedChannelAndEscapesDiagnosisHTML(t *testing.T) {
	routes := []model.ProjectWebhook{
		{TaskType: "Animation", ChannelName: "shared"},
		{TaskType: "Lighting", ChannelName: "shared"},
		{TaskType: "Animation", ChannelName: "other"},
	}
	if got := routeChannels(routes, "Animation"); strings.Join(got, ",") != "other,shared" {
		t.Fatalf("unexpected shared channel routes: %v", got)
	}
	status := workflowDiagnosisData{Production: &workflowProduction{ID: "p1", Name: "<safe>"}}
	html := renderWorkflowDiagnosis(status, httptest.NewRequest(http.MethodGet, "/bot/admin/workflow-diagnosis", nil))
	if strings.Contains(html, "<safe>") {
		t.Fatal("production name was not HTML escaped")
	}
	if strings.Contains(html, "Bearer ") || strings.Contains(html, "token") {
		t.Fatal("diagnosis HTML must not expose authentication material")
	}
}

func TestWorkflowStatusNotificationClassification(t *testing.T) {
	for _, status := range []string{"wfa", "RETAKE", "done"} {
		if !workflowStatusNotifiable(status) {
			t.Fatalf("expected %q to be notifiable", status)
		}
	}
	for _, status := range []string{"wip", "ready", "approved", ""} {
		if workflowStatusNotifiable(status) {
			t.Fatalf("expected %q not to be notifiable", status)
		}
	}
}

func TestWorkflowDiagnosisClientDisconnected(t *testing.T) {
	_, err := newWorkflowDiagnosisClient("", "").load("")
	if err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("expected sanitized disconnected error, got %v", err)
	}
}

func TestWorkflowDiagnosisClientReadsProductionScopedData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("expected bearer auth header")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/data/projects":
			_, _ = w.Write([]byte(`[{"id":"project-1","name":"Local Production"}]`))
		case "/api/data/task-types":
			_, _ = w.Write([]byte(`[{"id":"global-1","name":"Shading","for_entity":"Asset","department_id":"dept-1"}]`))
		case "/api/data/task-status":
			_, _ = w.Write([]byte(`[{"id":"status-1","name":"Waiting For Approval","short_name":"wfa","is_feedback_request":true}]`))
		case "/api/data/entity-types":
			_, _ = w.Write([]byte(`[{"id":"entity-1","name":"Character"}]`))
		case "/api/data/departments":
			_, _ = w.Write([]byte(`[{"id":"dept-1","name":"Modeling"}]`))
		case "/api/data/projects/project-1/task-types":
			_, _ = w.Write([]byte(`[{"id":"prod-1","name":"Shading","for_entity":"Asset","department_id":"dept-1"}]`))
		case "/api/data/projects/project-1/settings/task-status":
			_, _ = w.Write([]byte(`[{"id":"status-1","name":"Waiting For Approval","short_name":"wfa","is_feedback_request":true}]`))
		case "/api/data/user/projects/project-1/asset-types":
			_, _ = w.Write([]byte(`[{"id":"asset-1","name":"Character","short_name":"CHAR"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	data, err := newWorkflowDiagnosisClient(server.URL, "test-token").load("")
	if err != nil {
		t.Fatalf("expected diagnosis load to succeed: %v", err)
	}
	if data.Production == nil || data.Production.ID != "project-1" {
		t.Fatalf("unexpected selected production: %#v", data.Production)
	}
	if len(data.ProductionTaskTypes) != 1 || data.ProductionTaskTypes[0].ForEntity != "Asset" {
		t.Fatalf("production task type scope was not preserved: %#v", data.ProductionTaskTypes)
	}
	if len(data.ProductionAssetTypes) != 1 || data.ProductionAssetTypes[0].ID != "asset-1" {
		t.Fatalf("production asset types were not read: %#v", data.ProductionAssetTypes)
	}
}

func findWorkflowTemplateEntry(entries []workflowTemplateDiagnosis, taskType string) workflowTemplateDiagnosis {
	for _, entry := range entries {
		if entry.ExpectedTaskType == taskType {
			return entry
		}
	}
	return workflowTemplateDiagnosis{}
}
