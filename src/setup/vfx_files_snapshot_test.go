package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"

	"app/src/api/kitsu"
)

const (
	snapProduction = "10000000-0000-4000-8000-000000000001"
	snapEpisode    = "20000000-0000-4000-8000-000000000001"
	snapSequence   = "30000000-0000-4000-8000-000000000001"
	snapShot       = "40000000-0000-4000-8000-000000000001"
	snapTask       = "50000000-0000-4000-8000-000000000002"
	snapType       = "50000000-0000-4000-8000-000000000010"
)

func TestBuildVFXFilesSnapshotFiltersScopeAndIncludesAncestors(t *testing.T) {
	source := vfxFilesSnapshotSourceData{
		Production: vfxFilesSnapshotRawObject{ID: snapProduction, Name: "Synthetic Production", ProjectID: snapProduction},
		Episodes:   []vfxFilesSnapshotRawObject{{ID: snapEpisode, ProjectID: snapProduction, Name: "EP01"}},
		Sequences:  []vfxFilesSnapshotRawObject{{ID: snapSequence, ProjectID: snapProduction, EpisodeID: snapEpisode, Name: "SQ01"}},
		Shots:      []vfxFilesSnapshotRawObject{{ID: snapShot, ProjectID: snapProduction, ParentID: snapSequence, Name: "SH010"}},
		Tasks:      []vfxFilesSnapshotRawObject{{ID: snapTask, ProjectID: snapProduction, EntityID: snapShot, TaskTypeID: snapType, Name: "Compositing"}},
		TaskTypes:  []kitsu.TaskType{{ID: snapType, Name: "Compositing"}},
	}
	objects, fingerprint, err := buildVFXFilesSnapshot(snapProduction, vfxFilesSnapshotScope{Mode: "adopt_existing", Shots: true}, source)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" || len(objects) != 6 {
		t.Fatalf("snapshot size/fingerprint = %d/%q", len(objects), fingerprint)
	}
	byID := map[string]vfxFilesSnapshotObject{}
	for _, object := range objects {
		byID[object.ID] = object
	}
	if !byID[snapShot].IncludedInSync || byID[snapSequence].IncludedInSync || byID[snapEpisode].IncludedInSync || !byID[snapProduction].IncludedInSync {
		t.Fatalf("selected object/ancestor state is wrong: %#v", byID)
	}
	pages, hashes, fullHash, err := pageVFXFilesSnapshot(objects)
	if err != nil || len(pages) != 1 || hashes[0] == "" || fullHash == "" {
		t.Fatalf("page snapshot: pages=%d hashes=%v full=%q err=%v", len(pages), hashes, fullHash, err)
	}
	if _, _, err := buildVFXFilesSnapshot(snapProduction, vfxFilesSnapshotScope{Mode: "future_only", Shots: true}, source); err != nil {
		t.Fatalf("future-only scope should preserve the production root: %v", err)
	}
}

func TestVFXFilesSnapshotPageHashMatchesCanonicalFixture(t *testing.T) {
	data, err := os.ReadFile("../../docs/integrations/vfx-files-plugin/v1/fixtures/sync-snapshot-page-0.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PageSHA256 string                   `json:"page_sha256"`
		Objects    []vfxFilesSnapshotObject `json:"objects"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	pages, hashes, fullHash, err := pageVFXFilesSnapshot(fixture.Objects)
	if err != nil || len(pages) != 1 || hashes[0] != fixture.PageSHA256 || fullHash != fixture.PageSHA256 {
		t.Fatalf("canonical fixture digest mismatch: page=%v full=%q want=%q err=%v", hashes, fullHash, fixture.PageSHA256, err)
	}
}

func TestBuildVFXFilesSnapshotRejectsUnresolvedCrossProductionAndTaskType(t *testing.T) {
	base := vfxFilesSnapshotSourceData{
		Production: vfxFilesSnapshotRawObject{ID: snapProduction, Name: "Synthetic Production"},
		Shots:      []vfxFilesSnapshotRawObject{{ID: snapShot, ProjectID: "60000000-0000-4000-8000-000000000001", ParentID: "70000000-0000-4000-8000-000000000001", Name: "SH010"}},
	}
	if _, _, err := buildVFXFilesSnapshot(snapProduction, vfxFilesSnapshotScope{Mode: "adopt_existing", Shots: true}, base); err == nil {
		t.Fatal("cross-Production object accepted")
	}
	base.Shots[0].ProjectID = snapProduction
	if _, _, err := buildVFXFilesSnapshot(snapProduction, vfxFilesSnapshotScope{Mode: "adopt_existing", Shots: true}, base); err == nil {
		t.Fatal("selected object with unresolved parent accepted")
	}
	base.Shots = nil
	base.Tasks = []vfxFilesSnapshotRawObject{{ID: snapTask, ProjectID: snapProduction, EntityID: snapShot, TaskTypeID: snapType, Name: "Task"}}
	if _, _, err := buildVFXFilesSnapshot(snapProduction, vfxFilesSnapshotScope{Mode: "adopt_existing", Tasks: true}, base); err == nil {
		t.Fatal("Task without included Task Type metadata accepted")
	}
}

func TestBuildVFXFilesSnapshotNormalizesZouUTCDateTimeWithoutZone(t *testing.T) {
	source := vfxFilesSnapshotSourceData{
		Production: vfxFilesSnapshotRawObject{ID: snapProduction, Name: "Synthetic Production", UpdatedAt: "2026-10-14T05:20:15"},
	}
	objects, _, err := buildVFXFilesSnapshot(snapProduction, vfxFilesSnapshotScope{Mode: "new_production"}, source)
	if err != nil {
		t.Fatalf("normalize Zou UTC timestamp: %v", err)
	}
	if len(objects) != 1 || objects[0].UpdatedAt == nil || *objects[0].UpdatedAt != "2026-10-14T05:20:15Z" {
		t.Fatalf("normalized Production timestamp = %#v, want RFC3339 UTC", objects)
	}
}

func TestFetchVFXFilesKitsuAssetsReadsBareProjectListOnce(t *testing.T) {
	const assetID = "60000000-0000-4000-8000-000000000001"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/api/data/projects/"+snapProduction+"/assets" {
			t.Errorf("unexpected Kitsu asset path: %s", r.URL.Path)
		}
		if r.URL.Query().Has("page") || r.URL.Query().Has("limit") {
			t.Errorf("bare project asset listing must not be paginated: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{"id":%q,"project_id":%q,"name":"Asset A"}]`, assetID, snapProduction)
	}))
	defer server.Close()

	connection := KitsuURLModel{
		ResolvedAPIBaseURL: server.URL + "/api",
		VerifiedIPs:        []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		TargetScope:        "loopback",
	}
	var assets []vfxFilesSnapshotRawObject
	err := fetchVFXFilesKitsuArray(context.Background(), connection, "synthetic-session-token", "/data/projects/"+snapProduction+"/assets", &assets)
	if err != nil {
		t.Fatalf("fetch bare asset collection: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("Kitsu asset requests = %d, want exactly one", requests.Load())
	}
	if len(assets) != 1 || assets[0].ID != assetID {
		t.Fatalf("assets = %#v, want the one returned asset", assets)
	}
}

func TestBuildVFXFilesSnapshotResolvesTaskTypesMissingFromProjectSettings(t *testing.T) {
	const (
		assetID    = "60000000-0000-4000-8000-000000000001"
		taskTypeID = "70000000-0000-4000-8000-000000000001"
		otherType  = "70000000-0000-4000-8000-000000000002"
		actorID    = "80000000-0000-4000-8000-000000000001"
	)
	globalTaskTypesRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/data/projects/" + snapProduction:
			_, _ = fmt.Fprintf(w, `{"id":%q,"project_id":%q,"name":"Synthetic Production"}`, snapProduction, snapProduction)
		case "/api/data/projects/" + snapProduction + "/episodes", "/api/data/projects/" + snapProduction + "/sequences", "/api/data/projects/" + snapProduction + "/shots", "/api/data/projects/" + snapProduction + "/task-types":
			_, _ = io.WriteString(w, `[]`)
		case "/api/data/projects/" + snapProduction + "/assets":
			_, _ = fmt.Fprintf(w, `[{"id":%q,"project_id":%q,"name":"Asset A"}]`, assetID, snapProduction)
		case "/api/data/projects/" + snapProduction + "/tasks":
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"project_id":%q,"entity_id":%q,"task_type_id":%q,"name":"Compositing"}],"page":1,"is_more":false}`, snapTask, snapProduction, assetID, taskTypeID)
		case "/api/data/task-types":
			globalTaskTypesRequested = true
			_, _ = fmt.Fprintf(w, `[{"id":%q,"name":"Compositing"},{"id":%q,"name":"Unrelated"}]`, taskTypeID, otherType)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	connection := KitsuURLModel{
		ResolvedAPIBaseURL: server.URL + "/api",
		VerifiedIPs:        []netip.Addr{netip.MustParseAddr("127.0.0.1")},
		TargetScope:        "loopback",
	}
	source := fakeVFXFilesKitsuIdentitySource{
		current: kitsu.Person{ID: actorID, Email: "manager@example.test", Role: "manager", Active: true},
		team:    []kitsu.Person{{ID: actorID, ProjectRole: "manager", Active: true}},
	}
	reader := vfxFilesKitsuSessionAuthorizer{connection: connection, email: "manager@example.test", token: "synthetic-session-token", role: "manager", source: source}
	objects, _, err := reader.BuildVFXFilesSnapshot(context.Background(), snapProduction, vfxFilesSnapshotScope{Mode: "new_production", Assets: true, Tasks: true})
	if err != nil {
		t.Fatalf("build snapshot with Task Type absent from project settings: %v", err)
	}
	if !globalTaskTypesRequested {
		t.Fatal("Kitsu global Task Type metadata was not queried for a Task referenced by the Production")
	}
	types := map[string]bool{}
	for _, object := range objects {
		if object.ObjectType == "task_type" {
			types[object.ID] = true
		}
	}
	if !types[taskTypeID] || types[otherType] {
		t.Fatalf("snapshot Task Type metadata = %v, want only the Task's referenced type", types)
	}
}
