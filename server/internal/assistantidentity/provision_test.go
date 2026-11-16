package assistantidentity

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestAgentNameCandidate(t *testing.T) {
	t.Parallel()
	longest := strings.Repeat("é", maxAgentNameLength)
	for _, tc := range []struct {
		name          string
		assistantName string
		attempt       int
		want          string
	}{
		{name: "first attempt keeps the assistant name", assistantName: "Support desk", attempt: 1, want: "Support desk"},
		{name: "later attempts add a suffix", assistantName: "Support desk", attempt: 2, want: "Support desk (2)"},
		{name: "a full-length name is shortened by characters", assistantName: longest, attempt: 12, want: strings.Repeat("é", maxAgentNameLength-5) + " (12)"},
		{name: "shortening drops trailing spaces", assistantName: strings.Repeat("a", maxAgentNameLength-5) + "   bc", attempt: 3, want: strings.Repeat("a", maxAgentNameLength-5) + " (3)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agentNameCandidate(tc.assistantName, tc.attempt)
			require.Equal(t, tc.want, got)
			require.LessOrEqual(t, utf8.RuneCountInString(got), maxAgentNameLength)
		})
	}
}
