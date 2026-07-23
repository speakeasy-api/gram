package nobaresqlstate

import (
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

// alreadyUsingConstant must not be flagged: it already uses pgerrcode.
func alreadyUsingConstant(pgErr *pgconn.PgError) bool {
	return pgErr.Code == pgerrcode.UniqueViolation
}

// nonLiteralOperand must not be flagged: the other operand is not a string
// literal.
func nonLiteralOperand(pgErr *pgconn.PgError, code string) bool {
	return pgErr.Code == code
}

type response struct {
	Code string
}

// unrelatedCodeField must not be flagged: Code here is not pgconn.PgError.Code.
func unrelatedCodeField(r response) bool {
	return r.Code == "23505"
}

// bareLiteralNotCompared must not be flagged: no comparison to PgError.Code.
func bareLiteralNotCompared() string {
	return "23505"
}
