package setup

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type telemetryObservationResponse struct {
	At             string `json:"at"`
	DurationMS     int64  `json:"duration_ms"`
	Success        bool   `json:"success"`
	Classification string `json:"classification,omitempty"`
}

type telemetrySnapshotResponse struct {
	GeneratedAt  string                                    `json:"generated_at"`
	Window       string                                    `json:"window"`
	Observations map[string][]telemetryObservationResponse `json:"observations"`
}

// TelemetrySnapshotHandler serves only bounded, redacted in-memory observations.
func TelemetrySnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		windowName := telemetryWindowName(strings.TrimSpace(r.URL.Query().Get("window")))
		now := time.Now()
		stats := Stats.Snapshot()
		observations := make(map[string][]telemetryObservationResponse, 2)
		for _, service := range []string{"kitsu", "discord"} {
			items := filterAPIObservations(stats.APIObservations[service], now, telemetryWindowDuration(windowName))
			out := make([]telemetryObservationResponse, 0, len(items))
			for _, item := range items {
				out = append(out, telemetryObservationResponse{
					At: item.At.UTC().Format(time.RFC3339), DurationMS: item.Duration.Milliseconds(),
					Success: item.Success, Classification: item.Classification,
				})
			}
			observations[service] = out
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(telemetrySnapshotResponse{
			GeneratedAt: now.UTC().Format(time.RFC3339), Window: windowName, Observations: observations,
		})
	}
}
