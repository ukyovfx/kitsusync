package main

import "strings"

var (
	BuildVersion   = "dev"
	BuildCommit    = "unknown"
	BuildTimestamp = "unknown"
	BuildSchema    = "1"
)

type buildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Timestamp string `json:"timestamp"`
	Schema    string `json:"schema_version"`
}

func currentBuildInfo() buildInfo {
	return buildInfo{safeBuildValue(BuildVersion, "dev"), safeBuildValue(BuildCommit, "unknown"), safeBuildValue(BuildTimestamp, "unknown"), safeBuildValue(BuildSchema, "1")}
}

func safeBuildValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
