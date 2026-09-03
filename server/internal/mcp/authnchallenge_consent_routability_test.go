// A remote_session is one grant per (subject, client), shared by every
// endpoint bound to that client, so the consent page sees live sessions the
// endpoint's runtime routing will never forward. Those must render as needing
// a reconnect, never as "Connected".

package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tunneledmcp_repo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const consentReconnectCopy = "Connected elsewhere — reconnect to continue"

// grant writes a live session for the fixture subject on clientID; an empty
// resource is the pre-resource legacy shape.
func grant(t *testing.T, ctx context.Context, fx consentActionFixture, clientID uuid.UUID, resource string) {
	t.Helper()
	encrypted, err := fx.ti.enc.Encrypt([]byte("token-" + clientID.String()))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:             fx.subject,
		UserSessionIssuerID:    fx.shared,
		RemoteSessionClientID:  clientID,
		AccessTokenEncrypted:   encrypted,
		AccessExpiresAt:        pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		RefreshTokenEncrypted:  pgtype.Text{String: "", Valid: false},
		AuthorizationExpiresAt: pgtype.Timestamptz{Valid: false},
		RefreshExpiresAt:       pgtype.Timestamptz{Valid: false},
		Scopes:                 []string{},
		Resource:               conv.ToPGTextEmpty(resource),
		AutoRefresh:            true,
	})
	require.NoError(t, err)
}

// registerPageClient registers the OAuth client the challenge names, which
// the page looks up before rendering.
func registerPageClient(t *testing.T, ctx context.Context, fx consentActionFixture) {
	t.Helper()
	_, err := usersessions_repo.New(fx.ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     fx.shared,
		ClientID:                "age3328-mcp-client",
		ClientSecretHash:        pgtype.Text{String: "", Valid: false},
		ClientName:              "Routability Client",
		RedirectUris:            []string{"http://example.com/cb"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "none",
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
}

// metaConsent seeds a gateway consent challenge plus one provider client,
// returning the client and its authorization server.
func metaConsent(t *testing.T, tag string) (context.Context, consentActionFixture, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, fx, metaID := seedMetaConsentEndpoint(t, tag+"-gw")
	registerPageClient(t, ctx, fx)
	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, tag, "", []uuid.UUID{fx.shared})
	return ctx, fx, metaID, clientID, clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)
}

// standaloneConsent seeds a consent challenge for the single proxied server
// build creates, resolving to upstream.
func standaloneConsent(t *testing.T, tag, upstream string, build func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID) (context.Context, consentActionFixture) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	projectID, orgID := consentTestTenant(t, ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := tag + "-" + uuid.NewString()[:8]
	serverID := build(ctx, ti, projectID, issuerID, slug)
	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, issuerID, slug)
	endpoint.McpServerID = conv.ToNullUUID(serverID)
	endpoint.UpstreamResource = upstream
	fx := consentActionFixture{ti: ti, endpoint: endpoint, stateID: stateID, projectID: projectID, orgID: orgID, shared: issuerID, subject: subject, clientA: uuid.Nil, clientB: uuid.Nil, clientC: uuid.Nil, clientD: uuid.Nil}
	registerPageClient(t, ctx, fx)
	return ctx, fx
}

func createTunneledServer(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug, identifier string) uuid.UUID {
	t.Helper()
	tunneled, err := tunneledmcp_repo.New(ti.conn).CreateServer(ctx, tunneledmcp_repo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          projectID,
		Name:               slug,
		KeyHash:            "hash-" + slug,
		KeyPrefix:          "pfx",
		ResourceIdentifier: conv.ToPGTextEmpty(identifier),
	})
	require.NoError(t, err)
	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		TunneledMcpServerID: conv.ToNullUUID(tunneled.ID),
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	require.NoError(t, err)
	return server.ID
}

// render GETs the consent page: the rendered html, or the auto-connect
// redirect the page issues once per challenge when its sole card is unlinked.
func render(t *testing.T, fx consentActionFixture) (int, string, *url.URL) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+fx.endpoint.Slug+"/connect?state="+url.QueryEscape(fx.stateID), nil)
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(w, req, fx.endpoint))
	var loc *url.URL
	if l := w.Header().Get("Location"); l != "" {
		var err error
		loc, err = url.Parse(l)
		require.NoError(t, err)
	}
	return w.Code, strings.Join(strings.Fields(w.Body.String()), " "), loc
}

// expect asserts the connected count and whether the reconnect prompt shows.
func expect(t *testing.T, fx consentActionFixture, connected, total int, reconnect bool) {
	t.Helper()
	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, strconv.Itoa(connected)+" of "+strconv.Itoa(total)+" connected")
	if reconnect {
		require.Contains(t, html, consentReconnectCopy)
		require.Contains(t, html, "> Reconnect </button>")
	} else {
		require.NotContains(t, html, consentReconnectCopy)
	}
}

// expectAutoReconnect asserts the page sent the subject straight to the
// provider with the resource the new grant will record.
func expectAutoReconnect(t *testing.T, fx consentActionFixture, resource string) {
	t.Helper()
	code, html, loc := render(t, fx)
	require.Equal(t, http.StatusSeeOther, code, html)
	require.NotNil(t, loc)
	require.Equal(t, resource, loc.Query().Get("resource"))
}

// The production shape: a grant minted through a sibling server before
// resources were recorded is live, shared with the gateway, and unroutable.
func TestServeConsent_MetaRemoteMemberLegacyGrantAsksForReconnect(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "rt-legacy")
	const upstream = "https://member.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "rt-legacy-member", upstream, conv.ToNullUUID(issuerID), 0)

	grant(t, ctx, fx, clientID, "")
	expectAutoReconnect(t, fx, upstream)
	expect(t, fx, 0, 1, true)
	grant(t, ctx, fx, clientID, "https://elsewhere.example.com/mcp")
	expect(t, fx, 0, 1, true)
	grant(t, ctx, fx, clientID, upstream+"/")
	expect(t, fx, 1, 1, false)
}

// A tunneled member accepts an unqualified grant; only one qualified to
// another identifier is refused.
func TestServeConsent_MetaTunneledMemberHonoursIdentifierRule(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientID, issuerID := metaConsent(t, "rt-tunnel")
	const identifier = "urn:gram:tunnel:routability"
	createTunneledMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "rt-tunnel-member", identifier, conv.ToNullUUID(issuerID), 0)

	grant(t, ctx, fx, clientID, "")
	expect(t, fx, 1, 1, false)
	grant(t, ctx, fx, clientID, identifier)
	expect(t, fx, 1, 1, false)
	grant(t, ctx, fx, clientID, "urn:gram:tunnel:other")
	expectAutoReconnect(t, fx, identifier)
	expect(t, fx, 0, 1, true)
}

// A provider no member claims keeps its stored status.
func TestServeConsent_MetaUnclaimedProviderKeepsStoredStatus(t *testing.T) {
	t.Parallel()
	ctx, fx, _, clientID, _ := metaConsent(t, "rt-unclaimed")
	grant(t, ctx, fx, clientID, "")
	expect(t, fx, 1, 1, false)
}

// Two grants naming one remote upstream make the runtime refuse both.
func TestServeConsent_DuplicateRemoteGrantsAskForReconnect(t *testing.T) {
	t.Parallel()
	ctx, fx, metaID, clientA, issuerA := metaConsent(t, "rt-dup-a")
	const upstream = "https://dup.example.com/mcp"
	clientB := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "rt-dup-b", "", []uuid.UUID{fx.shared})
	issuerB := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientB)
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "rt-dup-member-a", upstream, conv.ToNullUUID(issuerA), 0)
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaID, "rt-dup-member-b", upstream, conv.ToNullUUID(issuerB), 1)

	grant(t, ctx, fx, clientA, upstream)
	expect(t, fx, 1, 2, false)
	grant(t, ctx, fx, clientB, upstream)
	expect(t, fx, 0, 2, true)
}

// A single remote-backed server applies the rule against its one upstream.
func TestServeConsent_RemoteServerLegacyGrantAsksForReconnect(t *testing.T) {
	t.Parallel()
	const upstream = "https://single.example.com/mcp"
	ctx, fx := standaloneConsent(t, "rt-remote", upstream, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, upstream, slug, "public", issuerID)
		return server.ID
	})
	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "rt-remote", "", []uuid.UUID{fx.shared})

	grant(t, ctx, fx, clientID, "")
	expectAutoReconnect(t, fx, upstream)
	expect(t, fx, 0, 1, true)
	grant(t, ctx, fx, clientID, upstream)
	expect(t, fx, 1, 1, false)
}

// A tunneled server reads only the entry keyed by its own derived issuer, so
// another provider's grant is never judged.
func TestServeConsent_TunneledServerAppliesRuleToOwnIssuerOnly(t *testing.T) {
	t.Parallel()
	const identifier = "urn:gram:tunnel:single"
	var serverID uuid.UUID
	ctx, fx := standaloneConsent(t, "rt-tunnel", identifier, func(ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug string) uuid.UUID {
		serverID = createTunneledServer(t, ctx, ti, projectID, issuerID, slug, identifier)
		return serverID
	})
	own := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "rt-tunnel-own", "", []uuid.UUID{fx.shared})
	stampRemoteSessionIssuer(t, ctx, fx.ti.conn, fx.projectID, serverID, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, own)))
	other := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "rt-tunnel-other", "", []uuid.UUID{fx.shared})

	grant(t, ctx, fx, other, "https://unrelated.example.com/mcp")
	grant(t, ctx, fx, own, "")
	expect(t, fx, 2, 2, false)
	grant(t, ctx, fx, own, "urn:gram:tunnel:other")
	expect(t, fx, 1, 2, true)
}
