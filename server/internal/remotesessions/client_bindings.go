package remotesessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// remoteSessionClientWithIssuers pairs a client row with the user_session_issuer
// ids it is attached to (from the join table), giving the two project-scoped
// list queries a single shape for the view builder.
type remoteSessionClientWithIssuers struct {
	Client               repo.RemoteSessionClient
	UserSessionIssuerIDs []uuid.UUID
}

// listRemoteSessionClientsByProjectID reads the project's own clients plus
// organization-level clients (project_id NULL) belonging to organizationID.
// When filtering by user_session_issuer_id it resolves the client / user
// session issuer relationship solely through the
// remote_session_client_user_session_issuers join table.
func (s *Service) listRemoteSessionClientsByProjectID(
	ctx context.Context,
	projectID uuid.UUID,
	organizationID string,
	remoteSessionIssuerID uuid.NullUUID,
	userSessionIssuerID uuid.NullUUID,
	cursor uuid.NullUUID,
	limit int32,
) ([]remoteSessionClientWithIssuers, error) {
	q := repo.New(s.db)
	if !userSessionIssuerID.Valid {
		rows, err := q.ListRemoteSessionClientsByProjectID(ctx, repo.ListRemoteSessionClientsByProjectIDParams{
			ProjectID:             projectID,
			OrganizationID:        conv.ToPGText(organizationID),
			RemoteSessionIssuerID: remoteSessionIssuerID,
			Cursor:                cursor,
			LimitValue:            limit,
		})
		if err != nil {
			return nil, fmt.Errorf("list remote session clients by project: %w", err)
		}
		out := make([]remoteSessionClientWithIssuers, 0, len(rows))
		for _, row := range rows {
			out = append(out, remoteSessionClientWithIssuers{Client: row.RemoteSessionClient, UserSessionIssuerIDs: row.UserSessionIssuerIds})
		}
		return out, nil
	}

	rows, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		UserSessionIssuerID:   userSessionIssuerID.UUID,
		ProjectID:             projectID,
		OrganizationID:        conv.ToPGText(organizationID),
		RemoteSessionIssuerID: remoteSessionIssuerID,
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session clients by project for user_session_issuer: %w", err)
	}
	out := make([]remoteSessionClientWithIssuers, 0, len(rows))
	for _, row := range rows {
		out = append(out, remoteSessionClientWithIssuers{Client: row.RemoteSessionClient, UserSessionIssuerIDs: row.UserSessionIssuerIds})
	}
	return out, nil
}

// listRemoteSessionClientRowsForUserSessionIssuer is the runtime counterpart to
// listRemoteSessionClientsByProjectID: it resolves the clients linked to a user
// session issuer solely through the join table, including organization-level
// clients (project_id NULL) belonging to organizationID. Used by consent
// rendering and token resolution.
func (m *ChallengeManager) listRemoteSessionClientRowsForUserSessionIssuer(
	ctx context.Context,
	projectID uuid.UUID,
	organizationID string,
	userSessionIssuerID uuid.UUID,
) ([]repo.ListRemoteSessionClientsForUserSessionIssuerRow, error) {
	rows, err := repo.New(m.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(projectID),
		OrganizationID:      conv.ToPGText(organizationID),
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session clients for user_session_issuer: %w", err)
	}
	return rows, nil
}
