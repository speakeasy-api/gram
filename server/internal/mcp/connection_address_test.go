package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
	require.Equal(t, `server "member" can't connect: its address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's address.`+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
	require.NotContains(t, text, "192.0.2.10")
	requirePlainEndUserText(t, text)
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
	require.Equal(t, `server "member" can't connect: its address doesn't exist. Waiting won't fix this. Ask your administrator to fix the server's address.`+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
	requirePlainEndUserText(t, text)
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

	for _, call := range []string{"first", "inside the cooldown"} {
		text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
		require.True(t, isError, call)
		require.Contains(t, text, `server "member" can't connect: its sign-in address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's sign-in address.`, call)
		requirePlainEndUserText(t, text)
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
		require.Equal(t, "member can't connect: its sign-in address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's sign-in address.", status.Servers[0].Outage.Message, call)
		requirePlainEndUserText(t, status.Servers[0].Outage.Message)
		requirePlainEndUserText(t, status.Text())
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
		require.Equal(t, "This server can't connect: its sign-in address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's sign-in address.", instructions)
		requirePlainEndUserText(t, instructions)

		w := httptest.NewRecorder()
		gate := mcp.RemoteSessionUnavailableError(w, token.ClientCredentialErr)
		require.Equal(t, oops.CodeUnavailable, gate.Code, "still a 503")
		require.Equal(t, "This server can't connect: its sign-in address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's sign-in address.", gate.Error())
		requirePlainEndUserText(t, gate.Error())
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
	require.Contains(t, html.UnescapeString(page), "Can't connect · This sign-in's address isn't public. Ask your administrator.")
	require.Contains(t, page, "0 of 1 connected", "a down connection is not counted")
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
	require.Equal(t, "This server can't connect: its address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's address.", page.Items[0].Connection.Instructions)
	requirePlainEndUserText(t, page.Items[0].Connection.Instructions)
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
	require.Equal(t, "Can't connect · This server's sign-in address doesn't exist. Ask your administrator.", outcomes[0].Label)
	require.Equal(t, []string{mcpconnectionattempts.CodeAddressNotFound.String()}, discoveryReasons(rec), "the row agrees with the card")
	require.Contains(t, normalizeSpaces(page), `class="text-xs text-default-warning" data-gateway-server-status="unavailable"`, "styled as needing attention")

	_, focus, _ := render(t, fx)
	require.Contains(t, html.UnescapeString(normalizeSpaces(focus)), "can't connect: its sign-in address doesn't exist. Ask your administrator to fix it.")
	requirePlainEndUserText(t, outcomes[0].Label)
	require.NotContains(t, focus, "Try again shortly")
	require.NotContains(t, focus, "needs no sign-in")
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
		{name: "refused", url: "https://192.0.2.10/mcp", label: "Can't connect · This server's address isn't public. Ask your administrator.", reason: mcpconnectionattempts.CodeAddressNotAllowed},
		{name: "missing", url: "https://" + missingMemberHost + "/mcp", label: "Can't connect · This server's address doesn't exist. Ask your administrator.", reason: mcpconnectionattempts.CodeAddressNotFound},
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

// A connection stopped by its sign-in provider's address is down, like an outage: approval waits, as it does for an outage.
func TestConsentPost_SignInProviderAddressIsNotApprovable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		key        func(remotesessions_repo.RemoteSession) string
		value      any
		approvable bool
	}{
		{name: "address", key: remotesessions.RefreshAddressKey, value: guardian.AddressNotFound, approvable: false},
		{name: "outage", key: remotesessions.RefreshOutageKey, value: remotesessions.RefreshOutageProvider, approvable: false},
		{name: "nothing recorded", key: nil, value: nil, approvable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, fx := seedTwoClientConsentEndpoint(t, "address-approve-"+uuid.NewString()[:8])
			insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientA, fx.subject, "token-a", consentChainedUpstream)
			refreshableConnection(t, ctx, fx, fx.clientA, time.Now().Add(-time.Minute), serviceUnavailable)
			if tc.key != nil {
				session, err := remotesessions_repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: fx.clientA})
				require.NoError(t, err)
				redisClient, err := infra.NewRedisClient(t, 0)
				require.NoError(t, err)
				require.NoError(t, cache.NewRedisCacheAdapter(redisClient).Set(ctx, tc.key(session), tc.value, time.Minute))
			}

			_, page, _ := render(t, fx)
			require.Contains(t, page, fmt.Sprintf(`data-consent-enabled="%t"`, tc.approvable))
		})
	}
}

// requirePlainEndUserText checks an end-user string names no configuration.
func requirePlainEndUserText(t *testing.T, text string) {
	t.Helper()
	for _, word := range []string{"provider", "credential", "token", "URL", "tunnel", "connect to directly"} {
		require.NotContains(t, strings.ToLower(text), strings.ToLower(word), text)
	}
}

// signInAddressOutage is a member's real connection error after its sign-in provider's token endpoint failed at tokenEndpoint.
func signInAddressOutage(t *testing.T, tokenEndpoint string, guardianOpts ...func(*guardian.Policy)) error {
	t.Helper()
	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardianOpts...)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	rows, err := testrepo.New(ti.conn).ForceRemoteSessionIssuerTokenEndpointFixture(ctx, testrepo.ForceRemoteSessionIssuerTokenEndpointFixtureParams{
		TokenEndpoint: conv.ToPGText(tokenEndpoint), RemoteSessionClientID: f.clientID, ProjectID: f.project, OrganizationID: f.org,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	tokens, err := ti.service.RemoteChallengeManager().ResolveAvailableAccessTokens(ctx, remotesessions.RefreshAttribution{}, f.project, f.org, f.issuerID, f.subject)
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	for _, token := range tokens {
		if token.ClientCredentialErr != nil {
			return token.ClientCredentialErr
		}
	}
	require.Fail(t, "no outage")
	return nil
}

// On the consent page, both address problems read the same plain words; only Technical details say which.
// What the client receives stays the plain sentence.
func TestConsentAgentOutagePageNamesTheProblemOnlyInTechnicalDetails(t *testing.T) {
	t.Parallel()

	missingIssuer := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(ctx context.Context, network, host string) ([]net.IP, error) {
		if host == "token.example.invalid" {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return net.DefaultResolver.LookupIP(ctx, network, host)
	}})
	for name, tc := range map[string]struct {
		endpoint string
		opts     []func(*guardian.Policy)
		detail   string
		message  string
		body     string
	}{
		"refused": {endpoint: "https://192.0.2.10/token", opts: []func(*guardian.Policy){guardian.WithBlockedCIDRBlocks("192.0.2.0/24")}, detail: "The sign-in provider uses a private or internal address Speakeasy can't reach.", message: "can't connect: its sign-in address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's sign-in address.", body: "because its sign-in address is a private or internal one that Speakeasy can't reach. This isn't something you did."},
		"missing": {endpoint: "https://token.example.invalid/token", opts: []func(*guardian.Policy){guardian.WithResolver(missingIssuer)}, detail: "The sign-in provider's address doesn't exist.", message: "can't connect: its sign-in address doesn't exist. Waiting won't fix this. Ask your administrator to fix the server's sign-in address.", body: "because its sign-in address doesn't exist. This isn't something you did."},
	} {
		outage := signInAddressOutage(t, tc.endpoint, tc.opts...)

		client := httptest.NewRecorder()
		gate := mcp.RemoteSessionUnavailableError(client, outage)
		require.Equal(t, "This server "+tc.message, gate.Error(), name)

		page := httptest.NewRecorder()
		oops.BrowserErrHandle(testenv.NewLogger(t), func(w http.ResponseWriter, _ *http.Request) error {
			return mcp.AgentConnectionsOutageError(w, outage)
		}).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/mcp/x/connect", nil))
		text := html.UnescapeString(normalizeSpaces(page.Body.String()))
		require.Equal(t, http.StatusServiceUnavailable, page.Code, name)
		require.Contains(t, text, tc.body, name)
		require.Contains(t, text, tc.detail, name)
		require.Contains(t, text, "This server "+tc.message, "the error description stays the end-user sentence")
	}
}
