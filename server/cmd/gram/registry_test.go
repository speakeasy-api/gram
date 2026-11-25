package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryHasNoProductionImportCommand(t *testing.T) {
	t.Parallel()
	for _, command := range newApp().Commands {
		require.NotEqual(t, "registry-import", command.Name)
	}
}
