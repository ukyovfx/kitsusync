package setup

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"app/src/model"
	"gorm.io/gorm"
)

const (
	hostReachableKitsu = "HOST_REACHABLE_KITSU"
	authenticatedKitsu = "AUTHENTICATED_KITSU"
)

type KitsuHostDiscoveryResult struct {
	RuntimeHost string
	DisplayHost string
	Status      string
}

type kitsuHostProbe struct {
	RuntimeHost string
	DisplayHost string
}

func KitsuHostForUI(db *gorm.DB) string {
	if db != nil {
		if saved := strings.TrimSpace(model.GetSetting(db, "kitsu.hostname")); saved != "" {
			return saved
		}
	}
	if configured := strings.TrimSpace(os.Getenv("KITSU_HOSTNAME")); configured != "" {
		return configured
	}
	if discovered := DiscoverKitsuHost(db); discovered.RuntimeHost != "" {
		return discovered.RuntimeHost
	}
	return LocalDevelopmentKitsuHostname()
}

var (
	discoveryMu       sync.Mutex
	discoveryAt       time.Time
	discoveryResult   KitsuHostDiscoveryResult
	discoveryInterval = 30 * time.Second
)

// DiscoverKitsuHost uses saved/configured values first. Only an explicit local
// development profile enables the small bounded probe set.
func DiscoverKitsuHost(db *gorm.DB) KitsuHostDiscoveryResult {
	if db != nil {
		if saved := strings.TrimSpace(model.GetSetting(db, "kitsu.hostname")); saved != "" {
			if normalized, err := validateKitsuEndpoint(saved); err == nil {
				return KitsuHostDiscoveryResult{RuntimeHost: normalized, DisplayHost: safeKitsuHostDisplay(normalized), Status: authenticatedKitsu}
			}
		}
	}
	if configured := strings.TrimSpace(osKitsuHostname()); configured != "" {
		if normalized, err := validateKitsuEndpoint(configured); err == nil {
			return KitsuHostDiscoveryResult{RuntimeHost: normalized, DisplayHost: safeKitsuHostDisplay(normalized), Status: authenticatedKitsu}
		}
	}
	if strings.TrimSpace(LocalDevelopmentKitsuHostname()) == "" {
		return KitsuHostDiscoveryResult{}
	}

	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if time.Since(discoveryAt) < discoveryInterval {
		return discoveryResult
	}
	discoveryAt = time.Now()
	discoveryResult = KitsuHostDiscoveryResult{}
	var found []kitsuHostProbe
	for _, candidate := range discoveryCandidates() {
		if probeKitsu(candidate.RuntimeHost) {
			found = append(found, candidate)
		}
	}
	if len(found) == 1 {
		discoveryResult = KitsuHostDiscoveryResult{
			RuntimeHost: found[0].RuntimeHost,
			DisplayHost: found[0].DisplayHost,
			Status:      hostReachableKitsu,
		}
	}
	return discoveryResult
}

func osKitsuHostname() string {
	return strings.TrimSpace(os.Getenv("KITSU_HOSTNAME"))
}

func localKitsuHostCandidates() []kitsuHostProbe {
	return []kitsuHostProbe{
		{RuntimeHost: "http://host.docker.internal:8080/", DisplayHost: "http://127.0.0.1:8080"},
		{RuntimeHost: "http://host.docker.internal/", DisplayHost: "http://127.0.0.1"},
	}
}

var discoveryCandidates = localKitsuHostCandidates

func probeKitsu(host string) bool {
	client := &http.Client{Timeout: 750 * time.Millisecond}
	for _, endpoint := range []string{"/api/auth/authenticated", "/api/data/projects/"} {
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(host, "/")+endpoint, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		var marker struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&marker)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return false
		}
	}
	return true
}
