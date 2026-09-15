package activities

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestAllocationAlreadyClaimed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want bool
	}{
		{err: &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: stripeAllocationIdempotencyKeyIndex}, want: true},
		{err: fmt.Errorf("freeze baseline for 2026-07-01: %w", &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: stripeAllocationIdempotencyKeyIndex}), want: true},
		// The arbiter resolves without raising, so a violation naming it is a
		// state this insert cannot reach and must not be swallowed.
		{err: &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: "stripe_invoice_allocations_org_source_seq_key"}, want: false},
		{err: &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: "stripe_invoice_allocations_pkey"}, want: false},
		{err: &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation}, want: false},
		{err: errors.New("connection reset"), want: false},
		{err: nil, want: false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, allocationAlreadyClaimed(tc.err), "err=%v", tc.err)
	}
}

// The predicate keys off an index name, so a migration that renames the index
// turns a swallowed race back into a hard failure. Catch that here rather than
// in a settle run.
func TestAllocationIndexNameMatchesSchema(t *testing.T) {
	t.Parallel()

	schema, err := os.ReadFile("../../../database/schema.sql")
	require.NoError(t, err)
	require.Contains(t, string(schema), stripeAllocationIdempotencyKeyIndex,
		"index renamed in schema.sql without updating stripeAllocationIdempotencyKeyIndex")
	require.Contains(t, string(schema), "CREATE UNIQUE INDEX IF NOT EXISTS "+stripeAllocationIdempotencyKeyIndex,
		"index is no longer unique, so it can no longer signal a lost race")
}
