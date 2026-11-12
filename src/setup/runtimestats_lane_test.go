package setup

import "testing"

func TestRecordProductionPollTracksVisibilityAndStreak(t *testing.T) {
	stats := &RuntimeStats{}
	stats.RecordProductionPoll("p1", 0)
	stats.RecordProductionPoll("p1", 0)
	if got := stats.TakeStallNotification("p1"); got {
		t.Fatal("stall notification fired before three consecutive zero polls")
	}
	stats.RecordProductionPoll("p1", 0)
	if !stats.TakeStallNotification("p1") {
		t.Fatal("stall notification did not fire after three consecutive zero polls")
	}
	if stats.TakeStallNotification("p1") {
		t.Fatal("stall notification fired twice for the same stall episode")
	}
	// A poll with tasks clears the streak and re-arms the alert.
	stats.RecordProductionPoll("p1", 4)
	if stats.TakeStallNotification("p1") {
		t.Fatal("stall notification fired right after tasks were observed")
	}
	snapshot := stats.ProductionPollStats()
	stat, ok := snapshot["p1"]
	if !ok || stat.TaskCount != 4 || stat.ZeroStreak != 0 || stat.StallNotified {
		t.Fatalf("unexpected production poll stat: %+v ok=%v", stat, ok)
	}
	if stat.LastSuccessAt.IsZero() {
		t.Fatal("last polling success time was not recorded")
	}
}

func TestTakeStallNotificationUnknownProductionIsQuiet(t *testing.T) {
	stats := &RuntimeStats{}
	if stats.TakeStallNotification("never-polled") {
		t.Fatal("unknown production reported a stall")
	}
}
