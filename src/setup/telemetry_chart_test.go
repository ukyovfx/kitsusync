package setup

import (
	"strings"
	"testing"
	"time"
)

func TestAPIObservationStaleStateUsesMonitoringCycleAge(t *testing.T) {
	asOf := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	if apiObservationIsStale([]APIObservation{{At: asOf.Add(-44 * time.Second)}}, asOf) {
		t.Fatal("observation inside stale threshold was marked stale")
	}
	if !apiObservationIsStale([]APIObservation{{At: asOf.Add(-46 * time.Second)}}, asOf) {
		t.Fatal("observation older than stale threshold was not marked stale")
	}
	stats := RuntimeSnapshot{APIObservations: map[string][]APIObservation{"kitsu": {{At: asOf.Add(-46 * time.Second), Success: true}}}}
	if got := apiObservationStatusAt("en", stats, "kitsu", asOf); got != "Stale" || apiObservationStatusClassAt(stats, "kitsu", asOf) != "warning" {
		t.Fatalf("stale observation status = %q / %q", got, apiObservationStatusClassAt(stats, "kitsu", asOf))
	}
}

func TestTelemetryLineGraphGeometryAndTimestampScale(t *testing.T) {
	geometry := telemetryChartGeometry()
	if geometry.Width != 496 || geometry.Height != 104 || geometry.PlotLeft != 44 || geometry.PlotRight != 452 || geometry.DataLeft != 48 || geometry.DataRight != 448 || geometry.PlotMiddle != 45 || geometry.PlotCenterX != 248 || geometry.PlotBottom != 82 {
		t.Fatalf("unexpected canonical chart geometry: %#v", geometry)
	}
	asOf := time.Now()
	items := []APIObservation{{At: asOf.Add(-60 * time.Second), Duration: 1 * time.Millisecond, Success: true}, {At: asOf, Duration: 1 * time.Millisecond, Success: true}}
	graph := apiObservationLineGraphAt(items, "en", asOf, observationYDomain{Lower: 0, Upper: 10})
	if !strings.Contains(graph, `cx="`) || strings.Count(graph, `class="telemetry-point success"`) != 2 {
		t.Fatalf("observations do not have timestamp-positioned points: %s", graph)
	}
	if !strings.Contains(graph, `cx="48.0"`) || !strings.Contains(graph, `cx="448.0"`) {
		t.Fatalf("timestamp endpoints need equal internal padding so markers stay visible: %s", graph)
	}
}

func TestTelemetryLineGraphsUseOneFixedRollingDomainAndIdenticalXGeometry(t *testing.T) {
	asOf := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	times := []time.Time{asOf.Add(-time.Minute), asOf.Add(-30 * time.Second), asOf}
	kitsuItems := []APIObservation{{At: times[0], Duration: 8 * time.Millisecond, Success: true}, {At: times[1], Duration: 9 * time.Millisecond, Success: true}, {At: times[2], Duration: 7 * time.Millisecond, Success: true}}
	discordItems := []APIObservation{{At: times[0], Duration: 200 * time.Millisecond, Success: true}, {At: times[1], Duration: 250 * time.Millisecond, Success: true}, {At: times[2], Duration: 180 * time.Millisecond, Success: true}}
	kitsu := apiObservationLineGraphAt(kitsuItems, "en", asOf, observationYDomain{Lower: 0, Upper: 10})
	discord := apiObservationLineGraphAt(discordItems, "en", asOf, observationYDomain{Lower: 0, Upper: 250})
	for _, x := range []string{`cx="48.0"`, `cx="248.0"`, `cx="448.0"`} {
		if !strings.Contains(kitsu, x) || !strings.Contains(discord, x) {
			t.Fatalf("both services must share fixed 60-second X positions at %s: kitsu=%s discord=%s", x, kitsu, discord)
		}
	}
	empty := apiObservationLineGraphAt(nil, "ja", asOf, observationYDomain{Lower: 0, Upper: 10})
	if !strings.Contains(empty, `>60秒</text>`) || !strings.Contains(empty, `>30秒</text>`) || !strings.Contains(empty, `>今</text>`) || strings.Contains(empty, `telemetry-line success`) {
		t.Fatalf("empty state must retain the same fixed timeline without inventing data: %s", empty)
	}
}

func TestSystemStatusRefreshUsesCanonicalLineGraphContract(t *testing.T) {
	updated := replaceSystemStatusRefreshScript(`<script data-system-status-refresh></script>`)
	for _, fragment := range []string{`viewBox=\"0 0 496 104\"`, `telemetry-line`, `Date.parse(item.at)`, `entry.at-previousAt>30000`, `Request failed`, `60s`, `30s`, `Now`, `payload.generated_at`, `window=60s`, `x1=\"44\"`, `x2=\"452\"`, `var left=48,right=448`} {
		if !strings.Contains(updated, fragment) {
			t.Fatalf("refresh graph is missing canonical contract %q", fragment)
		}
	}
	if strings.Contains(updated, "telemetry-failure") || strings.Contains(updated, "Current response time") || strings.Contains(updated, "現在の応答時間") {
		t.Fatal("refresh rendering retained a bottom failure marker or redundant response label")
	}
	for _, obsolete := range []string{"telemetry-bar", "<rect class", "barWidth", "equal sample"} {
		if strings.Contains(updated, obsolete) {
			t.Fatalf("refresh graph retains obsolete bar implementation %q", obsolete)
		}
	}
}
