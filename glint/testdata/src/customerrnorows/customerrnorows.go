package customerrnorows

import "errors"

var ErrNoRows = errors.New("custom no rows error unrelated to database/sql")
