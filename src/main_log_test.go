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
// (AllLevels would let DEBUG through) and rotate the file when MaxSize is hit.
func TestFileLogHandlerFiltersLevelAndRotates(t *testing.T) {
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
	defer rw.Close()

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

	// Force a size rotation: 10MB + 1 more write must produce a backup file.
	big := strings.Repeat("x", 1024*1024)
	for i := 0; i < 11; i++ {
		logger.Info(big)
	}

	backups, err := filepath.Glob(logPath + ".*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) == 0 {
		t.Error("no rotated backup file after writing > MaxSize")
	}
}
