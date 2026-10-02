//nolint:wrapcheck // Callers map bounded inspection outcomes to MCP tool results.
package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

const (
	directRemoteURLMaxBytes       = 2048
	directRemoteProbeDeadline     = 10 * time.Second
	directRemoteProbeMaxRedirects = 3
	// directRemoteProbeMaxBytes caps aggregate response bytes, including redirects and cleanup.
	directRemoteProbeMaxBytes = 256 << 10 // 256 KiB
	// directRemoteProbeMaxRequests bounds all wire requests, including OAuth discovery and cleanup.
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
		return DirectRemoteInspection{}, setupFailure(SetupCategoryTemporarilyUnavailable, ErrDirectRemoteUnavailable)
	}
	return s.inspect(ctx, rawURL, s.policy.Client())
}

// inspect uses the supplied Guardian client for both MCP and metadata requests.
func (s *GuardianDirectRemoteInspector) inspect(ctx context.Context, rawURL string, client *guardian.HTTPClient) (DirectRemoteInspection, error) {
	canonicalURL, err := canonicalDirectRemoteURL(rawURL)
	if err != nil {
		return DirectRemoteInspection{}, setupFailure(SetupCategoryInvalidURL, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, directRemoteProbeDeadline)
	defer cancel()
	if _, err := s.policy.ValidateHTTPSURL(probeCtx, canonicalURL); err != nil {
		return DirectRemoteInspection{}, directRemoteValidationError(probeCtx, err)
	}

	transport := &directRemoteRoundTripper{
		base: client.Transport, policy: s.policy, ctx: probeCtx,
		mu: sync.Mutex{}, budget: directRemoteResponseBudget{remaining: directRemoteProbeMaxBytes, requestsRemaining: directRemoteProbeMaxRequests},
		failure: nil, finalURL: canonicalURL, status: 0, authenticationURL: "",
	}
	client.Transport = transport
	client.CheckRedirect = transport.checkRedirect
	sdkTransport := &directRemoteTransport{inner: &mcp.StreamableClientTransport{
		Endpoint: canonicalURL, HTTPClient: client, MaxRetries: -1,
		DisableStandaloneSSE: true, OAuthHandler: nil,
	}, conn: nil}
	// Connect can fail without closing its connection. Close it even on those
	// paths; the HTTP adapter bounds detached cleanup by the inspection deadline.
	defer o11y.NoLogDefer(func() error {
		if sdkTransport.conn != nil {
			return sdkTransport.conn.Close()
		}
		return nil
	})
	sdk := mcp.NewClient(&mcp.Implementation{Name: "gram-platform-mcp", Version: "1", Title: "", Description: "", WebsiteURL: "", Icons: nil}, nil)
	session, err := sdk.Connect(probeCtx, sdkTransport, nil)
	var toolList *mcp.ListToolsResult
	if err == nil {
		// A successful fallback supersedes discovery's authentication challenge.
		// tools/list can still record a fresh challenge of its own.
		transport.mu.Lock()
		transport.authenticationURL = ""
		transport.mu.Unlock()
		defer o11y.NoLogDefer(session.Close)
		// One page only: inspection exposes at most 50 names, not a full catalogue.
		toolList, err = session.ListTools(probeCtx, &mcp.ListToolsParams{Meta: nil, Cursor: ""})
	}
	finalURL, _, authenticationURL, failure := transport.observation()
	if failure != nil {
		return DirectRemoteInspection{}, sanitizedSetupFailure(failure)
	}
	if err != nil {
		if authenticationURL != "" {
			oauthDiscovery, discoveryErr := directRemoteOAuthDiscovery(probeCtx, s.policy, client, authenticationURL)
			if discoveryErr != nil {
				return DirectRemoteInspection{}, sanitizedSetupFailure(discoveryErr)
			}
			return directRemoteInspection(authenticationURL, nil, "authentication_required", oauthDiscovery, true), nil
		}
		return DirectRemoteInspection{}, directRemoteProbeError(probeCtx, err)
	}
	if toolList == nil {
		return DirectRemoteInspection{}, setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
	}
	names := make([]string, 0, min(len(toolList.Tools), directRemoteToolNameLimit))
	for _, tool := range toolList.Tools {
		if tool == nil {
			return DirectRemoteInspection{}, setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
		}
		if name := strings.TrimSpace(tool.Name); name != "" {
			names = append(names, name)
			if len(names) == directRemoteToolNameLimit {
				break
			}
		}
	}
	return directRemoteInspection(finalURL, names, "anonymous", "not_advertised", false), nil
}

// directRemoteTransport retains connections even when SDK negotiation fails.
type directRemoteTransport struct {
	inner mcp.Transport
	conn  mcp.Connection
}

func (t *directRemoteTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	t.conn = conn
	return conn, err
}

type directRemoteResponseBudget struct {
	remaining         int
	requestsRemaining int
}

func (b *directRemoteResponseBudget) consumeRequest() error {
	if b.remaining <= 0 || b.requestsRemaining <= 0 {
		return setupFailure(SetupCategoryTemporarilyUnavailable, ErrDirectRemoteUnavailable)
	}
	b.requestsRemaining--
	return nil
}

// directRemoteRoundTripper serializes accounting across SDK requests, redirects,
// OAuth discovery and session cleanup. Bodies are buffered within the aggregate
// cap so redirects and SDK-discarded error bodies also consume the same budget.
type directRemoteRoundTripper struct {
	base   http.RoundTripper
	policy *guardian.Policy
	ctx    context.Context //nolint:containedctx // This per-inspection transport must bound SDK cleanup, which detaches its request context.
	mu     sync.Mutex
	budget directRemoteResponseBudget
	// failure is sticky: SDK fallback cannot bypass an admission policy failure.
	failure           error
	finalURL          string
	status            int
	authenticationURL string
}

func (rt *directRemoteRoundTripper) observation() (string, int, string, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.finalURL, rt.status, rt.authenticationURL, rt.failure
}

// directRemoteRequestPurpose survives HTTP method changes during redirects.
type directRemoteRequestPurpose string

const (
	directRemoteMCPRequest      directRemoteRequestPurpose = "mcp"
	directRemoteMetadataRequest directRemoteRequestPurpose = "metadata"
	directRemoteCleanupRequest  directRemoteRequestPurpose = "cleanup"
)

type directRemoteRequestPurposeKey struct{}

func directRemotePurpose(req *http.Request) directRemoteRequestPurpose {
	if purpose, ok := req.Context().Value(directRemoteRequestPurposeKey{}).(directRemoteRequestPurpose); ok {
		return purpose
	}
	switch req.Method {
	case http.MethodDelete:
		return directRemoteCleanupRequest
	case http.MethodGet:
		return directRemoteMetadataRequest
	default:
		return directRemoteMCPRequest
	}
}

func (rt *directRemoteRoundTripper) checkRedirect(req *http.Request, via []*http.Request) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	purpose := directRemotePurpose(via[0])
	*req = *req.WithContext(context.WithValue(req.Context(), directRemoteRequestPurposeKey{}, purpose))
	if len(via) > directRemoteProbeMaxRedirects {
		err := setupFailure(SetupCategoryUnsafeTargetOrRedirect, ErrDirectRemoteRejected)
		if purpose == directRemoteMCPRequest && rt.failure == nil {
			rt.failure = err
		}
		return err
	}
	// Every destination is validated and charged by RoundTrip, including redirects.
	req.Header.Del("Authorization")
	return nil
}

func (rt *directRemoteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	// Optional metadata attempts and cleanup must not poison negotiation or
	// overwrite its observation, even when a redirect changes the HTTP method.
	purpose := directRemotePurpose(req)
	resp, err := rt.roundTrip(req)
	if err != nil && purpose == directRemoteMCPRequest && rt.failure == nil {
		rt.failure = err
	}
	return resp, err
}

func (rt *directRemoteRoundTripper) roundTrip(req *http.Request) (*http.Response, error) {
	purpose := directRemotePurpose(req)
	if rt.failure != nil && purpose == directRemoteMCPRequest {
		return nil, rt.failure
	}
	if err := rt.ctx.Err(); err != nil {
		return nil, directRemoteTransportError(rt.ctx, err)
	}
	if err := rt.budget.consumeRequest(); err != nil {
		return nil, err
	}
	target, err := canonicalDirectRemoteURL(req.URL.String())
	if err != nil {
		return nil, setupFailure(SetupCategoryUnsafeTargetOrRedirect, ErrDirectRemoteRejected)
	}
	validated, err := rt.policy.ValidateHTTPSURL(rt.ctx, target)
	if err != nil {
		return nil, directRemoteValidationError(rt.ctx, err)
	}
	if len(req.Header.Get("Mcp-Session-Id")) > directRemoteSessionIDMaxBytes {
		return nil, setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
	}
	// SDK cleanup detaches its context. Reattach the absolute inspection deadline
	// and cancellation while retaining any earlier request-specific cancellation.
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(rt.ctx, cancel)
	defer stop()
	defer cancel()
	req = req.Clone(ctx)
	req.URL = validated
	req.Header.Del("Authorization")
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, directRemoteTransportError(rt.ctx, err)
	}
	body := resp.Body
	defer o11y.NoLogDefer(func() error { return body.Close() })
	if purpose == directRemoteMCPRequest {
		rt.finalURL, rt.status = target, resp.StatusCode
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			rt.authenticationURL = target
		}
	}
	if len(resp.Header.Get("Mcp-Session-Id")) > directRemoteSessionIDMaxBytes {
		return nil, setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	successfulMCP := purpose == directRemoteMCPRequest && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices
	// Reject SSE before buffering: a stream would otherwise hold the body open.
	if successfulMCP && mediaType == "text/event-stream" {
		return nil, setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(rt.budget.remaining)+1))
	exceeded := len(payload) > rt.budget.remaining
	rt.budget.remaining = max(0, rt.budget.remaining-len(payload))
	if exceeded {
		return nil, setupFailure(SetupCategoryTemporarilyUnavailable, ErrDirectRemoteUnavailable)
	}
	if err != nil {
		return nil, directRemoteTransportError(rt.ctx, err)
	}
	// Notification acknowledgements may be an empty 200 or 204 instead of 202.
	if successfulMCP && resp.StatusCode != http.StatusAccepted && len(payload) > 0 && mediaType != "application/json" {
		return nil, setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
	}
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	return resp, nil
}

func directRemoteValidationError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return directRemoteTransportError(ctx, err)
	}
	return setupFailure(SetupCategoryUnsafeTargetOrRedirect, ErrDirectRemoteRejected)
}

// Transport failures are availability failures even when their concrete error
// type is not net.Error (for example certificate verification or a broken body).
func directRemoteTransportError(ctx context.Context, err error) error {
	classified := directRemoteProbeError(ctx, err)
	if setupCategoryFromError(classified) == SetupCategoryInvalidMCPResponse {
		return setupFailure(SetupCategoryUnreachable, ErrDirectRemoteUnavailable)
	}
	return classified
}

func directRemoteProbeError(ctx context.Context, err error) error {
	if category := setupCategoryFromError(err); category != "" {
		return sanitizedSetupFailure(err)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return setupFailure(SetupCategoryTimeout, ErrDirectRemoteUnavailable)
	}
	if errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) {
		return setupFailure(SetupCategoryUnsafeTargetOrRedirect, ErrDirectRemoteRejected)
	}
	var networkError net.Error
	if errors.As(err, &networkError) || errors.Is(err, context.Canceled) {
		return setupFailure(SetupCategoryUnreachable, ErrDirectRemoteUnavailable)
	}
	return setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
}

type directRemoteHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// directRemoteOAuthDiscovery returns a safe discovery category or a sanitized
// temporary failure. Metadata URLs are derived from the canonical resource or a
// discovered issuer, rechecked with Guardian before egress, and charged to the
// inspection response budget.
func directRemoteOAuthDiscovery(ctx context.Context, policy *guardian.Policy, client directRemoteHTTPClient, resourceURL string) (string, error) {
	available := false
	for _, metadataURL := range directRemoteProtectedResourceMetadataURLs(resourceURL) {
		metadata, status, err := directRemoteGetJSON(ctx, policy, client, metadataURL)
		if transientDirectRemoteMetadataStatus(status) {
			err = setupFailure(SetupCategoryTemporarilyUnavailable, ErrDirectRemoteUnavailable)
		}
		if setupCategoryFromError(err) == SetupCategoryTemporarilyUnavailable {
			if available {
				return "available", nil
			}
			return "", err
		}
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
				authorizationMetadata, status, err := directRemoteGetJSON(ctx, policy, client, authorizationMetadataURL)
				if transientDirectRemoteMetadataStatus(status) {
					err = setupFailure(SetupCategoryTemporarilyUnavailable, ErrDirectRemoteUnavailable)
				}
				if setupCategoryFromError(err) == SetupCategoryTemporarilyUnavailable {
					if available {
						return "available", nil
					}
					return "", err
				}
				if err != nil || status != http.StatusOK {
					continue
				}
				if endpoint, _ := authorizationMetadata["registration_endpoint"].(string); endpoint != "" {
					return "available_dcr", nil
				}
				available = true
			}
		}
	}
	if available {
		return "available", nil
	}
	return "incomplete", nil
}

func transientDirectRemoteMetadataStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status == http.StatusInternalServerError || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
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

func directRemoteGetJSON(ctx context.Context, policy *guardian.Policy, client directRemoteHTTPClient, rawURL string) (map[string]any, int, error) {
	if policy == nil || client == nil {
		return nil, 0, ErrDirectRemoteUnavailable
	}
	canonicalURL, err := canonicalDirectRemoteURL(rawURL)
	if err != nil {
		return nil, 0, err
	}
	if _, err := policy.ValidateHTTPSURL(ctx, canonicalURL); err != nil {
		return nil, 0, ErrDirectRemoteRejected
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
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		return nil, resp.StatusCode, ErrDirectRemoteRejected
	}
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, ErrDirectRemoteRejected
	}
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
