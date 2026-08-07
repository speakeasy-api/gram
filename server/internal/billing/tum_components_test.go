package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTumComponents_KeysColumnsNonEmptyAndUnique(t *testing.T) {
	t.Parallel()

	components := TumComponents()
	require.NotEmpty(t, components, "the TUM definition must have at least one component")

	keys := make(map[string]bool, len(components))
	columns := make(map[string]bool, len(components))
	for _, c := range components {
		require.NotEmpty(t, c.Key, "component key must be set")
		require.NotEmpty(t, c.Column, "component column must be set")
		require.False(t, keys[c.Key], "duplicate component key %q", c.Key)
		require.False(t, columns[c.Column], "duplicate component column %q", c.Column)
		keys[c.Key] = true
		columns[c.Column] = true
	}
}

func TestTumComponents_ReturnsACopy(t *testing.T) {
	t.Parallel()

	mutated := TumComponents()
	mutated[0].Column = "tampered"
	require.NotEqual(t, "tampered", TumComponents()[0].Column,
		"callers must not be able to mutate the registry")
}
