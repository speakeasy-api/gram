package catalog

import (
	"context"
	"fmt"
	"io"

	registriesgen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const handlerSearch = "search_catalog"

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

// SearchTool wraps mcpRegistries.listCatalog as a platform tool so dispatch
// runs through the same RBAC path (ScopeProjectRead) as the dashboard
// catalog view.
type SearchTool struct {
	descriptor core.ToolDescriptor
	catalog    Catalog
}

func NewSearchTool(catalog Catalog) *SearchTool {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := true

	return &SearchTool{
		catalog: catalog,
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
	if t.catalog == nil {
		return oops.E(oops.CodeUnexpected, nil, "catalog tools are not configured")
	}

	if _, ok := contextvalues.GetAssistantPrincipal(ctx); !ok {
		return oops.E(oops.CodeUnauthorized, nil, "catalog tools require an assistant principal")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "catalog tools require a project auth context")
	}

	var input searchInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	result, err := t.catalog.ListCatalog(ctx, &registriesgen.ListCatalogPayload{
		RegistryID:       input.RegistryID,
		Search:           input.Query,
		Cursor:           input.Cursor,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("list catalog: %w", err)
	}

	views := make([]searchToolView, 0, len(result.Servers))
	for _, server := range result.Servers {
		if server == nil {
			continue
		}
		views = append(views, toolViewFromServer(server))
	}

	return writeJSON(wr, searchResult{
		Servers:    views,
		NextCursor: result.NextCursor,
	})
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
