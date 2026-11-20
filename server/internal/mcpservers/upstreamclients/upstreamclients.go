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

// Reconcile prunes retired associations and synchronizes their typed offers.
// Never infer an offer from a shared sign-in issuer.
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

// ReconcileInLockedTransaction prunes and synchronizes associations atomically.
// The caller must hold LockEMAWriterScope before its first mutation or row lock.
func ReconcileInLockedTransaction(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, _ []uuid.UUID) error {
	if organizationID == "" || projectID == uuid.Nil || tx == nil {
		return errors.New("reconcile mcp server upstream clients: no transaction or tenant scope")
	}
	pruned, err := prune(ctx, repo.New(tx), organizationID, projectID)
	if err != nil {
		return err
	}
	if err := authclientbackfill.SyncServerAssociations(ctx, tx, organizationID, pruned...); err != nil {
		return fmt.Errorf("sync pruned upstream clients: %w", err)
	}
	return nil
}

// CaptureSoleConsumers records the registrations explicitly supplied by a
// registration writer, when the sign-in has exactly one server and no gateway.
// It never reads or copies issuer-registration compatibility rows.
func CaptureSoleConsumers(ctx context.Context, dbtx repo.DBTX, organizationID string, projectID uuid.UUID, userIssuerIDs, clientIDs []uuid.UUID) error {
	if len(userIssuerIDs) == 0 || len(clientIDs) == 0 {
		return nil
	}
	return inTx(ctx, dbtx, func(tx pgx.Tx) error {
		if err := authclientbackfill.LockEMAWriterScope(ctx, tx, organizationID, []uuid.UUID{projectID}); err != nil {
			return fmt.Errorf("lock upstream association writer scope: %w", err)
		}
		sole, err := repo.New(tx).CaptureSoleConsumerAssociations(ctx, repo.CaptureSoleConsumerAssociationsParams{
			OrganizationID: organizationID, ProjectID: projectID,
			UserSessionIssuerIds: userIssuerIDs, RemoteSessionClientIds: clientIDs,
		})
		if err != nil {
			return fmt.Errorf("capture explicit mcp server upstream clients: %w", err)
		}
		if err := authclientbackfill.SyncServerAssociations(ctx, tx, organizationID, sole...); err != nil {
			return fmt.Errorf("record auth clients of captured upstream clients: %w", err)
		}
		return nil
	})
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

// CredentialKindIssuanceProfile identifies an issuance selection profile:
// the choices a headless caller's new sessions are given, copied onto each
// once validated.
const CredentialKindIssuanceProfile = "issuance_profile"

// CredentialKindAPIKey identifies an agent API key credential by its
// api_keys row id. A rotation's replacement key gets the choices of the key
// it replaces.
const CredentialKindAPIKey = "api_key"

// CredentialKindExecutionProfileVersion identifies one version of an
// assistant's execution-selection profile, which the turn credentials it binds
// read their choices through.
const CredentialKindExecutionProfileVersion = "execution_profile_version"

// How an upstream client selection was made.
const (
	// SelectionSourceIssuance is a choice recorded when the credential was
	// issued.
	SelectionSourceIssuance = "issuance"

	// SelectionSourceBackfill is a choice inferred for an existing credential
	// by the offline backfill or configuration lifecycle maintenance.
	SelectionSourceBackfill = "backfill"

	// SelectionSourceNewMember is a choice recorded for a gateway member
	// added after the credential was issued, the first time it was used.
	SelectionSourceNewMember = "new_member"

	// SelectionSourceAdmin is a choice an administrator made.
	SelectionSourceAdmin = "admin"
)
