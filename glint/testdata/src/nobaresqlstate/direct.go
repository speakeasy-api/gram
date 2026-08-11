package nobaresqlstate

import (
	"github.com/jackc/pgx/v5/pgconn"
)

func pointerReceiver(pgErr *pgconn.PgError) bool {
	return pgErr.Code == "23505" // want `compare pgconn\.PgError\.Code against a github\.com/jackc/pgerrcode constant instead of a bare SQLSTATE string literal`
}

func valueReceiver(pgErr pgconn.PgError) bool {
	return pgErr.Code != "21000" // want `compare pgconn\.PgError\.Code against a github\.com/jackc/pgerrcode constant instead of a bare SQLSTATE string literal`
}

func literalOnLeft(pgErr *pgconn.PgError) bool {
	return "23503" == pgErr.Code // want `compare pgconn\.PgError\.Code against a github\.com/jackc/pgerrcode constant instead of a bare SQLSTATE string literal`
}
