package setup

import (
	"encoding/json"
	"os"
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
