package catalog

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const handlerSearch = "search_catalog"

// maxSearchResults caps the aggregated multi-registry response to keep tool
// output small enough for an LLM to read in one shot. Mirrors the v0 limit
// applied by externalmcp.ListCatalog.
const maxSearchResults = 100

type searchInput struct {
	Query      *string `json:"query,omitempty" jsonschema:"Substring filter passed to the registry."`
	RegistryID *string `json:"registry_id,omitempty" jsonschema:"Restrict the search to a single registry. Omit to query every configured registry."`
	Cursor     *string `json:"cursor,omitempty" jsonschema:"Pagination cursor from a previous response. Only honored when registry_id is set."`
}

type searchToolView struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Version           string                 `json:"version"`
	Title             *string                `json:"title,omitempty"`
	IconURL           *string                `json:"icon_url,omitempty"`
	RegistryID        string                 `json:"registry_id"`
	RegistrySpecifier string                 `json:"registry_specifier"`
	Tools             []catalogToolPreview   `json:"tools,omitempty"`
	Remotes           []catalogRemotePreview `json:"remotes,omitempty"`
}

type catalogToolPreview struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type catalogRemotePreview struct {
	URL           string `json:"url"`
	TransportType string `json:"transport_type"`
}

type searchResult struct {
	Servers    []searchToolView `json:"servers"`
	NextCursor *string          `json:"next_cursor,omitempty"`
}

// SearchTool wraps the MCP registry list endpoint as a platform tool.
type SearchTool struct {
	descriptor     core.ToolDescriptor
	registryClient *externalmcp.RegistryClient
	repo           *externalmcprepo.Queries
}

func NewSearchTool(registryClient *externalmcp.RegistryClient, repo *externalmcprepo.Queries) *SearchTool {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &SearchTool{
		registryClient: registryClient,
		repo:           repo,
		descriptor: core.ToolDescriptor{
			SourceSlug:  SourceCatalog,
			HandlerName: handlerSearch,
			Name:        ToolNameSearchCatalog,
			Description: "Search the MCP catalog for installable servers. Each result includes a registry_id and registry_specifier pair that must be passed verbatim to platform_install_catalog_server.",
			InputSchema: core.BuildInputSchema[searchInput](
				core.WithPropertyFormat("registry_id", "uuid"),
			),
			Variables:   nil,
			Annotations: catalogToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
	}
}

func (t *SearchTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *SearchTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.registryClient == nil || t.repo == nil {
		return oops.E(oops.CodeUnexpected, nil, "catalog tools are not configured")
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "catalog tools require a project auth context")
	}

	var input searchInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	registries, err := t.resolveRegistries(ctx, input.RegistryID)
	if err != nil {
		return err
	}

	servers := make([]searchToolView, 0)
	var nextCursor *string
	for _, registry := range registries {
		result, err := t.registryClient.ListServers(ctx, externalmcp.Registry{
			ID:  registry.ID,
			URL: registry.URL,
		}, externalmcp.ListServersParams{
			Search: input.Query,
			Cursor: input.Cursor,
		})
		if err != nil {
			// One registry being down should not blank out the rest of the
			// catalog — externalmcp.ListCatalog applies the same resilience
			// when aggregating multiple registries.
			continue
		}
		for _, server := range result.Servers {
			if server == nil {
				continue
			}
			servers = append(servers, toolViewFromServer(server))
			if len(servers) >= maxSearchResults {
				break
			}
		}
		if len(registries) == 1 {
			nextCursor = result.NextCursor
		}
		if len(servers) >= maxSearchResults {
			break
		}
	}

	return writeJSON(wr, searchResult{
		Servers:    servers,
		NextCursor: nextCursor,
	})
}

type registryRow struct {
	ID  uuid.UUID
	URL string
}

func (t *SearchTool) resolveRegistries(ctx context.Context, registryID *string) ([]registryRow, error) {
	if registryID != nil && *registryID != "" {
		parsed, err := uuid.Parse(*registryID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid registry_id")
		}
		row, err := t.repo.GetMCPRegistryByID(ctx, parsed)
		if err != nil {
			return nil, fmt.Errorf("get mcp registry: %w", err)
		}
		return []registryRow{{ID: row.ID, URL: row.Url}}, nil
	}

	rows, err := t.repo.ListMCPRegistries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mcp registries: %w", err)
	}
	out := make([]registryRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, registryRow{ID: row.ID, URL: row.Url})
	}
	return out, nil
}

func toolViewFromServer(server *types.ExternalMCPServer) searchToolView {
	view := searchToolView{
		Name:              server.RegistrySpecifier,
		Description:       server.Description,
		Version:           server.Version,
		Title:             server.Title,
		IconURL:           server.IconURL,
		RegistryID:        "",
		RegistrySpecifier: server.RegistrySpecifier,
		Tools:             nil,
		Remotes:           nil,
	}
	if server.RegistryID != nil {
		view.RegistryID = *server.RegistryID
	}
	for _, tool := range server.Tools {
		if tool == nil {
			continue
		}
		preview := catalogToolPreview{Name: "", Description: ""}
		if tool.Name != nil {
			preview.Name = *tool.Name
		}
		if tool.Description != nil {
			preview.Description = *tool.Description
		}
		view.Tools = append(view.Tools, preview)
	}
	for _, remote := range server.Remotes {
		if remote == nil {
			continue
		}
		view.Remotes = append(view.Remotes, catalogRemotePreview{
			URL:           remote.URL,
			TransportType: remote.TransportType,
		})
	}
	return view
}
