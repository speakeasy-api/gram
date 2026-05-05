package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func BuildUserSessionConsentView(row repo.UserSessionConsent) *types.UserSessionConsent {
	return &types.UserSessionConsent{
		ID:                  row.ID.String(),
		SubjectUrn:          row.SubjectUrn.String(),
		UserSessionClientID: row.UserSessionClientID.String(),
		RemoteSetHash:       row.RemoteSetHash,
		ConsentedAt:         row.ConsentedAt.Time.Format(time.RFC3339),
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func BuildUserSessionConsentListView(rows []repo.UserSessionConsent) []*types.UserSessionConsent {
	out := make([]*types.UserSessionConsent, len(rows))
	for i, row := range rows {
		out[i] = BuildUserSessionConsentView(row)
	}
	return out
}
