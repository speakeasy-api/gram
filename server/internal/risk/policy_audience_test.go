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
		testenv.NewMeterProvider(t),
	)
	require.NoError(t, err)

	otherUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "user_"+uuid.NewString(), "irrelevant text", message.User)
	require.NoError(t, err)
	require.Nil(t, otherUserResult)

	targetedUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "irrelevant text", message.User)
	require.NoError(t, err)
	require.NotNil(t, targetedUserResult)
	require.Equal(t, "Targeted Runtime", targetedUserResult.PolicyName)
}

func TestScanner_ScanForEnforcement_RequiresResolvedAudiencePrincipal(t *testing.T) {
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
		testenv.NewMeterProvider(t),
	)
	require.NoError(t, err)

	emptyUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "", "irrelevant text", message.User)
	require.NoError(t, err)
	require.Nil(t, emptyUserResult)

	unknownUserResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "user_"+uuid.NewString(), "irrelevant text", message.User)
	require.NoError(t, err)
	require.Nil(t, unknownUserResult)

	memberResult, err := scanner.ScanForEnforcement(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, "irrelevant text", message.User)
	require.NoError(t, err)
	require.NotNil(t, memberResult)
	require.Equal(t, "Everyone Runtime", memberResult.PolicyName)
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
