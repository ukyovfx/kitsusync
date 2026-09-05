package setup

// This file defines the deliberately small, non-discovering boundary used for
// user supplied Kitsu endpoints. It must stay separate from persistence: a
// successful probe never changes settings.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	APISourceDerived       = "derived"
	APISourceExplicit      = "explicit"
	APISourceInstallerHint = "installer_hint"
	APISourceLegacy        = "legacy"
	maxKitsuCandidates     = 4
)

type KitsuConnectionError struct{ Class string }

func (e *KitsuConnectionError) Error() string { return e.Class }
func connectionError(class string) error      { return &KitsuConnectionError{Class: class} }
func connectionErrorClass(err error) string {
	var e *KitsuConnectionError
	if errors.As(err, &e) {
		return e.Class
	}
	return "network_failed"
}

// KitsuURLModel keeps browser-facing, runtime, and API addresses distinct.
type KitsuURLModel struct {
	DisplayBaseURL     string
	RuntimeBaseURL     string
	ResolvedAPIBaseURL string
	APISource          string
	TargetScope        string
	VerifiedIPs        []netip.Addr
}

var kitsuLookupNetIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func NormalizeKitsuURL(raw, source string) (KitsuURLModel, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return KitsuURLModel{}, connectionError("url_parse_invalid")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return KitsuURLModel{}, connectionError("url_parse_invalid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return KitsuURLModel{}, connectionError("scheme_not_allowed")
	}
	if u.User != nil {
		return KitsuURLModel{}, connectionError("embedded_credentials_rejected")
	}
	if u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(u.Host, "\\@") {
		return KitsuURLModel{}, connectionError("url_parse_invalid")
	}
	if isMetadataHost(u.Hostname()) {
		return KitsuURLModel{}, connectionError("forbidden_metadata_target")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if strings.HasSuffix(path, "/api/auth/login") {
		path = strings.TrimSuffix(path, "/auth/login")
	}
	apiPath := path
	if !strings.HasSuffix(apiPath, "/api") {
		apiPath += "/api"
	}
	displayPath := strings.TrimSuffix(apiPath, "/api")
	u.Path, u.RawPath = displayPath, ""
	display := strings.TrimRight(u.String(), "/")
	u.Path = apiPath
	api := strings.TrimRight(u.String(), "/")
	if source == "" {
		source = APISourceDerived
	}
	return KitsuURLModel{DisplayBaseURL: display, RuntimeBaseURL: display, ResolvedAPIBaseURL: api, APISource: source, TargetScope: classifyKitsuHost(u.Hostname())}, nil
}

func isMetadataHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "metadata.google.internal" || host == "metadata" || host == "169.254.169.254"
}

func classifyKitsuHost(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "host.docker.internal" {
		return "docker_host"
	}
	if host == "localhost" {
		return "loopback"
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return classifyKitsuIP(ip)
	}
	return "public"
}
func classifyKitsuIP(ip netip.Addr) string {
	if ip.IsLoopback() {
		return "loopback"
	}
	if ip.IsPrivate() {
		return "private_explicit"
	}
	if ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		return "special"
	}
	if ip.Is4() && ip.As4()[0] == 100 && ip.As4()[1]&0xc0 == 0x40 {
		return "vpn/shared"
	}
	return "public"
}

func resolveKitsuIPs(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addresses, err := kitsuLookupNetIP(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, connectionError("dns_resolution_failed")
	}
	for _, ip := range addresses {
		if isMetadataHost(ip.String()) || classifyKitsuIP(ip) == "special" {
			return nil, connectionError("forbidden_metadata_target")
		}
	}
	return addresses, nil
}

func resolvedKitsuScope(ips []netip.Addr) (string, error) {
	if len(ips) == 0 {
		return "", connectionError("dns_resolution_failed")
	}
	scope := classifyKitsuIP(ips[0])
	if scope == "special" {
		return "", connectionError("forbidden_metadata_target")
	}
	for _, ip := range ips[1:] {
		other := classifyKitsuIP(ip)
		if other == "special" {
			return "", connectionError("forbidden_metadata_target")
		}
		if other != scope {
			return "", connectionError("dns_scope_mixed")
		}
	}
	return scope, nil
}

func sameKitsuIPs(a, b []netip.Addr) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[netip.Addr]struct{}, len(a))
	for _, ip := range a {
		seen[ip] = struct{}{}
	}
	for _, ip := range b {
		if _, ok := seen[ip]; !ok {
			return false
		}
	}
	return true
}

func safeKitsuClient(model KitsuURLModel, pinned []netip.Addr) *http.Client {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips := pinned
		if len(ips) == 0 {
			ips, err = resolveKitsuIPs(ctx, host)
			if err != nil {
				return nil, err
			}
		}
		for _, ip := range ips {
			if model.TargetScope == "public" && classifyKitsuIP(ip) != "public" {
				return nil, connectionError("dns_scope_changed")
			}
			if classifyKitsuIP(ip) == "special" {
				return nil, connectionError("forbidden_metadata_target")
			}
		}
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	// Direct dials are intentional for user-selected private and container
	// targets. Public proxy support is opt-in and is not enabled by setup.
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func ensureKitsuTargetStable(ctx context.Context, model KitsuURLModel) error {
	if len(model.VerifiedIPs) == 0 {
		return connectionError("dns_resolution_failed")
	}
	u, err := url.Parse(model.ResolvedAPIBaseURL)
	if err != nil {
		return connectionError("url_parse_invalid")
	}
	ips, err := resolveKitsuIPs(ctx, u.Hostname())
	if err != nil {
		return err
	}
	if !sameKitsuIPs(ips, model.VerifiedIPs) {
		return connectionError("dns_scope_changed")
	}
	return nil
}

// ProbeKitsuZou verifies identity and health before any credential POST.
func ProbeKitsuZou(ctx context.Context, model KitsuURLModel) error {
	_, err := verifyKitsuZou(ctx, model)
	return err
}

func verifyKitsuZou(ctx context.Context, model KitsuURLModel) (KitsuURLModel, error) {
	u, err := url.Parse(model.ResolvedAPIBaseURL)
	if err != nil {
		return KitsuURLModel{}, connectionError("url_parse_invalid")
	}
	ips, err := resolveKitsuIPs(ctx, u.Hostname())
	if err != nil {
		return KitsuURLModel{}, err
	}
	scope, err := resolvedKitsuScope(ips)
	if err != nil {
		return KitsuURLModel{}, err
	}
	model.TargetScope, model.VerifiedIPs = scope, ips
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, model.ResolvedAPIBaseURL+"/status", nil)
	if err != nil {
		return KitsuURLModel{}, connectionError("url_parse_invalid")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := safeKitsuClient(model, ips).Do(req)
	if err != nil {
		return KitsuURLModel{}, classifyKitsuTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if location, err := req.URL.Parse(resp.Header.Get("Location")); err == nil && req.URL.Scheme == "https" && location.Scheme == "http" {
			return KitsuURLModel{}, connectionError("tls_downgrade_blocked")
		}
		return KitsuURLModel{}, connectionError("redirect_origin_changed")
	}
	if resp.StatusCode == http.StatusNotFound {
		return KitsuURLModel{}, connectionError("zou_status_not_found")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KitsuURLModel{}, connectionError("zou_unhealthy")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return KitsuURLModel{}, connectionError("zou_identity_mismatch")
	}
	var status struct {
		Name            string `json:"name"`
		DatabaseUp      *bool  `json:"database-up"`
		EventStreamUp   *bool  `json:"event-stream-up"`
		JobQueueUp      *bool  `json:"job-queue-up"`
		KeyValueStoreUp *bool  `json:"key-value-store-up"`
		Version         string `json:"version"`
	}
	if json.Unmarshal(body, &status) != nil || !strings.EqualFold(status.Name, "Zou") || status.Version == "" {
		return KitsuURLModel{}, connectionError("zou_identity_mismatch")
	}
	if status.DatabaseUp == nil || status.EventStreamUp == nil || status.JobQueueUp == nil || status.KeyValueStoreUp == nil || !*status.DatabaseUp || !*status.EventStreamUp || !*status.JobQueueUp || !*status.KeyValueStoreUp {
		return KitsuURLModel{}, connectionError("zou_unhealthy")
	}
	return model, nil
}

func classifyKitsuTransportError(err error) error {
	var unknown *net.DNSError
	if errors.As(err, &unknown) {
		return connectionError("dns_resolution_failed")
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "expired") {
		return connectionError("tls_certificate_expired")
	}
	if strings.Contains(message, "hostname") || strings.Contains(message, "not valid for") {
		return connectionError("tls_hostname_mismatch")
	}
	if strings.Contains(message, "certificate") {
		return connectionError("tls_untrusted_ca")
	}
	if strings.Contains(message, "connection refused") {
		return connectionError("tcp_connection_refused")
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(message, "timeout") {
		return connectionError("tcp_connect_timeout")
	}
	return connectionError("network_failed")
}
func ResolveAndProbeKitsu(ctx context.Context, raw, source string) (KitsuURLModel, error) {
	model, err := NormalizeKitsuURL(raw, source)
	if err != nil {
		return KitsuURLModel{}, err
	}
	return verifyKitsuZou(ctx, model)
}

// ResolveKitsuConnection keeps the browser/runtime base distinct from an
// optional API endpoint. The API endpoint owns DNS scope and verification.
func ResolveKitsuConnection(ctx context.Context, displayRaw, apiOverride string) (KitsuURLModel, error) {
	display, err := NormalizeKitsuURL(displayRaw, APISourceDerived)
	if err != nil {
		return KitsuURLModel{}, err
	}
	target := display
	if strings.TrimSpace(apiOverride) != "" {
		target, err = NormalizeKitsuURL(apiOverride, APISourceExplicit)
		if err != nil {
			return KitsuURLModel{}, err
		}
	}
	verified, err := verifyKitsuZou(ctx, target)
	if err != nil {
		return KitsuURLModel{}, err
	}
	verified.DisplayBaseURL = display.DisplayBaseURL
	verified.RuntimeBaseURL = display.RuntimeBaseURL
	return verified, nil
}

// AuthenticateKitsuCredentials posts only through the pinned, verified
// transport. It rechecks DNS equality before each credential delivery.
func AuthenticateKitsuCredentials(ctx context.Context, model KitsuURLModel, email, password string) (string, string, error) {
	if err := ensureKitsuTargetStable(ctx, model); err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(map[string]string{"email": strings.TrimSpace(email), "password": password})
	if err != nil {
		return "", "", connectionError("auth_request_invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, model.ResolvedAPIBaseURL+"/auth/login", strings.NewReader(string(payload)))
	if err != nil {
		return "", "", connectionError("auth_request_invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := safeKitsuClient(model, model.VerifiedIPs).Do(req)
	if err != nil {
		return "", "", classifyKitsuTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "", "", connectionError("auth_redirect_blocked")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", connectionError("auth_failed")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", "", connectionError("auth_response_invalid")
	}
	var result struct {
		AccessToken string `json:"access_token"`
		User        struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if json.Unmarshal(body, &result) != nil || strings.TrimSpace(result.AccessToken) == "" {
		return "", "", connectionError("auth_response_invalid")
	}
	return result.AccessToken, result.User.Role, nil
}

func VerifyKitsuToken(ctx context.Context, model KitsuURLModel, token string) error {
	if err := ensureKitsuTargetStable(ctx, model); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, model.ResolvedAPIBaseURL+"/auth/authenticated", nil)
	if err != nil {
		return connectionError("auth_request_invalid")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	resp, err := safeKitsuClient(model, model.VerifiedIPs).Do(req)
	if err != nil {
		return classifyKitsuTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return connectionError("auth_redirect_blocked")
	}
	if resp.StatusCode != http.StatusOK {
		return connectionError("auth_failed")
	}
	return nil
}
