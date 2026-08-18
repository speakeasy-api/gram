package activities

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// A settle run that loses the insert race has to carry on: the allocation it
// wanted is already stored and immutable. Only a unique violation means that,
// so every other failure still has to surface.
func TestAllocationAlreadyClaimed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "idempotency key index, the non-arbiter one ON CONFLICT cannot swallow",
			err: &pgconn.PgError{
				Code:           pgerrcode.UniqueViolation,
				ConstraintName: "stripe_invoice_allocations_idempotency_key_key",
			},
			want: true,
		},
		{
			name: "arbiter index, when the conflict surfaces as an error rather than DO NOTHING",
			err: &pgconn.PgError{
				Code:           pgerrcode.UniqueViolation,
				ConstraintName: "stripe_invoice_allocations_org_source_seq_key",
			},
			want: true,
		},
		{
			name: "wrapped by the caller's context",
			err:  fmt.Errorf("freeze baseline for 2026-07-01: %w", &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: "stripe_invoice_allocations_idempotency_key_key"}),
			want: true,
		},
		{
			name: "a unique violation from the primary key is a real fault",
			err: &pgconn.PgError{
				Code:           pgerrcode.UniqueViolation,
				ConstraintName: "stripe_invoice_allocations_pkey",
			},
			want: false,
		},
		{
			name: "a unique violation from an index this activity has never reasoned about is a real fault",
			err: &pgconn.PgError{
				Code:           pgerrcode.UniqueViolation,
				ConstraintName: "stripe_invoice_allocations_some_future_key",
			},
			want: false,
		},
		{
			name: "a foreign key violation is a real fault",
			err:  &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation},
			want: false,
		},
		{
			name: "a check constraint violation is a real fault",
			err:  &pgconn.PgError{Code: pgerrcode.CheckViolation},
			want: false,
		},
		{
			name: "a non-database failure is a real fault",
			err:  errors.New("connection reset"),
			want: false,
		},
		{
			name: "no error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, allocationAlreadyClaimed(tt.err))
		})
	}
}
