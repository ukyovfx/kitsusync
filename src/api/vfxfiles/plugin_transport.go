package vfxfiles

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

var lookupPluginOrigin = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func resolvePluginOrigin(ctx context.Context, host string) ([]netip.Addr, error) {
	host = normalizePluginHost(host)
	if host == "" || host == "metadata" || host == "metadata.google.internal" {
		return nil, ErrPluginRequestRejected
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if !validPluginAddress(address) {
			return nil, ErrPluginRequestRejected
		}
		return []netip.Addr{address}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := lookupPluginOrigin(ctx, host)
	if err != nil || len(addresses) == 0 || len(addresses) > 16 {
		return nil, ErrPluginRequestRejected
	}
	unique := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !validPluginAddress(address) {
			return nil, ErrPluginRequestRejected
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		unique = append(unique, address)
	}
	if len(unique) == 0 {
		return nil, ErrPluginRequestRejected
	}
	return unique, nil
}

func validPluginAddress(address netip.Addr) bool {
	return address.IsValid() && address.IsGlobalUnicast() && !address.IsLinkLocalUnicast() && !address.IsMulticast() && !address.IsUnspecified()
}

func normalizePluginHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func newPinnedPluginTransport(host, port string, pinned []netip.Addr, dial func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	host = normalizePluginHost(host)
	addresses := append([]netip.Addr(nil), pinned...)
	transport := &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
	}
	if dial == nil {
		dial = (&net.Dialer{Timeout: 4 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		requestedHost, requestedPort, err := net.SplitHostPort(address)
		if err != nil || normalizePluginHost(requestedHost) != host || requestedPort != port || len(addresses) == 0 {
			return nil, ErrPluginRequestRejected
		}
		var lastErr error
		for _, pinnedAddress := range addresses {
			if !validPluginAddress(pinnedAddress) {
				return nil, ErrPluginRequestRejected
			}
			connection, dialErr := dial(ctx, network, net.JoinHostPort(pinnedAddress.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = errors.New("no pinned Plugin address")
		}
		return nil, lastErr
	}
	return transport
}

func defaultPluginHTTPClient(baseURL string) (*http.Client, error) {
	parsed, err := parsePluginAudience(baseURL)
	if err != nil {
		return nil, ErrPluginRequestRejected
	}
	host := normalizePluginHost(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	pinned, err := resolvePluginOrigin(ctx, host)
	if err != nil {
		return nil, ErrPluginRequestRejected
	}
	return &http.Client{Transport: newPinnedPluginTransport(host, port, pinned, nil), Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
