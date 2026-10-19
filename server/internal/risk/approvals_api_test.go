package risk_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// pendingApproval holds a call under the policy and files its request, as an
// agent would, and returns the request token.
func pendingApproval(t *testing.T, ctx context.Context, ti *testInstance, policyID string, arguments string) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := riskrepo.New(ti.conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: uuid.MustParse(policyID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	service := approval.NewService(testenv.NewLogger(t), ti.conn, testenv.DefaultSiteURL(t), audit.NewLogger())
	agentURN := "agent:" + uuid.NewString()
	outcome, err := service.HoldCall(ctx, approval.HoldInput{
		Policies:        []policycore.Policy{policycore.Project(row, nil)},
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		RequesterURN:    agentURN,
		RequesterUserID: "",
		Surface:         "hosted_mcp",
		ToolsetID:       "",
		ServerID:        uuid.NewString(),
		ToolName:        "post_message",
		ChatID:          "",
		Arguments:       []byte(arguments),
	})
	require.NoError(t, err)
	_, status, err := service.Request(ctx, approval.RequestInput{Token: outcome.Token, RequesterURN: agentURN, OrganizationID: authCtx.ActiveOrganizationID, Reason: "Tell the team the deploy finished.", Scope: "", ExpiresIn: 0})
	require.NoError(t, err)
	require.Equal(t, approval.StatusPending, status)
	return outcome.Token
}

func createApprovalPolicy(t *testing.T, ctx context.Context, ti *testInstance, name string, approvers ...string) *types.RiskPolicy {
	t.Helper()
	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:           &name,
		Action:         "require_approval",
		McpScope:       approvalScope(),
		ApprovalConfig: &types.RiskPolicyApprovalConfig{Approvers: approvers},
	})
	require.NoError(t, err)
	return policy
}

func TestApprovalRequestsListGetAndDecide(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.flags.SetFlag(feature.FlagToolApprovals, authCtx.ActiveOrganizationID, true)
	policy := createApprovalPolicy(t, ctx, ti, "Approve Slack posts", "user:"+authCtx.UserID)
	pendingApproval(t, ctx, ti, policy.ID, `{"channel":"C1","text":"Deploy finished"}`)

	listed, err := ti.service.ListRiskPolicyApprovals(ctx, &gen.ListRiskPolicyApprovalsPayload{Status: new("pending"), Limit: 50})
	require.NoError(t, err)
	require.Len(t, listed.Approvals, 1)
	require.Nil(t, listed.NextCursor)
	got := listed.Approvals[0]
	require.Equal(t, "pending", got.Status)
	require.Equal(t, "Approve Slack posts", got.PolicyName)
	require.Equal(t, "post_message", got.ToolName)
	require.Equal(t, "Tell the team the deploy finished.", *got.Reason)
	require.Equal(t, "exact_once", *got.RequestedScope)
	preview, err := json.Marshal(got.ArgumentsPreview)
	require.NoError(t, err)
	require.JSONEq(t, `{"channel":"C1","text":"Deploy finished"}`, string(preview))
	require.True(t, got.CanDecide)

	fetched, err := ti.service.GetRiskPolicyApproval(ctx, &gen.GetRiskPolicyApprovalPayload{ID: got.ID})
	require.NoError(t, err)
	require.Equal(t, got, fetched)

	decided, err := ti.service.DecideRiskPolicyApproval(ctx, &gen.DecideRiskPolicyApprovalPayload{ID: got.ID, Decision: "approve", Note: new("Go ahead.")})
	require.NoError(t, err)
	require.Equal(t, "approved", decided.Status)
	require.Equal(t, "dashboard", *decided.DecidedVia)
	require.Equal(t, "user:"+authCtx.UserID, *decided.DecidedByUrn)
	require.Equal(t, "Go ahead.", *decided.DecisionNote)
	require.Equal(t, "exact_once", *decided.GrantedScope)
	require.False(t, decided.CanDecide)

	_, err = ti.service.DecideRiskPolicyApproval(ctx, &gen.DecideRiskPolicyApprovalPayload{ID: got.ID, Decision: "deny", Note: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	pending, err := ti.service.ListRiskPolicyApprovals(ctx, &gen.ListRiskPolicyApprovalsPayload{Status: new("pending"), Limit: 50})
	require.NoError(t, err)
	require.Empty(t, pending.Approvals)
}

func TestApprovalRequestsPaginate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.flags.SetFlag(feature.FlagToolApprovals, authCtx.ActiveOrganizationID, true)
	policy := createApprovalPolicy(t, ctx, ti, "Approve posts", approverRole(t, ctx, ti, "approvers"))
	for i := range 3 {
		pendingApproval(t, ctx, ti, policy.ID, `{"n":`+strconv.Itoa(i)+`}`)
	}

	first, err := ti.service.ListRiskPolicyApprovals(ctx, &gen.ListRiskPolicyApprovalsPayload{Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Approvals, 2)
	require.NotNil(t, first.NextCursor)
	second, err := ti.service.ListRiskPolicyApprovals(ctx, &gen.ListRiskPolicyApprovalsPayload{Cursor: first.NextCursor, Limit: 2})
	require.NoError(t, err)
	require.Len(t, second.Approvals, 1)
	require.Nil(t, second.NextCursor)
}

func TestApprovalRequestsVisibleOnlyToApproversAndAdmins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.flags.SetFlag(feature.FlagToolApprovals, authCtx.ActiveOrganizationID, true)
	mine := createApprovalPolicy(t, ctx, ti, "Mine", "user:"+authCtx.UserID)
	theirs := createApprovalPolicy(t, ctx, ti, "Theirs", approverRole(t, ctx, ti, "security-team"))
	pendingApproval(t, ctx, ti, mine.ID, `{"a":1}`)
	pendingApproval(t, ctx, ti, theirs.ID, `{"b":2}`)

	// An org admin sees both, but can decide only where listed.
	all, err := ti.service.ListRiskPolicyApprovals(ctx, &gen.ListRiskPolicyApprovalsPayload{Limit: 50})
	require.NoError(t, err)
	require.Len(t, all.Approvals, 2)
	var theirsID string
	for _, item := range all.Approvals {
		require.Equal(t, item.PolicyID == mine.ID, item.CanDecide)
		if item.PolicyID == theirs.ID {
			theirsID = item.ID
		}
	}
	_, err = ti.service.DecideRiskPolicyApproval(ctx, &gen.DecideRiskPolicyApprovalPayload{ID: theirsID, Decision: "approve", Note: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	// A project member who is not an admin sees only the policy that lists them.
	member := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String())})
	visible, err := ti.service.ListRiskPolicyApprovals(member, &gen.ListRiskPolicyApprovalsPayload{Limit: 50})
	require.NoError(t, err)
	require.Len(t, visible.Approvals, 1)
	require.Equal(t, mine.ID, visible.Approvals[0].PolicyID)
	_, err = ti.service.GetRiskPolicyApproval(member, &gen.GetRiskPolicyApprovalPayload{ID: theirsID})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDecideApprovalNeedsSignedInPerson(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.flags.SetFlag(feature.FlagToolApprovals, authCtx.ActiveOrganizationID, true)
	policy := createApprovalPolicy(t, ctx, ti, "Approve posts", "user:"+authCtx.UserID)
	pendingApproval(t, ctx, ti, policy.ID, `{}`)
	listed, err := ti.service.ListRiskPolicyApprovals(ctx, &gen.ListRiskPolicyApprovalsPayload{Limit: 50})
	require.NoError(t, err)
	require.Len(t, listed.Approvals, 1)

	keyAuth := *authCtx
	keyAuth.SessionID = nil
	keyAuth.APIKeyID = uuid.NewString()
	_, err = ti.service.DecideRiskPolicyApproval(contextvalues.SetAuthContext(ctx, &keyAuth), &gen.DecideRiskPolicyApprovalPayload{ID: listed.Approvals[0].ID, Decision: "approve", Note: nil})
	require.Error(t, err)

	still, err := ti.service.GetRiskPolicyApproval(ctx, &gen.GetRiskPolicyApprovalPayload{ID: listed.Approvals[0].ID})
	require.NoError(t, err)
	require.Equal(t, "pending", still.Status)
}
