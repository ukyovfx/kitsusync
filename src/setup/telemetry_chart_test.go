package setup

import (
	"strings"
	"testing"
	"time"
)

func TestTelemetryLineGraphUsesTimestampPositionsAndBreaksAcrossFailures(t *testing.T) {
	now := time.Now()
	items := []APIObservation{
		{At: now.Add(-50 * time.Second), Duration: 10 * time.Millisecond, Success: true},
		{At: now.Add(-40 * time.Second), Duration: 30 * time.Millisecond, Success: true},
		{At: now.Add(-30 * time.Second), Duration: 9000 * time.Millisecond, Success: false},
		{At: now.Add(-20 * time.Second), Duration: 20 * time.Millisecond, Success: true},
		{At: now.Add(-10 * time.Second), Duration: 40 * time.Millisecond, Success: true},
	}
	graph := apiObservationLineGraphWithScale(items, "en", telemetryWindow60Seconds, 50)
	if !strings.Contains(graph, `viewBox="0 0 496 104"`) {
		t.Fatalf("graph does not reserve enough Y-axis label space: %s", graph)
	}
	if strings.Count(graph, `class="telemetry-line success"`) != 2 || strings.Count(graph, `class="telemetry-point success"`) != 4 {
		t.Fatalf("successful observations are not drawn as two line segments with four points: %s", graph)
	}
	if strings.Contains(graph, `class="telemetry-failure"`) || strings.Contains(graph, `class="telemetry-bar`) || strings.Contains(graph, "<rect") {
		t.Fatalf("graph must omit failure markers and bars while preserving the successful line segments: %s", graph)
	}
	if strings.Contains(graph, `data-telemetry-duration="9000"`) || strings.Contains(graph, "9000 ms Request failed") {
		t.Fatal("failed observation fabricated or exposed latency")
	}
	if strings.Count(graph, `class="chart-tick"`) != 3 {
		t.Fatalf("graph has %d Y ticks, want 3", strings.Count(graph, `class="chart-tick"`))
	}
	for _, fragment := range []string{`>50ms</text>`, `>25ms</text>`, `>0ms</text>`, `x1="54"`, `x2="484"`, `x="269"`, `60s`, `30s`, `Now`} {
		if !strings.Contains(graph, fragment) {
			t.Fatalf("graph is missing canonical line geometry/label %q: %s", fragment, graph)
		}
	}
}

func TestTelemetryLineGraphsUseIndependentZeroBasedScales(t *testing.T) {
	now := time.Now()
	kitsuItems := []APIObservation{{At: now, Duration: 8 * time.Millisecond, Success: true}}
	discordItems := []APIObservation{{At: now, Duration: 204 * time.Millisecond, Success: true}}
	kitsu := apiObservationLineGraphWithScale(kitsuItems, "en", telemetryWindow60Seconds, observationScaleForItems(kitsuItems))
	discord := apiObservationLineGraphWithScale(discordItems, "en", telemetryWindow60Seconds, observationScaleForItems(discordItems))
	if !strings.Contains(kitsu, `>10ms</text>`) || !strings.Contains(kitsu, `>5ms</text>`) || !strings.Contains(kitsu, `>0ms</text>`) {
		t.Fatalf("Kitsu graph does not expose its 10/5/0ms scale: %s", kitsu)
	}
	if !strings.Contains(discord, `>250ms</text>`) || !strings.Contains(discord, `>125ms</text>`) || !strings.Contains(discord, `>0ms</text>`) {
		t.Fatalf("Discord graph does not expose its independent 250/125/0ms scale: %s", discord)
	}
	failedOnly := []APIObservation{{At: now, Duration: 4000 * time.Millisecond, Success: false}}
	if got := observationScaleForItems(failedOnly); got != 10 {
		t.Fatalf("failed observations affected the latency scale: got %.0fms", got)
	}
}

func TestTelemetryLineGraphGeometryAndTimestampScale(t *testing.T) {
	geometry := telemetryChartGeometry()
	if geometry.Width != 496 || geometry.Height != 104 || geometry.PlotLeft != 54 || geometry.PlotRight != 484 || geometry.PlotMiddle != 45 || geometry.PlotBottom != 82 {
		t.Fatalf("unexpected canonical chart geometry: %#v", geometry)
	}
	items := []APIObservation{{At: time.Now().Add(-55 * time.Second), Duration: 1 * time.Millisecond, Success: true}, {At: time.Now().Add(-5 * time.Second), Duration: 1 * time.Millisecond, Success: true}}
	graph := apiObservationLineGraphWithScale(items, "en", telemetryWindow60Seconds, 10)
	if !strings.Contains(graph, `cx="`) || strings.Count(graph, `class="telemetry-point success"`) != 2 {
		t.Fatalf("observations do not have timestamp-positioned points: %s", graph)
	}
}

func TestTelemetryLineGraphTooltipsAreKeyboardReachableAndFailureSafe(t *testing.T) {
	graph := apiObservationLineGraphWithScale([]APIObservation{
		{At: time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC), Duration: 42 * time.Millisecond, Success: true},
		{At: time.Date(2026, 8, 10, 12, 35, 1, 0, time.UTC), Duration: 900 * time.Millisecond, Success: false},
	}, "en", telemetryWindow60Seconds, 250)
	if strings.Count(graph, `tabindex="0"`) != 1 || strings.Count(graph, `<title>`) != 1 {
		t.Fatalf("successful points need keyboard/native tooltips, while failures must remain visually quiet: %s", graph)
	}
	if !strings.Contains(graph, "42 ms Healthy") || strings.Contains(graph, "Request failed") || strings.Contains(graph, "telemetry-failure") {
		t.Fatal("graph should expose successful point details without rendering a failure mark")
	}
	if strings.Contains(graph, "900 ms") || strings.Contains(graph, "Authorization") || strings.Contains(graph, "Bearer") || strings.Contains(graph, "token") {
		t.Fatal("failure observation fabricated latency or exposed sensitive content")
	}
}

func TestSystemStatusRefreshUsesCanonicalLineGraphContract(t *testing.T) {
	updated := replaceSystemStatusRefreshScript(`<script data-system-status-refresh></script>`)
	for _, fragment := range []string{`viewBox=\"0 0 496 104\"`, `telemetry-line`, `Date.parse(item.at)`, `points.push(null)`, `item.success&&isFinite(value)`, `Request failed`, `60s`, `2m30s`, `Now`, `x1=\"54\"`, `x2=\"484\"`} {
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

func TestSystemStatusInitialAndRefreshGraphsShareLocalizedWindowLabels(t *testing.T) {
	now := time.Now()
	items := []APIObservation{{At: now.Add(-2 * time.Minute), Duration: 25 * time.Millisecond, Success: true}, {At: now.Add(-time.Minute), Duration: 20 * time.Millisecond, Success: true}}
	initial := apiObservationLineGraphWithScale(items, "ja", telemetryWindow5Minutes, 50)
	refresh := systemStatusRefreshScriptCanonical()
	for _, label := range []string{"5分", "2分30秒", "今", "telemetry-line", "chart-time-label"} {
		if !strings.Contains(initial+refresh, label) {
			t.Fatalf("initial/AJAX line graph contract is missing %q", label)
		}
	}
	for _, label := range []string{"5m", "2m30s", "Now"} {
		if !strings.Contains(refresh, label) {
			t.Fatalf("AJAX graph is missing English time label %q", label)
		}
	}
}

func TestSystemStatusMetricAndFailureMetadataRemainSecondaryAndHonest(t *testing.T) {
	stats := RuntimeSnapshot{APIObservations: map[string][]APIObservation{
		"kitsu": {{At: time.Now(), Duration: 600 * time.Millisecond, Success: false}},
	}}
	card := apiObservationDetails("en", stats, "kitsu", telemetryWindow60Seconds)
	if !strings.Contains(card, `class="api-observation-primary"><strong`) || strings.Contains(card, "Current response time") || !strings.Contains(card, "Request failed") {
		t.Fatalf("response state should be the primary value without a redundant label: %s", card)
	}
	if strings.Contains(card, "600 ms") || !strings.Contains(card, `class="api-observation-meta"`) || !strings.Contains(card, "Last updated") {
		t.Fatal("failed sample displayed latency or omitted secondary last-updated metadata")
	}
	if strings.Index(card, `class="api-observation-primary"`) > strings.Index(card, `class="api-observation-meta"`) {
		t.Fatal("last-updated metadata should share the response row after the primary state")
	}
}

func TestSystemStatusUsesSharedViewerLocalFormatterForLineTooltips(t *testing.T) {
	stats := RuntimeSnapshot{APIObservations: map[string][]APIObservation{
		"kitsu": {{At: time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC), Duration: 42 * time.Millisecond, Success: true}},
	}}
	initial := addTelemetryViewerLocalTimes(`<span class="api-observation-meta" data-telemetry-meta>Last updated 00:00:00</span><svg><circle class="telemetry-point success" data-telemetry-at="2026-08-10T12:34:56Z" data-telemetry-duration="42" data-telemetry-success="true"><title>old</title></circle></svg>`, stats, telemetryWindow60Seconds)
	for _, fragment := range []string{`window.kitsuSyncSystemStatusTime=function(value)`, `[data-telemetry-meta][data-telemetry-at]`, `.telemetry-point[data-telemetry-at]`} {
		if !strings.Contains(initial, fragment) {
			t.Fatalf("line tooltip localization is missing %q", fragment)
		}
	}
	if strings.Contains(initial, "toLocaleTimeString") || strings.Contains(initial, `querySelectorAll("[data-telemetry-at]")`) {
		t.Fatal("line tooltip localization uses inconsistent time formatting")
	}
}
