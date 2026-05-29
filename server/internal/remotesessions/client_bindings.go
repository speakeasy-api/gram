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
		attr.SlogUserSessionIssuerID(userSessionIssuerID.String()),
		attr.SlogRemoteSessionClientLegacyRowCount(rowCount),
	)
}

// ensureLegacyRemoteSessionClientRowsBackfilled opportunistically writes join-table
// bindings for legacy remote_session_clients that still only reference a
// user_session_issuer through the old column. The join table currently enforces
// one client per issuer, so detect legacy drift before inserting and only warn
// on transient backfill failures after returning the legacy rows to the caller.
func ensureLegacyRemoteSessionClientRowsBackfilled(
	ctx context.Context,
	db *pgxpool.Pool,
	logger *slog.Logger,
	projectID uuid.UUID,
	userSessionIssuerID uuid.UUID,
	clientIDs []uuid.UUID,
) error {
	if len(clientIDs) == 0 {
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

	for _, clientID := range clientIDs {
		if err := backfillRemoteSessionClientUserSessionIssuer(ctx, db, clientID, userSessionIssuerID); err != nil {
			if isRemoteSessionClientIssuerDrift(err) {
				return err
			}
			logger.WarnContext(
				ctx,
				"failed to backfill remote session client user_session_issuer binding",
				attr.SlogProjectID(projectID.String()),
				attr.SlogRemoteSessionClientID(clientID.String()),
				attr.SlogUserSessionIssuerID(userSessionIssuerID.String()),
				attr.SlogError(err),
			)
		}
	}

	return nil
}

// listRemoteSessionClientsByProjectID reads project-scoped clients from the
// join table when filtering by user_session_issuer_id, falling back to the
// legacy column for rows created before AGE-2520 dual-write. The fallback keeps
// old configs visible and triggers an opportunistic backfill so future reads use
// the new relationship table.
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
	legacyClientIDs := make([]uuid.UUID, 0, len(legacyRows))
	for _, row := range legacyRows {
		legacyClientIDs = append(legacyClientIDs, row.ID)
	}
	if err := ensureLegacyRemoteSessionClientRowsBackfilled(ctx, s.db, s.logger, projectID, userSessionIssuerID.UUID, legacyClientIDs); err != nil {
		return nil, err
	}

	return legacyRows, nil
}

// listRemoteSessionClientRowsForUserSessionIssuer is the runtime counterpart to
// listRemoteSessionClientsByProjectID: it prefers join-table bindings but falls
// back to legacy-column rows created before AGE-2520 dual-write. When fallback
// rows are usable, it opportunistically backfills the join table so consent and
// token-resolution reads converge on the new relationship table.
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
	legacyClientIDs := make([]uuid.UUID, 0, len(legacyRows))
	for _, row := range legacyRows {
		legacyClientIDs = append(legacyClientIDs, row.ClientID)
	}
	if err := ensureLegacyRemoteSessionClientRowsBackfilled(ctx, m.db, m.logger, projectID, userSessionIssuerID, legacyClientIDs); err != nil {
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
		out = append(out, repo.ListRemoteSessionClientsForUserSessionIssuerRow(row))
	}
	return out
}
