package repo

import "context"

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
