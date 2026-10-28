//nolint:wrapcheck // Callers map bounded inspection outcomes to MCP tool results.
package platformmcp

import (
	"bufio"
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

// oauth_discovery values for an endpoint that answered with an authentication
// challenge. available_dcr and available_cimd name the automatic client
// registration path its provider offers; available means the provider
// advertises OAuth but neither path; incomplete means no usable metadata.
const (
	oauthDiscoveryAvailableDCR  = "available_dcr"
	oauthDiscoveryAvailableCIMD = "available_cimd"
	oauthDiscoveryAvailable     = "available"
	oauthDiscoveryIncomplete    = "incomplete"
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
	// AttachmentSupported reports that attach_platform_mcp_identity_provider
	// can obtain a client from the discovered provider on its own. A dynamic
	// registration endpoint only the dashboard's automatic setup can use leaves
	// it false.
	AttachmentSupported bool `json:"-"`
}

// DirectRemoteInspector is the boundary shared by candidate inspection and
// registration. Registration must call it again; an earlier tool result is
// never admission evidence.
type DirectRemoteInspector interface {
	Inspect(ctx context.Context, rawURL string) (DirectRemoteInspection, error)
}

// GuardianDirectRemoteInspector canonicalizes, validates, and probes one
// direct remote MCP using Guardian before every network hop. It supports both
// JSON and SSE responses to Streamable HTTP POST requests; standalone SSE is
// intentionally not an admission path.
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
	return s.inspect(ctx, rawURL, s.policy.Client(guardian.WithInternalCatalog()))
}

// inspect uses the supplied Guardian client for both MCP and metadata requests.
func (s *GuardianDirectRemoteInspector) inspect(ctx context.Context, rawURL string, client *guardian.HTTPClient) (DirectRemoteInspection, error) {
	canonicalURL, err := canonicalDirectRemoteURL(rawURL)
	if err != nil {
		return DirectRemoteInspection{}, setupFailure(SetupCategoryInvalidURL, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, directRemoteProbeDeadline)
	defer cancel()
	if _, err := s.policy.ValidateHTTPSURL(probeCtx, canonicalURL, guardian.WithInternalCatalog()); err != nil {
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
			oauthDiscovery, attachmentSupported, discoveryErr := directRemoteOAuthDiscovery(probeCtx, s.policy, client, authenticationURL)
			if discoveryErr != nil {
				return DirectRemoteInspection{}, sanitizedSetupFailure(discoveryErr)
			}
			inspection := directRemoteInspection(authenticationURL, nil, "authentication_required", oauthDiscovery, true)
			inspection.AttachmentSupported = attachmentSupported
			return inspection, nil
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
// OAuth discovery and session cleanup. Finite bodies are buffered within the
// aggregate cap. SSE bodies are metered as the SDK reads through the first
// complete JSON-RPC response, so an open stream cannot hold inspection forever.
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
	validated, err := rt.policy.ValidateHTTPSURL(rt.ctx, target, guardian.WithInternalCatalog())
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
	if successfulMCP && mediaType == "text/event-stream" {
		payload, err := rt.bufferSSEThroughResponse(body)
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(payload))
		return resp, nil
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
	if successfulMCP && len(payload) > 0 && mediaType != "application/json" {
		return nil, setupFailure(SetupCategoryInvalidMCPResponse, ErrDirectRemoteRejected)
	}
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	return resp, nil
}

type directRemoteCountingReader struct {
	reader io.Reader
	read   int
}

func (r *directRemoteCountingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

func (rt *directRemoteRoundTripper) bufferSSEThroughResponse(body io.Reader) ([]byte, error) {
	available := rt.budget.remaining
	counted := &directRemoteCountingReader{reader: io.LimitReader(body, int64(available)+1), read: 0}
	reader := bufio.NewReader(counted)
	payload := make([]byte, 0, min(4096, available))
	eventName := ""
	data := make([]string, 0, 1)

	for {
		line, readErr := reader.ReadString('\n')
		payload = append(payload, line...)
		if counted.read > available {
			rt.budget.remaining = 0
			return nil, setupFailure(SetupCategoryTemporarilyUnavailable, ErrDirectRemoteUnavailable)
		}
		text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		switch {
		case text == "":
			if directRemoteSSEEventIsResponse(eventName, data) {
				rt.budget.remaining -= counted.read
				return payload, nil
			}
			eventName = ""
			data = data[:0]
		case strings.HasPrefix(text, ":"):
		case text == "data":
			data = append(data, "")
		case strings.HasPrefix(text, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(text, "data:"), " "))
		case text == "event":
			eventName = ""
		case strings.HasPrefix(text, "event:"):
			eventName = strings.TrimPrefix(strings.TrimPrefix(text, "event:"), " ")
		}
		if readErr != nil {
			rt.budget.remaining -= counted.read
			if errors.Is(readErr, io.EOF) {
				return payload, nil
			}
			return nil, directRemoteTransportError(rt.ctx, readErr)
		}
	}
}

func directRemoteSSEEventIsResponse(eventName string, data []string) bool {
	if (eventName != "" && eventName != "message") || len(data) == 0 {
		return false
	}
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &message); err != nil {
		return false
	}
	return len(message.ID) > 0 && message.Method == "" && (message.Result != nil || message.Error != nil)
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
// inspection response budget. The boolean reports whether attachment can use
// the automatic registration path the category names.
func directRemoteOAuthDiscovery(ctx context.Context, policy *guardian.Policy, client directRemoteHTTPClient, resourceURL string) (string, bool, error) {
	available := false
	// A dynamic registration endpoint only the dashboard can use is kept as a
	// fallback while later servers are checked, because attachment skips such
	// a server and connects the first one it can use.
	fallback := ""
	for _, metadataURL := range directRemoteProtectedResourceMetadataURLs(resourceURL) {
		metadata, status, err := directRemoteGetJSON(ctx, policy, client, metadataURL)
		if transientDirectRemoteMetadataStatus(status) {
			err = setupFailure(SetupCategoryTemporarilyUnavailable, ErrDirectRemoteUnavailable)
		}
		if setupCategoryFromError(err) == SetupCategoryTemporarilyUnavailable {
			if fallback != "" {
				return fallback, false, nil
			}
			if available {
				return oauthDiscoveryAvailable, false, nil
			}
			return "", false, err
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
					if fallback != "" {
						return fallback, false, nil
					}
					if available {
						return oauthDiscoveryAvailable, false, nil
					}
					return "", false, err
				}
				if err != nil || status != http.StatusOK {
					continue
				}
				discovery, attachmentSupported := directRemoteAutomaticRegistration(authorizationMetadata)
				if attachmentSupported {
					return discovery, true, nil
				}
				if discovery != "" {
					fallback = discovery
					continue
				}
				available = true
			}
		}
	}
	if fallback != "" {
		return fallback, false, nil
	}
	if available {
		return oauthDiscoveryAvailable, false, nil
	}
	return oauthDiscoveryIncomplete, false, nil
}

// directRemoteAutomaticRegistration names the automatic client registration
// path one authorization server's metadata offers, or "" when it offers none.
// It prefers the path attachment takes: dynamic client registration when
// attachment can use it, then a Client ID Metadata Document under the
// predicate the dashboard's automatic setup and attachment share. A valid
// dynamic registration endpoint attachment cannot use still counts, since the
// dashboard's automatic setup registers public clients through it; the boolean
// is false only in that case, because attachment cannot connect it.
func directRemoteAutomaticRegistration(metadata map[string]any) (string, bool) {
	endpoint, _ := metadata["registration_endpoint"].(string)
	supported, _ := metadata["client_id_metadata_document_supported"].(bool)
	var methods []string
	if advertised, ok := metadata["token_endpoint_auth_methods_supported"].([]any); ok {
		methods = make([]string, 0, len(advertised))
		for _, method := range advertised {
			if name, ok := method.(string); ok {
				methods = append(methods, name)
			}
		}
	}
	switch {
	case attachmentCanUseDynamicRegistration(endpoint, methods):
		return oauthDiscoveryAvailableDCR, true
	case remotesessions.SupportsClientIDMetadataDocument(supported, methods):
		return oauthDiscoveryAvailableCIMD, true
	case validDynamicClientRegistrationEndpoint(endpoint):
		return oauthDiscoveryAvailableDCR, false
	default:
		return "", false
	}
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
	if _, err := policy.ValidateHTTPSURL(ctx, canonicalURL, guardian.WithInternalCatalog()); err != nil {
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
		AttachmentSupported:    false,
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
