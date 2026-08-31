package mcpendpoints

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

// SlugAvailabilityCheck describes one probe against the unified MCP slug
// namespace. A slug is unavailable when a live mcp_endpoints row or a live
// toolsets.mcp_slug holds it in the same address scope (platform when
// CustomDomainID is null, otherwise that custom domain), because the runtime
// resolves mcp_endpoints first and falls back to toolsets.mcp_slug.
type SlugAvailabilityCheck struct {
	// Slug is the candidate address slug, already lowercased by the caller.
	Slug string

	// CustomDomainID selects the address scope: null probes the platform
	// namespace, non-null probes that custom domain's namespace.
	CustomDomainID uuid.NullUUID

	// OrganizationID guards custom-domain probes: a domain not owned by this
	// organization short-circuits to "unavailable" so callers cannot probe
	// slug existence under domains they don't own. Ignored on platform
	// probes.
	OrganizationID string

	// ExcludeToolsetID is set when validating a toolset's own address. That
	// toolset's row and the endpoints of its wrapping mcp_servers row do not
	// count against it.
	ExcludeToolsetID uuid.NullUUID

	// ExcludeMcpServerID is set when validating an endpoint of a server. The
	// toolset backing that server does not count against it.
	ExcludeMcpServerID uuid.NullUUID
}

// CheckSlugAvailable reports whether an MCP address slug is free in its scope
// across both toolsets.mcp_slug and mcp_endpoints.slug. It is shared by the
// toolsets and mcpEndpoints services so the two tables form one namespace for
// as long as both representations of a hosted server coexist. Callers
// validating inside a write transaction should pass that transaction so the
// probe sees rows locked by the caller.
func CheckSlugAvailable(ctx context.Context, db repo.DBTX, check SlugAvailabilityCheck) (bool, error) {
	available, err := repo.New(db).CheckUnifiedSlugAvailability(ctx, repo.CheckUnifiedSlugAvailabilityParams{
		Slug:               check.Slug,
		CustomDomainID:     check.CustomDomainID,
		OrganizationID:     check.OrganizationID,
		ExcludeToolsetID:   check.ExcludeToolsetID,
		ExcludeMcpServerID: check.ExcludeMcpServerID,
	})
	if err != nil {
		return false, fmt.Errorf("check unified mcp slug availability: %w", err)
	}

	// available.Valid is always true for this query (boolean expression with
	// no nullable sub-terms), but sqlc widens the return type to pgtype.Bool
	// because it doesn't infer non-null on compound expressions.
	return available.Bool, nil
}
