// Package upstreamclients records which upstream OAuth clients (remote
// session clients) each MCP server uses.
//
// An association is a configuration fact about one server, unlike a client's
// binding to a user session issuer, which every server sharing that issuer
// inherits. Associations are written ahead of the serving path that will read
// them: until then they are kept to the combinations the issuer bindings would
// serve, so an association never names a client the current runtime would
// refuse for that server.
package upstreamclients

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
)

// Reconcile brings the project's associations in line with its issuer
// bindings after a binding, server or gateway membership change: it
// associates each client bound to one of userIssuerIDs with that issuer's
// server when the server is the issuer's only consumer, associates each
// gateway-fronted server with the one client its gateway issuer binds for
// the server's derived authorization server, then drops associations no
// issuer reaching the server still binds. Otherwise associations on an issuer
// shared by several servers are only added by explicit configuration or the
// backfill: the gateway capture is the one path that writes them, and only
// for the single client a gateway issuer binds for a server's derived
// authorization server.
//
// Each statement decides from the bindings as they are when it runs, and the
// prune runs after the captures, so a reconcile that races a binding change
// and captures a binding removed meanwhile prunes it again before it returns.
//
// The associations it captures get their typed auth clients and server links
// in the same transaction, as every association write does. dbtx is either
// the caller's transaction or a pool, from which a reconcile that captures
// begins its own. Transaction callers must acquire LockEMAWriterScope before
// their first row lock or mutation, not only when they reach this function.
func Reconcile(ctx context.Context, dbtx repo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil {
		return errors.New("reconcile mcp server upstream clients: no tenant scope")
	}
	return inTx(ctx, dbtx, func(tx pgx.Tx) error {
		// This must precede capture's foreign-key locks, not merely the typed
		// client/copy writes. Existing transactions must preflight before their
		// own earlier mutations; reacquiring their scope here is harmless.
		if err := authclientbackfill.LockEMAWriterScope(ctx, tx, organizationID, []uuid.UUID{projectID}); err != nil {
			return fmt.Errorf("lock upstream association writer scope: %w", err)
		}
		return ReconcileInLockedTransaction(ctx, tx, organizationID, projectID, userIssuerIDs)
	})
}

// ReconcileInLockedTransaction captures and prunes associations atomically.
// The caller must hold LockEMAWriterScope before its first mutation or row lock.
func ReconcileInLockedTransaction(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, userIssuerIDs []uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil || tx == nil {
		return errors.New("reconcile mcp server upstream clients: no transaction or tenant scope")
	}
	q := repo.New(tx)
	if len(userIssuerIDs) == 0 {
		servers, err := prune(ctx, q, organizationID, projectID)
		if err != nil {
			return err
		}
		if err := authclientbackfill.SyncServerAssociations(ctx, tx, organizationID, servers...); err != nil {
			return fmt.Errorf("sync pruned upstream clients: %w", err)
		}
		return nil
	}
	sole, err := q.CaptureSoleConsumerAssociations(ctx, repo.CaptureSoleConsumerAssociationsParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		UserSessionIssuerIds: userIssuerIDs,
	})
	if err != nil {
		return fmt.Errorf("capture mcp server upstream clients: %w", err)
	}
	gateway, err := q.CaptureGatewayDerivedAssociations(ctx, repo.CaptureGatewayDerivedAssociationsParams{
		ProjectID:            projectID,
		OrganizationID:       organizationID,
		UserSessionIssuerIds: userIssuerIDs,
	})
	if err != nil {
		return fmt.Errorf("capture gateway upstream clients: %w", err)
	}
	pruned, err := prune(ctx, q, organizationID, projectID)
	if err != nil {
		return err
	}
	// After pruning, publish only the captured associations that remain.
	// The writer scope predates capture and protects OAuth and EMA copies.
	if err := authclientbackfill.SyncServerAssociations(ctx, tx, organizationID, append(append(sole, gateway...), pruned...)...); err != nil {
		return fmt.Errorf("record auth clients of captured upstream clients: %w", err)
	}
	return nil
}

func prune(ctx context.Context, q *repo.Queries, organizationID string, projectID uuid.UUID) ([]uuid.UUID, error) {
	servers, err := q.PruneUnservableAssociations(ctx, repo.PruneUnservableAssociationsParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("prune mcp server upstream clients: %w", err)
	}
	return servers, nil
}

// inTx runs fn in dbtx when it is a transaction, and otherwise in a
// transaction begun from dbtx and committed when fn succeeds.
func inTx(ctx context.Context, dbtx repo.DBTX, fn func(tx pgx.Tx) error) error {
	if tx, ok := dbtx.(pgx.Tx); ok {
		return fn(tx)
	}
	beginner, ok := dbtx.(interface {
		Begin(ctx context.Context) (pgx.Tx, error)
	})
	if !ok {
		return fmt.Errorf("reconcile mcp server upstream clients: %T can't begin a transaction", dbtx)
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile mcp server upstream clients: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reconcile mcp server upstream clients: commit: %w", err)
	}
	return nil
}

// CredentialKindUserSession identifies a user session credential: the
// lineage of user_sessions rows that refresh rotation continues, named by
// COALESCE(credential_id, id).
const CredentialKindUserSession = "user_session"

// CredentialKindExecutionProfileVersion identifies one version of an
// assistant's execution-selection profile, which the turn credentials it binds
// read their choices through.
const CredentialKindExecutionProfileVersion = "execution_profile_version"

// How an upstream client selection was made.
const (
	// SelectionSourceIssuance is a choice recorded when the credential was
	// issued.
	SelectionSourceIssuance = "issuance"

	// SelectionSourceBackfill is a choice recorded for an existing credential
	// by the offline backfill.
	SelectionSourceBackfill = "backfill"
)
