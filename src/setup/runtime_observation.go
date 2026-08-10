package setup

import (
	"net/http"
	"strings"
	"time"
)

// ObserveKitsuRuntime records one bounded read-only authenticated health observation.
func ObserveKitsuRuntime(hostname, token string) {
	started := time.Now()
	success := false
	classification := "not_configured"
	if strings.TrimSpace(hostname) != "" && strings.TrimSpace(token) != "" {
		request, err := http.NewRequest(http.MethodGet, strings.TrimRight(hostname, "/")+"/api/auth/authenticated", nil)
		if err != nil {
			classification = "request_build_failed"
		} else {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Accept", "application/json")
			response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
			if err != nil {
				classification = "network_error"
			} else {
				response.Body.Close()
				switch response.StatusCode {
				case http.StatusOK:
					success, classification = true, "success"
				case http.StatusUnauthorized:
					classification = "authentication_error"
				case http.StatusForbidden:
					classification = "permission_error"
				default:
					classification = "http_error"
				}
			}
		}
	}
	Stats.RecordAPIObservation("kitsu", started, success, classification)
}

// ObserveDiscordRuntime records one bounded read-only bot identity observation.
func ObserveDiscordRuntime(token string) {
	if strings.TrimSpace(token) == "" {
		Stats.RecordAPIObservation("discord", time.Now(), false, "not_configured")
		return
	}
	checkDiscordStatus(token, "")
}
