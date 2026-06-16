package remotesessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// listRemoteSessionClientsByProjectID reads project-scoped clients. When
// filtering by user_session_issuer_id it resolves the client / user session
// issuer relationship solely through the
// remote_session_client_user_session_issuers join table.
func (s *Service) listRemoteSessionClientsByProjectID(
	ctx context.Context,
	projectID uuid.UUID,
	remoteSessionIssuerID uuid.NullUUID,
	userSessionIssuerID uuid.NullUUID,
	cursor uuid.NullUUID,
	limit int32,
) ([]repo.RemoteSessionClient, error) {
	q := repo.New(s.db)
	if !userSessionIssuerID.Valid {
		clients, err := q.ListRemoteSessionClientsByProjectID(ctx, repo.ListRemoteSessionClientsByProjectIDParams{
			ProjectID:             conv.ToNullUUID(projectID),
			RemoteSessionIssuerID: remoteSessionIssuerID,
			Cursor:                cursor,
			LimitValue:            limit,
		})
		if err != nil {
			return nil, fmt.Errorf("list remote session clients by project: %w", err)
		}
		return clients, nil
	}

	rows, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		UserSessionIssuerID:   userSessionIssuerID.UUID,
		ProjectID:             projectID,
		RemoteSessionIssuerID: remoteSessionIssuerID,
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session clients by project for user_session_issuer: %w", err)
	}
	return rows, nil
}

// listRemoteSessionClientRowsForUserSessionIssuer is the runtime counterpart to
// listRemoteSessionClientsByProjectID: it resolves the clients linked to a user
// session issuer solely through the join table. Used by consent rendering and
// token resolution.
func (m *ChallengeManager) listRemoteSessionClientRowsForUserSessionIssuer(
	ctx context.Context,
	projectID uuid.UUID,
	userSessionIssuerID uuid.UUID,
) ([]repo.ListRemoteSessionClientsForUserSessionIssuerRow, error) {
	rows, err := repo.New(m.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(projectID),
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session clients for user_session_issuer: %w", err)
	}
	return rows, nil
}
