package setup

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	logutil "app/src/utils/log"

	"github.com/gookit/slog"
)

func TestF11DatabaseFailureUsesSafeErrorClass(t *testing.T) {
	const canary = "f11-gorm-database-canary-4c2a"
	var output bytes.Buffer
	logger := slog.NewSugaredLogger(logutil.NewRedactingWriter(&output), slog.DebugLevel)

	databaseErr := fmt.Errorf("database is locked: %s", canary)
	logger.Error("database write failed", "error_class", classifySQLitePersistenceError(databaseErr), "status", http.StatusInternalServerError)
	text := output.String()
	if strings.Contains(text, canary) {
		t.Fatalf("database canary leaked into log output: %q", text)
	}
	if !strings.Contains(text, "error_class") || !strings.Contains(text, "database_busy") {
		t.Fatalf("safe database error class was not observable: %q", text)
	}
}

func TestF11HTTPErrorResponseDoesNotEchoCredentialBearingBody(t *testing.T) {
	canaries := []string{
		"f11-password-http-canary-5d3b",
		"f11-runtime-token-http-canary-6e4c",
		"f11-session-http-canary-7f5d",
		"f11-key-http-canary-8a6e",
		"f11-response-http-canary-9b7f",
	}
	requestBody := fmt.Sprintf(`{"password":%q,"token":%q,"cookie":%q,"key":%q,"body":%q}`,
		canaries[0], canaries[1], canaries[2], canaries[3], canaries[4])
	req := httptest.NewRequest(http.MethodPost, "/api/setup/test-kitsu", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	TestKitsuHandler(nil)(rr, req)
	text := rr.Body.String()
	for _, canary := range canaries {
		if strings.Contains(text, canary) {
			t.Fatalf("HTTP error response echoed canary %q: %q", canary, text)
		}
	}
	if !strings.Contains(text, "hostname(auto), email, and password are required") {
		t.Fatalf("safe HTTP error was not observable: %q", text)
	}
}

func TestF11SafeHTTPErrorLogKeepsMetadata(t *testing.T) {
	const canary = "f11-http-log-canary-a08c"
	t.Setenv("ADMIN_PASSWORD", canary)
	var output bytes.Buffer
	logger := slog.NewSugaredLogger(logutil.NewRedactingWriter(&output), slog.DebugLevel)
	logger.Warn("HTTP request rejected", "error_class", "http_error", "status", http.StatusBadGateway, "body_bytes", 128, "detail", canary)
	text := output.String()
	if strings.Contains(text, canary) {
		t.Fatalf("HTTP log canary leaked: %q", text)
	}
	if !strings.Contains(text, "http_error") || !strings.Contains(text, "502") || !strings.Contains(text, "body_bytes") {
		t.Fatalf("safe HTTP metadata was not observable: %q", text)
	}
}
