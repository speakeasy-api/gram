package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
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
	// MaxResponseBytes caps the total size of each HTTP response body,
	// including a whole SSE stream, when greater than zero. It is intended for
	// short-lived, untrusted probes.
	MaxResponseBytes int64

	// MaxMessageBytes caps each discrete upstream message: a whole non-SSE
	// response body (results and error bodies alike) or a single SSE event.
	// Zero or below selects defaultMaxMessageBytes. The cap cannot be disabled
	// because the MCP SDK buffers every message in full before parsing it.
	MaxMessageBytes int64
}

// defaultMaxMessageBytes bounds one upstream MCP message when the caller sets
// no cap. Realistic messages sit well under it, while it keeps a hostile
// server from growing the shared API or worker process without bound.
const defaultMaxMessageBytes = 16 << 20 // 16 MiB

// disabledTransportRetries turns off the transport's reconnect loop. Callers
// bound a probe with their own deadline, and an unreachable host should spend
// one connection attempt. The SDK reads zero as "use the default of 5".
const disabledTransportRetries = -1

var ErrResponseTooLarge = errors.New("external mcp response body exceeds the configured read limit")

type bodyLimitRoundTripper struct {
	base http.RoundTripper

	// messageLimit caps each non-SSE body and each SSE event.
	messageLimit int64

	// totalLimit caps each whole response body when greater than zero.
	totalLimit int64

	// tripped records that a limit rejected a response since the last request.
	tripped atomic.Bool
}

func (rt *bodyLimitRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err //nolint:wrapcheck // preserve the base transport's error
	}
	body := resp.Body
	if rt.totalLimit > 0 {
		body = http.MaxBytesReader(nil, body, rt.totalLimit)
	}
	if isEventStream(resp) {
		body = &sseEventLimiter{ReadCloser: body, limit: rt.messageLimit, eventBytes: 0, lineBlank: true, err: nil}
	} else {
		body = http.MaxBytesReader(nil, body, rt.messageLimit)
	}
	resp.Body = &limitedBody{ReadCloser: body, tripped: &rt.tripped}
	return resp, nil
}

// isEventStream mirrors the SDK's dispatch: only a successful response whose
// media type is text/event-stream is consumed event by event. The SDK buffers
// every other body it reads, error bodies included, in full.
func isEventStream(resp *http.Response) bool {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return err == nil && mediaType == "text/event-stream"
}

// sseEventLimiter fails a text/event-stream body once a single event exceeds
// limit bytes, so a long-lived stream stays usable while no one event can grow
// the SDK's per-event buffer without bound. Like the SDK's scanner, it splits
// lines on LF only and treats a line holding nothing but CR bytes as the blank
// line that ends an event. Every byte since the previous blank line counts,
// comment and field lines included.
type sseEventLimiter struct {
	io.ReadCloser

	limit int64

	// eventBytes counts the bytes read since the last blank line.
	eventBytes int64

	// lineBlank reports whether the current line holds only CR bytes so far.
	lineBlank bool

	// err is sticky: once the limit trips, every later read fails too.
	err error
}

func (l *sseEventLimiter) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	n, err := l.ReadCloser.Read(p)
	for i, b := range p[:n] {
		l.eventBytes++
		if l.eventBytes > l.limit {
			l.err = &http.MaxBytesError{Limit: l.limit}
			return i, l.err
		}
		switch {
		case b == '\n':
			if l.lineBlank {
				l.eventBytes = 0
			}
			l.lineBlank = true
		case b != '\r':
			l.lineBlank = false
		}
	}
	return n, err //nolint:wrapcheck // io.EOF must be returned unchanged
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
	logger      *slog.Logger
	remoteURL   string
	session     *mcp.ClientSession
	authRT      *authRoundTripper
	bodyLimitRT *bodyLimitRoundTripper
}

func classifyRequestError(op, remoteURL string, authRT *authRoundTripper, bodyLimitRT *bodyLimitRoundTripper, err error) error {
	if authRT.authRejected {
		return &AuthRejectedError{RemoteURL: remoteURL, StatusCode: authRT.statusCode, WWWAuthenticate: authRT.wwwAuthenticate}
	}
	if bodyLimitRT.tripped.Load() {
		return fmt.Errorf("%s: %w", op, ErrResponseTooLarge)
	}
	// SDK response decoding may include raw numeric values or remote field
	// names, so redact typed JSON diagnostics.
	if isJSONDiagnostic(err) {
		err = safeJSONDiagnostic(err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func (c *Client) beginRequest() {
	c.bodyLimitRT.tripped.Store(false)
}

// NewClient creates a new client connection to an external MCP server.
// This performs the MCP protocol initialization internally.
func NewClient(ctx context.Context, logger *slog.Logger, guardianPolicy *guardian.Policy, remoteURL string, opts *ClientOptions) (*Client, error) {
	if opts == nil {
		opts = &ClientOptions{MaxResponseBytes: 0, MaxMessageBytes: 0}
	}

	logger.InfoContext(ctx, "connecting to external MCP server", attr.SlogURL(remoteURL))

	httpClient := guardianPolicy.PooledClient(guardian.WithInternalCatalog())
	authRT := &authRoundTripper{
		base:            httpClient.Transport,
		authRejected:    false,
		statusCode:      0,
		wwwAuthenticate: "",
	}
	messageLimit := opts.MaxMessageBytes
	if messageLimit <= 0 {
		messageLimit = defaultMaxMessageBytes
	}
	//nolint:exhaustruct // atomic.Bool must retain its documented zero value.
	bodyLimitRT := &bodyLimitRoundTripper{base: authRT, messageLimit: messageLimit, totalLimit: opts.MaxResponseBytes}
	httpClient.Transport = bodyLimitRT

	client := mcp.NewClient(&mcp.Implementation{
		Name:        "gram-server",
		Version:     "1.0.0",
		Title:       "",
		Description: "",
		WebsiteURL:  "https://getgram.ai",
		Icons:       nil,
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint:             remoteURL,
		HTTPClient:           httpClient,
		MaxRetries:           disabledTransportRetries,
		DisableStandaloneSSE: true,
		OAuthHandler:         nil,
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, classifyRequestError("connect to external mcp server", remoteURL, authRT, bodyLimitRT, err)
	}

	logger.InfoContext(ctx, "connected to external MCP server")

	return &Client{
		logger:      logger,
		remoteURL:   remoteURL,
		session:     session,
		authRT:      authRT,
		bodyLimitRT: bodyLimitRT,
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
	maxToolSchemaBytes   = 1 << 20 // 1 MiB skips a single oversized definition without failing the listing.
)

// ListTools lists available tools from the external MCP server, following
// cursors until exhausted.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var tools []Tool
	schemaBytes := 0
	listedTools := 0
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
			// its neighbors.
			if len(schema) > maxToolSchemaBytes {
				c.logger.WarnContext(ctx, "skipping oversized external mcp tool schema")
				continue
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

		if toolsResult.NextCursor == "" {
			break
		}
		cursor = toolsResult.NextCursor
	}

	c.logger.InfoContext(ctx, "listed tools from external MCP server",
		attr.SlogValueInt(len(tools)),
	)

	return tools, nil
}

type authRoundTripper struct {
	base            http.RoundTripper
	authRejected    bool
	statusCode      int
	wwwAuthenticate string
}

func (rt *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
