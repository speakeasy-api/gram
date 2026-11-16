package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// pluginMCPServerRow is a plugin server with the legacy alias address of a hosted server that has no usable endpoint.
type pluginMCPServerRow struct {
	repo.ListPluginsWithMcpServersForProjectRow

	// AddressSlug and AddressCustomDomain are the toolset's legacy alias, overriding the endpoint; the domain is set
	// only while it is verified and activated.
	AddressSlug         pgtype.Text
	AddressCustomDomain pgtype.Text
}

// listPluginMCPServers lists plugin servers and resolves the legacy alias of hosted servers without a usable
// endpoint, leaving out those that then have no address at all.
func listPluginMCPServers(ctx context.Context, db repo.DBTX, params repo.ListPluginsWithMcpServersForProjectParams) ([]pluginMCPServerRow, error) {
	rows, err := repo.New(db).ListPluginsWithMcpServersForProject(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list plugins with mcp servers: %w", err)
	}
	var ids []uuid.UUID
	for _, row := range rows {
		if row.NeedsAlias {
			ids = append(ids, row.McpServerID.UUID)
		}
	}
	aliases, err := legacyalias.List(ctx, db, params.ProjectID, ids)
	if err != nil {
		return nil, fmt.Errorf("load plugin MCP server aliases: %w", err)
	}
	none := pgtype.Text{String: "", Valid: false}
	domains := make(map[uuid.UUID]pgtype.Text)
	out := make([]pluginMCPServerRow, 0, len(rows))
	for _, row := range rows {
		server := pluginMCPServerRow{ListPluginsWithMcpServersForProjectRow: row, AddressSlug: none, AddressCustomDomain: none}
		if row.NeedsAlias {
			alias, ok := aliases[row.McpServerID.UUID]
			switch {
			case ok && alias.Slug.Valid:
				server.AddressSlug = alias.Slug
				if alias.CustomDomainID.Valid {
					domain, cached := domains[alias.CustomDomainID.UUID]
					if !cached {
						record, err := customdomainsrepo.New(db).GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{ID: alias.CustomDomainID.UUID, OrganizationID: row.PluginOrganizationID})
						switch {
						case errors.Is(err, pgx.ErrNoRows):
							domain = none
						case err != nil:
							return nil, fmt.Errorf("load legacy alias custom domain: %w", err)
						case record.Verified && record.Activated:
							domain = pgtype.Text{String: record.Domain, Valid: true}
						default:
							domain = none
						}
						domains[alias.CustomDomainID.UUID] = domain
					}
					server.AddressCustomDomain = domain
				}
			case row.PrivateEndpointSlug == "" && !row.UnproxiedUrl.Valid && !row.NetworkAccessMode.Valid:
				continue
			}
		}
		out = append(out, server)
	}
	return out, nil
}
