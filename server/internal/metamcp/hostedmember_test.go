package metamcp_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// fakeProvider is an MCP resource server plus its authorization server on one
// TLS origin: RFC 9728 protected-resource metadata under /mcp, RFC 8414
// authorization-server metadata, and an RFC 7591 registration endpoint.
type fakeProvider struct {
	server        *httptest.Server
	registrations atomic.Int32
	registerCode  int
	publicClient  bool
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()

	p := &fakeProvider{registerCode: http.StatusCreated}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"resource":              p.resourceURL(),
			"authorization_servers": []string{p.issuer()},
			"scopes_supported":      []string{"read"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"issuer":                                p.issuer(),
			"authorization_endpoint":                p.issuer() + "/authorize",
			"token_endpoint":                        p.issuer() + "/token",
			"registration_endpoint":                 p.issuer() + "/register",
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "none"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		p.registrations.Add(1)
		if p.registerCode != http.StatusCreated {
			w.WriteHeader(p.registerCode)
			_, _ = w.Write([]byte(`{"error":"invalid_client_metadata"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		if p.publicClient {
			writeJSON(t, w, map[string]any{
				"client_id":                  "hosted-member-public-client",
				"token_endpoint_auth_method": "none",
			})
			return
		}
		writeJSON(t, w, map[string]any{
			"client_id":                  "hosted-member-client",
			"client_secret":              "hosted-member-secret",
			"token_endpoint_auth_method": "client_secret_basic",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	p.server = httptest.NewTLSServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeProvider) issuer() string      { return p.server.URL }
func (p *fakeProvider) resourceURL() string { return p.server.URL + "/mcp" }

func (p *fakeProvider) policy(t *testing.T) *guardian.Policy {
	t.Helper()

	pool := x509.NewCertPool()
	pool.AddCert(p.server.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(pool))
	require.NoError(t, err)
	return policy
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

// newTestServiceWithProviders builds the service with the real platform
// attachment service reaching the fake provider through policy.
func newTestServiceWithProviders(t *testing.T, policy *guardian.Policy) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

	auditLogger := audit.NewLogger()
	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)
	providers := platformmcp.NewCatalogIdentityProviderAttachmentService(conn, testenv.NewEncryptionClient(t), policy, auditLogger, serverURL)
	svc := metamcp.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), auditLogger, nil, providers)

	return ctx, &testInstance{service: svc, conn: conn, sessionManager: sessionManager}
}

type externalMCPTool struct {
	slug          string
	remoteURL     string
	issuer        string
	requiresOAuth bool
}

// seedHostedServer creates a toolset whose latest version selects the given
// external MCP tools and an issuer-less mcp_servers row fronting it, as the
// gateway add-member picker does.
func seedHostedServer(t *testing.T, ctx context.Context, ti *testInstance, tools ...externalMCPTool) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	deploymentID, err := deploymentsrepo.New(ti.conn).InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)
	require.NoError(t, deploymentsrepo.New(ti.conn).CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	extRepo := externalmcprepo.New(ti.conn)
	toolURNs := make([]urn.Tool, 0, len(tools))
	for _, tool := range tools {
		attachment, err := extRepo.CreateExternalMCPAttachment(ctx, externalmcprepo.CreateExternalMCPAttachmentParams{
			DeploymentID:            deploymentID,
			RegistryID:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Name:                    tool.slug,
			Slug:                    tool.slug,
			RegistryServerSpecifier: tool.slug,
		})
		require.NoError(t, err)

		toolURN := urn.NewTool(urn.ToolKindExternalMCP, tool.slug, "call")
		toolURNs = append(toolURNs, toolURN)
		_, err = extRepo.CreateExternalMCPToolDefinition(ctx, externalmcprepo.CreateExternalMCPToolDefinitionParams{
			ExternalMcpAttachmentID:    attachment.ID,
			ToolUrn:                    toolURN.String(),
			Type:                       "direct",
			Name:                       pgtype.Text{String: tool.slug, Valid: true},
			Description:                pgtype.Text{String: "proxy tool", Valid: true},
			Schema:                     []byte(`{"type":"object"}`),
			RemoteUrl:                  tool.remoteURL,
			TransportType:              externalmcptypes.TransportTypeStreamableHTTP,
			RequiresOauth:              tool.requiresOAuth,
			OauthVersion:               "2.1",
			OauthAuthorizationEndpoint: conv.ToPGText(tool.issuer + "/authorize"),
			OauthTokenEndpoint:         conv.ToPGText(tool.issuer + "/token"),
			OauthRegistrationEndpoint:  conv.ToPGText(tool.issuer + "/register"),
			OauthScopesSupported:       []string{},
			HeaderDefinitions:          nil,
			Title:                      pgtype.Text{},
			ReadOnlyHint:               pgtype.Bool{},
			DestructiveHint:            pgtype.Bool{},
			IdempotentHint:             pgtype.Bool{},
			OpenWorldHint:              pgtype.Bool{},
		})
		require.NoError(t, err)
	}

	toolsetID := seedToolsetBackend(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID)
	_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolsetID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	return seedHostedRow(t, ctx, ti, projectID, toolsetID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
}

// seedHostedRow creates an mcp_servers row fronting toolsetID; issuer-less
// unless issuerID is set.
func seedHostedRow(t *testing.T, ctx context.Context, ti *testInstance, projectID, toolsetID uuid.UUID, issuerID uuid.NullUUID) uuid.UUID {
	t.Helper()

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             projectID,
		Name:                  conv.ToPGText("hosted member"),
		Slug:                  conv.ToPGText("hosted-member-" + uuid.NewString()[:8]),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   issuerID,
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             conv.ToNullUUID(toolsetID),
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
	})
	require.NoError(t, err)
	return server.ID
}

// seedProviderIssuer inserts a remote_session_issuer for a specific
// authorization server URL.
func seedProviderIssuer(t *testing.T, ctx context.Context, ti *testInstance, issuerURL string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "provider-" + uuid.NewString()[:8],
		Issuer:                            issuerURL,
		Name:                              conv.ToPGTextEmpty(""),
		LogoAssetID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientSetupDocumentationUrl:       conv.ToPGTextEmpty(""),
		AuthorizationEndpoint:             conv.ToPGText(issuerURL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(issuerURL + "/token"),
		RevocationEndpoint:                conv.ToPGTextEmpty(""),
		RegistrationEndpoint:              conv.ToPGText(issuerURL + "/register"),
		JwksUri:                           conv.ToPGTextEmpty(""),
		ServiceDocumentation:              conv.ToPGTextEmpty(""),
		OpPolicyUri:                       conv.ToPGTextEmpty(""),
		OpTosUri:                          conv.ToPGTextEmpty(""),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ClientIDMetadataDocumentSupported: false,
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)
	return issuer.ID
}

func loadServer(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) mcpserversrepo.McpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	return server
}

func listMembers(t *testing.T, ctx context.Context, ti *testInstance, metaID string) []metamcprepo.ListMetaMCPMembersRow {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	members, err := metamcprepo.New(ti.conn).ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{
		MetaMcpServerID: uuid.MustParse(metaID),
		ProjectID:       *authCtx.ProjectID,
	})
	require.NoError(t, err)
	return members
}

func addMember(ctx context.Context, ti *testInstance, metaID string, serverID uuid.UUID) error {
	_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  metaID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	if err != nil {
		return fmt.Errorf("add meta mcp member: %w", err)
	}
	return nil
}

func TestAddMetaMcpMember_HostedMemberWiresUpstreamProvider(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	meta := seedMetaMcpServer(t, ctx, ti, "hosted oauth host")
	gatewayIssuerID := uuid.MustParse(*meta.UserSessionIssuerID)
	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "notes", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})

	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))

	server := loadServer(t, ctx, ti, serverID)
	require.False(t, server.UserSessionIssuerID.Valid, "the hosted server's own endpoint stays as it was")
	require.True(t, server.RemoteSessionIssuerID.Valid, "provider issuer is stamped for member token routing")
	require.Equal(t, int32(1), provider.registrations.Load(), "one dynamic client registration")

	rsRepo := remotesessionsrepo.New(ti.conn)
	issuer, err := rsRepo.GetRemoteSessionIssuerByID(ctx, remotesessionsrepo.GetRemoteSessionIssuerByIDParams{
		ID:                    server.RemoteSessionIssuerID.UUID,
		ProjectID:             conv.ToNullUUID(projectID),
		IncludeOrganizational: false,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:         false,
	})
	require.NoError(t, err)
	require.Equal(t, strings.TrimRight(provider.issuer(), "/"), strings.TrimRight(issuer.Issuer, "/"))
	require.Equal(t, provider.issuer()+"/register", issuer.RegistrationEndpoint.String)

	gatewayClients, err := rsRepo.ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: gatewayIssuerID,
		ProjectID:           conv.ToNullUUID(projectID),
		OrganizationID:      conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.Len(t, gatewayClients, 1, "registered client is offered on the gateway's consent page")
	require.Equal(t, "hosted-member-client", gatewayClients[0].ExternalClientID)
	require.Equal(t, server.RemoteSessionIssuerID.UUID, gatewayClients[0].RemoteSessionIssuerID)

	members := listMembers(t, ctx, ti, meta.ID)
	require.Len(t, members, 1)

	// Removal unbinds the provider from the gateway; re-adding rebinds the
	// existing registration instead of registering another.
	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               members[0].ID.String(),
	}))
	require.Equal(t, 0, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, gatewayIssuerID, server.RemoteSessionIssuerID.UUID))

	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))
	require.Equal(t, int32(1), provider.registrations.Load(), "existing registration is reused")
	require.Equal(t, 1, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, gatewayIssuerID, server.RemoteSessionIssuerID.UUID))
	require.Equal(t, server.RemoteSessionIssuerID, loadServer(t, ctx, ti, serverID).RemoteSessionIssuerID)
}

func TestAddMetaMcpMember_HostedMemberAcceptsPublicClient(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	provider.publicClient = true
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))

	meta := seedMetaMcpServer(t, ctx, ti, "hosted public host")
	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "public-client", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})

	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))
	require.True(t, loadServer(t, ctx, ti, serverID).RemoteSessionIssuerID.Valid)
	require.Equal(t, int32(1), provider.registrations.Load())
}

func TestAddMetaMcpMember_HostedMemberWithoutOAuthToolsIsUntouched(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))

	meta := seedMetaMcpServer(t, ctx, ti, "hosted plain host")
	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "public", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: false})

	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))

	server := loadServer(t, ctx, ti, serverID)
	require.False(t, server.UserSessionIssuerID.Valid)
	require.False(t, server.RemoteSessionIssuerID.Valid)
	require.Equal(t, int32(0), provider.registrations.Load())
}

func TestAddMetaMcpMember_ProxiedMemberIgnoresProviderWiring(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "remote host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))
	require.Equal(t, int32(0), provider.registrations.Load())
	require.Len(t, listMembers(t, ctx, ti, meta.ID), 1)
}

func TestAddMetaMcpMember_HostedMemberRegistrationRefusedFailsClosed(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	provider.registerCode = http.StatusBadRequest
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))

	meta := seedMetaMcpServer(t, ctx, ti, "hosted refused host")
	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "refused", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})

	err := addMember(ctx, ti, meta.ID, serverID)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), provider.resourceURL())
	require.Empty(t, listMembers(t, ctx, ti, meta.ID), "a member that cannot route a credential is not added")

	server := loadServer(t, ctx, ti, serverID)
	require.False(t, server.UserSessionIssuerID.Valid)
	require.False(t, server.RemoteSessionIssuerID.Valid)
}

func TestAddMetaMcpMember_HostedMemberRegistrationOutageIsRetryable(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	provider.registerCode = http.StatusServiceUnavailable
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))

	meta := seedMetaMcpServer(t, ctx, ti, "hosted outage host")
	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "outage", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})

	requireOopsCode(t, addMember(ctx, ti, meta.ID, serverID), oops.CodeUnavailable)
	require.Empty(t, listMembers(t, ctx, ti, meta.ID))

	provider.registerCode = http.StatusCreated
	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))
	require.Len(t, listMembers(t, ctx, ti, meta.ID), 1)
}

func TestAddMetaMcpMember_HostedMemberRejectedAddKeepsGatewayClean(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "unknown-meta", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})
	requireOopsCode(t, addMember(ctx, ti, uuid.NewString(), serverID), oops.CodeNotFound)
	require.Equal(t, int32(0), provider.registrations.Load(), "no provider traffic without a gateway to bind to")

	// The add transaction rejects a second hosted server on the same toolset;
	// the binding the first member owns survives, nothing else is left behind.
	meta := seedMetaMcpServer(t, ctx, ti, "hosted shared backend host")
	gatewayIssuerID := uuid.MustParse(*meta.UserSessionIssuerID)
	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))
	first := loadServer(t, ctx, ti, serverID)

	twinID := seedHostedRow(t, ctx, ti, projectID, first.ToolsetID.UUID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	requireOopsCode(t, addMember(ctx, ti, meta.ID, twinID), oops.CodeConflict)
	require.Equal(t, int32(1), provider.registrations.Load(), "the existing registration is reused")
	require.Equal(t, 1, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, gatewayIssuerID, first.RemoteSessionIssuerID.UUID))
	require.Len(t, listMembers(t, ctx, ti, meta.ID), 1)
}

func TestAddMetaMcpMember_HostedMemberSeveralProvidersRejected(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))

	meta := seedMetaMcpServer(t, ctx, ti, "hosted multi host")
	serverID := seedHostedServer(t, ctx, ti,
		externalMCPTool{slug: "first", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true},
		externalMCPTool{slug: "second", remoteURL: "https://other.example.test/mcp", issuer: "https://other.example.test", requiresOAuth: true},
	)

	err := addMember(ctx, ti, meta.ID, serverID)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "several OAuth upstreams")
	require.Equal(t, int32(0), provider.registrations.Load())
	require.False(t, loadServer(t, ctx, ti, serverID).RemoteSessionIssuerID.Valid)
}

func TestAddMetaMcpMember_HostedMemberConflictsWithProxiedMemberOnSameProvider(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	meta := seedMetaMcpServer(t, ctx, ti, "hosted shared provider host")

	// A remote member already authenticates with the fake provider.
	providerIssuerID := seedProviderIssuer(t, ctx, ti, provider.issuer())
	remoteID := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, remoteID, providerIssuerID, createRemoteSessionClient(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, providerIssuerID, "remote-member-client"))
	require.NoError(t, addMember(ctx, ti, meta.ID, remoteID))

	hostedID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "shared", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})
	err := addMember(ctx, ti, meta.ID, hostedID)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "one credential per provider")
	require.Equal(t, int32(0), provider.registrations.Load(), "the project's existing registration is reused, never duplicated")
	require.Len(t, listMembers(t, ctx, ti, meta.ID), 1)
}

func TestAddMetaMcpMember_HostedMemberWithoutAttacherIsRefused(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestService(t)

	meta := seedMetaMcpServer(t, ctx, ti, "hosted no attacher host")
	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "orphan", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})

	requireOopsCode(t, addMember(ctx, ti, meta.ID, serverID), oops.CodeInvalid)
	require.Equal(t, int32(0), provider.registrations.Load())
	require.Empty(t, listMembers(t, ctx, ti, meta.ID))
}

func TestAddMetaMcpMember_HostedMemberWithIssuerBindsOnItsOwnIssuer(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	meta := seedMetaMcpServer(t, ctx, ti, "hosted gated host")
	gatewayIssuerID := uuid.MustParse(*meta.UserSessionIssuerID)
	orphanID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "gated", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})
	toolsetID := loadServer(t, ctx, ti, orphanID).ToolsetID.UUID
	memberIssuerID := seedUserSessionIssuer(t, ctx, ti.conn, projectID)
	serverID := seedHostedRow(t, ctx, ti, projectID, toolsetID, conv.ToNullUUID(memberIssuerID))

	require.NoError(t, addMember(ctx, ti, meta.ID, serverID))

	server := loadServer(t, ctx, ti, serverID)
	require.Equal(t, memberIssuerID, server.UserSessionIssuerID.UUID, "existing issuer is kept")
	require.True(t, server.RemoteSessionIssuerID.Valid, "derived from the member issuer's binding")
	memberClients, err := remotesessionsrepo.New(ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessionsrepo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: memberIssuerID,
		ProjectID:           conv.ToNullUUID(projectID),
		OrganizationID:      conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.Len(t, memberClients, 1, "client is bound to the member's own issuer")
	require.Equal(t, 1, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, gatewayIssuerID, server.RemoteSessionIssuerID.UUID), "auto-attach copies it onto the gateway")
}

func TestAddMetaMcpMember_ProxiedMemberConflictsWithHostedOnSameProvider(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	meta := seedMetaMcpServer(t, ctx, ti, "hosted first host")
	hostedID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "first", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})
	require.NoError(t, addMember(ctx, ti, meta.ID, hostedID))
	providerIssuerID := loadServer(t, ctx, ti, hostedID).RemoteSessionIssuerID.UUID

	remoteID := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, remoteID, providerIssuerID, createRemoteSessionClient(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, providerIssuerID, "late-remote-client"))

	err := addMember(ctx, ti, meta.ID, remoteID)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "one credential per provider")
	require.Len(t, listMembers(t, ctx, ti, meta.ID), 1)
}

func TestAddMetaMcpMember_HostedMemberUnbindsSharedClientOnConflict(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	meta := seedMetaMcpServer(t, ctx, ti, "hosted shared client host")
	gatewayIssuerID := uuid.MustParse(*meta.UserSessionIssuerID)

	// A remote server outside the gateway already holds the project's client
	// for the provider, so any grant on that client is qualified to its URL.
	providerIssuerID := seedProviderIssuer(t, ctx, ti, provider.issuer())
	remoteID := seedMcpServer(t, ctx, ti.conn, projectID)
	stampAndWireMemberClient(t, ctx, ti.conn, projectID, remoteID, providerIssuerID, createRemoteSessionClient(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, providerIssuerID, "shared-client"))

	hostedID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "shared", remoteURL: provider.resourceURL(), issuer: provider.issuer(), requiresOAuth: true})
	err := addMember(ctx, ti, meta.ID, hostedID)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "sharing the same OAuth client")
	require.Equal(t, int32(0), provider.registrations.Load(), "the existing client was reused rather than re-registered")
	require.Equal(t, 0, countGatewayClientsForUpstream(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, gatewayIssuerID, providerIssuerID), "the binding this add created is removed again")
	require.Empty(t, listMembers(t, ctx, ti, meta.ID))
	require.False(t, loadServer(t, ctx, ti, hostedID).RemoteSessionIssuerID.Valid)
}

func TestAddMetaMcpMember_HostedMemberCleartextUpstreamRejected(t *testing.T) {
	t.Parallel()

	provider := newFakeProvider(t)
	ctx, ti := newTestServiceWithProviders(t, provider.policy(t))

	meta := seedMetaMcpServer(t, ctx, ti, "hosted cleartext host")
	serverID := seedHostedServer(t, ctx, ti, externalMCPTool{slug: "plain", remoteURL: "http://insecure.example.test/mcp", issuer: "http://insecure.example.test", requiresOAuth: true})

	err := addMember(ctx, ti, meta.ID, serverID)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "must use https")
	require.Equal(t, int32(0), provider.registrations.Load())
}
