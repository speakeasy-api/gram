package insights_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/insights"
	variationsgen "github.com/speakeasy-api/gram/server/gen/variations"
)

func TestInsightsService_ApplyProposal_MutatesUnderlyingVariation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestInsightsService(t)

	// 1) Propose a tool variation. Because no prior variation exists for this
	//    tool, the agent-supplied current_value is "null", which matches the
	//    live state — so the staleness check will pass on apply.
	proposedValue := `{
		"src_tool_urn": "tools:http:test:apply-target",
		"src_tool_name": "apply-target",
		"description": "clearer description courtesy of the agent",
		"name": "apply-target-v2"
	}`
	reasoning := "the description was confusing users"

	proposed, err := ti.service.ProposeToolVariation(ctx, &gen.ProposeToolVariationPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ToolName:         "apply-target",
		ProposedValue:    proposedValue,
		CurrentValue:     nil,
		Reasoning:        &reasoning,
		SourceChatID:     nil,
	})
	require.NoError(t, err, "propose should not error")
	require.NotNil(t, proposed)
	require.Equal(t, "pending", proposed.Proposal.Status)

	// 2) Pre-condition: no variation exists for this tool yet.
	before, err := ti.variationsSvc.ListGlobal(ctx, &variationsgen.ListGlobalPayload{})
	require.NoError(t, err)
	for _, v := range before.Variations {
		require.NotEqual(t, "apply-target", v.SrcToolName, "variation must not exist before apply")
	}

	// 3) Apply the proposal. This must mutate the underlying variation.
	applied, err := ti.service.ApplyProposal(ctx, &gen.ApplyProposalPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProposalID:       proposed.Proposal.ID,
		Force:            nil,
	})
	require.NoError(t, err, "apply should not error")
	require.NotNil(t, applied)
	require.Equal(t, "applied", applied.Proposal.Status, "proposal status should be applied")
	require.NotNil(t, applied.Proposal.AppliedAt, "applied_at should be set")
	require.NotNil(t, applied.Proposal.AppliedValue, "applied_value should be set")
	require.NotNil(t, applied.Proposal.AppliedByUserID, "applied_by_user_id should be set")

	// 4) The underlying variation must now exist with the proposed overrides.
	after, err := ti.variationsSvc.ListGlobal(ctx, &variationsgen.ListGlobalPayload{})
	require.NoError(t, err)

	var found *string
	var foundDescription *string
	for _, v := range after.Variations {
		if v.SrcToolName == "apply-target" {
			found = &v.SrcToolName
			foundDescription = v.Description
			break
		}
	}
	require.NotNil(t, found, "variation should exist after apply")
	require.NotNil(t, foundDescription, "description override should be present")
	require.Equal(t, "clearer description courtesy of the agent", *foundDescription, "description should match proposed value")
}
