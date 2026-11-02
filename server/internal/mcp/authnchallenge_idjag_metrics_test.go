package mcp_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
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
	toolset = setToolsetIssuer(t, ctx, ti.conn, *authCtx.ProjectID, toolset, issuer.ID)
	client := seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)
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

	choice := assertionSigner.sign(t, upstreamIssuer, resource, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	form := url.Values{
		"grant_type":           {oauthwire.GrantTypeJWTBearer},
		"client_id":            {client.ClientID},
		"assertion":            {choice},
		"upstream_auth_client": {uuid.NewString() + ":none"},
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	var oauthErr *oops.OAuthError
	require.ErrorAs(t, ti.service.HandleToken(httptest.NewRecorder(), req), &oauthErr)
	require.Equal(t, http.StatusBadRequest, oauthErr.Status)

	require.Equal(t, map[string]int64{
		string(mcpmetrics.IDJAGExchangeRejected) + "/" + string(idjag.ReasonNotProvisioned):                        1,
		string(mcpmetrics.IDJAGExchangeRejected) + "/" + string(idjag.ReasonAudienceMismatch):                      1,
		string(mcpmetrics.IDJAGExchangeAccepted) + "/":                                                             1,
		string(mcpmetrics.IDJAGExchangeRejected) + "/" + string(mcpmetrics.IDJAGRejectionUpstreamCredentialChoice): 1,
	}, idJAGExchangeCounts(t, reader, issuer.ID))
}

// idJAGOutageFixture is a toolset an app exchanges ID-JAGs for, with a key set
// that can be taken down.
type idJAGOutageFixture struct {
	ti       *testInstance
	slug     string
	clientID string
	issuerID uuid.UUID
	resource string
	jwksURL  string
	signer   idJAGTestAssertion
	down     *atomic.Bool
	fetches  *atomic.Int32
	reader   *sdkmetric.ManualReader
}

const idJAGOutageIssuer = "https://identity.example.test"

func newIDJAGOutageFixture(t *testing.T, granted bool) (context.Context, idJAGOutageFixture) {
	t.Helper()

	signer := newIDJAGTestAssertion(t)
	down, fetches := &atomic.Bool{}, &atomic.Int32{}
	jwksServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(signer.jwks)
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
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, idJAGOutageIssuer, jwksServer.URL)
	toolset = setToolsetIssuer(t, ctx, ti.conn, *authCtx.ProjectID, toolset, issuer.ID)
	client := seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)
	seedIDJAGDirectoryUser(t, ctx, ti, authCtx.ActiveOrganizationID)
	if granted {
		seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID), toolset.ID)
	}
	resource, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	return ctx, idJAGOutageFixture{ti: ti, slug: toolset.McpSlug.String, clientID: client.ClientID, issuerID: issuer.ID, resource: resource, jwksURL: jwksServer.URL, signer: signer, down: down, fetches: fetches, reader: reader}
}

func (f idJAGOutageFixture) assertion(t *testing.T) string {
	t.Helper()
	return f.signer.sign(t, idJAGOutageIssuer, f.resource, f.resource, f.clientID, mockidp.MockUserEmail, uuid.NewString())
}

func (f idJAGOutageFixture) post(t *testing.T, ctx context.Context, assertion string) *httptest.ResponseRecorder {
	t.Helper()
	return postIDJAGTokenHandled(t, ctx, f.ti, f.slug, f.clientID, assertion)
}

func requireIDJAGOutage(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Contains(t, w.Body.String(), "temporarily_unavailable")
	require.NotContains(t, w.Body.String(), "invalid_grant")
}

// A key outage leaves the assertion unjudged and unspent, and the failed fetch
// is held back for a new issuer too.
func TestTokenIDJAGExchangeRetriesSameAssertionAfterKeyOutage(t *testing.T) {
	t.Parallel()

	ctx, f := newIDJAGOutageFixture(t, true)
	f.down.Store(true)
	assertion := f.assertion(t)

	first := f.post(t, ctx, assertion)
	requireIDJAGOutage(t, first)
	require.Equal(t, "1", first.Header().Get(constants.HeaderProxiedResponse), "the customer's key set being down doesn't alert on-call")
	require.EqualValues(t, 1, f.fetches.Load())

	f.down.Store(false)
	requireIDJAGOutage(t, f.post(t, ctx, assertion))
	require.EqualValues(t, 1, f.fetches.Load(), "the failed fetch is held back")

	rows, err := testrepo.New(f.ti.conn).ClearRemoteSessionIssuerJWKSConsultFailureFixture(ctx, conv.ToPGText(f.jwksURL))
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	retried := f.post(t, ctx, assertion)
	require.Equal(t, http.StatusOK, retried.Code, "the retry is not refused as a replay: %s", retried.Body.String())

	replayed := f.post(t, ctx, assertion)
	require.Equal(t, http.StatusBadRequest, replayed.Code, replayed.Body.String())
	require.Contains(t, replayed.Body.String(), "invalid_grant")
}

// A forged token can't turn a permanent key failure into a retry, or learn
// from the status whether its issuer and key id matched.
func TestTokenIDJAGExchangeForgedAlgorithmStaysInvalidGrant(t *testing.T) {
	t.Parallel()

	ctx, f := newIDJAGOutageFixture(t, true)
	forger, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES384, Key: forger},
		(&jose.SignerOptions{}).WithType(jose.ContentType(idjag.Type)).WithHeader(jose.HeaderKey("kid"), "id-jag-test-key"),
	)
	require.NoError(t, err)
	forged := idJAGTestAssertion{signer: signer, jwks: nil}.sign(t, idJAGOutageIssuer, f.resource, f.resource, f.clientID, mockidp.MockUserEmail, uuid.NewString())

	w := f.post(t, ctx, forged)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"assertion is invalid"}`, w.Body.String())
	require.Empty(t, w.Header().Get("Retry-After"))
	require.Equal(t, map[string]int64{
		string(mcpmetrics.IDJAGExchangeRejected) + "/" + string(idjag.ReasonKeyUnusable): 1,
	}, idJAGExchangeCounts(t, f.reader, f.issuerID))
}

// failingAuthzEngine reads a closed database, so it reaches no decision.
func failingAuthzEngine(t *testing.T) *authz.Engine {
	t.Helper()
	closed, err := infra.CloneTestDatabase(t, "idjag_outage")
	require.NoError(t, err)
	closed.Close()
	return authz.NewEngine(testenv.NewLogger(t), closed, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{})
}

// A failure past the reservation delivers no token, so it gives the identifier
// back. Ours, so it stays counted.
func TestTokenIDJAGExchangeReleasesAssertionAfterLaterOutage(t *testing.T) {
	t.Parallel()

	_, f := newIDJAGOutageFixture(t, true)
	// No caller grants on the request, as in production, so the engine is consulted.
	ctx := t.Context()
	assertion := f.assertion(t)
	working := f.ti.authzEngine
	f.ti.service.SetAuthzEngine(failingAuthzEngine(t))

	w := f.post(t, ctx, assertion)
	requireIDJAGOutage(t, w)
	require.Empty(t, w.Header().Get(constants.HeaderProxiedResponse), "our own dependency stays counted")

	f.ti.service.SetAuthzEngine(working)
	retried := f.post(t, ctx, assertion)
	require.Equal(t, http.StatusOK, retried.Code, retried.Body.String())
}

// Refusing the subject is a verdict, so the assertion stays spent.
func TestTokenIDJAGExchangeForbiddenSubjectStaysSpent(t *testing.T) {
	t.Parallel()

	ctx, f := newIDJAGOutageFixture(t, false)
	ctx = authztest.WithExactGrants(t, ctx)
	assertion := f.assertion(t)

	require.Equal(t, http.StatusBadRequest, f.post(t, ctx, assertion).Code)
	require.Equal(t, http.StatusBadRequest, f.post(t, ctx, assertion).Code)
	require.EqualValues(t, 1, idJAGExchangeCounts(t, f.reader, f.issuerID)[string(mcpmetrics.IDJAGExchangeRejected)+"/"+string(idjag.ReasonReplayed)])
}

func TestTokenIDJAGExchangeRecordsClientNotAllowed(t *testing.T) {
	t.Parallel()

	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false, ReplaceAttr: nil}))
	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, logger, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil)
	authCtx := requireProjectAuthContext(t, ctx)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, "https://identity.example.test", "https://identity.example.test/keys")
	toolset = setToolsetIssuer(t, ctx, ti.conn, *authCtx.ProjectID, toolset, issuer.ID)
	client := createDCRClient(t, ctx, ti, issuer.ID)

	w := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, client.ClientID, "unused.assertion.value")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var rejected []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(logs.String()))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		if line["msg"] == "oauth ID-JAG token request rejected" {
			rejected = append(rejected, line)
		}
	}
	require.Len(t, rejected, 1)
	reason := string(mcpmetrics.IDJAGRejectionClientNotCIMD)
	require.Equal(t, reason, rejected[0][string(attr.OAuthIDJAGRejectionReasonKey)])
	require.Equal(t, issuer.ID.String(), rejected[0][string(attr.UserSessionIssuerIDKey)])

	require.Equal(t, map[string]int64{string(mcpmetrics.IDJAGExchangeRejected) + "/" + reason: 1}, idJAGExchangeCounts(t, reader, issuer.ID))
}

// Client refusals on the ID-JAG grant are each one ID-JAG line and count.
func TestTokenIDJAGExchangeRecordsClientRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mode          admission.Mode
		clientID      func(ds *cimdDocServer) string
		failFetch     bool
		block         string
		authorization string
		outcome       mcpmetrics.IDJAGExchangeOutcome
		reason        mcpmetrics.IDJAGRejectionReason
		message       string
		status        int
	}{
		"presets refuses an unlisted document": {mode: admission.ModePresets, clientID: func(ds *cimdDocServer) string { return ds.clientID }, outcome: mcpmetrics.IDJAGExchangeRejected, reason: mcpmetrics.IDJAGRejectionClientNotAdmitted, message: "oauth ID-JAG token request rejected", status: http.StatusBadRequest},
		"disabled refuses every document":      {mode: admission.ModeDisabled, clientID: func(*cimdDocServer) string { return codexCIMDClientID }, outcome: mcpmetrics.IDJAGExchangeRejected, reason: mcpmetrics.IDJAGRejectionClientNotAdmitted, message: "oauth ID-JAG token request rejected", status: http.StatusBadRequest},
		"unknown client":                       {mode: admission.ModeOpen, clientID: func(*cimdDocServer) string { return "unknown-client" }, outcome: mcpmetrics.IDJAGExchangeRejected, reason: mcpmetrics.IDJAGRejectionClientInvalid, message: "oauth ID-JAG token request rejected", status: http.StatusBadRequest},
		"document fetch fails":                 {mode: admission.ModeOpen, clientID: func(ds *cimdDocServer) string { return ds.clientID }, failFetch: true, outcome: mcpmetrics.IDJAGExchangeError, reason: mcpmetrics.IDJAGRejectionClientMetadataUnavailable, message: "oauth ID-JAG token request failed", status: http.StatusServiceUnavailable},
		"blocked AI tool":                      {mode: admission.ModeOpen, clientID: func(*cimdDocServer) string { return codexCIMDClientID }, block: "codex", outcome: mcpmetrics.IDJAGExchangeRejected, reason: mcpmetrics.IDJAGRejectionClientBlocked, message: "oauth ID-JAG token request rejected", status: http.StatusBadRequest},
		"malformed client authentication":      {mode: admission.ModeOpen, clientID: func(*cimdDocServer) string { return codexCIMDClientID }, authorization: "Bearer not-a-client", outcome: mcpmetrics.IDJAGExchangeRejected, reason: mcpmetrics.IDJAGRejectionClientInvalid, message: "oauth ID-JAG token request rejected", status: http.StatusUnauthorized},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := startCIMDDocServer(t)
			if tc.failFetch {
				ds.set(t, func(ds *cimdDocServer) { ds.status = http.StatusInternalServerError })
			}
			var logs syncBuffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false, ReplaceAttr: nil}))
			reader := sdkmetric.NewManualReader()
			ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, logger, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
				SessionTTL:         0,
				LiveSessionCap:     0,
				InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
				RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
				MaxRequestLifetime: 0,
			}, nil, guardian.WithTLSRootCAs(ds.certPool()))
			authCtx := requireProjectAuthContext(t, ctx)
			toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, "https://identity.example.test", "https://identity.example.test/keys")
			toolset = setToolsetIssuer(t, ctx, ti.conn, *authCtx.ProjectID, toolset, issuer.ID)
			seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)
			setOrganizationIssuerAdmissionMode(t, ctx, ti, issuer, tc.mode)
			if tc.block != "" {
				blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, tc.block)
			}

			w := postIDJAGTokenWithAuthorization(t, ctx, ti, toolset.McpSlug.String, tc.clientID(ds), "unused.assertion.value", tc.authorization)
			require.Equal(t, tc.status, w.Code, w.Body.String())

			var lines []map[string]any
			scanner := bufio.NewScanner(strings.NewReader(logs.String()))
			for scanner.Scan() {
				var line map[string]any
				require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
				require.NotEqual(t, "oauth token client authentication rejected", line["msg"], "no second, generic line")
				require.NotEqual(t, "cimd document fetch failed", line["msg"], "no second, generic line")
				if msg, _ := line["msg"].(string); strings.HasPrefix(msg, "oauth ID-JAG token request") {
					lines = append(lines, line)
				}
			}
			require.Len(t, lines, 1)
			require.Equal(t, tc.message, lines[0]["msg"])
			require.Equal(t, string(tc.outcome), lines[0][string(attr.OAuthIDJAGOutcomeKey)])
			require.Equal(t, string(tc.reason), lines[0][string(attr.OAuthIDJAGRejectionReasonKey)])
			require.Equal(t, issuer.ID.String(), lines[0][string(attr.UserSessionIssuerIDKey)])
			require.Equal(t, map[string]int64{string(tc.outcome) + "/" + string(tc.reason): 1}, idJAGExchangeCounts(t, reader, issuer.ID))
		})
	}
}

func TestTokenIDJAGExchangeRecordsInfrastructureRefusalAsError(t *testing.T) {
	t.Parallel()

	assertionSigner := newIDJAGTestAssertion(t)
	jwksServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
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
	toolset = setToolsetIssuer(t, ctx, ti.conn, *authCtx.ProjectID, toolset, issuer.ID)
	client := seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)
	resource, _ := fetchAdvertisedIssuer(t, ctx, ti, toolset.McpSlug.String)

	assertion := assertionSigner.sign(t, upstreamIssuer, resource, resource, client.ClientID, mockidp.MockUserEmail, uuid.NewString())
	w := postIDJAGTokenHandled(t, ctx, ti, toolset.McpSlug.String, client.ClientID, assertion)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "invalid_grant")

	require.Equal(t, map[string]int64{
		string(mcpmetrics.IDJAGExchangeError) + "/" + string(idjag.ReasonKeyUnresolvable): 1,
	}, idJAGExchangeCounts(t, reader, issuer.ID))
}
