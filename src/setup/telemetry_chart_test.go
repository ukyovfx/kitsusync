package setup

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAPIObservationLineGraphUsesEqualSamplePositions(t *testing.T) {
	now := time.Now()
	items := []APIObservation{
		{At: now.Add(-55 * time.Second), Duration: 10 * time.Millisecond, Success: true},
		{At: now.Add(-30 * time.Second), Duration: 20 * time.Millisecond, Success: true},
		{At: now.Add(-5 * time.Second), Duration: 30 * time.Millisecond, Success: true},
	}
	graph := apiObservationBarGraph(items, "en", telemetryWindow60Seconds)
	if strings.Contains(graph, "<circle") {
		t.Fatal("line graph should not render point markers")
	}
	if !strings.Contains(graph, `d="M0.0,`) || !strings.Contains(graph, `392.0,`) {
		t.Fatal("line graph path is missing equal plot-bound positions")
	}
	if !strings.Contains(graph, `196.0,`) {
		t.Fatal("line graph path does not use equal sample slots")
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
	if !strings.Contains(kitsu, `api-sparkline-y-label-max">10ms</span>`) || !strings.Contains(discord, `api-sparkline-y-label-max">250ms</span>`) {
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

func TestObservationYDomainUsesIndependentHeadroomAndMinimumSpan(t *testing.T) {
	kitsu := observationYDomainForItems([]APIObservation{
		{Duration: 8 * time.Millisecond},
		{Duration: 12 * time.Millisecond},
	})
	discord := observationYDomainForItems([]APIObservation{
		{Duration: 190 * time.Millisecond},
		{Duration: 260 * time.Millisecond},
	})
	if kitsu.Upper >= 100 || kitsu.Lower < 0 || kitsu.Upper-kitsu.Lower < 10 {
		t.Fatalf("Kitsu domain is not a compact padded range: %#v", kitsu)
	}
	if discord.Upper <= 260 || discord.Lower >= 190 || discord.Upper-kitsu.Upper < 100 {
		t.Fatalf("Discord domain is not independently padded: %#v", discord)
	}
}

func TestStableObservationYDomainExpandsImmediatelyAndContractsAfterHold(t *testing.T) {
	service := "domain-stability-test"
	window := telemetryWindow60Seconds
	high := []APIObservation{{Duration: 240 * time.Millisecond}}
	low := []APIObservation{{Duration: 8 * time.Millisecond}}
	first := stableObservationYDomain(service, window, high)
	if first.Upper <= 240 {
		t.Fatalf("expected headroom above outlier, got %#v", first)
	}
	second := stableObservationYDomain(service, window, low)
	if second != first {
		t.Fatalf("domain contracted before stability hold: first=%#v second=%#v", first, second)
	}
}

func TestAPIObservationLineGraphUsesIndependentDomainTicksAndEqualX(t *testing.T) {
	items := []APIObservation{{Duration: 8 * time.Millisecond}, {Duration: 12 * time.Millisecond}, {Duration: 10 * time.Millisecond}}
	domain := observationYDomainForItems(items)
	graph := apiObservationLineGraphWithDomain(items, "en", telemetryWindow60Seconds, domain)
	if !strings.Contains(graph, `class="api-sparkline-y-labels"`) || !strings.Contains(graph, `d="M0.0,`) || !strings.Contains(graph, `196.0,`) {
		t.Fatalf("labels or equal plot slots are missing: %s", graph)
	}
	if strings.Contains(graph, `>0ms</text>`) && domain.Lower > 0 {
		t.Fatal("dynamic lower bound was incorrectly forced to zero")
	}
}

func TestAPIObservationLineGraphUsesReadableTicksWithoutTimeAxisChrome(t *testing.T) {
	item := []APIObservation{{At: time.Now().Add(-5 * time.Second), Duration: 8 * time.Millisecond, Success: true}}
	graph60 := apiObservationBarGraphWithScale(item, "en", telemetryWindow60Seconds, 250)
	for _, want := range []string{`viewBox="0 0 394 104"`, `api-sparkline-y-label-max">250ms</span>`, `api-sparkline-y-label-min">0ms</span>`, `class="telemetry-line"`} {
		if !strings.Contains(graph60, want) {
			t.Fatalf("60-second chart is missing %q", want)
		}
	}
	graph5m := apiObservationBarGraphWithScale(item, "en", telemetryWindow5Minutes, 250)
	if !strings.Contains(graph60, `class="chart-guide"`) {
		t.Fatal("chart is missing the subtle middle guide")
	}
	for _, forbidden := range []string{"60s", "30s", "Now", `class="chart-axis"`, `class="chart-time-label"`} {
		if strings.Contains(graph60, forbidden) || strings.Contains(graph5m, forbidden) {
			t.Fatalf("chart retains removed axis chrome %q", forbidden)
		}
	}
}

func TestAPIObservationLineGraphUsesNoAxisChrome(t *testing.T) {
	graph := apiObservationBarGraphWithScale([]APIObservation{{At: time.Now(), Duration: 8 * time.Millisecond, Success: true}}, "en", telemetryWindow60Seconds, 250)
	if strings.Contains(graph, `x1="2" y1="8" x2="42" y2="82"`) || strings.Contains(graph, `x1="42" y1="8" x2="2" y2="82"`) {
		t.Fatal("chart contains a diagonal axis line")
	}
	if strings.Contains(graph, `class="chart-axis"`) {
		t.Fatal("chart contains axis chrome")
	}
}

func TestAPIObservationChartsReserveSharedYAxisColumn(t *testing.T) {
	geometry := telemetryChartGeometry()
	if geometry.PlotLeft != 0 || geometry.Width-geometry.PlotRight != 392 {
		t.Fatalf("unexpected shared chart geometry: %#v", geometry)
	}
	line := apiObservationLineGraphWithDomain([]APIObservation{{Duration: 300 * time.Millisecond}}, "en", telemetryWindow60Seconds, observationYDomain{Lower: 0, Upper: 300})
	bar := apiObservationBarGraphWithScale([]APIObservation{{Duration: 300 * time.Millisecond}}, "en", telemetryWindow60Seconds, 300)
	for name, graph := range map[string]string{"line": line, "bar": bar} {
		if !strings.Contains(graph, `class="api-sparkline-y-labels"`) || !strings.Contains(graph, `x1="0"`) {
			t.Fatalf("%s chart does not keep labels outside the shared plot start: %s", name, graph)
		}
		if strings.Contains(graph, `<svg class="api-sparkline"`) && strings.Contains(graph[strings.Index(graph, `<svg class="api-sparkline"`):], `class="chart-tick"`) {
			t.Fatalf("%s chart still renders Y labels inside the plot SVG: %s", name, graph)
		}
	}
}

func TestSystemStatusRefreshUsesExternalYAxisLabelSiblings(t *testing.T) {
	updated := replaceSystemStatusRefreshScript(`<script data-system-status-refresh></script>`)
	for _, want := range []string{`api-sparkline-row`, `api-sparkline-y-label-max`, `api-sparkline-y-label-min`, `viewBox=\"0 0 394 104\"`} {
		if !strings.Contains(updated, want) {
			t.Fatalf("refresh graph is missing external Y-axis structure %q", want)
		}
	}
	if strings.Contains(updated, `class=\"chart-tick\"`) {
		t.Fatalf("refresh graph still places persistent Y labels inside the plot SVG: %s", updated)
	}
}

func TestPhase19SharedSparklineGapAndManagementStateAreSymmetric(t *testing.T) {
	for _, want := range []string{
		`--sparkline-axis-gap:2px`,
		`--sparkline-axis-label-width:40px`,
		`.api-sparkline-row{display:grid;grid-template-columns:var(--sparkline-axis-label-width,40px) minmax(0,1fr);gap:var(--sparkline-axis-gap,4px)`,
		`.system-status-sections>.section-stack>.pipeline-health>.page-heading,.system-status-sections>.section-stack>.system-issues>.page-heading{padding-inline:var(--system-status-content-inset)}`,
		`.dashboard-menu-card:hover,.dashboard-menu-card:focus,.dashboard-menu-card:focus-visible,.dashboard-menu-card:active,.dashboard-menu-card[aria-current="page"]{border-color:var(--line);border-right-color:var(--line);border-inline-end-color:var(--line);box-shadow:none;transform:none}`,
	} {
		if !strings.Contains(adminThemeCSS, want) {
			t.Fatalf("Phase 19 shared visual contract is missing %q", want)
		}
	}
}

func TestPhase20SparklineUsesMetadataAlignedCompactLabelColumn(t *testing.T) {
	for _, want := range []string{
		`text-align:left`,
		`.api-sparkline-y-label{position:absolute;left:0;right:auto`,
		`--sparkline-axis-label-width:40px`,
	} {
		if !strings.Contains(adminThemeCSS, want) {
			t.Fatalf("sparkline label alignment contract is missing %q", want)
		}
	}
}

func TestSparklineInteractionUsesWholePlotAndNearestSamples(t *testing.T) {
	script := sparklineInteractionScript()
	for _, fragment := range []string{`data-sparkline-hit-area`, `pointermove`, `pointerleave`, `Math.round`, `ArrowLeft`, `ArrowRight`, `duration_ms`, `toLocaleTimeString`, `sparkline-hover-indicator`, `sparkline-tooltip`} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("sparkline interaction is missing %q", fragment)
		}
	}
	if !strings.Contains(script, `items.length===1?left`) {
		t.Fatal("single-sample plot geometry is not handled")
	}
}

func TestSystemStatusRefreshRebindsSparklineInspectionAfterPolling(t *testing.T) {
	body := `<script data-system-status-refresh>details.innerHTML=foo+graph(items,domain)}function refresh` + "</script>"
	updated := replaceSystemStatusRefreshScript(body)
	if !strings.Contains(updated, `+graph(items,domain);bindSparkline(card,items)}`) {
		t.Fatal("polling refresh does not rebind sparkline interaction")
	}
}

func TestAPIObservationLongYAxisLabelsStayBeforeStablePlot(t *testing.T) {
	for _, value := range []int{25, 250, 500, 1000, 2000, 5000, 10000} {
		graph := apiObservationBarGraphWithScale([]APIObservation{{At: time.Now(), Duration: time.Duration(value) * time.Millisecond, Success: true}}, "en", telemetryWindow60Seconds, float64(value))
		if !strings.Contains(graph, fmt.Sprintf("> %dms</span>", value)) && !strings.Contains(graph, fmt.Sprintf(">%dms</span>", value)) {
			t.Fatalf("%dms label is missing", value)
		}
	}
}

func TestAPIObservationBarsExposeSecretSafeKeyboardTooltips(t *testing.T) {
	graph := apiObservationBarGraphWithScale([]APIObservation{
		{At: time.Date(2026, 8, 10, 12, 34, 56, 0, time.UTC), Duration: 42 * time.Millisecond, Success: true},
		{At: time.Date(2026, 8, 10, 12, 35, 1, 0, time.UTC), Duration: 0, Success: false},
	}, "en", telemetryWindow60Seconds, 250)
	if strings.Contains(graph, "<circle") || strings.Contains(graph, "tabindex=\"0\"") {
		t.Fatal("sparkline should not render point markers")
	}
	if !strings.Contains(graph, `role="img"`) || !strings.Contains(graph, "observations") {
		t.Fatal("sparkline accessible name is incomplete")
	}
	if strings.Contains(graph, "Authorization") || strings.Contains(graph, "Bearer") || strings.Contains(graph, "token") {
		t.Fatal("telemetry tooltip contains secret-like content")
	}
}

func TestSystemStatusVerticalRhythmUsesExplicitHierarchyTokens(t *testing.T) {
	for _, fragment := range []string{
		`--system-status-page-title-to-section:var(--space-5)`,
		`--system-status-heading-to-content:var(--space-3)`,
		`--system-status-internal-item-gap:var(--space-2)`,
		`--system-status-section-to-section:var(--space-5)`,
		`.page-card:has(.system-status-sections)>.page-heading{margin-bottom:var(--system-status-page-title-to-section)}`,
		`.system-status-sections>.system-observability,.system-status-sections>.section-stack{gap:var(--system-status-section-to-section)}`,
		`.system-status-sections .pipeline-health-grid{margin-top:var(--system-status-heading-to-content)`,
		`.system-status-sections .pipeline-health-details{margin-top:var(--system-status-internal-item-gap)}`,
	} {
		if !strings.Contains(adminThemeCSS, fragment) {
			t.Fatalf("System Status vertical rhythm contract is missing %q", fragment)
		}
	}
}

func TestSparklineYAxisLabelsUseSharedPlotBoundAlignment(t *testing.T) {
	graph := apiObservationLineGraphWithDomain(
		[]APIObservation{{Duration: 15 * time.Millisecond}, {Duration: 0}},
		"en",
		telemetryWindow60Seconds,
		observationYDomain{Lower: 0, Upper: 15},
	)
	labelsEnd := strings.Index(graph, `</div><svg class="api-sparkline"`)
	if labelsEnd < 0 || strings.Count(graph[:labelsEnd], `class="api-sparkline-y-label `) != 2 {
		t.Fatalf("external max/min labels are not rendered as the chart-row siblings: %s", graph)
	}
	if strings.Index(graph, `class="api-sparkline-y-labels"`) > strings.Index(graph, `<svg class="api-sparkline"`) {
		t.Fatal("external labels must precede, not nest inside, the visible chart box")
	}
	for _, fragment := range []string{
		`--sparkline-chart-box-top:0px;--sparkline-chart-box-bottom:104px`,
		`.api-observation-details .api-sparkline{margin-top:0}`,
		`.api-sparkline-y-labels{margin-top:0}`,
		`.api-observation-details .api-sparkline-row{margin-top:var(--sparkline-label-safe-gap)}`,
		`.api-sparkline-y-label-max{top:var(--sparkline-chart-box-top);transform:none}`,
		`.api-sparkline-y-label-min{top:calc(var(--sparkline-chart-box-bottom) - 1em);bottom:auto;transform:none}`,
	} {
		if !strings.Contains(adminThemeCSS, fragment) {
			t.Fatalf("sparkline label alignment contract is missing %q", fragment)
		}
	}
	if strings.Contains(adminThemeCSS, `.api-sparkline-y-label-max{top:var(--sparkline-chart-box-top);transform:translateY(-50%)}`) || strings.Contains(adminThemeCSS, `.api-sparkline-y-label-min{top:var(--sparkline-chart-box-bottom);transform:translateY(-50%)}`) {
		t.Fatal("sparkline labels must use visible chart-box edge alignment, not center alignment")
	}
	for _, obsolete := range []string{
		"--sparkline-plot-top",
		"--sparkline-plot-bottom",
		"--sparkline-label-plot-offset",
		`.api-sparkline-y-labels{margin-top:var(--sparkline-label-plot-offset,8px)}`,
	} {
		if strings.Contains(adminThemeCSS, obsolete) {
			t.Fatalf("sparkline label alignment retains obsolete plot-offset contract %q", obsolete)
		}
	}
}
