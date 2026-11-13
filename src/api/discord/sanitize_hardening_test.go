package discord

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSanitizeDiscordTextStripsControlAndDirectionalChars reproduces the
// pentest finding: Unicode control characters such as U+202E (RTL override)
// passed into Discord embeds untouched and could visually disguise content.
func TestSanitizeDiscordTextStripsControlAndDirectionalChars(t *testing.T) {
	if got := sanitizeDiscordText("task\u202eRLO\u200bZWSP\u202bLRE"); got != "taskRLOZWSPLRE" {
		t.Fatalf("sanitizeDiscordText = %q, want %q", got, "taskRLOZWSPLRE")
	}
	// Newlines and tabs survive: multi-line comment text stays readable.
	if got := sanitizeDiscordText("line1\nline2\tcol"); got != "line1\nline2\tcol" {
		t.Fatalf("legitimate whitespace was stripped: %q", got)
	}
}

// TestSanitizeDiscordTextNeutralizesMaskedLinks reproduces the pentest
// finding: [text](url) markdown links passed into embeds as clickable masked
// links whose visible text can hide the real target.
func TestSanitizeDiscordTextNeutralizesMaskedLinks(t *testing.T) {
	got := sanitizeDiscordText("See [the safe site](https://evil.example/steal)")
	want := "See the safe site (https://evil.example/steal)"
	if got != want {
		t.Fatalf("sanitizeDiscordText = %q, want %q", got, want)
	}
	// The mention-safe replacements keep working.
	if got := sanitizeDiscordText("@everyone <script>"); got != "@ everyone ＜script＞" {
		t.Fatalf("legacy sanitization changed: %q", got)
	}
}

// TestSendMessageSkipsInlineRetryWhileRateLimitGateActive reproduces the
// deferred Codex P2 finding: a rate-limited destination stalled the whole
// serial batch with inline retry sleeps. While the gate is active, a send
// that draws a 429 must return retryable immediately (one POST, no retry
// sleep) so the durable ledger resumes it on the next poll.
func TestSendMessageSkipsInlineRetryWhileRateLimitGateActive(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0.1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	previous := discordRateLimitedUntil.Load()
	discordRateLimitedUntil.Store(time.Now().Add(time.Minute).UnixNano())
	defer discordRateLimitedUntil.Store(previous)

	result := SendMessage(Payload{Content: "x"}, server.URL+"/webhook", "", "")
	if !result.Retryable || result.FailureCategory != "rate_limited" {
		t.Fatalf("gated send = %#v, want retryable rate_limited", result)
	}
	if attempts != 1 {
		t.Fatalf("gated send made %d POSTs, want 1 (no inline retry while gated)", attempts)
	}

	dmAttempts := 0
	dmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dmAttempts++
		w.Header().Set("Retry-After", "0.1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer dmServer.Close()
	oldAPI := SetDiscordBotAPIBase(dmServer.URL)
	defer SetDiscordBotAPIBase(oldAPI)

	dmResult := SendDirectMessage("bot-token", "channel-1", Payload{Content: "x"})
	if !dmResult.Retryable || dmResult.FailureCategory != "rate_limited" {
		t.Fatalf("gated DM send = %#v, want retryable rate_limited", dmResult)
	}
	if dmAttempts != 1 {
		t.Fatalf("gated DM send made %d POSTs, want 1", dmAttempts)
	}
}

// TestMarkDiscordRateLimitedExpires verifies the gate clears itself so a
// rate-limit window cannot outlive its Retry-After.
func TestMarkDiscordRateLimitedExpires(t *testing.T) {
	previous := discordRateLimitedUntil.Load()
	defer discordRateLimitedUntil.Store(previous)

	discordRateLimitedUntil.Store(0)
	markDiscordRateLimited(50 * time.Millisecond)
	if !discordRateLimitGateActive() {
		t.Fatal("gate should be active right after marking")
	}
	deadline := time.Now().Add(2 * time.Second)
	for discordRateLimitGateActive() {
		if time.Now().After(deadline) {
			t.Fatal("gate did not expire")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
