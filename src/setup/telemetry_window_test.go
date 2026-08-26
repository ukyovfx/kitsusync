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
	if got := filterAPIObservations(items, now, time.Minute); len(got) != 2 {
		t.Fatalf("60-second window returned %d observations, want 2", len(got))
	}
	if got := filterAPIObservations(items, now, 5*time.Minute); len(got) != 3 {
		t.Fatalf("5-minute window returned %d observations, want 3", len(got))
	}
}
