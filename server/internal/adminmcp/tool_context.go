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

func registerContextTool(server *mcp.Server, organizationReadsAvailable, projectReadsAvailable, configurationReadsAvailable, activityReadsAvailable, usageReadsAvailable, coverageReadsAvailable, issuerReadsAvailable, matrixReadsAvailable, onboardingReadsAvailable, projectMCPReadsAvailable, billingDiagnosticsAvailable bool) {
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
		if projectReadsAvailable {
			workflows = append(workflows, "inspect organization projects and project setup")
		}
		if configurationReadsAvailable {
			workflows = append(workflows, "inspect organization feature flags and chat analysis settings")
		}
		if activityReadsAvailable {
			workflows = append(workflows, "inspect paginated organization activity (without snapshots or metadata)")
		}
		if usageReadsAvailable {
			workflows = append(workflows, "inspect current-cycle organization usage estimate")
		}
		if coverageReadsAvailable {
			workflows = append(workflows, "inspect which agent surfaces Gram observes for an organization")
		}
		if issuerReadsAvailable {
			workflows = append(workflows, "inspect global issuers and bounded duplicate/migration preflights")
		}
		if matrixReadsAvailable {
			workflows = append(workflows, "inspect global support matrix facts without operator notes")
		}
		if onboardingReadsAvailable {
			workflows = append(workflows, "inspect organization onboarding task configuration")
		}
		if projectMCPReadsAvailable {
			workflows = append(workflows, "inspect project MCP server inventory without URLs")
		}
		if billingDiagnosticsAvailable {
			workflows = append(workflows, "inspect bounded organization inference key state, spend history and metered usage totals")
		}
		return nil, AdminContext{
			Email:     principal.Email,
			ReadOnly:  !slices.Contains(principal.Scopes, "admin:write"),
			Scopes:    slices.Clone(principal.Scopes),
			Workflows: workflows,
		}, nil
	})
}
