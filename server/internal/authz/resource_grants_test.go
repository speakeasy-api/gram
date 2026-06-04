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

func TestReplaceGrantsForResource_invalidInputDoesNotBeginTransaction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	testCases := []struct {
		name           string
		organizationID string
		scope          Scope
		resourceID     string
		principals     []urn.Principal
		wantErr        string
	}{
		{
			name:           "missing organization id",
			organizationID: "",
			scope:          ScopeRiskPolicyEvaluate,
			resourceID:     "policy_123",
			principals:     nil,
			wantErr:        "organization id is required",
		},
		{
			name:           "missing scope",
			organizationID: "org_policy_evaluate_invalid_input",
			scope:          "",
			resourceID:     "policy_123",
			principals:     nil,
			wantErr:        "scope is required",
		},
		{
			name:           "missing resource id",
			organizationID: "org_policy_evaluate_invalid_input",
			scope:          ScopeRiskPolicyEvaluate,
			resourceID:     "",
			principals:     nil,
			wantErr:        "resource id is required",
		},
		{
			name:           "invalid principal",
			organizationID: "org_policy_evaluate_invalid_input",
			scope:          ScopeRiskPolicyEvaluate,
			resourceID:     "policy_123",
			principals:     []urn.Principal{{}},
			wantErr:        "invalid grant principal",
		},
		{
			name:           "scope without resource kind",
			organizationID: "org_policy_evaluate_invalid_input",
			scope:          ScopeRoot,
			resourceID:     "policy_123",
			principals:     nil,
			wantErr:        `scope "root" does not map to a resource kind`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var err error
			require.NotPanics(t, func() {
				err = ReplaceGrantsForResource(ctx, nil, tc.organizationID, tc.scope, tc.resourceID, tc.principals)
			})
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
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

func TestGrantAndRevokeResourcePrincipalGrantDoesNotReplaceResourceSet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_policy_evaluate_single"
	policyID := "policy_123"
	firstPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_123")
	secondPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "role_support")

	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, ReplaceGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID, []urn.Principal{firstPrincipal}))

	require.NoError(t, GrantResourceToPrincipalTx(ctx, conn, organizationID, secondPrincipal, ScopeRiskPolicyEvaluate, policyID))
	require.NoError(t, GrantResourceToPrincipalTx(ctx, conn, organizationID, secondPrincipal, ScopeRiskPolicyEvaluate, policyID))

	grants, err := ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 2)
	require.ElementsMatch(t, []string{firstPrincipal.String(), secondPrincipal.String()}, []string{grants[0].PrincipalUrn, grants[1].PrincipalUrn})

	require.NoError(t, RevokeResourceFromPrincipalTx(ctx, conn, organizationID, secondPrincipal, ScopeRiskPolicyEvaluate, policyID))
	require.NoError(t, RevokeResourceFromPrincipalTx(ctx, conn, organizationID, secondPrincipal, ScopeRiskPolicyEvaluate, policyID))

	grants, err = ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, firstPrincipal.String(), grants[0].PrincipalUrn)
}

func TestPatchPrincipalGrantsTxAddsAndRemovesExactGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_patch_principal_grants"
	policyID := "policy_123"
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_123")
	otherPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_other")
	selector := NewSelector(ScopeRiskPolicyEvaluate, policyID)

	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, PatchPrincipalGrantsTx(ctx, conn, organizationID, otherPrincipal, []*RoleGrant{{
		Scope:     string(ScopeRiskPolicyEvaluate),
		Effect:    PolicyEffectAllow,
		Selectors: []Selector{selector},
	}}, nil))

	require.NoError(t, PatchPrincipalGrantsTx(ctx, conn, organizationID, principal, []*RoleGrant{{
		Scope:     string(ScopeRiskPolicyEvaluate),
		Effect:    PolicyEffectAllow,
		Selectors: []Selector{selector},
	}}, nil))

	grants, err := ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 2)

	require.NoError(t, PatchPrincipalGrantsTx(ctx, conn, organizationID, principal, nil, []*RoleGrant{{
		Scope:     string(ScopeRiskPolicyEvaluate),
		Effect:    PolicyEffectAllow,
		Selectors: []Selector{selector},
	}}))

	grants, err = ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyEvaluate, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, otherPrincipal.String(), grants[0].PrincipalUrn)
}

func TestGrantAndRevokeResourcePrincipalGrantWithSelector(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_policy_bypass_single"
	policyID := "policy_123"
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_123")
	selector := Selector{
		"resource_kind": ResourceKindRiskPolicy,
		"resource_id":   policyID,
		"server_url":    "api.example.com",
	}

	seedOrganization(t, ctx, conn, organizationID)
	require.NoError(t, GrantResourceToPrincipalWithSelectorTx(ctx, conn, organizationID, principal, ScopeRiskPolicyBypass, selector))

	grants, err := ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyBypass, policyID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, principal.String(), grants[0].PrincipalUrn)
	require.Equal(t, selector, grants[0].Selector)

	require.NoError(t, RevokeResourceFromPrincipalWithSelectorTx(ctx, conn, organizationID, principal, ScopeRiskPolicyBypass, selector))

	grants, err = ListGrantsForResource(ctx, conn, organizationID, ScopeRiskPolicyBypass, policyID)
	require.NoError(t, err)
	require.Empty(t, grants)
}
