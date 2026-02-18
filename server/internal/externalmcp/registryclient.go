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
	"github.com/speakeasy-api/gram/server/internal/cache"
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
	httpClient   *http.Client
	logger       *slog.Logger
	backend      RegistryBackend
	listCache    *cache.TypedCacheObject[CachedListServersResponse]
	detailsCache *cache.TypedCacheObject[CachedServerDetailsResponse]
}

// NewRegistryClient creates a new registry client. The cacheImpl parameter is
// optional — pass nil to disable caching.
func NewRegistryClient(logger *slog.Logger, tracerProvider trace.TracerProvider, backend RegistryBackend, cacheImpl cache.Cache) *RegistryClient {
	rc := &RegistryClient{
		httpClient: &http.Client{
			Transport: otelhttp.NewTransport(
				retryablehttp.NewClient().StandardClient().Transport,
				otelhttp.WithTracerProvider(tracerProvider),
			),
		},
		logger:       logger.With(attr.SlogComponent("mcp-registry-client")),
		backend:      backend,
		listCache:    nil,
		detailsCache: nil,
	}

	if cacheImpl != nil {
		listCache := cache.NewTypedObjectCache[CachedListServersResponse](
			logger.With(attr.SlogCacheNamespace("registry-list")),
			cacheImpl,
			cache.SuffixNone,
		)
		rc.listCache = &listCache

		detailsCache := cache.NewTypedObjectCache[CachedServerDetailsResponse](
			logger.With(attr.SlogCacheNamespace("registry-details")),
			cacheImpl,
			cache.SuffixNone,
		)
		rc.detailsCache = &detailsCache
	}

	return rc
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
	Meta   serverMeta `json:"_meta"`
}

type serverMeta struct {
	Server  serverMetaServer  `json:"com.pulsemcp/server"`
	Version serverMetaVersion `json:"com.pulsemcp/server-version"`
}

type serverMetaServer struct {
	VisitorsEstimateMostRecentWeek int  `json:"visitorsEstimateMostRecentWeek"`
	VisitorsEstimateLastFourWeeks  int  `json:"visitorsEstimateLastFourWeeks"`
	VisitorsEstimateTotal          int  `json:"visitorsEstimateTotal"`
	IsOfficial                     bool `json:"isOfficial"`
}

type serverMetaVersion struct {
	Source       string           `json:"source"`
	Status       string           `json:"status"`
	PublishedAt  string           `json:"publishedAt"`
	UpdatedAt    string           `json:"updatedAt"`
	IsLatest     bool             `json:"isLatest"`
	FirstRemote  serverRemoteMeta `json:"remotes[0]"`
	SecondRemote serverRemoteMeta `json:"remotes[1]"`
	ThirdRemote  serverRemoteMeta `json:"remotes[2]"`
	FourthRemote serverRemoteMeta `json:"remotes[3]"`
	FifthRemote  serverRemoteMeta `json:"remotes[4]"`
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
	Remotes []serverRemoteBasic `json:"remotes"`
}

type serverRemoteBasic struct {
	URL     string         `json:"url"`
	Type    string         `json:"type"`
	Headers []RemoteHeader `json:"headers"`
}

// RemoteHeader represents a header requirement from the registry.
type RemoteHeader struct {
	Name        string  `json:"name"`
	IsSecret    bool    `json:"isSecret"`
	IsRequired  bool    `json:"isRequired"`
	Description *string `json:"description,omitempty"`
	Placeholder *string `json:"placeholder,omitempty"`
}

type serverRemoteMeta struct {
	Auth  any          `json:"auth"`
	Tools []serverTool `json:"tools"`
}

// serverDetailsEntry represents the response from the server details endpoint
type serverDetailsEntry struct {
	Server serverDetailsJSON `json:"server"`
	Meta   serverMeta        `json:"_meta"`
}

type serverDetailsJSON struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Version     string              `json:"version"`
	Remotes     []serverRemoteBasic `json:"remotes"`
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

	// Check cache after authorization so headers are populated.
	if c.listCache != nil {
		cacheKey := registryCacheKey("list", req)
		cached, err := c.listCache.Get(ctx, cacheKey)
		if err == nil {
			c.logger.DebugContext(ctx, "registry list cache hit", attr.SlogCacheKey(cacheKey))
			return cached.Servers, nil
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
		if s.Meta.Version.Status == "deleted" {
			continue
		}

		var iconURL *string
		if len(s.Server.Icons) > 0 {
			iconURL = &s.Server.Icons[0].Src
		}

		tools := make([]*types.ExternalMCPTool, 0)
		for _, tool := range s.Meta.Version.FirstRemote.Tools {
			tools = append(tools, &types.ExternalMCPTool{
				Name:        &tool.Name,
				Description: &tool.Description,
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

	// Store in cache on success.
	if c.listCache != nil {
		cacheKey := registryCacheKey("list", req)
		if storeErr := c.listCache.Store(ctx, CachedListServersResponse{
			Key:     cacheKey,
			Servers: servers,
		}); storeErr != nil {
			c.logger.WarnContext(ctx, "failed to store registry list in cache", attr.SlogError(storeErr))
		}
	}

	return servers, nil
}

// ServerDetails contains detailed information about an MCP server including connection info.
type ServerDetails struct {
	Name          string
	Description   string
	Version       string
	RemoteURL     string
	TransportType externalmcptypes.TransportType
	Tools         []serverTool
	Headers       []RemoteHeader
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

	// Check cache after authorization so headers are populated.
	if c.detailsCache != nil {
		cacheKey := registryCacheKey("details", req)
		cached, err := c.detailsCache.Get(ctx, cacheKey)
		if err == nil {
			c.logger.DebugContext(ctx, "registry details cache hit", attr.SlogCacheKey(cacheKey))
			return cached.Details, nil
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

	var serverResp serverDetailsEntry
	if err := json.Unmarshal(body, &serverResp); err != nil {
		return nil, fmt.Errorf("decode external mcp server details response: %w", err)
	}

	// Find the remote URL, preferring streamable-http over sse
	var remoteURL string
	var transportType externalmcptypes.TransportType
	var tools []serverTool
	var headers []RemoteHeader
	var remoteIndex int
	for i, remote := range serverResp.Server.Remotes {
		if remote.Type == "streamable-http" {
			remoteURL = remote.URL
			transportType = externalmcptypes.TransportTypeStreamableHTTP
			headers = remote.Headers
			remoteIndex = i
			break
		} else if remote.Type == "sse" {
			remoteURL = remote.URL
			transportType = externalmcptypes.TransportTypeSSE
			headers = remote.Headers
			remoteIndex = i
		}
	}

	// Obviously not ideal, this is just the way the registry API is structured
	switch remoteIndex {
	case 0:
		tools = serverResp.Meta.Version.FirstRemote.Tools
	case 1:
		tools = serverResp.Meta.Version.SecondRemote.Tools
	case 2:
		tools = serverResp.Meta.Version.ThirdRemote.Tools
	case 3:
		tools = serverResp.Meta.Version.FourthRemote.Tools
	case 4:
		tools = serverResp.Meta.Version.FifthRemote.Tools
	}

	details := &ServerDetails{
		Name:          serverResp.Server.Name,
		Description:   serverResp.Server.Description,
		Version:       serverResp.Server.Version,
		RemoteURL:     remoteURL,
		TransportType: transportType,
		Tools:         tools,
		Headers:       headers,
	}

	// Store in cache on success.
	if c.detailsCache != nil {
		cacheKey := registryCacheKey("details", req)
		if storeErr := c.detailsCache.Store(ctx, CachedServerDetailsResponse{
			Key:     cacheKey,
			Details: details,
		}); storeErr != nil {
			c.logger.WarnContext(ctx, "failed to store registry details in cache", attr.SlogError(storeErr))
		}
	}

	return details, nil
}
