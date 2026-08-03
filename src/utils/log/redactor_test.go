package log

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	t.Setenv("KITSU_RUNTIME_PASSWORD", "super-secret-runtime-password")
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/123456/token-abc")
	t.Setenv("DISCORD_BOT_TOKEN", "bot-token-value")

	message := strings.Join([]string{
		"password=super-secret-runtime-password",
		"webhook=https://discord.com/api/webhooks/123456/token-abc",
		"token=MTabc_DEF.ghiJKL",
		"bot=bot-token-value",
		"jwt=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJydW50aW1lIn0.signature123",
	}, " | ")

	redacted := RedactSecrets(message)

	for _, secret := range []string{
		"super-secret-runtime-password",
		"token-abc",
		"MTabc_DEF.ghiJKL",
		"bot-token-value",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJydW50aW1lIn0.signature123",
	} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q was not redacted: %s", secret, redacted)
		}
	}

	if !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("expected generic redaction marker, got %s", redacted)
	}
	if !strings.Contains(redacted, "[REDACTED-TOKEN]") {
		t.Fatalf("expected token redaction marker, got %s", redacted)
	}
	if !strings.Contains(redacted, "webhooks/[REDACTED]/[REDACTED]") {
		t.Fatalf("expected webhook redaction marker, got %s", redacted)
	}
	if !strings.Contains(redacted, "[REDACTED-JWT]") {
		t.Fatalf("expected JWT redaction marker, got %s", redacted)
	}
}
