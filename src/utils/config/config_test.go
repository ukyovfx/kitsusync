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
