//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// secureRuntimeDataFiles sets the process umask before SQLite opens the
// database and repairs only known sensitive runtime files left by an older
// process. It deliberately does not chmod the data directory or unrelated
// files in a bind mount.
func secureRuntimeDataFiles(dataDir string) error {
	syscall.Umask(0077)
	for _, name := range []string{"sqlite.db", "sqlite.db-wal", "sqlite.db-shm", "runtime-secret.key"} {
		path := filepath.Join(dataDir, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect runtime file %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("runtime file %s is not a regular file", name)
		}
		if info.Mode().Perm()&0077 != 0 {
			if err := os.Chmod(path, 0600); err != nil {
				return fmt.Errorf("tighten runtime file %s: %w", name, err)
			}
		}
	}
	return nil
}
