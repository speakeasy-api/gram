package mcp_test

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

// idJAGExchangeCounts reads oauth.id_jag.exchange as outcome/reason -> count,
// asserting every point names issuerID.
func idJAGExchangeCounts(t *testing.T, reader *sdkmetric.ManualReader, issuerID uuid.UUID) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	counts := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != mcpmetrics.InstrumentOAuthIDJAGExchange {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				issuer, _ := point.Attributes.Value(attr.UserSessionIssuerIDKey)
				require.Equal(t, issuerID.String(), issuer.AsString())
				outcome, _ := point.Attributes.Value(attr.OAuthIDJAGOutcomeKey)
				reason, _ := point.Attributes.Value(attr.OAuthIDJAGRejectionReasonKey)
				counts[outcome.AsString()+"/"+reason.AsString()] += point.Value
			}
		}
	}
	return counts
}

func TestTokenIDJAGExchangeRecordsOutcomeAndRejectionReason(t *testing.T) {
	t.Parallel()

	assertionSigner := newIDJAGTestAssertion(t)
	jwksServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(assertionSigner.jwks); err != nil {
			t.Errorf("write JWKS response: %v", err)
		}
	}))
	t.Cleanup(jwksServer.Close)
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(jwksServer.Certificate())
	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), guardian.WithTLSRootCAs(rootCAs))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	const upstreamIssuer = "https://identity.example.test"
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, upstreamIssuer, jwksServer.URL)
	toolset, err := toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	client := createIDJAGClient(t, ctx, ti, issuer.ID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), toolset.ID)
	resource, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	// No directory user yet: the validator refuses the subject.
	unprovisioned := assertionSigner.sign(t, upstreamIssuer, resource, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	w := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, unprovisioned)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	wrongAudience := assertionSigner.sign(t, upstreamIssuer, "https://elsewhere.example.test", resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	w = postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, wrongAudience)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	accepted := assertionSigner.sign(t, upstreamIssuer, resource, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	w = postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, accepted)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Equal(t, map[string]int64{
		string(mcpmetrics.IDJAGExchangeRejected) + "/" + string(idjag.ReasonNotProvisioned):   1,
		string(mcpmetrics.IDJAGExchangeRejected) + "/" + string(idjag.ReasonAudienceMismatch): 1,
		string(mcpmetrics.IDJAGExchangeAccepted) + "/":                                        1,
	}, idJAGExchangeCounts(t, reader, issuer.ID))
}
