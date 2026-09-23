//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AdminContext struct {
	Email     string   `json:"email"`
	ReadOnly  bool     `json:"read_only"`
	Scopes    []string `json:"scopes"`
	Workflows []string `json:"available_workflows"`
}

func registerContextTool(server *mcp.Server, organizationReadsAvailable bool) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_admin_context",
		Title:       "Get Staff Admin Context",
		Description: "Show the current authenticated staff connection's permitted workflows. This server does not grant access to customer tools.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, AdminContext, error) {
		principal, ok := principalFromContext(ctx)
		if !ok {
			return nil, AdminContext{}, errors.New("staff context is unavailable")
		}
		workflows := []string{"inspect staff admin context"}
		if organizationReadsAvailable {
			workflows = append(workflows, "find organizations", "inspect organization account and trial")
		}
		return nil, AdminContext{
			Email:     principal.Email,
			ReadOnly:  !slices.Contains(principal.Scopes, "admin:write"),
			Scopes:    slices.Clone(principal.Scopes),
			Workflows: workflows,
		}, nil
	})
}
