package nobaresqlstate

import (
	"github.com/jackc/pgx/v5/pgconn"
)

// unknownCode is reported but has no autofix: this SQLSTATE has no known
// pgerrcode constant, so the analyzer can't name a replacement.
func unknownCode(pgErr *pgconn.PgError) bool {
	return pgErr.Code == "99999" // want `compare pgconn\.PgError\.Code against a github\.com/jackc/pgerrcode constant instead of a bare SQLSTATE string literal`
}
