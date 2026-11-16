package mcp_test

import (
	"net"
	"net/http"
	"testing"

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
		{name: "saved key rejected", url: failing(http.StatusUnauthorized), savedKey: true, reason: mcpconnectionattempts.CodeStaticKeyRejected, who: mcpconnectionattempts.WhoAdmin, named: true, status: "unavailable", label: "Can't connect · it rejected the credential set up for it · ask your administrator"},
		{name: "rate limited", url: failing(http.StatusTooManyRequests), reason: mcpconnectionattempts.CodeProviderRateLimited, who: mcpconnectionattempts.WhoProvider, named: true, status: "unavailable", label: "Unavailable · it's limiting requests right now · try again in a few minutes"},
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
		// A plain configured header makes the page ask for setup before it lists; typed saved keys are covered in the internal test.
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

// A person's own connection the upstream rejects is recorded on the listing; a member they have not connected is not.
func TestGatewayDirectListingPersonCredential(t *testing.T) {
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
			member := createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
			subject := createTestUser(t, ctx, ti, "meta-listing-user-"+uuid.NewString())
			bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
			if connected {
				insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-listing", upstream)
			}
			setGatewayMode(t, ctx, ti, meta.ID, "direct")

			w, err := servePublicHTTP(t, t.Context(), ti, metaSlug, makeMetaRPCBody(t, "tools/list", map[string]any{}), bearer, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code)
			if !connected {
				require.Empty(t, discoveryReasons(rec), "a sign-in not done yet is not a failure")
				return
			}
			row := rec.RequireEnded(mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeProviderRejected)
			requireListingRow(t, row, listingCase{who: mcpconnectionattempts.WhoPerson, named: true}, member.mcpServerID, upstream)
		})
	}
}

func happenedReason(what mcpconnectionattempts.Happened) string {
	if code, ok := what.(mcpconnectionattempts.Code); ok {
		return code.String()
	}
	return ""
}
