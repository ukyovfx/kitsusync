package setup

import (
	"context"
	"strings"
	"time"
)

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
