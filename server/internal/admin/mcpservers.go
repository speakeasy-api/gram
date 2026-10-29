package admin

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/endpointaddr"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// SetMCPServerURL sets the public Speakeasy server origin that platform-domain MCP
// URLs are built on.
func (s *Service) SetMCPServerURL(serverURL *url.URL) { s.mcpServerURL = serverURL }

type adminMCPServer struct {
	server    *gen.AdminMcpServer
	createdAt time.Time
}

func (s *Service) ListProjectMcpServers(ctx context.Context, payload *gen.ListProjectMcpServersPayload) (*gen.AdminListProjectMcpServersResult, error) {
	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, s.logger)
	}

	queries := repo.New(s.db)
	belongs, err := queries.AdminProjectBelongsToOrganization(ctx, repo.AdminProjectBelongsToOrganizationParams{
		ProjectID:      projectID,
		OrganizationID: payload.OrganizationID,
	})
	switch {
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup project organization").LogError(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID))
	case !belongs:
		return nil, oops.C(oops.CodeNotFound)
	}

	serverRows, err := queries.AdminListProjectMcpServerRows(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list project mcp servers").LogError(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID))
	}
	servers := make([]adminMCPServer, 0, len(serverRows))

	// The query returns a row per endpoint, with each server's rows adjacent.
	for start := 0; start < len(serverRows); {
		end := start + 1
		for end < len(serverRows) && serverRows[end].ID == serverRows[start].ID {
			end++
		}
		row := serverRows[start]
		servers = append(servers, adminMCPServer{
			server: &gen.AdminMcpServer{
				ID:         row.ID.String(),
				Name:       cmp.Or(row.Name.String, row.ToolsetName.String, row.Slug.String, row.ID.String()),
				URL:        s.primaryEndpointURL(serverRows[start:end]),
				Visibility: row.Visibility,
				Source:     mcpServerSource(row),
				CreatedAt:  row.CreatedAt.Time.Format(time.RFC3339),
			},
			createdAt: row.CreatedAt.Time,
		})
		start = end
	}

	slices.SortStableFunc(servers, func(a, b adminMCPServer) int {
		return cmp.Or(a.createdAt.Compare(b.createdAt), cmp.Compare(a.server.ID, b.server.ID))
	})

	result := make([]*gen.AdminMcpServer, len(servers))
	for i, server := range servers {
		result[i] = server.server
	}

	return &gen.AdminListProjectMcpServersResult{McpServers: result}, nil
}

// primaryEndpointURL picks the address the dashboard shows for one server's
// rows, skipping endpoints on a domain that cannot serve them.
func (s *Service) primaryEndpointURL(rows []repo.AdminListProjectMcpServerRowsRow) *string {
	endpoints := make([]mcpendpointsrepo.McpEndpoint, 0, len(rows))
	domains := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		if !row.EndpointID.Valid || (row.EndpointCustomDomainID.Valid && !row.CustomDomain.Valid) {
			continue
		}
		endpoints = append(endpoints, mcpendpointsrepo.McpEndpoint{
			ID:              row.EndpointID.UUID,
			ProjectID:       uuid.Nil,
			CustomDomainID:  row.EndpointCustomDomainID,
			McpServerID:     uuid.NullUUID{UUID: row.ID, Valid: true},
			MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Slug:            row.EndpointSlug.String,
			IsDomainRoot:    row.EndpointIsDomainRoot,
			CreatedAt:       row.EndpointCreatedAt,
			UpdatedAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			DeletedAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
			Deleted:         false,
		})
		if row.CustomDomain.Valid {
			domains[row.EndpointID.UUID] = row.CustomDomain.String
		}
	}

	primary := endpointaddr.PrimaryEndpoint(endpoints)
	if primary == nil {
		return nil
	}
	domain := domains[primary.ID]
	serverURL := ""
	switch {
	case s.mcpServerURL != nil:
		serverURL = s.mcpServerURL.String()
	case domain == "":
		return nil
	}
	u, err := endpointaddr.EndpointURL(primary, domain, serverURL)
	if err != nil {
		return nil
	}
	return &u
}

func mcpServerSource(row repo.AdminListProjectMcpServerRowsRow) string {
	switch {
	case row.ToolsetID.Valid:
		return "toolset"
	case row.RemoteMcpServerID.Valid:
		return "remote"
	case row.TunneledMcpServerID.Valid:
		return "tunneled"
	default:
		return "unproxied"
	}
}
