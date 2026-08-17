//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchCatalogInput struct {
	Query       string `json:"query,omitempty" jsonschema:"optional search text; only reviewed catalog candidates are returned"`
	ProviderKey string `json:"provider_key,omitempty" jsonschema:"optional reviewed provider key to filter"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"opaque continuation cursor returned by search_mcp_catalog"`
}

type SearchCatalogOutput struct {
	Candidates []CatalogCandidate `json:"candidates"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type InspectCatalogCandidateInput struct {
	ProviderKey string `json:"provider_key" jsonschema:"reviewed provider key returned by search_mcp_catalog"`
	CatalogRef  string `json:"catalog_ref" jsonschema:"canonical catalog reference returned by search_mcp_catalog"`
}

func registerCatalogTools(reg *Registrar, catalog Catalog, budget OperationBudget, cursorCodec *catalogCursorCodec, onboarding *OnboardingService) {
	addTool(reg, &mcp.Tool{
		Name:        "search_mcp_catalog",
		Title:       "Search MCP Catalog",
		Description: "Search reviewed catalog MCP candidates available for Platform onboarding. The results do not install or distribute an MCP.",
	}, ToolMeta{
		Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchCatalogInput) (*mcp.CallToolResult, SearchCatalogOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, SearchCatalogOutput{}, err
		}
		if err := budget.Allow(ctx, principal); err != nil {
			if result, ok := operationBudgetToolResult(err); ok {
				return result, SearchCatalogOutput{}, nil
			}
			return nil, SearchCatalogOutput{}, err
		}
		if catalog == nil {
			return nil, SearchCatalogOutput{}, ErrCatalogUnavailable
		}
		if cursorCodec == nil {
			return nil, SearchCatalogOutput{}, ErrCatalogUnavailable
		}
		position := 0
		if input.Cursor != "" {
			position, err = cursorCodec.Decode(input.Cursor, principal, input.Query, input.ProviderKey)
			if err != nil {
				return nil, SearchCatalogOutput{}, ErrCatalogCursorInvalid
			}
		}
		candidates, err := catalog.Search(ctx, normalizeCatalogQuery(input.Query))
		if err != nil {
			if result, ok := operationBudgetToolResult(ErrCatalogUnavailable); ok {
				return result, SearchCatalogOutput{}, nil
			}
			return nil, SearchCatalogOutput{}, ErrCatalogUnavailable
		}
		providerKey := normalizeCatalogProviderKey(input.ProviderKey)
		filtered := make([]CatalogCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if providerKey == "" || normalizeCatalogProviderKey(candidate.ProviderKey) == providerKey {
				filtered = append(filtered, candidate)
			}
		}
		page, nextPosition, err := catalogSearchPage(filtered, position)
		if err != nil {
			return nil, SearchCatalogOutput{}, err
		}
		if onboarding != nil {
			if err := onboarding.RecordCatalogExplored(ctx, principal); err != nil {
				return nil, SearchCatalogOutput{}, err
			}
		}
		output := SearchCatalogOutput{Candidates: page}
		if nextPosition > 0 {
			output.NextCursor, err = cursorCodec.Encode(catalogCursor{
				OrganizationID: principal.OrganizationID,
				Generation:     principalCursorBinding(principal),
				Query:          normalizeCatalogQuery(input.Query),
				ProviderKey:    providerKey,
				Position:       nextPosition,
			})
			if err != nil {
				return nil, SearchCatalogOutput{}, fmt.Errorf("encode platform mcp catalog cursor: %w", err)
			}
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "inspect_mcp_candidate",
		Title:       "Inspect MCP Candidate",
		Description: "Inspect one reviewed catalog MCP candidate by its provider key and canonical catalog reference.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input InspectCatalogCandidateInput) (*mcp.CallToolResult, CatalogDetails, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, CatalogDetails{}, err
		}
		if err := budget.Allow(ctx, principal); err != nil {
			if result, ok := operationBudgetToolResult(err); ok {
				return result, CatalogDetails{}, nil
			}
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
			if result, ok := operationBudgetToolResult(ErrCatalogUnavailable); ok {
				return result, CatalogDetails{}, nil
			}
			return nil, CatalogDetails{}, ErrCatalogUnavailable
		}
		return nil, details, nil
	})
}

func catalogSearchPage(candidates []CatalogCandidate, position int) ([]CatalogCandidate, int, error) {
	if position < 0 || position > len(candidates) {
		return nil, 0, ErrCatalogCursorInvalid
	}
	end := min(position+catalogPageSize, len(candidates))
	if end == len(candidates) {
		return candidates[position:end], 0, nil
	}
	return candidates[position:end], end, nil
}
