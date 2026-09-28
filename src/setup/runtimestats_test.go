package setup

import (
	"testing"
	"time"
)

func TestRuntimeMonitoringCycleRunsProbesConcurrentlyWithSharedTimestamp(t *testing.T) {
	stats := &RuntimeStats{}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	probe := func() (bool, string) {
		entered <- struct{}{}
		<-release
		return true, "success"
	}
	done := make(chan struct{})
	go func() {
		recordAPIMonitoringCycle(stats, []runtimeAPIProbe{{service: "kitsu", run: probe}, {service: "discord", run: probe}})
		close(done)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("API probes did not begin concurrently")
		}
	}
	if snapshot := stats.Snapshot(); len(snapshot.APIObservations) != 0 {
		t.Fatal("monitoring cycle exposed one service before its peer probe completed")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitoring cycle did not finish")
	}
	snapshot := stats.Snapshot()
	kitsu, discord := snapshot.APIObservations["kitsu"], snapshot.APIObservations["discord"]
	if len(kitsu) != 1 || len(discord) != 1 || !kitsu[0].At.Equal(discord[0].At) {
		t.Fatalf("probe cycle timestamps differ: kitsu=%#v discord=%#v", kitsu, discord)
	}
}

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
