package pgconn

// PgError is a minimal stand-in for github.com/jackc/pgx/v5/pgconn.PgError,
// carrying only the Code field the nobaresqlstate analyzer keys on.
type PgError struct {
	Severity string
	Code     string
	Message  string
}

func (e *PgError) Error() string { return e.Message }
