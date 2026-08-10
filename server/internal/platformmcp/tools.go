//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const unavailableCode = "feature_unavailable"

type Reader interface {
	ListProjects(ctx context.Context, principal Principal, input ListProjectsInput) (ListProjectsOutput, error)
	ListProjectMCPs(ctx context.Context, principal Principal, input ListProjectMCPsInput) (ListProjectMCPsOutput, error)
	GetMCP(ctx context.Context, principal Principal, input GetMCPInput) (MCP, error)
}

type PlatformContext struct {
	OrganizationID string `json:"organization_id"`
	ConnectionID   string `json:"connection_id"`
	ReadOnly       bool   `json:"read_only"`
}

type ListProjectsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum number of projects to return; server clamps this to 100"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ListProjectsOutput struct {
	Projects  []Project `json:"projects"`
	Truncated bool      `json:"truncated"`
}

type ListProjectMCPsInput struct {
	ProjectID string `json:"project_id" jsonschema:"Gram project ID to inspect"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of MCPs to return; server clamps this to 100"`
}

type MCP struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	Name       string `json:"name,omitempty"`
	Slug       string `json:"slug,omitempty"`
	Visibility string `json:"visibility"`
}

type ListProjectMCPsOutput struct {
	MCPs      []MCP `json:"mcps"`
	Truncated bool  `json:"truncated"`
}

type GetMCPInput struct {
	ProjectID string `json:"project_id" jsonschema:"Gram project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID"`
}

type featureUnavailableResult struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Message string `json:"message"`
}

type operationBudgetResult struct {
	Code    string `json:"code"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message"`
}

func newServer(reader Reader, catalog Catalog, registrations *RegistrationService, cursorKeyMaterial string, setupResources []SetupResource) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gram-platform-mcp",
		Title:   "Gram Platform MCP",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Use this server to inspect the selected Gram organization. All mutations are unavailable during the read-only rollout.",
		PageSize:     32,
	})

	registerReadTools(server, reader)
	registerSetupResources(server, setupResources)
	if catalog == nil || registrations == nil || !registrations.budgets.Catalog.valid() {
		registerUnavailableCatalogTools(server)
	} else if cursorCodec, err := newCatalogCursorCodec(cursorKeyMaterial); err != nil {
		registerUnavailableCatalogTools(server)
	} else {
		registerCatalogTools(server, catalog, registrations.budgets.Catalog, cursorCodec)
	}
	if registrations == nil || registrations.store == nil || !registrations.budgets.Registration.valid() {
		registerUnavailableCatalogRegistrationTool(server)
	} else {
		registerCatalogRegistrationTool(server, registrations)
	}
	if registrations == nil || registrations.store == nil || !registrations.budgets.Handoff.valid() {
		registerUnavailableSetupHandoffTool(server)
	} else {
		registerSetupHandoffTool(server, registrations)
	}
	if registrations == nil || registrations.readiness == nil || !registrations.budgets.Repair.valid() {
		registerUnavailableReadinessTools(server)
	} else {
		registerReadinessTools(server, registrations.readiness)
	}
	registerUnavailableTools(server)
	return server
}

func registerReadTools(server *mcp.Server, reader Reader) {
	registerGetPlatformContextTool(server)
	registerListProjectsTool(server, reader)
	registerListProjectMCPsTool(server, reader)
	registerGetMCPTool(server, reader)
}

func registerUnavailableCatalogTools(server *mcp.Server) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"search_mcp_catalog", "Search MCP Catalog", "Search reviewed catalog MCP candidates. Catalog access is not enabled in the current rollout."},
		{"inspect_mcp_candidate", "Inspect MCP Candidate", "Inspect one reviewed catalog MCP candidate. Catalog access is not enabled in the current rollout."},
	} {
		mcp.AddTool(server, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, unavailableTool("catalog"))
	}
}

func registerUnavailableCatalogRegistrationTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "register_catalog_mcp",
		Title:       "Register Catalog MCP",
		Description: "Register an approved catalog MCP in a project. Registration is not enabled in the current rollout.",
	}, unavailableTool("catalog_registration"))
}

func registerUnavailableSetupHandoffTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_setup_handoff",
		Title:       "Get Setup Handoff",
		Description: "Create a secure setup handoff. Provider handoffs are not enabled in the current rollout.",
	}, unavailableTool("setup_handoff"))
}

func registerUnavailableTools(server *mcp.Server) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
		feature     string
	}{

		{"distribute_mcp_to_default_plugin", "Distribute MCP to Default Plugin", "Distribute a configured MCP to the default plugin. Distribution is not enabled in the read-only rollout.", "plugin_distribution"},
		{"remove_mcp_from_default_plugin", "Remove MCP from Default Plugin", "Remove an MCP from the default plugin. Distribution changes are not enabled in the read-only rollout.", "plugin_distribution"},
	} {
		mcp.AddTool(server, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
		}, unavailableTool(tool.feature))
	}
}

func registerUnavailableReadinessTools(server *mcp.Server) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"get_mcp_readiness", "Get MCP Readiness", "Check configured MCP readiness. Readiness checks are not enabled in the current rollout."},
		{"get_mcp_repair_plan", "Get MCP Repair Plan", "Get a safe MCP repair plan. Repair planning is not enabled in the current rollout."},
	} {
		mcp.AddTool(server, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, unavailableTool("mcp_readiness"))
	}
}

func operationBudgetToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, ErrOperationRateLimited), errors.Is(err, ErrReadinessRateLimited):
		result = operationBudgetResult{Code: "rate_limited", Message: "This Platform MCP operation is temporarily rate limited. Retry after a short delay."}
	case errors.Is(err, ErrOperationBudgetUnavailable), errors.Is(err, ErrRegistrationUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Message: "This Platform MCP operation is temporarily unavailable."}
	case errors.Is(err, ErrRegistrationCap):
		result = operationBudgetResult{Code: "conflict", Reason: "active_registration_cap", Message: "This project has reached its active Platform MCP registration limit."}
	case errors.Is(err, ErrRegistrationConflict):
		result = operationBudgetResult{Code: "conflict", Message: "This Platform MCP registration conflicts with the current project state."}
	case errors.Is(err, ErrTargetIneligible):
		result = operationBudgetResult{Code: "ineligible_organization", Message: "This project is not eligible for Platform MCP registration."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

func unavailableTool(feature string) mcp.ToolHandlerFor[map[string]any, featureUnavailableResult] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, featureUnavailableResult, error) {
		result := featureUnavailableResult{
			Code:    unavailableCode,
			Feature: feature,
			Message: "This Platform MCP capability is not enabled for the current rollout.",
		}
		content, err := json.Marshal(result)
		if err != nil {
			return nil, featureUnavailableResult{}, fmt.Errorf("encode unavailable result: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(content)}},
			IsError: true,
		}, result, nil
	}
}

func principalFromToolContext(ctx context.Context) (Principal, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}, ErrUnauthorized
	}
	return principal, nil
}

func readOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true}
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return min(limit, 100)
}
