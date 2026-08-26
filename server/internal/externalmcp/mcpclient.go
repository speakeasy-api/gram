package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// AuthRejectedError is returned when an MCP server rejects authentication (401 or 403).
// WWWAuthenticate is populated when the server provides a WWW-Authenticate header.
type AuthRejectedError struct {
	RemoteURL       string
	StatusCode      int
	WWWAuthenticate string
}

func (e *AuthRejectedError) Error() string {
	return fmt.Sprintf("authentication rejected by MCP server %s (status %d)", e.RemoteURL, e.StatusCode)
}

// ClientOptions contains options for creating an MCP client.
type ClientOptions struct {
	// Authorization is the value for the Authorization header (e.g., "Bearer token").
	// If empty, no Authorization header is sent.
	Authorization string
	// Headers contains additional HTTP headers to send with each request.
	// Keys are header names, values are header values.
	Headers map[string]string
	// DisableRetries skips both the HTTP-transport retry layer and the MCP
	// transport's own connection retries. The two compound (up to 4 HTTP
	// attempts per MCP-level retry, each with its own backoff), which is
	// fine for the resilient gateway proxy path but defeats a caller-imposed
	// context deadline meant to bound a one-shot interactive probe — without
	// this, an unreachable server can take minutes to report as such instead
	// of the ~10s the probe intends.
	DisableRetries bool
	// MaxResponseBytes caps each HTTP response when greater than zero. It is
	// intended for short-lived, untrusted probes; the long-lived gateway path
	// leaves this at zero.
	MaxResponseBytes int64
}

var ErrResponseTooLarge = errors.New("external mcp response body exceeds the configured read limit")

type bodyLimitRoundTripper struct {
	base    http.RoundTripper
	limit   int64
	tripped atomic.Bool
}

func (rt *bodyLimitRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err //nolint:wrapcheck // preserve the base transport's error
	}
	resp.Body = &limitedBody{ReadCloser: http.MaxBytesReader(nil, resp.Body, rt.limit), tripped: &rt.tripped}
	return resp, nil
}

type limitedBody struct {
	io.ReadCloser
	tripped *atomic.Bool
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		b.tripped.Store(true)
		return n, fmt.Errorf("%w: %w", ErrResponseTooLarge, err)
	}
	return n, err //nolint:wrapcheck // io.EOF must be returned unchanged
}

// Client represents an active connection to an external MCP server.
type Client struct {
	logger         *slog.Logger
	guardianPolicy *guardian.Policy
	remoteURL      string
	session        *mcp.ClientSession
	authRT         *authRoundTripper
	bodyLimitRT    *bodyLimitRoundTripper
}

func classifyRequestError(op, remoteURL string, authRT *authRoundTripper, bodyLimitRT *bodyLimitRoundTripper, err error) error {
	if authRT.authRejected {
		return &AuthRejectedError{RemoteURL: remoteURL, StatusCode: authRT.statusCode, WWWAuthenticate: authRT.wwwAuthenticate}
	}
	if bodyLimitRT != nil && bodyLimitRT.tripped.Load() {
		return fmt.Errorf("%s: %w", op, ErrResponseTooLarge)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (c *Client) beginRequest() {
	if c.bodyLimitRT != nil {
		c.bodyLimitRT.tripped.Store(false)
	}
}

// NewClient creates a new client connection to an external MCP server.
// This performs the MCP protocol initialization internally.
func NewClient(ctx context.Context, logger *slog.Logger, guardianPolicy *guardian.Policy, remoteURL string, transportType types.TransportType, opts *ClientOptions) (*Client, error) {
	if opts == nil {
		opts = &ClientOptions{
			Authorization:    "",
			Headers:          nil,
			DisableRetries:   false,
			MaxResponseBytes: 0,
		}
	}

	logger.InfoContext(ctx, "connecting to external MCP server", attr.SlogURL(remoteURL))

	var httpClient *guardian.HTTPClient
	if opts.DisableRetries {
		httpClient = guardianPolicy.PooledClient()
	} else {
		httpClient = guardianPolicy.PooledClient(guardian.WithRetryConfig(discoverAwareRetryConfig(logger, remoteURL)))
	}
	trasnport := httpClient.Transport
	authRT := &authRoundTripper{
		base:            trasnport,
		authorization:   opts.Authorization,
		headers:         opts.Headers,
		authRejected:    false,
		statusCode:      0,
		wwwAuthenticate: "",
	}
	httpClient.Transport = authRT
	var bodyLimitRT *bodyLimitRoundTripper
	if opts.MaxResponseBytes > 0 {
		//nolint:exhaustruct // atomic.Bool must retain its documented zero value.
		bodyLimitRT = &bodyLimitRoundTripper{base: authRT, limit: opts.MaxResponseBytes}
		httpClient.Transport = bodyLimitRT
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:        "gram-server",
		Version:     "1.0.0",
		Title:       "",
		Description: "",
		WebsiteURL:  "https://getgram.ai",
		Icons:       nil,
	}, nil)

	// mcp.StreamableClientTransport treats MaxRetries == 0 as "use the SDK's
	// own default of 5" — a negative value is required to actually disable
	// its reconnect-retry loop.
	maxRetries := 3
	if opts.DisableRetries {
		maxRetries = -1
	}

	var transport mcp.Transport
	switch transportType {
	case types.TransportTypeStreamableHTTP:
		transport = &mcp.StreamableClientTransport{
			Endpoint:             remoteURL,
			HTTPClient:           httpClient,
			MaxRetries:           maxRetries,
			DisableStandaloneSSE: true,
			OAuthHandler:         nil,
		}
	case types.TransportTypeSSE:
		transport = &mcp.SSEClientTransport{
			Endpoint:   remoteURL,
			HTTPClient: httpClient,
		}
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", transportType)
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, classifyRequestError("connect to external mcp server", remoteURL, authRT, bodyLimitRT, err)
	}

	logger.InfoContext(ctx, "connected to external MCP server")

	return &Client{
		logger:         logger,
		guardianPolicy: guardianPolicy,
		remoteURL:      remoteURL,
		session:        session,
		authRT:         authRT,
		bodyLimitRT:    bodyLimitRT,
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	if err := c.session.Close(); err != nil {
		return fmt.Errorf("close external mcp client: %w", err)
	}
	return nil
}

// ToolAnnotations contains MCP tool behavior hints.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// Tool represents a tool discovered from an external MCP server.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Annotations *ToolAnnotations
}

// ListTools lists available tools from the external MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	c.beginRequest()
	toolsResult, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return nil, classifyRequestError("list tools from external mcp server", c.remoteURL, c.authRT, c.bodyLimitRT, err)
	}

	tools := make([]Tool, 0, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			c.logger.WarnContext(ctx, "failed to marshal tool schema",
				attr.SlogToolName(tool.Name),
				attr.SlogError(err),
			)
			schema = []byte("{}")
		}

		// Extract annotations from MCP tool response
		var annotations *ToolAnnotations
		if tool.Annotations != nil {
			annotations = &ToolAnnotations{
				Title:           tool.Annotations.Title,
				ReadOnlyHint:    new(tool.Annotations.ReadOnlyHint),
				DestructiveHint: tool.Annotations.DestructiveHint, // already *bool
				IdempotentHint:  new(tool.Annotations.IdempotentHint),
				OpenWorldHint:   tool.Annotations.OpenWorldHint, // already *bool
			}
		}

		tools = append(tools, Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      schema,
			Annotations: annotations,
		})
	}

	c.logger.InfoContext(ctx, "listed tools from external MCP server",
		attr.SlogValueInt(len(tools)),
	)

	return tools, nil
}

// CallToolResult represents the result of a tool call.
type CallToolResult struct {
	Content []json.RawMessage
	IsError bool
}

// CallTool calls a tool on the external MCP server.
func (c *Client) CallTool(ctx context.Context, toolName string, arguments json.RawMessage) (*CallToolResult, error) {
	c.logger.InfoContext(ctx, "calling tool on external MCP server",
		attr.SlogToolName(toolName),
	)

	// Parse arguments into map for MCP SDK
	var args map[string]any
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return nil, fmt.Errorf("parse external mcp tool arguments: %w", err)
		}
	}

	c.beginRequest()
	callResult, err := c.session.CallTool(ctx, &mcp.CallToolParams{
		Meta:           mcp.Meta{},
		Name:           toolName,
		Arguments:      args,
		InputResponses: nil,
		RequestState:   "",
	})
	if err != nil {
		return nil, classifyRequestError("call tool on external mcp server", c.remoteURL, c.authRT, c.bodyLimitRT, err)
	}

	// Marshal each content item back to JSON
	content := make([]json.RawMessage, 0, len(callResult.Content))
	for _, item := range callResult.Content {
		itemJSON, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("marshal external mcp tool result: %w", err)
		}
		content = append(content, itemJSON)
	}

	c.logger.InfoContext(ctx, "tool call completed",
		attr.SlogToolName(toolName),
	)

	return &CallToolResult{
		Content: content,
		IsError: callResult.IsError,
	}, nil
}

type authRoundTripper struct {
	base          http.RoundTripper
	authorization string
	headers       map[string]string

	authRejected    bool
	statusCode      int
	wwwAuthenticate string
}

func (rt *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Configured headers are operator-supplied and are not yet reserved against
	// the protocol's own header names, so a definition naming Mcp-Method would
	// overwrite the SDK's value below. Classify the request from what the SDK
	// sent, before that can happen.
	discoverProbe := req.Header.Get(headerMCPMethod) == methodServerDiscover

	if rt.authorization != "" || len(rt.headers) > 0 {
		req = req.Clone(req.Context())
		if rt.authorization != "" {
			req.Header.Set("Authorization", rt.authorization)
		}
		for k, v := range rt.headers {
			req.Header.Set(k, v)
		}
	}

	if discoverProbe {
		req = req.WithContext(context.WithValue(req.Context(), discoverProbeContextKey{}, true))
	}

	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("external mcp round trip: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		rt.statusCode = resp.StatusCode
		if challenge := resp.Header.Get("WWW-Authenticate"); challenge != "" {
			rt.wwwAuthenticate = challenge
		}
		rt.authRejected = true
	}

	return resp, nil
}

const (
	// headerMCPMethod mirrors the JSON-RPC method of an MCP request into an
	// HTTP header. The SDK sets it only from protocol revision 2026-07-28
	// onwards, so the legacy initialize handshake never carries it.
	headerMCPMethod = "Mcp-Method"

	// methodServerDiscover is the capability probe the SDK sends before every
	// connect. An upstream that predates protocol revision 2026-07-28 rejects
	// it, and the SDK then falls back to the legacy initialize handshake on
	// the same connection.
	methodServerDiscover = "server/discover"
)

// discoverProbeContextKey marks a request context as belonging to the
// server/discover capability probe. The marker is set on the outbound request
// and read back by the retry policy, which only receives a context and a
// response and so cannot recognize the probe on a transport failure otherwise.
type discoverProbeContextKey struct{}

// discoverAwareRetryConfig returns the default retry configuration with one
// exception: the server/discover capability probe is never retried.
//
// A rejected probe is the expected answer from any upstream that predates MCP
// 2026-07-28, and the SDK answers it by falling back to the legacy initialize
// handshake. Retrying a rejection that will not change spends the whole backoff
// budget first: five attempts and roughly 15 seconds, on a client that connects
// once per tool call. Every other request, the fallback handshake and the tool
// call included, keeps the full retry budget.
func discoverAwareRetryConfig(logger *slog.Logger, remoteURL string) *guardian.RetryConfig {
	config := guardian.DefaultRetryConfig()
	checkRetry := config.CheckRetry

	config.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		retry, checkErr := checkRetry(ctx, resp, err)
		if !retry {
			return retry, checkErr
		}
		if probe, _ := ctx.Value(discoverProbeContextKey{}).(bool); !probe {
			return retry, checkErr
		}

		args := []any{attr.SlogURL(remoteURL)}
		if resp != nil {
			args = append(args, attr.SlogHTTPResponseStatusCode(resp.StatusCode))
		}
		if err != nil {
			args = append(args, attr.SlogError(err))
		}
		logger.InfoContext(ctx, "external mcp server rejected the capability probe, falling back to the legacy handshake", args...)

		return false, checkErr
	}

	return config
}
