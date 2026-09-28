package setup

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelemetrySnapshotHandlerIsReadOnlyAndSecretSafe(t *testing.T) {
	Stats.mu.Lock()
	Stats.apiObservations = map[string][]APIObservation{}
	Stats.mu.Unlock()
	sampledAt := time.Now()
	Stats.RecordAPIObservationAt("kitsu", sampledAt.Add(-65*time.Second), 10*time.Millisecond, true, "success")
	Stats.RecordAPIObservationAt("kitsu", sampledAt.Add(-45*time.Second), 12*time.Millisecond, true, "success")
	Stats.RecordAPIObservationAt("discord", sampledAt.Add(-65*time.Second), 20*time.Millisecond, true, "success")
	Stats.RecordAPIObservationAt("discord", sampledAt.Add(-45*time.Second), 22*time.Millisecond, true, "success")
	req := httptest.NewRequest("GET", "/bot/api/setup/observability?window=5m", nil)
	res := httptest.NewRecorder()
	TelemetrySnapshotHandler()(res, req)
	if res.Code != 200 || res.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("snapshot response = %d / %q", res.Code, res.Header().Get("Content-Type"))
	}
	body := res.Body.String()
	var snapshot telemetrySnapshotResponse
	if json.Unmarshal(res.Body.Bytes(), &snapshot) != nil || snapshot.Window != telemetryWindow60Seconds || snapshot.GeneratedAt == "" {
		t.Fatalf("snapshot did not retain the fixed shared 60-second timeline: %s", body)
	}
	if len(snapshot.Observations["kitsu"]) != 2 || len(snapshot.Observations["discord"]) != 2 || snapshot.Observations["kitsu"][0].At != snapshot.Observations["discord"][0].At || snapshot.Observations["kitsu"][0].At != sampledAt.Add(-65*time.Second).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("snapshot must include one shared pre-window sample and identical cycle timestamps: %#v", snapshot.Observations)
	}
	if !strings.Contains(body, `"observations"`) || strings.Contains(body, "Authorization") || strings.Contains(body, "Bearer") {
		t.Fatal("snapshot is not a redacted observation payload")
	}
}

func TestDiscordRuntimeProbeKeepsMissingCredentialUnconfigured(t *testing.T) {
	success, classification := probeDiscordRuntime(" \t")
	if success || classification != "not_configured" {
		t.Fatalf("missing Discord credential result = %t / %q", success, classification)
	}
}
