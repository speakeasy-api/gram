package dataexports_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDestinationAndRouteReadsRequireProjectRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	noGrants := withExactAccessGrants(t, ctx, ti.conn)
	_, err := ti.service.ListOtelDestinations(noGrants, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListRoutes(noGrants, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	projectRead := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.ListOtelDestinations(projectRead, &gen.ListOtelDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.ListRoutes(projectRead, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
}

func TestDestinationAndRouteMutationsRequireProjectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createOtelDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectRead := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err := ti.service.CreateOtelDestination(projectRead, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://denied.example.test", SensitiveData: "exclude", Headers: []*gen.OtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateRoute(projectRead, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "otel_logs", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	orgAdmin := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
	_, err = ti.service.CreateOtelDestination(orgAdmin, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://org-admin-denied.example.test", SensitiveData: "exclude", Headers: []*gen.OtelDestinationHeaderInput{}})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateRoute(orgAdmin, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "otel_logs", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	projectWrite := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	_, err = ti.service.CreateOtelDestination(projectWrite, &gen.CreateOtelDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		EndpointURL: "https://allowed.example.test", SensitiveData: "exclude", Headers: []*gen.OtelDestinationHeaderInput{}})
	require.NoError(t, err)
	_, err = ti.service.CreateRoute(projectWrite, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "otel_logs", Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)
}
