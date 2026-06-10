package risk_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
	token := riskPolicyBypassRequestToken(t, authCtx, policy.ID, fullURL)

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
	assert.Equal(t, policy.ID, request.PolicyID)
	assert.Equal(t, "requested", request.Status)
	require.NotNil(t, request.TargetKind)
	assert.Equal(t, "shadow_mcp_server", *request.TargetKind)
	require.NotNil(t, request.TargetKey)
	assert.Equal(t, fullURL, *request.TargetKey)
	assert.Equal(t, fullURL, request.TargetDimensions[authz.SelectorKeyServerURL])
	assert.Equal(t, authCtx.UserID, request.RequesterUserID)

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
	token := riskPolicyBypassRequestTokenForServerIdentity(t, authCtx, policy.ID, serverIdentity)
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	require.NotNil(t, request)
	assert.Equal(t, policy.ID, request.PolicyID)
	assert.Equal(t, "requested", request.Status)
	require.NotNil(t, request.TargetKind)
	assert.Equal(t, "shadow_mcp_server", *request.TargetKind)
	require.NotNil(t, request.TargetKey)
	assert.Equal(t, serverIdentity, *request.TargetKey)
	assert.Equal(t, serverIdentity, request.TargetDimensions[authz.SelectorKeyServerIdentity])
	assert.Equal(t, authCtx.UserID, request.RequesterUserID)

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
		RequestToken: riskPolicyBypassRequestToken(t, authCtx, policy.ID, fullURL),
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
		RequestToken: riskPolicyBypassRequestToken(t, authCtx, policy.ID, fullURL),
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
		RequestToken: riskPolicyBypassRequestToken(t, authCtx, policy.ID, fullURL),
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
		RequestToken: riskPolicyBypassRequestToken(t, authCtx, policy.ID, "https://mcp.example.com/unknown-role"),
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

	token := riskPolicyBypassRequestToken(t, authCtx, policy.ID, "https://mcp.example.com/denied")
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
	assert.Nil(t, refreshed.DecidedBy)
	assert.Empty(t, refreshed.GrantedPrincipalUrns)
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
	token := riskPolicyBypassRequestToken(t, authCtx, policy.ID, fullURL)
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
	assert.Equal(t, approved.DecidedBy, refreshed.DecidedBy)
	assert.Equal(t, approved.DecidedAt, refreshed.DecidedAt)
	assert.Equal(t, approved.GrantedPrincipalUrns, refreshed.GrantedPrincipalUrns)
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
	token, _, err := risk.GeneratePolicyBypassRequestToken("test-jwt-secret", risk.PolicyBypassRequestTokenInput{
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
	assert.Equal(t, policy.ID, request.PolicyID)
	assert.Equal(t, "requested", request.Status)
	assert.Nil(t, request.TargetKind)
	require.NotNil(t, request.TargetKey)
	assert.Equal(t, "policy", *request.TargetKey)
	assert.Empty(t, request.TargetDimensions)
}

func TestGeneratePolicyBypassRequestToken_RequiresEvidence(t *testing.T) {
	t.Parallel()

	_, _, err := risk.GeneratePolicyBypassRequestToken("test-jwt-secret", risk.PolicyBypassRequestTokenInput{
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

func riskPolicyBypassRequestToken(t *testing.T, authCtx *contextvalues.AuthContext, policyID string, fullURL string) string {
	t.Helper()

	token, _, err := risk.GeneratePolicyBypassRequestToken("test-jwt-secret", risk.PolicyBypassRequestTokenInput{
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

func riskPolicyBypassRequestTokenForServerIdentity(t *testing.T, authCtx *contextvalues.AuthContext, policyID string, serverIdentity string) string {
	t.Helper()

	token, _, err := risk.GeneratePolicyBypassRequestToken("test-jwt-secret", risk.PolicyBypassRequestTokenInput{
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
