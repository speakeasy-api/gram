package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type RegistryBackend interface {
	Match(req *http.Request) bool
	Authorize(req *http.Request) error
}

// RegistryClient handles communication with external MCP registries.
type RegistryClient struct {
	httpClient *http.Client
	logger     *slog.Logger
	backend    RegistryBackend
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(logger *slog.Logger, tracerProvider trace.TracerProvider, backend RegistryBackend) *RegistryClient {
	return &RegistryClient{
		httpClient: &http.Client{
			Transport: otelhttp.NewTransport(
				retryablehttp.NewClient().StandardClient().Transport,
				otelhttp.WithTracerProvider(tracerProvider),
			),
		},
		logger:  logger.With(attr.SlogComponent("mcp-registry-client")),
		backend: backend,
	}
}

// Registry represents an MCP registry endpoint.
type Registry struct {
	ID  uuid.UUID
	URL string
}

// ListServersParams contains optional parameters for listing servers.
type ListServersParams struct {
	Search *string
	Cursor *string
}

// listResponse represents the response from the MCP registry API.
type listResponse struct {
	Servers  []serverEntry `json:"servers"`
	Metadata struct {
		Count      int     `json:"count"`
		NextCursor *string `json:"nextCursor"`
	} `json:"metadata"`
}

type serverEntry struct {
	Server serverJSON `json:"server"`
	Meta   any        `json:"_meta"`
}

type serverJSON struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     string  `json:"version"`
	Title       *string `json:"title"`
	WebsiteURL  *string `json:"websiteUrl"`
	Icons       []struct {
		Src string `json:"src"`
	} `json:"icons"`
	Remotes []serverRemote `json:"remotes"`
}

type serverRemote struct {
	URL   string       `json:"url"`
	Type  string       `json:"type"`
	Tools []serverTool `json:"tools"`
}

type serverTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations map[string]any  `json:"annotations"`
}

// ListServers fetches servers from the given registry.
func (c *RegistryClient) ListServers(ctx context.Context, registry Registry, params ListServersParams) ([]*types.ExternalMCPServer, error) {
	reqURL := fmt.Sprintf("%s/v0.1/servers?version=latest&limit=50", registry.URL)
	if params.Search != nil && *params.Search != "" {
		reqURL += fmt.Sprintf("&search=%s", *params.Search)
	}
	if params.Cursor != nil && *params.Cursor != "" {
		reqURL += fmt.Sprintf("&cursor=%s", *params.Cursor)
	}

	c.logger.InfoContext(ctx, "fetching servers from registry", attr.SlogMCPRegistryURL(reqURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create list servers request: %w", err)
	}

	if c.backend.Match(req) {
		if err := c.backend.Authorize(req); err != nil {
			return nil, fmt.Errorf("authorize list servers request: %w", err)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from registry: %w", err)
	}
	defer o11y.LogDefer(ctx, c.logger, func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var listResp listResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("failed to decode registry response: %w", err)
	}

	registryID := registry.ID.String()
	servers := make([]*types.ExternalMCPServer, 0, len(listResp.Servers))
	for _, s := range listResp.Servers {
		var iconURL *string
		if len(s.Server.Icons) > 0 {
			iconURL = &s.Server.Icons[0].Src
		}

		tools := make([]*types.ExternalMCPTool, 0, len(s.Server.Remotes[0].Tools))
		for _, tool := range s.Server.Remotes[0].Tools {
			tools = append(tools, &types.ExternalMCPTool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
				Annotations: tool.Annotations,
			})
		}

		server := &types.ExternalMCPServer{
			RegistrySpecifier: s.Server.Name,
			Version:           s.Server.Version,
			Description:       s.Server.Description,
			RegistryID:        registryID,
			Title:             s.Server.Title,
			IconURL:           iconURL,
			Meta:              s.Meta,
			Tools:             tools,
		}

		servers = append(servers, server)
	}

	hackilyEnrichWithLogos(servers)
	return servers, nil
}

// hackilyEnrichWithLogos patches servers with hardcoded logo URLs for servers
// that don't have icons from the registry.
func hackilyEnrichWithLogos(servers []*types.ExternalMCPServer) {
	for _, s := range servers {
		if s.IconURL == nil {
			if logo, ok := HardcodedLogos[s.RegistrySpecifier]; ok {
				s.IconURL = &logo
			}
		}
	}
}

// ServerDetails contains detailed information about an MCP server including connection info.
type ServerDetails struct {
	Name          string
	Description   string
	Version       string
	RemoteURL     string
	TransportType externalmcptypes.TransportType
	Tools         []serverTool
}

// getServerResponse wraps a single server from the registry.
type getServerResponse struct {
	Server serverJSON `json:"server"`
}

// GetServerDetails fetches server details including the remote URL from the registry.
func (c *RegistryClient) GetServerDetails(ctx context.Context, registry Registry, serverName string) (*ServerDetails, error) {
	u, err := url.Parse(registry.URL)
	if err != nil {
		return nil, oops.Permanent(fmt.Errorf("parse external mcp registry url: %w", err))
	}
	u = u.JoinPath("v0.1", "servers", url.PathEscape(serverName), "versions", "latest")

	c.logger.InfoContext(ctx, "fetching server details from registry", attr.SlogURL(u.String()))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, oops.Permanent(fmt.Errorf("create external mcp server details request: %w", err))
	}

	if c.backend.Match(req) {
		if err := c.backend.Authorize(req); err != nil {
			return nil, fmt.Errorf("authorize external mcp server details request: %w", err)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send external mcp server details request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("registry returned status %d", resp.StatusCode)
		isRetryable := resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode < 600
		if !isRetryable {
			err = oops.Permanent(err)
		}
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read external mcp server details response: %w", err)
	}

	var serverResp getServerResponse
	if err := json.Unmarshal(body, &serverResp); err != nil {
		return nil, fmt.Errorf("decode external mcp server details response: %w", err)
	}

	// Find the remote URL, preferring streamable-http over sse
	var remoteURL string
	var transportType externalmcptypes.TransportType
	var tools []serverTool
	for _, remote := range serverResp.Server.Remotes {
		if remote.Type == "streamable-http" {
			remoteURL = remote.URL
			transportType = externalmcptypes.TransportTypeStreamableHTTP
			tools = remote.Tools
			break
		} else if remote.Type == "sse" {
			remoteURL = remote.URL
			transportType = externalmcptypes.TransportTypeSSE
			tools = remote.Tools
		}
	}

	if remoteURL == "" {
		return nil, oops.Permanent(fmt.Errorf("server %s has no streamable-http or sse remote", serverName))
	}

	return &ServerDetails{
		Name:          serverResp.Server.Name,
		Description:   serverResp.Server.Description,
		Version:       serverResp.Server.Version,
		RemoteURL:     remoteURL,
		TransportType: transportType,
		Tools:         tools,
	}, nil
}
