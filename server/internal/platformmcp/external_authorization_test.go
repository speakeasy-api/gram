package platformmcp

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestExternalAuthorizationPoliciesBuildTypedChecks(t *testing.T) {
	t.Parallel()

	org := authz.OrgIDFrom("organization-1")

	_, gated := ExternalAuthorizationMember.check(org)
	require.False(t, gated, "member relies on live membership only")
	_, requestable := ExternalAuthorizationMember.requestCheck(org)
	require.False(t, requestable)

	check, gated := ExternalAuthorizationOrgAdmin.check(org)
	require.True(t, gated)
	require.Equal(t, authz.Org(org).Admin(), check)
	request, requestable := ExternalAuthorizationOrgAdmin.requestCheck(org)
	require.True(t, requestable)
	require.Equal(t, authz.Org(org).Admin(), request)

	var none ExternalAuthorization
	require.True(t, none.IsZero())
	require.False(t, ExternalAuthorizationMember.IsZero())
	require.Equal(t, "none", none.String())
	require.Equal(t, "member", ExternalAuthorizationMember.String())
	require.Equal(t, "org_admin", ExternalAuthorizationOrgAdmin.String())
}

// Discovery of an org admin tool used to probe org:admin by scope string. It
// now asks the policy's typed check, and must list exactly the same tools.
func TestExternalAuthorizationDiscoveryMatchesTheOrgAdminScopeProbe(t *testing.T) {
	t.Parallel()

	principal := testPrincipal()
	org := principal.OrganizationID
	grantSets := map[string][]authz.Grant{
		"none":                    nil,
		"org admin":               {authz.NewGrant(authz.ScopeOrgAdmin, org)},
		"org admin everywhere":    {authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)},
		"org admin elsewhere":     {authz.NewGrant(authz.ScopeOrgAdmin, "organization-2")},
		"org admin and its block": {authz.NewGrant(authz.ScopeOrgAdmin, org), authz.NewGrant(authz.ScopeOrgBlockedAdmin, org)},
		"block elsewhere":         {authz.NewGrant(authz.ScopeOrgAdmin, org), authz.NewGrant(authz.ScopeOrgBlockedAdmin, "organization-2")},
		"org read":                {authz.NewGrant(authz.ScopeOrgRead, org)},
		"project write":           {authz.NewGrant(authz.ScopeProjectWrite, authz.WildcardResource)},
		"org admin and mcp read":  {authz.NewGrant(authz.ScopeOrgAdmin, org), authz.NewGrant(authz.ScopeMCPRead, "mcp-1")},
		"root":                    {authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)},
	}
	metas := map[string]ToolMeta{
		"admin":     {Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly},
		"admin_mcp": {Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, DiscoveryScopes: discoveryMCPRead},
	}

	for grantsName, grants := range grantSets {
		for toolName, meta := range metas {
			previous := grantsAuthorizeAnyScope(grants, org, []authz.Scope{authz.ScopeOrgAdmin}) &&
				(len(meta.DiscoveryScopes) == 0 || grantsAuthorizeAnyScope(grants, org, meta.DiscoveryScopes))
			require.Equal(t, previous, externalToolDiscoverable(grants, true, principal, meta), "%s with %s", toolName, grantsName)
		}
	}
}

// The policy a family PR uses while its gates are rewritten: the new scope or
// org:admin, asking callers to request the new scope.
func TestExternalAuthorizationEitherPolicy(t *testing.T) {
	t.Parallel()

	principal := testPrincipal()
	org := principal.OrganizationID
	either := ExternalAuthorization{policy: &externalPolicy{
		name: "org_read_or_admin",
		requires: func(org authz.OrgID) authz.Check {
			return authz.Either(authz.Org(org).Read(), authz.Org(org).Admin())
		},
		request: func(org authz.OrgID) authz.Check { return authz.Org(org).Read() },
	}}
	meta := ToolMeta{Authorization: either, Audiences: externalOnly}

	cases := []struct {
		name   string
		grants []authz.Grant
		want   bool
	}{
		{name: "none", grants: nil, want: false},
		{name: "new scope", grants: []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, org)}, want: true},
		{name: "org admin", grants: []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, org)}, want: true},
		{name: "new scope elsewhere", grants: []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, "organization-2")}, want: false},
		{
			name:   "a block on either alternative refuses the group",
			grants: []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, org), authz.NewGrant(authz.ScopeOrgBlockedAdmin, org)},
			want:   false,
		},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, externalToolDiscoverable(tc.grants, true, principal, meta), tc.name)
	}

	request, ok := either.requestCheck(authz.OrgIDFrom(org))
	require.True(t, ok)
	require.Equal(t, authz.Org(authz.OrgIDFrom(org)).Read(), request)

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: org, OrganizationSlug: "acme"})
	authorizer := (&LiveOrgAdminAuthorizer{}).WithDashboardURL(mustTestURL(t, "https://app.example.test"))
	link, err := url.Parse(authorizer.requestAccessURL(ctx, request))
	require.NoError(t, err)
	require.Equal(t, "/acme/request-access", link.Path)
	require.Equal(t, string(authz.ScopeOrgRead), link.Query().Get("scope"))
	require.Equal(t, org, link.Query().Get("resource_id"))
}

// A prefilled request names one grant a person can hold on one resource.
func TestRequestAccessURLWithholdsChecksNobodyCanRequest(t *testing.T) {
	t.Parallel()

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "organization-1", OrganizationSlug: "acme"})
	authorizer := (&LiveOrgAdminAuthorizer{}).WithDashboardURL(mustTestURL(t, "https://app.example.test"))

	require.NotEmpty(t, authorizer.requestAccessURL(ctx, authz.Org(authz.OrgIDFrom("organization-1")).Admin()))
	require.Empty(t, authorizer.requestAccessURL(ctx, authz.Org(authz.OrgIDFrom("")).Admin()), "no concrete organization")
	require.Empty(t, authorizer.requestAccessURL(ctx, authz.Check{Scope: authz.ScopeOrgBlockedAdmin, ResourceID: "organization-1"}), "a block is internal")
	require.Empty(t, authorizer.requestAccessURL(ctx, authz.Check{Scope: "not:a_scope", ResourceID: "organization-1"}), "unknown scope")
}
