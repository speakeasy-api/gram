package platformmcp

import (
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

func TestFindingLabelUnicodeFormatting(t *testing.T) {
	t.Parallel()
	for _, character := range []rune{'\u202e', '\u2066', '\u200d', '\u2028', '\u2029'} {
		t.Run(string(character), func(t *testing.T) {
			t.Parallel()
			label := findingLabel("safe" + string(character) + "label")
			require.True(t, strings.HasPrefix(label, "safelabel…#"))
			require.Equal(t, label, findingLabel("safe"+string(character)+"label"))
			require.NotEqual(t, findingLabel("safelabel"), label)
			for _, r := range label {
				require.False(t, unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r))
			}
		})
	}
	require.Equal(t, "ordinary label", findingLabel("ordinary label"))
}
