package toolsets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedExternalOAuthServerSlug(t *testing.T) {
	t.Parallel()

	require.Equal(t, "my-toolset-oauth", generatedExternalOAuthServerSlug("my-toolset", ""))

	base := generatedExternalOAuthServerSlug(strings.Repeat("a", 40), "")
	require.Len(t, base, externalOAuthServerSlugMaxLength)
	require.True(t, strings.HasSuffix(base, "-oauth"))

	collision := generatedExternalOAuthServerSlug(strings.Repeat("a", 40), "abc12")
	require.Len(t, collision, externalOAuthServerSlugMaxLength)
	require.True(t, strings.HasSuffix(collision, "-oauth-abc12"))
}
