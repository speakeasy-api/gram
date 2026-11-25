package otel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHexEventIDTreatsEmptyAndZeroAsAbsent(t *testing.T) {
	t.Parallel()

	require.Empty(t, hexEventID(nil))
	require.Empty(t, hexEventID([]byte{}))
	require.Empty(t, hexEventID(make([]byte, 16)))
	require.Equal(t, "0102030405060708", hexEventID([]byte{1, 2, 3, 4, 5, 6, 7, 8}))
}
