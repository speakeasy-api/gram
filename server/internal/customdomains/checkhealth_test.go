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

func TestCheckHealth_StartsWorkflowForOrganizationDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	authCtx := testAuthContext(t, ctx)
	domain, err := ti.repo.CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "health-check.example.com",
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	result, err := ti.service.CheckHealth(ctx, &gen.CheckHealthPayload{})
	require.NoError(t, err)
	require.Equal(t, domain.ID.String(), result.ID)
	require.Equal(t, 1, ti.temporal.healthCheckCalls)
	require.Equal(t, authCtx.ActiveOrganizationID, ti.temporal.lastOrganization)
	require.Equal(t, domain.ID, ti.temporal.lastHealthCheckID)
}

func TestCheckHealth_RequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCustomDomainsService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.CheckHealth(ctx, &gen.CheckHealthPayload{})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
	require.Zero(t, ti.temporal.healthCheckCalls)
}
