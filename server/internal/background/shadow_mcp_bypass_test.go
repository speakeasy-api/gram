package background

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

type capturePolicyBypassBatchEvaluator struct {
	evaluations []risk.PolicyBypassEvaluation
}

func (e *capturePolicyBypassBatchEvaluator) CanBypassBatch(_ context.Context, inputs []risk.PolicyBypassEvaluation) map[risk.PolicyBypassEvaluation]bool {
	e.evaluations = append(e.evaluations, inputs...)
	results := make(map[risk.PolicyBypassEvaluation]bool, len(inputs))
	for _, input := range inputs {
		results[input] = true
	}
	return results
}

func TestShadowMCPPolicyBypassChecker_UsesCanonicalURLAndWholePolicyFallback(t *testing.T) {
	t.Parallel()

	serverURL := "https://mcp.example.test/sse"
	serverIdentity := "mcp.example.test"
	resolved := shadowmcpscan.BypassRequest{
		UserID: "user-1",
		Evidence: shadowmcp.AccessEvidence{
			FullURL:        serverURL,
			URLHost:        "",
			ServerIdentity: serverIdentity,
		},
		ToolName: "MCP:authenticate",
		Resolved: true,
	}
	unresolvedOne := shadowmcpscan.BypassRequest{
		UserID:   "user-1",
		Evidence: shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: ""},
		ToolName: "mcp__db__read",
		Resolved: false,
	}
	unresolvedTwo := shadowmcpscan.BypassRequest{
		UserID:   "user-1",
		Evidence: shadowmcp.AccessEvidence{FullURL: "", URLHost: "", ServerIdentity: ""},
		ToolName: "mcp__db__write",
		Resolved: false,
	}
	evaluator := &capturePolicyBypassBatchEvaluator{evaluations: nil}
	checker := &shadowMCPPolicyBypassChecker{evaluator: evaluator}

	decisions := checker.CanBypassShadowMCP(
		t.Context(),
		"org-1",
		uuid.New(),
		[]shadowmcpscan.BypassRequest{resolved, unresolvedOne, unresolvedTwo},
	)

	require.True(t, decisions[resolved])
	require.True(t, decisions[unresolvedOne])
	require.True(t, decisions[unresolvedTwo])
	require.Len(t, evaluator.evaluations, 3)
	require.NotNil(t, evaluator.evaluations[0].Target)
	require.Equal(t, serverURL, evaluator.evaluations[0].Target.Dimensions[authz.SelectorKeyServerURL])
	require.NotContains(t, evaluator.evaluations[0].Target.Dimensions, authz.SelectorKeyServerIdentity)
	require.Nil(t, evaluator.evaluations[1].Target)
	require.Nil(t, evaluator.evaluations[2].Target)
}
