//go:build windows

package main

// Windows does not expose Unix mode bits or a process umask. The container
// runtime is Linux; keep the startup hook portable for local Windows builds.
func secureRuntimeDataFiles(string) error { return nil }
