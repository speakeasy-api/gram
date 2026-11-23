package feature

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamSelectorWritesWithoutProviderOrOrganization(t *testing.T) {
	t.Parallel()

	allowed, err := UpstreamSelectorWrites(nil)(t.Context(), "org")
	require.NoError(t, err)
	require.False(t, allowed)
}
