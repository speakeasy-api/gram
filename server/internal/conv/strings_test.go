package conv_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestTruncateDetail_UnderLimitIsUnchanged(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"", "short", strings.Repeat("a", 10)} {
		require.Equal(t, s, conv.TruncateDetail(s, 10))
	}
}

func TestTruncateDetail_OverLimitAppendsNotice(t *testing.T) {
	t.Parallel()

	got := conv.TruncateDetail(strings.Repeat("a", 20), 10)
	require.True(t, strings.HasPrefix(got, strings.Repeat("a", 10)))
	require.Greater(t, len(got), 10, "the notice is appended past the bound on purpose")
	require.Contains(t, got, "truncated")
}

// The cut is by rune. Slicing at a byte offset would land mid-sequence and emit
// invalid UTF-8 into an API response.
func TestTruncateDetail_CutsOnRuneBoundaries(t *testing.T) {
	t.Parallel()

	// Each of these is multi-byte, so a byte-wise cut at 3 would split one.
	got := conv.TruncateDetail("日本語テキスト", 3)
	require.True(t, utf8.ValidString(got), "must not emit invalid utf-8")
	require.True(t, strings.HasPrefix(got, "日本語"))
	require.Contains(t, got, "truncated")
}

func TestTruncateDetail_NegativeBoundDoesNotPanic(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		got := conv.TruncateDetail("anything", -1)
		require.Contains(t, got, "truncated")
	})
}
