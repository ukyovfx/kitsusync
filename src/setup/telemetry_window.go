package setup

import (
	"sort"
	"time"
)

const (
	telemetryWindow60Seconds   = "60s"
	runtimeObservationInterval = 20 * time.Second
	telemetryStaleAfter        = runtimeObservationInterval * 9 / 4
	telemetryLineGapAfter      = runtimeObservationInterval * 3 / 2
)

func filterAPIObservations(items []APIObservation, now time.Time, window time.Duration) []APIObservation {
	cutoff := now.Add(-window)
	filtered := make([]APIObservation, 0, len(items))
	var preceding *APIObservation
	for _, item := range items {
		if item.At.Before(cutoff) {
			if preceding == nil || item.At.After(preceding.At) {
				copy := item
				preceding = &copy
			}
			continue
		}
		if !item.At.After(now) {
			filtered = append(filtered, item)
		}
	}
	if preceding != nil {
		filtered = append([]APIObservation{*preceding}, filtered...)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].At.Before(filtered[j].At) })
	return filtered
}
