package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/slog"
	"github.com/gookit/slog/handler"
	"github.com/gookit/slog/rotatefile"
)

func TestLogLevelForEnv(t *testing.T) {
	cases := map[string]slog.Level{
		"development": slog.DebugLevel,
		"production":  slog.InfoLevel,
		"staging":     slog.InfoLevel,
		"":            slog.InfoLevel,
	}
	for env, want := range cases {
		if got := logLevelForEnv(env); got != want {
			t.Errorf("logLevelForEnv(%q) = %v, want %v", env, got, want)
		}
	}
}

// Pins the F13 wiring: the file handler must honor the configured level
// (AllLevels would let DEBUG through).
func TestFileLogHandlerFiltersLevel(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	rw, err := rotatefile.NewWriter(&rotatefile.Config{
		Filepath:   logPath,
		MaxSize:    10 * rotatefile.OneMByte,
		BackupNum:  5,
		FilePerm:   rotatefile.DefaultFilePerm,
		RenameFunc: rotatefile.DefaultFilenameFn,
	})
	if err != nil {
		t.Fatalf("create rotate writer: %v", err)
	}
	// The writer is not closed on purpose; see TestLogRotationCreatesBackup.

	logger := slog.New()
	logger.PushHandler(handler.NewSimpleHandler(rw, slog.InfoLevel))

	logger.Debug("debug should be filtered")
	logger.Info("info should be kept")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if strings.Contains(string(data), "debug should be filtered") {
		t.Error("DEBUG line was written to the file at INFO level")
	}
	if !strings.Contains(string(data), "info should be kept") {
		t.Error("INFO line missing from the file")
	}
}

// Rotation must produce a backup file once MaxSize is exceeded. The writer
// is deliberately not closed: gookit/slog v0.5.5's rotatefile races Writer
// .Close against the clean-up goroutine it starts on the first write
// (both touch the stop-channel fields without synchronization), which the
// race detector flags. Leaving the handle open is safe here: writes go
// straight to the file, and the temp dir is removed best-effort.
func TestLogRotationCreatesBackup(t *testing.T) {
	dir, err := os.MkdirTemp("", "kitsusync-log-rotation")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir) // best-effort; the writer stays open on purpose

	logPath := filepath.Join(dir, "test.log")
	rw, err := rotatefile.NewWriter(&rotatefile.Config{
		Filepath:   logPath,
		MaxSize:    1024,
		BackupNum:  5,
		FilePerm:   rotatefile.DefaultFilePerm,
		RenameFunc: rotatefile.DefaultFilenameFn,
	})
	if err != nil {
		t.Fatalf("create rotate writer: %v", err)
	}

	logger := slog.New()
	logger.PushHandler(handler.NewSimpleHandler(rw, slog.InfoLevel))
	line := strings.Repeat("x", 200)
	for i := 0; i < 20; i++ {
		logger.Info(line)
	}

	backups, err := filepath.Glob(logPath + ".*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) == 0 {
		t.Error("no rotated backup file after writing > MaxSize")
	}
}
