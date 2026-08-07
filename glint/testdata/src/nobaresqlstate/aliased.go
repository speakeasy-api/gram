package nobaresqlstate

import (
	pgcode "github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

// aliasedImport ensures the fix uses the import's alias, not the default name.
func aliasedImport(pgErr *pgconn.PgError) bool {
	_ = pgcode.UniqueViolation
	return pgErr.Code == "40001" // want `compare pgconn\.PgError\.Code against a github\.com/jackc/pgerrcode constant instead of a bare SQLSTATE string literal`
}
