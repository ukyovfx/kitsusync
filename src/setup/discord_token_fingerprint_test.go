package setup

import "testing"

func TestDiscordBotTokenFingerprintIsStableAndSecretSafe(t *testing.T) {
	if got := DiscordBotTokenFingerprint(""); got != "" {
		t.Fatalf("empty token fingerprint = %q", got)
	}
	first := DiscordBotTokenFingerprint("token-one")
	if len(first) != 12 {
		t.Fatalf("fingerprint length = %d, want 12", len(first))
	}
	if first != DiscordBotTokenFingerprint(" token-one ") {
		t.Fatal("fingerprint should trim token whitespace")
	}
	if first == DiscordBotTokenFingerprint("token-two") {
		t.Fatal("different tokens should not share the test fingerprint")
	}
}
