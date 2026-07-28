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

// isGlobalRemoteSessionIssuerSlugConflict reports whether err is the unique
// violation on the global-slug partial index — raised when a global issuer is
// created or renamed to a slug another global issuer already uses.
func isGlobalRemoteSessionIssuerSlugConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "remote_session_issuers_global_slug_key"
}
