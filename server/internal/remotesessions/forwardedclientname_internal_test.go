package remotesessions

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestSanitizeForwardedClientName(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"plain", "Claude", "Claude"},
		{"unicode kept", "Clàude 日本", "Clàude 日本"},
		{"control characters", "Cla\x00ude\r\n", "Claude"},
		{"bidirectional override", "Cla\u202eude", "Claude"},
		{"zero width", "Cla\u200bude", "Claude"},
		{"surrounding space", "  Claude  ", "Claude"},
		{"invalid utf-8", "Cla\xffude", "Claude"},
		{"line separator", "Claude\u2028Approved", "ClaudeApproved"},
		{"variation selector", "Cla\ufe0fude", "Claude"},
		{"hangul filler", "\u3164", ""},
		{"only invisible", "\u202e\x00 ", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, sanitizeForwardedClientName(tc.in))
		})
	}
}

func TestSanitizeForwardedClientName_CapsOnCharacterBoundary(t *testing.T) {
	t.Parallel()

	got := sanitizeForwardedClientName(strings.Repeat("日", 200))
	require.LessOrEqual(t, len(got), maxForwardedClientNameBytes)
	require.True(t, utf8.ValidString(got))
	require.Greater(t, len(got), maxForwardedClientNameBytes-3)
}
