package networkingress_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestExpansionAdmissionRuntimeDisabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	admission := networkingress.NewExpansionAdmission(ti.features, ti.flags, orgrepo.New(ti.conn), true, false)
	require.ErrorContains(t, admission.CheckExpansion(ctx, ti.orgID), "network ingress is disabled")
	_, err := admission.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModeDual})
	require.Error(t, err)
	finalize, err := admission.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModePublicOnly})
	require.NoError(t, err)
	require.NoError(t, finalize.Finalize(ctx, nil))
}

func TestDisabledRuntimePreservesContainment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceWithRuntime(t, false)
	require.NoError(t, testrepo.New(ti.conn).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: uuid.New(), OrganizationID: ti.orgID, DnsName: pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	enabled := true
	_, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Enabled: &enabled})
	requireOopsCode(t, err, oops.CodeForbidden)
	enabled = false
	_, err = ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Enabled: &enabled})
	require.NoError(t, err)
	require.False(t, loadRow(t, ctx, ti).Enabled)
	require.NoError(t, ti.service.DeleteIngress(ctx, &gen.DeleteIngressPayload{}))
}

func TestDisabledMountPreservesObservationAndHidesExpansionRoutes(t *testing.T) {
	t.Parallel()
	_, ti := newTestServiceWithRuntime(t, false)
	mux := goahttp.NewMuxer()
	networkingress.Attach(mux, ti.service, false)

	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/rpc/networkIngress.get"},
		{http.MethodGet, "/rpc/networkIngress.getDeleteImpact"},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		require.NotEqual(t, http.StatusNotFound, response.Code, endpoint.path)
	}

	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, "/rpc/networkIngress.create"},
		{http.MethodPost, "/rpc/networkIngress.rotateCredentials"},
		{http.MethodPost, "/rpc/networkIngress.checkHealth"},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		require.Equal(t, http.StatusNotFound, response.Code, endpoint.path)
	}
}
