package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripNUL_Untouched(t *testing.T) {
	t.Parallel()

	require.Equal(t, "plain text", StripNUL("plain text"))
	require.Empty(t, StripNUL(""))
}

func TestStripNUL_RemovesEveryNUL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ab", StripNUL("a\x00b\x00"))
	require.Empty(t, StripNUL("\x00\x00"))
}
