package risk_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// A URL-list edit that contradicts a standing decision is refused until the
// caller explicitly confirms superseding it; the confirmed save records the
// supersession attributed to the editing admin.
func TestUpdateRiskPolicy_ContradictingURLEditRequiresSupersede(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Supersede Conflict"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	conflict := shadowmcp.StandingDecisionConflict{
		RequestID: uuid.New(),
		TargetKey: "https://github.example.com/mcp",
		TargetRaw: "https://github.example.com/mcp",
		Decision:  "approved",
	}
	intake.reviewConflicts = []shadowmcp.StandingDecisionConflict{conflict}
	intake.reviewStandingURLs = []string{conflict.TargetKey}

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpAllowedUrls: []string{},
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, conflict.TargetRaw)
	require.Empty(t, intake.supersededConflicts, "an unconfirmed save must not supersede anything")

	confirm := true
	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpAllowedUrls: []string{},
		SupersedeDecisions:   &confirm,
	})
	require.NoError(t, err)
	require.Equal(t, []shadowmcp.StandingDecisionConflict{conflict}, intake.supersededConflicts)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, authCtx.UserID, intake.gotSupersedeActor.ID)
	require.Equal(t, created.ID, intake.gotReviewPolicyID.String())
}

// Standing decision URLs survive an ordinary re-save untouched: the review's
// standing set flows into the reconciler as its preserve set, so re-sending
// the list never rewrites a decision's recorded blast radius with the policy
// audience.
func TestUpdateRiskPolicy_PreservesStandingDecisionURLs(t *testing.T) {
	t.Parallel()

	standingURL := "https://github.example.com/mcp"
	intake := &fakeApprovalIntake{}
	intake.reviewStandingURLs = []string{standingURL}

	var gotPreserve []map[string]struct{}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
		inner := instance.reconcileShadowMCPPolicyURLs
		instance.reconcileShadowMCPPolicyURLs = func(ctx context.Context, db riskrepo.DBTX, input policybypass.ReconcilePolicyURLsInput) error {
			gotPreserve = append(gotPreserve, input.PreserveURLs)
			return inner(ctx, db, input)
		}
	})

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Supersede Preserve"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
	gotPreserve = nil

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpAllowedUrls: []string{standingURL},
	})
	require.NoError(t, err)

	require.Len(t, gotPreserve, 1)
	require.Equal(t, map[string]struct{}{standingURL: {}}, gotPreserve[0])
}
