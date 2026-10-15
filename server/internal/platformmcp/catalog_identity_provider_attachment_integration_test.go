package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func attachmentTestIssuerMetadata(issuerURL string) remotesessions.DiscoveredIssuerMetadata {
	return remotesessions.DiscoveredIssuerMetadata{
		Issuer:                                     issuerURL,
		AuthorizationEndpoint:                      issuerURL + "/authorize",
		TokenEndpoint:                              issuerURL + "/token",
		RegistrationEndpoint:                       issuerURL + "/register",
		ScopesSupported:                            []string{"read"},
		GrantTypesSupported:                        []string{"authorization_code"},
		AuthorizationGrantProfilesSupported:        []string{"urn:ietf:params:oauth:grant-profile:id-jag"},
		ResponseTypesSupported:                     []string{"code"},
		TokenEndpointAuthMethodsSupported:          []string{"client_secret_basic"},
		CodeChallengeMethodsSupported:              []string{"S256"},
		ClientIDMetadataDocumentSupported:          false,
		UserinfoEndpoint:                           "",
		IntrospectionEndpoint:                      "",
		IntrospectionEndpointAuthMethodsSupported:  []string{},
		IDTokenSigningAlgValuesSupported:           []string{},
		ClaimsSupported:                            []string{},
		BackchannelLogoutSupported:                 false,
		AuthorizationResponseIssParameterSupported: false,
		Metadata:          []byte("{}"),
		UnreadableMessage: "",
		UnreadableURL:     "",
	}
}

// attachmentTestResource is a resource's RFC 9728 document naming itself.
func attachmentTestResource(resourceURL, name, policyURL string) wellknown.OAuthProtectedResourceMetadata {
	return wellknown.OAuthProtectedResourceMetadata{
		Resource:               resourceURL,
		AuthorizationServers:   []string{"https://auth.example.test"},
		ScopesSupported:        []string{"read"},
		BearerMethodsSupported: nil,
		ResourceDocumentation:  resourceURL + "/docs",
		ResourceName:           name,
		ResourcePolicyURI:      policyURL,
		ResourceTosURI:         "",
		Raw:                    nil,
	}
}

func attachmentTestService(t *testing.T, conn *pgxpool.Pool) *CatalogIdentityProviderAttachmentService {
	t.Helper()
	return &CatalogIdentityProviderAttachmentService{db: conn, identity: remotesessions.NewIdentityCommitter(testenv.NewLogger(t), conn, nil, audit.NewLogger(), nil, nil, nil, nil), policy: nil, serverURL: nil}
}

func attachmentTestUserSessionIssuer(t *testing.T, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		OrganizationID:     pgtype.Text{},
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func attachmentTestClients(t *testing.T, conn *pgxpool.Pool, principal Principal, project ResolvedProject, userSessionIssuerID uuid.UUID) []remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerRow {
	t.Helper()
	rows, err := remotesessionsrepo.New(conn).ListRemoteSessionClientsForUserSessionIssuer(t.Context(), remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           conv.ToNullUUID(project.ID),
		OrganizationID:      conv.ToPGText(principal.OrganizationID),
	})
	require.NoError(t, err)
	return rows
}

// attachmentTestCommit commits an attachment the way attachLocked does, with
// already-registered credentials standing in for dynamic registration: reuse a
// stored issuer when the flow may, otherwise create one in the commit.
func attachmentTestCommit(t *testing.T, service *CatalogIdentityProviderAttachmentService, principal Principal, project ResolvedProject, userSessionIssuerID uuid.UUID, mcpServerID uuid.NullUUID, metadata remotesessions.DiscoveredIssuerMetadata, clientID, resourceURL string, resource wellknown.OAuthProtectedResourceMetadata) error {
	t.Helper()
	ctx := t.Context()
	existing, reuse, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	provider := remotesessions.CreateProvider(discoveredIssuerParams(principal, project, uuid.New(), metadata))
	if reuse {
		provider = remotesessions.UseProvider(existing.ID)
	}
	commit := service.identity.Prepare(remotesessions.IdentityPlan{
		Scope: remotesessions.IdentityScope{
			OrganizationID:   principal.OrganizationID,
			ProjectID:        project.ID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			ActorDisplayName: nil,
		},
		UserSessionIssuerID: userSessionIssuerID,
		MCPServerID:         mcpServerID,
		Provider:            provider,
		Client: remotesessions.ManualClient(remotesessions.ClientCredentials{
			ClientID:                clientID,
			ClientSecret:            "",
			SecretExpiresAt:         pgtype.Timestamptz{},
			TokenEndpointAuthMethod: nil,
			Scope:                   nil,
			Audience:                nil,
		}),
		Bound:           remotesessions.ReuseBound,
		ResourceDisplay: &remotesessions.ResourceDisplay{ResourceURL: resourceURL, Metadata: resource},
	})
	if err := commit.Preflight(ctx); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	reg, err := commit.Register(ctx)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	tx, err := commit.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := commit.Lock(ctx, tx); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	if err := commit.Bind(ctx, tx, reg); err != nil {
		return fmt.Errorf("bind: %w", err)
	}
	if _, err := commit.Commit(ctx, tx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Two resources behind one authorization server share the issuer row, which
// therefore stays neutral; each resource's name and links live on its own
// client, so B's card never inherits A's.
func TestSharedAuthorizationServerDoesNotShareResourceBranding(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_shared_as")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)
	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	resourceA := attachmentTestResource("https://a.example.test/mcp", "Resource A", "https://a.example.test/policy")
	resourceB := attachmentTestResource("https://b.example.test/mcp", "Resource B", "https://b.example.test/policy")

	usiA := attachmentTestUserSessionIssuer(t, conn, project.ID)
	usiB := attachmentTestUserSessionIssuer(t, conn, project.ID)
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usiA, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata, "client-a", resourceA.Resource, resourceA))

	issuer, reuse, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	require.True(t, reuse, "one authorization server, one issuer")
	require.Equal(t, "Remote identity provider", issuer.Name.String)
	require.False(t, issuer.ServiceDocumentation.Valid)
	require.False(t, issuer.OpPolicyUri.Valid)
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usiB, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata, "client-b", resourceB.Resource, resourceB))

	clientsB := attachmentTestClients(t, conn, principal, project, usiB)
	require.Len(t, clientsB, 1)
	require.Equal(t, "Resource B", conv.FromPGTextOrEmpty[string](clientsB[0].ResourceName))
	require.Equal(t, "https://b.example.test/policy", conv.FromPGTextOrEmpty[string](clientsB[0].ResourcePolicyUri))
	require.Equal(t, resourceB.Resource, conv.FromPGTextOrEmpty[string](clientsB[0].ResourceIdentifier))
	require.NotEqual(t, "Resource A", conv.FromPGTextOrEmpty[string](clientsB[0].ResourceName))
	require.NotEqual(t, "https://a.example.test/policy", conv.FromPGTextOrEmpty[string](clientsB[0].ResourcePolicyUri))

	clientsA := attachmentTestClients(t, conn, principal, project, usiA)
	require.Len(t, clientsA, 1)
	require.Equal(t, "Resource A", conv.FromPGTextOrEmpty[string](clientsA[0].ResourceName))
}

// A document read from the origin-style well-known path may describe a
// sibling resource; its display members are not stored for this one.
func TestAttachmentStoresNothingFromMismatchedResourceDocument(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_mismatch")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)

	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	sibling := attachmentTestResource("https://example.test/mcp/a", "Resource A", "https://a.example.test/policy")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, attachmentTestIssuerMetadata("https://auth.example.test"), "client-b", "https://example.test/mcp/b", sibling))

	clients := attachmentTestClients(t, conn, principal, project, usi)
	require.Len(t, clients, 1)
	require.False(t, clients[0].ResourceIdentifier.Valid)
	require.False(t, clients[0].ResourceName.Valid)
	require.False(t, clients[0].ResourcePolicyUri.Valid)
}

// Re-attaching refreshes the existing client's display members instead of
// creating a second client.
func TestReattachRefreshesResourceDisplayOnExistingClient(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_reattach")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)

	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	first := attachmentTestResource("https://a.example.test/mcp", "Resource A", "https://a.example.test/policy")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata, "client-a", first.Resource, first))
	renamed := attachmentTestResource("https://a.example.test/mcp", "Resource A v2", "")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata, "client-a", renamed.Resource, renamed))

	clients := attachmentTestClients(t, conn, principal, project, usi)
	require.Len(t, clients, 1)
	require.Equal(t, "Resource A v2", conv.FromPGTextOrEmpty[string](clients[0].ResourceName))
	require.False(t, clients[0].ResourcePolicyUri.Valid, "a link the resource stopped advertising clears")
}

// A stored issuer bound to a tunnel is refused rather than reused: the
// registration this flow would hang off it goes out over direct egress, while
// every later refresh and revocation would ride the tunnel.
func TestAttachmentRefusesTunnelBoundIssuer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_tunnel_bound")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)
	metadata := attachmentTestIssuerMetadata("https://auth.private.test")

	issuer, err := remotesessionsrepo.New(conn).CreateRemoteSessionIssuer(ctx, discoveredIssuerParams(principal, project, uuid.New(), metadata))
	require.NoError(t, err)
	require.False(t, issuer.TunneledMcpServerID.Valid)

	tunnel, err := tunneledmcprepo.New(conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          project.ID,
		Name:               "private idp tunnel " + uuid.NewString(),
		KeyHash:            "attachment-key-hash-" + uuid.NewString(),
		KeyPrefix:          "attachment-key-prefix",
		ResourceIdentifier: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	// Every other column keeps its stored value under the query's three-state narg semantics.
	_, err = remotesessionsrepo.New(conn).UpdateRemoteSessionIssuer(ctx, remotesessionsrepo.UpdateRemoteSessionIssuerParams{
		ID:                  issuer.ID,
		ProjectID:           conv.ToNullUUID(project.ID),
		TunneledMcpServerID: conv.ToPGText(tunnel.ID.String()),
	})
	require.NoError(t, err)

	_, _, err = service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.ErrorIs(t, err, ErrIdentityProviderAttachmentConflict)
}

// The attachment restamps the MCP servers on the registration's user session
// issuer, so gateway token routing sees the new provider without waiting for
// an unrelated binding change to heal it.
func TestAttachmentStampsMCPServerRemoteSessionIssuer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_resync")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)
	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	serverID := attachmentTestMCPServer(t, conn, project.ID, usi)

	resource := attachmentTestResource("https://a.example.test/mcp", "Resource A", "")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, conv.ToNullUUID(serverID), metadata, "client-a", resource.Resource, resource))

	issuer, _, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(issuer.ID), server.RemoteSessionIssuerID)
}

// AIM-577: a client bound to the user session issuer is not on its own proof
// that the registration's server is attached. A cleared provider leaves
// exactly that state, and the server-level reconcile does not repair it on a
// shared issuer, so reporting success there would leave the server with no
// sign-in instead of reusing the client and configuring it.
func TestAttachmentIdempotencyRequiresTheServersOwnProvider(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_idempotency")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)
	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	serverID := attachmentTestMCPServer(t, conn, project.ID, usi)

	resource := attachmentTestResource("https://a.example.test/mcp", "Resource A", "")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, conv.ToNullUUID(serverID), metadata, "client-a", resource.Resource, resource))
	issuer, _, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)

	attached, err := service.matchingAttachment(ctx, principal.OrganizationID, project, usi, conv.ToNullUUID(issuer.ID), metadata.Issuer)
	require.NoError(t, err)
	require.True(t, attached, "the server signs in to the bound client's provider")

	// The provider is cleared, as turning User Identity off leaves it, while
	// the client stays bound for the other servers on the issuer.
	cleared, err := mcpserversrepo.New(conn).StampMCPServerRemoteSessionIssuer(ctx, mcpserversrepo.StampMCPServerRemoteSessionIssuerParams{
		RemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ID:                    serverID,
		ProjectID:             project.ID,
		UserSessionIssuerID:   conv.ToNullUUID(usi),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), cleared)

	attached, err = service.matchingAttachment(ctx, principal.OrganizationID, project, usi, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata.Issuer)
	require.NoError(t, err)
	require.False(t, attached, "a bound client the server does not name is not an attachment")

	// Running the attachment again reuses that client and configures the
	// server with it.
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, conv.ToNullUUID(serverID), metadata, "client-a", resource.Resource, resource))
	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(issuer.ID), server.RemoteSessionIssuerID)
}

// Several providers on one user session issuer belong to the several servers
// sharing it, so they are not an ambiguous attachment.
func TestAttachmentToleratesOtherProvidersOnTheIssuer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_siblings")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	serverID := attachmentTestMCPServer(t, conn, project.ID, usi)

	sibling := attachmentTestIssuerMetadata("https://sibling.example.test")
	siblingResource := attachmentTestResource("https://b.example.test/mcp", "Resource B", "")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, sibling, "client-b", siblingResource.Resource, siblingResource))

	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	attached, err := service.matchingAttachment(ctx, principal.OrganizationID, project, usi, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata.Issuer)
	require.NoError(t, err)
	require.False(t, attached, "another server's provider is not this attachment")

	resource := attachmentTestResource("https://a.example.test/mcp", "Resource A", "")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, conv.ToNullUUID(serverID), metadata, "client-a", resource.Resource, resource))
	issuer, _, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: project.ID})
	require.NoError(t, err)
	require.Equal(t, conv.ToNullUUID(issuer.ID), server.RemoteSessionIssuerID)
}

// The provider and client land in one transaction: a failure after the issuer
// insert leaves no issuer behind for the next attempt to trip over.
func TestAttachmentFailureLeavesNoIssuer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_atomic")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)
	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	testenv.RejectWritesTo(t, ctx, conn, "remote_session_clients")

	resource := attachmentTestResource("https://a.example.test/mcp", "Resource A", "")
	require.Error(t, attachmentTestCommit(t, service, principal, project, usi, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata, "client-a", resource.Resource, resource))

	_, reuse, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	require.False(t, reuse, "the issuer insert must roll back with the client insert")
}

func attachmentTestMCPServer(t *testing.T, conn *pgxpool.Pool, projectID, userSessionIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	slug := "attachment-" + uuid.NewString()[:8]
	remote, err := remotemcprepo.New(conn).CreateServer(t.Context(), remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          conv.ToPGText(slug),
		Slug:          conv.ToPGText(slug),
		TransportType: "streamable-http",
		Url:           "https://a.example.test/mcp",
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		UserSessionIssuerID: conv.ToNullUUID(userSessionIssuerID),
		RemoteMcpServerID:   conv.ToNullUUID(remote.ID),
		Visibility:          "private",
	})
	require.NoError(t, err)
	return server.ID
}

// attachmentTestCIMDMetadata is a provider with no registration_endpoint that
// supports Client ID Metadata Documents for public PKCE clients.
func attachmentTestCIMDMetadata(issuerURL string) remotesessions.DiscoveredIssuerMetadata {
	metadata := attachmentTestIssuerMetadata(issuerURL)
	metadata.RegistrationEndpoint = ""
	metadata.TokenEndpointAuthMethodsSupported = []string{"none"}
	metadata.ClientIDMetadataDocumentSupported = true
	return metadata
}

// attachmentTestRegister runs the attachment's own identity plan through
// registration and, when it is ready, commits it.
func attachmentTestRegister(t *testing.T, service *CatalogIdentityProviderAttachmentService, principal Principal, project ResolvedProject, userSessionIssuerID uuid.UUID, metadata remotesessions.DiscoveredIssuerMetadata, resource wellknown.OAuthProtectedResourceMetadata) remotesessions.Registration {
	t.Helper()
	ctx := t.Context()
	existing, reuse, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	commit := service.identity.Prepare(attachmentIdentityPlan(principal, project, uuid.New(), userSessionIssuerID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, metadata, existing, reuse, resource.Resource, resource, remotesessions.ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: resource.ScopesSupported, Live: true, UseDiscovered: true}))
	require.NoError(t, commit.Preflight(ctx))
	reg, err := commit.Register(ctx)
	require.NoError(t, err)
	if !reg.Ready() {
		return reg
	}
	tx, err := commit.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, commit.Lock(ctx, tx))
	require.NoError(t, commit.Bind(ctx, tx, reg))
	_, err = commit.Commit(ctx, tx)
	require.NoError(t, err)
	return reg
}

func attachmentTestCIMDService(t *testing.T, conn *pgxpool.Pool) *CatalogIdentityProviderAttachmentService {
	t.Helper()
	serverURL := &url.URL{Scheme: "https", Host: "app.example.com"}
	return &CatalogIdentityProviderAttachmentService{db: conn, identity: remotesessions.NewIdentityCommitter(testenv.NewLogger(t), conn, nil, audit.NewLogger(), serverURL, nil, nil, nil), policy: nil, serverURL: serverURL}
}

// A provider without dynamic registration that supports Client ID Metadata
// Documents is attached through one, as the dashboard's automatic setup does,
// instead of being refused as needing manual setup.
func TestAttachmentUsesClientIDMetadataDocumentWithoutDynamicRegistration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_cimd")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestCIMDService(t, conn)
	metadata := attachmentTestCIMDMetadata("https://auth.example.com")
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	resource := attachmentTestResource("https://mcp.example.com/mcp", "Example", "")

	reg := attachmentTestRegister(t, service, principal, project, usi, metadata, resource)
	require.True(t, reg.Ready())
	require.Equal(t, remotesessions.RegistrationCIMD, reg.Method)

	clients := attachmentTestClients(t, conn, principal, project, usi)
	require.Len(t, clients, 1)
	require.True(t, strings.HasPrefix(clients[0].ExternalClientID, "https://app.example.com/.well-known/oauth-client/"), clients[0].ExternalClientID)
	require.False(t, clients[0].ClientSecretEncrypted.Valid)
	require.Empty(t, clients[0].ClientScope, "the resource's advertised scopes are not copied onto the client")

	issuer, reuse, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	require.True(t, reuse, "a stored provider supporting only client ID metadata documents is reusable")
	require.False(t, issuer.RegistrationEndpoint.Valid, "no registration endpoint is stored for a provider that advertises none")
	require.True(t, issuer.ClientIDMetadataDocumentSupported)
}

// A provider offering neither dynamic registration nor Client ID Metadata
// Documents still needs manual setup and stores no client.
func TestAttachmentRequiresManualSetupWithoutAutomaticRegistration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_manual")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestCIMDService(t, conn)
	metadata := attachmentTestCIMDMetadata("https://auth.example.com")
	metadata.ClientIDMetadataDocumentSupported = false
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)

	reg := attachmentTestRegister(t, service, principal, project, usi, metadata, attachmentTestResource("https://mcp.example.com/mcp", "Example", ""))
	require.True(t, reg.ManualSetupRequired)
	require.ErrorIs(t, identityProviderRegistrationError(reg), ErrIdentityProviderAttachmentUnsupported)
	require.Empty(t, attachmentTestClients(t, conn, principal, project, usi))
}

// The attached provider keeps its discovered grant profiles and is stamped as
// discovered, as a dashboard create from a discovered draft is.
func TestAttachmentRecordsDiscoveredGrantProfiles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_profiles")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestCIMDService(t, conn)
	metadata := attachmentTestCIMDMetadata("https://auth.example.com")
	metadata.Metadata = []byte(`{"issuer":"https://auth.example.com"}`)
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)

	reg := attachmentTestRegister(t, service, principal, project, usi, metadata, attachmentTestResource("https://mcp.example.com/mcp", "Example", ""))
	require.True(t, reg.Ready())

	issuer, reuse, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	require.True(t, reuse)
	require.Equal(t, []string{"urn:ietf:params:oauth:grant-profile:id-jag"}, issuer.AuthorizationGrantProfilesSupported)
	require.True(t, issuer.MetadataFetchedAt.Valid, "attached provider is stamped as discovered")
	require.JSONEq(t, `{"issuer":"https://auth.example.com"}`, string(issuer.Metadata))
}

// Dynamic registration names the scopes the client's first login would
// request from the resource, while the client itself stores none.
func TestAttachmentRegistersDiscoveredScopesWithoutStoringThem(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_dcr_scope")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	var requested atomic.Value
	registration := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Scope string `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		requested.Store(body.Scope)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id":                  "dcr-scope-client",
			"client_secret":              "dcr-scope-secret",
			"token_endpoint_auth_method": "client_secret_basic",
		})
	}))
	t.Cleanup(registration.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	serverURL := &url.URL{Scheme: "https", Host: "app.example.com"}
	service := &CatalogIdentityProviderAttachmentService{db: conn, identity: remotesessions.NewIdentityCommitter(testenv.NewLogger(t), conn, testenv.NewEncryptionClient(t), audit.NewLogger(), serverURL, policy, nil, nil), policy: policy, serverURL: serverURL}
	metadata := attachmentTestIssuerMetadata(registration.URL)
	metadata.ScopesSupported = []string{"read", "openid"}
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	resource := attachmentTestResource("https://mcp.example.com/mcp", "Example", "")
	resource.ScopesSupported = []string{"files:read"}

	reg := attachmentTestRegister(t, service, principal, project, usi, metadata, resource)
	require.True(t, reg.Ready(), "manual=%v failure=%+v requested=%v", reg.ManualSetupRequired, reg.Failure, requested.Load())
	require.Equal(t, remotesessions.RegistrationDCR, reg.Method)
	require.Equal(t, "files:read", requested.Load(), "the resource's scopes as published, as the login requests")

	clients := attachmentTestClients(t, conn, principal, project, usi)
	require.Len(t, clients, 1)
	require.Empty(t, clients[0].ClientScope)
}

// signInRemovalIssuer is attachmentTestUserSessionIssuer with the
// organization recorded, which a registration's composite foreign key to its
// issuer requires.
func signInRemovalIssuer(t *testing.T, conn *pgxpool.Pool, principal Principal, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		OrganizationID:     conv.ToPGText(principal.OrganizationID),
		Slug:               "usi-removal-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

// signInRemovalRegistration seeds a registration bound to its own MCP server
// and user session issuer, which is what the sign-in removal acts on.
func signInRemovalRegistration(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, userSessionIssuerID, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	registration, err := platformrepo.New(conn).CreatePlatformMCPCatalogRegistration(ctx, platformrepo.CreatePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       "remote",
		CatalogProvider:  "direct-remote-url-v1",
		CatalogReference: "https://a.example.test/mcp",
		Status:           "registered",
		// Lifecycle actions match on the user that created the registration.
		UserID: conv.ToPGText(principal.UserID),
	})
	require.NoError(t, err)
	_, err = platformrepo.New(conn).UpdatePlatformMCPCatalogRegistrationComponents(ctx, platformrepo.UpdatePlatformMCPCatalogRegistrationComponentsParams{
		ID:                  registration.ID,
		OrganizationID:      principal.OrganizationID,
		ProjectID:           project.ID,
		Status:              "registered",
		UserSessionIssuerID: conv.ToNullUUID(userSessionIssuerID),
		McpServerID:         conv.ToNullUUID(serverID),
	})
	require.NoError(t, err)
	return registration.ID
}

// AIM-577: the agent confirms a sign-in removal after showing the user what
// the preview said. A confirmation that does not name the state it was agreed
// against must be refused, so a sign-in reconfigured in between is not turned
// off on the strength of an answer about the old one, and a confirmation
// cannot be replayed once it has been carried out.
func TestSignInRemovalRefusesAStalePreviewConfirmation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_sign_in_removal_stale")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(t, conn)
	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	usi := signInRemovalIssuer(t, conn, principal, project.ID)
	serverID := attachmentTestMCPServer(t, conn, project.ID, usi)
	registrationID := signInRemovalRegistration(t, ctx, conn, principal, project, usi, serverID)

	resource := attachmentTestResource("https://a.example.test/mcp", "Resource A", "")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, conv.ToNullUUID(serverID), metadata, "client-a", resource.Resource, resource))

	preview, err := service.PreviewSignInRemoval(ctx, principal, project, registrationID)
	require.NoError(t, err)
	require.True(t, preview.Configured)
	require.NotEmpty(t, preview.Version, "the preview must name the state it describes")

	// The sign-in is configured again between the preview and the
	// confirmation, which is a different answer to the same question.
	second := attachmentTestIssuerMetadata("https://auth-second.example.test")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, conv.ToNullUUID(serverID), second, "client-b", resource.Resource, resource))

	_, err = service.RemoveSignIn(ctx, principal, project, registrationID, preview.Version)
	require.ErrorIs(t, err, ErrSignInRemovalStale)
	after, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: project.ID})
	require.NoError(t, err)
	require.True(t, after.RemoteSessionIssuerID.Valid, "the refused confirmation must leave the sign-in configured")

	// A confirmation naming no state at all is refused for the same reason.
	_, err = service.RemoveSignIn(ctx, principal, project, registrationID, "")
	require.ErrorIs(t, err, ErrSignInRemovalStale)

	// Previewing again and confirming that is carried out, and the same
	// confirmation cannot then be replayed.
	fresh, err := service.PreviewSignInRemoval(ctx, principal, project, registrationID)
	require.NoError(t, err)
	require.NotEqual(t, preview.Version, fresh.Version)
	removal, err := service.RemoveSignIn(ctx, principal, project, registrationID, fresh.Version)
	require.NoError(t, err)
	require.True(t, removal.Removed)
	cleared, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: project.ID})
	require.NoError(t, err)
	require.False(t, cleared.RemoteSessionIssuerID.Valid)

	_, err = service.RemoveSignIn(ctx, principal, project, registrationID, fresh.Version)
	require.ErrorIs(t, err, ErrSignInRemovalStale, "a carried-out confirmation must not be replayable")
}
