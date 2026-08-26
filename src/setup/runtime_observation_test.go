package setup

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTelemetrySnapshotHandlerIsReadOnlyAndSecretSafe(t *testing.T) {
	Stats.mu.Lock()
	Stats.apiObservations = map[string][]APIObservation{}
	Stats.mu.Unlock()
	Stats.RecordAPIObservation("kitsu", time.Now(), true, "success")
	req := httptest.NewRequest("GET", "/bot/api/setup/observability?window=60s", nil)
	res := httptest.NewRecorder()
	TelemetrySnapshotHandler()(res, req)
	if res.Code != 200 || res.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("snapshot response = %d / %q", res.Code, res.Header().Get("Content-Type"))
	}
	body := res.Body.String()
	if !strings.Contains(body, `"observations"`) || strings.Contains(body, "Authorization") || strings.Contains(body, "Bearer") {
		t.Fatal("snapshot is not a redacted observation payload")
	}
}
