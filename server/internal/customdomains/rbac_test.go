package customdomains_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCustomDomainsService_GetDomain_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetDomain(ctx, &gen.GetDomainPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCustomDomainsService_GetDomain_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: authCtx.ActiveOrganizationID, Domain: "docs.example.com"})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgRead, Resource: authCtx.ActiveOrganizationID})

	domain, err := ti.service.GetDomain(ctx, &gen.GetDomainPayload{})
	require.NoError(t, err)
	require.Equal(t, "docs.example.com", domain.Domain)
}

func TestCustomDomainsService_CreateDomain_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	err := ti.service.CreateDomain(ctx, &gen.CreateDomainPayload{Domain: "create.example.com"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCustomDomainsService_CreateDomain_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

	err := ti.service.CreateDomain(ctx, &gen.CreateDomainPayload{Domain: "create.example.com"})
	require.NoError(t, err)
	require.Equal(t, 1, ti.temporal.registrationCalls)
	require.Equal(t, "create.example.com", ti.temporal.lastDomain)
}

func TestCustomDomainsService_DeleteDomain_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: authCtx.ActiveOrganizationID, Domain: "delete.example.com"})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn)
	err = ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCustomDomainsService_DeleteDomain_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	_, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{OrganizationID: authCtx.ActiveOrganizationID, Domain: "delete.example.com"})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})
	err = ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{})
	require.NoError(t, err)
}

func TestCustomDomainsService_GetDomain_UnauthorizedWithEmptyOrgID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	authCtx.ActiveOrganizationID = ""
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.GetDomain(ctx, &gen.GetDomainPayload{})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestCustomDomainsService_CreateDomain_UnauthorizedWithEmptyOrgID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	authCtx.ActiveOrganizationID = ""
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	err := ti.service.CreateDomain(ctx, &gen.CreateDomainPayload{Domain: "create.example.com"})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestCustomDomainsService_DeleteDomain_UnauthorizedWithEmptyOrgID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	authCtx.ActiveOrganizationID = ""
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	err := ti.service.DeleteDomain(ctx, &gen.DeleteDomainPayload{})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func testAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}
