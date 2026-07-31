package policybypass

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCanonicalizeURLs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		rawURLs  []string
		expected []string
	}{
		{
			name: "sorts and deduplicates canonical URLs",
			rawURLs: []string{
				"HTTPS://MCP.EXAMPLE.COM:443/mcp?token=one#fragment",
				"https://mcp.example.com/mcp?token=two",
				"https://second.example.com/sse",
			},
			expected: []string{
				"https://mcp.example.com/mcp",
				"https://second.example.com/sse",
			},
		},
	}

	for _, testCase := range testCases {
		got, err := CanonicalizeURLs(testCase.rawURLs)
		require.NoError(t, err, testCase.name)
		require.Equal(t, testCase.expected, got, testCase.name)
	}
}

func TestCanonicalizeURLsRejectsInvalidURL(t *testing.T) {
	t.Parallel()

	_, err := CanonicalizeURLs([]string{"not a shadow mcp url"})
	require.ErrorContains(t, err, "invalid shadow mcp server url")
}

func TestReconcilePolicyURLsAddsRemovesAndPreserves(t *testing.T) {
	t.Parallel()

	ctx, conn, organizationID := newURLGrantTestDatabase(t)
	policyID := uuid.NewString()
	otherPolicyID := uuid.NewString()
	oldAudience := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, "old-user")}
	newAudience := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeRole, "security-admins")}

	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, policyID, "https://remove.example.com/mcp", oldAudience))
	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, policyID, "https://keep.example.com/mcp", oldAudience))
	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, otherPolicyID, "https://remove.example.com/mcp", oldAudience))

	input := ReconcilePolicyURLsInput{
		OrganizationID: organizationID,
		PolicyID:       policyID,
		DesiredURLs:    []string{"https://keep.example.com/mcp", "https://add.example.com/mcp"},
		Principals:     newAudience,
	}
	require.NoError(t, ReconcilePolicyURLs(ctx, conn, input))
	require.NoError(t, ReconcilePolicyURLs(ctx, conn, input))

	require.Equal(t, map[string][]string{
		"https://add.example.com/mcp":  {"role:security-admins"},
		"https://keep.example.com/mcp": {"role:security-admins"},
	}, policyURLGrantPrincipals(t, ctx, conn, organizationID, policyID))
	require.Contains(t, policyURLGrantPrincipals(t, ctx, conn, organizationID, otherPolicyID), "https://remove.example.com/mcp")
}

func TestReconcilePolicyURLsRemovesMixedSelectorForDeselectedURL(t *testing.T) {
	t.Parallel()

	ctx, conn, organizationID := newURLGrantTestDatabase(t)
	policyID := uuid.NewString()
	serverURL := "https://remove-mixed.example.com/mcp"
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "old-user")
	mixedSelector := URLSelector(policyID, serverURL)
	mixedSelector[authz.SelectorKeyServerIdentity] = "shadow-server"

	require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: []urn.Principal{principal},
		Selector:   mixedSelector,
	}))
	require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: []urn.Principal{principal},
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID),
	}))

	require.NoError(t, ReconcilePolicyURLs(ctx, conn, ReconcilePolicyURLsInput{
		OrganizationID: organizationID,
		PolicyID:       policyID,
		DesiredURLs:    nil,
		Principals:     nil,
	}))

	require.Empty(t, policyURLGrants(t, ctx, conn, organizationID, policyID))
	grants, err := authz.ListGrantsForResource(ctx, conn, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
	})
	require.NoError(t, err)
	require.Contains(t, grants, authz.Grant{
		PrincipalUrn: principal.String(),
		Scope:        authz.ScopeRiskPolicyBypass,
		Effect:       authz.PolicyEffectAllow,
		Selector:     authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID),
	})
}

func TestReconcilePolicyURLsNormalizesMixedSelectorForRetainedURL(t *testing.T) {
	t.Parallel()

	ctx, conn, organizationID := newURLGrantTestDatabase(t)
	policyID := uuid.NewString()
	serverURL := "https://retain-mixed.example.com/mcp"
	mixedSelector := URLSelector(policyID, serverURL)
	mixedSelector[authz.SelectorKeyServerIdentity] = "shadow-server"

	require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, "old-user")},
		Selector:   mixedSelector,
	}))

	newPrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, "security-admins")
	require.NoError(t, ReconcilePolicyURLs(ctx, conn, ReconcilePolicyURLsInput{
		OrganizationID: organizationID,
		PolicyID:       policyID,
		DesiredURLs:    []string{serverURL},
		Principals:     []urn.Principal{newPrincipal},
	}))

	require.Equal(t, []authz.Grant{
		{
			PrincipalUrn: newPrincipal.String(),
			Scope:        authz.ScopeRiskPolicyBypass,
			Effect:       authz.PolicyEffectAllow,
			Selector:     URLSelector(policyID, serverURL),
		},
	}, policyURLGrants(t, ctx, conn, organizationID, policyID))
}

func TestReplacePolicyURLAudienceReplacesExactURLAudience(t *testing.T) {
	t.Parallel()

	ctx, conn, organizationID := newURLGrantTestDatabase(t)
	policyID := uuid.NewString()
	serverURL := "https://replace.example.com/mcp"

	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, policyID, serverURL, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, "old-user"),
	}))
	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, policyID, serverURL, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeRole, "security-admins"),
	}))

	require.Equal(t, map[string][]string{
		serverURL: {"role:security-admins"},
	}, policyURLGrantPrincipals(t, ctx, conn, organizationID, policyID))
}

func TestRevokePolicyURLPreservesUnrelatedGrants(t *testing.T) {
	t.Parallel()

	ctx, conn, organizationID := newURLGrantTestDatabase(t)
	policyID := uuid.NewString()
	otherPolicyID := uuid.NewString()
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user-one")
	targetURL := "https://remove.example.com/mcp"
	keepURL := "https://keep.example.com/mcp"

	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, policyID, targetURL, []urn.Principal{principal}))
	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, policyID, keepURL, []urn.Principal{principal}))
	require.NoError(t, ReplacePolicyURLAudience(ctx, conn, organizationID, otherPolicyID, targetURL, []urn.Principal{principal}))
	require.NoError(t, authz.ReplaceGrantAudience(ctx, conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: []urn.Principal{principal},
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID),
	}))

	require.NoError(t, RevokePolicyURL(ctx, conn, organizationID, policyID, targetURL))

	require.Equal(t, map[string][]string{
		keepURL: {"user:user-one"},
	}, policyURLGrantPrincipals(t, ctx, conn, organizationID, policyID))
	require.Contains(t, policyURLGrantPrincipals(t, ctx, conn, organizationID, otherPolicyID), targetURL)

	grants, err := authz.ListGrantsForResource(ctx, conn, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
	})
	require.NoError(t, err)
	require.Contains(t, grants, authz.Grant{
		PrincipalUrn: principal.String(),
		Scope:        authz.ScopeRiskPolicyBypass,
		Effect:       authz.PolicyEffectAllow,
		Selector:     authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID),
	})
}

func policyURLGrantPrincipals(
	t *testing.T,
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	policyID string,
) map[string][]string {
	t.Helper()

	principalsByURL := map[string][]string{}
	for _, grant := range policyURLGrants(t, ctx, db, organizationID, policyID) {
		serverURL := grant.Selector[authz.SelectorKeyServerURL]
		if !maps.Equal(grant.Selector, URLSelector(policyID, serverURL)) {
			continue
		}
		principalsByURL[serverURL] = append(principalsByURL[serverURL], grant.PrincipalUrn)
	}
	for serverURL := range principalsByURL {
		slices.Sort(principalsByURL[serverURL])
	}

	return principalsByURL
}

func policyURLGrants(
	t *testing.T,
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	policyID string,
) []authz.Grant {
	t.Helper()

	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID,
	})
	require.NoError(t, err)

	result := make([]authz.Grant, 0, len(grants))
	for _, grant := range grants {
		if grant.Effect != authz.PolicyEffectAllow || grant.Selector[authz.SelectorKeyServerURL] == "" {
			continue
		}
		result = append(result, grant)
	}

	return result
}
