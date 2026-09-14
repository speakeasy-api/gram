package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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

func attachmentTestService(conn *pgxpool.Pool) *CatalogIdentityProviderAttachmentService {
	return &CatalogIdentityProviderAttachmentService{db: conn, enc: nil, policy: nil, audit: audit.NewLogger(), serverURL: nil}
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

// Two resources behind one authorization server share the issuer row, which
// therefore stays neutral; each resource's name and links live on its own
// client, so B's card never inherits A's.
func TestSharedAuthorizationServerDoesNotShareResourceBranding(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_attachment_shared_as")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service := attachmentTestService(conn)
	metadata := attachmentTestIssuerMetadata("https://auth.example.test")
	resourceA := attachmentTestResource("https://a.example.test/mcp", "Resource A", "https://a.example.test/policy")
	resourceB := attachmentTestResource("https://b.example.test/mcp", "Resource B", "https://b.example.test/policy")

	issuerA, err := service.ensureIssuer(ctx, principal, project, uuid.New(), metadata)
	require.NoError(t, err)
	issuerB, err := service.ensureIssuer(ctx, principal, project, uuid.New(), metadata)
	require.NoError(t, err)
	require.Equal(t, issuerA.ID, issuerB.ID, "one authorization server, one issuer")
	require.Equal(t, "Remote identity provider", issuerB.Name.String)
	require.False(t, issuerB.ServiceDocumentation.Valid)
	require.False(t, issuerB.OpPolicyUri.Valid)

	usiA := attachmentTestUserSessionIssuer(t, conn, project.ID)
	usiB := attachmentTestUserSessionIssuer(t, conn, project.ID)
	registeredA := remotesessions.ProxyRegisterResponse{ClientID: "client-a", ClientSecret: "", ClientSecretExpiresAt: pgtype.Timestamptz{}, TokenEndpointAuthMethod: ""}
	registeredB := remotesessions.ProxyRegisterResponse{ClientID: "client-b", ClientSecret: "", ClientSecretExpiresAt: pgtype.Timestamptz{}, TokenEndpointAuthMethod: ""}
	attachedA, err := service.createAndAttachClient(ctx, principal, project, usiA, issuerA.ID, registeredA, resourceA.Resource, resourceA)
	require.NoError(t, err)
	require.True(t, attachedA)
	attachedB, err := service.createAndAttachClient(ctx, principal, project, usiB, issuerB.ID, registeredB, resourceB.Resource, resourceB)
	require.NoError(t, err)
	require.True(t, attachedB)

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
	service := attachmentTestService(conn)

	issuer, err := service.ensureIssuer(ctx, principal, project, uuid.New(), attachmentTestIssuerMetadata("https://auth.example.test"))
	require.NoError(t, err)
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	sibling := attachmentTestResource("https://example.test/mcp/a", "Resource A", "https://a.example.test/policy")
	registered := remotesessions.ProxyRegisterResponse{ClientID: "client-b", ClientSecret: "", ClientSecretExpiresAt: pgtype.Timestamptz{}, TokenEndpointAuthMethod: ""}
	attached, err := service.createAndAttachClient(ctx, principal, project, usi, issuer.ID, registered, "https://example.test/mcp/b", sibling)
	require.NoError(t, err)
	require.True(t, attached)

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
	service := attachmentTestService(conn)

	issuer, err := service.ensureIssuer(ctx, principal, project, uuid.New(), attachmentTestIssuerMetadata("https://auth.example.test"))
	require.NoError(t, err)
	usi := attachmentTestUserSessionIssuer(t, conn, project.ID)
	registered := remotesessions.ProxyRegisterResponse{ClientID: "client-a", ClientSecret: "", ClientSecretExpiresAt: pgtype.Timestamptz{}, TokenEndpointAuthMethod: ""}
	first := attachmentTestResource("https://a.example.test/mcp", "Resource A", "https://a.example.test/policy")
	_, err = service.createAndAttachClient(ctx, principal, project, usi, issuer.ID, registered, first.Resource, first)
	require.NoError(t, err)
	renamed := attachmentTestResource("https://a.example.test/mcp", "Resource A v2", "")
	_, err = service.createAndAttachClient(ctx, principal, project, usi, issuer.ID, registered, renamed.Resource, renamed)
	require.NoError(t, err)

	clients := attachmentTestClients(t, conn, principal, project, usi)
	require.Len(t, clients, 1)
	require.Equal(t, "Resource A v2", conv.FromPGTextOrEmpty[string](clients[0].ResourceName))
	require.False(t, clients[0].ResourcePolicyUri.Valid, "a link the resource stopped advertising clears")
}
