package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"go.opentelemetry.io/otel"
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
	// Metrics optionally injects recovery instruments; nil uses the process OTel provider.
	Metrics *Metrics
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
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
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
	transportType  types.TransportType
	options        ClientOptions
	// Fresh definitions discovered on this isolated session override persisted metadata.
	discovered map[string]json.RawMessage
}

func classifyRequestError(op, remoteURL string, authRT *authRoundTripper, bodyLimitRT *bodyLimitRoundTripper, err error) error {
	if authRT.authRejected {
		return &AuthRejectedError{RemoteURL: remoteURL, StatusCode: authRT.statusCode, WWWAuthenticate: authRT.wwwAuthenticate}
	}
	if bodyLimitRT != nil && bodyLimitRT.tripped.Load() {
		return fmt.Errorf("%s: %w", op, ErrResponseTooLarge)
	}
	// SDK response decoding may include raw numeric values or remote field
	// names. Retain typed protocol errors, but redact typed JSON diagnostics.
	switch {
	case isJSONDiagnostic(err):
		err = safeJSONDiagnostic(err)
	default:
		if rpcErr, ok := errors.AsType[*jsonrpc.Error](err); ok && rpcErr.Code == mcp.CodeHeaderMismatch {
			err = &jsonrpc.Error{Code: rpcErr.Code, Message: "external MCP parameter header mismatch", Data: nil}
		}
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
			Metrics:          nil,
			Authorization:    "",
			Headers:          nil,
			DisableRetries:   false,
			MaxResponseBytes: 0,
		}
	}

	// Recovery must retain the exact effective configuration, not a mutable caller map.
	options := *opts
	options.Headers = maps.Clone(opts.Headers)
	if options.Metrics == nil {
		options.Metrics = NewMetrics(otel.GetMeterProvider(), logger)
	}
	opts = &options

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
		transportType:  transportType,
		options:        options,
		discovered:     make(map[string]json.RawMessage),
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

// Bound the aggregate discovery before retaining remote metadata across pages.
const (
	maxListedTools       = 1000    // 1,000 tools bounds retained definitions and per-tool validation.
	maxListedPages       = 1000    // 1,000 pages also bounds empty-page requests and cursor storage.
	maxListedSchemaBytes = 8 << 20 // 8 MiB caps aggregate schema bytes across pages, including skipped tools.
)

// ListTools lists available tools from the external MCP server.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	return c.listTools(ctx, "")
}

// listTools follows cursors until exhausted, or until the requested tool is found.
// A fresh recovery Client is required: ListTools on this session can hit the SDK TTL cache.
func (c *Client) listTools(ctx context.Context, target string) ([]Tool, error) {
	// Do not consult a previous session listing when deciding whether this
	// discovery found its target. Failed discovery also discards local metadata.
	c.discovered = make(map[string]json.RawMessage)
	var tools []Tool
	schemaBytes := 0
	listedTools := 0
	complete := false
	defer func() {
		if !complete {
			c.discovered = make(map[string]json.RawMessage)
		}
	}()
	seen := make(map[string]bool)
	cursor := ""
	for page := 0; ; page++ {
		// Empty pages must not permit unbounded pagination or cursor retention.
		if page >= maxListedPages {
			return nil, errors.New("external mcp tools/list exceeds page limit")
		}
		if seen[cursor] {
			return nil, errors.New("external mcp tools/list pagination did not terminate")
		}
		seen[cursor] = true
		c.beginRequest()
		toolsResult, err := c.session.ListTools(ctx, &mcp.ListToolsParams{Meta: nil, Cursor: cursor})
		if err != nil {
			return nil, classifyRequestError("list tools from external mcp server", c.remoteURL, c.authRT, c.bodyLimitRT, err)
		}
		if len(toolsResult.Tools) > maxListedTools-listedTools {
			return nil, errors.New("external mcp tools/list exceeds tool limit")
		}
		listedTools += len(toolsResult.Tools)
		for _, tool := range toolsResult.Tools {
			schema, err := json.Marshal(tool.InputSchema)
			if err != nil {
				c.logger.WarnContext(ctx, "skipping invalid external mcp tool schema", attr.SlogError(safeJSONDiagnostic(err)))
				continue
			}
			// Charge every marshaled definition, including skipped tools, so
			// oversized schemas cannot bypass the aggregate discovery budget.
			if len(schema) > maxListedSchemaBytes-schemaBytes {
				return nil, errors.New("external mcp tools/list exceeds schema byte limit")
			}
			schemaBytes += len(schema)
			// Within the aggregate budget, an unusable definition must not hide
			// its neighbors. Never call this tool with incomplete headers.
			if len(schema) > maxParameterHeaderSchemaBytes {
				c.logger.WarnContext(ctx, "skipping oversized external mcp tool schema")
				continue
			}
			if _, err := parameterHeaders(schema, nil); err != nil {
				// Reject only this tool: never call it with incomplete parameter headers.
				c.logger.WarnContext(ctx, "skipping unsafe external mcp tool schema", attr.SlogError(err))
				continue
			}
			c.discovered[tool.Name] = schema

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

		if target != "" {
			if _, ok := c.discovered[target]; ok {
				break
			}
		}
		if toolsResult.NextCursor == "" {
			break
		}
		cursor = toolsResult.NextCursor
	}

	c.logger.InfoContext(ctx, "listed tools from external MCP server",
		attr.SlogValueInt(len(tools)),
	)

	if target != "" {
		if _, ok := c.discovered[target]; !ok {
			return nil, errors.New("external mcp tool definition absent or invalid")
		}
	}
	complete = true
	return tools, nil
}

// CallToolResult represents the result of a tool call.
type CallToolResult struct {
	Content []json.RawMessage
	IsError bool
}

// CallTool calls a tool on the external MCP server. A nil schema deliberately
// sends an ordinary call first, without discovery. Only conformant pre-execution
// HeaderMismatch responses permit one discovery and replay. Proxy schemas are not
// persisted, so later isolated calls may need the same recovery again.
func (c *Client) CallTool(ctx context.Context, toolName string, arguments json.RawMessage, schema json.RawMessage) (*CallToolResult, error) {
	c.logger.InfoContext(ctx, "calling tool on external MCP server",
		attr.SlogToolName(toolName),
	)

	// Keep numbers as raw JSON, including integers beyond float64 precision.
	var args map[string]json.RawMessage
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid external mcp tool arguments: %w", safeJSONDiagnostic(err))
		}
	} else {
		arguments = json.RawMessage("{}")
	}

	// Fresh discovery is authoritative metadata, but the SDK's header generator
	// is not: v1.7.0 drops all bindings when an unrelated property is a union or
	// boolean schema. Derive complete request headers from the original schema
	// in every path, including discovery and recovery.
	if discovered, known := c.discovered[toolName]; known {
		schema = discovered
	}

	callResult, err := c.callTool(ctx, toolName, arguments, schema)
	if rpcErr, ok := errors.AsType[*jsonrpc.Error](err); ok && rpcErr.Code == mcp.CodeHeaderMismatch {
		// Do not log the upstream mismatch message: it can contain parameter values.
		c.logger.InfoContext(ctx, "external mcp header mismatch; refreshing tool metadata")
		c.options.Metrics.RecordRecoveryAttempt(ctx)
		callResult, err = c.recoverTool(ctx, toolName, arguments)
		outcome := "success"
		if err != nil || callResult.IsError {
			outcome = "failed"
			c.options.Metrics.RecordRecoveryFailure(ctx)
		} else {
			c.options.Metrics.RecordRecoverySuccess(ctx)
		}
		c.logger.InfoContext(ctx, "external mcp header mismatch recovery completed", attr.SlogOutcome(outcome))
	}
	if err != nil {
		return nil, err
	}

	// Marshal each content item back to JSON
	content := make([]json.RawMessage, 0, len(callResult.Content))
	for _, item := range callResult.Content {
		itemJSON, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("marshal external mcp tool result: %w", safeJSONDiagnostic(err))
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

// recoverTool handles a conformant pre-execution HeaderMismatch rejection. It
// creates a fresh authenticated session so tools/list cannot use the old SDK
// session's cached definitions, discovers authoritative metadata, then replays
// exactly once. It never retries transport failures or tool execution errors.
// Successful recovery adopts this session locally; authenticated sessions and
// discovered schemas are never shared between clients.
func (c *Client) recoverTool(ctx context.Context, name string, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	delete(c.discovered, name)
	recovery, err := NewClient(ctx, c.logger, c.guardianPolicy, c.remoteURL, c.transportType, &c.options)
	if err != nil {
		return nil, err
	}
	adopted := false
	defer func() {
		if !adopted {
			_ = recovery.Close()
		}
	}()
	if err := recovery.discoverTool(ctx, name); err != nil {
		return nil, err
	}
	// Use only refreshed metadata, never the stale schema or generated headers.
	result, err := recovery.callTool(ctx, name, arguments, recovery.discovered[name])
	if err != nil {
		// Only a replay mismatch exhausts recovery; discovery may independently
		// fail with a protocol error before a second tools/call was attempted.
		if rpcErr, ok := errors.AsType[*jsonrpc.Error](err); ok && rpcErr.Code == mcp.CodeHeaderMismatch {
			c.options.Metrics.RecordRecoveryExhaustion(ctx)
		}
		return nil, err
	}
	// A wire-level mismatch can close the SDK's original session. Transfer the
	// successful isolated session and its metadata to this caller only, so reuse
	// does not select stale schemas or a closed transport.
	_ = c.session.Close()
	c.session = recovery.session
	c.authRT = recovery.authRT
	c.bodyLimitRT = recovery.bodyLimitRT
	c.discovered = recovery.discovered
	adopted = true
	return result, nil
}

func (c *Client) discoverTool(ctx context.Context, name string) error {
	if _, err := c.listTools(ctx, name); err != nil {
		return err
	}
	if _, ok := c.discovered[name]; !ok {
		return errors.New("external mcp tool definition absent or invalid")
	}
	return nil
}

// callTool performs exactly one attempt. It must not invoke recovery recursively.
func (c *Client) callTool(ctx context.Context, name string, arguments json.RawMessage, schema json.RawMessage) (*mcp.CallToolResult, error) {
	var headers http.Header
	if len(schema) > 0 {
		var err error
		headers, err = parameterHeaders(schema, arguments)
		if err != nil {
			return nil, err
		}
	}
	ctx = context.WithValue(ctx, toolCallContextKey{}, headers)
	c.beginRequest()
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Meta: nil, Name: name, Arguments: arguments, InputResponses: nil, RequestState: ""})
	if err != nil {
		// SDK mismatch messages can echo Mcp-Param values. Preserve the typed
		// protocol code for recovery/callers, but never forward those values to logging.
		if rpcErr, ok := errors.AsType[*jsonrpc.Error](err); ok && rpcErr.Code == mcp.CodeHeaderMismatch {
			c.options.Metrics.RecordHeaderMismatch(ctx)
			err = &jsonrpc.Error{Code: rpcErr.Code, Message: "external MCP parameter header mismatch", Data: nil}
		}
		return nil, classifyRequestError("call tool on external mcp server", c.remoteURL, c.authRT, c.bodyLimitRT, err)
	}
	return result, nil
}

// Carries only request-local adapter headers, never values in shared transport configuration.
// Its presence also disables generic HTTP retries for possibly effectful tools/call.
type toolCallContextKey struct{}

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
	sdkMethod := req.Header.Get(headerMCPMethod)
	sdkProtocolVersion := req.Header.Get("Mcp-Protocol-Version")
	discoverProbe := sdkMethod == methodServerDiscover

	if rt.authorization != "" || len(rt.headers) > 0 {
		req = req.Clone(req.Context())
		if rt.authorization != "" {
			req.Header.Set("Authorization", rt.authorization)
		}
		for k, v := range rt.headers {
			// Reserve the parameter namespace, even when refreshed annotations vanish.
			if !strings.HasPrefix(strings.ToLower(k), "mcp-param-") {
				req.Header.Set(k, v)
			}
		}
	}

	if headers, ok := req.Context().Value(toolCallContextKey{}).(http.Header); ok &&
		sdkMethod == "tools/call" && sdkProtocolVersion >= "2026-07-28" {
		req = req.Clone(req.Context())
		// Replace, do not merge with, SDK-generated parameters. The complete
		// schema-derived set also clears stale/incorrect SDK values for removed
		// annotations and absent arguments, and overrides its empty-string bug.
		for name := range req.Header {
			if strings.HasPrefix(strings.ToLower(name), "mcp-param-") {
				delete(req.Header, name)
			}
		}
		for name, values := range headers {
			req.Header[name] = append([]string(nil), values...)
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

// discoverAwareRetryConfig disables generic retries for the capability probe
// and effectful tools/call requests (which only recover typed HeaderMismatch).
//
// A rejected probe is the expected answer from any upstream that predates MCP
// 2026-07-28, and the SDK answers it by falling back to the legacy initialize
// handshake. Retrying a rejection that will not change spends the whole backoff
// budget first: five attempts and roughly 15 seconds, on a client that connects
// once per tool call. Discovery and the fallback handshake keep their retry
// budget; ambiguous tools/call failures must not duplicate execution.
func discoverAwareRetryConfig(logger *slog.Logger, remoteURL string) *guardian.RetryConfig {
	config := guardian.DefaultRetryConfig()
	checkRetry := config.CheckRetry

	config.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if _, call := ctx.Value(toolCallContextKey{}).(http.Header); call {
			return false, nil
		}
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

// safeJSONDiagnostic keeps useful decoder categories without copying raw values,
// schema field names, or errors from a custom JSON marshaler into diagnostics.
func safeJSONDiagnostic(err error) error {
	if e, ok := errors.AsType[*json.SyntaxError](err); ok {
		return fmt.Errorf("JSON syntax error at byte offset %d", e.Offset)
	}
	if e, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return fmt.Errorf("JSON type mismatch at byte offset %d (destination %s)", e.Offset, e.Type)
	}
	if e, ok := errors.AsType[*json.UnsupportedTypeError](err); ok {
		return fmt.Errorf("unsupported JSON type %s", e.Type)
	}
	if _, ok := errors.AsType[*json.UnsupportedValueError](err); ok {
		return errors.New("unsupported JSON value")
	}
	return errors.New("JSON encoding or decoding failed")
}

func isJSONDiagnostic(err error) bool {
	if _, ok := errors.AsType[*json.SyntaxError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*json.UnsupportedTypeError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*json.UnsupportedValueError](err); ok {
		return true
	}
	_, ok := errors.AsType[*json.MarshalerError](err)
	return ok
}
