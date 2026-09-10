package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func requireUnixPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode bits and umask are not available on Windows")
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func TestSecureRuntimeDataFilesRestrictsNewDatabaseFiles(t *testing.T) {
	requireUnixPermissions(t)
	dir := t.TempDir()
	if err := secureRuntimeDataFiles(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sqlite.db", "sqlite.db-wal", "sqlite.db-shm"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("runtime"), 0666); err != nil {
			t.Fatal(err)
		}
		if got := filePerm(t, path); got != 0600 {
			t.Fatalf("%s mode = %04o, want 0600", name, got)
		}
	}
}

func TestSecureRuntimeDataFilesRestrictsSQLiteWALAndSHM(t *testing.T) {
	requireUnixPermissions(t)
	dir := t.TempDir()
	if err := secureRuntimeDataFiles(dir); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "sqlite.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE permissions_probe (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO permissions_probe(value) VALUES ('probe')").Error; err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := dbPath + suffix
		if got := filePerm(t, path); got != 0600 {
			t.Fatalf("%s mode = %04o, want 0600", suffix, got)
		}
	}
}

func TestSecureRuntimeDataFilesTightensExistingFilesAndSupportsReopen(t *testing.T) {
	requireUnixPermissions(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sqlite.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE restart_probe (value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0644); err != nil {
		t.Fatal(err)
	}
	if err := secureRuntimeDataFiles(dir); err != nil {
		t.Fatal(err)
	}
	if got := filePerm(t, dbPath); got != 0600 {
		t.Fatalf("tightened database mode = %04o, want 0600", got)
	}
	db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err = db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSecureRuntimeDataFilesFailsSafelyForUnsafeExistingPath(t *testing.T) {
	requireUnixPermissions(t)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "sqlite.db")); err != nil {
		t.Fatal(err)
	}
	if err := secureRuntimeDataFiles(dir); err == nil {
		t.Fatal("unsafe symlink database path should be rejected")
	}
}
