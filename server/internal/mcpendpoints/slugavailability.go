package mcpendpoints

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

// SlugAvailabilityCheck probes the unified MCP slug namespace spanning
// mcp_endpoints.slug and toolsets.mcp_slug; see CheckUnifiedSlugAvailability
// in queries.sql for the semantics of each field.
type SlugAvailabilityCheck struct {
	// Slug is the candidate address slug, already lowercased by the caller.
	Slug string

	// CustomDomainID selects the namespace: null for platform, non-null for
	// that custom domain.
	CustomDomainID uuid.NullUUID

	// OrganizationID must own a supplied custom domain unless
	// SkipDomainOwnershipCheck is set.
	OrganizationID string

	// ExcludeToolsetID is set when validating a toolset's own address.
	ExcludeToolsetID uuid.NullUUID

	// ExcludeMcpServerID is set when validating an endpoint of a server.
	ExcludeMcpServerID uuid.NullUUID

	// SkipDomainOwnershipCheck skips the domain liveness/ownership guard, for
	// callers probing a scope the toolset already holds (which may reference a
	// soft-deleted domain).
	SkipDomainOwnershipCheck bool
}

// CheckSlugAvailable reports whether an MCP address slug is free in its scope
// across both toolsets.mcp_slug and mcp_endpoints.slug. Write paths must call
// LockSlugScope first, in the same transaction.
func CheckSlugAvailable(ctx context.Context, db repo.DBTX, check SlugAvailabilityCheck) (bool, error) {
	available, err := repo.New(db).CheckUnifiedSlugAvailability(ctx, repo.CheckUnifiedSlugAvailabilityParams{
		Slug:               check.Slug,
		CustomDomainID:     check.CustomDomainID,
		OrganizationID:     check.OrganizationID,
		ExcludeToolsetID:   check.ExcludeToolsetID,
		ExcludeMcpServerID: check.ExcludeMcpServerID,
		SkipDomainCheck:    check.SkipDomainOwnershipCheck,
	})
	if err != nil {
		return false, fmt.Errorf("check unified mcp slug availability: %w", err)
	}

	return available.Bool, nil
}

// LockSlugScope serializes competing claims on one (scope, slug) address for
// the rest of the caller's transaction; the availability check is
// check-then-write and the per-table unique indexes cannot see cross-table
// collisions.
func LockSlugScope(ctx context.Context, db repo.DBTX, customDomainID uuid.NullUUID, slug string) error {
	scope := "platform"
	if customDomainID.Valid {
		scope = customDomainID.UUID.String()
	}
	if err := repo.New(db).LockSlugScope(ctx, scope+"/"+slug); err != nil {
		return fmt.Errorf("lock mcp slug scope: %w", err)
	}
	return nil
}
