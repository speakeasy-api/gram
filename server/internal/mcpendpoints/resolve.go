package mcpendpoints

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	metamcp_visibility "github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ErrEndpointUnavailable marks an address that resolved to an mcp_endpoints
// row whose backend cannot serve: disabled visibility, or a dangling backend
// FK. The endpoint row makes the address authoritative, so this outcome is a
// terminal not-found on every surface — unlike a plain address miss, it must
// never fall back to the legacy toolsets.mcp_slug lookup, which would let a
// disabled server resurrect the same slug through its toolset (AIS-633).
var ErrEndpointUnavailable = errors.New("mcp endpoint unavailable")

// IsAddressMiss reports whether a resolution error is a true address miss —
// the only outcome that may fall back to a legacy toolsets.mcp_slug lookup.
func IsAddressMiss(err error) bool {
	var shareErr *oops.ShareableError
	return errors.As(err, &shareErr) && shareErr.Code == oops.CodeNotFound && !errors.Is(err, ErrEndpointUnavailable)
}

// BySlugAndCustomDomain walks the public addressing chain shared by the /mcp
// and /x/mcp slug handlers, the install-page handlers, and the .well-known
// routes: it scopes the lookup to the request's customdomains.Context, loads
// the mcp_endpoint by (slug, custom domain), then loads whichever backend the
// endpoint addresses. Exactly one of the returned server and metaServer is
// non-nil, matching the endpoint table's backend-exclusivity check. Disabled
// backends of either kind and missing rows all surface as oops.CodeNotFound to
// avoid leaking existence to unauthenticated callers. logger should already
// carry the slug attribute.
//
// Callers that want to fall back to a legacy lookup (e.g. /mcp's existing
// toolsets.mcp_slug path) may do so only on a true address miss — a
// CodeNotFound that is NOT ErrEndpointUnavailable. A resolvable-but-unavailable
// address (errors.Is(err, ErrEndpointUnavailable)) is terminal.
func BySlugAndCustomDomain(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, slug string) (*repo.McpEndpoint, *mcpservers_repo.McpServer, *metamcp_repo.MetaMcpServer, error) {
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
		return nil, nil, nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found")
	case err != nil:
		return nil, nil, nil, oops.E(oops.CodeUnexpected, err, "load mcp endpoint").LogError(ctx, logger)
	}

	if endpoint.MetaMcpServerID.Valid {
		metaServer, err := metamcp_repo.New(db).GetMetaMCPServerByIDAndProjectID(ctx, metamcp_repo.GetMetaMCPServerByIDAndProjectIDParams{
			ID:        endpoint.MetaMcpServerID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, nil, nil, oops.E(oops.CodeNotFound, fmt.Errorf("%w: %w", ErrEndpointUnavailable, err), "meta mcp server not found")
		case err != nil:
			return nil, nil, nil, oops.E(oops.CodeUnexpected, err, "load meta mcp server").LogError(ctx, logger)
		}

		if metaServer.Visibility == metamcp_visibility.Disabled {
			return nil, nil, nil, oops.E(oops.CodeNotFound, ErrEndpointUnavailable, "mcp endpoint not found")
		}

		return &endpoint, nil, &metaServer, nil
	}

	server, err := mcpservers_repo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        endpoint.McpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, nil, oops.E(oops.CodeNotFound, fmt.Errorf("%w: %w", ErrEndpointUnavailable, err), "mcp server not found")
	case err != nil:
		return nil, nil, nil, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, logger)
	}

	switch server.Visibility {
	// Upstream serves like the others; what differs is who authenticates the
	// caller, which the serve path decides. ResolveAuthorizationMode still
	// refuses an upstream row whose columns do not describe a servable server.
	case mcpservers.VisibilityPublic, mcpservers.VisibilityPrivate, mcpservers.VisibilityUpstream:
	default:
		// Disabled or unrecognized visibility never serves; unknown values
		// must not silently map onto a servable policy.
		return nil, nil, nil, oops.E(oops.CodeNotFound, ErrEndpointUnavailable, "mcp endpoint not found")
	}

	return &endpoint, &server, nil, nil
}
