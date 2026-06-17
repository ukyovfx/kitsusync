package log

import (
	"os"
	"path/filepath"
)

// OpenAppendFile ensures the parent directory exists, then opens the file for append.
func OpenAppendFile(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}
