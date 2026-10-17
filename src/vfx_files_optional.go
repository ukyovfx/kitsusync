package main

import (
	"net/http"

	"app/src/api/vfxfiles"
)

// loadOptionalVFXFilesRuntimeConfig keeps malformed optional integration
// settings from taking down KitsuSync. The caller must leave the Plugin client,
// event trust, and background delivery processor disabled when invalid is true.
func loadOptionalVFXFilesRuntimeConfig(getenv func(string) string, client *http.Client) (*vfxfiles.RuntimeConfig, bool) {
	config, err := vfxfiles.LoadRuntimeConfig(getenv, client)
	return config, err != nil
}
