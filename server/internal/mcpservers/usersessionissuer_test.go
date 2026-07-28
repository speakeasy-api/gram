package mcpservers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildUserSessionResourceSlug(t *testing.T) {
	t.Parallel()

	t.Run("appends suffix", func(t *testing.T) {
		t.Parallel()

		slug, err := buildUserSessionResourceSlug("test-mcp-server-abcd")
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(slug, "test-mcp-server-abcd-"))
		require.Len(t, strings.TrimPrefix(slug, "test-mcp-server-abcd-"), userSessionResourceSlugSuffixLen)
	})

	t.Run("caps length", func(t *testing.T) {
		t.Parallel()

		slug, err := buildUserSessionResourceSlug(strings.Repeat("a", 100))
		require.NoError(t, err)
		require.Len(t, slug, userSessionResourceSlugMaxLen)
		maxBaseLen := userSessionResourceSlugMaxLen - userSessionResourceSlugSuffixLen - 1
		require.True(t, strings.HasPrefix(slug, strings.Repeat("a", maxBaseLen)+"-"))
	})

	t.Run("falls back for empty normalized base", func(t *testing.T) {
		t.Parallel()

		slug, err := buildUserSessionResourceSlug("!@#$%")
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(slug, "mcp-"))
		require.Len(t, strings.TrimPrefix(slug, "mcp-"), userSessionResourceSlugSuffixLen)
	})
}
