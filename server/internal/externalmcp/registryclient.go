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
	"golang.org/x/sync/singleflight"

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
	listCache    cache.TypedCacheObject[CachedListServers]
	detailsCache cache.TypedCacheObject[CachedServerDetailsResponse]
	listFlight   singleflight.Group
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(logger *slog.Logger, tracerProvider trace.TracerProvider, guardianPolicy *guardian.Policy, backend RegistryBackend, cacheImpl cache.Cache) *RegistryClient {
	return &RegistryClient{
		httpClient: guardianPolicy.PooledClient(guardian.WithDefaultRetryConfig()),
		logger:     logger.With(attr.SlogComponent("mcp_registry_client")),
		backend:    backend,
		listCache: cache.NewTypedObjectCache[CachedListServers](
			logger.With(attr.SlogCacheNamespace("registry-list")),
			cacheImpl,
			cache.SuffixNone,
		),
		detailsCache: cache.NewTypedObjectCache[CachedServerDetailsResponse](
			logger.With(attr.SlogCacheNamespace("registry-details")),
			cacheImpl,
			cache.SuffixNone,
		),
		listFlight: singleflight.Group{},
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
}

const (
	registryListPageSize = 50
	// registryListMaxPages bounds the catalog crawl to the expected catalog size
	// (~100 servers = two pages), matching the multi-registry cap in the
	// ListCatalog handler. If a registry returns more, fetchAllServers serves the
	// first registryListMaxPages*registryListPageSize and logs a warning, so
	// growth past the assumption surfaces loudly instead of truncating silently.
	registryListMaxPages = 2
)

// ListServersResult is the result of a ListServers call.
type ListServersResult struct {
	Servers []*types.ExternalMCPServerEntry
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
	Type   string                     `json:"type"`
	Detail serverRemoteMetaAuthDetail `json:"detail"`
}

type serverRemoteMetaAuthDetail struct {
	// AuthorizationServerMetadata is PulseMCP's embedded RFC 8414 discovery
	// result for OAuth remotes. A non-empty registration_endpoint means the
	// upstream authorization server supports dynamic client registration (DCR).
	// Note: we deliberately read registration_endpoint here rather than
	// clientRegistration.dynamic.supported, which PulseMCP reports
	// optimistically (true even when the AS exposes no registration endpoint).
	AuthorizationServerMetadata struct {
		RegistrationEndpoint string `json:"registration_endpoint"`
	} `json:"authorizationServerMetadata"`
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

// ListServers fetches every server from the given registry and applies an
// optional in-memory search filter. The catalog is small and stable, so the
// full list is fetched, deduplicated, and cached under a single key per
// registry; callers receive the whole result set and paginate client-side.
func (c *RegistryClient) ListServers(ctx context.Context, registry Registry, params ListServersParams) (ListServersResult, error) {
	search := ""
	if params.Search != nil {
		search = *params.Search
	}

	cacheKey := listCacheKey(registry.URL, registry.ID)

	if cached, err := c.listCache.Get(ctx, cacheKey); err == nil {
		c.logger.DebugContext(ctx, "registry list cache hit", attr.SlogCacheKey(cacheKey))
		return ListServersResult{Servers: filterServers(cached.Servers, search)}, nil
	}

	// Collapse concurrent misses for the same registry into a single crawl so a
	// cold or unavailable cache cannot fan out into repeated upstream traffic.
	value, err, _ := c.listFlight.Do(cacheKey, func() (any, error) {
		if cached, err := c.listCache.Get(ctx, cacheKey); err == nil {
			return cached, nil
		}

		list, truncated, err := c.fetchAllServers(ctx, registry, cacheKey)
		if err != nil {
			return CachedListServers{}, err
		}

		// A truncated list is a partial catalog: the registry outgrew the page
		// bound. Serve it so the page still renders, but do not freeze a partial
		// catalog in the cache for the full TTL — let the next request re-crawl.
		if truncated {
			return list, nil
		}

		if storeErr := c.listCache.Store(ctx, list); storeErr != nil {
			c.logger.WarnContext(ctx, "failed to store registry list in cache", attr.SlogError(storeErr))
		}
		return list, nil
	})
	if err != nil {
		return ListServersResult{}, fmt.Errorf("list registry servers: %w", err)
	}

	list, ok := value.(CachedListServers)
	if !ok {
		return ListServersResult{}, fmt.Errorf("registry list singleflight returned unexpected type %T", value)
	}

	return ListServersResult{Servers: filterServers(list.Servers, search)}, nil
}

// listCacheKeyPrefix is the cache-key prefix for a registry URL. ClearCache
// deletes by this prefix so it evicts every per-registry entry under the URL,
// including a stale one left behind by a deleted-and-recreated registry.
func listCacheKeyPrefix(registryURL string) string {
	return fmt.Sprintf("registry:list:%s:%s", registryCacheSchemaVersion, strings.TrimRight(registryURL, "/"))
}

// listCacheKey keys the cached list by both URL and registry ID. The cached
// servers embed the Gram registry ID (stamped during conversion), so a URL-only
// key could serve servers carrying a different registry's ID if a registry is
// recreated under the same URL. Storage and ClearCache both derive from
// listCacheKeyPrefix so the two cannot drift apart.
func listCacheKey(registryURL string, registryID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", listCacheKeyPrefix(registryURL), registryID)
}

// fetchAllServers pages through the registry, deduplicating servers by registry
// specifier. Some registries return overlapping pages under a continuing
// cursor, so the crawl also stops once a page contributes no new servers. The
// returned bool reports whether the crawl hit the page bound with more pages
// still remaining (a partial, truncated catalog).
func (c *RegistryClient) fetchAllServers(ctx context.Context, registry Registry, cacheKey string) (CachedListServers, bool, error) {
	seen := make(map[string]struct{})
	servers := make([]*types.ExternalMCPServerEntry, 0, registryListPageSize)
	cursor := ""

	for range registryListMaxPages {
		req, err := c.newListServersRequest(ctx, registry.URL, cursor)
		if err != nil {
			return CachedListServers{}, false, err
		}
		if c.backend.Match(req) {
			if err := c.backend.Authorize(req); err != nil {
				return CachedListServers{}, false, fmt.Errorf("authorize list servers request: %w", err)
			}
		}

		page, err := c.fetchListServersPage(ctx, req)
		if err != nil {
			return CachedListServers{}, false, err
		}

		converted, err := convertListServers(registry.ID, page.Servers)
		if err != nil {
			return CachedListServers{}, false, err
		}

		added := 0
		for _, server := range converted {
			if _, ok := seen[server.RegistrySpecifier]; ok {
				continue
			}
			seen[server.RegistrySpecifier] = struct{}{}
			servers = append(servers, server)
			added++
		}

		next := ""
		if page.Metadata.NextCursor != nil {
			next = *page.Metadata.NextCursor
		}

		// Stop at the end of the registry's pages, or once a page is entirely
		// duplicates (some registries return overlapping pages under a stable
		// cursor; this assumes new servers never appear after an all-duplicate
		// page).
		if added == 0 || next == "" {
			cursor = ""
			break
		}
		cursor = next
	}

	// A non-empty cursor here means the registry still had more pages than the
	// bound allows, so the catalog outgrew the ~100-server assumption. Serve what
	// we have, but warn loudly and report truncation so the caller skips caching
	// the partial list rather than freezing it for the full TTL.
	truncated := cursor != ""
	if truncated {
		c.logger.WarnContext(ctx, "registry catalog exceeded page bound; returning partial list",
			attr.SlogMCPRegistryID(registry.ID.String()),
			attr.SlogMCPRegistryURL(registry.URL),
			attr.SlogStatsMCPServerCount(len(servers)),
			attr.SlogPaginationLimit(registryListMaxPages*registryListPageSize),
		)
	}

	return CachedListServers{Key: cacheKey, Servers: servers}, truncated, nil
}

func (c *RegistryClient) newListServersRequest(ctx context.Context, registryURL string, cursor string) (*http.Request, error) {
	parsed, err := url.Parse(fmt.Sprintf("%s/v0.1/servers", strings.TrimRight(registryURL, "/")))
	if err != nil {
		return nil, fmt.Errorf("parse list servers url: %w", err)
	}

	q := parsed.Query()
	q.Set("version", "latest")
	q.Set("limit", fmt.Sprintf("%d", registryListPageSize))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	parsed.RawQuery = q.Encode()

	c.logger.InfoContext(ctx, "fetching servers from registry", attr.SlogMCPRegistryURL(parsed.String()))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create list servers request: %w", err)
	}
	return req, nil
}

func (c *RegistryClient) fetchListServersPage(ctx context.Context, req *http.Request) (listResponse, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return listResponse{}, fmt.Errorf("fetch from registry: %w", err)
	}
	defer o11y.LogDefer(ctx, c.logger, func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return listResponse{}, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return listResponse{}, fmt.Errorf("read response body: %w", err)
	}

	var listResp listResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return listResponse{}, fmt.Errorf("decode registry response: %w", err)
	}
	return listResp, nil
}

// filterServers applies a case-insensitive substring match over the server
// specifier, title, and description. Search runs in memory because the catalog
// is small enough to hold in full.
func filterServers(servers []*types.ExternalMCPServerEntry, search string) []*types.ExternalMCPServerEntry {
	if search == "" {
		return servers
	}

	needle := strings.ToLower(search)
	filtered := make([]*types.ExternalMCPServerEntry, 0, len(servers))
	for _, server := range servers {
		title := ""
		if server.Title != nil {
			title = *server.Title
		}
		if strings.Contains(strings.ToLower(server.RegistrySpecifier), needle) ||
			strings.Contains(strings.ToLower(title), needle) ||
			strings.Contains(strings.ToLower(server.Description), needle) {
			filtered = append(filtered, server)
		}
	}
	return filtered
}

func convertListServers(registryUUID uuid.UUID, entries []serverEntry) ([]*types.ExternalMCPServerEntry, error) {
	registryID := registryUUID.String()
	servers := make([]*types.ExternalMCPServerEntry, 0, len(entries))
	for _, s := range entries {

		if s.Meta.Version.Status == "deleted" {
			continue
		}

		var iconURL *string
		if len(s.Server.Icons) > 0 {
			iconURL = &s.Server.Icons[0].Src
		}

		// The catalog list view needs only a tool count and a read-only flag
		// (for the card badge and the tool-behavior filter); it never reads the
		// tool definitions, whose JSON Schemas dominate the registry payload
		// (repeated in the top-level tools list and across every _meta remote
		// slot). Compute the two scalars the list needs, then drop the tools from
		// the _meta blob and omit the top-level tools array entirely. The detail
		// page fetches full tools via getServerDetails.
		listTools := s.Meta.Version.FirstRemote.Tools
		toolCount := len(listTools)
		isReadOnly := toolCount > 0
		for _, tool := range listTools {
			if readOnly, _ := tool.Annotations["readOnlyHint"].(bool); !readOnly {
				isReadOnly = false
				break
			}
		}

		// A server supports DCR when any remote's OAuth auth option carries a
		// non-empty registration endpoint in PulseMCP's embedded discovery
		// result. Computed in the same pass that strips per-remote tools.
		supportsDCR := false
		for _, remote := range []*serverRemoteMeta{
			&s.Meta.Version.FirstRemote,
			&s.Meta.Version.SecondRemote,
			&s.Meta.Version.ThirdRemote,
			&s.Meta.Version.FourthRemote,
			&s.Meta.Version.FifthRemote,
		} {
			for _, auth := range remote.AuthOptions {
				if auth.Detail.AuthorizationServerMetadata.RegistrationEndpoint != "" {
					supportsDCR = true
					break
				}
			}
			remote.Tools = nil
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
			return []*types.ExternalMCPServerEntry{}, fmt.Errorf("convert meta: %w", err)
		}

		servers = append(servers, &types.ExternalMCPServerEntry{
			RegistrySpecifier:                   s.Server.Name,
			Version:                             s.Server.Version,
			Description:                         s.Server.Description,
			ToolsetID:                           nil,
			McpServerID:                         nil,
			RegistryID:                          &registryID,
			OrganizationMcpCollectionRegistryID: nil,
			Title:                               s.Server.Title,
			IconURL:                             iconURL,
			Meta:                                meta,
			ToolCount:                           toolCount,
			IsReadOnly:                          isReadOnly,
			SupportsDcr:                         supportsDCR,
			Remotes:                             remotes,
		})
	}

	return servers, nil
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
	if err := c.listCache.DeleteByPrefix(ctx, listCacheKeyPrefix(registryURL)); err != nil {
		return fmt.Errorf("clear list cache for registry %s: %w", registryURL, err)
	}

	detailsPrefix := fmt.Sprintf("registry:details:%s:%s", registryCacheSchemaVersion, registryURL)
	if err := c.detailsCache.DeleteByPrefix(ctx, detailsPrefix); err != nil {
		return fmt.Errorf("clear details cache for registry %s: %w", registryURL, err)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
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
	cacheKey := buildCacheKey()
	cached, err := c.detailsCache.Get(ctx, cacheKey)
	if err == nil {
		c.logger.DebugContext(ctx, "registry details cache hit", attr.SlogCacheKey(cacheKey))
		return cached.Details, nil
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
	if storeErr := c.detailsCache.Store(ctx, CachedServerDetailsResponse{
		Key:     cacheKey,
		Details: details,
	}); storeErr != nil {
		c.logger.WarnContext(ctx, "failed to store registry details in cache", attr.SlogError(storeErr))
	}

	return details, nil
}
