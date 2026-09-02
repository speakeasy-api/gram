package hostedinference

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResourceAdapterCoversOnlyGovernedInventory(t *testing.T) {
	t.Parallel()
	adapter := ResourceAdapter{}
	for category, class := range categoryClasses {
		result, err := adapter.Canonicalize("org", string(category))
		require.NoError(t, err)
		key, supported, err := result.Key()
		require.NoError(t, err)
		if class == CallClassGovernedUser {
			require.True(t, supported, category)
			require.Equal(t, string(category), string(key))
		} else {
			require.False(t, supported, category)
		}
	}

	result, err := adapter.Canonicalize("org", "future_unregistered_category")
	require.NoError(t, err)
	_, supported, err := result.Key()
	require.NoError(t, err)
	require.False(t, supported)
}
