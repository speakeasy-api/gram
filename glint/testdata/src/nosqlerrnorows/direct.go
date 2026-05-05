package nosqlerrnorows

import (
	"database/sql"
	"errors"
)

func directComparison(err error) bool {
	return err == sql.ErrNoRows // want `use github.com/jackc/pgx/v5\.ErrNoRows instead of database/sql\.ErrNoRows`
}

func errorsIs(err error) bool {
	return errors.Is(err, sql.ErrNoRows) // want `use github.com/jackc/pgx/v5\.ErrNoRows instead of database/sql\.ErrNoRows`
}
