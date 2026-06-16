package mcpendpoints

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// BySlugAndCustomDomain walks the public addressing chain shared by the /mcp
// and /x/mcp slug handlers, the install-page handlers, and the .well-known
// routes: it scopes the lookup to the request's customdomains.Context, loads
// the mcp_endpoint by (slug, custom domain), then loads the linked mcp_server.
// Disabled servers and missing rows both surface as oops.CodeNotFound to
// avoid leaking existence to unauthenticated callers. logger should already
// carry the slug attribute.
//
// Callers that want to fall back to a legacy lookup (e.g. /mcp's existing
// toolsets.mcp_slug path) should check for oops.CodeNotFound and proceed
// accordingly.
func BySlugAndCustomDomain(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, slug string) (*repo.McpEndpoint, *mcpservers_repo.McpServer, error) {
	var customDomainID uuid.NullUUID
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	}

	endpoint, err := repo.New(db).GetMCPEndpointByCustomDomainAndSlug(ctx, repo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: customDomainID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, err, "load mcp endpoint").LogError(ctx, logger)
	}

	server, err := mcpservers_repo.New(db).GetMCPServerByID(ctx, mcpservers_repo.GetMCPServerByIDParams{
		ID:        endpoint.McpServerID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return nil, nil, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, logger)
	}

	if server.Visibility == mcpservers.VisibilityDisabled {
		return nil, nil, oops.C(oops.CodeNotFound)
	}

	return &endpoint, &server, nil
}
