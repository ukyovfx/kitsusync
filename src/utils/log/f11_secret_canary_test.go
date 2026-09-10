package log

import (
	"bytes"
	"fmt"
	stdlog "log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	debuglog "app/src/utils/debug"

	gookitslog "github.com/gookit/slog"
)

type f11Canaries struct {
	password      string
	runtimeToken  string
	sessionCookie string
	encryptionKey string
	responseBody  string
}

func newF11Canaries(t *testing.T) f11Canaries {
	t.Helper()
	c := f11Canaries{
		password:      "f11-password-canary-7d4b",
		runtimeToken:  "f11-runtime-token-canary-8e5c",
		sessionCookie: "f11-session-cookie-canary-9f6d",
		encryptionKey: "f11-encryption-key-canary-a07e",
		responseBody:  "https://discord.com/api/webhooks/941234567890123456/f11-response-body-canary-b18f",
	}
	t.Setenv("KITSU_PASSWORD", c.password)
	// These use the existing runtime redaction inputs; the values are synthetic
	// and stand in for the corresponding token, cookie, and key classes.
	t.Setenv("KitsuJWTToken", c.runtimeToken)
	t.Setenv("ADMIN_PASSWORD", c.sessionCookie)
	t.Setenv("FB_PASSWORD", c.encryptionKey)
	return c
}

func (c f11Canaries) joined() string {
	return strings.Join([]string{c.password, c.runtimeToken, c.sessionCookie, c.encryptionKey, c.responseBody}, " ")
}

func assertF11CanariesAbsent(t *testing.T, output string, c f11Canaries) {
	t.Helper()
	for _, canary := range []string{c.password, c.runtimeToken, c.sessionCookie, c.encryptionKey, c.responseBody} {
		if strings.Contains(output, canary) {
			t.Fatalf("secret canary leaked in output: %q", canary)
		}
	}
}

func TestF11CaptureHarnessPositiveControl(t *testing.T) {
	c := newF11Canaries(t)
	var raw bytes.Buffer
	if _, err := fmt.Fprint(&raw, c.password); err != nil {
		t.Fatalf("positive control write failed: %v", err)
	}
	if !strings.Contains(raw.String(), c.password) {
		t.Fatal("positive control did not capture the synthetic canary")
	}
}

func TestF11GookitLoggerAndCLIStdoutRedactCanaries(t *testing.T) {
	c := newF11Canaries(t)
	var output bytes.Buffer
	logger := gookitslog.NewSugaredLogger(NewRedactingWriter(&output), gookitslog.DebugLevel)
	logger.Formatter = gookitslog.NewTextFormatter()
	logger.Error("database write failed", "error_class", "database_write_failed", "status", 500, "details", c.joined())
	if _, err := fmt.Fprint(NewRedactingWriter(&output), "cli stdout error_class=http_error "+c.joined()); err != nil {
		t.Fatalf("CLI stdout capture failed: %v", err)
	}
	text := output.String()
	assertF11CanariesAbsent(t, text, c)
	if !strings.Contains(text, "database_write_failed") || !strings.Contains(text, "http_error") {
		t.Fatalf("safe diagnostic metadata was lost: %q", text)
	}
}

func TestF11CLIStderrRedactsCanaries(t *testing.T) {
	c := newF11Canaries(t)
	var output bytes.Buffer
	oldWriter := stdlog.Writer()
	defer stdlog.SetOutput(oldWriter)
	stdlog.SetOutput(NewRedactingWriter(&output))
	stdlog.Printf("cli stderr error_class=database_write_failed details=%s", c.joined())
	text := output.String()
	assertF11CanariesAbsent(t, text, c)
	if !strings.Contains(text, "database_write_failed") {
		t.Fatalf("safe stderr metadata was lost: %q", text)
	}
}

func TestF11DebugLoggingOmitsHTTPResponseContent(t *testing.T) {
	c := newF11Canaries(t)
	confPath := filepath.Join(t.TempDir(), "conf.toml")
	if err := os.WriteFile(confPath, []byte("Debug = true\n"), 0600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	t.Setenv("TEST", "true")
	t.Setenv("CONF_PATH", confPath)

	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(NewRedactingWriter(&output), nil)))
	defer slog.SetDefault(previous)
	resp := &http.Response{StatusCode: http.StatusBadGateway}
	slog.Error("HTTP request rejected", "error_class", "http_error", "status", http.StatusBadGateway, "details", c.joined())
	debuglog.Info(resp, []byte(`{"password":"`+c.password+`","token":"`+c.runtimeToken+`","cookie":"`+c.sessionCookie+`","key":"`+c.encryptionKey+`","body":"`+c.responseBody+`"}`))
	text := output.String()
	assertF11CanariesAbsent(t, text, c)
	if !strings.Contains(text, "debug response metadata") || !strings.Contains(text, "body_bytes") || !strings.Contains(text, "status=502") {
		t.Fatalf("safe debug metadata was not observable: %q", text)
	}
}
