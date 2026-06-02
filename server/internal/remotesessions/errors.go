package remotesessions

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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
