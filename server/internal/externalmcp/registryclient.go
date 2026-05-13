package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type RegistryBackend interface {
	Match(req *http.Request) bool
	Authorize(req *http.Request) error
}

// RegistryClient handles communication with external MCP registries.
type RegistryClient struct {
	httpClient   *guardian.HTTPClient
	logger       *slog.Logger
	backend      RegistryBackend
	listCache    *cache.TypedCacheObject[CachedListServersResponse]
	detailsCache *cache.TypedCacheObject[CachedServerDetailsResponse]
}

// NewRegistryClient creates a new registry client. The cacheImpl parameter is
// optional — pass nil to disable caching.
func NewRegistryClient(logger *slog.Logger, tracerProvider trace.TracerProvider, guardianPolicy *guardian.Policy, backend RegistryBackend, cacheImpl cache.Cache) *RegistryClient {
	rc := &RegistryClient{
		httpClient:   guardianPolicy.PooledClient(guardian.WithDefaultRetryConfig()),
		logger:       logger.With(attr.SlogComponent("mcp_registry_client")),
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

// ListServersResult is the result of a ListServers call.
type ListServersResult struct {
	Servers    []*types.ExternalMCPServer
	NextCursor *string
}

// listResponse represents the response from the MCP registry API.
type listResponse struct {
	Servers  []serverEntry `json:"servers"`
	Metadata struct {
		NextCursor *string `json:"nextCursor"`
	} `json:"metadata"`
}

type serverEntry struct {
	Server serverJSON         `json:"server"`
	Meta   pulseMCPServerMeta `json:"_meta"`
}

type pulseMCPServerMeta struct {
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
	Remotes []serverRemoteJSON `json:"remotes"`
}

type serverRemoteJSON struct {
	URL       string                    `json:"url"`
	Type      string                    `json:"type"`
	Headers   []RemoteHeader            `json:"headers"`
	Variables map[string]RemoteVariable `json:"variables"`
}

// RemoteHeader represents a header requirement from the registry.
type RemoteHeader struct {
	Name        string  `json:"name"`
	IsSecret    bool    `json:"isSecret"`
	IsRequired  bool    `json:"isRequired"`
	Description *string `json:"description,omitempty"`
	Placeholder *string `json:"placeholder,omitempty"`
}

// RemoteVariable represents a URL template variable from the registry.
type RemoteVariable struct {
	Description *string  `json:"description,omitempty"`
	IsSecret    bool     `json:"isSecret"`
	IsRequired  bool     `json:"isRequired"`
	Default     *string  `json:"default,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}

type serverRemoteMeta struct {
	AuthOptions []serverRemoteMetaAuthOptions `json:"authOptions"`
	Tools       []serverTool                  `json:"tools"`
}

type serverRemoteMetaAuthOptions struct {
	Type string `json:"type"`
}

// serverDetailsEntry represents the response from the server details endpoint
type serverDetailsEntry struct {
	Server serverDetailsJSON  `json:"server"`
	Meta   pulseMCPServerMeta `json:"_meta"`
}

type serverDetailsJSON struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Version     string             `json:"version"`
	Remotes     []serverRemoteJSON `json:"remotes"`
}

type serverTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations map[string]any  `json:"annotations"`
}

// toCacheSafeAny round-trips v through JSON so the resulting value is built
// from plain Go types (map[string]any, []any, primitives). This is required
// because the cache serializes with msgpack: concrete struct types stored in
// an `any` field lose their type on decode, and raw []byte-like values
// (json.RawMessage) come back as []byte which JSON-encodes as base64. Plain
// maps/slices round-trip through msgpack cleanly and re-marshal to identical
// JSON on both cache-miss and cache-hit paths.
func toCacheSafeAny(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return out, nil
}

// ListServers fetches servers from the given registry.
func (c *RegistryClient) ListServers(ctx context.Context, registry Registry, params ListServersParams) (ListServersResult, error) {
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
		return ListServersResult{}, fmt.Errorf("create list servers request: %w", err)
	}

	if c.backend.Match(req) {
		if err := c.backend.Authorize(req); err != nil {
			return ListServersResult{}, fmt.Errorf("authorize list servers request: %w", err)
		}
	}

	// Check cache after authorization so headers are populated.
	if c.listCache != nil {
		cacheKey := registryCacheKey("list", req)
		cached, err := c.listCache.Get(ctx, cacheKey)
		if err == nil {
			c.logger.DebugContext(ctx, "registry list cache hit", attr.SlogCacheKey(cacheKey))
			return ListServersResult{
				Servers:    cached.Servers,
				NextCursor: cached.NextCursor,
			}, nil
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ListServersResult{}, fmt.Errorf("failed to fetch from registry: %w", err)
	}
	defer o11y.LogDefer(ctx, c.logger, func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return ListServersResult{}, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ListServersResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	var listResp listResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return ListServersResult{}, fmt.Errorf("failed to decode registry response: %w", err)
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

		var remotes []*types.ExternalMCPRemote
		for _, r := range s.Server.Remotes {
			remotes = append(remotes, &types.ExternalMCPRemote{
				URL:           r.URL,
				TransportType: r.Type,
				Headers:       toExternalMCPRemoteHeaders(r.Headers),
				Variables:     toExternalMCPRemoteVariables(r.Variables),
			})
		}

		meta, err := toCacheSafeAny(&s.Meta)
		if err != nil {
			return ListServersResult{}, fmt.Errorf("convert meta: %w", err)
		}

		server := &types.ExternalMCPServer{
			RegistrySpecifier:                   s.Server.Name,
			Version:                             s.Server.Version,
			Description:                         s.Server.Description,
			ToolsetID:                           nil,
			RegistryID:                          &registryID,
			OrganizationMcpCollectionRegistryID: nil,
			Title:                               s.Server.Title,
			IconURL:                             iconURL,
			Meta:                                meta,
			Tools:                               tools,
			Remotes:                             remotes,
		}

		servers = append(servers, server)
	}

	nextCursor := listResp.Metadata.NextCursor

	// Store in cache on success.
	if c.listCache != nil {
		cacheKey := registryCacheKey("list", req)
		if storeErr := c.listCache.Store(ctx, CachedListServersResponse{
			Key:        cacheKey,
			Servers:    servers,
			NextCursor: nextCursor,
		}); storeErr != nil {
			c.logger.WarnContext(ctx, "failed to store registry list in cache", attr.SlogError(storeErr))
		}
	}

	return ListServersResult{
		Servers:    servers,
		NextCursor: nextCursor,
	}, nil
}

func toExternalMCPRemoteHeaders(headers []RemoteHeader) []*types.ExternalMCPRemoteHeader {
	if len(headers) == 0 {
		return nil
	}

	result := make([]*types.ExternalMCPRemoteHeader, 0, len(headers))
	for _, header := range headers {
		result = append(result, &types.ExternalMCPRemoteHeader{
			Name:        header.Name,
			Description: header.Description,
			IsSecret:    new(header.IsSecret),
			IsRequired:  new(header.IsRequired),
			Placeholder: header.Placeholder,
		})
	}
	return result
}

func toExternalMCPRemoteVariables(variables map[string]RemoteVariable) map[string]*types.ExternalMCPRemoteVariable {
	if len(variables) == 0 {
		return nil
	}

	result := make(map[string]*types.ExternalMCPRemoteVariable, len(variables))
	for name, variable := range variables {
		result[name] = &types.ExternalMCPRemoteVariable{
			Description: variable.Description,
			IsSecret:    new(variable.IsSecret),
			IsRequired:  new(variable.IsRequired),
			Default:     variable.Default,
			Choices:     variable.Choices,
		}
	}
	return result
}

// ClearCache removes all cached entries for the given registry URL.
func (c *RegistryClient) ClearCache(ctx context.Context, registryURL string) error {
	if c.listCache != nil {
		prefix := fmt.Sprintf("registry:list:%s", registryURL)
		if err := c.listCache.DeleteByPrefix(ctx, prefix); err != nil {
			return fmt.Errorf("clear list cache for registry %s: %w", registryURL, err)
		}
	}
	if c.detailsCache != nil {
		prefix := fmt.Sprintf("registry:details:%s", registryURL)
		if err := c.detailsCache.DeleteByPrefix(ctx, prefix); err != nil {
			return fmt.Errorf("clear details cache for registry %s: %w", registryURL, err)
		}
	}
	return nil
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
// If allowedRemoteURLs is provided and non-empty, only remotes with matching URLs are considered.
func (c *RegistryClient) GetServerDetails(ctx context.Context, registry Registry, serverName string, allowedRemoteURLs []string) (*ServerDetails, error) {
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

	// Build cache key including allowedRemoteURLs filter
	buildCacheKey := func() string {
		baseKey := registryCacheKey("details", req)
		if len(allowedRemoteURLs) == 0 {
			return baseKey
		}
		// Sort URLs for deterministic key
		sortedURLs := make([]string, len(allowedRemoteURLs))
		copy(sortedURLs, allowedRemoteURLs)
		sort.Strings(sortedURLs)
		return fmt.Sprintf("%s:filter:%s", baseKey, strings.Join(sortedURLs, ","))
	}

	// Check cache after authorization so headers are populated.
	if c.detailsCache != nil {
		cacheKey := buildCacheKey()
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

	// Build a set of allowed URLs for filtering (if provided)
	allowedURLSet := make(map[string]struct{}, len(allowedRemoteURLs))
	for _, u := range allowedRemoteURLs {
		allowedURLSet[u] = struct{}{}
	}
	hasFilter := len(allowedURLSet) > 0

	// Find the remote URL, preferring streamable-http over sse
	// If allowedRemoteURLs is set, only consider remotes in that list
	var remoteURL string
	var transportType externalmcptypes.TransportType
	var tools []serverTool
	var headers []RemoteHeader
	remoteIndex := -1 // Use -1 as sentinel to detect when no remote matched
	for i, remote := range serverResp.Server.Remotes {
		// Skip remotes not in allowed list (if filter is active)
		if hasFilter {
			if _, ok := allowedURLSet[remote.URL]; !ok {
				continue
			}
		}

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

	// Only fetch tools if a remote was actually matched.
	// If remoteIndex is -1 (no match), tools stays nil.
	// Obviously not ideal, this is just the way the registry API is structured.
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
		cacheKey := buildCacheKey()
		if storeErr := c.detailsCache.Store(ctx, CachedServerDetailsResponse{
			Key:     cacheKey,
			Details: details,
		}); storeErr != nil {
			c.logger.WarnContext(ctx, "failed to store registry details in cache", attr.SlogError(storeErr))
		}
	}

	return details, nil
}
