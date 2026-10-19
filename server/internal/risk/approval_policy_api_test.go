package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func approvalScope() *types.RiskMCPScope {
	return &types.RiskMCPScope{AllServers: true, Servers: []*types.RiskMCPServerScope{}}
}

func TestCreateRequireApprovalPolicyRoundTripsApprovers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	approver := "user:" + authCtx.UserID

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Action:         "require_approval",
		McpScope:       approvalScope(),
		ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{approver, "role:admin"}},
	})
	require.NoError(t, err)
	require.Equal(t, "require_approval", created.Action)
	require.Empty(t, created.Sources)
	require.Equal(t, "Tool Approval", created.Name)
	require.NotNil(t, created.ApprovalConfig)
	require.Equal(t, []string{approver, "role:admin"}, created.ApprovalConfig.Approvers)

	got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ApprovalConfig, got.ApprovalConfig)

	// Changing only the approvers keeps the action and bumps the version.
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:             created.ID,
		Name:           created.Name,
		ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{"role:admin"}},
	})
	require.NoError(t, err)
	require.Equal(t, "require_approval", updated.Action)
	require.Equal(t, []string{"role:admin"}, updated.ApprovalConfig.Approvers)
	require.Greater(t, updated.Version, created.Version)

	// Moving to another action drops the approver list.
	blocked, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    created.Name,
		Action:  new("flag"),
		Sources: []string{"destructive_tool"},
	})
	require.NoError(t, err)
	require.Equal(t, "flag", blocked.Action)
	require.Nil(t, blocked.ApprovalConfig)
}

func TestCreateRequireApprovalPolicyRejectsInvalidShapes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	approvers := &types.RiskPolicyApprovalConfig{Approvers: []string{"role:admin"}}

	cases := map[string]*gen.CreateRiskPolicyPayload{
		"no scope":        {Action: "require_approval", ApprovalConfig: approvers},
		"no approvers":    {Action: "require_approval", McpScope: approvalScope()},
		"with sources":    {Action: "require_approval", McpScope: approvalScope(), Sources: []string{"gitleaks"}, ApprovalConfig: approvers},
		"agent approver":  {Action: "require_approval", McpScope: approvalScope(), ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{"agent:abc"}}},
		"config on block": {Action: "block", McpScope: approvalScope(), Sources: []string{"gitleaks"}, ApprovalConfig: approvers},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.CreateRiskPolicy(ctx, payload)
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeInvalid, oopsErr.Code)
		})
	}
}
