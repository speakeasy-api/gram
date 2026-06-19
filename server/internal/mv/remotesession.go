package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// BuildRemoteSessionView converts a remote_sessions row into its API view.
// subjectDisplayName and subjectEmail carry the resolved identity when the
// subject is a Gram user; pass nil for apikey/anonymous subjects or when the
// user could not be resolved.
func BuildRemoteSessionView(row repo.RemoteSession, subjectDisplayName, subjectEmail *string) *types.RemoteSession {
	var refreshExpiresAt *string
	if row.RefreshExpiresAt.Valid {
		v := row.RefreshExpiresAt.Time.Format(time.RFC3339)
		refreshExpiresAt = &v
	}
	return &types.RemoteSession{
		ID:                    row.ID.String(),
		SubjectUrn:            row.SubjectUrn.String(),
		SubjectDisplayName:    subjectDisplayName,
		SubjectEmail:          subjectEmail,
		UserSessionIssuerID:   row.UserSessionIssuerID.String(),
		RemoteSessionClientID: row.RemoteSessionClientID.String(),
		AccessExpiresAt:       row.AccessExpiresAt.Time.Format(time.RFC3339),
		RefreshExpiresAt:      refreshExpiresAt,
		HasRefreshToken:       row.RefreshTokenEncrypted.Valid && row.RefreshTokenEncrypted.String != "",
		Scopes:                row.Scopes,
		CreatedAt:             row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
