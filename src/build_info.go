package main

import "strings"

var (
	BuildVersion       = "0.4.5"
	BuildCommit        = "unknown"
	BuildTimestamp     = "unknown"
	BuildSchema        = "1"
	BuildWorktreeDirty = "false"
	BuildSourceID      = "unknown"
	BuildImageRevision = "unknown"
)

type buildInfo struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`    // Backward-compatible alias for base_commit.
	Timestamp     string `json:"timestamp"` // Backward-compatible alias for build_time.
	Schema        string `json:"schema_version"`
	BaseCommit    string `json:"base_commit"`
	WorktreeDirty bool   `json:"worktree_dirty"`
	BuildSourceID string `json:"build_source_id"`
	BuildTime     string `json:"build_time"`
	ImageRevision string `json:"image_revision"`
}

func currentBuildInfo() buildInfo {
	commit := safeBuildValue(BuildCommit, "unknown")
	timestamp := safeBuildValue(BuildTimestamp, "unknown")
	return buildInfo{
		Version:       BuildVersion,
		Commit:        commit,
		Timestamp:     timestamp,
		Schema:        safeBuildValue(BuildSchema, "1"),
		BaseCommit:    commit,
		WorktreeDirty: strings.EqualFold(strings.TrimSpace(BuildWorktreeDirty), "true"),
		BuildSourceID: safeBuildValue(BuildSourceID, "unknown"),
		BuildTime:     timestamp,
		ImageRevision: safeBuildValue(BuildImageRevision, commit),
	}
}

func safeBuildValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
