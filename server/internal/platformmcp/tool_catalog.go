//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchCatalogInput struct {
	Query string `json:"query,omitempty" jsonschema:"optional search text; only reviewed catalog candidates are returned"`
}

type SearchCatalogOutput struct {
	Candidates []CatalogCandidate `json:"candidates"`
}

type InspectCatalogCandidateInput struct {
	ProviderKey string `json:"provider_key" jsonschema:"reviewed provider key returned by search_mcp_catalog"`
	CatalogRef  string `json:"catalog_ref" jsonschema:"canonical catalog reference returned by search_mcp_catalog"`
}

func registerCatalogTools(server *mcp.Server, catalog Catalog) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_mcp_catalog",
		Title:       "Search MCP Catalog",
		Description: "Search reviewed catalog MCP candidates available for Platform onboarding. The results do not install or distribute an MCP.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchCatalogInput) (*mcp.CallToolResult, SearchCatalogOutput, error) {
		if _, err := principalFromToolContext(ctx); err != nil {
			return nil, SearchCatalogOutput{}, err
		}
		if catalog == nil {
			return nil, SearchCatalogOutput{}, ErrCatalogUnavailable
		}
		candidates, err := catalog.Search(ctx, input.Query)
		if err != nil {
			return nil, SearchCatalogOutput{}, fmt.Errorf("search reviewed mcp catalog: %w", err)
		}
		return nil, SearchCatalogOutput{Candidates: candidates}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "inspect_mcp_candidate",
		Title:       "Inspect MCP Candidate",
		Description: "Inspect one reviewed catalog MCP candidate by its provider key and canonical catalog reference.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input InspectCatalogCandidateInput) (*mcp.CallToolResult, CatalogDetails, error) {
		if _, err := principalFromToolContext(ctx); err != nil {
			return nil, CatalogDetails{}, err
		}
		if input.ProviderKey == "" || input.CatalogRef == "" {
			return nil, CatalogDetails{}, ErrCatalogRejected
		}
		if catalog == nil {
			return nil, CatalogDetails{}, ErrCatalogUnavailable
		}
		details, err := catalog.Inspect(ctx, input.ProviderKey, input.CatalogRef)
		if errors.Is(err, ErrCatalogRejected) {
			return nil, CatalogDetails{}, ErrCatalogRejected
		}
		if err != nil {
			return nil, CatalogDetails{}, fmt.Errorf("inspect reviewed mcp catalog candidate: %w", err)
		}
		return nil, details, nil
	})
}
