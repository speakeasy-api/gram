//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	ProviderKey string `json:"provider_key,omitempty" jsonschema:"reviewed provider key returned by search_mcp_catalog; provide with catalog_ref, or provide remote_url instead"`
	CatalogRef  string `json:"catalog_ref,omitempty" jsonschema:"canonical catalog reference returned by search_mcp_catalog; provide with provider_key, or provide remote_url instead"`
	RemoteURL   string `json:"remote_url,omitempty" jsonschema:"user-supplied HTTPS Streamable HTTP MCP URL; mutually exclusive with provider_key and catalog_ref; safe endpoint query parameters are supported, but credentials, credential-like query parameters, and fragments are not"`
}

// CandidateInspection is the safe common projection for reviewed and direct
// candidate paths. Direct remote URLs are always marked unreviewed and never
// enter the reviewed catalogue.
type CandidateInspection struct {
	ProviderKey            string                      `json:"provider_key,omitempty"`
	CatalogRef             string                      `json:"catalog_ref,omitempty"`
	CanonicalURL           string                      `json:"canonical_url,omitempty"`
	Name                   string                      `json:"name,omitempty"`
	Description            string                      `json:"description,omitempty"`
	Version                string                      `json:"version,omitempty"`
	Transport              string                      `json:"transport"`
	ToolNames              []string                    `json:"tool_names"`
	ToolCount              int                         `json:"tool_count"`
	Configuration          []CatalogConfigurationField `json:"configuration,omitempty"`
	RequiresDashboardSetup bool                        `json:"requires_dashboard_setup"`
	Trust                  string                      `json:"trust"`
	Authentication         string                      `json:"authentication,omitempty"`
	OAuthDiscovery         string                      `json:"oauth_discovery,omitempty"`
	SetupIntent            string                      `json:"setup_intent,omitempty"`
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
		if catalog == nil || cursorCodec == nil {
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
			output.NextCursor, err = cursorCodec.Encode(catalogCursor{OrganizationID: principal.OrganizationID, Generation: principalCursorBinding(principal), Query: normalizeCatalogQuery(input.Query), ProviderKey: providerKey, Position: nextPosition})
			if err != nil {
				return nil, SearchCatalogOutput{}, fmt.Errorf("encode platform mcp catalog cursor: %w", err)
			}
		}
		return nil, output, nil
	})
}

func registerCandidateInspectionTool(reg *Registrar, catalog Catalog, directRemote DirectRemoteInspector, gate CatalogRegistrationGateChecker, budget OperationBudget) {
	addTool(reg, &mcp.Tool{
		Name:        "inspect_mcp_candidate",
		Title:       "Inspect MCP Candidate",
		Description: "Inspect one reviewed catalog MCP candidate or one user-supplied HTTPS Streamable HTTP MCP URL. Inspection does not register or distribute an MCP.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input InspectCatalogCandidateInput) (*mcp.CallToolResult, CandidateInspection, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, CandidateInspection{}, err
		}
		if err := budget.Allow(ctx, principal); err != nil {
			if result, ok := operationBudgetToolResult(err); ok {
				return result, CandidateInspection{}, nil
			}
			return nil, CandidateInspection{}, err
		}
		remoteURL := strings.TrimSpace(input.RemoteURL)
		catalogSelection := input.ProviderKey != "" || input.CatalogRef != ""
		if (remoteURL == "" && (!catalogSelection || input.ProviderKey == "" || input.CatalogRef == "")) || (remoteURL != "" && catalogSelection) {
			return nil, CandidateInspection{}, ErrCatalogRejected
		}
		if remoteURL != "" {
			if directRemote == nil || gate == nil {
				return directRemoteInspectionUnavailableToolResult()
			}
			enabled, err := gate.EnabledOrganization(ctx, principal.OrganizationID)
			if err != nil || !enabled {
				return directRemoteInspectionUnavailableToolResult()
			}
			inspection, err := directRemote.Inspect(ctx, remoteURL)
			if err != nil {
				if result, ok := operationBudgetToolResult(err); ok {
					return result, CandidateInspection{}, nil
				}
				return directRemoteInspectionUnavailableToolResult()
			}
			return nil, CandidateInspection{CanonicalURL: inspection.CanonicalURL, Transport: inspection.Transport, ToolNames: inspection.ToolNames, ToolCount: inspection.ToolCount, RequiresDashboardSetup: inspection.RequiresDashboardSetup, Trust: inspection.Trust, Authentication: inspection.Authentication, OAuthDiscovery: inspection.OAuthDiscovery}, nil
		}
		if catalog == nil {
			return nil, CandidateInspection{}, ErrCatalogUnavailable
		}
		details, err := catalog.Inspect(ctx, input.ProviderKey, input.CatalogRef)
		if errors.Is(err, ErrCatalogRejected) {
			return nil, CandidateInspection{}, ErrCatalogRejected
		}
		if err != nil {
			if result, ok := operationBudgetToolResult(ErrCatalogUnavailable); ok {
				return result, CandidateInspection{}, nil
			}
			return nil, CandidateInspection{}, ErrCatalogUnavailable
		}
		return nil, CandidateInspection{ProviderKey: details.ProviderKey, CatalogRef: details.CatalogRef, Name: details.Name, Description: details.Description, Version: details.Version, Transport: details.Transport, ToolNames: details.ToolNames, ToolCount: details.ToolCount, Configuration: details.Configuration, RequiresDashboardSetup: details.RequiresDashboardSetup, Trust: "reviewed_catalog", SetupIntent: details.SetupIntent}, nil
	})
}

func directRemoteInspectionUnavailableToolResult() (*mcp.CallToolResult, CandidateInspection, error) {
	if result, ok := operationBudgetToolResult(ErrDirectRemoteUnavailable); ok {
		return result, CandidateInspection{}, nil
	}
	return nil, CandidateInspection{}, ErrDirectRemoteUnavailable
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
