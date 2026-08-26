//nolint:wrapcheck // Callers map bounded inspection outcomes to MCP tool results.
package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

const (
	directRemoteURLMaxBytes       = 2048
	directRemoteProbeDeadline     = 10 * time.Second
	directRemoteProbeMaxRedirects = 3
	directRemoteProbeMaxBytes     = 256 << 10
	directRemoteProbeMaxRequests  = 8
	directRemoteOAuthServerLimit  = 2
	directRemoteSessionIDMaxBytes = 512
	directRemoteToolNameLimit     = 50
	directRemoteProviderKey       = "direct-remote-url-v1"
	directRemoteSourceKind        = "remote_url"
)

var (
	ErrDirectRemoteRejected    = errors.New("direct remote MCP URL rejected")
	ErrDirectRemoteUnavailable = errors.New("direct remote MCP inspection unavailable")
)

// DirectRemoteInspection is the bounded, non-secret projection of a direct
// user-supplied remote MCP. It deliberately contains no response headers,
// response body, OAuth metadata, credentials, or schemas.
type DirectRemoteInspection struct {
	CanonicalURL           string   `json:"canonical_url"`
	Transport              string   `json:"transport"`
	ToolNames              []string `json:"tool_names"`
	ToolCount              int      `json:"tool_count"`
	Authentication         string   `json:"authentication"`
	OAuthDiscovery         string   `json:"oauth_discovery"`
	Trust                  string   `json:"trust"`
	RequiresDashboardSetup bool     `json:"requires_dashboard_setup"`
}

// DirectRemoteInspector is the boundary shared by candidate inspection and
// registration. Registration must call it again; an earlier tool result is
// never admission evidence.
type DirectRemoteInspector interface {
	Inspect(ctx context.Context, rawURL string) (DirectRemoteInspection, error)
}

// GuardianDirectRemoteInspector canonicalizes, validates, and probes one
// direct remote MCP using Guardian before every network hop. It supports only
// Streamable HTTP's JSON response form; standalone SSE is intentionally not a
// D1 admission path.
type GuardianDirectRemoteInspector struct {
	policy *guardian.Policy
}

func NewGuardianDirectRemoteInspector(policy *guardian.Policy) *GuardianDirectRemoteInspector {
	return &GuardianDirectRemoteInspector{policy: policy}
}

func (s *GuardianDirectRemoteInspector) Inspect(ctx context.Context, rawURL string) (DirectRemoteInspection, error) {
	if s == nil || s.policy == nil {
		return DirectRemoteInspection{}, ErrDirectRemoteUnavailable
	}
	canonicalURL, err := canonicalDirectRemoteURL(rawURL)
	if err != nil {
		return DirectRemoteInspection{}, err
	}
	if _, err := s.policy.ValidateHTTPSURL(ctx, canonicalURL); err != nil {
		return DirectRemoteInspection{}, fmt.Errorf("validate direct remote URL: %w", ErrDirectRemoteRejected)
	}

	probeCtx, cancel := context.WithTimeout(ctx, directRemoteProbeDeadline)
	defer cancel()
	budget := &directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests}
	client := s.policy.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > directRemoteProbeMaxRedirects || budget.consumeRequest() != nil {
			return fmt.Errorf("%w: too many redirects", ErrDirectRemoteRejected)
		}
		next, err := canonicalDirectRemoteURL(req.URL.String())
		if err != nil {
			return err
		}
		validated, err := s.policy.ValidateHTTPSURL(req.Context(), next)
		if err != nil {
			return fmt.Errorf("%w: unsafe redirect", ErrDirectRemoteRejected)
		}
		req.URL = validated
		// This probe never sends credentials. Explicitly clearing Authorization
		// keeps that invariant if a client implementation changes later.
		req.Header.Del("Authorization")
		return nil
	}

	initialize, finalURL, sessionID, status, err := directRemoteRequest(probeCtx, client, canonicalURL, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "gram-platform-mcp", "version": "1"},
	}, "", budget)
	if err != nil {
		return DirectRemoteInspection{}, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return directRemoteInspection(finalURL, nil, "authentication_required", directRemoteOAuthDiscovery(probeCtx, s.policy, client, finalURL, budget), true), nil
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices || initialize.Result == nil {
		return DirectRemoteInspection{}, ErrDirectRemoteRejected
	}
	if sessionID != "" {
		finalURL, status, err = directRemoteNotification(probeCtx, client, finalURL, "notifications/initialized", map[string]any{}, sessionID, budget)
		if err != nil || status < http.StatusOK || status >= http.StatusMultipleChoices {
			return DirectRemoteInspection{}, ErrDirectRemoteRejected
		}
	}

	tools, finalURL, _, status, err := directRemoteRequest(probeCtx, client, finalURL, "tools/list", map[string]any{}, sessionID, budget)
	if err != nil {
		return DirectRemoteInspection{}, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return directRemoteInspection(finalURL, nil, "authentication_required", directRemoteOAuthDiscovery(probeCtx, s.policy, client, finalURL, budget), true), nil
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices || tools.Result == nil {
		return DirectRemoteInspection{}, ErrDirectRemoteRejected
	}

	var toolList struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(tools.Result, &toolList); err != nil {
		return DirectRemoteInspection{}, ErrDirectRemoteRejected
	}
	names := make([]string, 0, min(len(toolList.Tools), directRemoteToolNameLimit))
	for _, tool := range toolList.Tools {
		if name := strings.TrimSpace(tool.Name); name != "" {
			names = append(names, name)
			if len(names) == directRemoteToolNameLimit {
				break
			}
		}
	}
	return directRemoteInspection(finalURL, names, "anonymous", "not_advertised", false), nil
}

type directRemoteResponseBudget struct {
	remaining         int
	requestsRemaining int
}

func (b *directRemoteResponseBudget) consumeRequest() error {
	if b == nil || b.remaining <= 0 || b.requestsRemaining <= 0 {
		return ErrDirectRemoteRejected
	}
	b.requestsRemaining--
	return nil
}

type directRemoteRPCResponse struct {
	Result json.RawMessage `json:"result"`
}

type directRemoteHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func emptyDirectRemoteRPCResponse() directRemoteRPCResponse {
	return directRemoteRPCResponse{Result: nil}
}

func directRemoteRequest(ctx context.Context, client directRemoteHTTPClient, rawURL, method string, params any, sessionID string, budget *directRemoteResponseBudget) (directRemoteRPCResponse, string, string, int, error) {
	if err := budget.consumeRequest(); err != nil {
		return emptyDirectRemoteRPCResponse(), "", "", 0, err
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return emptyDirectRemoteRPCResponse(), "", "", 0, fmt.Errorf("marshal MCP probe: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return emptyDirectRemoteRPCResponse(), "", "", 0, fmt.Errorf("build direct MCP probe: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		if len(sessionID) > directRemoteSessionIDMaxBytes {
			return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteRejected
		}
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrDirectRemoteRejected) || errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) {
			return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteRejected
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteUnavailable
		}
		return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	finalURL, err := canonicalDirectRemoteURL(resp.Request.URL.String())
	if err != nil {
		return emptyDirectRemoteRPCResponse(), "", "", 0, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return emptyDirectRemoteRPCResponse(), finalURL, "", resp.StatusCode, nil
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteRejected
	}
	if budget == nil || budget.remaining <= 0 {
		return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteRejected
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(budget.remaining+1)))
	if err != nil || len(payload) > budget.remaining {
		return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteRejected
	}
	budget.remaining -= len(payload)
	var result directRemoteRPCResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteRejected
	}
	sessionID = resp.Header.Get("Mcp-Session-Id")
	if len(sessionID) > directRemoteSessionIDMaxBytes {
		return emptyDirectRemoteRPCResponse(), "", "", 0, ErrDirectRemoteRejected
	}
	return result, finalURL, sessionID, resp.StatusCode, nil
}

func directRemoteNotification(ctx context.Context, client directRemoteHTTPClient, rawURL, method string, params any, sessionID string, budget *directRemoteResponseBudget) (string, int, error) {
	if err := budget.consumeRequest(); err != nil {
		return "", 0, err
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return "", 0, fmt.Errorf("marshal MCP probe notification: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("build direct MCP probe notification: %w", err)
	}
	if len(sessionID) > directRemoteSessionIDMaxBytes {
		return "", 0, ErrDirectRemoteRejected
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrDirectRemoteRejected) || errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) {
			return "", 0, ErrDirectRemoteRejected
		}
		return "", 0, ErrDirectRemoteUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	finalURL, err := canonicalDirectRemoteURL(resp.Request.URL.String())
	if err != nil {
		return "", 0, err
	}
	if budget == nil || budget.remaining <= 0 {
		return "", 0, ErrDirectRemoteRejected
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(budget.remaining+1)))
	if err != nil || len(payload) > budget.remaining {
		return "", 0, ErrDirectRemoteRejected
	}
	budget.remaining -= len(payload)
	return finalURL, resp.StatusCode, nil
}

// directRemoteOAuthDiscovery returns only the safe discovery category. Metadata
// URLs are derived from the canonical resource or a discovered issuer, rechecked
// with Guardian before egress, and charged to the inspection response budget.
func directRemoteOAuthDiscovery(ctx context.Context, policy *guardian.Policy, client directRemoteHTTPClient, resourceURL string, budget *directRemoteResponseBudget) string {
	available := false
	for _, metadataURL := range directRemoteProtectedResourceMetadataURLs(resourceURL) {
		metadata, status, err := directRemoteGetJSON(ctx, policy, client, metadataURL, budget)
		if err != nil || status != http.StatusOK {
			continue
		}
		servers, ok := metadata["authorization_servers"].([]any)
		if !ok || len(servers) == 0 {
			continue
		}
		for index, server := range servers {
			if index == directRemoteOAuthServerLimit {
				break
			}
			issuer, ok := server.(string)
			if !ok || issuer == "" {
				continue
			}
			for _, authorizationMetadataURL := range directRemoteAuthorizationServerMetadataURLs(issuer) {
				authorizationMetadata, status, err := directRemoteGetJSON(ctx, policy, client, authorizationMetadataURL, budget)
				if err != nil || status != http.StatusOK {
					continue
				}
				if endpoint, _ := authorizationMetadata["registration_endpoint"].(string); endpoint != "" {
					return "available_dcr"
				}
				available = true
			}
		}
	}
	if available {
		return "available"
	}
	return "incomplete"
}

func directRemoteProtectedResourceMetadataURLs(resourceURL string) []string {
	parsed, err := url.Parse(resourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil
	}
	origin := "https://" + parsed.Host
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "" {
		return []string{origin + "/.well-known/oauth-protected-resource"}
	}
	return []string{origin + "/.well-known/oauth-protected-resource" + path, origin + "/.well-known/oauth-protected-resource"}
}

// directRemoteAuthorizationServerMetadataURLs reuses the production issuer
// discovery candidates. Query parameters identify the MCP endpoint and never
// flow into metadata URLs.
func directRemoteAuthorizationServerMetadataURLs(issuer string) []string {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	candidates, err := remotesessions.IssuerMetadataProbeCandidates(parsed.String())
	if err != nil {
		return nil
	}
	return candidates
}

func directRemoteGetJSON(ctx context.Context, policy *guardian.Policy, client directRemoteHTTPClient, rawURL string, budget *directRemoteResponseBudget) (map[string]any, int, error) {
	if policy == nil || client == nil || budget == nil || budget.remaining <= 0 {
		return nil, 0, ErrDirectRemoteUnavailable
	}
	canonicalURL, err := canonicalDirectRemoteURL(rawURL)
	if err != nil {
		return nil, 0, err
	}
	if _, err := policy.ValidateHTTPSURL(ctx, canonicalURL); err != nil {
		return nil, 0, ErrDirectRemoteRejected
	}
	if err := budget.consumeRequest(); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, canonicalURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		return nil, resp.StatusCode, ErrDirectRemoteRejected
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(budget.remaining+1)))
	if err != nil || len(payload) > budget.remaining {
		return nil, resp.StatusCode, ErrDirectRemoteRejected
	}
	budget.remaining -= len(payload)
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, resp.StatusCode, ErrDirectRemoteRejected
	}
	return value, resp.StatusCode, nil
}

func directRemoteInspection(canonicalURL string, toolNames []string, authentication, oauthDiscovery string, requiresDashboardSetup bool) DirectRemoteInspection {
	return DirectRemoteInspection{
		CanonicalURL:           canonicalURL,
		Transport:              "streamable-http",
		ToolNames:              append([]string(nil), toolNames...),
		ToolCount:              len(toolNames),
		Authentication:         authentication,
		OAuthDiscovery:         oauthDiscovery,
		Trust:                  "user_supplied_unreviewed",
		RequiresDashboardSetup: requiresDashboardSetup,
	}
}

// canonicalDirectRemoteURL is intentionally pure so persistence can repeat the
// cheap shape/canonical-equality guard without DNS or egress while holding its
// project locks. Guardian validation remains mandatory before probing/persisting.
func canonicalDirectRemoteURL(rawURL string) (string, error) {
	if len(rawURL) > directRemoteURLMaxBytes || containsURLControlOrTemplate(rawURL) {
		return "", ErrDirectRemoteRejected
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > directRemoteURLMaxBytes {
		return "", ErrDirectRemoteRejected
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.Fragment != "" || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || containsURLControlOrTemplate(parsed.Path) || containsURLControlOrTemplate(parsed.RawPath) || !safeDirectRemoteQuery(parsed) {
		return "", ErrDirectRemoteRejected
	}
	port := parsed.Port()
	if port != "" && port != "443" {
		return "", ErrDirectRemoteRejected
	}
	host := strings.ToLower(parsed.Hostname())
	if ip := net.ParseIP(host); ip == nil && strings.HasSuffix(host, ".") {
		return "", ErrDirectRemoteRejected
	}
	parsed.Scheme = "https"
	parsed.Host = host
	if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	}
	parsed.User = nil
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	canonical := parsed.String()
	if len(canonical) > directRemoteURLMaxBytes {
		return "", ErrDirectRemoteRejected
	}
	return canonical, nil
}

func safeDirectRemoteQuery(parsed *url.URL) bool {
	if parsed == nil || parsed.ForceQuery || containsURLControlOrTemplate(parsed.RawQuery) {
		return false
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return false
	}
	for key, entries := range values {
		if containsURLControlOrTemplate(key) || directRemoteQueryCredentialKey(key) {
			return false
		}
		if slices.ContainsFunc(entries, containsURLControlOrTemplate) {
			return false
		}
	}
	return true
}

func directRemoteQueryCredentialKey(key string) bool {
	normalized := strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	if strings.HasPrefix(normalized, "x_amz_") || strings.HasPrefix(normalized, "x_goog_") {
		return true
	}
	switch normalized {
	case "access_token", "api_key", "apikey", "x_api_key", "xapikey", "authorization", "credential", "key", "password", "secret", "signature", "sig", "token", "client_secret":
		return true
	default:
		return false
	}
}

func containsURLControlOrTemplate(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) || r == '{' || r == '}' {
			return true
		}
	}
	return false
}

func validDirectRemoteRegistrationURL(rawURL string) bool {
	canonical, err := canonicalDirectRemoteURL(rawURL)
	return err == nil && canonical == rawURL
}
