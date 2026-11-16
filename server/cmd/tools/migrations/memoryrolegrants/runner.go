// Package memoryrolegrants stores the canonical memory grants for built-in
// Admin roles seeded before the memory scopes existed; see
// MEMORY_ROLE_GRANTS_BACKFILL.md.
package memoryrolegrants

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// DB is the connection the backfill reads candidates from and opens one
// transaction per role principal on.
type DB interface {
	DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Options bound one run.
type Options struct {
	// OrganizationID limits the run to one organization; empty covers all.
	OrganizationID string

	// Apply commits the writes. Otherwise every transaction is rolled back and
	// the report holds the prospective counts.
	Apply bool
}

// Report aggregates one run without naming organizations.
type Report struct {
	// Roles counts role principals that received, or would receive, grants.
	Roles int

	// GrantsAdded counts the grant rows written, or that would be written.
	GrantsAdded int
}

// Only the built-in Admin role holds memory grants by default. Members need an
// explicit grant, so the Member role is never a candidate.
var systemRoleSlugs = []string{authz.SystemRoleAdmin}

// SystemRoleMemoryGrants returns the memory grants from a built-in role's
// canonical grant set, or nil when the role holds none by default.
func SystemRoleMemoryGrants(roleSlug string) []*authz.RoleGrant {
	var grants []*authz.RoleGrant
	for _, grant := range authz.SystemRoleGrants[roleSlug] {
		if strings.HasPrefix(grant.Scope, "memory:") {
			grants = append(grants, &authz.RoleGrant{Scope: grant.Scope, Selectors: nil})
		}
	}
	return grants
}

// Run stores the canonical memory grants for every legacy built-in Admin role
// principal. Each principal is rechecked under the role grant write lock in
// its own transaction, so rerunning is a no-op once a role holds a memory
// grant or has an audited memory grant change.
func Run(ctx context.Context, db DB, opts Options) (Report, error) {
	candidates, err := New(db).ListCandidates(ctx, ListCandidatesParams{
		RoleSlugs:      systemRoleSlugs,
		OrganizationID: conv.ToPGTextEmpty(opts.OrganizationID),
	})
	if err != nil {
		return Report{}, fmt.Errorf("list candidate roles: %w", err)
	}

	report := Report{Roles: 0, GrantsAdded: 0}
	for _, candidate := range candidates {
		added, err := backfillRole(ctx, db, candidate, opts.Apply)
		if err != nil {
			return report, fmt.Errorf("backfill organization %s: %w", candidate.OrganizationID, err)
		}
		if added > 0 {
			report.Roles++
			report.GrantsAdded += added
		}
	}
	return report, nil
}

func backfillRole(ctx context.Context, db DB, candidate ListCandidatesRow, apply bool) (int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin role transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	principalURN := candidate.PrincipalUrn.String()
	q := New(tx)
	// Role edits take the same lock in PatchRoleGrantsTx. A revocation that
	// commits first is seen by the recheck below; one that starts later waits
	// for this transaction and then removes what it wrote.
	if err := authz.LockRolePrincipalGrants(ctx, tx, candidate.OrganizationID, principalURN); err != nil {
		return 0, fmt.Errorf("lock candidate role: %w", err)
	}
	current, err := q.ListCandidates(ctx, ListCandidatesParams{
		RoleSlugs:      []string{candidate.RoleSlug},
		OrganizationID: pgtype.Text{String: candidate.OrganizationID, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("recheck candidate role: %w", err)
	}
	stillCandidate := false
	for _, row := range current {
		if row.PrincipalUrn.String() == principalURN {
			stillCandidate = true
			break
		}
	}
	if !stillCandidate {
		return 0, nil
	}

	grants := SystemRoleMemoryGrants(candidate.RoleSlug)
	if len(grants) == 0 {
		return 0, nil
	}
	if _, err := authz.PatchRoleGrantsTx(ctx, tx, candidate.OrganizationID, candidate.RoleSlug, principalURN, grants, nil); err != nil {
		return 0, fmt.Errorf("add memory grants to %s role: %w", candidate.RoleSlug, err)
	}

	if apply {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit role transaction: %w", err)
		}
	}
	return len(grants), nil
}
