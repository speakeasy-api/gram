package admin

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type adminOpenAPISource struct {
	AssetID     uuid.UUID
	Name        string
	URL         string
	ContentType string
}

// projectMCPServerSources resolves original documents from the effective toolset,
// not all documents in a deployment. Storage URLs remain internal to staff handlers.
func (s *Service) projectMCPServerSources(ctx context.Context, organizationID string, projectID, serverID uuid.UUID) ([]adminOpenAPISource, error) {
	queries := repo.New(s.db)
	target, err := queries.AdminGetMCPServerSourceToolset(ctx, repo.AdminGetMCPServerSourceToolsetParams{OrganizationID: organizationID, ProjectID: projectID, ServerID: serverID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve MCP server source toolset")
	}
	sources := make([]adminOpenAPISource, 0)
	if !target.Slug.Valid {
		return sources, nil
	}
	var groupID *uuid.UUID
	if target.VariationsGroupID != uuid.Nil {
		groupID = &target.VariationsGroupID
	}
	// Reuse runtime resolution, including latest toolset membership and deployment
	// selection, instead of treating every source in the project as exposed.
	toolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(projectID), mv.ToolsetSlug(target.Slug.String), nil, groupID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(toolset.Tools))
	for _, tool := range toolset.Tools {
		if tool.HTTPToolDefinition == nil {
			continue
		}
		id, err := uuid.Parse(tool.HTTPToolDefinition.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid resolved HTTP tool id")
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return sources, nil
	}
	rows, err := queries.AdminListMCPServerOpenAPISources(ctx, repo.AdminListMCPServerOpenAPISourcesParams{ProjectID: projectID, ToolIds: ids})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve MCP server OpenAPI sources")
	}
	for _, row := range rows {
		sources = append(sources, adminOpenAPISource{AssetID: row.ID, Name: row.Name, URL: row.Url, ContentType: row.ContentType})
	}
	return sources, nil
}
