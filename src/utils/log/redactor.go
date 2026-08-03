package log

import (
	"io"
	"os"
	"regexp"
	"strings"
)

// Compiled regexps at package level to avoid recompilation on each call
var (
	botTokenPattern = regexp.MustCompile(`MT[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	webhookPattern  = regexp.MustCompile(`webhooks/[0-9]+/[A-Za-z0-9_-]+`)
	jwtPattern      = regexp.MustCompile(`[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	// Checked on every call because this app sets these via os.Setenv() at runtime
	secretEnvVars = []string{
		"KITSU_RUNTIME_PASSWORD",
		"KITSU_PASSWORD",
		"KitsuJWTToken",
		"DISCORD_BOT_TOKEN",
		"DISCORD_WEBHOOK_URL",
		"FB_PASSWORD",
		"ADMIN_PASSWORD",
	}
)

// RedactSecrets replaces sensitive information with [REDACTED]
func RedactSecrets(message string) string {
	if message == "" {
		return message
	}

	// Discord Bot Token pattern: MTxxxxxx.Gxxxx...
	message = botTokenPattern.ReplaceAllString(message, "[REDACTED-TOKEN]")

	// Discord Webhook URL pattern: https://discord.com/api/webhooks/xxx/xxx
	message = webhookPattern.ReplaceAllString(message, "webhooks/[REDACTED]/[REDACTED]")
	message = jwtPattern.ReplaceAllString(message, "[REDACTED-JWT]")

	// Environment variable values — read at call time because os.Setenv() may update these at runtime
	for _, envVar := range secretEnvVars {
		if val := os.Getenv(envVar); val != "" && len(val) > 3 {
			message = strings.ReplaceAll(message, val, "[REDACTED]")
		}
	}

	return message
}

// redactingWriter wraps io.Writer and redacts secrets before writing
type redactingWriter struct {
	underlying io.Writer
}

// Write implements io.Writer interface: redact secrets then write.
// Returns len(p) on success so callers see the full original byte count.
func (rw *redactingWriter) Write(p []byte) (int, error) {
	redacted := []byte(RedactSecrets(string(p)))
	written := 0

	for written < len(redacted) {
		n, err := rw.underlying.Write(redacted[written:])
		if n > 0 {
			written += n
		}

		if err != nil {
			if written > 0 {
				// Some output is already visible; report the original input length to avoid callers retrying the whole line.
				return len(p), err
			}
			return 0, err
		}

		if n == 0 {
			if written > 0 {
				return len(p), io.ErrShortWrite
			}
			return 0, io.ErrShortWrite
		}
	}

	return len(p), nil
}

// NewRedactingWriter wraps an io.Writer to redact secrets on Write
func NewRedactingWriter(w io.Writer) io.Writer {
	return &redactingWriter{underlying: w}
}
