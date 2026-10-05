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
	maxOnboardingSteps   = 100
	maxProjectMCPServers = 100
)

var errDiagnosticsUnavailable = errors.New("organization or project diagnostics are unavailable")

type OnboardingReader interface {
	GetOrganizationOnboardingPlaybook(context.Context, *gen.GetOrganizationOnboardingPlaybookPayload) (*gen.AdminOrganizationOnboardingPlaybook, error)
}

type ProjectMCPServerReader interface {
	ListProjectMcpServers(context.Context, *gen.ListProjectMcpServersPayload) (*gen.AdminListProjectMcpServersResult, error)
}

type OrganizationOnboardingInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact organization ID returned by find_organizations, not a slug"`
}

type OrganizationOnboardingOutput struct {
	OrganizationID string `json:"organization_id"`
	// The assigned playbook, absent until staff assign one.
	Playbook *OnboardingPlaybook `json:"playbook,omitempty"`
	// Each step of the assigned playbook against the recorded stack.
	Steps []OnboardingStep `json:"steps"`
}

type OnboardingPlaybook struct {
	ID string `json:"id"`
	// The use case slug of a shared playbook; absent for the organization's own.
	UseCase *string `json:"use_case,omitempty"`
	// Whether it is its use case's default, the one the survey assigns.
	Default bool `json:"default"`
	// Whether the playbook belongs to this organization alone.
	Custom bool `json:"custom"`
}

type OnboardingStep struct {
	Slug string `json:"slug"`
	// Whether the recorded stack supports the step.
	Applies bool `json:"applies"`
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

func registerDiagnosticTools(server *mcp.Server, reader Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_organization_onboarding",
		Title:       "Get Organization Onboarding",
		Description: "Read the onboarding playbook assigned to an exact organization ID: the playbook's id, use case slug and default/custom flags, and each of its steps by slug with whether the organization's recorded stack supports it. Names, descriptions and reasons are omitted.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationOnboardingInput) (*mcp.CallToolResult, OrganizationOnboardingOutput, error) {
		output := OrganizationOnboardingOutput{Steps: []OnboardingStep{}}
		if !verifiedStaff(ctx) {
			return nil, output, errDiagnosticsUnavailable
		}
		org, err := readExactOrganization(ctx, reader, input.OrganizationID)
		if err != nil {
			return nil, output, err
		}
		result, err := reader.GetOrganizationOnboardingPlaybook(ctx, &gen.GetOrganizationOnboardingPlaybookPayload{OrganizationID: org.ID})
		if err != nil || result == nil || result.OrganizationID != org.ID || len(result.Applicability) > maxOnboardingSteps {
			return nil, output, errDiagnosticsUnavailable
		}
		if playbook := result.Playbook; playbook != nil {
			custom := playbook.OrganizationID != nil
			// A playbook of another organization can never be the answer here.
			if len(playbook.ID) > 128 || (custom && *playbook.OrganizationID != org.ID) || (playbook.UseCaseSlug != nil && len(*playbook.UseCaseSlug) > 128) {
				return nil, OrganizationOnboardingOutput{}, errDiagnosticsUnavailable
			}
			var useCase *string
			if playbook.UseCaseSlug != nil {
				slug := *playbook.UseCaseSlug
				useCase = &slug
			}
			output.Playbook = &OnboardingPlaybook{ID: playbook.ID, UseCase: useCase, Default: playbook.IsDefault, Custom: custom}
		}
		for _, step := range result.Applicability {
			if step == nil || len(step.Slug) > 128 {
				return nil, OrganizationOnboardingOutput{}, errDiagnosticsUnavailable
			}
			output.Steps = append(output.Steps, OnboardingStep{Slug: step.Slug, Applies: step.Applies})
		}
		output.OrganizationID = org.ID
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
		org, err := readExactOrganization(ctx, reader, input.OrganizationID)
		if err != nil {
			return nil, output, err
		}
		project, err := reader.GetProject(ctx, &gen.GetProjectPayload{IDOrSlug: input.ProjectID, OrganizationIDOrSlug: &org.ID})
		if err != nil || project == nil || project.ID != input.ProjectID || project.OrganizationID != org.ID {
			return nil, output, errDiagnosticsUnavailable
		}
		result, err := reader.ListProjectMcpServers(ctx, &gen.ListProjectMcpServersPayload{OrganizationID: org.ID, ProjectID: project.ID})
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
