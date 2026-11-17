// Package mcpunannotatedgrants adds, beside every stored MCP connect rule or
// block limited to a disposition, the same rule or block for unannotated
// tools, so tools without annotations keep the access they had before they
// got a disposition of their own; see MCP_UNANNOTATED_GRANTS_BACKFILL.md.
package mcpunannotatedgrants

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// DB is the connection the backfill reads principals from and opens one
// transaction per principal on.
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

// Report aggregates one run without naming organizations or principals.
type Report struct {
	// Principals counts principals with at least one row added or extended.
	Principals int

	// Added counts unannotated rules and blocks inserted.
	Added int

	// Extended counts unannotated rules and blocks the principal already held
	// whose expiry was moved later to match the limited row beside them.
	Extended int
}

// siblingScopes are the stored scopes whose disposition limit used to reach
// tools without annotations: a tool call without annotations carried no
// disposition, and an absent disposition matched a rule or block limited to
// any disposition.
func siblingScopes() []string {
	return []string{string(authz.ScopeMCPConnect), string(authz.ScopeMCPBlockedConnect)}
}

// Run adds the unannotated twin of every disposition-limited connect rule
// and block that does not already have one, with the limited row's expiry.
// A twin that exists but expires sooner is extended. Each principal is
// handled in its own transaction with its limited rows locked, so rerunning
// finds nothing left to do.
func Run(ctx context.Context, db DB, opts Options) (Report, error) {
	principals, err := New(db).ListDispositionLimitedPrincipals(ctx, ListDispositionLimitedPrincipalsParams{
		Scopes:         siblingScopes(),
		Unannotated:    authz.DispositionUnannotated,
		OrganizationID: conv.ToPGTextEmpty(opts.OrganizationID),
	})
	if err != nil {
		return Report{}, fmt.Errorf("list principals with disposition-limited rules: %w", err)
	}

	report := Report{Principals: 0, Added: 0, Extended: 0}
	for _, principal := range principals {
		changed, err := backfillPrincipal(ctx, db, principal, opts.Apply)
		if err != nil {
			return report, fmt.Errorf("backfill a principal's rules: %w", err)
		}
		if changed.Added+changed.Extended == 0 {
			continue
		}
		report.Principals++
		report.Added += changed.Added
		report.Extended += changed.Extended
	}
	return report, nil
}

func backfillPrincipal(ctx context.Context, db DB, principal ListDispositionLimitedPrincipalsRow, apply bool) (Report, error) {
	changed := Report{Principals: 0, Added: 0, Extended: 0}

	tx, err := db.Begin(ctx)
	if err != nil {
		return changed, fmt.Errorf("begin principal transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	// Role edits take this lock before they replace a role's rules, so one
	// cannot interleave with the backfill of the same role.
	if err := authz.LockRolePrincipalGrants(ctx, tx, principal.OrganizationID, principal.PrincipalUrn.String()); err != nil {
		return changed, fmt.Errorf("lock principal rules: %w", err)
	}

	q := New(tx)
	rows, err := q.LockDispositionLimitedGrants(ctx, LockDispositionLimitedGrantsParams{
		OrganizationID: principal.OrganizationID,
		PrincipalUrn:   principal.PrincipalUrn,
		Scopes:         siblingScopes(),
		Unannotated:    authz.DispositionUnannotated,
	})
	if err != nil {
		return changed, fmt.Errorf("lock disposition-limited rules: %w", err)
	}

	for _, row := range rows {
		stored, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			return changed, fmt.Errorf("read stored selector: %w", err)
		}
		twin := make(authz.Selector, len(stored))
		for key, value := range stored {
			twin[key] = value
		}
		twin[authz.SelectorKeyDisposition] = authz.DispositionUnannotated
		if err := authz.ValidateSelector(authz.Scope(row.Scope), twin); err != nil {
			return changed, fmt.Errorf("build unannotated selector: %w", err)
		}
		selectors, err := twin.MarshalJSON()
		if err != nil {
			return changed, fmt.Errorf("encode selector: %w", err)
		}

		existing, err := q.LockGrantBySelectors(ctx, LockGrantBySelectorsParams{
			OrganizationID: principal.OrganizationID,
			PrincipalUrn:   principal.PrincipalUrn,
			Scope:          row.Scope,
			Selectors:      selectors,
		})
		if err != nil {
			return changed, fmt.Errorf("look up the unannotated rule: %w", err)
		}
		if len(existing) == 0 {
			affected, err := q.InsertGrant(ctx, InsertGrantParams{
				OrganizationID: principal.OrganizationID,
				PrincipalUrn:   principal.PrincipalUrn,
				Scope:          row.Scope,
				Selectors:      selectors,
				ExpiresAt:      row.ExpiresAt,
			})
			if err != nil {
				return changed, fmt.Errorf("add the unannotated rule: %w", err)
			}
			if affected != 1 {
				return changed, fmt.Errorf("add the unannotated rule: %d rows changed", affected)
			}
			changed.Added++
			continue
		}

		// The principal already holds the unannotated row, possibly added for
		// another limited row earlier in this loop. Give it whichever expiry
		// lasts longer, so it never ends before the access it stands in for.
		current := existing[0]
		if expiry := laterExpiry(current.ExpiresAt, row.ExpiresAt); expiry != current.ExpiresAt {
			affected, err := q.SetGrantExpiry(ctx, SetGrantExpiryParams{ExpiresAt: expiry, ID: current.ID, OrganizationID: principal.OrganizationID})
			if err != nil {
				return changed, fmt.Errorf("extend the unannotated rule: %w", err)
			}
			if affected != 1 {
				return changed, fmt.Errorf("extend the unannotated rule: %d rows changed", affected)
			}
			changed.Extended++
		}
	}

	if apply {
		if err := tx.Commit(ctx); err != nil {
			return changed, fmt.Errorf("commit principal transaction: %w", err)
		}
	}
	return changed, nil
}

// laterExpiry returns the expiry that keeps a rule in force longer. A rule
// with no expiry is permanent and outlasts any dated one.
func laterExpiry(a, b pgtype.Timestamptz) pgtype.Timestamptz {
	if !a.Valid {
		return a
	}
	if !b.Valid || b.Time.After(a.Time) {
		return b
	}
	return a
}
