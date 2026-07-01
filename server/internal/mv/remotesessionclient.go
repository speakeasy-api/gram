package mv

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// BuildRemoteSessionClientView renders a remote_session_client API view. The
// user_session_issuer attachments come from the join table via
// userSessionIssuerIDs, never the legacy remote_session_clients column.
//
// Organization-level clients have a null project_id (and a set organization_id);
// project_id serializes as an empty string for them, mirroring
// BuildRemoteSessionIssuerView. The error return is retained for call-site
// stability even though every value currently renders successfully.
func BuildRemoteSessionClientView(row repo.RemoteSessionClient, userSessionIssuerIDs []uuid.UUID) (*types.RemoteSessionClient, error) {
	projectID := ""
	if row.ProjectID.Valid {
		projectID = row.ProjectID.UUID.String()
	}

	organizationID := ""
	if row.OrganizationID.Valid {
		organizationID = row.OrganizationID.String
	}

	var issuedAt string
	if row.ClientIDIssuedAt.Valid {
		issuedAt = row.ClientIDIssuedAt.Time.Format(time.RFC3339)
	}
	var expiresAt *string
	if row.ClientSecretExpiresAt.Valid {
		s := row.ClientSecretExpiresAt.Time.Format(time.RFC3339)
		expiresAt = &s
	}
	issuerIDs := make([]string, 0, len(userSessionIssuerIDs))
	for _, id := range userSessionIssuerIDs {
		issuerIDs = append(issuerIDs, id.String())
	}
	return &types.RemoteSessionClient{
		ID:                      row.ID.String(),
		ProjectID:               projectID,
		OrganizationID:          organizationID,
		RemoteSessionIssuerID:   row.RemoteSessionIssuerID.String(),
		UserSessionIssuerIds:    issuerIDs,
		ClientID:                row.ClientID,
		ClientIDMetadataURI:     conv.FromPGText[string](row.ClientIDMetadataUri),
		ClientIDIssuedAt:        issuedAt,
		ClientSecretExpiresAt:   expiresAt,
		TokenEndpointAuthMethod: conv.FromPGText[string](row.TokenEndpointAuthMethod),
		Scope:                   row.Scope,
		Audience:                conv.FromPGText[string](row.Audience),
		CreatedAt:               row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:               row.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}
