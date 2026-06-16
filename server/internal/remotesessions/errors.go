package remotesessions

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// isRemoteSessionIssuerSlugConflict reports whether err is the unique violation
// on (project_id, slug) — raised when an issuer is moved into a project that
// already has an issuer with the same slug.
func isRemoteSessionIssuerSlugConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "remote_session_issuers_project_slug_key"
}
