package setup

import (
	"strings"
	"testing"
	"time"
)

func TestAPIObservationBarsUseTimestampGeometryAndCanonicalTicks(t *testing.T) {
	now := time.Now()
	items := []APIObservation{
		{At: now.Add(-55 * time.Second), Duration: 10 * time.Millisecond, Success: true},
		{At: now.Add(-5 * time.Second), Duration: 30 * time.Millisecond, Success: false},
	}
	graph := apiObservationBarGraphWithScale(items, "en", telemetryWindow60Seconds, 50)
	if !strings.Contains(graph, `viewBox="0 0 466 104"`) {
		t.Fatal("chart does not use the canonical 466x104 viewBox")
	}
	if strings.Count(graph, `<rect class="telemetry-bar`) != len(items) {
		t.Fatalf("rendered %d bars, want %d: %s", strings.Count(graph, `<rect class="telemetry-bar`), len(items), graph)
	}
	if !strings.Contains(graph, `class="telemetry-bar success"`) || !strings.Contains(graph, `class="telemetry-bar failure"`) {
		t.Fatal("success and failure bars are not semantically colored")
	}
	if strings.Contains(graph, "telemetry-line") || strings.Contains(graph, "<path") {
		t.Fatal("canonical telemetry graph must not render a line path")
	}
	if strings.Count(graph, `class="chart-tick"`) != 3 {
		t.Fatalf("chart has %d Y ticks, want exactly 3", strings.Count(graph, `class="chart-tick"`))
	}
	for _, label := range []string{`>60s<`, `>30s<`, `>0s<`, `x1="34"`, `x2="464"`, `x="233"`} {
		if !strings.Contains(graph, label) {
			t.Fatalf("chart is missing canonical geometry/label %q: %s", label, graph)
		}
	}
}

func TestAPIObservationGraphsUseIndependentZeroBasedSteppedScales(t *testing.T) {
	kitsuItems := []APIObservation{{At: time.Now(), Duration: 8 * time.Millisecond, Success: true}}
	discordItems := []APIObservation{{At: time.Now(), Duration: 204 * time.Millisecond, Success: true}}
	kitsu := apiObservationBarGraphWithScale(kitsuItems, "en", telemetryWindow60Seconds, observationScaleForItems(kitsuItems))
	discord := apiObservationBarGraphWithScale(discordItems, "en", telemetryWindow60Seconds, observationScaleForItems(discordItems))
	if !strings.Contains(kitsu, `>10ms</text>`) || !strings.Contains(kitsu, `>5ms</text>`) || !strings.Contains(kitsu, `>0ms</text>`) {
		t.Fatalf("Kitsu chart does not expose the 10/5/0ms scale: %s", kitsu)
	}
	if !strings.Contains(discord, `>250ms</text>`) || !strings.Contains(discord, `>125ms</text>`) || !strings.Contains(discord, `>0ms</text>`) {
		t.Fatalf("Discord chart does not expose the independent 250/125/0ms scale: %s", discord)
	}
}

func TestTelemetryChartGeometryMatchesCanonicalPlot(t *testing.T) {
	geometry := telemetryChartGeometry()
	if geometry.Width != 466 || geometry.Height != 104 || geometry.PlotLeft != 34 || geometry.PlotRight != 464 || geometry.PlotMiddle != 45 || geometry.PlotBottom != 82 {
		t.Fatalf("unexpected canonical chart geometry: %#v", geometry)
	}
}

func TestAPIObservationBarsExposeSecretSafeKeyboardTooltips(t *testing.T) {
	graph := apiObservationBarGraphWithScale([]APIObservation{
		{At: time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC), Duration: 42 * time.Millisecond, Success: true},
		{At: time.Date(2026, 8, 10, 12, 35, 1, 0, time.UTC), Duration: 9 * time.Millisecond, Success: false},
	}, "en", telemetryWindow60Seconds, 250)
	if strings.Count(graph, `tabindex="0"`) != 2 || strings.Count(graph, `<title>`) != 2 {
		t.Fatalf("each real bar must have keyboard/native tooltip accessibility: %s", graph)
	}
	if !strings.Contains(graph, "42 ms Healthy") || !strings.Contains(graph, "Request failed") {
		t.Fatal("bar accessibility labels do not distinguish success and failure safely")
	}
	if strings.Contains(graph, "9 ms Request failed") || strings.Contains(graph, "Authorization") || strings.Contains(graph, "Bearer") || strings.Contains(graph, "token") {
		t.Fatal("failure tooltip fabricated latency or exposed secret-like content")
	}
}

func TestSystemStatusRefreshUsesCanonicalBarContract(t *testing.T) {
	updated := replaceSystemStatusRefreshScript(`<script data-system-status-refresh></script>`)
	for _, fragment := range []string{`viewBox=\"0 0 466 104\"`, `telemetry-bar`, `Date.parse(item.at)`, `tabindex=\"0\"`, `Request failed`, `60s`, `2.5m`, `0s`, `x1=\"34\"`, `x2=\"464\"`} {
		if !strings.Contains(updated, fragment) {
			t.Fatalf("refresh graph is missing canonical contract %q", fragment)
		}
	}
	for _, obsolete := range []string{"stableDomain", "telemetry-line", "394 104", "equal sample"} {
		if strings.Contains(updated, obsolete) {
			t.Fatalf("refresh graph retains obsolete telemetry implementation %q", obsolete)
		}
	}
}

func TestSystemStatusRefreshAndInitialGraphUseMatchingLabels(t *testing.T) {
	items := []APIObservation{{At: time.Now().Add(-2 * time.Minute), Duration: 25 * time.Millisecond, Success: true}}
	initial := apiObservationBarGraphWithScale(items, "ja", telemetryWindow5Minutes, 50)
	refresh := systemStatusRefreshScriptCanonical()
	for _, label := range []string{"5m", "2.5m", "0s", "telemetry-bar", "chart-time-label"} {
		if !strings.Contains(initial+refresh, label) {
			t.Fatalf("initial/AJAX telemetry contract is missing %q", label)
		}
	}
	for _, obsolete := range []string{"Now", "2m30s", "5分", "2分30秒", "今", "60秒", "30秒"} {
		if strings.Contains(initial+refresh, obsolete) {
			t.Fatalf("initial/AJAX telemetry contract retained language-specific label %q", obsolete)
		}
	}
}

func TestSystemStatusUsesOneViewerLocalHHMMSSFormatter(t *testing.T) {
	stats := RuntimeSnapshot{APIObservations: map[string][]APIObservation{
		"kitsu": {{At: time.Now().Add(-5 * time.Second), Duration: 12 * time.Millisecond, Success: true}},
	}}
	initial := addTelemetryViewerLocalTimes(`<span class="api-observation-meta" data-telemetry-meta>Last updated 00:00:00</span>`, stats, telemetryWindow60Seconds)
	refresh := systemStatusRefreshScriptCanonical()
	for name, markup := range map[string]string{"initial": initial, "refresh": refresh} {
		if !strings.Contains(markup, `kitsuSyncSystemStatusTime`) {
			t.Fatalf("%s markup does not use the shared viewer-local formatter", name)
		}
		if strings.Contains(markup, "toLocaleTimeString") || strings.Contains(markup, "hour12") {
			t.Fatalf("%s markup permits locale-shaped or AM/PM time output", name)
		}
	}
	if !strings.Contains(initial, `Last updated`) || !strings.Contains(initial, `window.kitsuSyncSystemStatusTime(node.getAttribute("data-telemetry-at"))`) {
		t.Fatal("initial metadata does not preserve the single Last updated line")
	}
	if !strings.Contains(refresh, `data-telemetry-meta`) || !strings.Contains(refresh, `function localTime(value){return window.kitsuSyncSystemStatusTime(value)}`) {
		t.Fatal("AJAX metadata does not use the shared HH:MM:SS formatter")
	}
}

func TestSystemStatusMetadataKeepsInitialAndRefreshLayoutInParity(t *testing.T) {
	stats := RuntimeSnapshot{APIObservations: map[string][]APIObservation{
		"kitsu": {{At: time.Now().Add(-2 * time.Second), Duration: 18 * time.Millisecond, Success: true}},
	}}
	initial := apiObservationDetails("en", stats, "kitsu", telemetryWindow60Seconds)
	refresh := systemStatusRefreshScriptCanonical()
	for name, markup := range map[string]string{"initial": initial, "refresh": refresh} {
		if !strings.Contains(markup, "api-observation-primary") || !strings.Contains(markup, "api-observation-meta") {
			t.Fatalf("%s metadata does not use the shared primary/meta row contract", name)
		}
		if strings.Contains(markup, "Last 5 minutes") || strings.Contains(markup, "Last 60 seconds") {
			t.Fatalf("%s metadata exposes selected-window prose", name)
		}
	}
}

func TestInitialTelemetryLocalizesMetadataAndBarTooltipsWithSharedFormatter(t *testing.T) {
	stats := RuntimeSnapshot{APIObservations: map[string][]APIObservation{
		"kitsu": {{At: time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC), Duration: 42 * time.Millisecond, Success: true}},
	}}
	body := `<span class="api-observation-meta" data-telemetry-meta>Last updated 00:00:00</span><svg><rect class="telemetry-bar success" data-telemetry-at="2026-08-10T12:34:56Z" data-telemetry-duration="42" data-telemetry-success="true"><title>old</title></rect></svg>`
	initial := addTelemetryViewerLocalTimes(body, stats, telemetryWindow60Seconds)
	for _, fragment := range []string{
		`window.kitsuSyncSystemStatusTime=function(value)`,
		`[data-telemetry-meta][data-telemetry-at]`,
		`.telemetry-bar[data-telemetry-at]`,
		`window.kitsuSyncSystemStatusTime(node.getAttribute("data-telemetry-at"))`,
	} {
		if !strings.Contains(initial, fragment) {
			t.Fatalf("initial telemetry localization is missing %q", fragment)
		}
	}
	if strings.Contains(initial, `querySelectorAll("[data-telemetry-at]")`) || strings.Contains(initial, `new Intl.DateTimeFormat(undefined`) {
		t.Fatal("initial telemetry localization still applies locale-shaped formatting to every telemetry node")
	}
}
