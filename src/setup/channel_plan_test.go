package setup

import (
	"testing"

	"app/src/api/kitsu"
)

func TestNormalizeTaskTypeChannelName(t *testing.T) {
	cases := map[string]string{
		"Compositing":     "compositing",
		"Concept Art":     "concept-art",
		"3D Animation":    "3d-animation",
		"FX / Simulation": "fx-simulation",
		"  A___B  ":       "a-b",
		"!!!":             "task-type",
	}
	for input, want := range cases {
		if got := NormalizeTaskTypeChannelName(input); got != want {
			t.Fatalf("NormalizeTaskTypeChannelName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildTaskTypeChannelPlanReusesExactChannelAndDetectsCollision(t *testing.T) {
	plan := BuildTaskTypeChannelPlan("production-1", "guild-1", []kitsu.TaskType{
		{ID: "tt-1", Name: "Concept Art"},
		{ID: "tt-2", Name: "Concept-Art"},
		{ID: "tt-3", Name: "Compositing"},
	}, map[string]string{"compositing": "channel-3"})
	if plan.Valid() {
		t.Fatal("collision must make the plan invalid")
	}
	if len(plan.Conflicts) != 1 || plan.CreateCount() != 1 {
		t.Fatalf("unexpected plan conflicts/create count: %#v/%d", plan.Conflicts, plan.CreateCount())
	}
	for _, entry := range plan.Entries {
		if entry.TaskTypeID == "tt-3" && (entry.Action != "reuse" || entry.ExistingID != "channel-3") {
			t.Fatalf("expected exact channel reuse, got %#v", entry)
		}
	}
}

func TestTaskTypeChannelPlanRequiresExplicitConfirmationForCreates(t *testing.T) {
	plan := BuildTaskTypeChannelPlan("p", "g", []kitsu.TaskType{{ID: "tt", Name: "Shot"}}, nil)
	if !plan.Valid() || !plan.RequiresConfirmation() || plan.CreateCount() != 1 {
		t.Fatalf("unexpected create plan: %#v", plan)
	}
}
