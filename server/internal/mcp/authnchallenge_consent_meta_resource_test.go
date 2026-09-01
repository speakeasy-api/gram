package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	access_repo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	tunneledmcp_repo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// consentTestTenant reads the project and organization the test service's auth
// context was seeded with.
func consentTestTenant(t *testing.T, ctx context.Context) (uuid.UUID, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID, authCtx.ActiveOrganizationID
}

// clientRemoteIssuerID reads back the remote_session_issuer a seeded client
// authenticates against, so a member can be stamped with the same row. Row
// identity is what the lookup matches on, not the issuer URL.
func clientRemoteIssuerID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID string, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	client, err := remotesessions_repo.New(conn).GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      projectID,
		OrganizationID: conv.ToPGText(organizationID),
	})
	require.NoError(t, err)
	return client.RemoteSessionClient.RemoteSessionIssuerID
}

// seededMetaMember identifies a seeded member so a test can disable, detach, or
// tombstone it after the fact.
type seededMetaMember struct {
	mcpServerID    uuid.UUID
	remoteServerID uuid.UUID
	memberID       uuid.UUID
}

// createMetaMember attaches a remote-backed member to metaServerID, carrying
// both its own user session issuer and the remote session issuer its upstream
// authenticates against — the pair the dropped exclusivity CHECK forbade.
func createMetaMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, remoteIssuerID uuid.NullUUID, sortOrder int32) seededMetaMember {
	t.Helper()
	return createMetaMemberWithVisibility(t, ctx, conn, projectID, metaServerID, slug, serverURL, remoteIssuerID, sortOrder, "public")
}

func createMetaMemberWithVisibility(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, remoteIssuerID uuid.NullUUID, sortOrder int32, visibility string) seededMetaMember {
	t.Helper()
	return createMetaMemberWithUpstreamIn(t, ctx, conn, projectID, projectID, metaServerID, slug, serverURL, remoteIssuerID, sortOrder, visibility)
}

// createMetaMemberWithUpstreamIn puts the member's remote_mcp_servers row in
// upstreamProjectID, normally its own project. Pointing it elsewhere reproduces
// a cross-tenant upstream: remote_mcp_server_id is a single-column FK, so
// nothing in the schema requires the two projects to agree.
func createMetaMemberWithUpstreamIn(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, upstreamProjectID, metaServerID uuid.UUID, slug, serverURL string, remoteIssuerID uuid.NullUUID, sortOrder int32, visibility string) seededMetaMember {
	t.Helper()

	remoteServer, err := remotemcp_repo.New(conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     upstreamProjectID,
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
		Visibility:          visibility,
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, conn, projectID)),
	})
	require.NoError(t, err)
	stampRemoteSessionIssuer(t, ctx, conn, projectID, mcpServer.ID, remoteIssuerID)

	return seededMetaMember{
		mcpServerID:    mcpServer.ID,
		remoteServerID: remoteServer.ID,
		memberID:       attachMetaMemberRow(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder),
	}
}

// createSiblingProject creates a second project in the same organization, so a
// test can plant a row that only the query's project predicates keep out.
func createSiblingProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()
	slug := "aim87-sibling-" + uuid.NewString()[:8]
	project, err := projects_repo.New(conn).CreateProject(ctx, projects_repo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return project.ID
}

// seedMetaMemberConnectGrant grants mcp:connect on one member to every user
// in the organization — the principal a consent subject resolves through, since
// the subject is a session user and a grant on its own URN would never load.
func seedMetaMemberConnectGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, mcpServerID uuid.UUID) {
	t.Helper()
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, mcpServerID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = access_repo.New(conn).UpsertPrincipalGrant(ctx, access_repo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   authz.AllUsersPrincipal(),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// stampRemoteSessionIssuer writes the column the way the binding resync does,
// after creation, since a new server has no client bindings yet. Insists it
// landed on exactly one row: a stamp that matched nothing would leave a negative
// test asserting the absence of a member it never created.
func stampRemoteSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, mcpServerID uuid.UUID, remoteIssuerID uuid.NullUUID) {
	t.Helper()
	if !remoteIssuerID.Valid {
		return
	}
	stamped, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: remoteIssuerID,
		ID:                    mcpServerID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped, "the issuer stamp must land on exactly one live server")
}

// createTunneledMetaMember attaches a tunneled member stamped with an issuer.
// Its upstream resource is the recorded resource identifier — empty
// resourceIdentifier records none, so the member claims its issuer's
// credential but qualifies it to nothing.
func createTunneledMetaMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, resourceIdentifier string, remoteIssuerID uuid.NullUUID, sortOrder int32) seededMetaMember {
	t.Helper()

	tunneled, err := tunneledmcp_repo.New(conn).CreateServer(ctx, tunneledmcp_repo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          projectID,
		Name:               slug,
		KeyHash:            "hash-" + slug,
		KeyPrefix:          "pfx",
		ResourceIdentifier: conv.ToPGTextEmpty(resourceIdentifier),
	})
	require.NoError(t, err)

	mcpServer, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		TunneledMcpServerID: conv.ToNullUUID(tunneled.ID),
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, conn, projectID)),
	})
	require.NoError(t, err)
	stampRemoteSessionIssuer(t, ctx, conn, projectID, mcpServer.ID, remoteIssuerID)

	return seededMetaMember{
		mcpServerID:    mcpServer.ID,
		remoteServerID: uuid.Nil,
		memberID:       attachMetaMemberRow(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder),
	}
}

func attachMetaMemberRow(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID, mcpServerID uuid.UUID, sortOrder int32) uuid.UUID {
	t.Helper()
	member, err := metamcp_repo.New(conn).CreateMetaMCPMember(ctx, metamcp_repo.CreateMetaMCPMemberParams{
		ProjectID:       projectID,
		MetaMcpServerID: metaServerID,
		McpServerID:     mcpServerID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)
	return member.ID
}

// createMetaServer creates the meta_mcp_servers row a meta MCP endpoint
// resolves through.
func createMetaServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, name string, userSessionIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	metaServer, err := metamcp_repo.New(conn).CreateMetaMCPServer(ctx, metamcp_repo.CreateMetaMCPServerParams{
		OrganizationID:      organizationID,
		ProjectID:           projectID,
		Name:                name,
		UserSessionIssuerID: conv.ToNullUUID(userSessionIssuerID),
	})
	require.NoError(t, err)
	return metaServer.ID
}

// seedMetaConsentEndpoint builds a meta MCP endpoint over one shared user
// session issuer, so every client the consent screen offers hangs off the
// meta MCP rather than a member — which is why the stored derivation finds
// nothing and the member lookup has to answer.
func seedMetaConsentEndpoint(t *testing.T, slug string) (context.Context, consentActionFixture, uuid.UUID) {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaServerID := createMetaServer(t, ctx, ti.conn, projectID, orgID, slug, shared)
	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, slug)
	endpoint.MetaMcpServerID = conv.ToNullUUID(metaServerID)

	return ctx, consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   uuid.Nil,
		clientB:   uuid.Nil,
		clientC:   uuid.Nil,
		clientD:   uuid.Nil,
	}, metaServerID
}

// The member whose upstream authenticates against the connecting client's
// authorization server is the one that qualifies the credential.
func TestServeConsentAction_MetaMCPConnectResolvesMemberByStoredIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-lookup-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-lookup", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	const matched = "https://matched.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-lookup-match", matched, conv.ToNullUUID(issuerID), 0)
	// A sibling on a different authorization server must not be chosen.
	other := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-lookup-other", "", []uuid.UUID{fx.shared})
	otherIssuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, other)
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-lookup-sibling", "https://sibling.example.com/mcp", conv.ToNullUUID(otherIssuerID), 1)

	loc := postConnectAction(t, fx, clientID)
	require.Equal(t, matched, loc.Query().Get("resource"), "the credential must be qualified to the member behind its own authorization server")
	require.Equal(t, matched, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource, "and the resolved member must survive onto the login state")
}

// An unstamped member cannot be matched, which is the pre-backfill state: the
// lookup finds nothing and the endpoint behaves exactly as it does today.
func TestServeConsentAction_MetaMCPConnectUnstampedMemberSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-null-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-null", "", []uuid.UUID{fx.shared})
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-null-member", "https://unstamped.example.com/mcp", uuid.NullUUID{}, 0)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a NULL remote_session_issuer_id must match nothing rather than matching everything")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// One authorization server fronting two members of the same meta MCP is
// unsupported: a grant records one resource per (subject, client), so there is
// no value that routes both correctly.
func TestServeConsentAction_MetaMCPConnectSharedIssuerSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-shared-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-shared", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-shared-a", "https://shared-a.example.com/mcp", conv.ToNullUUID(issuerID), 0)
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-shared-b", "https://shared-b.example.com/mcp", conv.ToNullUUID(issuerID), 1)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "two members behind one authorization server must fail closed, not pick one")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// Declining to choose between two members is a decision, not a miss. The plain
// shared-issuer test cannot reach this: here the client is also bound to a
// member's own issuer, so the fallback has a real answer and the guard is the
// only thing between it and the grant.
func TestServeConsentAction_MetaMCPConnectAmbiguityIsNotUndoneByTheFallback(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-ambig-fb-gw")

	// A member-owned issuer that the stored derivation can resolve on its own.
	memberIssuer := createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID)
	attachConsentRemoteMcpServer(t, ctx, fx.ti.conn, fx.projectID, memberIssuer, "aim87-ambig-fb-srv", consentUpstreamA)

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-ambig-fb", "", []uuid.UUID{fx.shared, memberIssuer})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	// Positive control: with no meta MCP member claiming the issuer, the
	// fallback does answer for this client.
	require.Equal(t, consentUpstreamA, postConnectAction(t, fx, clientID).Query().Get("resource"),
		"the stored derivation must be able to answer, or this test proves nothing")

	// Now two members behind that one authorization server.
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-ambig-fb-a", "https://ambig-a.example.com/mcp", conv.ToNullUUID(issuerID), 0)
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-ambig-fb-b", "https://ambig-b.example.com/mcp", conv.ToNullUUID(issuerID), 1)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "an ambiguous meta MCP must not fall back to a resource it already refused to choose")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A member the caller cannot see still claimed the credential, so the fallback
// must not qualify it to something else on that member's behalf.
func TestServeConsentAction_MetaMCPConnectInvisibleClaimBlocksTheFallback(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-invis-gw")

	memberIssuer := createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID)
	attachConsentRemoteMcpServer(t, ctx, fx.ti.conn, fx.projectID, memberIssuer, "aim87-invis-srv", consentUpstreamA)

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-invis", "", []uuid.UUID{fx.shared, memberIssuer})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	require.Equal(t, consentUpstreamA, postConnectAction(t, fx, clientID).Query().Get("resource"),
		"the stored derivation must be able to answer, or this test proves nothing")

	createMetaMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-invis-member", "https://invisible.example.com/mcp", conv.ToNullUUID(issuerID), 0, "private")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a member the subject cannot see still claims the issuer, so nothing weaker may qualify it")
}

// Two members fronting one URL are one routing destination, so they collapse
// rather than reading as ambiguous: routeUpstreamToken keys on the URL.
func TestServeConsentAction_MetaMCPConnectMembersSharingOneURLCollapse(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-dupe-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-dupe", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	const upstream = "https://dupe.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-dupe-a", upstream, conv.ToNullUUID(issuerID), 0)
	// The trailing slash is the same destination once routing trims it.
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-dupe-b", upstream+"/", conv.ToNullUUID(issuerID), 1)

	require.Equal(t, upstream, postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// A member the caller cannot connect to must neither claim their credential —
// the resolved URL is echoed back and sent to the authorization server — nor
// contest the claim of a member they can reach.
func TestServeConsentAction_MetaMCPConnectExcludesMembersTheSubjectCannotConnectTo(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-rbac-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-rbac", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	// Private, and the consent subject holds no mcp:connect on it. A denial, not a
	// missing AuthContext: a genuinely anonymous subject would carry none and
	// Require would answer CodeUnauthorized, which fails the consent instead of
	// excluding the member. Unreachable here — a meta endpoint always has an issuer,
	// so /authorize forces the IDP first.
	createMetaMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-rbac-private", "https://private.example.com/mcp", conv.ToNullUUID(issuerID), 0, "private")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "an unreachable member must not claim the credential")

	// The same member, now public, does resolve — so the exclusion above was
	// the authorization check and not the lookup failing to see it at all.
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-rbac-public", "https://public.example.com/mcp", conv.ToNullUUID(issuerID), 1)
	require.Equal(t, "https://public.example.com/mcp", postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// The exclusion above is per-subject, not a blanket refusal of private members:
// a subject that does hold mcp:connect has that member qualify its credential,
// and an unreachable private sibling neither claims it nor makes it ambiguous.
func TestServeConsentAction_MetaMCPConnectResolvesAPrivateMemberTheSubjectCanConnectTo(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-grant-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-grant", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	const granted = "https://granted.example.com/mcp"
	reachable := createMetaMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-grant-reachable", granted, conv.ToNullUUID(issuerID), 0, "private")
	// Behind the same authorization server and left ungranted, so it stays
	// unreachable throughout and must never contest the claim of the member the
	// subject can reach.
	createMetaMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-grant-unreachable", "https://ungranted.example.com/mcp", conv.ToNullUUID(issuerID), 1, "private")

	_, hasResource := postConnectAction(t, fx, clientID).Query()["resource"]
	require.False(t, hasResource, "neither member is reachable yet, so the grant below is what changes the answer")

	seedMetaMemberConnectGrant(t, ctx, fx.ti.conn, fx.orgID, reachable.mcpServerID)

	loc := postConnectAction(t, fx, clientID)
	require.Equal(t, granted, loc.Query().Get("resource"), "a private member the subject holds mcp:connect on must qualify the credential")
	require.Equal(t, granted, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A member that claimed the credential outranks the stored derivation even when
// the derivation has an answer of its own. The member matched this client's
// authorization server; the derivation only knows which servers the client is
// bound to, and letting it win would send the credential to an upstream that
// never claimed it.
func TestServeConsentAction_MetaMCPConnectMemberClaimOutranksTheFallback(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-precedence-gw")

	memberIssuer := createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID)
	attachConsentRemoteMcpServer(t, ctx, fx.ti.conn, fx.projectID, memberIssuer, "aim87-precedence-srv", consentUpstreamA)

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-precedence", "", []uuid.UUID{fx.shared, memberIssuer})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	// Positive control: the fallback has a real answer, and a different one.
	require.Equal(t, consentUpstreamA, postConnectAction(t, fx, clientID).Query().Get("resource"),
		"the stored derivation must be able to answer, or this test proves nothing")

	const claimed = "https://claimed.example.com/mcp"
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-precedence-member", claimed, conv.ToNullUUID(issuerID), 0)

	loc := postConnectAction(t, fx, clientID)
	require.Equal(t, claimed, loc.Query().Get("resource"), "the member that claimed the issuer must outrank the stored derivation")
	require.Equal(t, claimed, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource,
		"and the claim must survive onto the login state, not just the redirect")
}

// A disabled member does not exist for the serving path, so it must not
// qualify a credential either.
func TestServeConsentAction_MetaMCPConnectIgnoresDisabledMembers(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-disabled-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-disabled", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	createMetaMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-disabled-member", "https://disabled.example.com/mcp", conv.ToNullUUID(issuerID), 0, "disabled")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a disabled member must not qualify a credential")
}

// An unknown visibility must fail closed. The query excludes only the value it
// names, so a visibility added later reaches the authorization switch, and
// admitting it there would let a member nobody can evaluate claim a credential.
func TestServeConsentAction_MetaMCPConnectIgnoresUnknownVisibility(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-unknown-vis-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-unknown-vis", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	// mcp_servers.visibility is only constrained to be non-empty, so a value
	// outside the known set is storable exactly as a future migration would
	// introduce one.
	createMetaMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-unknown-vis-member", "https://unknown-visibility.example.com/mcp", conv.ToNullUUID(issuerID), 0, "unlisted")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a visibility the authorization switch cannot evaluate must not qualify a credential")
}

// The upstream URL must come from this project's own remote server row.
// remote_mcp_server_id is a single-column FK, so the schema permits a member
// pointing at another tenant's upstream and only the lookup's project predicate
// keeps that URL out of a credential's resource.
func TestServeConsentAction_MetaMCPConnectIgnoresAnotherProjectsUpstream(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-tenancy-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-tenancy", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	sibling := createSiblingProject(t, ctx, fx.ti.conn, fx.orgID)
	createMetaMemberWithUpstreamIn(t, ctx, fx.ti.conn, fx.projectID, sibling, metaServerID, "aim87-tenancy-foreign", "https://another-tenant.example.com/mcp", conv.ToNullUUID(issuerID), 0, "public")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "an upstream row owned by another project must not qualify this project's credential")

	// The same member with its upstream in this project does resolve, so the
	// exclusion above was the project predicate and not the stamp or the join.
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-tenancy-mine", "https://mine.example.com/mcp", conv.ToNullUUID(issuerID), 1)
	require.Equal(t, "https://mine.example.com/mcp", postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// Soft deletes on either side of the member join must stop it from qualifying
// credentials: the mcp_servers row (membership rows survive server deletion)
// and the remote_mcp_servers row each carry their own tombstone.
func TestServeConsentAction_MetaMCPConnectIgnoresTombstonedRows(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-dead-rows-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-dead-rows", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	const upstreamA = "https://dead-srv.example.com/mcp"
	memberA := createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-dead-srv-member", upstreamA, conv.ToNullUUID(issuerID), 0)
	require.Equal(t, upstreamA, postConnectAction(t, fx, clientID).Query().Get("resource"), "the member must resolve while it is live")

	_, err := mcpservers_repo.New(fx.ti.conn).DeleteMCPServer(ctx, mcpservers_repo.DeleteMCPServerParams{
		ID:        memberA.mcpServerID,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a tombstoned member server must stop qualifying credentials")

	// A second live member still resolves, so the exclusions here are the
	// tombstone predicates and not general breakage.
	const upstreamB = "https://dead-upstream.example.com/mcp"
	memberB := createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-dead-upstream-member", upstreamB, conv.ToNullUUID(issuerID), 1)
	require.Equal(t, upstreamB, postConnectAction(t, fx, clientID).Query().Get("resource"), "the member must resolve while its upstream is live")

	_, err = remotemcp_repo.New(fx.ti.conn).DeleteServer(ctx, remotemcp_repo.DeleteServerParams{
		ID:        memberB.remoteServerID,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)

	loc = postConnectAction(t, fx, clientID)
	_, hasResource = loc.Query()["resource"]
	require.False(t, hasResource, "a tombstoned upstream must stop qualifying credentials")
}

// requireConnectActionFailsClosed drives one client's connect action and
// asserts it errored before minting anything: no upstream redirect, no login
// state, and therefore no grant that could carry a wrongly derived resource.
func requireConnectActionFailsClosed(t *testing.T, fx consentActionFixture, clientID uuid.UUID) {
	t.Helper()

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", clientID.String())

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	err := fx.ti.service.ServeConsentAction(w, req, fx.endpoint)
	require.Error(t, err)
	require.ErrorContains(t, err, "derive client upstream resource")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	require.Empty(t, w.Header().Get("Location"), "fail closed: no upstream redirect may be minted")
}

// A member lookup that faults must fail the connect closed. Treating the fault
// as "no member claimed this" would hand the question to the weaker per-client
// derivation, which answers for a different reason entirely.
func TestServeConsentAction_MetaMCPConnectLookupErrorFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-lookup-fault-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-lookup-fault", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-lookup-fault-member", "https://lookup-fault.example.com/mcp", conv.ToNullUUID(issuerID), 0)

	// Break only the member lookup's table; everything the connect arm touches
	// before it is already seeded (per-test cloned DB, safe to mutate).
	_, err := fx.ti.conn.Exec(ctx, "ALTER TABLE meta_mcp_server_members RENAME TO meta_mcp_server_members_unavailable") //nolint:glint // notestingrawsql: deliberate DDL breakage to force a member-lookup DB error; not expressible as an SQLc query
	require.NoError(t, err)

	requireConnectActionFailsClosed(t, fx, clientID)
}

// A tunneled member with no recorded resource identifier still claims its
// issuer's credential — blocking any weaker derivation — but qualifies it to
// nothing, so the grant is minted unqualified and serving routes it by the
// member's own issuer identity instead.
func TestServeConsentAction_MetaMCPConnectTunneledMemberWithoutIdentifierSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-tunnel-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-tunnel", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	createTunneledMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-tunnel-member", "", conv.ToNullUUID(issuerID), 0)
	// A derivable per-client resource exists, so an omitted one proves the
	// claim happened and suppressed it — without this the assertion would
	// hold even if the tunneled member stopped claiming altogether.
	attachConsentRemoteMcpServer(t, ctx, fx.ti.conn, fx.projectID, fx.shared, "aim87-tunnel-fallback", "https://fallback.example.com/mcp")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a tunneled member recording no resource identifier qualifies the credential to nothing")
}

// A tunneled member with a recorded resource identifier claims its issuer's
// credential and qualifies it to that identifier, exactly as a remote member
// qualifies to its URL (AIM-151).
func TestServeConsentAction_MetaMCPConnectResolvesTunneledMemberIdentifier(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim151-tunnel-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim151-tunnel", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	createTunneledMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim151-tunnel-member", "https://tunneled.internal/mcp", conv.ToNullUUID(issuerID), 0)

	require.Equal(t, "https://tunneled.internal/mcp", postConnectAction(t, fx, clientID).Query().Get("resource"),
		"the credential must be qualified to the tunneled member's recorded resource identifier")
}

// A member of another meta MCP, on the same issuer, must not be reachable from
// this endpoint.
func TestServeConsentAction_MetaMCPConnectIgnoresAnotherMetasMembers(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-scope-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-scope", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	otherMeta := createMetaServer(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-scope-other", createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID))
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, otherMeta, "aim87-scope-elsewhere", "https://elsewhere.example.com/mcp", conv.ToNullUUID(issuerID), 0)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "the lookup must be scoped to this meta MCP's members")

	// An equivalent member attached to this meta MCP does resolve, so the
	// exclusion above was the scoping and not the stamp being unreadable.
	createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-scope-mine", "https://mine.example.com/mcp", conv.ToNullUUID(issuerID), 1)
	require.Equal(t, "https://mine.example.com/mcp", postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// A non-meta MCP endpoint keeps the stored per-client derivation untouched.
func TestServeConsentAction_NonMetaEndpointKeepsPerClientDerivation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	memberIssuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, memberIssuer, "aim87-plain-srv", consentUpstreamA)

	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, "aim87-plain")
	fx := consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   uuid.Nil,
		clientB:   uuid.Nil,
		clientC:   uuid.Nil,
		clientD:   uuid.Nil,
	}

	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "aim87-plain", "", []uuid.UUID{shared, memberIssuer})
	require.Equal(t, consentUpstreamA, postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// The sync and the lookup meet only in production: every other test stamps the
// member column through a fixture. Here the real resync derives it from a
// client binding, and consent must resolve the member from what it wrote.
func TestServeConsentAction_MetaMCPConnectResolvesFromTheRealResync(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "aim87-composed-gw")
	projectID, orgID := consentTestTenant(t, ctx)

	// Unstamped on purpose: only the resync may write the column here.
	member := createMetaMember(t, ctx, fx.ti.conn, projectID, metaServerID, "aim87-composed-member", "https://aim87-composed.example.com/mcp", uuid.NullUUID{}, 0)

	// The meta MCP's connecting client, bound to the shared issuer.
	gatewayClientID := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "aim87-composed", "", []uuid.UUID{fx.shared})
	rsiID := clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, gatewayClientID)

	// The member's own identity-provider config: a client on the same
	// authorization server, bound to the member's own user session issuer.
	memberServer, err := mcpservers_repo.New(fx.ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        member.mcpServerID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.True(t, memberServer.UserSessionIssuerID.Valid)
	memberIssuerID := memberServer.UserSessionIssuerID.UUID

	memberClient, err := remotesessions_repo.New(fx.ti.conn).CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(orgID),
		RemoteSessionIssuerID: rsiID,
		ClientID:              "aim87-composed-member-client",
	})
	require.NoError(t, err)
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: memberClient.ID,
		UserSessionIssuerID:   memberIssuerID,
	}))

	// The production write path.
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, fx.ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))

	loc := postConnectAction(t, fx, gatewayClientID)
	require.Equal(t, "https://aim87-composed.example.com/mcp", loc.Query().Get("resource"),
		"the member must resolve from the value the resync wrote, not from any fixture")
}
