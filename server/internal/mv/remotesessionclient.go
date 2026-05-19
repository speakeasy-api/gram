package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func BuildRemoteSessionClientView(row repo.RemoteSessionClient) *types.RemoteSessionClient {
	var issuedAt string
	if row.ClientIDIssuedAt.Valid {
		issuedAt = row.ClientIDIssuedAt.Time.Format(time.RFC3339)
	}
	var expiresAt *string
	if row.ClientSecretExpiresAt.Valid {
		s := row.ClientSecretExpiresAt.Time.Format(time.RFC3339)
		expiresAt = &s
	}
	return &types.RemoteSessionClient{
		ID:                      row.ID.String(),
		ProjectID:               row.ProjectID.String(),
		RemoteSessionIssuerID:   row.RemoteSessionIssuerID.String(),
		UserSessionIssuerID:     row.UserSessionIssuerID.String(),
		ClientID:                row.ClientID,
		ClientIDIssuedAt:        issuedAt,
		ClientSecretExpiresAt:   expiresAt,
		TokenEndpointAuthMethod: conv.FromPGText[string](row.TokenEndpointAuthMethod),
		Scope:                   row.Scope,
		CreatedAt:               row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:               row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
