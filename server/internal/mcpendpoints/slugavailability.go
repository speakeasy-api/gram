package mcpendpoints

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

// SlugAvailabilityCheck probes the MCP slug namespace of mcp_endpoints.slug;
// see CheckEndpointSlugAvailability in queries.sql for the semantics of each
// field.
type SlugAvailabilityCheck struct {
	// Slug is the candidate address slug, already lowercased by the caller.
	Slug string

	// CustomDomainID selects the namespace: null for platform, non-null for
	// that custom domain.
	CustomDomainID uuid.NullUUID

	// OrganizationID must own a supplied custom domain unless
	// SkipDomainOwnershipCheck is set.
	OrganizationID string

	// SkipDomainOwnershipCheck skips the domain liveness/ownership guard, for
	// callers probing a scope the toolset already holds (which may reference a
	// soft-deleted domain).
	SkipDomainOwnershipCheck bool
}

// CheckSlugAvailable reports whether an MCP address slug is free in its scope
// among mcp_endpoints.slug. Write paths must call
// LockSlugScope first, in the same transaction.
func CheckSlugAvailable(ctx context.Context, db repo.DBTX, check SlugAvailabilityCheck) (bool, error) {
	available, err := repo.New(db).CheckEndpointSlugAvailability(ctx, repo.CheckEndpointSlugAvailabilityParams{
		Slug:            check.Slug,
		CustomDomainID:  check.CustomDomainID,
		OrganizationID:  check.OrganizationID,
		SkipDomainCheck: check.SkipDomainOwnershipCheck,
	})
	if err != nil {
		return false, fmt.Errorf("check mcp slug availability: %w", err)
	}
	return available.Bool, nil
}

// LockSlugScope serializes competing claims on one (scope, slug) address for
// the rest of the caller's transaction, since the availability check runs
// before the write.
func LockSlugScope(ctx context.Context, db repo.DBTX, customDomainID uuid.NullUUID, slug string) error {
	if err := repo.New(db).LockSlugScope(ctx, repo.LockSlugScopeParams{
		CustomDomainID: customDomainID,
		Slug:           slug,
	}); err != nil {
		return fmt.Errorf("lock mcp slug scope: %w", err)
	}
	return nil
}
