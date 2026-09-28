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

func ObserveKitsuRuntimeConnection(connection KitsuURLModel, token string) {
	started := time.Now()
	classification := "not_configured"
	if strings.TrimSpace(token) != "" {
		if err := VerifyKitsuToken(context.Background(), connection, token); err == nil {
			Stats.RecordAPIObservation("kitsu", started, true, "success")
			return
		} else {
			classification = connectionErrorClass(err)
		}
	}
	Stats.RecordAPIObservation("kitsu", started, false, classification)
}

// ObserveKitsuRuntime records one bounded read-only authenticated health observation.
func ObserveKitsuRuntime(hostname, token string) {
	started := time.Now()
	success := false
	classification := "not_configured"
	if strings.TrimSpace(hostname) != "" && strings.TrimSpace(token) != "" {
		connection, err := ResolveAndProbeKitsu(context.Background(), hostname, APISourceLegacy)
		if err != nil {
			classification = connectionErrorClass(err)
		} else if err := VerifyKitsuToken(context.Background(), connection, token); err != nil {
			classification = connectionErrorClass(err)
		} else {
			success, classification = true, "success"
		}
	}
	Stats.RecordAPIObservation("kitsu", started, success, classification)
}

// ObserveDiscordRuntime records one bounded read-only bot identity observation.
func ObserveDiscordRuntime(token string) {
	if strings.TrimSpace(token) == "" {
		Stats.RecordAPIObservation("discord", time.Now(), false, "not_configured")
		return
	}
	checkDiscordStatus(token, "")
}
