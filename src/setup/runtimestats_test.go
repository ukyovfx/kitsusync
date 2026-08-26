package setup

import (
	"testing"
	"time"
)

func TestRuntimeStatsBoundsAPIObservationsAndKeepsSecretsOut(t *testing.T) {
	stats := &RuntimeStats{StartTime: time.Now(), apiObservations: make(map[string][]APIObservation)}
	for i := 0; i < maxAPIObservations+5; i++ {
		stats.RecordAPIObservation("kitsu", time.Now(), i%2 == 0, "success")
	}
	snapshot := stats.Snapshot()
	if got := len(snapshot.APIObservations["kitsu"]); got != maxAPIObservations {
		t.Fatalf("observation history length = %d, want %d", got, maxAPIObservations)
	}
	if len(snapshot.APIObservations["kitsu"][0].Classification) == 0 {
		t.Fatal("observation classification was not retained")
	}
}
