package authclientbackfill

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LockEMAPreparationScope must precede the preparation's project, issuer,
// binding and registration row locks. Source/header writers take source, then
// server, then registration locks; preparation must use that same order.
// The shared organization reach gate precedes project discovery, so a gateway
// setting cannot miss this transaction's first, still-uncommitted binding.
// Discovery includes existing and prospective ownership, not only ready bindings.
// A changed scope retries only lock acquisition, before any registration work.
func LockEMAPreparationScope(ctx context.Context, tx pgx.Tx, organizationID string, projectID, issuerID uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil || issuerID == uuid.Nil {
		return fmt.Errorf("lock EMA preparation: missing scope")
	}
	// Protect the discovery boundary, not only the projects already found.
	// Gateway settings take the exclusive counterpart before discovering them.
	if err := New(tx).LockEMAGatewayReachReader(ctx, organizationID); err != nil {
		return fmt.Errorf("lock EMA gateway reach: %w", err)
	}
	params := ListEMAPreparationLockScopeParams{OrganizationID: organizationID, ProjectID: projectID, IssuerID: issuerID}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("check scope lock context: %w", err)
		}
		attempt, err := tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin scope lock attempt: %w", err)
		}
		q := New(attempt)
		before, err := q.ListEMAPreparationLockScope(ctx, params)
		if err == nil {
			for _, row := range before {
				if row.Kind != "project" {
					continue
				}
				if err = q.LockEMAProjectScope(ctx, LockEMAProjectScopeParams{OrganizationID: organizationID, ProjectID: row.ID}); err != nil {
					break
				}
			}
		}
		if err == nil {
			// Sources are ordered by kind and UUID. Servers are a separate phase.
			for _, row := range before {
				if row.Kind == "server" || row.Kind == "project" {
					continue
				}
				if err = lockSource(ctx, attempt, organizationID, row.Kind, row.ID); err != nil {
					break
				}
			}
		}
		if err == nil {
			for _, row := range before {
				if row.Kind != "server" {
					continue
				}
				if err = q.LockEMAPreparationServer(ctx, LockEMAPreparationServerParams{OrganizationID: organizationID, ServerID: row.ID}); err != nil {
					break
				}
			}
		}
		var after []ListEMAPreparationLockScopeRow
		if err == nil {
			after, err = q.ListEMAPreparationLockScope(ctx, params)
		}
		if err != nil {
			_ = attempt.Rollback(context.WithoutCancel(ctx))
			return fmt.Errorf("lock EMA preparation scope: %w", err)
		}
		if slices.Equal(before, after) {
			if err := attempt.Commit(ctx); err != nil {
				return fmt.Errorf("commit scope locks: %w", err)
			}
			return nil
		}
		// A source/server appeared or moved while locks were acquired. Releasing
		// the savepoint's locks lets us restart in order, without caller retries.
		if err := attempt.Rollback(ctx); err != nil {
			return fmt.Errorf("rollback scope lock attempt: %w", err)
		}
	}
}
