package plugins

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/install"
	"github.com/stretchr/testify/require"
)

// TestOpenCodeShimBodyMatchesAgentHooks guards against silent drift between
// gram's opencodeObservabilityShim and agenthooks' canonical OpenCode shim.
//
// The two files intentionally differ in their invocation layer: agenthooks
// bakes a static COMMAND (a direct binary path), while gram wraps it in a
// per-OS hooks/bootstrap.{sh,ps1} pinned-binary launcher. But the NDJSON
// serve protocol and the hook-forwarding body below the COMMAND are a copy of
// agenthooks' render_opencode.go. If agenthooks changes the protocol or the
// forwarding logic, gram's hand-maintained copy must be re-synced — this test
// fails when it isn't.
func TestOpenCodeShimBodyMatchesAgentHooks(t *testing.T) {
	t.Parallel()

	fsys, err := install.Render(
		install.Manifest{Command: []string{"speakeasy-hooks"}},
		install.Target{Provider: agenthooks.ProviderOpenCode, Scope: install.ScopeProject, Dir: "."},
	)
	require.NoError(t, err)
	b, err := fs.ReadFile(fsys, ".opencode/plugin/agenthooks.ts")
	require.NoError(t, err)

	// The shared region is everything from the seq counter (right after the
	// spawn call, where the two invocation preambles rejoin) up to the
	// differing `export default <name>` trailer.
	body := func(shim string) string {
		start := strings.Index(shim, "let seq = 0")
		end := strings.Index(shim, "export default")
		require.GreaterOrEqual(t, start, 0, "shim missing `let seq = 0` anchor")
		require.Greater(t, end, start, "shim missing `export default` anchor")
		return shim[start:end]
	}

	require.Equal(t, body(string(b)), body(opencodeObservabilityShim),
		"opencodeObservabilityShim has drifted from agenthooks install.Render for OpenCode; "+
			"re-sync the forwarding body from agenthooks/install/render_opencode.go")
}
