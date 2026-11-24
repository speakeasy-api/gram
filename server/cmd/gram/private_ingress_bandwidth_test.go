package gram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const privateBandwidthTestHost = "private-bandwidth.example.ts.net"

// admittingWorkloadVerifier admits every request as the configured ingress.
type admittingWorkloadVerifier struct {
	ingress netingress.Ingress
}

func (v admittingWorkloadVerifier) Verify(context.Context, string, string, string) (netingress.Ingress, error) {
	return v.ingress, nil
}

func newPrivateBandwidthTestHandler(t *testing.T, publisher *capturingMeterPublisher, ingress netingress.Ingress, runtime http.HandlerFunc) goahttp.Muxer {
	t.Helper()

	logger := testenv.NewLogger(t)
	telemetry := netingress.NewTelemetry(logger, testenv.NewMeterProvider(t))
	return newPrivateIngressHandler(logger, telemetry, admittingWorkloadVerifier{ingress: ingress}, publisher, func(mux goahttp.Muxer) {
		mux.Handle(http.MethodPost, "/mcp/{mcpSlug}", runtime)
	})
}

func newPrivateBandwidthTestRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "https://"+privateBandwidthTestHost+"/mcp/private-bandwidth", strings.NewReader(body))
	request.Header.Set(netingress.AttestationHeader, "Bearer gateway-token")
	return request
}

func TestPrivateIngressMetersAttributedMCPExchanges(t *testing.T) {
	t.Parallel()

	ingress := netingress.Ingress{
		ID: uuid.New(), OrganizationID: "org-private", Provider: netingress.ProviderTailscale,
		DNSName: privateBandwidthTestHost, IdentityRequired: false,
	}
	projectID := uuid.New()
	publisher := &capturingMeterPublisher{}
	var originSeen bool
	handler := newPrivateBandwidthTestHandler(t, publisher, ingress, func(w http.ResponseWriter, r *http.Request) {
		origin, ok := requestorigin.FromContext(r.Context())
		originSeen = ok
		metering.AttributeMCPBandwidth(r.Context(), origin.OrganizationID, projectID)
		metering.AttributeMCPBandwidthServer(r.Context(), metering.MCPServerTypeHosted, "server-id", "private-bandwidth")
		_, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, "private-response")
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, newPrivateBandwidthTestRequest("private-request"))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.True(t, originSeen, "the meter must sit below the private admission middleware")
	messages := publisher.published()
	require.Len(t, messages, 2)
	require.Equal(t, string(metering.MeterMCPBandwidthIngress), messages[0].GetMeterId())
	require.Equal(t, int64(len("private-request")), messages[0].GetValue())
	require.Equal(t, string(metering.MeterMCPBandwidthEgress), messages[1].GetMeterId())
	require.Equal(t, int64(len("private-response")), messages[1].GetValue())
	for _, message := range messages {
		require.Equal(t, "org-private", message.GetOrganizationId())
		require.Equal(t, projectID.String(), message.GetProjectId())
		require.Equal(t, ingress.ID.String(), message.GetAttributes()[metering.AttributeNetworkIngressID])
		require.Equal(t, string(metering.MCPServerTypeHosted), message.GetAttributes()[metering.AttributeMCPServerType])
	}
}

func TestPrivateIngressDoesNotMeterRejectedAdmissions(t *testing.T) {
	t.Parallel()

	ingress := netingress.Ingress{
		ID: uuid.New(), OrganizationID: "org-private", Provider: netingress.ProviderTailscale,
		DNSName: privateBandwidthTestHost, IdentityRequired: false,
	}
	publisher := &capturingMeterPublisher{}
	handler := newPrivateBandwidthTestHandler(t, publisher, ingress, func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-private", uuid.New())
		_, _ = io.WriteString(w, "unreachable")
	})
	request := newPrivateBandwidthTestRequest("private-request")
	request.Header.Del(netingress.AttestationHeader)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Empty(t, publisher.published())
}

func TestPrivateIngressDoesNotMeterPanickedExchanges(t *testing.T) {
	t.Parallel()

	ingress := netingress.Ingress{
		ID: uuid.New(), OrganizationID: "org-private", Provider: netingress.ProviderTailscale,
		DNSName: privateBandwidthTestHost, IdentityRequired: false,
	}
	publisher := &capturingMeterPublisher{}
	handler := newPrivateBandwidthTestHandler(t, publisher, ingress, func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-private", uuid.New())
		_, _ = io.ReadAll(r.Body)
		panic("runtime handler panicked")
	})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, newPrivateBandwidthTestRequest("private-request"))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Empty(t, publisher.published())
}
