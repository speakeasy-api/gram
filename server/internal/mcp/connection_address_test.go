package mcp_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// missingMemberHost is a member host the resolver says does not exist.
const missingMemberHost = "member.example.invalid"

// callMemberAt calls a gateway member whose address is memberURL and returns the call's text and its row.
func callMemberAt(t *testing.T, memberURL string, guardianOpts ...func(*guardian.Policy)) (string, mcpconnectionattempts.Row) {
	t.Helper()

	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardianOpts...)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-address-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-address", "", []uuid.UUID{issuerID})
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", memberURL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
	subject := createTestUser(t, ctx, ti, "meta-address-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-address", memberURL)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	rows := rec.Rows()
	require.NotEmpty(t, rows, text)
	return text, rows[len(rows)-1]
}

func TestGatewayMemberAtBlockedAddress(t *testing.T) {
	t.Parallel()

	text, row := callMemberAt(t, "https://192.0.2.10/mcp", guardian.WithBlockedCIDRBlocks("192.0.2.0/24"))

	require.Equal(t, mcpconnectionattempts.CodeAddressNotAllowed.String(), row.Reason)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.NotEmpty(t, row.MemberID)
	require.Equal(t, "192.0.2.10", row.ProviderHost, "the member's configured host")
	require.Equal(t, `server "member" uses an address Speakeasy does not connect to directly. Ask your administrator to check the server's address`+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
	require.NotContains(t, text, "192.0.2.10")
}

func TestGatewayMemberAtMissingHost(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(ctx context.Context, network, host string) ([]net.IP, error) {
			if host == missingMemberHost {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			return net.DefaultResolver.LookupIP(ctx, network, host)
		},
	})
	text, row := callMemberAt(t, "https://"+missingMemberHost+"/mcp", guardian.WithResolver(resolver))

	require.Equal(t, mcpconnectionattempts.CodeAddressNotFound.String(), row.Reason)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.Equal(t, missingMemberHost, row.ProviderHost, "the member's configured host")
	require.Equal(t, `server "member" uses an address that does not point anywhere. Ask your administrator to check the server's address`+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
}

// A lookup that fails for now is not a missing name: it stays the provider's to answer.
func TestGatewayMemberAtHostWhoseLookupFails(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(ctx context.Context, network, host string) ([]net.IP, error) {
			if host == missingMemberHost {
				return nil, errors.New("resolver down")
			}
			return net.DefaultResolver.LookupIP(ctx, network, host)
		},
	})
	text, row := callMemberAt(t, "https://"+missingMemberHost+"/mcp", guardian.WithResolver(resolver))

	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable.String(), row.Reason)
	require.Equal(t, `server "member" did not answer: upstream unreachable or timed out`+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
}

// A sign-in provider whose address egress policy refuses reads as the admin's on every surface, on the
// first call and on calls paced inside the cooldown that follows, and is never shown as a provider outage.
func TestGatewayMemberSignInProviderAtBlockedAddress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardian.WithBlockedCIDRBlocks("192.0.2.0/24"))
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	rows, err := testrepo.New(ti.conn).ForceRemoteSessionIssuerTokenEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerTokenEndpointFixtureParams{
		TokenEndpoint: conv.ToPGText("https://192.0.2.10/token"), RemoteSessionClientID: f.clientID, ProjectID: f.project, OrganizationID: f.org,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	const want = "uses an address Speakeasy does not connect to directly. Ask your administrator to check the sign-in provider's address"

	for _, call := range []string{"first", "inside the cooldown"} {
		text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
		require.True(t, isError, call)
		require.Contains(t, text, `server "member" can't renew its sign-in because its sign-in provider `+want, call)
		require.NotContains(t, text, "Retry", call)
		require.NotContains(t, text, "reconnect", call)
		require.NotContains(t, text, "192.0.2.10", call)
		row := rec.Rows()[len(rec.Rows())-1]
		require.Equal(t, mcpconnectionattempts.CodeAddressNotAllowed.String(), row.Reason, call)
		require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes, call)
		require.Empty(t, row.ProviderHost, "a sign-in provider's address names no member host")

		status := f.connectionStatus(t, ctx)
		require.Equal(t, metamcp.SignInUnavailable, status.Servers[0].Status, call)
		require.NotNil(t, status.Servers[0].Outage, call)
		require.Equal(t, "member can't connect because its sign-in provider "+want+".", status.Servers[0].Outage.Message, call)
		require.Empty(t, status.Servers[0].Outage.ReconnectURL, call)
		require.True(t, status.Servers[0].Outage.NeedsAdmin, call)
		require.Contains(t, status.Text(), "- member: can't connect (", call)
		require.NotContains(t, status.Text(), "temporarily unavailable", call)
		require.NotContains(t, status.Servers[0].Detail, "reconnect at", call)
	}
	require.Zero(t, f.refreshes.Load(), "nothing reached the provider")

	tokens, err := ti.service.RemoteChallengeManager().ResolveAvailableAccessTokens(ctx, remotesessions.RefreshAttribution{}, f.project, f.org, f.issuerID, f.subject)
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	for _, token := range tokens {
		state, instructions := mcp.ConnectionRouteFailureForOutage("member", token.ClientCredentialErr, true)
		require.Equal(t, "unreachable", state)
		require.Equal(t, "This server can't connect because its sign-in provider "+want+".", instructions)

		w := httptest.NewRecorder()
		gate := mcp.RemoteSessionUnavailableError(w, token.ClientCredentialErr)
		require.Equal(t, oops.CodeUnavailable, gate.Code, "still a 503")
		require.Equal(t, "This MCP server can't get the credential it needs because its sign-in provider "+want+".", gate.Error())
		require.Empty(t, w.Header().Get("Retry-After"), "waiting does not fix an address")
	}
}

// The connect page reads the recorded address: no outage, no Reconnect, and who fixes it.
func TestServeConsent_SignInProviderAddressCard(t *testing.T) {
	t.Parallel()

	ctx, fx, clientID := outageConsentFixture(t, "address-card")
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), serviceUnavailable)
	session, err := remotesessions_repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	require.NoError(t, cache.NewRedisCacheAdapter(redisClient).Set(ctx, remotesessions.RefreshAddressKey(session), guardian.AddressNotAllowed, time.Minute))

	_, page, _ := render(t, fx)
	require.Contains(t, page, "Sign-in provider address can't be used · ask your administrator")
	require.NotContains(t, page, "Unavailable · try again shortly")
	require.NotContains(t, page, "> Reconnect </button>")
	require.NotContains(t, page, "Service connected")
	require.NotContains(t, page, "data-auto-close")
}

// Code Mode's verify names a member's own refused address instead of asking to try later.
func TestCodeServersVerifyMemberAtBlockedAddress(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardian.WithBlockedCIDRBlocks("192.0.2.0/24"))
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "verify-address-"+uuid.NewString()[:8], issuer)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Remote", "remote", 0, "https://192.0.2.10/mcp")
	host := mcp.NewTestMetaCodeHost(t, ctx, ti.service.Service, gateway.ID)

	page, err := host.Servers(ctx, codemode.PageArgs{Server: "remote", Verify: true})
	require.NoError(t, err)
	require.Equal(t, "unreachable", page.Items[0].Connection.Authentication)
	require.Equal(t, "This server uses an address Speakeasy does not connect to directly. Ask your administrator to check the server's address.", page.Items[0].Connection.Instructions)
}

// The gateway list says a member's sign-in provider address is the problem, inside the cooldown that follows it.
func TestServeConsent_GatewayListSaysTheSignInProviderAddress(t *testing.T) {
	t.Parallel()

	ctx, fx, clientID := outageConsentFixture(t, "address-gateway")
	refreshableConnection(t, ctx, fx, clientID, time.Now().Add(-time.Minute), serviceUnavailable)
	session, err := remotesessions_repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	locks := cache.NewRedisCacheAdapter(redisClient)
	require.NoError(t, locks.Set(ctx, remotesessions.RefreshAddressKey(session), guardian.AddressNotFound, time.Minute))
	require.NoError(t, locks.Set(ctx, remotesessions.RefreshCooldownKey(session), time.Now().Add(time.Minute), time.Minute))

	rec := mcpconnectionattemptstest.New(t)
	fx.ti.service.SetConnectionRecorder(rec.Recorder)
	withConsentFlow(t, ctx, fx.ti, fx.stateID)

	list := fx
	list.server = ""
	_, page, _ := render(t, list)
	outcomes := gatewayOutcomesFromPage(page)
	require.Len(t, outcomes, 1)
	require.Equal(t, "unavailable", outcomes[0].Status)
	require.Equal(t, "Can't connect · its sign-in provider uses an address that does not point anywhere · ask your administrator", outcomes[0].Label)
	require.Equal(t, []string{mcpconnectionattempts.CodeAddressNotFound.String()}, discoveryReasons(rec), "the row agrees with the card")
	require.NotContains(t, page, "try again shortly")
}

// withConsentFlow gives a stored consent challenge a flow, so its page records an attempt.
func withConsentFlow(t *testing.T, ctx context.Context, ti *testInstance, stateID string) {
	t.Helper()
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
}

// discoveryReasons are the reasons of the discovery_member rows recorded, in order.
func discoveryReasons(rec *mcpconnectionattemptstest.Recording) []string {
	var reasons []string
	for _, row := range rec.Rows() {
		if row.Step == mcpconnectionattempts.StepDiscoveryMember.String() {
			reasons = append(reasons, row.Reason)
		}
	}
	return reasons
}

// A gateway member whose own address is refused or missing says so on the gateway list, and its row agrees.
// A member that is unavailable for another reason keeps member_tools_missing.
func TestGatewayConsentMemberAtUnusableAddress(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(ctx context.Context, network, host string) ([]net.IP, error) {
			if host == missingMemberHost {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			return net.DefaultResolver.LookupIP(ctx, network, host)
		},
	})
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)

	for _, tc := range []struct {
		name   string
		url    string
		label  string
		reason mcpconnectionattempts.Code
	}{
		{name: "refused", url: "https://192.0.2.10/mcp", label: "Can't connect · it uses an address Speakeasy does not connect to directly · ask your administrator", reason: mcpconnectionattempts.CodeAddressNotAllowed},
		{name: "missing", url: "https://" + missingMemberHost + "/mcp", label: "Can't connect · it uses an address that does not point anywhere · ask your administrator", reason: mcpconnectionattempts.CodeAddressNotFound},
		{name: "broken server", url: broken.URL, label: "Unavailable · try again shortly, or ask your administrator", reason: mcpconnectionattempts.CodeMemberToolsMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPServiceWithTunnelPublicConfig(t, &mockIdentityResolver{}, mcp.TunnelPublicConfig{}, guardian.WithBlockedCIDRBlocks("192.0.2.0/24"), guardian.WithResolver(resolver))
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "gateway-address-" + uuid.NewString()[:8]
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member", "member-"+uuid.NewString()[:8], 1, tc.url)

			stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
			withConsentFlow(t, ctx, ti, stateID)
			outcomes := gatewayOutcomesFromPage(consentGetPageWithContext(t, ctx, ti, slug, stateID))
			require.Len(t, outcomes, 1)
			require.Equal(t, "unavailable", outcomes[0].Status)
			require.Equal(t, tc.label, outcomes[0].Label)
			require.Equal(t, []string{tc.reason.String()}, discoveryReasons(rec), "the row agrees with the card")
		})
	}
}
