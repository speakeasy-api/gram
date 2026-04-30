package nosqlerrnorows

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"customerrnorows"
)

func goodPgx(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func goodCustomPackage(err error) bool {
	return errors.Is(err, customerrnorows.ErrNoRows)
}
