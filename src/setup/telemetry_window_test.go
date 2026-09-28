package setup

import (
	"testing"
	"time"
)

func TestFilterAPIObservationsUsesTimeWindows(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	items := []APIObservation{
		{At: now.Add(-6 * time.Minute), Duration: time.Second},
		{At: now.Add(-4 * time.Minute), Duration: 2 * time.Second},
		{At: now.Add(-45 * time.Second), Duration: 3 * time.Second},
		{At: now.Add(-2 * time.Second), Duration: 4 * time.Second},
	}
	if got := filterAPIObservations(items, now, time.Minute); len(got) != 3 {
		t.Fatalf("60-second window plus boundary sample returned %d observations, want 3", len(got))
	}
	if got := filterAPIObservations(items, now, 5*time.Minute); len(got) != 4 {
		t.Fatalf("5-minute window plus boundary sample returned %d observations, want 4", len(got))
	}
}

func TestFilterAPIObservationsRetainsPrecedingSampleForLeftBoundary(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	items := []APIObservation{
		{At: now.Add(-70 * time.Second)},
		{At: now.Add(-61 * time.Second)},
		{At: now.Add(-59 * time.Second)},
		{At: now.Add(-35 * time.Second)},
		{At: now.Add(-5 * time.Second)},
		{At: now.Add(time.Second)},
	}
	got := filterAPIObservations(items, now, time.Minute)
	if len(got) != 4 || !got[0].At.Equal(items[1].At) || !got[1].At.Equal(items[2].At) {
		t.Fatalf("60-second series must retain exactly the immediately preceding sample for boundary continuity: %#v", got)
	}
}
