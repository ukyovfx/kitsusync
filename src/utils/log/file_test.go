package log

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAppendFile_CreatesMissingParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "all-levels.log")

	f, err := OpenAppendFile(path)
	if err != nil {
		t.Fatalf("OpenAppendFile returned error: %v", err)
	}
	f.Close()

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("expected parent directory to exist: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected log file to exist: %v", err)
	}
}

func TestOpenAppendFile_PreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "all-levels.log")

	first, err := OpenAppendFile(path)
	if err != nil {
		t.Fatalf("OpenAppendFile returned error: %v", err)
	}
	if _, err := first.WriteString("line one\n"); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	first.Close()

	second, err := OpenAppendFile(path)
	if err != nil {
		t.Fatalf("OpenAppendFile returned error on reopen: %v", err)
	}
	if _, err := second.WriteString("line two\n"); err != nil {
		t.Fatalf("WriteString returned error on reopen: %v", err)
	}
	second.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "line one\nline two\n" {
		t.Fatalf("expected appended file contents, got %q", string(data))
	}
}
