package nosqlerrnorows

import (
	"database/sql"
	"errors"
)

// keepSqlImport ensures the SuggestedFix does not delete the database/sql
// import when the file uses sql for symbols other than ErrNoRows.
var _ *sql.DB

func mixedUsage(err error) bool {
	return errors.Is(err, sql.ErrNoRows) // want `use github.com/jackc/pgx/v5\.ErrNoRows instead of database/sql\.ErrNoRows`
}
