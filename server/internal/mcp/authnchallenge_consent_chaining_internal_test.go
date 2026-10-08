package mcp

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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
	return &Service{identityChainer: g, logger: testenv.NewLogger(t)}, endpoint, AuthnChallengeState{Subject: &subject}
}

func directRouting(backend consentBackend, issuer uuid.NullUUID) consentRouting {
	return consentRouting{backend: backend, upstream: "", issuer: issuer, members: nil, grants: nil}
}

func TestConsentChainedClients_RequestMatchesRuntime(t *testing.T) {
	t.Parallel()
	tunnelIssuer := uuid.New()
	for _, tc := range []struct {
		name     string
		routing  consentRouting
		tunneled bool
	}{
		{"remote", directRouting(consentBackendRemote, uuid.NullUUID{}), false},
		{"tunneled", directRouting(consentBackendTunneled, uuid.NullUUID{UUID: tunnelIssuer, Valid: true}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &funcGovernor{configured: true, serves: func(identitychaining.Request) (uuid.UUID, bool) { return tunnelIssuer, true }}
			s, endpoint, state := consentChainingFixture(t, g)
			client := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: tunnelIssuer}

			require.True(t, s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{client}, tc.routing)[client.ID])

			runtime, ok := s.identityChainingRequest(humanChainingContext(t, "user-1", chainingTestOrg), endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID, endpoint.UpstreamResource, tc.tunneled, uuid.NullUUID{UUID: tunnelIssuer, Valid: true})
			require.True(t, ok)
			require.Equal(t, []identitychaining.Request{runtime}, g.requests, "consent judges with the runtime's own request")
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

	chained := s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{clientA, clientB}, directRouting(consentBackendRemote, uuid.NullUUID{}))
	require.True(t, chained[clientA.ID])
	require.False(t, chained[clientB.ID])
	require.Len(t, g.requests, 1)
}

func TestConsentChainedClients_UnservedUpstreamIsNotChained(t *testing.T) {
	t.Parallel()
	g := &funcGovernor{configured: true, serves: func(identitychaining.Request) (uuid.UUID, bool) { return uuid.Nil, false }}
	s, endpoint, state := consentChainingFixture(t, g)
	client := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: uuid.New()}

	require.Empty(t, s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{client}, directRouting(consentBackendRemote, uuid.NullUUID{})))
}

func TestConsentChainedClients_UnconfiguredIssuerSkipsSelection(t *testing.T) {
	t.Parallel()
	g := &funcGovernor{configured: false, serves: func(identitychaining.Request) (uuid.UUID, bool) { return uuid.Nil, true }}
	s, endpoint, state := consentChainingFixture(t, g)
	client := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: uuid.New()}

	require.Empty(t, s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{client}, directRouting(consentBackendRemote, uuid.NullUUID{})))
	require.Empty(t, g.requests)
}

func TestConsentChainedClients_TunnelWithoutIssuerNeverChains(t *testing.T) {
	t.Parallel()
	g := &funcGovernor{configured: true, serves: func(identitychaining.Request) (uuid.UUID, bool) { return uuid.Nil, true }}
	s, endpoint, state := consentChainingFixture(t, g)
	client := remotesessions.Client{ID: uuid.New(), RemoteSessionIssuerID: uuid.New()}

	require.Empty(t, s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{client}, directRouting(consentBackendTunneled, uuid.NullUUID{})))
	require.Empty(t, g.requests)
}

func TestConsentTemplateRendersIdentityChainedServiceWithFallback(t *testing.T) {
	t.Parallel()

	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:     "Client",
		MCPSlug:        "example",
		MCPRouteBase:   "mcp",
		State:          "state",
		CSRFToken:      "csrf",
		SubjectDisplay: "user@example.com",
		ScriptURL:      "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{{
			ClientID:      "client-id",
			IssuerSlug:    "example-issuer",
			IssuerDisplay: "Example",
			Chained:       true,
		}},
		ConsentEnabled:     true,
		ConnectedCardCount: 0,
	})
	require.NoError(t, err)

	html := normalizeWhitespace(page.String())
	require.Contains(t, html, "Managed by your identity provider")
	require.Contains(t, html, "data-connect-fallback")
	require.Contains(t, html, "Connect manually")
	require.Contains(t, html, "0 of 1 connected")
	require.NotContains(t, html, `aria-label="Disconnect`)
	require.NotContains(t, html, "Not connected")
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
	member := metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow{McpServerID: uuid.New(), McpServerVisibility: "private", UpstreamUrl: "https://upstream.example.test/mcp", Tunneled: false}
	routing := consentRouting{backend: consentBackendMeta, upstream: "", issuer: uuid.NullUUID{}, members: map[uuid.UUID][]metamcprepo.ListMetaMCPMembersForRemoteSessionIssuerRow{issuer: {member, member}}, grants: nil}

	chained := s.consentChainedClients(t.Context(), endpoint, state, []remotesessions.Client{clientA, clientB}, routing)
	require.True(t, chained[clientA.ID])
	require.True(t, chained[clientB.ID])
	require.Len(t, g.requests, 1, "one render judges each distinct request once")
}
