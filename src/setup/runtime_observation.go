package setup

import (
	"context"
	"strings"
	"sync"
	"time"
)

type runtimeAPIProbe struct {
	service string
	run     func() (bool, string)
}

var runtimeAPIMonitoringMu sync.Mutex

func recordAPIMonitoringCycle(stats *RuntimeStats, probes []runtimeAPIProbe) {
	sampledAt := time.Now()
	var wg sync.WaitGroup
	observations := make(map[string]APIObservation, len(probes))
	var observationsMu sync.Mutex
	for _, probe := range probes {
		probe := probe
		wg.Add(1)
		go func() {
			defer wg.Done()
			started := time.Now()
			success, classification := probe.run()
			observationsMu.Lock()
			observations[probe.service] = APIObservation{At: sampledAt, Duration: time.Since(started), Success: success, Classification: classification}
			observationsMu.Unlock()
		}()
	}
	wg.Wait()
	stats.RecordAPIObservationBatch(observations)
}

// ObserveRuntimeAPIMonitoring records Kitsu and Discord probes from one concurrent cycle.
func ObserveRuntimeAPIMonitoring(hostname, apiOverride, kitsuToken, discordToken string) {
	if !runtimeAPIMonitoringMu.TryLock() {
		return // skip an overlapping cycle so sample order remains chronological
	}
	defer runtimeAPIMonitoringMu.Unlock()
	recordAPIMonitoringCycle(Stats, []runtimeAPIProbe{
		{service: "kitsu", run: func() (bool, string) { return probeKitsuRuntime(hostname, apiOverride, kitsuToken) }},
		{service: "discord", run: func() (bool, string) { return probeDiscordRuntime(discordToken) }},
	})
}

func probeDiscordRuntime(token string) (bool, string) {
	if strings.TrimSpace(token) == "" {
		return false, "not_configured"
	}
	info := checkDiscordStatusReadOnly(token, "")
	if info.Error != nil {
		return false, "error"
	}
	return info.BotValid, "success"
}

func probeKitsuRuntime(hostname, apiOverride, token string) (bool, string) {
	classification := "not_configured"
	if strings.TrimSpace(hostname) != "" && strings.TrimSpace(token) != "" {
		connection, err := ResolveKitsuConnection(context.Background(), hostname, apiOverride)
		if err != nil {
			return false, connectionErrorClass(err)
		}
		if err := VerifyKitsuToken(context.Background(), connection, token); err != nil {
			return false, connectionErrorClass(err)
		}
		return true, "success"
	}
	return false, classification
}
