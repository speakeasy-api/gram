package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestReplaceGrantsForResource_replacesGrantsForResource(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_policy_evaluate"
	policyID := "policy_123"
	otherPolicyID := "policy_456"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_123")
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_support")
	otherPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_other")

	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, otherPolicyID, []urn.Principal{otherPrincipal}))

	require.NoError(t, ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID, []urn.Principal{userPrincipal, rolePrincipal, rolePrincipal}))

	grants, err := ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 2)
	require.ElementsMatch(t, []string{userPrincipal.String(), rolePrincipal.String()}, []string{grants[0].PrincipalUrn, grants[1].PrincipalUrn})
	for _, grant := range grants {
		require.Equal(t, ScopeRiskPolicyEvaluate, grant.Scope)
		require.Equal(t, PolicyEffectAllow, grant.Effect)
		require.Equal(t, Selector{"resource_kind": ResourceKindRiskPolicy, "resource_id": policyID}, grant.Selector)
	}

	require.NoError(t, ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID, []urn.Principal{rolePrincipal}))

	grants, err = ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, rolePrincipal.String(), grants[0].PrincipalUrn)

	otherGrants, err := ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, otherPolicyID)
	require.NoError(t, err)
	require.Len(t, otherGrants, 1)
	require.Equal(t, otherPrincipal.String(), otherGrants[0].PrincipalUrn)
}

func TestReplaceGrantsForResource_emptyPrincipalsClearsResourceGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_policy_evaluate_clear"
	policyID := "policy_123"

	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_123"),
	}))

	require.NoError(t, ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID, nil))

	grants, err := ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Empty(t, grants)
}

func TestReplaceGrantsForResource_invalidPrincipalDoesNotClearExistingGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_policy_evaluate_invalid"
	policyID := "policy_123"
	userPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_123")

	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID, []urn.Principal{userPrincipal}))

	err := ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID, []urn.Principal{{}})
	require.Error(t, err)

	grants, err := ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, userPrincipal.String(), grants[0].PrincipalUrn)
}
