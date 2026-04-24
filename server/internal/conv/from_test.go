package conv_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

func TestFromPGInt4_Valid(t *testing.T) {
	t.Parallel()

	input := pgtype.Int4{Int32: 42, Valid: true}
	result := conv.FromPGInt4(input)

	require.NotNil(t, result)
	require.Equal(t, int32(42), *result)
}

func TestFromPGInt4_Invalid(t *testing.T) {
	t.Parallel()

	input := pgtype.Int4{Int32: 0, Valid: false}
	result := conv.FromPGInt4(input)

	require.Nil(t, result)
}

func TestPtrInt32ToInt_NonNil(t *testing.T) {
	t.Parallel()

	v := int32(99)
	result := conv.PtrInt32ToInt(&v)

	require.NotNil(t, result)
	require.Equal(t, 99, *result)
}

func TestPtrInt32ToInt_Nil(t *testing.T) {
	t.Parallel()

	result := conv.PtrInt32ToInt(nil)

	require.Nil(t, result)
}
