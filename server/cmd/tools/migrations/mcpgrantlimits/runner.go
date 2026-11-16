// Package mcpgrantlimits rewrites stored view and manage rules that still
// carry a tool or disposition limit into the form they take effect as; see
// MCP_GRANT_LIMITS_REWRITE.md.
package mcpgrantlimits

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// DB is the connection the rewrite reads principals from and opens one
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
	// Principals counts principals with at least one rule rewritten.
	Principals int

	// LimitsRemoved counts view and manage rules stored again without their
	// limit.
	LimitsRemoved int

	// Merged counts limited view and manage rules deleted because the
	// principal already held the same rule without a limit.
	Merged int

	// BlocksRemoved counts limited view and manage blocks deleted. They never
	// matched a check.
	BlocksRemoved int
}

func limitScopes() []string {
	scopes := authz.StoredLimitScopes()
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, string(scope))
	}
	return out
}

// Run rewrites every limited view or manage rule, and every limited block on
// either, into what authz.StoredSelectorInEffect already loads it as. No
// decision changes. Each principal is handled in its own transaction with its
// limited rows locked, so rerunning finds nothing left to do.
func Run(ctx context.Context, db DB, opts Options) (Report, error) {
	principals, err := New(db).ListLimitedPrincipals(ctx, ListLimitedPrincipalsParams{
		Scopes:         limitScopes(),
		OrganizationID: conv.ToPGTextEmpty(opts.OrganizationID),
	})
	if err != nil {
		return Report{}, fmt.Errorf("list principals with limited rules: %w", err)
	}

	report := Report{Principals: 0, LimitsRemoved: 0, Merged: 0, BlocksRemoved: 0}
	for _, principal := range principals {
		changed, err := rewritePrincipal(ctx, db, principal, opts.Apply)
		if err != nil {
			return report, fmt.Errorf("rewrite organization %s: %w", principal.OrganizationID, err)
		}
		if changed.LimitsRemoved+changed.Merged+changed.BlocksRemoved == 0 {
			continue
		}
		report.Principals++
		report.LimitsRemoved += changed.LimitsRemoved
		report.Merged += changed.Merged
		report.BlocksRemoved += changed.BlocksRemoved
	}
	return report, nil
}

func rewritePrincipal(ctx context.Context, db DB, principal ListLimitedPrincipalsRow, apply bool) (Report, error) {
	changed := Report{Principals: 0, LimitsRemoved: 0, Merged: 0, BlocksRemoved: 0}

	tx, err := db.Begin(ctx)
	if err != nil {
		return changed, fmt.Errorf("begin principal transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	// Role edits take this lock before they replace a role's rules, so one
	// cannot interleave with the rewrite of the same role.
	if err := authz.LockRolePrincipalGrants(ctx, tx, principal.OrganizationID, principal.PrincipalUrn.String()); err != nil {
		return changed, fmt.Errorf("lock principal rules: %w", err)
	}

	q := New(tx)
	rows, err := q.LockLimitedGrants(ctx, LockLimitedGrantsParams{
		OrganizationID: principal.OrganizationID,
		PrincipalUrn:   principal.PrincipalUrn,
		Scopes:         limitScopes(),
	})
	if err != nil {
		return changed, fmt.Errorf("lock limited rules: %w", err)
	}

	for _, row := range rows {
		scope := authz.Scope(row.Scope)
		stored, err := authz.SelectorFromRow(row.Selectors)
		if err != nil {
			return changed, fmt.Errorf("read stored selector: %w", err)
		}
		inEffect, ok := authz.StoredSelectorInEffect(scope, stored)
		if !ok {
			if err := removeLimitedGrant(ctx, q, principal.OrganizationID, row); err != nil {
				return changed, err
			}
			changed.BlocksRemoved++
			continue
		}

		unlimited, err := inEffect.MarshalJSON()
		if err != nil {
			return changed, fmt.Errorf("encode selector: %w", err)
		}
		twins, err := q.LockGrantBySelectors(ctx, LockGrantBySelectorsParams{
			OrganizationID: principal.OrganizationID,
			PrincipalUrn:   principal.PrincipalUrn,
			Scope:          row.Scope,
			Selectors:      unlimited,
		})
		if err != nil {
			return changed, fmt.Errorf("look up the rule without its limit: %w", err)
		}
		if len(twins) == 0 {
			affected, err := q.SetGrantSelectors(ctx, SetGrantSelectorsParams{Selectors: unlimited, ID: row.ID, OrganizationID: principal.OrganizationID})
			if err != nil {
				return changed, fmt.Errorf("store rule without its limit: %w", err)
			}
			if affected != 1 {
				return changed, fmt.Errorf("store rule without its limit: %d rows changed", affected)
			}
			changed.LimitsRemoved++
			continue
		}

		// The principal already holds the rule without a limit. Keep that row
		// and give it whichever of the two lasts longer, so removing the
		// limited row never shortens access.
		twin := twins[0]
		if expiry := laterExpiry(twin.ExpiresAt, row.ExpiresAt); expiry != twin.ExpiresAt {
			affected, err := q.SetGrantExpiry(ctx, SetGrantExpiryParams{ExpiresAt: expiry, ID: twin.ID, OrganizationID: principal.OrganizationID})
			if err != nil {
				return changed, fmt.Errorf("extend the rule without a limit: %w", err)
			}
			if affected != 1 {
				return changed, fmt.Errorf("extend the rule without a limit: %d rows changed", affected)
			}
		}
		if err := removeLimitedGrant(ctx, q, principal.OrganizationID, row); err != nil {
			return changed, err
		}
		changed.Merged++
	}

	if apply {
		if err := tx.Commit(ctx); err != nil {
			return changed, fmt.Errorf("commit principal transaction: %w", err)
		}
	}
	return changed, nil
}

func removeLimitedGrant(ctx context.Context, q *Queries, organizationID string, row LockLimitedGrantsRow) error {
	affected, err := q.DeleteGrant(ctx, DeleteGrantParams{ID: row.ID, OrganizationID: organizationID})
	if err != nil {
		return fmt.Errorf("delete limited rule: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("delete limited rule: %d rows changed", affected)
	}
	return nil
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
