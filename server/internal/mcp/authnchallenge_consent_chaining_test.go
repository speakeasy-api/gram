package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
)

const (
	consentChainedUpstream = "https://chained-upstream.example.com/mcp"
	consentManagedCopy     = "Managed by your identity provider"
)

// consentGovernor answers Serves with issuer for every upstream in served,
// Governs with governs, and fails any acquisition.
type consentGovernor struct {
	t          *testing.T
	issuer     uuid.UUID
	served     map[string]bool
	governs    bool
	configured bool
	mu         sync.Mutex
	requests   []identitychaining.Request
}

func (g *consentGovernor) Governs(context.Context, identitychaining.Request) bool {
	return g.governs
}

func (g *consentGovernor) Serves(_ context.Context, req identitychaining.Request) (uuid.UUID, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, req)
	return g.issuer, g.served[req.UpstreamResource]
}

func (g *consentGovernor) Configured(context.Context, string, uuid.UUID, uuid.UUID) bool {
	return g.configured
}

func (g *consentGovernor) Acquire(context.Context, identitychaining.Request) (identitychaining.Token, identitychaining.Outcome) {
	g.t.Error("consent must never acquire a chained token")
	return identitychaining.Token{}, identitychaining.Outcome{}
}

func (g *consentGovernor) seen() []identitychaining.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]identitychaining.Request(nil), g.requests...)
}

func setConsentGovernor(t *testing.T, fx consentActionFixture, issuer uuid.UUID, served ...string) *consentGovernor {
	t.Helper()
	g := &consentGovernor{t: t, issuer: issuer, served: map[string]bool{}, governs: len(served) > 0, configured: true, mu: sync.Mutex{}, requests: nil}
	for _, u := range served {
		g.served[u] = true
	}
	fx.ti.service.SetIdentityChainer(g)
	return g
}

// seedChainedConsentEndpoint: a remote-backed endpoint with one bound client.
func seedChainedConsentEndpoint(t *testing.T, tag string) (context.Context, consentActionFixture, uuid.UUID) {
	t.Helper()

	ctx, fx := standaloneConsent(t, tag, consentChainedUpstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		return attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerID, slug, consentChainedUpstream)
	})
	fx.clientA = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag, "", []uuid.UUID{fx.shared})
	return ctx, fx, clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, fx.clientA)
}

func TestConsentPage_IdentityChainedServiceIsManagedWithFallback(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim61-chained-page")
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	code, page, loc := render(t, fx)
	require.Equal(t, http.StatusOK, code, "a governed sole service must not auto-connect")
	require.Nil(t, loc)
	require.Contains(t, page, consentManagedCopy)
	require.Contains(t, page, "0 of 1 connected")
	require.Contains(t, page, "data-connect-fallback")
	require.Contains(t, page, "Connect manually")
	require.Contains(t, page, `data-consent-enabled="true"`)
	require.NotContains(t, page, "Connect a service above to enable access.")

	reqs := g.seen()
	require.Len(t, reqs, 1)
	require.Equal(t, identitychaining.Request{
		OrganizationID:        fx.orgID,
		ProjectID:             fx.projectID,
		UserSessionIssuerID:   fx.shared,
		UserID:                fx.subject.ID,
		UpstreamResource:      consentChainedUpstream,
		RemoteSessionIssuerID: uuid.NullUUID{},
	}, reqs[0], "a direct upstream is judged with the runtime's issuer-free request")
}

func TestConsentPage_UngovernedServiceStillConnectsInteractively(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim61-unchained-page")
	setConsentGovernor(t, fx, issuer)

	code, _, loc := render(t, fx)
	require.Equal(t, http.StatusSeeOther, code)
	require.NotNil(t, loc)
	require.Equal(t, "aim61-unchained-page-as.example.com", loc.Host)
}

func TestConsentPage_AmbiguousBindingsAreNotChained(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim61-ambiguous-page")
	g := setConsentGovernor(t, fx, issuer)
	// The governor's answer for ambiguous bindings: the runtime claims the upstream, but no single binding serves it.
	g.governs = true

	code, page, loc := render(t, fx)
	require.Equal(t, http.StatusSeeOther, code, "configuration required is not presented as managed")
	require.NotNil(t, loc)
	require.NotContains(t, page, consentManagedCopy)
}

func TestConsentPage_UnroutableGrantIsNotChained(t *testing.T) {
	t.Parallel()

	ctx, fx, issuer := seedChainedConsentEndpoint(t, "aim61-unroutable-page")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	grant(t, ctx, fx, fx.clientA, "https://elsewhere.example.com/mcp")

	expectAutoReconnect(t, fx, consentChainedUpstream)
	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, page)
	require.Contains(t, page, consentReconnectCopy)
	require.NotContains(t, page, consentManagedCopy)
}

func TestConsentPage_ChainingLookupFaultDegrades(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim61-fault-page")
	g := setConsentGovernor(t, fx, issuer, consentChainedUpstream)
	fx.endpoint.McpServerID = conv.ToNullUUID(uuid.New())

	code, page, _ := render(t, fx)
	require.NotEqual(t, http.StatusInternalServerError, code)
	require.NotContains(t, page, consentManagedCopy)
	require.Empty(t, g.seen())
}

func postChainedConnect(t *testing.T, fx consentActionFixture) *url.URL {
	t.Helper()

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", fx.clientA.String())
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return loc
}

func TestServeConsentAction_ConnectGovernedServiceStillAuthorizes(t *testing.T) {
	t.Parallel()

	_, fx, issuer := seedChainedConsentEndpoint(t, "aim61-chained-action")
	setConsentGovernor(t, fx, issuer, consentChainedUpstream)

	loc := postChainedConnect(t, fx)
	require.Equal(t, "aim61-chained-action-as.example.com", loc.Host, "manual connect stays available as a fallback")
}

func TestConsentPage_DirectTunneledServiceIsManaged(t *testing.T) {
	t.Parallel()

	const identifier = "https://tunneled-chained.example.test/mcp"
	var serverID uuid.UUID
	ctx, fx := standaloneConsent(t, "aim480-tunnel-chained", identifier, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		serverID = createTunneledServer(t, ctx, ti, projectID, issuerID, slug, identifier)
		return serverID
	})
	fx.clientA = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim480-tunnel-chained", "", []uuid.UUID{fx.shared})
	issuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, fx.clientA)
	stampRemoteSessionIssuer(t, ctx, fx.ti.conn, fx.projectID, serverID, conv.ToNullUUID(issuer))
	g := setConsentGovernor(t, fx, issuer, identifier)

	code, page, loc := render(t, fx)
	require.Equal(t, http.StatusOK, code, "a governed sole tunneled service must not auto-connect")
	require.Nil(t, loc)
	require.Contains(t, page, consentManagedCopy)
	require.Contains(t, page, "Connect manually")

	reqs := g.seen()
	require.Len(t, reqs, 1)
	require.Equal(t, identitychaining.Request{
		OrganizationID:        fx.orgID,
		ProjectID:             fx.projectID,
		UserSessionIssuerID:   fx.shared,
		UserID:                fx.subject.ID,
		UpstreamResource:      identifier,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuer, Valid: true},
	}, reqs[0], "a tunneled upstream is judged with its own derived issuer")
}
