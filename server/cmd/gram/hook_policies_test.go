package gram

import (
	"regexp"
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// closureSuffix strips the compiler-assigned closure suffix from a
// constructor-built handler's reflected name, so the snapshot pins the
// exported constructor. The remaining name is reduced to its last named
// segment because inlining prepends the enclosing function (NewRunner.
// RiskScanPromptGate.func1) and whether that happens is a compiler decision,
// not part of the pipeline's shape.
var closureSuffix = regexp.MustCompile(`(\.func\d+)+$`)

// TestNewRunner_WalkPinsRunOrder snapshots the ingest policy pipeline in
// dispatch order, built by the real cmd registration (newHookPolicyRunner)
// from a zero Enforcer — Walk never dispatches, so only the registration
// shape matters. The order is load-bearing: middleware resolves the actor
// before any gate runs, and within a kind the first conclusive decision wins
// — the spend gate outranks every risk scan, the risk scan outranks the
// shadow-MCP gate on tool and permission requests, and the
// permission-flavored scan outranks the duplicate MCP-or-tool scan, exactly
// as the old inline evaluateCanonicalHook ordered them. A diff here means
// the decision layer's precedence changed; update newHookPolicyRunner and
// this snapshot together, deliberately.
func TestNewRunner_WalkPinsRunOrder(t *testing.T) {
	t.Parallel()

	runner := newHookPolicyRunner(testenv.NewLogger(t), &hooks.Enforcer{})

	var got []string
	require.NoError(t, runner.Walk(func(stage agenthooks.StageInfo) error {
		// Every stage is built from the policies package; reduce the
		// reflected name (pkg path + enclosing funcs + closure suffix) to
		// the constructor: the last named segment once the closure suffix is
		// stripped.
		name := strings.TrimSuffix(stage.Name, "-fm")
		name = closureSuffix.ReplaceAllString(name, "")
		name = name[strings.LastIndex(name, ".")+1:]
		parts := []string{string(stage.Type)}
		if stage.Kind != "" {
			parts = append(parts, string(stage.Kind))
		}
		parts = append(parts, name)
		got = append(got, strings.Join(parts, " "))
		return nil
	}))

	require.Equal(t, []string{
		"middleware ActorResolution",
		"handler prompt.submitted SpendGatePrompt",
		"handler prompt.submitted RiskScanPromptGate",
		"handler tool.pre SpendGateToolPre",
		"handler tool.pre RiskScanToolPreGate",
		"handler tool.pre ShadowMCPToolPreGate",
		"handler permission.request SpendGatePermission",
		"handler permission.request RiskScanPermissionGate",
		"handler permission.request RiskScanPermissionToolGate",
		"handler permission.request ShadowMCPPermissionGate",
	}, got)
}
