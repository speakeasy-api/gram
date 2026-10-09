package mcp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// tunnelHeaderPolicyFiles are the only files allowed to select the tunneled
// header policy: the two places that build a proxy to a tunnel gateway.
var tunnelHeaderPolicyFiles = []string{"tunnel_manager.go", "tunnelpublic.go"}

// A proxy built without a policy gets the hardened remote one, so the way to
// leave a remote path unprotected is to select the tunneled policy for it.
// Fail if any other file in this package starts doing so.
func TestOnlyTunnelBuildersSelectTunneledHeaderPolicy(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	var selecting []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		if strings.Contains(string(source), "HeaderPolicyTunneled") {
			selecting = append(selecting, file)
		}
	}
	slices.Sort(selecting)
	require.Equal(t, tunnelHeaderPolicyFiles, selecting)
}
