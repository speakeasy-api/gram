// Package assistantrolegrants stores the canonical assistant grants for
// built-in Admin and Member roles seeded before the assistant scopes existed;
// see ASSISTANT_ROLE_GRANTS_BACKFILL.md.
package assistantrolegrants

import (
	"context"
	"fmt"

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

var systemRoleSlugs = []string{authz.SystemRoleAdmin, authz.SystemRoleMember}

// Run stores the canonical assistant grants for every legacy built-in role
// principal. Each principal is rechecked under a lock in its own transaction,
// so rerunning is a no-op once a role holds an assistant grant or has an
// audited assistant grant change.
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
			return report, err
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
	if err := q.LockCandidate(ctx, LockCandidateParams{OrganizationID: candidate.OrganizationID, PrincipalUrn: principalURN}); err != nil {
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

	grants := authz.SystemRoleAssistantGrants(candidate.RoleSlug)
	if len(grants) == 0 {
		return 0, nil
	}
	if _, err := authz.PatchRoleGrantsTx(ctx, tx, candidate.OrganizationID, candidate.RoleSlug, principalURN, grants, nil); err != nil {
		return 0, fmt.Errorf("add assistant grants to %s role: %w", candidate.RoleSlug, err)
	}

	if apply {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("commit role transaction: %w", err)
		}
	}
	return len(grants), nil
}
