package mcpconnectionattempts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminFixesAreAdminReasons(t *testing.T) {
	t.Parallel()
	for reason := range adminFixes {
		meaning, ok := MeaningOf(reason)
		require.True(t, ok, reason)
		require.Equal(t, WhoAdmin, meaning.WhoFixes, reason)
	}
}
