package setup

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAPIObservationBarGraphUsesTimePositions(t *testing.T) {
	now := time.Now()
	items := []APIObservation{
		{At: now.Add(-55 * time.Second), Duration: 10 * time.Millisecond, Success: true},
		{At: now.Add(-30 * time.Second), Duration: 20 * time.Millisecond, Success: true},
		{At: now.Add(-5 * time.Second), Duration: 30 * time.Millisecond, Success: true},
	}
	graph := apiObservationBarGraph(items, "en", telemetryWindow60Seconds)
	values := regexp.MustCompile(`x="([0-9.]+)" y="[0-9.]+" width=`).FindAllStringSubmatch(graph, -1)
	if len(values) != len(items) {
		t.Fatalf("expected %d bars, got %d", len(items), len(values))
	}
	first, _ := strconv.ParseFloat(values[0][1], 64)
	last, _ := strconv.ParseFloat(values[len(values)-1][1], 64)
	if first > 100 || last < 400 {
		t.Fatalf("sparse observations did not use the time window: first=%v last=%v", first, last)
	}
}

func TestAPIObservationGraphsUseIndependentRoundedYScales(t *testing.T) {
	stats := RuntimeSnapshot{APIObservations: map[string][]APIObservation{
		"kitsu":   {{At: time.Now().Add(-5 * time.Second), Duration: 8 * time.Millisecond, Success: true}},
		"discord": {{At: time.Now().Add(-5 * time.Second), Duration: 204 * time.Millisecond, Success: true}},
	}}
	kitsuScale := observationScaleForItems(stats.APIObservations["kitsu"])
	discordScale := observationScaleForItems(stats.APIObservations["discord"])
	if kitsuScale != 10 || discordScale != 250 {
		t.Fatalf("expected independent rounded scales 10ms and 250ms, got %v and %v", kitsuScale, discordScale)
	}
	kitsu := apiObservationBarGraphWithScale(stats.APIObservations["kitsu"], "en", telemetryWindow60Seconds, kitsuScale)
	discord := apiObservationBarGraphWithScale(stats.APIObservations["discord"], "en", telemetryWindow60Seconds, discordScale)
	if !strings.Contains(kitsu, `>10ms</text>`) || !strings.Contains(discord, `>250ms</text>`) {
		t.Fatal("graphs do not expose independent Y-axis ceilings")
	}
}

func TestStableObservationScaleUpscalesImmediatelyAndHoldsDownscale(t *testing.T) {
	service := "scale-stability-test"
	window := telemetryWindow60Seconds
	high := []APIObservation{{At: time.Now(), Duration: 240 * time.Millisecond, Success: true}}
	low := []APIObservation{{At: time.Now(), Duration: 8 * time.Millisecond, Success: true}}
	if got := stableObservationScale(service, window, high); got != 250 {
		t.Fatalf("expected immediate scale-up to 250ms, got %v", got)
	}
	if got := stableObservationScale(service, window, low); got != 250 {
		t.Fatalf("expected bounded downscale hold at 250ms, got %v", got)
	}
}

func TestAPIObservationBarGraphUsesReadableTicksAndTimeLabels(t *testing.T) {
	item := []APIObservation{{At: time.Now().Add(-5 * time.Second), Duration: 8 * time.Millisecond, Success: true}}
	graph60 := apiObservationBarGraphWithScale(item, "en", telemetryWindow60Seconds, 250)
	for _, want := range []string{`viewBox="0 0 466 104"`, ">250ms</text>", ">125ms</text>", ">0ms</text>", `x="54" y="100">60s`, `x="259" y="100">30s`, `x="464" y="100">Now`} {
		if !strings.Contains(graph60, want) {
			t.Fatalf("60-second chart is missing %q", want)
		}
	}
	graph5m := apiObservationBarGraphWithScale(item, "en", telemetryWindow5Minutes, 250)
	for _, want := range []string{`x="54" y="100">5m`, `x="259" y="100">2m30s`, `x="464" y="100">Now`} {
		if !strings.Contains(graph5m, want) {
			t.Fatalf("5-minute chart is missing %q", want)
		}
	}
	if !strings.Contains(graph60, `class="chart-guide"`) {
		t.Fatal("chart is missing the subtle middle guide")
	}
}

func TestAPIObservationBarGraphUsesOrthogonalAxes(t *testing.T) {
	graph := apiObservationBarGraphWithScale([]APIObservation{{At: time.Now(), Duration: 8 * time.Millisecond, Success: true}}, "en", telemetryWindow60Seconds, 250)
	if strings.Contains(graph, `x1="2" y1="8" x2="42" y2="82"`) || strings.Contains(graph, `x1="42" y1="8" x2="2" y2="82"`) {
		t.Fatal("chart contains a diagonal axis line")
	}
	if !strings.Contains(graph, `x1="54" y1="8" x2="54" y2="82"`) {
		t.Fatal("chart is missing the vertical Y axis")
	}
}

func TestAPIObservationLongYAxisLabelsStayBeforeStablePlot(t *testing.T) {
	for _, value := range []int{25, 250, 500, 1000, 2000, 5000, 10000} {
		graph := apiObservationBarGraphWithScale([]APIObservation{{At: time.Now(), Duration: time.Duration(value) * time.Millisecond, Success: true}}, "en", telemetryWindow60Seconds, float64(value))
		if !strings.Contains(graph, `text-anchor="end" x="48"`) {
			t.Fatalf("%dms tick is not right-aligned in the reserved label column", value)
		}
		if !strings.Contains(graph, `x1="54" y1="8" x2="54" y2="82"`) || !strings.Contains(graph, `x1="54" y1="82" x2="464" y2="82"`) {
			t.Fatalf("%dms chart changed the stable plot geometry", value)
		}
		if !strings.Contains(graph, fmt.Sprintf(">%dms</text>", value)) {
			t.Fatalf("%dms label is missing", value)
		}
	}
}

func TestAPIObservationBarsExposeSecretSafeKeyboardTooltips(t *testing.T) {
	graph := apiObservationBarGraphWithScale([]APIObservation{
		{At: time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC), Duration: 42 * time.Millisecond, Success: true},
		{At: time.Date(2026, 8, 10, 12, 35, 1, 0, time.UTC), Duration: 0, Success: false},
	}, "en", telemetryWindow60Seconds, 250)
	if strings.Count(graph, `tabindex="0" role="img"`) != 2 {
		t.Fatal("each telemetry bar should be keyboard reachable")
	}
	if !strings.Contains(graph, "Healthy") || !strings.Contains(graph, "Request failed") || !strings.Contains(graph, "42 ms") {
		t.Fatal("telemetry tooltip labels are incomplete")
	}
	if strings.Contains(graph, "Authorization") || strings.Contains(graph, "Bearer") || strings.Contains(graph, "token") {
		t.Fatal("telemetry tooltip contains secret-like content")
	}
}
