package main

import (
	"encoding/json"
	"testing"
)

func TestBuildInfoIsNonSecretAndSerializable(t *testing.T) {
	old := []string{BuildVersion, BuildCommit, BuildTimestamp, BuildSchema}
	BuildVersion, BuildCommit, BuildTimestamp, BuildSchema = "v-test", "abc123", "2026-08-04T00:00:00Z", "7"
	t.Cleanup(func() { BuildVersion, BuildCommit, BuildTimestamp, BuildSchema = old[0], old[1], old[2], old[3] })
	encoded, err := json.Marshal(currentBuildInfo())
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secretLike := range []string{"password", "token", "webhook", "KITSU_HOSTNAME"} {
		if containsFold(text, secretLike) {
			t.Fatalf("build identity exposed secret-like field %q: %s", secretLike, text)
		}
	}
	if !containsFold(text, "schema_version") || !containsFold(text, "abc123") {
		t.Fatalf("build identity fields missing: %s", text)
	}
}

func containsFold(value, needle string) bool {
	return len(value) >= len(needle) && stringContainsFold(value, needle)
}

func stringContainsFold(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		match := true
		for j := range needle {
			if lowerASCII(value[i+j]) != lowerASCII(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func lowerASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}
