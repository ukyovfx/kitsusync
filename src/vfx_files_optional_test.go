package main

import "testing"

func TestOptionalVFXFilesRuntimeConfigDisablesOnlyInvalidIntegration(t *testing.T) {
	for _, key := range []string{
		"KITSUSYNC_VFX_FILES_API_URL",
		"KITSUSYNC_VFX_FILES_ISSUER",
		"KITSUSYNC_VFX_FILES_SUBJECT",
		"KITSUSYNC_VFX_FILES_CLIENT_ID",
		"KITSUSYNC_VFX_FILES_KEY_ID",
		"KITSUSYNC_VFX_FILES_SIGNING_KEY_PATH",
		"KITSUSYNC_VFX_FILES_JWKS_DIR",
		"KITSUSYNC_VFX_FILES_EVENT_HMAC_KEYS",
	} {
		t.Setenv(key, "")
	}

	config, invalid := loadOptionalVFXFilesRuntimeConfig(nil, nil)
	if config != nil || invalid {
		t.Fatalf("absent optional integration config = (%v, %v), want (nil, false)", config, invalid)
	}

	t.Setenv("KITSUSYNC_VFX_FILES_API_URL", "https://files.example.invalid/api/plugins/vfx_files/integrations/v1")
	config, invalid = loadOptionalVFXFilesRuntimeConfig(nil, nil)
	if config != nil || !invalid {
		t.Fatalf("partial optional integration config = (%v, %v), want (nil, true)", config, invalid)
	}
}
