package vfxfiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	contractVersion         = "1.0"
	pluginRequestBodyLimit  = 1 << 20
	pluginResponseBodyLimit = 4 << 20
)

func validID(value string) bool { return serviceUUIDPattern.MatchString(strings.TrimSpace(value)) }

// ValidID reports whether a contract resource identifier is a canonical UUID.
func ValidID(value string) bool { return validID(value) }

// IsTrustedAuthorizationURL accepts only the Plugin's fixed Google OAuth URL.
func IsTrustedAuthorizationURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), "accounts.google.com") && parsed.Port() == "" && parsed.User == nil && parsed.Path == "/o/oauth2/v2/auth" && parsed.Fragment == ""
}

var ErrPluginRequestRejected = errors.New("vfx files Plugin request rejected")

type PluginRequest struct {
	Method         string
	Path           string
	Query          url.Values
	Headers        http.Header
	Body           []byte
	ProductionID   string
	Authorizer     ProductionAdminAuthorizer
	Scope          string
	IdempotencyKey string
	Mutating       bool
	Now            time.Time
}

type PluginClient struct {
	baseURL *url.URL
	http    *http.Client
	token   ServiceTokenConfig
}

type pluginOperation struct {
	method string
	path   string
	scope  string
	write  bool
}

var pluginOperations = []pluginOperation{
	{http.MethodGet, "/drive-connections", "vfx.connections.read", false},
	{http.MethodPost, "/drive-connections/authorizations", "vfx.connections.authorize", true},
	{http.MethodGet, "/drive-connections/{connection_id}", "vfx.connections.read", false},
	{http.MethodPost, "/drive-connections/{connection_id}/revoke", "vfx.connections.revoke", true},
	{http.MethodGet, "/drive-connections/{connection_id}/roots", "vfx.roots.read", false},
	{http.MethodPost, "/drive-connections/{connection_id}/roots", "vfx.roots.verify", true},
	{http.MethodGet, "/templates", "vfx.templates.read", false},
	{http.MethodGet, "/productions/{production_id}/desired-config", "vfx.config.read", false},
	{http.MethodPut, "/productions/{production_id}/desired-config", "vfx.config.write", true},
	{http.MethodPost, "/productions/{production_id}/activation", "vfx.activation.write", true},
	{http.MethodPost, "/productions/{production_id}/deactivation", "vfx.activation.write", true},
	{http.MethodPost, "/productions/{production_id}/sync-previews", "vfx.sync.preview", false},
	{http.MethodPost, "/productions/{production_id}/sync-jobs", "vfx.sync.apply", true},
	{http.MethodGet, "/sync-jobs/{job_id}", "vfx.sync.read", false},
	{http.MethodPost, "/sync-jobs/{job_id}/retry", "vfx.sync.retry", true},
	{http.MethodPost, "/sync-jobs/{job_id}/reconcile", "vfx.sync.reconcile", true},
	{http.MethodPost, "/productions/{production_id}/sync-snapshots", "vfx.sync.snapshot.write", true},
	{http.MethodPut, "/sync-snapshots/{snapshot_id}/pages/{page_index}", "vfx.sync.snapshot.write", true},
	{http.MethodPost, "/sync-snapshots/{snapshot_id}/complete", "vfx.sync.snapshot.write", true},
}

// NewPluginClient accepts only the exact Zou plugin route base. The HTTP client
// never follows redirects, which prevents bearer credentials crossing origin.
func NewPluginClient(baseURL string, httpClient *http.Client, tokenConfig ServiceTokenConfig) (*PluginClient, error) {
	parsed, err := parsePluginAudience(baseURL)
	if err != nil {
		return nil, ErrPluginRequestRejected
	}
	if err := validateServiceTokenConfig(tokenConfig); err != nil || tokenConfig.Audience != parsed.String() {
		return nil, ErrPluginRequestRejected
	}
	if httpClient == nil {
		httpClient, err = defaultPluginHTTPClient(parsed.String())
		if err != nil {
			return nil, err
		}
	}
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	clientCopy.Timeout = 15 * time.Second
	return &PluginClient{baseURL: parsed, http: &clientCopy, token: tokenConfig}, nil
}

func parsePluginAudience(baseURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Path != ServiceAudienceSuffix || parsed.Opaque != "" {
		return nil, ErrPluginRequestRejected
	}
	return parsed, nil
}

// Do sends one contract-declared management API operation. Scope is derived
// from the operation table, not from caller input. All writes except the
// explicitly read-only preview require a caller-provided idempotency key.
func (c *PluginClient) Do(ctx context.Context, request PluginRequest) (*http.Response, error) {
	if c == nil || c.baseURL == nil || c.http == nil || ctx == nil || len(request.Body) > pluginRequestBodyLimit {
		return nil, ErrPluginRequestRejected
	}
	relativePath, err := normalizePluginOperationPath(request.Path)
	if err != nil {
		return nil, ErrPluginRequestRejected
	}
	operation, ok := findPluginOperation(request.Method, relativePath)
	if !ok || operation.scope != request.Scope || !serviceUUIDPattern.MatchString(request.ProductionID) {
		return nil, ErrPluginRequestRejected
	}
	if !requestProductionMatches(relativePath, request.Query, request.Body, request.ProductionID) {
		return nil, ErrPluginRequestRejected
	}
	if operation.write != request.Mutating {
		return nil, ErrPluginRequestRejected
	}
	if operation.write {
		if !safeIdempotencyKey(request.IdempotencyKey) {
			return nil, ErrPluginRequestRejected
		}
	} else if request.IdempotencyKey != "" {
		return nil, ErrPluginRequestRejected
	}
	if request.Method == http.MethodPost && request.Body == nil {
		return nil, ErrPluginRequestRejected
	}
	// Re-authorize on every call so revoked Production access does not survive a
	// previously created API client or in-memory actor record.
	actor, err := AuthorizeProductionActor(ctx, request.Authorizer, request.ProductionID)
	if err != nil {
		return nil, ErrPluginRequestRejected
	}
	token, err := IssueServiceToken(c.token, actor, []string{operation.scope}, request.Now)
	if err != nil {
		return nil, ErrPluginRequestRejected
	}
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimSuffix(c.baseURL.Path, "/") + relativePath
	endpoint.RawPath = ""
	if len(request.Query) > 0 {
		endpoint.RawQuery = request.Query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, endpoint.String(), bytes.NewReader(request.Body))
	if err != nil {
		return nil, ErrPluginRequestRejected
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-KitsuSync-Actor-ID", actor.actorID)
	req.Header.Set("Accept", "application/json")
	if len(request.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, values := range request.Headers {
		if !allowedPluginRequestHeader(name) {
			return nil, ErrPluginRequestRejected
		}
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	if operation.write {
		req.Header.Set("Idempotency-Key", request.IdempotencyKey)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("vfx files Plugin request failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, pluginResponseBodyLimit+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(body) > pluginResponseBodyLimit {
		return nil, errors.New("vfx files Plugin response could not be safely read")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}

func requestProductionMatches(path string, query url.Values, body []byte, productionID string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	bound := false
	if len(segments) > 1 && segments[0] == "productions" {
		if segments[1] != productionID {
			return false
		}
		bound = true
	}
	if values, exists := query["production_id"]; exists {
		if len(values) != 1 || values[0] != productionID {
			return false
		}
		bound = true
	}
	if len(body) > 0 {
		var decoded any
		if err := json.Unmarshal(body, &decoded); err != nil {
			return false
		}
		match, found, valid := productionIDInValue(decoded, productionID)
		if !valid || found && !match {
			return false
		}
		bound = bound || found
	}
	if strings.HasPrefix(path, "/drive-connections") {
		return bound
	}
	// The v1 template catalog is actor-authorized and has a global route shape.
	// The short-lived bearer still carries the one Production whose live admin
	// permission was checked by AuthorizeProductionActor.
	if path == "/templates" {
		return true
	}
	// Job and snapshot identifiers are resolved server-side under the signed
	// one-Production token. The Plugin must still enforce that binding in storage.
	return true
}

func productionIDInValue(value any, expected string) (matches, found, valid bool) {
	switch typed := value.(type) {
	case map[string]any:
		for name, child := range typed {
			if name == "production_id" {
				id, ok := child.(string)
				if !ok {
					return false, true, false
				}
				if id != expected {
					return false, true, true
				}
				matches, found = true, true
				continue
			}
			childMatches, childFound, childValid := productionIDInValue(child, expected)
			if !childValid || childFound && !childMatches {
				return false, childFound, childValid
			}
			matches, found = matches || childMatches, found || childFound
		}
	case []any:
		for _, child := range typed {
			childMatches, childFound, childValid := productionIDInValue(child, expected)
			if !childValid || childFound && !childMatches {
				return false, childFound, childValid
			}
			matches, found = matches || childMatches, found || childFound
		}
	}
	return matches, found, true
}

func normalizePluginOperationPath(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "?#\\\x00") || strings.HasPrefix(value, "//") {
		return "", ErrPluginRequestRejected
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\\x00") {
			return "", ErrPluginRequestRejected
		}
	}
	return value, nil
}

func findPluginOperation(method, path string) (pluginOperation, bool) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for _, operation := range pluginOperations {
		if method != operation.method {
			continue
		}
		pattern := strings.Split(strings.Trim(operation.path, "/"), "/")
		if len(segments) != len(pattern) {
			continue
		}
		matches := true
		for index, expected := range pattern {
			if strings.HasPrefix(expected, "{") && strings.HasSuffix(expected, "}") {
				if segments[index] == "" {
					matches = false
					break
				}
				continue
			}
			if expected != segments[index] {
				matches = false
				break
			}
		}
		if matches {
			return operation, true
		}
	}
	return pluginOperation{}, false
}

func safeIdempotencyKey(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._:", char)) {
			return false
		}
	}
	return true
}

func allowedPluginRequestHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "If-Match":
		return true
	default:
		return false
	}
}
