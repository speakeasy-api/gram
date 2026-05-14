package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func BuildRemoteSessionView(row repo.RemoteSession) *types.RemoteSession {
	var refreshExpiresAt *string
	if row.RefreshExpiresAt.Valid {
		v := row.RefreshExpiresAt.Time.Format(time.RFC3339)
		refreshExpiresAt = &v
	}
	return &types.RemoteSession{
		ID:                    row.ID.String(),
		SubjectUrn:            row.SubjectUrn.String(),
		UserSessionIssuerID:   row.UserSessionIssuerID.String(),
		RemoteSessionClientID: row.RemoteSessionClientID.String(),
		AccessExpiresAt:       row.AccessExpiresAt.Time.Format(time.RFC3339),
		RefreshExpiresAt:      refreshExpiresAt,
		Scopes:                row.Scopes,
		CreatedAt:             row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
