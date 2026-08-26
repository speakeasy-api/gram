// Package readmodel provides shared project and MCP server selection for management transports.
package readmodel

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// Reader owns the common database selection used by dashboard management APIs
// and Platform MCP. Callers remain responsible for their own authentication,
// authorization, pagination, and response mapping.
type Reader struct {
	projects   *projectsrepo.Queries
	mcpServers *mcpserversrepo.Queries
}

func New(db *pgxpool.Pool) *Reader {
	return &Reader{
		projects:   projectsrepo.New(db),
		mcpServers: mcpserversrepo.New(db),
	}
}

func (r *Reader) ListProjects(ctx context.Context, organizationID string) ([]projectsrepo.Project, error) {
	projects, err := r.projects.ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list projects by organization: %w", err)
	}
	return projects, nil
}

func (r *Reader) ListProjectsLimited(ctx context.Context, organizationID string, limit int32) ([]projectsrepo.Project, error) {
	projects, err := r.projects.ListProjectsByOrganizationLimited(ctx, projectsrepo.ListProjectsByOrganizationLimitedParams{
		OrganizationID: organizationID,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list limited projects by organization: %w", err)
	}
	return projects, nil
}

func (r *Reader) GetProject(ctx context.Context, projectID uuid.UUID, organizationID string) (projectsrepo.Project, error) {
	project, err := r.projects.GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return projectsrepo.Project{}, fmt.Errorf("get project by organization: %w", err)
	}
	return project, nil
}

func (r *Reader) GetProjectBySlug(ctx context.Context, slug, organizationID string) (projectsrepo.Project, error) {
	project, err := r.projects.GetProjectBySlug(ctx, projectsrepo.GetProjectBySlugParams{
		Slug:           slug,
		OrganizationID: organizationID,
	})
	if err != nil {
		return projectsrepo.Project{}, fmt.Errorf("get project by slug: %w", err)
	}
	return project, nil
}

func (r *Reader) ListMCPServers(ctx context.Context, projectID uuid.UUID, filters mcpserversrepo.ListMCPServersByProjectIDParams) ([]mcpserversrepo.McpServer, error) {
	filters.ProjectID = projectID
	servers, err := r.mcpServers.ListMCPServersByProjectID(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers by project: %w", err)
	}
	return servers, nil
}

func (r *Reader) ListMCPServersLimited(ctx context.Context, projectID uuid.UUID, organizationID string, limit int32) ([]mcpserversrepo.McpServer, error) {
	servers, err := r.mcpServers.ListMCPServersByLiveProjectForOrganizationLimited(ctx, mcpserversrepo.ListMCPServersByLiveProjectForOrganizationLimitedParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list limited mcp servers by live project: %w", err)
	}
	return servers, nil
}

func (r *Reader) GetMCPServer(ctx context.Context, mcpServerID, projectID uuid.UUID, organizationID string) (mcpserversrepo.McpServer, error) {
	server, err := r.mcpServers.GetMCPServerByLiveProjectForOrganization(ctx, mcpserversrepo.GetMCPServerByLiveProjectForOrganizationParams{
		ID:             mcpServerID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return mcpserversrepo.McpServer{}, fmt.Errorf("get mcp server by live project: %w", err)
	}
	return server, nil
}

func (r *Reader) ListMCPServersForOrganization(ctx context.Context, organizationID string) ([]mcpserversrepo.McpServer, error) {
	servers, err := r.mcpServers.ListMCPServersByOrganizationID(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers by organization: %w", err)
	}
	return servers, nil
}
