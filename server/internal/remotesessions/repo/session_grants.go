package repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// UpsertRemoteSessionParams preserves the session repository contract while the
// compatibility query supports both the old and resource-qualified indexes.
type UpsertRemoteSessionParams = UpsertRemoteSessionGrantParams

// UpsertRemoteSession materializes a grant without relying on an index-specific
// conflict target. Consent callers must hold the registration's write lock.
func (q *Queries) UpsertRemoteSession(ctx context.Context, arg UpsertRemoteSessionParams) (RemoteSession, error) {
	row, err := q.UpsertRemoteSessionGrant(ctx, arg)
	if err != nil {
		return RemoteSession{}, err
	}
	return RemoteSession{
		ID:                     row.ID,
		GrantGeneration:        row.GrantGeneration,
		SubjectUrn:             arg.SubjectUrn,
		UserSessionIssuerID:    row.UserSessionIssuerID,
		RemoteSessionClientID:  row.RemoteSessionClientID,
		AccessTokenEncrypted:   row.AccessTokenEncrypted,
		AccessExpiresAt:        row.AccessExpiresAt,
		RefreshTokenEncrypted:  row.RefreshTokenEncrypted,
		AuthorizationExpiresAt: row.AuthorizationExpiresAt,
		RefreshExpiresAt:       row.RefreshExpiresAt,
		Scopes:                 row.Scopes,
		Resource:               row.Resource,
		AutoRefresh:            row.AutoRefresh,
		LastRefreshAttemptAt:   row.LastRefreshAttemptAt,
		LastUsedAt:             row.LastUsedAt,
		UpstreamSubject:        row.UpstreamSubject,
		UpstreamEmail:          row.UpstreamEmail,
		UpstreamDisplayName:    row.UpstreamDisplayName,
		IdentitySource:         row.IdentitySource,
		Enrichment:             row.Enrichment,
		LastValidatedAt:        row.LastValidatedAt,
		ValidationStatus:       row.ValidationStatus,
		ValidationReason:       row.ValidationReason,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
		DeletedAt:              row.DeletedAt,
		Deleted:                row.Deleted,
	}, nil
}

// UpdateRemoteSessionTokensIfUnchangedParams keeps refresh callers independent
// of the serialization needed to backfill a legacy grant's audience.
type UpdateRemoteSessionTokensIfUnchangedParams = StoreRemoteSessionTokensIfUnchangedParams

// UpdateRemoteSessionTokensIfUnchanged serializes resource backfill with consent.
// Take the registration lock in a separate statement so the CAS sees any consent
// that committed while we waited. Locking within the UPDATE would use an older
// statement snapshot and could lose provider-rotated credentials to a unique violation.
func (q *Queries) UpdateRemoteSessionTokensIfUnchanged(ctx context.Context, arg UpdateRemoteSessionTokensIfUnchangedParams) (RemoteSession, error) {
	if !arg.BackfillResource.Valid || arg.BackfillResource.String == "" {
		return q.StoreRemoteSessionTokensIfUnchanged(ctx, arg)
	}
	beginner, ok := q.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return RemoteSession{}, fmt.Errorf("session token storage requires transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return RemoteSession{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	queries := q.WithTx(tx)
	if _, err := queries.LockRemoteSessionClientForSessionWrite(ctx, arg.RemoteSessionClientID); err != nil {
		return RemoteSession{}, err
	}
	row, err := queries.StoreRemoteSessionTokensIfUnchanged(ctx, arg)
	if err != nil {
		return RemoteSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RemoteSession{}, err
	}
	return row, nil
}
