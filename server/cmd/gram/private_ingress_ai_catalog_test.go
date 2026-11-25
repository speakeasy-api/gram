package gram

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestPrivateIngressPinsOneAICatalogSnapshotPerRequest(t *testing.T) {
	t.Parallel()

	ingress := netingress.Ingress{
		ID: uuid.New(), OrganizationID: "org-private", Provider: netingress.ProviderTailscale,
		DNSName: privateBandwidthTestHost, IdentityRequired: false,
	}
	store := aivendorstest.NewStore()
	products := aivendors.Products()
	products[0].DisplayName = "Swapped"
	next := aivendors.NewSnapshot(products)
	before := store.Snapshot()

	var first, second *aivendors.Snapshot
	logger := testenv.NewLogger(t)
	telemetry := netingress.NewTelemetry(logger, testenv.NewMeterProvider(t))
	handler := newPrivateIngressHandler(logger, telemetry, admittingWorkloadVerifier{ingress: ingress}, &capturingMeterPublisher{}, store, func(mux goahttp.Muxer) {
		mux.Handle(http.MethodPost, "/mcp/{mcpSlug}", func(w http.ResponseWriter, r *http.Request) {
			first = store.SnapshotFor(r.Context())
			store.Publish(next)
			second = store.SnapshotFor(r.Context())
			w.WriteHeader(http.StatusNoContent)
		})
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, newPrivateBandwidthTestRequest("private-request"))

	require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
	require.Same(t, before, first, "a request starts on the snapshot published before it")
	require.Same(t, first, second)
	require.Same(t, next, store.Snapshot())
	require.NotSame(t, store.Snapshot(), second)
}
