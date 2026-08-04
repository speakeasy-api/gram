//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package adminmcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const unavailableCode = "feature_unavailable"

type Reader interface {
	ListProjects(ctx context.Context, principal Principal, input ListProjectsInput) (ListProjectsOutput, error)
	ListProjectMCPs(ctx context.Context, principal Principal, input ListProjectMCPsInput) (ListProjectMCPsOutput, error)
	GetMCP(ctx context.Context, principal Principal, input GetMCPInput) (MCP, error)
}

type AdminContext struct {
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

func newServer(reader Reader) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gram-admin",
		Title:   "Gram Admin",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Use this server to inspect the selected Gram organization. All mutations are unavailable during the read-only rollout.",
		PageSize:     32,
	})

	registerReadTools(server, reader)
	registerUnavailableTools(server)
	return server
}

func registerReadTools(server *mcp.Server, reader Reader) {
	registerGetAdminContextTool(server)
	registerListProjectsTool(server, reader)
	registerListProjectMCPsTool(server, reader)
	registerGetMCPTool(server, reader)
}

func registerUnavailableTools(server *mcp.Server) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
		feature     string
	}{
		{"search_mcp_catalog", "Search MCP Catalog", "Search the approved MCP catalog. Catalog discovery is not enabled in the read-only rollout.", "catalog_discovery"},
		{"inspect_mcp_candidate", "Inspect MCP Candidate", "Inspect an MCP catalog candidate. Candidate inspection is not enabled in the read-only rollout.", "catalog_discovery"},
		{"register_catalog_mcp", "Register Catalog MCP", "Register an approved catalog MCP in a project. Registration is not enabled in the read-only rollout.", "catalog_registration"},
		{"distribute_mcp_to_default_plugin", "Distribute MCP to Default Plugin", "Distribute a configured MCP to the default plugin. Distribution is not enabled in the read-only rollout.", "plugin_distribution"},
		{"remove_mcp_from_default_plugin", "Remove MCP from Default Plugin", "Remove an MCP from the default plugin. Distribution changes are not enabled in the read-only rollout.", "plugin_distribution"},
		{"get_mcp_readiness", "Get MCP Readiness", "Check configured MCP readiness. Readiness checks are not enabled in the read-only rollout.", "mcp_readiness"},
		{"get_mcp_repair_plan", "Get MCP Repair Plan", "Get a safe MCP repair plan. Repair planning is not enabled in the read-only rollout.", "mcp_readiness"},
		{"get_setup_handoff", "Get Setup Handoff", "Create a secure setup handoff. Provider handoffs are not enabled in the read-only rollout.", "setup_handoff"},
	} {
		mcp.AddTool(server, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
		}, unavailableTool(tool.feature))
	}
}

func unavailableTool(feature string) mcp.ToolHandlerFor[map[string]any, featureUnavailableResult] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, featureUnavailableResult, error) {
		result := featureUnavailableResult{
			Code:    unavailableCode,
			Feature: feature,
			Message: "This Admin MCP capability is not enabled for the current rollout.",
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
