package assignments

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSubsetWildcardCoversEveryAudience(t *testing.T) {
	t.Parallel()

	require.True(t, IsSubset(nil, []string{"*"}))
	require.True(t, IsSubset([]string{"*"}, []string{"*"}))
	require.True(t, IsSubset([]string{"user:member", "role:organization:team"}, []string{"*"}))
	require.True(t, IsSubset([]string{"user:member"}, []string{"role:organization:team", "*"}))
}

func TestIsSubsetFiniteAudiencesDoNotInferMembership(t *testing.T) {
	t.Parallel()

	require.True(t, IsSubset(nil, nil))
	require.True(t, IsSubset(nil, []string{"user:member"}))
	require.True(t, IsSubset([]string{"user:member"}, []string{"user:member", "user:other"}))
	require.False(t, IsSubset([]string{"*"}, []string{"user:member"}))
	require.False(t, IsSubset([]string{"user:member"}, []string{"role:organization:team"}))
	require.False(t, IsSubset([]string{"user:member"}, nil))
}
