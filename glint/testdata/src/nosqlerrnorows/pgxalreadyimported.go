package nosqlerrnorows

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
)

// pgxAlreadyImported ensures the SuggestedFix reuses an existing pgx import
// rather than adding a duplicate.
func pgxAlreadyImported(err error) bool {
	_ = pgx.ErrNoRows
	return errors.Is(err, sql.ErrNoRows) // want `use github.com/jackc/pgx/v5\.ErrNoRows instead of database/sql\.ErrNoRows`
}
