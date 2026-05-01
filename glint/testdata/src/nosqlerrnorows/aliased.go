package nosqlerrnorows

import (
	sqlx "database/sql"
	"errors"
)

func aliasedErrorsIs(err error) bool {
	return errors.Is(err, sqlx.ErrNoRows) // want `use github.com/jackc/pgx/v5\.ErrNoRows instead of database/sql\.ErrNoRows`
}
