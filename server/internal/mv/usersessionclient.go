package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// BuildUserSessionClientView converts a repo client row into the API response
// type. client_secret_hash is intentionally omitted — it is never returned
// over the management API.
func BuildUserSessionClientView(row repo.UserSessionClient) *types.UserSessionClient {
	var clientSecretExpiresAt *string
	if row.ClientSecretExpiresAt.Valid {
		s := row.ClientSecretExpiresAt.Time.Format(time.RFC3339)
		clientSecretExpiresAt = &s
	}

	return &types.UserSessionClient{
		ID:                    row.ID.String(),
		UserSessionIssuerID:   row.UserSessionIssuerID.String(),
		ClientID:              row.ClientID,
		ClientName:            row.ClientName,
		RedirectUris:          row.RedirectUris,
		ClientIDIssuedAt:      row.ClientIDIssuedAt.Time.Format(time.RFC3339),
		ClientSecretExpiresAt: clientSecretExpiresAt,
		CreatedAt:             row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildUserSessionClientListView converts a slice of repo rows into a slice of
// API response types, preserving order.
func BuildUserSessionClientListView(rows []repo.UserSessionClient) []*types.UserSessionClient {
	out := make([]*types.UserSessionClient, len(rows))
	for i, row := range rows {
		out[i] = BuildUserSessionClientView(row)
	}
	return out
}
