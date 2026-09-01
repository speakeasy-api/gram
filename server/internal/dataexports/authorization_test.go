package dataexports_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDestinationAndRouteReadsRequireOrgRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	noGrants := authztest.WithExactGrants(t, ctx)
	_, err := ti.service.ListOtelDestinations(noGrants, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListRoutes(noGrants, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	projectRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.ListOtelDestinations(projectRead, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListRoutes(projectRead, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	orgRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.ListOtelDestinations(orgRead, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.ListRoutes(orgRead, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
}

func TestDestinationAndRouteMutationsRequireOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createOtelDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectWrite := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	_, err := ti.service.CreateOtelDestination(projectWrite, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Name: "Denied destination", EndpointURL: "https://denied.example.test", SensitiveData: "exclude", Headers: []*gen.CreateOtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateRoute(projectWrite, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	orgRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.CreateOtelDestination(orgRead, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Name: "Org reader destination", EndpointURL: "https://org-reader-denied.example.test", SensitiveData: "exclude", Headers: []*gen.CreateOtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateRoute(orgRead, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	orgAdmin := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
	_, err = ti.service.CreateOtelDestination(orgAdmin, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Name: "Allowed destination", EndpointURL: "https://allowed.example.test", SensitiveData: "exclude", Headers: []*gen.CreateOtelDestinationHeaderInput{}})
	require.NoError(t, err)
	_, err = ti.service.CreateRoute(orgAdmin, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)
}
