package platformmcp

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
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
func attachmentTestCommit(t *testing.T, service *CatalogIdentityProviderAttachmentService, principal Principal, project ResolvedProject, userSessionIssuerID uuid.UUID, metadata remotesessions.DiscoveredIssuerMetadata, clientID, resourceURL string, resource wellknown.OAuthProtectedResourceMetadata) error {
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
		Provider:            provider,
		Client: remotesessions.ManualClient(remotesessions.ClientCredentials{
			ClientID:                clientID,
			ClientSecret:            "",
			SecretExpiresAt:         pgtype.Timestamptz{},
			TokenEndpointAuthMethod: nil,
			Scope:                   resource.ScopesSupported,
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
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usiA, metadata, "client-a", resourceA.Resource, resourceA))

	issuer, reuse, err := service.reusableIssuer(ctx, principal, project, metadata.Issuer)
	require.NoError(t, err)
	require.True(t, reuse, "one authorization server, one issuer")
	require.Equal(t, "Remote identity provider", issuer.Name.String)
	require.False(t, issuer.ServiceDocumentation.Valid)
	require.False(t, issuer.OpPolicyUri.Valid)
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usiB, metadata, "client-b", resourceB.Resource, resourceB))

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
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, attachmentTestIssuerMetadata("https://auth.example.test"), "client-b", "https://example.test/mcp/b", sibling))

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
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, metadata, "client-a", first.Resource, first))
	renamed := attachmentTestResource("https://a.example.test/mcp", "Resource A v2", "")
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, metadata, "client-a", renamed.Resource, renamed))

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
	require.NoError(t, attachmentTestCommit(t, service, principal, project, usi, metadata, "client-a", resource.Resource, resource))

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
	require.Error(t, attachmentTestCommit(t, service, principal, project, usi, metadata, "client-a", resource.Resource, resource))

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
