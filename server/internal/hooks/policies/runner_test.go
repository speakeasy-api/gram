package policies

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// stubDeps satisfies Deps with inert primitives: Walk never dispatches, so
// the bodies are irrelevant — only the registration shape matters.
type stubDeps struct{}

func (stubDeps) CheckSpend(context.Context, *Request, Actor, string, time.Time) (string, bool) {
	return "", false
}

func (stubDeps) ScanPrompt(context.Context, *Request, Actor, string, time.Time) *risk.ScanResult {
	return nil
}

func (stubDeps) ScanMCPToolRequest(context.Context, *Request, Actor, time.Time) *risk.ScanResult {
	return nil
}

func (stubDeps) ScanToolRequest(context.Context, *Request, Actor, time.Time) *risk.ScanResult {
	return nil
}

func (stubDeps) ScanPermissionRequest(context.Context, *Request, Actor, time.Time) *risk.ScanResult {
	return nil
}

func (stubDeps) AppendBlockPageURL(_ context.Context, _ *Request, _ Actor, _, _, _, userReason string) string {
	return userReason
}

func (stubDeps) EvaluateShadowMCP(context.Context, *Request, Actor, string, any) (string, string) {
	return "", ""
}

func (stubDeps) WarnAcknowledged(context.Context, *Request, Actor, *risk.ScanResult, string, time.Time) bool {
	return false
}

func (stubDeps) WarnDenyReason(context.Context, *Request, Actor, *risk.ScanResult, string, time.Time) (string, string, bool) {
	return "", "", false
}

// closureSuffix strips the compiler-assigned closure suffix from a
// constructor-built handler's reflected name, so the snapshot pins the
// exported constructor. The remaining name is reduced to its last named
// segment because inlining prepends the enclosing function (NewRunner.
// RiskScanPromptGate.func1) and whether that happens is a compiler decision,
// not part of the pipeline's shape.
var closureSuffix = regexp.MustCompile(`(\.func\d+)+$`)

// TestNewRunner_WalkPinsRunOrder snapshots the ingest policy pipeline in
// dispatch order. The order is load-bearing: middleware resolves the actor
// before any gate runs, and within a kind the first conclusive decision wins
// — the spend gate outranks every risk scan, the risk scan outranks the
// shadow-MCP gate on tool and permission requests, and the
// permission-flavored scan outranks the duplicate MCP-or-tool scan, exactly
// as the old inline evaluateCanonicalHook ordered them. A diff here means
// the decision layer's precedence changed; update NewRunner and this
// snapshot together, deliberately.
func TestNewRunner_WalkPinsRunOrder(t *testing.T) {
	t.Parallel()

	runner := NewRunner(testenv.NewLogger(t), stubDeps{})

	var got []string
	require.NoError(t, runner.Walk(func(stage agenthooks.StageInfo) error {
		// Every stage is built in this package; reduce the reflected name
		// (pkg path + enclosing funcs + closure suffix) to the constructor:
		// the last named segment once the closure suffix is stripped.
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
