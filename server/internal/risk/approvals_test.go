package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestApproveShadowMCP_HappyPath(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	approval, err := ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID:   policy.ID,
		Match:      "https://mcp.example.com/server/",
		ServerName: new("Example"),
	})
	require.NoError(t, err)
	require.NotNil(t, approval)
	assert.Equal(t, policy.ID, approval.PolicyID)
	assert.Equal(t, "https://mcp.example.com/server", approval.Match, "match should be canonicalized on write")
	require.NotNil(t, approval.ServerName)
	assert.Equal(t, "Example", *approval.ServerName)
}

func TestApproveShadowMCP_CommandHappyPath(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	approval, err := ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID:   policy.ID,
		Match:      "  mise   mcp  ",
		ServerName: new("mise"),
	})
	require.NoError(t, err)
	require.NotNil(t, approval)
	assert.Equal(t, "mise mcp", approval.Match, "command-shaped match should be whitespace-canonicalized")
}

// Server-prefix-keyed approvals (the form the batch scanner emits) round-trip
// identically — single field, single canonicalization.
func TestApproveShadowMCP_ServerPrefix(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	approval, err := ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID: policy.ID,
		Match:    "mise",
	})
	require.NoError(t, err)
	assert.Equal(t, "mise", approval.Match)
}

func TestApproveShadowMCP_InvalidPolicyID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID: "not-a-uuid",
		Match:    "https://mcp.example.com/server",
	})
	require.Error(t, err)
}

func TestApproveShadowMCP_UnknownPolicy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID: uuid.New().String(),
		Match:    "https://mcp.example.com/server",
	})
	require.Error(t, err, "policy id that doesn't belong to this project must be rejected")
}

func TestApproveShadowMCP_EmptyMatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	_, err = ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID: policy.ID,
		Match:    "   ",
	})
	require.Error(t, err, "whitespace-only match must be rejected")
}

func TestApproveShadowMCP_DeniesWithoutOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Create the policy with admin scope first.
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	adminCtx := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})
	policy, err := ti.service.CreateRiskPolicy(adminCtx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	// Now attempt approval without admin scope.
	_, err = ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID: policy.ID,
		Match:    "https://mcp.example.com/server",
	})
	require.Error(t, err, "missing org:admin scope must deny")
}

func TestListShadowMCPApprovals_Empty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	result, err := ti.service.ListShadowMCPApprovals(ctx, &gen.ListShadowMCPApprovalsPayload{
		PolicyID: policy.ID,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Approvals)
}

func TestListShadowMCPApprovals_AfterApprove(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	for _, match := range []string{
		"https://mcp.example.com/a",
		"https://mcp.example.com/b",
	} {
		_, err := ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
			PolicyID: policy.ID,
			Match:    match,
		})
		require.NoError(t, err)
	}

	result, err := ti.service.ListShadowMCPApprovals(ctx, &gen.ListShadowMCPApprovalsPayload{
		PolicyID: policy.ID,
	})
	require.NoError(t, err)
	require.Len(t, result.Approvals, 2)
	matches := []string{result.Approvals[0].Match, result.Approvals[1].Match}
	assert.ElementsMatch(t, []string{"https://mcp.example.com/a", "https://mcp.example.com/b"}, matches)
}

func TestRevokeShadowMCPApproval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	_, err = ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID: policy.ID,
		Match:    "https://mcp.example.com/server",
	})
	require.NoError(t, err)

	err = ti.service.RevokeShadowMCPApproval(ctx, &gen.RevokeShadowMCPApprovalPayload{
		PolicyID: policy.ID,
		Match:    "https://mcp.example.com/server",
	})
	require.NoError(t, err)

	result, err := ti.service.ListShadowMCPApprovals(ctx, &gen.ListShadowMCPApprovalsPayload{
		PolicyID: policy.ID,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Approvals)
}

func TestRevokeShadowMCPApproval_CommandPath(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	_, err = ti.service.ApproveShadowMCP(ctx, &gen.ApproveShadowMCPPayload{
		PolicyID: policy.ID,
		Match:    "mise mcp",
	})
	require.NoError(t, err)

	err = ti.service.RevokeShadowMCPApproval(ctx, &gen.RevokeShadowMCPApprovalPayload{
		PolicyID: policy.ID,
		Match:    "mise mcp",
	})
	require.NoError(t, err)

	result, err := ti.service.ListShadowMCPApprovals(ctx, &gen.ListShadowMCPApprovalsPayload{
		PolicyID: policy.ID,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Approvals, "command-keyed revoke must remove the approval")
}

func TestRevokeShadowMCPApproval_MissingIsNoop(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Block"),
		Sources: []string{"shadow_mcp"},
	})
	require.NoError(t, err)

	err = ti.service.RevokeShadowMCPApproval(ctx, &gen.RevokeShadowMCPApprovalPayload{
		PolicyID: policy.ID,
		Match:    "https://mcp.example.com/never-approved",
	})
	require.NoError(t, err, "revoking a match that was never approved must be a no-op")
}
