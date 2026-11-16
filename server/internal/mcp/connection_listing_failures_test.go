package mcp_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// stoppedUpstream accepts connections and closes them unanswered, and keeps its port.
func stoppedUpstream(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return "http://" + listener.Addr().String()
}

// listingCase is a member upstream and what a listing that loses it records and shows.
type listingCase struct {
	name     string
	url      func(t *testing.T) string
	savedKey bool
	reason   mcpconnectionattempts.Happened
	who      mcpconnectionattempts.WhoFixes
	named    bool
	status   string
	label    string
}

func listingCases() []listingCase {
	failing := func(status int) func(t *testing.T) string {
		return func(t *testing.T) string { t.Helper(); return statusUpstream(t, status).URL }
	}
	return []listingCase{
		{name: "saved key rejected", url: failing(http.StatusUnauthorized), savedKey: true},
		{name: "rate limited", url: failing(http.StatusTooManyRequests), reason: mcpconnectionattempts.CodeProviderRateLimited, who: mcpconnectionattempts.WhoProvider, named: true, status: "unavailable", label: "Unavailable · it's busy · try again in a few minutes"},
		{name: "server error", url: failing(http.StatusInternalServerError), reason: mcpconnectionattempts.CodeProviderServerError, who: mcpconnectionattempts.WhoProvider, named: true, status: "unavailable", label: "Unavailable · it had an error · try again in a few minutes"},
		{name: "stopped", url: stoppedUpstream, reason: mcpconnectionattempts.CodeProviderUnreachable, who: mcpconnectionattempts.WhoProvider, named: true, status: "unavailable", label: "Unavailable · it didn't answer · try again in a few minutes"},
		{name: "unreadable listing", url: func(t *testing.T) string {
			t.Helper()
			return newListingUpstream(t, []any{map[string]any{"description": "no name", "inputSchema": map[string]any{"type": "object"}}}, nil)
		}, reason: mcpconnectionattempts.CodeMemberToolsMissing, who: mcpconnectionattempts.WhoAdmin, status: "unavailable", label: "Unavailable · try again shortly, or ask your administrator"},
		{name: "no tools", url: func(t *testing.T) string { t.Helper(); return newListingUpstream(t, []any{}, nil) }, reason: mcpconnectionattempts.ResultAllowed, status: "no_sign_in", label: "Ready · no sign-in needed"},
	}
}

// seedListingMember adds a member at url to gateway, with a saved key when asked.
func seedListingMember(t *testing.T, ti *testInstance, projectID, gatewayID uuid.UUID, tc listingCase, url string) mcpserversrepo.McpServer {
	t.Helper()
	ctx := t.Context()
	slug := "member-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gatewayID, "Member", slug, 1, url)
	member, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{Slug: conv.ToPGText(slug), ProjectID: projectID})
	require.NoError(t, err)
	if tc.savedKey {
		seedRemoteHeader(t, ctx, ti.conn, projectID, member.RemoteMcpServerID.UUID, "Authorization", "Bearer saved-key", "", false)
	}
	return member
}

func requireListingRow(t *testing.T, row mcpconnectionattempts.Row, tc listingCase, member uuid.UUID, url string) {
	t.Helper()
	require.Equal(t, member.String(), row.MemberID)
	if tc.who != "" {
		require.Equal(t, string(tc.who), row.WhoFixes)
	}
	if tc.named {
		require.Equal(t, mcpconnectionattempts.NormalizeProviderHost(url), row.ProviderHost, "the upstream failed, so its host is named")
	} else {
		require.Empty(t, row.ProviderHost)
	}
}

// The gateway's connect page and its rows say why a member did not list its tools, once per attempt.
func TestGatewayConsentListingFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range listingCases() {
		// A key with no sign-in makes the page ask for setup before it lists; a key beside a sign-in is TestGatewayConsentSavedKeyBesideASignIn.
		if tc.savedKey {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPServiceWithTunnelPublicConfig(t, &mockIdentityResolver{}, mcp.TunnelPublicConfig{})
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "gateway-listing-" + uuid.NewString()[:8]
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuer.ID)
			url := tc.url(t)
			member := seedListingMember(t, ti, *authCtx.ProjectID, gateway.ID, tc, url)

			stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
			withConsentFlow(t, ctx, ti, stateID)
			for range 2 {
				outcomes := gatewayOutcomesFromPage(consentGetPageWithContext(t, ctx, ti, slug, stateID))
				require.Len(t, outcomes, 1)
				require.Equal(t, tc.status, outcomes[0].Status)
				require.Equal(t, tc.label, outcomes[0].Label)
			}
			require.Equal(t, []string{happenedReason(tc.reason)}, discoveryReasons(rec), "a reload writes no second row")
			for _, row := range rec.Rows() {
				if row.Step == mcpconnectionattempts.StepDiscoveryMember.String() {
					requireListingRow(t, row, tc, member.ID, url)
				}
			}
		})
	}
}

// A Direct listing records a member it lost as a call on it would be recorded, and leaves the others listed.
func TestGatewayDirectListingFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range listingCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			rec := recordConnections(t, ti)
			authCtx, _ := contextvalues.GetAuthContext(ctx)
			slug := "gateway-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
			healthy := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 2, mcpservers.VisibilityPublic, "alpha")
			url := tc.url(t)
			member := seedListingMember(t, ti, *authCtx.ProjectID, gateway.ID, tc, url)
			setGatewayMode(t, ctx, ti, gateway.ID, "direct")

			w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code)
			require.Contains(t, w.Body.String(), healthy.slug+"--alpha")
			require.NotContains(t, w.Body.String(), `"error"`)

			code, failed := tc.reason.(mcpconnectionattempts.Code)
			if !failed {
				rec.RequireNothing()
				return
			}
			requireListingRow(t, rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, code), tc, member.ID, url)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// A person's own connection is theirs to make: a listing that loses it writes no row, rejected or not yet made.
func TestGatewayDirectListingPersonCredentialWritesNothing(t *testing.T) {
	t.Parallel()

	for _, connected := range []bool{true, false} {
		t.Run(map[bool]string{true: "rejected", false: "not connected"}[connected], func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t))
			authCtx := requireProjectAuthContext(t, ctx)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
			upstream := statusUpstream(t, http.StatusUnauthorized).URL

			issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
			metaSlug := "meta-listing-" + uuid.NewString()[:8]
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
			clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-listing", "", []uuid.UUID{issuerID})
			createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
			subject := createTestUser(t, ctx, ti, "meta-listing-user-"+uuid.NewString())
			bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
			if connected {
				insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-listing", upstream)
			}
			setGatewayMode(t, ctx, ti, meta.ID, "direct")

			w, err := servePublicHTTP(t, t.Context(), ti, metaSlug, makeMetaRPCBody(t, "tools/list", map[string]any{}), bearer, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code)
			require.Empty(t, discoveryReasons(rec))
		})
	}
}

// A member with a saved key and a sign-in the person has not made shows Not connected, which connecting fixes.
func TestGatewayConsentSavedKeyBesideASignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithTunnelPublicConfig(t, &mockIdentityResolver{}, mcp.TunnelPublicConfig{})
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "gateway-key-" + uuid.NewString()[:8]
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, issuer.ID)
	upstream := statusUpstream(t, http.StatusUnauthorized).URL
	remoteClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "gateway-key", "", []uuid.UUID{issuer.ID})
	member := createMetaMember(t, ctx, ti.conn, projectID, gateway.ID, "member", upstream, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, remoteClient)), 0)
	seedRemoteHeader(t, ctx, ti.conn, projectID, member.remoteServerID, "Authorization", "Bearer saved-key", "", false)

	stateID, _ := seedGatewayConsentChallenge(t, ctx, ti, issuer.ID, client, gateway.ID, slug)
	withConsentFlow(t, ctx, ti, stateID)
	page := consentGetPageWithContext(t, ctx, ti, slug, stateID)
	outcomes := gatewayOutcomesFromPage(page)
	require.Len(t, outcomes, 1)
	require.Equal(t, "not_connected", outcomes[0].Status)
	require.Equal(t, "Not connected", outcomes[0].Label)
	require.Equal(t, []string{mcpconnectionattempts.CodeNeedsSignIn.String()}, discoveryReasons(rec))
	require.NotContains(t, page, "isn't set up correctly")
}

// The gateway listing's own time running out is no member's failure: a slow member, and one that never got a turn, keep member_tools_missing.
func TestGatewayDirectListingTimeIsNotTheUpstreams(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := recordConnections(t, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// More slow members than a listing describes at once, so one waits for a turn it never gets.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slow.Close)
	members := 7
	for i := range members {
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Slow", "slow-"+uuid.NewString()[:8], int32(i+2), slow.URL)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")

	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	rows := rec.Rows()
	require.Len(t, rows, members)
	for _, row := range rows {
		require.Equal(t, mcpconnectionattempts.CodeMemberToolsMissing.String(), row.Reason)
		require.Empty(t, row.ProviderHost)
	}
}

// A rate limit already answered stands when the retry after it runs out of listing time.
func TestGatewayDirectListingKeepsAnAnswerPastTheListingTime(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := recordConnections(t, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	var requests atomic.Int32
	var first atomic.Int64
	// The first try's requests are rate limited; the retry's hang past the listing time.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		first.CompareAndSwap(0, time.Now().UnixNano())
		if time.Since(time.Unix(0, first.Load())) < 300*time.Millisecond {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(upstream.Close)
	member := seedListingMember(t, ti, *authCtx.ProjectID, gateway.ID, listingCase{}, upstream.URL)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")

	_, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Greater(t, requests.Load(), int32(1), "the member was retried")
	row := rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeProviderRateLimited)
	requireListingRow(t, row, listingCase{who: mcpconnectionattempts.WhoProvider, named: true}, member.ID, upstream.URL)
}

// A retry that got its own answer keeps it, even when cleaning up its session runs past the listing time.
func TestGatewayDirectListingRetryAnswerStandsPastTheListingTime(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := recordConnections(t, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	var first atomic.Int64
	var rejected atomic.Bool
	// The first try is unavailable; the retry is refused, minting a session whose cleanup hangs past the listing time.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.CompareAndSwap(0, time.Now().UnixNano())
		switch {
		case r.Method == http.MethodDelete:
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		case time.Since(time.Unix(0, first.Load())) < 300*time.Millisecond:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			rejected.Store(true)
			w.Header().Set("Mcp-Session-Id", "orphan")
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(upstream.Close)
	seedListingMember(t, ti, *authCtx.ProjectID, gateway.ID, listingCase{}, upstream.URL)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")

	_, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.True(t, rejected.Load(), "the member was retried")
	require.Empty(t, discoveryReasons(rec), "a member with no credential to send writes no row, as before")
}

func happenedReason(what mcpconnectionattempts.Happened) string {
	if code, ok := what.(mcpconnectionattempts.Code); ok {
		return code.String()
	}
	return ""
}
