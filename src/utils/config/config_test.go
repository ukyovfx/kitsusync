package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFromPathRejectsDirectoryWithSafeDiagnostic(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadFromPath(dir)
	if err == nil || !strings.Contains(err.Error(), "configured path is a directory") {
		t.Fatalf("expected directory diagnostic, got %v", err)
	}
}

func TestReadFromPathExpandsEnvironmentWithoutPersistingValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.toml")
	if err := os.WriteFile(path, []byte("[kitsu]\nhostname = \"${TEST_CONFIG_HOST}\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_CONFIG_HOST", "http://kitsu.invalid")
	loaded, err := ReadFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Kitsu.Hostname != "http://kitsu.invalid" {
		t.Fatalf("expected expanded hostname, got %q", loaded.Kitsu.Hostname)
	}
}

func TestValidateRejectsNonPositiveDiscordBatchAndRateValues(t *testing.T) {
	for _, value := range []int{0, -1} {
		config := Config{}
		config.Discord.EmbedsPerRequests = value
		config.Discord.RequestsPerMinute = value

		issues := config.Validate()
		joined := strings.Join(issues, "\n")
		if !strings.Contains(joined, "[FATAL] discord.embedsPerRequests must be greater than zero") {
			t.Fatalf("value %d: expected batch validation issue, got %v", value, issues)
		}
		if !strings.Contains(joined, "[FATAL] discord.requestsPerMinute must be greater than zero") {
			t.Fatalf("value %d: expected rate validation issue, got %v", value, issues)
		}
	}
}

func TestValidateAcceptsPositiveDiscordBatchAndRateValues(t *testing.T) {
	config := Config{}
	config.Discord.EmbedsPerRequests = 10
	config.Discord.RequestsPerMinute = 30

	for _, issue := range config.Validate() {
		if strings.Contains(issue, "discord.embedsPerRequests") || strings.Contains(issue, "discord.requestsPerMinute") {
			t.Fatalf("positive values should not produce divisor/rate issue: %v", issue)
		}
	}
}

func TestPollIntervalUsesExplicitSecondsWithinSafeRange(t *testing.T) {
	config := Config{}
	config.Kitsu.PollIntervalSeconds = 10
	if got := config.PollIntervalSeconds(); got != 10 || config.PollInterval().Seconds() != 10 {
		t.Fatalf("explicit poll interval = %d/%s, want 10 seconds", got, config.PollInterval())
	}
	for _, value := range []int{1, 61, -1} {
		config := Config{}
		config.Kitsu.PollIntervalSeconds = value
		if len(config.Validate()) == 0 {
			t.Fatalf("poll interval %d should be rejected", value)
		}
	}
}

func TestPollIntervalPreservesLegacyMinuteSemantics(t *testing.T) {
	config := Config{}
	config.Kitsu.RequestInterval = 1
	if got := config.PollIntervalSeconds(); got != 60 || config.PollInterval().Seconds() != 60 {
		t.Fatalf("legacy requestInterval = %d/%s, want 60 seconds", got, config.PollInterval())
	}
}

func TestPollIntervalDefaultsToTenSecondsWhenUnset(t *testing.T) {
	if got := (Config{}).PollIntervalSeconds(); got != 10 {
		t.Fatalf("unset poll interval = %d, want 10 seconds", got)
	}
}
