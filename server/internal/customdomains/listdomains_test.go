package customdomains_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListDomains_NoCustomDomain_ReturnsEmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.ListDomains(ctx, &gen.ListDomainsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Domains)
}

func TestListDomains_ReturnsConfiguredDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "docs.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.ListDomains(ctx, &gen.ListDomainsPayload{})
	require.NoError(t, err)
	require.Len(t, result.Domains, 1)
	require.Equal(t, "docs.example.com", result.Domains[0].Domain)
}

func TestListDomains_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListDomains(ctx, &gen.ListDomainsPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
