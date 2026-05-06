package conv_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestTruncateString_Shorter(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hi", conv.TruncateString("hi", 10))
}

func TestTruncateString_ExactLength(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hello", conv.TruncateString("hello", 5))
}

func TestTruncateString_Longer(t *testing.T) {
	t.Parallel()

	require.Equal(t, "hel", conv.TruncateString("hello world", 3))
}

func TestTruncateString_Empty(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", conv.TruncateString("", 5))
	require.Equal(t, "", conv.TruncateString("", 0))
}

func TestTruncateString_ZeroMax(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", conv.TruncateString("hello", 0))
}

func TestTruncateString_MultiByteRunes(t *testing.T) {
	t.Parallel()

	// "héllo" is 5 runes but 6 bytes. Truncating to 4 runes must NOT split a rune.
	require.Equal(t, "héll", conv.TruncateString("héllo", 4))
}

func TestTruncateString_Emoji(t *testing.T) {
	t.Parallel()

	// Each emoji is one rune (though multiple bytes).
	require.Equal(t, "🔐🔒", conv.TruncateString("🔐🔒🔓", 2))
}
