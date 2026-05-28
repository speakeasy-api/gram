package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

type remoteSessionClientIssuerDriftError struct {
	UserSessionIssuerID uuid.UUID
	Count               int64
}

func (e *remoteSessionClientIssuerDriftError) Error() string {
	return fmt.Sprintf("multiple active legacy remote_session_clients found for user_session_issuer %s: %d", e.UserSessionIssuerID, e.Count)
}

func isRemoteSessionClientIssuerDrift(err error) bool {
	var driftErr *remoteSessionClientIssuerDriftError
	if errors.As(err, &driftErr) {
		return true
	}

	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "remote_session_client_user_session_issuers_one_per_issuer"
}

func backfillRemoteSessionClientUserSessionIssuer(
	ctx context.Context,
	db *pgxpool.Pool,
	remoteSessionClientID uuid.UUID,
	userSessionIssuerID uuid.UUID,
) error {
	dbtx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin remote session client binding backfill: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := repo.New(dbtx).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: remoteSessionClientID,
		UserSessionIssuerID:   userSessionIssuerID,
	}); err != nil {
		return fmt.Errorf("insert remote session client binding backfill: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit remote session client binding backfill: %w", err)
	}
	return nil
}

func logRemoteSessionClientLegacyBindingFallback(
	ctx context.Context,
	logger *slog.Logger,
	projectID uuid.UUID,
	userSessionIssuerID uuid.UUID,
	rowCount int,
) {
	logger.WarnContext(
		ctx,
		"using legacy remote session client user_session_issuer binding",
		attr.SlogProjectID(projectID.String()),
		slog.String("user_session_issuer_id", userSessionIssuerID.String()),
		slog.Int("legacy_row_count", rowCount),
	)
}

func ensureLegacyRemoteSessionClientRowsBackfilled(
	ctx context.Context,
	db *pgxpool.Pool,
	logger *slog.Logger,
	projectID uuid.UUID,
	userSessionIssuerID uuid.UUID,
	rows []repo.RemoteSessionClient,
) error {
	if len(rows) == 0 {
		return nil
	}

	q := repo.New(db)
	count, err := q.CountLegacyRemoteSessionClientsForUserSessionIssuer(ctx, repo.CountLegacyRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           projectID,
	})
	if err != nil {
		return fmt.Errorf("count legacy remote session clients for user_session_issuer: %w", err)
	}
	if count > 1 {
		return &remoteSessionClientIssuerDriftError{UserSessionIssuerID: userSessionIssuerID, Count: count}
	}

	for _, row := range rows {
		if err := backfillRemoteSessionClientUserSessionIssuer(ctx, db, row.ID, userSessionIssuerID); err != nil {
			if isRemoteSessionClientIssuerDrift(err) {
				return err
			}
			logger.WarnContext(
				ctx,
				"failed to backfill remote session client user_session_issuer binding",
				attr.SlogProjectID(projectID.String()),
				slog.String("remote_session_client_id", row.ID.String()),
				slog.String("user_session_issuer_id", userSessionIssuerID.String()),
				attr.SlogError(err),
			)
		}
	}

	return nil
}

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
		return q.ListRemoteSessionClientsByProjectID(ctx, repo.ListRemoteSessionClientsByProjectIDParams{
			ProjectID:             conv.ToNullUUID(projectID),
			RemoteSessionIssuerID: remoteSessionIssuerID,
			Cursor:                cursor,
			LimitValue:            limit,
		})
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
	if len(rows) > 0 {
		return rows, nil
	}

	legacyRows, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuerLegacy(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerLegacyParams{
		UserSessionIssuerID:   userSessionIssuerID.UUID,
		ProjectID:             projectID,
		RemoteSessionIssuerID: remoteSessionIssuerID,
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list legacy remote session clients by project for user_session_issuer: %w", err)
	}
	if len(legacyRows) == 0 {
		return legacyRows, nil
	}

	logRemoteSessionClientLegacyBindingFallback(ctx, s.logger, projectID, userSessionIssuerID.UUID, len(legacyRows))
	if err := ensureLegacyRemoteSessionClientRowsBackfilled(ctx, s.db, s.logger, projectID, userSessionIssuerID.UUID, legacyRows); err != nil {
		return nil, err
	}

	return legacyRows, nil
}

func (m *ChallengeManager) listRemoteSessionClientRowsForUserSessionIssuer(
	ctx context.Context,
	projectID uuid.UUID,
	userSessionIssuerID uuid.UUID,
) ([]repo.ListRemoteSessionClientsForUserSessionIssuerRow, error) {
	q := repo.New(m.db)
	rows, err := q.ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(projectID),
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session clients for user_session_issuer: %w", err)
	}
	if len(rows) > 0 {
		return rows, nil
	}

	legacyRows, err := q.ListRemoteSessionClientsForUserSessionIssuerLegacy(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerLegacyParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(projectID),
	})
	if err != nil {
		return nil, fmt.Errorf("list legacy remote session clients for user_session_issuer: %w", err)
	}
	if len(legacyRows) == 0 {
		return nil, nil
	}

	logRemoteSessionClientLegacyBindingFallback(ctx, m.logger, projectID, userSessionIssuerID, len(legacyRows))
	legacyClientRows := make([]repo.RemoteSessionClient, 0, len(legacyRows))
	for _, row := range legacyRows {
		legacyClientRows = append(legacyClientRows, repo.RemoteSessionClient{
			ID:                    row.ClientID,
			ProjectID:             conv.ToNullUUID(projectID),
			RemoteSessionIssuerID: row.RemoteSessionIssuerID,
			UserSessionIssuerID:   row.UserSessionIssuerID,
		})
	}
	if err := ensureLegacyRemoteSessionClientRowsBackfilled(ctx, m.db, m.logger, projectID, userSessionIssuerID, legacyClientRows); err != nil {
		if isRemoteSessionClientIssuerDrift(err) {
			return nil, err
		}
		return legacyRuntimeRowsToJoinRows(legacyRows), nil
	}

	return legacyRuntimeRowsToJoinRows(legacyRows), nil
}

func legacyRuntimeRowsToJoinRows(rows []repo.ListRemoteSessionClientsForUserSessionIssuerLegacyRow) []repo.ListRemoteSessionClientsForUserSessionIssuerRow {
	out := make([]repo.ListRemoteSessionClientsForUserSessionIssuerRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, repo.ListRemoteSessionClientsForUserSessionIssuerRow{
			ClientID:                row.ClientID,
			ExternalClientID:        row.ExternalClientID,
			ClientSecretEncrypted:   row.ClientSecretEncrypted,
			TokenEndpointAuthMethod: row.TokenEndpointAuthMethod,
			ClientScope:             row.ClientScope,
			ClientAudience:          row.ClientAudience,
			RemoteSessionIssuerID:   row.RemoteSessionIssuerID,
			UserSessionIssuerID:     row.UserSessionIssuerID,
			IssuerSlug:              row.IssuerSlug,
			IssuerUrl:               row.IssuerUrl,
			AuthorizationEndpoint:   row.AuthorizationEndpoint,
			TokenEndpoint:           row.TokenEndpoint,
			ScopesSupported:         row.ScopesSupported,
			Passthrough:             row.Passthrough,
			Oidc:                    row.Oidc,
		})
	}
	return out
}
