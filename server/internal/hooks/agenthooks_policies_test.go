package hooks

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"
)

// TestPolicyRunner_WalkPinsRunOrder snapshots the ingest policy pipeline in
// dispatch order. The order is load-bearing: middleware resolves the actor
// before any gate runs, and within a kind the first conclusive decision wins
// — the spend gate outranks every risk scan, the risk scan outranks the
// shadow-MCP gate on tool and permission requests, and the
// permission-flavored scan outranks the duplicate MCP-or-tool scan, exactly
// as the old inline evaluateCanonicalHook ordered them. A diff here means
// the decision layer's precedence changed; update newPolicyRunner and this
// snapshot together, deliberately.
func TestPolicyRunner_WalkPinsRunOrder(t *testing.T) {
	t.Parallel()

	_, ti := newTestHooksService(t)

	var got []string
	require.NoError(t, ti.service.policies.Walk(func(stage agenthooks.StageInfo) error {
		// Every stage is a (*Service) method value; reduce the reflected
		// name (pkg path + "(*Service).x-fm") to the method name.
		name := strings.TrimSuffix(stage.Name[strings.LastIndex(stage.Name, ".")+1:], "-fm")
		parts := []string{string(stage.Type)}
		if stage.Kind != "" {
			parts = append(parts, string(stage.Kind))
		}
		parts = append(parts, name)
		got = append(got, strings.Join(parts, " "))
		return nil
	}))

	require.Equal(t, []string{
		"middleware actorResolution",
		"handler prompt.submitted spendGatePromptGate",
		"handler prompt.submitted riskScanPromptGate",
		"handler tool.pre spendGateToolPreGate",
		"handler tool.pre riskScanToolPreGate",
		"handler tool.pre shadowMCPToolPreGate",
		"handler permission.request spendGatePermissionGate",
		"handler permission.request riskScanPermissionGate",
		"handler permission.request riskScanPermissionToolGate",
		"handler permission.request shadowMCPPermissionGate",
	}, got)
}
