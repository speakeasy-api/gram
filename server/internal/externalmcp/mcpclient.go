package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"
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

// ClientOptions contains options for creating an MCP client.
type ClientOptions struct {
	// Authorization is the value for the Authorization header (e.g., "Bearer token").
	// If empty, no Authorization header is sent.
	Authorization string
}

// Client represents an active connection to an external MCP server.
type Client struct {
	logger    *slog.Logger
	remoteURL string
	session   *mcp.ClientSession
	authRT    *authRoundTripper
}

// NewClient creates a new client connection to an external MCP server.
// This performs the MCP protocol initialization internally.
func NewClient(ctx context.Context, logger *slog.Logger, remoteURL string, transportType TransportType, opts *ClientOptions) (*Client, error) {
	if opts == nil {
		opts = &ClientOptions{
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

	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient.Transport = authRT

	httpClient := retryClient.StandardClient()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "gram-server",
		Version: "1.0.0",
		Title:   "",
	}, nil)

	var transport mcp.Transport
	switch transportType {
	case TransportTypeStreamableHTTP:
		transport = &mcp.StreamableClientTransport{
			Endpoint:   remoteURL,
			HTTPClient: httpClient,
			MaxRetries: 3,
		}
	case TransportTypeSSE:
		transport = &mcp.SSEClientTransport{
			Endpoint:   remoteURL,
			HTTPClient: httpClient,
		}
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if authRT.authRequired {
			return nil, &AuthRequiredError{
				RemoteURL:       remoteURL,
				WWWAuthenticate: authRT.wwwAuthenticate,
			}
		}
		return nil, fmt.Errorf("connect to external mcp server: %w", err)
	}

	logger.InfoContext(ctx, "connected to external MCP server")

	return &Client{
		logger:    logger,
		remoteURL: remoteURL,
		session:   session,
		authRT:    authRT,
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	if err := c.session.Close(); err != nil {
		return fmt.Errorf("close external mcp client: %w", err)
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
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	toolsResult, err := c.session.ListTools(ctx, nil)
	if err != nil {
		if c.authRT.authRequired {
			return nil, &AuthRequiredError{
				RemoteURL:       c.remoteURL,
				WWWAuthenticate: c.authRT.wwwAuthenticate,
			}
		}
		return nil, fmt.Errorf("list tools from external mcp server: %w", err)
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

		tools = append(tools, Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Schema:      schema,
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

	callResult, err := c.session.CallTool(ctx, &mcp.CallToolParams{
		Meta:      mcp.Meta{},
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		if c.authRT.authRequired {
			return nil, &AuthRequiredError{
				RemoteURL:       c.remoteURL,
				WWWAuthenticate: c.authRT.wwwAuthenticate,
			}
		}
		return nil, fmt.Errorf("call tool on external mcp server: %w", err)
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
		return nil, fmt.Errorf("external mcp round trip: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		rt.authRequired = true
		rt.wwwAuthenticate = resp.Header.Get("WWW-Authenticate")
	}

	return resp, nil
}
