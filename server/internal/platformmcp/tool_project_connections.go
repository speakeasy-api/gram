package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/projects"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// ProjectConnectionsInput reads or changes one exact project's connection policy.
type ProjectConnectionsInput struct {
	// ProjectID names the exact project from list_projects.
	ProjectID string `json:"project_id" jsonschema:"exact project ID from list_projects"`
	// RequireMCPGateway is omitted to inspect without changing anything.
	RequireMCPGateway *bool `json:"require_mcp_gateway,omitempty" jsonschema:"omit to inspect; true blocks direct MCP connections and false allows them"`
	// ExpectedUpdatedAt binds a change to the state the user reviewed.
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"updated_at from the preceding inspection, required when changing the setting"`
	// Confirmed records approval of the exact target and impact.
	Confirmed bool `json:"confirmed,omitempty" jsonschema:"true only after the user confirms the exact project and the impact on existing direct connections"`
}

// ProjectConnectionsOutput is a live project connection-policy snapshot.
type ProjectConnectionsOutput struct {
	// ProjectID is the inspected project.
	ProjectID string `json:"project_id"`
	// Name identifies the project to the user.
	Name string `json:"name"`
	// RequireMCPGateway reports whether direct connections are blocked.
	RequireMCPGateway bool `json:"require_mcp_gateway"`
	// UpdatedAt is the version to send with a subsequent change.
	UpdatedAt string `json:"updated_at"`
}

func registerProjectConnectionsTool(reg *Registrar, service *ProjectLifecycleService) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name: "configure_project_connections", Title: "Configure Project Connections",
		Description: "Inspect or change whether a project requires MCP connections through a gateway. First call with only project_id. Show the project and current setting to the user. Enabling blocks existing and new direct server connections; users must have a working gateway URL first. To change it, supply require_mcp_gateway, the inspection's updated_at as expected_updated_at, and confirmed: true after explicit approval. A repeated assignment to the current value makes no change. Requires write access to that exact project. Managed assistants cannot change their own connection policy.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, Title: "", IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectWrite}, func(ctx context.Context, _ *mcp.CallToolRequest, input ProjectConnectionsInput) (*mcp.CallToolResult, ProjectConnectionsOutput, error) {
		return principalToolCall(ctx, projectLifecycleToolResult, func(principal Principal) (ProjectConnectionsOutput, error) {
			if service == nil {
				return ProjectConnectionsOutput{}, ErrUnavailable
			}
			return service.ConfigureProjectConnections(ctx, principal, input)
		})
	})
}

// ConfigureProjectConnections preserves the dashboard's authorization and atomic audit.
func (s *ProjectLifecycleService) ConfigureProjectConnections(ctx context.Context, principal Principal, input ProjectConnectionsInput) (ProjectConnectionsOutput, error) {
	id, err := uuid.Parse(input.ProjectID)
	if err != nil || principal.OrganizationID == "" || principal.UserID == "" {
		return ProjectConnectionsOutput{}, projectLifecycleInvalid("Provide an exact project ID and authenticated user.")
	}
	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceID: id.String(), ResourceKind: "", Dimensions: nil}); err != nil {
		return ProjectConnectionsOutput{}, toolExposureAuthorizationError(err, authz.ScopeProjectWrite)
	}
	if input.RequireMCPGateway != nil {
		if err := s.changes.AllowConnectionOrOrganization(ctx, principal); err != nil {
			return ProjectConnectionsOutput{}, projectBudgetError(err)
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ProjectConnectionsOutput{}, s.unexpected(ctx, fmt.Errorf("begin project connection update: %w", err))
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	project, err := projectsrepo.New(tx).GetProjectByIDAndOrganizationIDForUpdate(ctx, projectsrepo.GetProjectByIDAndOrganizationIDForUpdateParams{ID: id, OrganizationID: principal.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectConnectionsOutput{}, projectLifecycleMissing()
	}
	if err != nil {
		return ProjectConnectionsOutput{}, s.unexpected(ctx, fmt.Errorf("read project connections: %w", err))
	}
	if input.RequireMCPGateway != nil {
		if !input.Confirmed || input.ExpectedUpdatedAt == "" {
			return ProjectConnectionsOutput{}, projectLifecycleInvalid("Inspect the project and confirm the connection change before applying it.")
		}
		if *input.RequireMCPGateway != project.RequireMcpGateway.Bool {
			if input.ExpectedUpdatedAt != project.UpdatedAt.Time.Format(time.RFC3339Nano) {
				return ProjectConnectionsOutput{}, projectLifecycleInvalid("Project settings changed. Inspect them again before retrying.")
			}
			_, err = s.core.UpdateInTransaction(ctx, tx, projects.UpdateProjectMutation{OrganizationID: principal.OrganizationID, ProjectID: id, Name: project.Name, RequireMCPGateway: input.RequireMCPGateway, Actor: projects.ProjectActor{UserID: principal.UserID, DisplayName: nil}})
			if err != nil {
				return ProjectConnectionsOutput{}, s.unexpected(ctx, fmt.Errorf("update project connections: %w", err))
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectConnectionsOutput{}, s.unexpected(ctx, fmt.Errorf("commit project connections: %w", err))
	}
	project, err = projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: id, OrganizationID: principal.OrganizationID})
	if err != nil {
		return ProjectConnectionsOutput{}, s.unexpected(ctx, fmt.Errorf("verify project connections: %w", err))
	}
	return ProjectConnectionsOutput{ProjectID: id.String(), Name: project.Name, RequireMCPGateway: project.RequireMcpGateway.Bool, UpdatedAt: project.UpdatedAt.Time.Format(time.RFC3339Nano)}, nil
}
