package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// AuthRequiredError is returned when an external MCP server requires authentication.
type AuthRequiredError struct {
	RemoteURL       string
	WWWAuthenticate string
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("authentication required for MCP server %s", e.RemoteURL)
}

// IsAuthRequiredError checks if an error is an AuthRequiredError and returns it.
func IsAuthRequiredError(err error) (*AuthRequiredError, bool) {
	var authErr *AuthRequiredError
	if errors.As(err, &authErr) {
		return authErr, true
	}
	return nil, false
}

// SessionOptions contains options for creating an MCP session.
type SessionOptions struct {
	// Authorization is the value for the Authorization header (e.g., "Bearer token").
	// If empty, no Authorization header is sent.
	Authorization string
}

// Session represents an active connection to an external MCP server.
type Session struct {
	logger    *slog.Logger
	remoteURL string
	session   *mcp.ClientSession
	authRT    *authRoundTripper
}

// NewSession creates a new session to an external MCP server.
func NewSession(ctx context.Context, logger *slog.Logger, remoteURL string, opts *SessionOptions) (*Session, error) {
	if opts == nil {
		opts = &SessionOptions{
			Authorization: "",
		}
	}

	logger.InfoContext(ctx, "connecting to external MCP server", attr.SlogURL(remoteURL))

	authRT := &authRoundTripper{
		base:            http.DefaultTransport,
		authorization:   opts.Authorization,
		authRequired:    false,
		wwwAuthenticate: "",
	}

	httpClient := &http.Client{
		Transport: authRT,
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "gram-server",
		Version: "1.0.0",
		Title:   "",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint:   remoteURL,
		HTTPClient: httpClient,
		MaxRetries: 0,
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if authRT.authRequired {
			return nil, &AuthRequiredError{
				RemoteURL:       remoteURL,
				WWWAuthenticate: authRT.wwwAuthenticate,
			}
		}
		return nil, fmt.Errorf("failed to connect to MCP server: %w", err)
	}

	logger.InfoContext(ctx, "connected to external MCP server")

	return &Session{
		logger:    logger,
		remoteURL: remoteURL,
		session:   session,
		authRT:    authRT,
	}, nil
}

// Close closes the session.
func (s *Session) Close() error {
	if err := s.session.Close(); err != nil {
		return fmt.Errorf("close MCP session: %w", err)
	}
	return nil
}

// Tool represents a tool discovered from an external MCP server.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// ListTools lists available tools from the external MCP server.
func (s *Session) ListTools(ctx context.Context) ([]Tool, error) {
	toolsResult, err := s.session.ListTools(ctx, nil)
	if err != nil {
		if s.authRT.authRequired {
			return nil, &AuthRequiredError{
				RemoteURL:       s.remoteURL,
				WWWAuthenticate: s.authRT.wwwAuthenticate,
			}
		}
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	tools := make([]Tool, 0, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to marshal tool schema",
				attr.SlogToolName(tool.Name),
				attr.SlogError(err),
			)
			schema = []byte("{}")
		}

		tools = append(tools, Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      schema,
		})
	}

	s.logger.InfoContext(ctx, "listed tools from external MCP server",
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
func (s *Session) CallTool(ctx context.Context, toolName string, arguments json.RawMessage) (*CallToolResult, error) {
	s.logger.InfoContext(ctx, "calling tool on external MCP server",
		attr.SlogToolName(toolName),
	)

	// Parse arguments into map for MCP SDK
	var args map[string]any
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	}

	callResult, err := s.session.CallTool(ctx, &mcp.CallToolParams{
		Meta:      mcp.Meta{},
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		if s.authRT.authRequired {
			return nil, &AuthRequiredError{
				RemoteURL:       s.remoteURL,
				WWWAuthenticate: s.authRT.wwwAuthenticate,
			}
		}
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}

	// Marshal each content item back to JSON
	content := make([]json.RawMessage, 0, len(callResult.Content))
	for _, item := range callResult.Content {
		itemJSON, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tool result content: %w", err)
		}
		content = append(content, itemJSON)
	}

	s.logger.InfoContext(ctx, "tool call completed",
		attr.SlogToolName(toolName),
	)

	return &CallToolResult{
		Content: content,
		IsError: callResult.IsError,
	}, nil
}

// ListToolsFromProxy connects to an external MCP server and lists its tools.
// This is a convenience function that creates a session, lists tools, and closes the session.
// If authorization is required and not provided, tools requiring OAuth will be skipped with a warning.
func ListToolsFromProxy(ctx context.Context, logger *slog.Logger, remoteURL string, opts *SessionOptions) ([]Tool, error) {
	session, err := NewSession(ctx, logger, remoteURL, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	return session.ListTools(ctx)
}

// authRoundTripper is an http.RoundTripper that adds Authorization headers
// and captures 401 responses.
type authRoundTripper struct {
	base          http.RoundTripper
	authorization string

	// Captured from 401 response
	authRequired    bool
	wwwAuthenticate string
}

func (rt *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.authorization != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", rt.authorization)
	}

	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("round trip failed: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		rt.authRequired = true
		rt.wwwAuthenticate = resp.Header.Get("WWW-Authenticate")
	}

	return resp, nil
}
