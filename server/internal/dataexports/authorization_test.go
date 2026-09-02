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
	_, err := ti.service.ListDestinations(noGrants, &gen.ListDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListRoutes(noGrants, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	projectRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.ListDestinations(projectRead, &gen.ListDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListRoutes(projectRead, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	orgRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.ListDestinations(orgRead, &gen.ListDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.ListRoutes(orgRead, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
}

func TestDestinationAndRouteMutationsRequireOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectWrite := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	_, err := ti.service.CreateDestination(projectWrite, &gen.CreateDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DestinationType: "otel", Name: "Denied destination", Otel: &gen.CreateOtelDestinationInput{EndpointURL: "https://denied.example.test", Headers: []*gen.CreateOtelDestinationHeaderInput{}}, SensitiveData: "exclude"})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateRoute(projectWrite, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	orgRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.CreateDestination(orgRead, &gen.CreateDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DestinationType: "otel", Name: "Org reader destination", Otel: &gen.CreateOtelDestinationInput{EndpointURL: "https://org-reader-denied.example.test", Headers: []*gen.CreateOtelDestinationHeaderInput{}}, SensitiveData: "exclude"})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateRoute(orgRead, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	orgAdmin := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
	_, err = ti.service.CreateDestination(orgAdmin, &gen.CreateDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DestinationType: "otel", Name: "Allowed destination", Otel: &gen.CreateOtelDestinationInput{EndpointURL: "https://allowed.example.test", Headers: []*gen.CreateOtelDestinationHeaderInput{}}, SensitiveData: "exclude"})
	require.NoError(t, err)
	_, err = ti.service.CreateRoute(orgAdmin, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)
}
