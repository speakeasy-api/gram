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

func TestListMcpEndpoints_NoCustomDomain_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{SessionToken: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestListMcpEndpoints_DomainWithNoEndpoints_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "no-endpoints.example.com",
		IngressName:    pgTextValid("ingress-noep"),
		CertSecretName: pgTextValid("cert-noep"),
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, result.McpEndpoints)
}

func TestListMcpEndpoints_ReturnsEndpointsAcrossProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	domainRow, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "list-cross.example.com",
		IngressName:    pgTextValid("ingress-list"),
		CertSecretName: pgTextValid("cert-list"),
	})
	require.NoError(t, err)

	// Two endpoints in caller's project + one in a separate project under
	// the same organization.
	seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domainRow.ID, "alpha")
	seedMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, domainRow.ID, "beta")

	otherProjectID := seedProject(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	seedMcpEndpoint(t, ctx, ti.conn, otherProjectID, domainRow.ID, "gamma")

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.McpEndpoints, 3)

	// Every returned row must carry project + mcp_server context. Slug + ids
	// are non-empty because the repo query JOINs against projects and
	// mcp_servers.
	for _, e := range result.McpEndpoints {
		require.NotEmpty(t, e.ID)
		require.NotEmpty(t, e.Slug)
		require.NotEmpty(t, e.ProjectID)
		require.NotEmpty(t, e.ProjectName)
		require.NotEmpty(t, e.ProjectSlug)
		require.NotEmpty(t, e.McpServerID)
	}
}

func TestListMcpEndpoints_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{SessionToken: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestListMcpEndpoints_ForbiddenWithOrgReadGrantOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.ListMcpEndpoints(ctx, &gen.ListMcpEndpointsPayload{SessionToken: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
