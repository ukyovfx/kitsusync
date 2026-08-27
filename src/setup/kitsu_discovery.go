package setup

import (
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
	Source      string
}

type kitsuHostProbe struct {
	RuntimeHost string
	DisplayHost string
}

func KitsuHostForUI(db *gorm.DB) string {
	if discovered := DiscoverKitsuHost(db); discovered.RuntimeHost != "" {
		return discovered.RuntimeHost
	}
	return ""
}

var (
	discoveryMu       sync.Mutex
	discoveryAt       time.Time
	discoveryResult   KitsuHostDiscoveryResult
	discoveryCacheKey string
	discoveryInterval = 30 * time.Second
)

// DiscoverKitsuHost validates the explicit endpoint first, then a saved
// endpoint, then the small set of known local deployment endpoints. It never
// scans arbitrary hosts or returns a placeholder as a usable endpoint.
func DiscoverKitsuHost(db *gorm.DB) KitsuHostDiscoveryResult {
	if configured := strings.TrimSpace(osKitsuHostname()); configured != "" {
		if normalized, err := validateKitsuEndpoint(configured); err == nil && !isPlaceholderKitsuEndpoint(normalized) && probeKitsu(normalized) {
			return KitsuHostDiscoveryResult{RuntimeHost: normalized, DisplayHost: safeKitsuHostDisplay(normalized), Status: authenticatedKitsu, Source: "explicit"}
		}
	}
	if db != nil {
		if saved := strings.TrimSpace(model.GetSetting(db, "kitsu.hostname")); saved != "" {
			if normalized, err := validateKitsuEndpoint(saved); err == nil && !isPlaceholderKitsuEndpoint(normalized) && probeKitsu(normalized) {
				return KitsuHostDiscoveryResult{RuntimeHost: normalized, DisplayHost: safeKitsuHostDisplay(normalized), Status: authenticatedKitsu, Source: "persisted"}
			}
		}
	}

	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	candidates := discoveryCandidates()
	cacheKey := discoveryCacheFingerprint(candidates)
	if time.Since(discoveryAt) < discoveryInterval && discoveryCacheKey == cacheKey {
		return discoveryResult
	}
	discoveryAt = time.Now()
	discoveryCacheKey = cacheKey
	discoveryResult = KitsuHostDiscoveryResult{}
	var found []kitsuHostProbe
	for _, candidate := range candidates {
		if normalized, err := validateKitsuEndpoint(candidate.RuntimeHost); err == nil && !isPlaceholderKitsuEndpoint(normalized) && probeKitsu(normalized) {
			found = append(found, candidate)
		}
	}
	if len(found) == 1 {
		discoveryResult = KitsuHostDiscoveryResult{
			RuntimeHost: found[0].RuntimeHost,
			DisplayHost: found[0].DisplayHost,
			Status:      hostReachableKitsu,
			Source:      "local-discovered",
		}
	}
	return discoveryResult
}

func osKitsuHostname() string {
	return strings.TrimSpace(os.Getenv("KITSU_HOSTNAME"))
}

func isPlaceholderKitsuEndpoint(raw string) bool {
	host := strings.ToLower(strings.TrimSpace(raw))
	return host == "http://your_kitsu_host" || host == "https://your_kitsu_host" ||
		host == "http://your_kitsu_host/" || host == "https://your_kitsu_host/"
}

func localKitsuHostCandidates() []kitsuHostProbe {
	return []kitsuHostProbe{
		{RuntimeHost: "http://host.docker.internal:8080/", DisplayHost: "http://127.0.0.1:8080"},
		{RuntimeHost: "http://host.docker.internal/", DisplayHost: "http://127.0.0.1"},
	}
}

var discoveryCandidates = localKitsuHostCandidates

func discoveryCacheFingerprint(candidates []kitsuHostProbe) string {
	parts := []string{
		strings.TrimSpace(os.Getenv(localProfileEnv)),
		strings.TrimSpace(os.Getenv("KITSUSYNC_LOCAL_KITSU_HOST")),
	}
	for _, candidate := range candidates {
		parts = append(parts, candidate.RuntimeHost, candidate.DisplayHost)
	}
	return strings.Join(parts, "\x00")
}

func probeKitsu(host string) bool {
	client := &http.Client{Timeout: 750 * time.Millisecond}
	for _, endpoint := range []string{"/api/", "/api/auth/authenticated", "/api/data/projects/"} {
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(host, "/")+endpoint, nil)
		if err != nil {
			return false
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		if endpoint == "/api/" && resp.StatusCode != http.StatusOK {
			return false
		}
		if endpoint != "/api/" && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return false
		}
	}
	return true
}
