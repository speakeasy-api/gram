package risk_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRiskPolicyAudience_TargetedUserLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	targeted := "targeted"
	audience := []string{"user:" + authCtx.UserID}
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Targeted Policy"),
		Sources:               []string{"gitleaks"},
		AudienceType:          targeted,
		AudiencePrincipalUrns: audience,
	})
	require.NoError(t, err)
	require.Equal(t, targeted, created.AudienceType)
	require.Equal(t, audience, created.AudiencePrincipalUrns)
	requirePolicyAudience(t, ctx, ti, authCtx.ActiveOrganizationID, created.ID, audience)

	got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, targeted, got.AudienceType)
	require.Equal(t, audience, got.AudiencePrincipalUrns)

	everyone := "everyone"
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                    created.ID,
		Name:                  created.Name,
		AudienceType:          &everyone,
		AudiencePrincipalUrns: nil,
	})
	require.NoError(t, err)
	require.Equal(t, everyone, updated.AudienceType)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, updated.AudiencePrincipalUrns)
	requirePolicyAudience(t, ctx, ti, authCtx.ActiveOrganizationID, created.ID, []string{authz.AllUsersPrincipal().String()})

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	requirePolicyAudience(t, ctx, ti, authCtx.ActiveOrganizationID, created.ID, nil)
}

func TestRiskPolicyAudience_InvalidTargetedPrincipalRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	targeted := "targeted"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Invalid Audience"),
		Sources:               []string{"gitleaks"},
		AudienceType:          targeted,
		AudiencePrincipalUrns: []string{"user:user_" + uuid.NewString()},
	})
	require.Error(t, err)
}

func TestRiskPolicyAudience_UpdatePreservesNonAudienceGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	everyone := "everyone"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Preserve Grants"),
		Sources:               []string{"shadow_mcp"},
		AudienceType:          everyone,
		AudiencePrincipalUrns: nil,
		Action:                "block",
	})
	require.NoError(t, err)

	denyPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_deny")
	bypassPrincipal := authz.AllUsersPrincipal()
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     created.ID,
		},
		Effect:     authz.PolicyEffectDeny,
		Principals: []urn.Principal{denyPrincipal},
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, created.ID),
	}))
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     created.ID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: []urn.Principal{bypassPrincipal},
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindRiskPolicy,
			authz.SelectorKeyResourceID:   created.ID,
			authz.SelectorKeyServerURL:    "https://api.example.com",
		},
	}))

	targeted := "targeted"
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                    created.ID,
		Name:                  created.Name,
		AudienceType:          &targeted,
		AudiencePrincipalUrns: []string{"user:" + authCtx.UserID},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"user:" + authCtx.UserID}, updated.AudiencePrincipalUrns)

	evaluateGrants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyEvaluate,
		ResourceID:     created.ID,
	})
	require.NoError(t, err)
	require.Contains(t, grantKeys(evaluateGrants), grantKey(authz.PolicyEffectAllow, "user:"+authCtx.UserID))
	require.Contains(t, grantKeys(evaluateGrants), grantKey(authz.PolicyEffectDeny, denyPrincipal.String()))

	bypassGrants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
		OrganizationID: authCtx.ActiveOrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     created.ID,
	})
	require.NoError(t, err)
	require.Contains(t, grantKeys(bypassGrants), grantKey(authz.PolicyEffectAllow, bypassPrincipal.String()))
}

func TestScanner_ScanForEnforcement_RespectsTargetedAudience(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	targeted := "targeted"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Targeted Runtime"),
		Sources:               []string{"presidio"},
		PresidioEntities:      []string{"EMAIL_ADDRESS"},
		AudienceType:          targeted,
		AudiencePrincipalUrns: []string{"user:" + authCtx.UserID},
		Action:                "block",
	})
	require.NoError(t, err)

	pii := &instrumentedPIIScanner{findOnEntity: "EMAIL_ADDRESS"}
	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		ti.conn,
		pii,
		nil,
		nil,
		nil,
		testenv.NewMeterProvider(t),
		testCELEngine(t),
	)
	require.NoError(t, err)

	otherUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "user_"+uuid.NewString(), "irrelevant text", message.User, "")
	require.NoError(t, err)
	require.Nil(t, otherUserResult)

	targetedUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "irrelevant text", message.User, "")
	require.NoError(t, err)
	require.NotNil(t, targetedUserResult)
	require.Equal(t, "Targeted Runtime", targetedUserResult.PolicyName)
}

func TestScanner_ScanForEnforcement_EveryoneAudienceAppliesWithoutResolvedUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	everyone := "everyone"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Everyone Runtime"),
		Sources:               []string{"presidio"},
		PresidioEntities:      []string{"EMAIL_ADDRESS"},
		AudienceType:          everyone,
		AudiencePrincipalUrns: nil,
		Action:                "block",
	})
	require.NoError(t, err)

	pii := &instrumentedPIIScanner{findOnEntity: "EMAIL_ADDRESS"}
	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		ti.conn,
		pii,
		nil,
		nil,
		nil,
		testenv.NewMeterProvider(t),
		testCELEngine(t),
	)
	require.NoError(t, err)

	emptyUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "", "irrelevant text", message.User, "")
	require.NoError(t, err)
	require.NotNil(t, emptyUserResult)
	require.Equal(t, "Everyone Runtime", emptyUserResult.PolicyName)

	unknownUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "user_"+uuid.NewString(), "irrelevant text", message.User, "")
	require.NoError(t, err)
	require.NotNil(t, unknownUserResult)
	require.Equal(t, "Everyone Runtime", unknownUserResult.PolicyName)

	memberResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "irrelevant text", message.User, "")
	require.NoError(t, err)
	require.NotNil(t, memberResult)
	require.Equal(t, "Everyone Runtime", memberResult.PolicyName)
}

func TestScanner_LookupShadowMCPBlockingPolicy_EveryoneAudienceAppliesWithoutResolvedUser(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	everyone := "everyone"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Everyone Shadow MCP"),
		Sources:               []string{"shadow_mcp"},
		AudienceType:          everyone,
		AudiencePrincipalUrns: nil,
		Action:                "block",
	})
	require.NoError(t, err)

	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		ti.conn,
		nil,
		nil,
		nil,
		nil,
		testenv.NewMeterProvider(t),
		testCELEngine(t),
	)
	require.NoError(t, err)

	emptyUserPolicy, err := scanner.LookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "")
	require.NoError(t, err)
	require.NotNil(t, emptyUserPolicy)
	require.Equal(t, "Everyone Shadow MCP", emptyUserPolicy.Name)

	unknownUserPolicy, err := scanner.LookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "user_"+uuid.NewString())
	require.NoError(t, err)
	require.NotNil(t, unknownUserPolicy)
	require.Equal(t, "Everyone Shadow MCP", unknownUserPolicy.Name)
}

func grantKey(effect authz.PolicyEffect, principalURN string) string {
	return string(effect) + ":" + principalURN
}

func grantKeys(grants []authz.Grant) []string {
	keys := make([]string, 0, len(grants))
	for _, grant := range grants {
		keys = append(keys, grantKey(grant.Effect, grant.PrincipalUrn))
	}
	return keys
}

func requirePolicyAudience(t *testing.T, ctx context.Context, ti *testInstance, orgID, policyID string, want []string) {
	t.Helper()

	grants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
		OrganizationID: orgID,
		Scope:          authz.ScopeRiskPolicyEvaluate,
		ResourceID:     policyID,
	})
	require.NoError(t, err)

	got := make([]string, 0, len(grants))
	for _, grant := range grants {
		got = append(got, grant.PrincipalUrn)
	}
	require.ElementsMatch(t, want, got)
}
