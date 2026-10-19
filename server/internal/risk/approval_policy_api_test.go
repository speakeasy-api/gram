package risk_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func approvalScope() *types.RiskMCPScope {
	return &types.RiskMCPScope{AllServers: true, Servers: []*types.RiskMCPServerScope{}}
}

// approverRole creates an organization role and returns its principal URN.
func approverRole(t *testing.T, ctx context.Context, ti *testInstance, slug string) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	now := time.Now().UTC()
	role, err := accessrepo.New(ti.conn).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        slug,
		WorkosName:        slug,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return role.RoleUrn
}

func TestCreateRequireApprovalPolicyRoundTripsApprovers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.flags.SetFlag(feature.FlagToolApprovals, authCtx.ActiveOrganizationID, true)
	approver := "user:" + authCtx.UserID
	security := approverRole(t, ctx, ti, "security")

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Action:         "require_approval",
		McpScope:       approvalScope(),
		ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{approver, security}},
	})
	require.NoError(t, err)
	require.Equal(t, "require_approval", created.Action)
	require.Empty(t, created.Sources)
	require.Equal(t, "Tool Approval", created.Name)
	require.NotNil(t, created.ApprovalConfig)
	require.Equal(t, []string{approver, security}, created.ApprovalConfig.Approvers)

	got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ApprovalConfig, got.ApprovalConfig)

	// Changing only the approvers keeps the action and bumps the version.
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:             created.ID,
		Name:           created.Name,
		ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{security}},
	})
	require.NoError(t, err)
	require.Equal(t, "require_approval", updated.Action)
	require.Equal(t, []string{security}, updated.ApprovalConfig.Approvers)
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.flags.SetFlag(feature.FlagToolApprovals, authCtx.ActiveOrganizationID, true)
	approvers := &types.RiskPolicyApprovalConfig{Approvers: []string{"user:" + authCtx.UserID}}

	cases := map[string]*gen.CreateRiskPolicyPayload{
		"no scope":        {Action: "require_approval", ApprovalConfig: approvers},
		"no approvers":    {Action: "require_approval", McpScope: approvalScope()},
		"with sources":    {Action: "require_approval", McpScope: approvalScope(), Sources: []string{"gitleaks"}, ApprovalConfig: approvers},
		"agent approver":  {Action: "require_approval", McpScope: approvalScope(), ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{"agent:abc"}}},
		"unknown role":    {Action: "require_approval", McpScope: approvalScope(), ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{"role:organization:" + uuid.NewString()}}},
		"outside user":    {Action: "require_approval", McpScope: approvalScope(), ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{"user:user_not_in_org"}}},
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

func TestCreateRequireApprovalPolicyNeedsFlag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Action:         "require_approval",
		McpScope:       approvalScope(),
		ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: []string{"user:" + authCtx.UserID}},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
