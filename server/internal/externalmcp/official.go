package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	officialRegistryListPageSize = 50
	officialRegistryMaxPages     = 10
)

// OfficialRegistryAdapter implements the public read-only v0.1 Official MCP
// Registry contract. It is deliberately not composed into production until its
// source profile is certified and enabled by a later rollout.
type OfficialRegistryAdapter struct {
	httpClient *guardian.HTTPClient
	logger     *slog.Logger
}

var _ RegistryReader = (*OfficialRegistryAdapter)(nil)

func NewOfficialRegistryAdapter(logger *slog.Logger, policy *guardian.Policy) *OfficialRegistryAdapter {
	return &OfficialRegistryAdapter{
		httpClient: policy.PooledClient(guardian.WithDefaultRetryConfig()),
		logger:     logger,
	}
}

type officialListResponse struct {
	Servers  []officialServerEntry `json:"servers"`
	Metadata struct {
		NextCursor *string `json:"nextCursor"`
	} `json:"metadata"`
}

type officialServerEntry struct {
	Server serverJSON `json:"server"`
	Meta   struct {
		Official struct {
			Status string `json:"status"`
		} `json:"io.modelcontextprotocol.registry/official"`
	} `json:"_meta"`
}

func (a *OfficialRegistryAdapter) ListServers(ctx context.Context, registry Registry, params ListServersParams) (ListServersResult, error) {
	if a == nil || a.httpClient == nil {
		return ListServersResult{}, fmt.Errorf("official registry adapter is not configured")
	}
	base, err := url.Parse(registry.URL)
	if err != nil {
		return ListServersResult{}, fmt.Errorf("parse official registry URL: %w", err)
	}

	seen := make(map[string]struct{})
	servers := make([]*types.ExternalMCPServerEntry, 0, officialRegistryListPageSize)
	cursor := ""
	for range officialRegistryMaxPages {
		requestURL := base.JoinPath("v0.1", "servers")
		query := requestURL.Query()
		query.Set("version", "latest")
		query.Set("limit", fmt.Sprintf("%d", officialRegistryListPageSize))
		if params.Search != nil && *params.Search != "" {
			query.Set("search", *params.Search)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		requestURL.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), http.NoBody)
		if err != nil {
			return ListServersResult{}, fmt.Errorf("create official registry list request: %w", err)
		}
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return ListServersResult{}, fmt.Errorf("fetch official registry list: %w", err)
		}
		body, readErr := readBoundedBody(resp.Body)
		closeErr := resp.Body.Close()
		if closeErr != nil {
			a.logger.WarnContext(ctx, "close official registry list response", attr.SlogError(closeErr))
		}
		if readErr != nil {
			return ListServersResult{}, fmt.Errorf("read official registry list: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return ListServersResult{}, fmt.Errorf("official registry returned status %d", resp.StatusCode)
		}
		var page officialListResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return ListServersResult{}, fmt.Errorf("decode official registry list: %w", err)
		}
		for _, entry := range page.Servers {
			if entry.Meta.Official.Status == "deleted" || entry.Server.Name == "" {
				continue
			}
			if _, exists := seen[entry.Server.Name]; exists {
				continue
			}
			seen[entry.Server.Name] = struct{}{}
			servers = append(servers, officialEntry(registry.ID, entry.Server))
		}
		if page.Metadata.NextCursor == nil || *page.Metadata.NextCursor == "" {
			cursor = ""
			break
		}
		cursor = *page.Metadata.NextCursor
	}
	if cursor != "" {
		return ListServersResult{}, fmt.Errorf("official registry exceeded %d-page catalogue bound", officialRegistryMaxPages)
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].RegistrySpecifier < servers[j].RegistrySpecifier })
	return ListServersResult{Servers: servers}, nil
}

func officialEntry(registryID uuid.UUID, server serverJSON) *types.ExternalMCPServerEntry {
	registryIDString := registryID.String()
	remotes := make([]*types.ExternalMCPRemote, 0, len(server.Remotes))
	for _, remote := range server.Remotes {
		remotes = append(remotes, &types.ExternalMCPRemote{
			URL:           remote.URL,
			TransportType: remote.Type,
			Headers:       toExternalMCPRemoteHeaders(remote.Headers),
			Variables:     toExternalMCPRemoteVariables(remote.Variables),
		})
	}
	return &types.ExternalMCPServerEntry{
		Repository:                          toExternalMCPRepository(server.Repository),
		Packages:                            toExternalMCPPackages(server.Packages),
		RegistrySpecifier:                   server.Name,
		Version:                             server.Version,
		Description:                         server.Description,
		ToolsetID:                           nil,
		McpServerID:                         nil,
		RegistryID:                          &registryIDString,
		OrganizationMcpCollectionRegistryID: nil,
		Title:                               server.Title,
		IconURL:                             officialIconURL(server),
		Meta:                                nil,
		ToolCount:                           0,
		IsReadOnly:                          false,
		SupportsDcr:                         false,
		Remotes:                             remotes,
	}
}

func officialIconURL(server serverJSON) *string {
	if len(server.Icons) == 0 || server.Icons[0].Src == "" {
		return nil
	}
	return &server.Icons[0].Src
}

func (a *OfficialRegistryAdapter) GetServerDetails(ctx context.Context, registry Registry, serverName string, allowedRemoteURLs []string) (*ServerDetails, error) {
	if a == nil || a.httpClient == nil {
		return nil, fmt.Errorf("official registry adapter is not configured")
	}
	base, err := url.Parse(registry.URL)
	if err != nil {
		return nil, fmt.Errorf("parse official registry URL: %w", err)
	}
	requestURL := base.JoinPath("v0.1", "servers", url.PathEscape(serverName), "versions", "latest")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create official registry detail request: %w", err)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch official registry detail: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("official registry returned status %d", resp.StatusCode)
	}
	body, err := readBoundedBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read official registry detail: %w", err)
	}
	var detail struct {
		Server serverJSON `json:"server"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, fmt.Errorf("decode official registry detail: %w", err)
	}
	remote, ok := selectOfficialRemote(detail.Server.Remotes, allowedRemoteURLs)
	if !ok {
		return &ServerDetails{
			Name:          detail.Server.Name,
			Description:   detail.Server.Description,
			Version:       detail.Server.Version,
			RemoteURL:     "",
			TransportType: "",
			Tools:         nil,
			Headers:       nil,
			Variables:     nil,
		}, nil
	}
	return &ServerDetails{
		Name:          detail.Server.Name,
		Description:   detail.Server.Description,
		Version:       detail.Server.Version,
		RemoteURL:     remote.URL,
		TransportType: externalmcptypes.TransportType(remote.Type),
		Tools:         nil,
		Headers:       remote.Headers,
		Variables:     remote.Variables,
	}, nil
}

func selectOfficialRemote(remotes []serverRemoteJSON, allowed []string) (serverRemoteJSON, bool) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, rawURL := range allowed {
		allowedSet[rawURL] = struct{}{}
	}
	var fallback *serverRemoteJSON
	for i := range remotes {
		remote := &remotes[i]
		if len(allowedSet) > 0 {
			if _, ok := allowedSet[remote.URL]; !ok {
				continue
			}
		}
		if strings.EqualFold(remote.Type, string(externalmcptypes.TransportTypeStreamableHTTP)) {
			return *remote, true
		}
		if fallback == nil && strings.EqualFold(remote.Type, string(externalmcptypes.TransportTypeSSE)) {
			fallback = remote
		}
	}
	if fallback == nil {
		return serverRemoteJSON{URL: "", Type: "", Headers: nil, Variables: nil}, false
	}
	return *fallback, true
}
