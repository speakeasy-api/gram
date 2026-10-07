// A gateway may front two members whose upstreams sign in through one
// authorization server, each configured with its own OAuth client. These
// tests carry both clients through consent, authorize, code exchange and
// persisted remote_sessions rows, so each member's grant is asserted end to
// end without either replacing or borrowing the other's.

package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// sharedIssuerAS is a live fake authorization server shared by several
// clients. It mints a distinct access token per client and records the RFC
// 8707 resource each client's code exchange carried.
type sharedIssuerAS struct {
	*httptest.Server

	mu        sync.Mutex
	resources map[string]consentExchangeCapture
}

func newSharedIssuerAS(t *testing.T) *sharedIssuerAS {
	t.Helper()
	as := &sharedIssuerAS{Server: nil, mu: sync.Mutex{}, resources: map[string]consentExchangeCapture{}}
	as.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/token" || r.ParseForm() != nil || r.PostForm.Get("grant_type") != "authorization_code" {
			http.NotFound(w, r)
			return
		}
		clientID := r.PostForm.Get("client_id")
		_, has := r.PostForm["resource"]
		as.mu.Lock()
		as.resources[clientID] = consentExchangeCapture{HasResource: has, Resource: r.PostForm.Get("resource")}
		as.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-` + clientID + `","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-` + clientID + `"}`))
	}))
	t.Cleanup(as.Close)
	return as
}

func (as *sharedIssuerAS) exchanged(externalClientID string) consentExchangeCapture {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.resources[externalClientID]
}

// createConsentClientOnIssuer registers another client with an existing
// remote_session_issuer, attached to every given user_session_issuer.
func createConsentClientOnIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID string, remoteIssuerID uuid.UUID, externalClientID string, userSessionIssuerIDs []uuid.UUID) uuid.UUID {
	t.Helper()
	q := remotesessions_repo.New(conn)
	client, err := q.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              externalClientID,
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	for _, issuerID := range userSessionIssuerIDs {
		attachConsentClient(t, ctx, conn, client.ID, issuerID)
	}
	return client.ID
}

func attachConsentClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, clientID, userSessionIssuerID uuid.UUID) {
	t.Helper()
	require.NoError(t, remotesessions_repo.New(conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userSessionIssuerID,
	}))
}

// createMetaMemberOnIssuer attaches a remote-backed member gated by
// memberIssuerID, its own user_session_issuer, stamped with remoteIssuerID.
// The member's configured client is whichever client that issuer binds.
func createMetaMemberOnIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID, memberIssuerID uuid.UUID, slug, serverURL string, remoteIssuerID uuid.UUID, sortOrder int32) seededMetaMember {
	t.Helper()
	remoteServer, err := remotemcp_repo.New(conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "sse",
		Url:           serverURL,
	})
	require.NoError(t, err)
	mcpServer, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(memberIssuerID),
	})
	require.NoError(t, err)
	stampRemoteSessionIssuer(t, ctx, conn, projectID, mcpServer.ID, conv.ToNullUUID(remoteIssuerID))
	return seededMetaMember{
		mcpServerID:    mcpServer.ID,
		remoteServerID: remoteServer.ID,
		memberID:       attachMetaMemberRow(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder),
	}
}

// sharedIssuerGateway is a gateway with two members whose upstreams share one
// authorization server, each configured with its own client, and both
// clients bound to the gateway's issuer.
type sharedIssuerGateway struct {
	fx             consentActionFixture
	as             *sharedIssuerAS
	remoteIssuerID uuid.UUID
	clients        [2]uuid.UUID
	externalIDs    [2]string
	upstreams      [2]string
}

func seedSharedIssuerGateway(t *testing.T, prefix string) (context.Context, sharedIssuerGateway) {
	t.Helper()
	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, prefix+"-gw")
	as := newSharedIssuerAS(t)

	gw := sharedIssuerGateway{
		fx:             fx,
		as:             as,
		remoteIssuerID: uuid.Nil,
		clients:        [2]uuid.UUID{},
		externalIDs:    [2]string{prefix + "-a-external-client", prefix + "-b-external-client"},
		upstreams:      [2]string{"https://" + prefix + "-a.example.com/mcp", "https://" + prefix + "-b.example.com/mcp"},
	}

	// Both members' own issuers bind their configured client, and the
	// gateway's issuer binds both: the state member auto-wiring produces.
	memberIssuers := [2]uuid.UUID{createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID), createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID)}
	gw.clients[0] = createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, prefix+"-a", as.URL, []uuid.UUID{fx.shared, memberIssuers[0]})
	gw.remoteIssuerID = clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, gw.clients[0])
	gw.clients[1] = createConsentClientOnIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, gw.remoteIssuerID, gw.externalIDs[1], []uuid.UUID{fx.shared, memberIssuers[1]})

	for i := range gw.clients {
		createMetaMemberOnIssuer(t, ctx, fx.ti.conn, fx.projectID, metaServerID, memberIssuers[i], prefix+"-member-"+string(rune('a'+i)), gw.upstreams[i], gw.remoteIssuerID, int32(i))
	}
	return ctx, gw
}

func (gw sharedIssuerGateway) resolveGatewayTokens(t *testing.T, ctx context.Context, mgr *remotesessions.ChallengeManager) remotesessions.ClientTokens {
	t.Helper()
	tokens, err := mgr.ResolveGatewayAccessTokens(ctx, gw.fx.projectID, gw.fx.orgID, gw.fx.shared, gw.fx.subject)
	require.NoError(t, err)
	return tokens
}

// Two members behind one authorization server, each with its own client:
// consent qualifies each client's grant to its own member, and the gateway
// resolves both credentials rather than collapsing them by provider.
func TestMetaMCPCredentials_SharedAuthorizationServerDistinctClients(t *testing.T) {
	t.Parallel()

	ctx, gw := seedSharedIssuerGateway(t, "gwmc-distinct")
	mgr := newConsentCallbackManager(t, gw.fx.ti)

	for i, clientID := range gw.clients {
		loc := postConnectAction(t, gw.fx, clientID)
		require.Equal(t, gw.upstreams[i], loc.Query().Get("resource"), "each client must be qualified to the member configured with it")
		completeRemoteLogin(t, mgr, loc)
		require.Equal(t, consentExchangeCapture{HasResource: true, Resource: gw.upstreams[i]}, gw.as.exchanged(gw.externalIDs[i]),
			"the code exchange must carry the client's own member as its resource")
	}

	tokens := gw.resolveGatewayTokens(t, ctx, mgr)
	require.Len(t, tokens, 2, "both members' credentials must resolve, not one per provider")
	for i, clientID := range gw.clients {
		token, ok := tokens[clientID]
		require.True(t, ok, "client %d must resolve", i)
		require.Equal(t, "token-"+gw.externalIDs[i], token.Token, "member %d must hold its own credential", i)
		require.Equal(t, gw.upstreams[i], token.Resource, "and that credential must be qualified to member %d", i)
		require.Equal(t, gw.remoteIssuerID, token.RemoteSessionIssuerID)
	}
}

// Connecting one member must not depend on, or disturb, the other: the
// gateway resolves just the connected member's credential.
func TestMetaMCPCredentials_SharedAuthorizationServerPartialConnection(t *testing.T) {
	t.Parallel()

	ctx, gw := seedSharedIssuerGateway(t, "gwmc-partial")
	mgr := newConsentCallbackManager(t, gw.fx.ti)

	completeRemoteLogin(t, mgr, postConnectAction(t, gw.fx, gw.clients[1]))

	tokens := gw.resolveGatewayTokens(t, ctx, mgr)
	require.Len(t, tokens, 1)
	token, ok := tokens[gw.clients[1]]
	require.True(t, ok, "the connected member's credential resolves")
	require.Equal(t, gw.upstreams[1], token.Resource)
	_, ok = tokens[gw.clients[0]]
	require.False(t, ok, "the unconnected member resolves nothing rather than borrowing its sibling's grant")
}

// Issuer-keyed resolution cannot represent two clients of one provider, so
// it refuses the configuration explicitly instead of picking one.
func TestMetaMCPCredentials_SharedAuthorizationServerRefusedByIssuerKeyedResolution(t *testing.T) {
	t.Parallel()

	ctx, gw := seedSharedIssuerGateway(t, "gwmc-strict")
	mgr := newConsentCallbackManager(t, gw.fx.ti)

	_, err := mgr.ResolveAccessTokens(ctx, gw.fx.projectID, gw.fx.orgID, gw.fx.shared, gw.fx.subject)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionMisconfigured)
}

// A member whose own issuer binds neither gateway client claims neither: with
// several clients per provider only the configured association qualifies a
// grant, never a member that merely shares the authorization server.
func TestMetaMCPCredentials_SharedAuthorizationServerUnassociatedMemberClaimsNothing(t *testing.T) {
	t.Parallel()

	ctx, gw := seedSharedIssuerGateway(t, "gwmc-unassoc")
	extra := createConsentClientOnIssuer(t, ctx, gw.fx.ti.conn, gw.fx.projectID, gw.fx.orgID, gw.remoteIssuerID, "gwmc-unassoc-extra-external-client", []uuid.UUID{gw.fx.shared})

	loc := postConnectAction(t, gw.fx, extra)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a client no member is configured with must not be qualified to a sibling's upstream")
}
