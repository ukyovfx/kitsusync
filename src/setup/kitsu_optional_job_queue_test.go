package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeKitsuZouAllowsOptionalJobQueueDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
"name":"Zou",
"version":"1.0.67",
"database-up":true,
"key-value-store-up":true,
"event-stream-up":true,
"job-queue-up":false,
"indexer-up":false
}`))
	}))
	defer server.Close()

	model, err := NormalizeKitsuURL(server.URL, APISourceExplicit)
	if err != nil {
		t.Fatal(err)
	}

	if err := ProbeKitsuZou(context.Background(), model); err != nil {
		t.Fatalf("healthy Zou rejected because optional services are down: %v", err)
	}
}
