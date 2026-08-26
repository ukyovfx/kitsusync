package setup

import "time"

const (
	telemetryWindow60Seconds = "60s"
	telemetryWindow5Minutes  = "5m"
)

func telemetryWindowDuration(value string) time.Duration {
	if value == telemetryWindow5Minutes {
		return 5 * time.Minute
	}
	return time.Minute
}

func telemetryWindowName(value string) string {
	if value == telemetryWindow5Minutes {
		return telemetryWindow5Minutes
	}
	return telemetryWindow60Seconds
}

func filterAPIObservations(items []APIObservation, now time.Time, window time.Duration) []APIObservation {
	cutoff := now.Add(-window)
	filtered := make([]APIObservation, 0, len(items))
	for _, item := range items {
		if !item.At.Before(cutoff) && !item.At.After(now) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
