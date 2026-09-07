package publiclimits

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestEffective(t *testing.T) {
	t.Parallel()

	unset := pgtype.Int4{Int32: 0, Valid: false}

	rate, burst := Effective(unset, unset)
	require.Equal(t, DefaultRequestRatePerSecond, rate)
	require.Equal(t, DefaultRequestBurst, burst)

	rate, burst = Effective(unset, pgtype.Int4{Int32: 450, Valid: true})
	require.Equal(t, DefaultRequestRatePerSecond, rate, "a burst without a rate is ignored")
	require.Equal(t, DefaultRequestBurst, burst)

	rate, burst = Effective(pgtype.Int4{Int32: 300, Valid: true}, unset)
	require.Equal(t, 300, rate)
	require.Equal(t, 600, burst, "a stored rate without a burst gets twice the rate")

	rate, burst = Effective(pgtype.Int4{Int32: 300, Valid: true}, pgtype.Int4{Int32: 450, Valid: true})
	require.Equal(t, 300, rate)
	require.Equal(t, 450, burst)

	// A stored 0 is the clear sentinel on the write path and the CHECK
	// constraint keeps it off the row, but the resolver must still read a
	// non-positive value as "no limit" rather than a zero-rate bucket.
	for _, v := range []int32{0, -1} {
		rate, burst = Effective(pgtype.Int4{Int32: v, Valid: true}, pgtype.Int4{Int32: 450, Valid: true})
		require.Equal(t, DefaultRequestRatePerSecond, rate)
		require.Equal(t, DefaultRequestBurst, burst)
		require.False(t, Stored(pgtype.Int4{Int32: v, Valid: true}))
	}
	require.False(t, Stored(unset))
	require.True(t, Stored(pgtype.Int4{Int32: 1, Valid: true}))
}
