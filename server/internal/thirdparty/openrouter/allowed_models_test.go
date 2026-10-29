package openrouter

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllowedModels(t *testing.T) {
	t.Parallel()
	want := make([]string, 0, len(allowList))
	for id, allowed := range allowList {
		if allowed {
			want = append(want, id)
		}
	}
	slices.Sort(want)
	got := AllowedModels()
	require.NotEmpty(t, got)
	require.Equal(t, want, got)
	for _, id := range got {
		require.True(t, IsModelAllowed(id), id)
	}
	got[0] = "not-a-supported-model"
	require.Equal(t, want, AllowedModels(), "callers must not mutate the catalog")
	require.False(t, IsModelAllowed(got[0]))
}
