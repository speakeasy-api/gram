package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func BuildUserSessionView(row repo.ListUserSessionsByProjectIDRow) *types.UserSession {
	return &types.UserSession{
		ID:                  row.ID.String(),
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		SubjectUrn:          row.SubjectUrn.String(),
		Jti:                 row.Jti,
		RefreshExpiresAt:    row.RefreshExpiresAt.Time.Format(time.RFC3339),
		ExpiresAt:           row.ExpiresAt.Time.Format(time.RFC3339),
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func BuildUserSessionListView(rows []repo.ListUserSessionsByProjectIDRow) []*types.UserSession {
	out := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		out[i] = BuildUserSessionView(row)
	}
	return out
}
