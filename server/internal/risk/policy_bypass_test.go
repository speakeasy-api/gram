package risk_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"goa.design/goa/v3/security"
)

func TestCreateApproveAndRevokePolicyBypassRequest_AddsAndRemovesServerURLGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass URL Token"),
	})
	require.NoError(t, err)

	fullURL := "https://mcp.example.com/server"
	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL)

	beforeCreateAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestCreate)
	require.NoError(t, err)
	beforeApproveAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestApprove)
	require.NoError(t, err)
	beforeRevokeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestRevoke)
	require.NoError(t, err)

	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	require.NotNil(t, request)
	assert.Equal(t, "requested", request.Status)
	row := redeemedBypassRow(t, ctx, ti, request)
	assert.Equal(t, policy.ID, row.PolicyID)
	require.NotNil(t, row.TargetKind)
	assert.Equal(t, "shadow_mcp_server", *row.TargetKind)
	require.NotNil(t, row.TargetKey)
	assert.Equal(t, fullURL, *row.TargetKey)
	assert.Equal(t, fullURL, row.TargetDimensions[authz.SelectorKeyServerURL])
	assert.Equal(t, authCtx.UserID, row.RequesterUserID)

	afterCreateAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCreateAuditCount+1, afterCreateAuditCount)
	createRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestCreate)
	require.NoError(t, err)
	createMetadata, err := audittest.DecodeAuditData(createRecord.Metadata)
	require.NoError(t, err)
	assert.Equal(t, request.ID, createMetadata["request_id"])
	assert.Equal(t, "requested", createMetadata["current_status"])
	assert.Empty(t, createRecord.BeforeSnapshot)
	require.NotEmpty(t, createRecord.AfterSnapshot)

	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	require.Len(t, approved.GrantedPrincipalUrns, 1)
	assert.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String(), approved.GrantedPrincipalUrns[0])
	assert.True(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))

	afterApproveAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestApprove)
	require.NoError(t, err)
	require.Equal(t, beforeApproveAuditCount+1, afterApproveAuditCount)
	approveRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestApprove)
	require.NoError(t, err)
	approveMetadata, err := audittest.DecodeAuditData(approveRecord.Metadata)
	require.NoError(t, err)
	assert.Equal(t, "requested", approveMetadata["previous_status"])
	assert.Equal(t, "approved", approveMetadata["current_status"])
	require.NotEmpty(t, approveRecord.BeforeSnapshot)
	require.NotEmpty(t, approveRecord.AfterSnapshot)

	revoked, err := ti.service.RevokeRiskPolicyBypassRequest(ctx, &gen.RevokeRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "revoked", revoked.Status)
	assert.Empty(t, revoked.GrantedPrincipalUrns)
	assert.False(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))

	afterRevokeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestRevoke)
	require.NoError(t, err)
	require.Equal(t, beforeRevokeAuditCount+1, afterRevokeAuditCount)
}

func TestCreateApprovePolicyBypassRequest_AddsServerIdentityGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Identity Token"),
	})
	require.NoError(t, err)

	serverIdentity := "mise mcp"
	token := riskPolicyBypassRequestTokenForServerIdentity(t, ti, authCtx, policy.ID, serverIdentity)
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	require.NotNil(t, request)
	assert.Equal(t, "requested", request.Status)
	row := redeemedBypassRow(t, ctx, ti, request)
	assert.Equal(t, policy.ID, row.PolicyID)
	require.NotNil(t, row.TargetKind)
	assert.Equal(t, "shadow_mcp_server", *row.TargetKind)
	require.NotNil(t, row.TargetKey)
	assert.Equal(t, serverIdentity, *row.TargetKey)
	assert.Equal(t, serverIdentity, row.TargetDimensions[authz.SelectorKeyServerIdentity])
	assert.Equal(t, authCtx.UserID, row.RequesterUserID)

	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	assert.True(t, userHasRiskPolicyBypassServerIdentityGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, serverIdentity))
}

func TestApprovePolicyBypassRequest_CanGrantAllUsers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass All Users"),
	})
	require.NoError(t, err)

	otherUserID := "user_policy_bypass_all"
	seedRiskPolicyBypassOrganizationUser(t, authCtx.ActiveOrganizationID, otherUserID, ti)

	fullURL := "https://mcp.example.com/all-users"
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL),
	})
	require.NoError(t, err)

	allUsersPrincipal := authz.AllUsersPrincipal()
	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID:                   request.ID,
		GrantedPrincipalUrns: []string{allUsersPrincipal.String()},
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	assert.Equal(t, []string{allUsersPrincipal.String()}, approved.GrantedPrincipalUrns)
	assert.True(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))
	assert.True(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, otherUserID, policy.ID, fullURL))

	revoked, err := ti.service.RevokeRiskPolicyBypassRequest(ctx, &gen.RevokeRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "revoked", revoked.Status)
	assert.False(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))
	assert.False(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, otherUserID, policy.ID, fullURL))
}

func TestPolicyBypassEvaluator_AudienceSemantics(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	allUsersPolicyID := "policy_all_user_bypass"
	allUsersURL := "https://mcp.example.com/all-users-runtime"
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, allUsersPolicyID)
	selector[authz.SelectorKeyServerURL] = allUsersURL
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     allUsersPolicyID,
		},
		Principals: []urn.Principal{authz.AllUsersPrincipal()},
		Selector:   selector,
	}))

	allUsersTarget := risk.ShadowMCPServerPolicyBypassTarget(allUsersURL, "", allUsersURL)
	evaluator := risk.NewPolicyBypassEvaluator(testenv.NewLogger(t), ti.conn)

	assert.True(t, evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		PolicyID:       allUsersPolicyID,
		Target:         &allUsersTarget,
	}))
	assert.True(t, evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "",
		PolicyID:       allUsersPolicyID,
		Target:         &allUsersTarget,
	}))
	assert.True(t, evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "not_connected_user",
		PolicyID:       allUsersPolicyID,
		Target:         &allUsersTarget,
	}))
	assert.False(t, evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         urn.AllUsersPrincipalID,
		PolicyID:       allUsersPolicyID,
		Target:         &allUsersTarget,
	}))

	rolePolicyID := "policy_role_bypass"
	roleURL := "https://mcp.example.com/role-runtime"
	roleSelector := authz.NewSelector(authz.ScopeRiskPolicyBypass, rolePolicyID)
	roleSelector[authz.SelectorKeyServerURL] = roleURL
	roleSlug := "risk-policy-bypass-runtime-role"
	rolePrincipal := seedRiskPolicyBypassOrganizationRole(t, ti, authCtx.ActiveOrganizationID, roleSlug)
	seedRiskPolicyBypassRoleAssignment(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, roleSlug)
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     rolePolicyID,
		},
		Principals: []urn.Principal{rolePrincipal},
		Selector:   roleSelector,
	}))

	roleTarget := risk.ShadowMCPServerPolicyBypassTarget(roleURL, "", roleURL)
	assert.True(t, evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		PolicyID:       rolePolicyID,
		Target:         &roleTarget,
	}))
	assert.False(t, evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "",
		PolicyID:       rolePolicyID,
		Target:         &roleTarget,
	}))
	assert.False(t, evaluator.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "not_connected_user",
		PolicyID:       rolePolicyID,
		Target:         &roleTarget,
	}))
}

func TestPolicyBypassEvaluator_LegacyCombinedGrantMatchesCanonicalURLTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	policyID := "policy_legacy_combined_target"
	serverURL := "HTTPS://MCP.EXAMPLE.COM:443/legacy?stale=1"
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	selector[authz.SelectorKeyServerIdentity] = "legacy-alias"
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Principals: []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:   selector,
	}))

	target := risk.ShadowMCPPolicyBypassTarget(shadowmcp.AccessEvidence{
		FullURL:        "https://user:pass@mcp.example.com/legacy?current=1",
		URLHost:        "",
		ServerIdentity: "",
	}, "list_events")
	require.NotNil(t, target)
	require.NotContains(t, target.Dimensions, authz.SelectorKeyServerIdentity)
	require.True(t, risk.NewPolicyBypassEvaluator(testenv.NewLogger(t), ti.conn).CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		PolicyID:       policyID,
		Target:         target,
	}))
}

func TestPolicyBypassEvaluator_UnresolvedTargetMatchesOnlyWholePolicyGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	scopedPolicyID := "policy_scoped_unresolved"
	scopedSelector := authz.NewSelector(authz.ScopeRiskPolicyBypass, scopedPolicyID)
	scopedSelector[authz.SelectorKeyServerURL] = "https://mcp.example.com/scoped"
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     scopedPolicyID,
		},
		Principals: []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:   scopedSelector,
	}))

	wholePolicyID := "policy_whole_unresolved"
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     wholePolicyID,
		},
		Principals: []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)},
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyBypass, wholePolicyID),
	}))

	scopedEvaluation := risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		PolicyID:       scopedPolicyID,
		Target:         nil,
	}
	wholeEvaluation := risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		PolicyID:       wholePolicyID,
		Target:         nil,
	}
	decisions := risk.NewPolicyBypassEvaluator(testenv.NewLogger(t), ti.conn).CanBypassBatch(ctx, []risk.PolicyBypassEvaluation{
		scopedEvaluation,
		wholeEvaluation,
	})

	require.False(t, decisions[scopedEvaluation], "an unresolved call must not match a server-scoped grant")
	require.True(t, decisions[wholeEvaluation], "an unresolved call may match an explicit whole-policy grant")
}

func TestApprovePolicyBypassRequest_CanGrantRole(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Role"),
	})
	require.NoError(t, err)

	fullURL := "https://mcp.example.com/role"
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL),
	})
	require.NoError(t, err)

	rolePrincipal := seedRiskPolicyBypassOrganizationRole(t, ti, authCtx.ActiveOrganizationID, "risk-policy-bypass")
	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID:                   request.ID,
		GrantedPrincipalUrns: []string{rolePrincipal.String()},
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	assert.Equal(t, []string{rolePrincipal.String()}, approved.GrantedPrincipalUrns)
	assert.True(t, principalHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, rolePrincipal, policy.ID, fullURL))

	revoked, err := ti.service.RevokeRiskPolicyBypassRequest(ctx, &gen.RevokeRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "revoked", revoked.Status)
	assert.False(t, principalHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, rolePrincipal, policy.ID, fullURL))
}

func TestApprovePolicyBypassRequest_ApprovedRequestReplacesGrantedPrincipals(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Replace Principals"),
	})
	require.NoError(t, err)

	fullURL := "https://mcp.example.com/replace-principals"
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL),
	})
	require.NoError(t, err)

	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	assert.True(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))

	rolePrincipal := seedRiskPolicyBypassOrganizationRole(t, ti, authCtx.ActiveOrganizationID, "risk-policy-bypass-edit")
	updated, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID:                   request.ID,
		GrantedPrincipalUrns: []string{rolePrincipal.String()},
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", updated.Status)
	assert.Equal(t, []string{rolePrincipal.String()}, updated.GrantedPrincipalUrns)
	assert.False(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))
	assert.True(t, principalHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, rolePrincipal, policy.ID, fullURL))
}

func TestApprovePolicyBypassRequest_RejectsUnknownRolePrincipal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Unknown Role"),
	})
	require.NoError(t, err)

	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, "https://mcp.example.com/unknown-role"),
	})
	require.NoError(t, err)

	_, err = ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID:                   request.ID,
		GrantedPrincipalUrns: []string{"role:organization:not-a-real-role"},
	})
	require.Error(t, err)
}

func TestCreatePolicyBypassRequest_AfterDeny_ResetsExistingRequestToRequested(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Re-request"),
	})
	require.NoError(t, err)

	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, "https://mcp.example.com/denied")
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)

	beforeDenyAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)

	denied, err := ti.service.DenyRiskPolicyBypassRequest(ctx, &gen.DenyRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "denied", denied.Status)
	require.NotNil(t, denied.DecidedBy)

	afterDenyAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)
	require.Equal(t, beforeDenyAuditCount+1, afterDenyAuditCount)
	denyRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)
	denyMetadata, err := audittest.DecodeAuditData(denyRecord.Metadata)
	require.NoError(t, err)
	assert.Equal(t, "requested", denyMetadata["previous_status"])
	assert.Equal(t, "denied", denyMetadata["current_status"])

	refreshed, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	assert.Equal(t, request.ID, refreshed.ID)
	assert.Equal(t, "requested", refreshed.Status)
	refreshedRow := redeemedBypassRow(t, ctx, ti, refreshed)
	assert.Nil(t, refreshedRow.DecidedBy)
	assert.Empty(t, refreshedRow.GrantedPrincipalUrns)
}

func TestCreatePolicyBypassRequest_AfterApprove_PreservesApprovedStateAndGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Approved Re-request"),
	})
	require.NoError(t, err)

	fullURL := "https://mcp.example.com/approved"
	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL)
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)

	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	require.NotNil(t, approved.DecidedBy)
	require.NotNil(t, approved.DecidedAt)
	require.Len(t, approved.GrantedPrincipalUrns, 1)

	refreshed, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	assert.Equal(t, request.ID, refreshed.ID)
	assert.Equal(t, "approved", refreshed.Status)
	refreshedRow := redeemedBypassRow(t, ctx, ti, refreshed)
	assert.Equal(t, approved.DecidedBy, refreshedRow.DecidedBy)
	assert.Equal(t, approved.DecidedAt, refreshedRow.DecidedAt)
	assert.Equal(t, approved.GrantedPrincipalUrns, refreshedRow.GrantedPrincipalUrns)
	assert.True(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))
}

func TestDenyPolicyBypassRequest_RejectsApprovedRequest(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Deny Approved"),
	})
	require.NoError(t, err)

	fullURL := "https://mcp.example.com/deny-approved"
	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL)
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)

	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Status)

	_, err = ti.service.DenyRiskPolicyBypassRequest(ctx, &gen.DenyRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.Error(t, err)
	assert.True(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))
}

func TestCreatePolicyBypassRequest_WithoutFullURLCreatesWholePolicyTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Whole Policy"),
	})
	require.NoError(t, err)

	host := "mcp.example.com"
	token, _, err := risk.GeneratePolicyBypassRequestToken(ctx, ti.cacheAdapter, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              authCtx.ProjectID.String(),
		RequesterUserID:        authCtx.UserID,
		ObservedName:           nil,
		ObservedFullURL:        nil,
		ObservedURLHost:        &host,
		ObservedServerIdentity: nil,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            nil,
		RiskPolicyID:           policy.ID,
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.NoError(t, err)

	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	assert.Equal(t, "requested", request.Status)
	row := redeemedBypassRow(t, ctx, ti, request)
	assert.Equal(t, policy.ID, row.PolicyID)
	assert.Nil(t, row.TargetKind)
	require.NotNil(t, row.TargetKey)
	assert.Equal(t, "policy", *row.TargetKey)
	assert.Empty(t, row.TargetDimensions)
}

func TestGeneratePolicyBypassRequestToken_RequiresEvidence(t *testing.T) {
	t.Parallel()

	// Evidence is validated before the cache is touched, so a nil cache is
	// fine here — the call must fail on the missing evidence first.
	_, _, err := risk.GeneratePolicyBypassRequestToken(t.Context(), nil, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         "org_test",
		ProjectID:              "00000000-0000-0000-0000-000000000001",
		RequesterUserID:        "user_test",
		ObservedName:           nil,
		ObservedFullURL:        nil,
		ObservedURLHost:        nil,
		ObservedServerIdentity: nil,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            nil,
		RiskPolicyID:           "00000000-0000-0000-0000-000000000002",
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.ErrorContains(t, err, "policy bypass request evidence is required")
}

func riskPolicyBypassRequestToken(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, policyID string, fullURL string) string {
	t.Helper()

	token, _, err := risk.GeneratePolicyBypassRequestToken(t.Context(), ti.cacheAdapter, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              authCtx.ProjectID.String(),
		RequesterUserID:        authCtx.UserID,
		ObservedName:           nil,
		ObservedFullURL:        &fullURL,
		ObservedURLHost:        nil,
		ObservedServerIdentity: nil,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            nil,
		RiskPolicyID:           policyID,
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.NoError(t, err)
	return token
}

// riskPolicyBypassRequestTokenWithBlockReason mints a link that carries the
// policy's block reason, which is what the note falls back to.
func riskPolicyBypassRequestTokenWithBlockReason(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, policyID string, fullURL string, blockReason string) string {
	t.Helper()

	token, _, err := risk.GeneratePolicyBypassRequestToken(t.Context(), ti.cacheAdapter, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              authCtx.ProjectID.String(),
		RequesterUserID:        authCtx.UserID,
		ObservedName:           nil,
		ObservedFullURL:        &fullURL,
		ObservedURLHost:        nil,
		ObservedServerIdentity: nil,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            &blockReason,
		RiskPolicyID:           policyID,
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.NoError(t, err)
	return token
}

func riskPolicyBypassRequestTokenForServerIdentity(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, policyID string, serverIdentity string) string {
	t.Helper()

	token, _, err := risk.GeneratePolicyBypassRequestToken(t.Context(), ti.cacheAdapter, risk.PolicyBypassRequestTokenInput{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              authCtx.ProjectID.String(),
		RequesterUserID:        authCtx.UserID,
		ObservedName:           nil,
		ObservedFullURL:        nil,
		ObservedURLHost:        nil,
		ObservedServerIdentity: &serverIdentity,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            nil,
		RiskPolicyID:           policyID,
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.NoError(t, err)
	return token
}

func seedRiskPolicyBypassOrganizationUser(t *testing.T, organizationID string, userID string, ti *testInstance) {
	t.Helper()

	_, err := usersrepo.New(ti.conn).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: userID,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func seedRiskPolicyBypassOrganizationRole(t *testing.T, ti *testInstance, organizationID string, slug string) urn.Principal {
	t.Helper()

	now := time.Now().UTC()
	row, err := accessrepo.New(ti.conn).UpsertOrganizationRole(t.Context(), accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        slug,
		WorkosName:        slug,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	principal, err := urn.ParsePrincipal(row.RoleUrn)
	require.NoError(t, err)
	return principal
}

func seedRiskPolicyBypassRoleAssignment(t *testing.T, ti *testInstance, organizationID string, userID string, roleSlug string) {
	t.Helper()

	_, err := accessrepo.New(ti.conn).UpsertOrganizationRoleAssignment(t.Context(), accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       userID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("membership_" + userID + "_" + roleSlug),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     roleSlug,
	})
	require.NoError(t, err)
}

func userHasRiskPolicyBypassGrant(t *testing.T, ti *testInstance, organizationID, userID, policyID, serverURL string) bool {
	t.Helper()

	principals, err := authz.ResolveUserPrincipals(t.Context(), ti.conn, organizationID, userID)
	require.NoError(t, err)

	return principalsHaveRiskPolicyBypassGrant(t, ti, organizationID, principals, policyID, serverURL)
}

func userHasRiskPolicyBypassServerIdentityGrant(t *testing.T, ti *testInstance, organizationID, userID, policyID, serverIdentity string) bool {
	t.Helper()

	principals, err := authz.ResolveUserPrincipals(t.Context(), ti.conn, organizationID, userID)
	require.NoError(t, err)

	grants, err := authz.LoadGrants(t.Context(), ti.conn, organizationID, principals)
	require.NoError(t, err)

	for _, grant := range grants {
		if grant.Scope != authz.ScopeRiskPolicyBypass {
			continue
		}
		if grant.Selector[authz.SelectorKeyResourceID] != policyID {
			continue
		}
		if grant.Selector[authz.SelectorKeyServerIdentity] != serverIdentity {
			continue
		}
		return true
	}
	return false
}

func principalHasRiskPolicyBypassGrant(t *testing.T, ti *testInstance, organizationID string, principal urn.Principal, policyID, serverURL string) bool {
	t.Helper()

	return principalsHaveRiskPolicyBypassGrant(t, ti, organizationID, []urn.Principal{principal}, policyID, serverURL)
}

func principalsHaveRiskPolicyBypassGrant(t *testing.T, ti *testInstance, organizationID string, principals []urn.Principal, policyID, serverURL string) bool {
	t.Helper()

	grants, err := authz.LoadGrants(t.Context(), ti.conn, organizationID, principals)
	require.NoError(t, err)

	for _, grant := range grants {
		if grant.Scope != authz.ScopeRiskPolicyBypass {
			continue
		}
		if grant.Selector[authz.SelectorKeyResourceID] != policyID {
			continue
		}
		if grant.Selector[authz.SelectorKeyServerURL] != serverURL {
			continue
		}
		return true
	}
	return false
}

func TestApproveAndRevokePolicyBypassRequest_AllowAllEditsBlockedList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	blockedURL := "https://sketchy.example.com/mcp"
	otherBlockedURL := "https://bad.example.com/mcp"
	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Allow All Bypass"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{blockedURL, otherBlockedURL},
	})
	require.NoError(t, err)

	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, blockedURL)
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)

	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	// Approval is project-wide under allow_all: the URL leaves the blocked
	// list and no principal-scoped grant is minted.
	assert.Empty(t, approved.GrantedPrincipalUrns)
	assert.False(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, blockedURL))

	require.Equal(t, []string{otherBlockedURL}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, policy.ID))

	revoked, err := ti.service.RevokeRiskPolicyBypassRequest(ctx, &gen.RevokeRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "revoked", revoked.Status)

	require.Equal(t, []string{otherBlockedURL, blockedURL}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, policy.ID))
}

func TestDenyPolicyBypassRequest_AllowAllLeavesBlockedListUntouched(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	blockedURL := "https://sketchy.example.com/mcp"
	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Allow All Deny"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{blockedURL},
	})
	require.NoError(t, err)

	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, blockedURL)
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)

	denied, err := ti.service.DenyRiskPolicyBypassRequest(ctx, &gen.DenyRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "denied", denied.Status)

	require.Equal(t, []string{blockedURL}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, policy.ID))
}

// fakeApprovalIntake stands in for the mcpapproval service at the intake
// seam, recording what the redemption handed it.
type fakeApprovalIntake struct {
	err       error
	requestID string
	status    string

	gotOrganizationID string
	gotProjectID      uuid.UUID
	gotServerURL      string
	gotRequesterID    string
	gotNote           string

	// URL-edit review fixtures and recordings: what the fake reports as the
	// project's standing decisions, and what the update path handed back.
	reviewConflicts     []shadowmcp.StandingDecisionConflict
	reviewStandingURLs  []string
	gotReviewPolicyID   uuid.UUID
	gotReviewAllowed    []string
	gotReviewBlocked    []string
	supersededConflicts []shadowmcp.StandingDecisionConflict
	gotSupersedeActor   urn.Principal
}

// The fake never backfills: these tests exercise the intake seam's admission
// half, and a no-op mirrors an org whose project has no recorded decisions.
func (f *fakeApprovalIntake) ReconcileStandingDecisionsForPolicy(_ context.Context, _ pgx.Tx, _ string, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

// The zero-value fake reports no standing decisions, mirroring an org whose
// project has never recorded one — URL-list edits proceed without conflicts.
func (f *fakeApprovalIntake) ReviewShadowMCPPolicyURLEdit(_ context.Context, _ pgx.Tx, _ string, _ uuid.UUID, policyID uuid.UUID, _ string, desiredAllowedURLs []string, desiredBlockedURLs []string) (shadowmcp.StandingDecisionReview, error) {
	f.gotReviewPolicyID = policyID
	f.gotReviewAllowed = desiredAllowedURLs
	f.gotReviewBlocked = desiredBlockedURLs
	return shadowmcp.StandingDecisionReview{Conflicts: f.reviewConflicts, StandingURLs: f.reviewStandingURLs}, nil
}

func (f *fakeApprovalIntake) SupersedeShadowMCPDecisions(_ context.Context, _ pgx.Tx, _ string, _ uuid.UUID, conflicts []shadowmcp.StandingDecisionConflict, actor urn.Principal, _ *string) error {
	f.supersededConflicts = conflicts
	f.gotSupersedeActor = actor
	return nil
}

func (f *fakeApprovalIntake) AdmitBlockedServer(_ context.Context, organizationID string, projectID uuid.UUID, serverURL, requesterUserID, _ string, note string) (string, string, error) {
	f.gotOrganizationID = organizationID
	f.gotProjectID = projectID
	f.gotServerURL = serverURL
	f.gotRequesterID = requesterUserID
	f.gotNote = note
	if f.err != nil {
		return "", "", f.err
	}
	return f.requestID, f.status, nil
}

// A URL-identified shadow-MCP block link redeems into the approval workflow:
// the redemption carries the ask to the intake and no bypass row is written.
func TestCreatePolicyBypassRequest_RedeemsIntoApprovalWorkflow(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err: nil, requestID: "0195c1f1-0000-7000-8000-00000000abcd", status: "requested",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Approval Intake"),
	})
	require.NoError(t, err)
	fullURL := "https://mcp.example.com/approval-intake"

	redemption, redeemErr := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL),
	})
	require.NoError(t, redeemErr)
	require.Equal(t, "approval_request", redemption.Kind)
	require.Equal(t, intake.requestID, redemption.ID)
	require.Equal(t, "requested", redemption.Status)

	// The ask reached the intake with the caller's identity and the server.
	require.Equal(t, authCtx.ActiveOrganizationID, intake.gotOrganizationID)
	require.Equal(t, fullURL, intake.gotServerURL)
	require.Equal(t, authCtx.UserID, intake.gotRequesterID)

	// No bypass row was written.
	list, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{
		ApikeyToken: nil, SessionToken: nil, ProjectSlugInput: nil, PolicyID: &policy.ID, Status: nil,
	})
	require.NoError(t, err)
	require.Empty(t, list.Requests)
}

// The requester's own words are what a reviewer needs, so they travel with
// the redemption rather than being replaced by the policy's block reason —
// which is the same sentence for everyone the policy stopped.
func TestCreatePolicyBypassRequest_CarriesTheRequestersNote(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err: nil, requestID: "0195c1f1-0000-7000-8000-00000000cafe", status: "requested",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Requester Note"),
	})
	require.NoError(t, err)

	note := "  The docs team works here and I need meeting notes searchable.  "
	_, err = redeemWithNote(ctx, ti, riskPolicyBypassRequestTokenWithBlockReason(
		t, ti, authCtx, policy.ID, "https://mcp.example.com/noted", "Blocked by policy: unreviewed MCP server",
	), note)
	require.NoError(t, err)

	require.Equal(t, "The docs team works here and I need meeting notes searchable.", intake.gotNote)
}

// A client that sends no note keeps the old behaviour rather than recording
// silence: the block reason is a worse answer than the requester's own, and a
// better one than nothing.
func TestCreatePolicyBypassRequest_FallsBackToTheBlockReasonWithoutANote(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err: nil, requestID: "0195c1f1-0000-7000-8000-00000000beef", status: "requested",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("No Note"),
	})
	require.NoError(t, err)

	_, err = redeemWithNote(ctx, ti, riskPolicyBypassRequestTokenWithBlockReason(
		t, ti, authCtx, policy.ID, "https://mcp.example.com/unnoted", "Blocked by policy: unreviewed MCP server",
	), "   ")
	require.NoError(t, err)

	require.Equal(t, "Blocked by policy: unreviewed MCP server", intake.gotNote)
}

// An intake that reports the approval feature is unavailable falls back to
// the legacy bypass request, so organizations without the workflow keep the
// old flow intact.
func TestCreatePolicyBypassRequest_FallsBackWhenApprovalUnavailable(t *testing.T) {
	t.Parallel()

	intake := &fakeApprovalIntake{
		err:       oops.E(oops.CodeForbidden, nil, "MCP approval is not enabled for this organization"),
		requestID: "", status: "",
		gotOrganizationID: "", gotProjectID: uuid.Nil, gotServerURL: "", gotRequesterID: "", gotNote: "",
	}
	ctx, ti := newTestRiskService(t, func(instance *testInstance) {
		instance.approvalIntake = intake
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Approval Intake Fallback"),
	})
	require.NoError(t, err)

	redemption, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, "https://mcp.example.com/fallback"),
	})
	require.NoError(t, err)
	require.Equal(t, "bypass_request", redemption.Kind)
	require.Equal(t, "requested", redemption.Status)
	require.Equal(t, authCtx.UserID, redeemedBypassRow(t, ctx, ti, redemption).RequesterUserID)
}

// withAgentKeyAuth rewrites the auth context to look like an API-key-
// authenticated device agent: no session, the given key scopes, and the key
// owner as the caller. Mirrors what internal/auth/key.go builds for a
// Gram-Key request.
func withAgentKeyAuth(t *testing.T, ctx context.Context, scopes []string, ownerUserID string) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.APIKeyScopes = scopes
	clone.SessionID = nil
	clone.UserID = ownerUserID
	return contextvalues.SetAuthContext(ctx, &clone)
}

// The device agent files the request with the per-user `agent_user` key. Its
// owner is the enrolled user, so the token's requester binding passes and the
// request is attributed to the key owner.
func TestCreateRiskPolicyBypassRequest_AgentUserKeyCreatesForKeyOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	adminCtx := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(adminCtx, &gen.CreateRiskPolicyPayload{
		Name: new("Agent Key Bypass Request"),
	})
	require.NoError(t, err)

	fullURL := "https://mcp.example.com/agent-key"
	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, fullURL)

	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestCreate)
	require.NoError(t, err)

	keyCtx := withAgentKeyAuth(t, ctx, []string{"agent_user"}, authCtx.UserID)
	request, err := ti.service.CreateRiskPolicyBypassRequest(keyCtx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	require.NotNil(t, request)
	require.Equal(t, "requested", request.Status)
	// The stack's redemption result carries only kind/id/status; the row
	// holds the attribution main's version of this test read directly.
	row := redeemedBypassRow(t, ctx, ti, request)
	assert.Equal(t, authCtx.UserID, row.RequesterUserID)
	require.NotNil(t, row.TargetKey)
	assert.Equal(t, fullURL, *row.TargetKey)

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestCreate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterAuditCount, "key-auth create must audit like the session path")
}

// A key owned by anyone other than the token's requester must not redeem it —
// the same binding that stops a leaked link stops a leaked or wrong key.
func TestCreateRiskPolicyBypassRequest_OtherUsersKeyForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	adminCtx := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(adminCtx, &gen.CreateRiskPolicyPayload{
		Name: new("Agent Key Bypass Wrong Owner"),
	})
	require.NoError(t, err)

	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, "https://mcp.example.com/wrong-owner")

	keyCtx := withAgentKeyAuth(t, ctx, []string{"agent_user"}, "user_someone_else")
	_, err = ti.service.CreateRiskPolicyBypassRequest(keyCtx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// The shared org install key (`agent` scope) is owned by the provisioning
// admin, not the developer named in an attributed token — it must not be able
// to file requests on that developer's behalf. The daemon only ever uses the
// per-user key for this call; this pins the server-side backstop.
func TestCreateRiskPolicyBypassRequest_OrgKeyAttributedTokenForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	adminCtx := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(adminCtx, &gen.CreateRiskPolicyPayload{
		Name: new("Agent Org Key Bypass Request"),
	})
	require.NoError(t, err)

	token := riskPolicyBypassRequestToken(t, ti, authCtx, policy.ID, "https://mcp.example.com/org-key")

	orgKeyCtx := withAgentKeyAuth(t, ctx, []string{"agent", "agent_user"}, "user_provisioning_admin")
	_, err = ti.service.CreateRiskPolicyBypassRequest(orgKeyCtx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

// The `agent_user` scope gate lives in the generated security wiring — the
// design's Security(ByKey, Scope("agent_user")) becomes the key scheme's
// RequiredScopes in gen/risk — not in the handler, so the direct-call tests
// above cannot see it. Drive the generated endpoint with a recording
// authorizer to pin that boundary: key auth must demand agent_user, and an
// authorization failure must short-circuit before the service method runs.
func TestCreateRiskPolicyBypassRequest_EndpointKeyAuthDemandsAgentUserScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	var schemes []*security.APIKeyScheme
	forbidden := oops.C(oops.CodeForbidden)
	endpoint := gen.NewCreateRiskPolicyBypassRequestEndpoint(ti.service,
		func(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
			schemes = append(schemes, scheme)
			return ctx, forbidden
		})

	_, err := endpoint(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		SessionToken: nil,
		ApikeyToken:  new("gram_test_key"),
		RequestToken: "rpbr2.never-redeemed",
	})
	require.Equal(t, forbidden, err, "the authorizer's rejection must surface, not a handler error")

	require.Len(t, schemes, 2, "session scheme tried first, then the key scheme")
	require.Equal(t, "session", schemes[0].Name)
	require.Empty(t, schemes[0].RequiredScopes)
	require.Equal(t, "apikey", schemes[1].Name)
	require.Equal(t, []string{"agent_user"}, schemes[1].RequiredScopes,
		"key auth must demand agent_user; removing the design's Scope() regenerates this away and fails here")
}
