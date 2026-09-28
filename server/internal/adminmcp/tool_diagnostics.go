//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

const (
	maxOnboardingTasks   = 100
	maxOnboardingPresets = 50
	maxPresetTaskKeys    = 100
	maxProjectMCPServers = 100
)

var errDiagnosticsUnavailable = errors.New("organization or project diagnostics are unavailable")

type OnboardingReader interface {
	GetOrganizationOnboarding(context.Context, *gen.GetOrganizationOnboardingPayload) (*gen.AdminOnboardingConfiguration, error)
}

type ProjectMCPServerReader interface {
	ListProjectMcpServers(context.Context, *gen.ListProjectMcpServersPayload) (*gen.AdminListProjectMcpServersResult, error)
}

type OrganizationOnboardingInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact organization ID returned by find_organizations, not a slug"`
}

type OrganizationOnboardingOutput struct {
	OrganizationID string             `json:"organization_id"`
	Preset         *string            `json:"preset,omitempty"`
	Tasks          []OnboardingTask   `json:"tasks"`
	Presets        []OnboardingPreset `json:"presets"`
}

type OnboardingTask struct {
	Key    string `json:"key"`
	Hidden bool   `json:"hidden"`
}

type OnboardingPreset struct {
	Key             string   `json:"key"`
	VisibleTaskKeys []string `json:"visible_task_keys"`
}

type ListProjectMCPServersInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact organization ID returned by find_organizations, not a slug"`
	ProjectID      string `json:"project_id" jsonschema:"Exact project ID returned by list_organization_projects, not a slug"`
}

type ProjectMCPServer struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Source     string `json:"source"`
	CreatedAt  string `json:"created_at"`
}

type ListProjectMCPServersOutput struct {
	OrganizationID     string             `json:"organization_id"`
	ProjectID          string             `json:"project_id"`
	Servers            []ProjectMCPServer `json:"servers"`
	PossiblyIncomplete bool               `json:"possibly_incomplete"`
}

func registerDiagnosticTools(server *mcp.Server, organizations OrganizationReader, projects ProjectReader, onboarding OnboardingReader, mcpServers ProjectMCPServerReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_organization_onboarding",
		Title:       "Get Organization Onboarding",
		Description: "Read bounded onboarding configuration for an exact organization ID. Returns task and preset keys with hidden-state only, not customer-authored titles or descriptions.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationOnboardingInput) (*mcp.CallToolResult, OrganizationOnboardingOutput, error) {
		output := OrganizationOnboardingOutput{Tasks: []OnboardingTask{}, Presets: []OnboardingPreset{}}
		if !verifiedStaff(ctx) {
			return nil, output, errDiagnosticsUnavailable
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, output, err
		}
		if onboarding == nil {
			return nil, output, errDiagnosticsUnavailable
		}
		result, err := onboarding.GetOrganizationOnboarding(ctx, &gen.GetOrganizationOnboardingPayload{OrganizationID: org.ID})
		if err != nil || result == nil || result.OrganizationID != org.ID || len(result.Tasks) > maxOnboardingTasks || len(result.Presets) > maxOnboardingPresets {
			return nil, output, errDiagnosticsUnavailable
		}
		for _, task := range result.Tasks {
			if task == nil || len(task.Key) > 128 {
				return nil, OrganizationOnboardingOutput{}, errDiagnosticsUnavailable
			}
			output.Tasks = append(output.Tasks, OnboardingTask{Key: task.Key, Hidden: task.Hidden})
		}
		for _, preset := range result.Presets {
			if preset == nil || len(preset.Key) > 128 || len(preset.VisibleTaskKeys) > maxPresetTaskKeys {
				return nil, OrganizationOnboardingOutput{}, errDiagnosticsUnavailable
			}
			keys := append([]string(nil), preset.VisibleTaskKeys...)
			for _, key := range keys {
				if len(key) > 128 {
					return nil, OrganizationOnboardingOutput{}, errDiagnosticsUnavailable
				}
			}
			output.Presets = append(output.Presets, OnboardingPreset{Key: preset.Key, VisibleTaskKeys: keys})
		}
		if result.Preset != nil && len(*result.Preset) > 128 {
			return nil, OrganizationOnboardingOutput{}, errDiagnosticsUnavailable
		}
		output.OrganizationID, output.Preset = org.ID, result.Preset
		return nil, output, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_project_mcp_servers",
		Title:       "List Project MCP Servers",
		Description: "List up to 100 MCP servers for an exact project ID within an exact organization ID. possibly_incomplete means the service returned more; there is no next page. Server URLs are omitted because they may contain customer-specific routing information.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListProjectMCPServersInput) (*mcp.CallToolResult, ListProjectMCPServersOutput, error) {
		output := ListProjectMCPServersOutput{Servers: []ProjectMCPServer{}}
		if !verifiedStaff(ctx) {
			return nil, output, errDiagnosticsUnavailable
		}
		if input.ProjectID != strings.TrimSpace(input.ProjectID) || len(input.ProjectID) > 128 {
			return nil, output, errors.New("provide an exact project ID from list_organization_projects")
		}
		id, err := uuid.Parse(input.ProjectID)
		if err != nil || id.String() != input.ProjectID {
			return nil, output, errors.New("provide an exact project ID from list_organization_projects")
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, output, err
		}
		if projects == nil || mcpServers == nil {
			return nil, output, errDiagnosticsUnavailable
		}
		project, err := projects.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: input.ProjectID, OrganizationIDOrSlug: &org.ID})
		if err != nil || project == nil || project.ID != input.ProjectID || project.OrganizationID != org.ID {
			return nil, output, errDiagnosticsUnavailable
		}
		result, err := mcpServers.ListProjectMcpServers(ctx, &gen.ListProjectMcpServersPayload{OrganizationID: org.ID, ProjectID: project.ID})
		if err != nil || result == nil {
			return nil, output, errDiagnosticsUnavailable
		}
		output.PossiblyIncomplete = len(result.McpServers) > maxProjectMCPServers
		for _, server := range result.McpServers[:min(len(result.McpServers), maxProjectMCPServers)] {
			if server == nil || len(server.ID) > 128 || len(server.Name) > 256 || len(server.Visibility) > 64 || len(server.Source) > 64 || len(server.CreatedAt) > 64 {
				return nil, ListProjectMCPServersOutput{}, errDiagnosticsUnavailable
			}
			output.Servers = append(output.Servers, ProjectMCPServer{
				ID: server.ID, Name: server.Name, Visibility: server.Visibility,
				Source: server.Source, CreatedAt: server.CreatedAt,
			})
		}
		output.OrganizationID, output.ProjectID = org.ID, project.ID
		return nil, output, nil
	})
}
