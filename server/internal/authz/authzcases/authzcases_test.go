package authzcases

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCasesAreCurrent fails when the dashboard's cases no longer match what
// the evaluator answers. `mise run gen:authz` rewrites them.
func TestCasesAreCurrent(t *testing.T) {
	t.Parallel()

	want, err := File()
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join("..", "..", "..", Path))
	require.NoError(t, err, Path)
	require.Equal(t, string(want), string(got), "%s is stale; run `mise run gen:authz`", Path)
}
