package mcp

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// funcGovernor answers Serves with the issuer serves returns and records
// every request; consent must never acquire.
type funcGovernor struct {
	configured bool
	serves     func(identitychaining.Request) (uuid.UUID, bool)
	mu         sync.Mutex
	requests   []identitychaining.Request
}

func (g *funcGovernor) Governs(context.Context, identitychaining.Request) bool {
	panic("consent judges with Serves, never Governs")
}

func (g *funcGovernor) Serves(_ context.Context, req identitychaining.Request) (uuid.UUID, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, req)
	return g.serves(req)
}

func (g *funcGovernor) Configured(context.Context, string, uuid.UUID, uuid.UUID) bool {
	return g.configured
}

func (g *funcGovernor) HasUsableCredential(context.Context, identitychaining.Request) bool {
	return false
}

func (g *funcGovernor) RefreshDecision(context.Context, identitychaining.Request) identitychaining.RefreshVerdict {
	return identitychaining.RefreshVerdict{Decision: identitychaining.RefreshNotChained, RemoteSessionIssuerID: uuid.Nil, Provider: "", Reason: ""}
}

func (g *funcGovernor) Acquire(context.Context, identitychaining.Request) (identitychaining.Token, identitychaining.Outcome) {
	panic("consent must never acquire a chained token")
}

func consentChainingFixture(t *testing.T, g *funcGovernor) (*Service, *ResolvedMcpEndpoint, AuthnChallengeState) {
	t.Helper()
	endpoint := &ResolvedMcpEndpoint{
		OrganizationID:      chainingTestOrg,
		ProjectID:           uuid.New(),
		UserSessionIssuerID: uuid.New(),
		McpServerID:         uuid.NullUUID{UUID: uuid.New(), Valid: true},
		UpstreamResource:    "https://upstream.example.test/mcp",
	}
	subject := urn.NewUserSubject("user-1")
	return &Service{
		aiCatalog:       aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),
		identityChainer: g, logger: testenv.NewLogger(t)}, endpoint, AuthnChallengeState{Subject: &subject}
}

func directRouting(backend consentBackend) consentRouting {
	return consentRouting{backend: backend, upstream: "", members: nil}
}

func TestConsentChainedClients_RequestMatchesRuntime(t *testing.T) {
	t.Parallel()
	tunnelIssuer := uuid.New()
	for _, tc := range []struct {
		name     string
		routing  consentRouting
		tunneled bool
	}{
		{"remote", directRouting(consentBackendRemote), false},
		{"tunneled", directRouting(consentBackendTunneled), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &funcGovernor{configured: true, serves: func(identitychaining.Request) (uuid.UUID, bool) { return tunnelIssuer, true }}
			s, endpoint, state := consentChainingFixture(t, g)
			client := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: tunnelIssuer}

			require.True(t, s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{client}, tc.routing)[client.ID])

			runtime, ok := s.identityChainingRequest(humanChainingContext(t, "user-1", chainingTestOrg), endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID, endpoint.UpstreamResource, tc.tunneled, uuid.NullUUID{UUID: tunnelIssuer, Valid: true}, uuid.NullUUID{UUID: client.ID, Valid: true})
			require.True(t, ok)
			require.NotEmpty(t, runtime.UserSessionJTI)
			runtime.UserSessionJTI = "" // Consent precedes grant creation and carries no validated grant identifier.
			require.Equal(t, []identitychaining.Request{runtime}, g.requests, "consent selects the same endpoint without asserting runtime SSO provenance")
		})
	}
}

func TestConsentChainedClients_AttributesSelectedBindingIssuer(t *testing.T) {
	t.Parallel()
	issuerA, issuerB := uuid.New(), uuid.New()
	g := &funcGovernor{configured: true, serves: func(identitychaining.Request) (uuid.UUID, bool) { return issuerA, true }}
	s, endpoint, state := consentChainingFixture(t, g)
	clientA := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: issuerA}
	clientB := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: issuerB}

	chained := s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{clientA, clientB}, directRouting(consentBackendRemote))
	require.True(t, chained[clientA.ID])
	require.False(t, chained[clientB.ID])
	require.Len(t, g.requests, 2, "each card is judged with its own client's request")
}

func TestConsentChainedClients_UnservedUpstreamIsNotChained(t *testing.T) {
	t.Parallel()
	g := &funcGovernor{configured: true, serves: func(identitychaining.Request) (uuid.UUID, bool) { return uuid.Nil, false }}
	s, endpoint, state := consentChainingFixture(t, g)
	client := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: uuid.New()}

	require.Empty(t, s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{client}, directRouting(consentBackendRemote)))
}

func TestConsentChainedClients_UnconfiguredIssuerSkipsSelection(t *testing.T) {
	t.Parallel()
	g := &funcGovernor{configured: false, serves: func(identitychaining.Request) (uuid.UUID, bool) { return uuid.Nil, true }}
	s, endpoint, state := consentChainingFixture(t, g)
	client := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: uuid.New()}

	require.Empty(t, s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{client}, directRouting(consentBackendRemote)))
	require.Empty(t, g.requests)
}

func TestConsentChainedClients_TunneledCardChainsByItsOwnClientsIssuer(t *testing.T) {
	t.Parallel()
	issuerA, issuerB := uuid.New(), uuid.New()
	g := &funcGovernor{configured: true, serves: func(req identitychaining.Request) (uuid.UUID, bool) {
		return req.RemoteSessionIssuerID.UUID, req.RemoteSessionIssuerID.UUID == issuerA
	}}
	s, endpoint, state := consentChainingFixture(t, g)
	clientA := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: issuerA}
	clientB := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: issuerB}

	chained := s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{clientA, clientB}, directRouting(consentBackendTunneled))
	require.True(t, chained[clientA.ID])
	require.False(t, chained[clientB.ID])
	require.Len(t, g.requests, 2, "each card is judged with its own client's issuer")
}

func renderChainedCard(t *testing.T, check, message string, connected int) string {
	t.Helper()
	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Client",
		MCPSlug:        "example",
		MCPRouteBase:   mcproute.MCP,
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:      "client-id",
			IssuerSlug:    "example-issuer",
			IssuerDisplay: "Example",
			Chained:       true,
			ChainCheck:    check,
			ChainMessage:  message,
		}},
		ConsentEnabled:     true,
		ConnectedCardCount: connected,
	})
	require.NoError(t, err)
	return normalizeWhitespace(page.String())
}

func TestConsentTemplateRendersIdentityChainedServiceWithFallback(t *testing.T) {
	t.Parallel()

	html := renderChainedCard(t, chainCheckPending, "", 0)
	require.Contains(t, html, "Managed by your identity provider", "pending reads neutral without script")
	require.Regexp(t, `<span class="text-xs text-muted-foreground" data-card-status data-identity-chained data-chain-check="pending"`, html)
	require.Contains(t, html, "data-connect-fallback")
	require.Contains(t, html, "Use a separate sign-in instead")
	require.Regexp(t, `<button type="submit" name="action" value="connect" class="[^"]*\btext-muted-foreground\b[^"]*" data-connect-link data-connect-fallback`, html)
	require.NotRegexp(t, `value="connect" class="[^"]*\b(border|bg-primary)\b[^"]*" data-connect-link data-connect-fallback`, html, "the fallback is a quiet text control")
	require.Contains(t, html, `data-connected-summary data-connected-count="0" data-connected-total="1"`)
	require.Contains(t, html, "0 of 1 connected")
	require.NotContains(t, html, `aria-label="Disconnect`)
	require.NotContains(t, html, "Not connected")
}

func TestConsentTemplateRendersCachedChainedServiceConnected(t *testing.T) {
	t.Parallel()

	html := renderChainedCard(t, chainCheckConnected, "", 1)
	require.Regexp(t, `<span class="text-xs text-default-success" data-card-status data-identity-chained data-chain-check="connected" >Service connected through your identity provider`, html)
	require.Contains(t, html, "1 of 1 connected")
	require.Contains(t, html, "Use a separate sign-in instead", "the fallback stays available once connected")
}

func TestConsentTemplateRendersRejectedChainedServiceWithPrimaryConnect(t *testing.T) {
	t.Parallel()

	html := renderChainedCard(t, chainCheckRejected, "Example didn't accept your identity provider sign-in.", 0)
	require.Regexp(t, `<span class="text-xs text-default-warning" data-card-status data-identity-chained data-chain-check="rejected" >Example didn&#39;t accept your identity provider sign-in.`, html)
	require.Regexp(t, `value="connect" class="bg-primary text-primary-foreground[^"]*" data-connect-link > Connect </button>`, html)
	require.NotContains(t, html, "data-connect-fallback")
	require.Contains(t, html, "0 of 1 connected")
}

// The runtime answers a definitive chaining refusal on a stale linked session
// with the reconnect challenge, relying on the consent page to offer a
// separate sign-in. Pin that every such refusal, repairable or not, leaves the
// chained card with a Connect action.
func TestConsentChainedCardOffersConnectForEveryDefinitiveRefusal(t *testing.T) {
	t.Parallel()

	for _, reason := range []identitychaining.Reason{
		identitychaining.ReasonConfigurationRequired, identitychaining.ReasonReauthenticationRequired, identitychaining.ReasonInvalidTarget,
		identitychaining.ReasonScopePolicyDenied, identitychaining.ReasonAccessDenied, identitychaining.ReasonInvalidClient,
		identitychaining.ReasonInvalidGrant, identitychaining.ReasonMalformedAssertion, identitychaining.ReasonMalformedResponse,
		identitychaining.ReasonInsufficientScope, identitychaining.ReasonUnknownRejection, identitychaining.ReasonIneligibleIdentity,
	} {
		for _, stage := range []identitychaining.Stage{identitychaining.StageSelection, identitychaining.StageAuthorization, identitychaining.StageDelegation, identitychaining.StageExchange, identitychaining.StageValidation, identitychaining.StageRedemption} {
			outcome := identitychaining.Outcome{Stage: stage, Reason: reason, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}
			checked := classifyChainCheck(outcome, "Example")
			require.Contains(t, []string{chainCheckRejected, chainCheckUnknown}, checked.Status, "%s at %s", reason, stage)
			if checked.Status == chainCheckRejected {
				require.Contains(t, checked.Message, "use a separate sign-in", "%s at %s", reason, stage)
			}
			html := renderChainedCard(t, checked.Status, checked.Message, 0)
			require.Regexp(t, `name="action" value="connect" class="[^"]*" data-connect-link`, html, "%s at %s", reason, stage)
		}
	}
}

func TestShouldAutoCloseFirstPartyKeepsChainedCardOpen(t *testing.T) {
	t.Parallel()
	require.False(t, shouldAutoCloseFirstParty(true, []remoteSessionCard{{Chained: true}}), "chaining alone has not connected anything")
}

func TestConsentChainedMetaClients_ServesEachRequestOnce(t *testing.T) {
	t.Parallel()
	issuer := uuid.New()
	g := &funcGovernor{configured: true, serves: func(identitychaining.Request) (uuid.UUID, bool) { return issuer, true }}
	s, endpoint, state := consentChainingFixture(t, g)
	endpoint.MetaMcpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	clientA := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: issuer}
	clientB := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: issuer}
	member := metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow{McpServerID: uuid.New(), ProjectID: endpoint.ProjectID, McpServerVisibility: "private", UpstreamUrl: "https://upstream.example.test/mcp", Tunneled: false}
	members := []metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow{member, member}
	routing := consentRouting{backend: consentBackendMeta, upstream: "", members: map[uuid.UUID][]metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow{clientA.ID: members, clientB.ID: members}}

	chained := s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{clientA, clientB}, routing)
	require.True(t, chained[clientA.ID])
	require.True(t, chained[clientB.ID])
	require.Len(t, g.requests, 2, "one render judges each distinct request once, and each client's request is its own")
}

func (g *funcGovernor) CheckConsent(context.Context, identitychaining.Request, uuid.UUID) identitychaining.Outcome {
	return identitychaining.Outcome{}
}

func (g *funcGovernor) RetireConsentCredentials(context.Context, string, uuid.UUID, uuid.UUID, string, uuid.UUID) error {
	return nil
}
