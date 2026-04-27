package insights_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/insights"
)

func TestInsightsService_ProposeToolVariation_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestInsightsService(t)

	reasoning := "the description is confusing users"
	currentValue := `{"description":"old"}`
	proposedValue := `{"description":"new clearer description"}`

	result, err := ti.service.ProposeToolVariation(ctx, &gen.ProposeToolVariationPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ToolName:         "create_invoice",
		ProposedValue:    proposedValue,
		CurrentValue:     &currentValue,
		Reasoning:        &reasoning,
		SourceChatID:     nil,
	})
	require.NoError(t, err, "propose tool variation should not error")
	require.NotNil(t, result, "result should not be nil")
	require.NotNil(t, result.Proposal, "proposal should not be nil")

	require.NotEmpty(t, result.Proposal.ID, "proposal id should not be empty")
	require.Equal(t, "tool_variation", result.Proposal.Kind, "kind should be tool_variation")
	require.Equal(t, "create_invoice", result.Proposal.TargetRef, "target ref should be tool name")
	require.Equal(t, "pending", result.Proposal.Status, "status should be pending")
	// JSONB normalization may change whitespace; compare semantically.
	// current_value is set by the backend via liveReadResource (caller's
	// supplied value is intentionally ignored — see ProposeToolVariation
	// for why). With no existing variation in the test fixture, the live
	// snapshot is "null".
	requireJSONEqual(t, "null", result.Proposal.CurrentValue, "current value should be live snapshot (null when no variation exists)")
	// proposed_value is canonicalized server-side: src_tool_name is forced
	// from tool_name and src_tool_urn is filled in from the project's
	// http_tool_definitions when missing. With no fixture tool present in
	// the test project, src_tool_urn is left blank and src_tool_name gets
	// filled in. The original `description` field is preserved.
	expectedProposed := `{"description":"new clearer description","src_tool_name":"create_invoice"}`
	requireJSONEqual(t, expectedProposed, result.Proposal.ProposedValue, "proposed value should be canonicalized")
	require.NotNil(t, result.Proposal.Reasoning, "reasoning should not be nil")
	require.Equal(t, reasoning, *result.Proposal.Reasoning, "reasoning should match")
	require.NotEmpty(t, result.Proposal.CreatedAt, "created at should not be empty")
}

func requireJSONEqual(t *testing.T, expected, actual, msg string) {
	t.Helper()
	var e, a any
	require.NoError(t, json.Unmarshal([]byte(expected), &e), msg)
	require.NoError(t, json.Unmarshal([]byte(actual), &a), msg)
	require.Equal(t, e, a, msg)
}
