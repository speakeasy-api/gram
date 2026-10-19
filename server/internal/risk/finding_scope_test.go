package risk

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
)

// The scopes see a tool request the way batchMessageView shows it to the
// legacy engines: the message's own text as content, the arguments only as
// tool_calls.
func TestScopeMessageViewMirrorsBatchMessageView(t *testing.T) {
	t.Parallel()

	view := scopeMessageView(llmanalyzer.ScopeTarget{
		ProjectID:         uuid.NewString(),
		RiskPolicyID:      uuid.NewString(),
		RiskPolicyVersion: 1,
		Content:           "running the deploy",
		MessageType:       message.ToolRequest,
		ToolCalls: []llmanalyzer.ToolCall{
			{ID: "call_1", Name: "Bash", Arguments: `{"command":"rm -rf /tmp/build"}`},
			{ID: "call_2", Name: "", Arguments: "  "},
		},
	})
	require.Equal(t, "running the deploy", view.Content)
	require.Equal(t, message.ToolRequest, view.Type)
	require.Len(t, view.Tools, 1, "empty calls are skipped as in batchMessageView")

	user := scopeMessageView(llmanalyzer.ScopeTarget{
		ProjectID:         uuid.NewString(),
		RiskPolicyID:      uuid.NewString(),
		RiskPolicyVersion: 1,
		Content:           "hello",
		MessageType:       message.User,
		ToolCalls:         []llmanalyzer.ToolCall{{ID: "call_1", Name: "Bash", Arguments: `{"command":"ls"}`}},
	})
	require.Empty(t, user.Tools, "only tool requests expose tool views")
}

// A scope over content does not match text that only appears in a tool
// call's arguments.
func TestFindingScopeResolverContentScopeIgnoresToolArguments(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)
	resolver, err := NewFindingScopeResolver(nil, eng)
	require.NoError(t, err)
	specified, err := risk_analysis.CompileDetectionScopes(eng, []risk_analysis.DetectionScopeConfig{
		{Category: string(categories.CategorySecrets), ScopeInclude: `content.matchRegex("deploy")`, ScopeExempt: ""},
	})
	require.NoError(t, err)

	projectID, policyID := uuid.New(), uuid.New()
	resolver.cache.Add(findingScopeKey{projectID: projectID, policyID: policyID, version: 4}, risk_analysis.NewCategoryScope(resolver.recommended, specified))
	secret := scanners.Finding{RuleID: llmanalyzer.RuleSecret, Source: "gitleaks", Description: "An access key id appears in the arguments.", Match: "", StartPos: 0, EndPos: 0, Tags: nil, Confidence: 1, DeadLetterReason: "", McpLookupToolCallID: "", SpanGroupKey: "", Field: "", Path: ""}
	target := llmanalyzer.ScopeTarget{
		ProjectID:         projectID.String(),
		RiskPolicyID:      policyID.String(),
		RiskPolicyVersion: 4,
		Content:           "run it",
		MessageType:       message.ToolRequest,
		ToolCalls:         []llmanalyzer.ToolCall{{ID: "call_1", Name: "Bash", Arguments: `{"command":"deploy --key AKIA0000000000000000"}`}},
	}

	kept, err := resolver.FilterFindings(t.Context(), target, []scanners.Finding{secret})
	require.NoError(t, err)
	require.Empty(t, kept, "deploy only appears in the tool arguments, which content does not cover")

	target.Content = "please deploy this"
	kept, err = resolver.FilterFindings(t.Context(), target, []scanners.Finding{secret})
	require.NoError(t, err)
	require.Len(t, kept, 1, "the message text matches the content scope")
}
