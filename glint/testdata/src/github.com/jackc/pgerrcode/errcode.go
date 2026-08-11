package pgerrcode

// Minimal stand-in for github.com/jackc/pgerrcode, carrying the named SQLSTATE
// constants the nobaresqlstate fixtures reference.
const (
	CardinalityViolation = "21000"
	NotNullViolation     = "23502"
	ForeignKeyViolation  = "23503"
	UniqueViolation      = "23505"
	CheckViolation       = "23514"
	SerializationFailure = "40001"
	DeadlockDetected     = "40P01"
)
