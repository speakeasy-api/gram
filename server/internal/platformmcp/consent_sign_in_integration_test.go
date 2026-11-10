package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// recordingAttachment stands in for the provider attachment, which discovers
// and registers with the upstream over the network.
type recordingAttachment struct {
	mu    sync.Mutex
	calls []uuid.UUID
	err   error
}

func (a *recordingAttachment) Attach(_ context.Context, _ Principal, _ ResolvedProject, registrationID uuid.UUID) (CatalogIdentityProviderAttachmentResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, registrationID)
	if a.err != nil {
		return CatalogIdentityProviderAttachmentResult{}, a.err
	}
	return CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: "https://auth.example.test"}, nil
}

func (a *recordingAttachment) attached() []uuid.UUID {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]uuid.UUID(nil), a.calls...)
}

// orgAdminAuthorizer answers the live org-admin check with err.
type orgAdminAuthorizer struct {
	allowExternalCallAuthorizer
	err error
}

func (a orgAdminAuthorizer) RequireLiveOrgAdmin(context.Context, Principal) error { return a.err }

type consentSignInHarness struct {
	principal     Principal
	project       ResolvedProject
	registrations *RegistrationService
	attachment    *recordingAttachment
	serverURL     *url.URL
	output        RegisterCatalogMCPToolOutput
	card          string
	registration  platformrepo.PlatformMcpCatalogRegistration
}

// newConsentSignInHarness adds a catalogue server whose OAuth client is set up
// automatically, so its card offers Connect.
func newConsentSignInHarness(t *testing.T, ctx context.Context, conn *pgxpool.Pool) consentSignInHarness {
	t.Helper()

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	store, err := NewRegistrationStore(conn)
	require.NoError(t, err)
	dashboardURL, err := url.Parse("https://dashboard.example.test")
	require.NoError(t, err)
	serverURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	providerKey := "browser-catalog-registry-" + uuid.NewString()
	attachment := &recordingAttachment{}
	registrations := newRegistrationService(testCatalog{details: CatalogDetails{
		CatalogCandidate: CatalogCandidate{Name: "Reviewed MCP", ProviderKey: providerKey, CatalogRef: "app.example/mcp", SetupIntent: "dashboard_source_settings", AuthSetup: "dcr"},
		Transport:        "streamable-http",
		remoteURL:        "https://reviewed.example.test/mcp",
	}}, &testRegistrationGate{enabled: true}, store).
		WithDashboardURL(dashboardURL).
		WithSignInConnectLinks(conn, serverURL).
		WithIdentityProviderAttachment(attachment)

	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
	registerCatalogRegistrationTool(reg, registrations, NewOnboardingService(conn))
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	reg.server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})

	result, err := connectTestClient(t, reg.server).CallTool(ctx, &mcp.CallToolParams{Name: "register_catalog_mcp", Arguments: map[string]any{
		"project_slug":    project.Slug,
		"provider_key":    providerKey,
		"catalog_ref":     "app.example/mcp",
		"idempotency_key": "consent-sign-in",
	}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	structured, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var output RegisterCatalogMCPToolOutput
	require.NoError(t, json.Unmarshal(structured, &output))
	require.Equal(t, nextActionConnectSignIn, output.NextAction)
	card, ok := result.Meta[cardMetaKey].(string)
	require.True(t, ok)

	registration, err := platformrepo.New(conn).GetPlatformMCPCatalogRegistrationByID(ctx, platformrepo.GetPlatformMCPCatalogRegistrationByIDParams{
		ID:             uuid.MustParse(output.RegistrationID),
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)

	return consentSignInHarness{
		principal:     principal,
		project:       project,
		registrations: registrations,
		attachment:    attachment,
		serverURL:     serverURL,
		output:        output,
		card:          card,
		registration:  registration,
	}
}

// Connect on the card opens the server's consent page, not its dashboard
// settings, so the user lands on the provider's sign-in. The dashboard link
// stays in the result as the fallback.
func TestCatalogRegistrationCardConnectOpensTheConsentPage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_card_connect_consent")
	require.NoError(t, err)
	h := newConsentSignInHarness(t, ctx, conn)

	slug, err := platformrepo.New(conn).GetPlatformMCPRegistrationConnectSlug(ctx, platformrepo.GetPlatformMCPRegistrationConnectSlugParams{
		RegistrationID: uuid.MustParse(h.output.RegistrationID),
		OrganizationID: h.principal.OrganizationID,
		ProjectID:      h.project.ID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, slug)
	consentURL := "https://app.example.test/mcp/" + slug + "/connect/first-party?setup=sign-in"

	require.Equal(t, consentURL, h.output.SignInURL, "the agent gets the same link as the card")
	require.Contains(t, h.card, "Connect")
	require.Contains(t, h.card, `href="`+consentURL+`"`)
	require.NotContains(t, h.card, h.output.DashboardSetupURL)
	require.Contains(t, h.output.DashboardSetupURL, "https://dashboard.example.test/")
}

// The consent page connects the sign-in provider of a server added from chat
// only for a live organization admin, and leaves every other server alone.
func TestConsentSignInConnectorConnectsOnlyAPendingServerForAnAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_consent_sign_in_connector")
	require.NoError(t, err)
	h := newConsentSignInHarness(t, ctx, conn)
	mcpServerID := h.registration.McpServerID.UUID
	registrationID := uuid.MustParse(h.output.RegistrationID)

	denied := NewConsentSignInConnector(conn, h.registrations, orgAdminAuthorizer{err: ErrForbidden})
	require.NoError(t, denied.ConnectPendingSignIn(ctx, h.principal.UserID, h.principal.OrganizationID, h.project.ID, mcpServerID))
	require.Empty(t, h.attachment.attached(), "a user who is not an organization admin connects nothing")

	admin := NewConsentSignInConnector(conn, h.registrations, orgAdminAuthorizer{err: nil})
	require.NoError(t, admin.ConnectPendingSignIn(ctx, h.principal.UserID, h.principal.OrganizationID, h.project.ID, uuid.New()))
	require.Empty(t, h.attachment.attached(), "a server that Platform MCP did not add is left alone")

	require.NoError(t, admin.ConnectPendingSignIn(ctx, h.principal.UserID, "org_"+uuid.NewString(), h.project.ID, mcpServerID))
	require.Empty(t, h.attachment.attached(), "another organization's server is left alone")

	require.NoError(t, admin.ConnectPendingSignIn(ctx, h.principal.UserID, h.principal.OrganizationID, h.project.ID, mcpServerID))
	require.Equal(t, []uuid.UUID{registrationID}, h.attachment.attached())
}

// A failed connection is returned to the consent page, which then shows an
// error instead of a page with nothing to connect (covered in package mcp).
func TestConsentSignInConnectorReturnsAFailedConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_consent_sign_in_failure")
	require.NoError(t, err)
	h := newConsentSignInHarness(t, ctx, conn)
	h.attachment.err = ErrIdentityProviderAttachmentUnsupported

	admin := NewConsentSignInConnector(conn, h.registrations, orgAdminAuthorizer{err: nil})
	err = admin.ConnectPendingSignIn(ctx, h.principal.UserID, h.principal.OrganizationID, h.project.ID, h.registration.McpServerID.UUID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
}

// A server whose client associations already name a live provider has its
// sign-in provider, so the consent page connects nothing. The server's
// upstream issuer column alone does not count.
func TestConsentSignInConnectorLeavesAServerWithAnAssociatedProvider(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_consent_sign_in_associated")
	require.NoError(t, err)
	h := newConsentSignInHarness(t, ctx, conn)
	mcpServerID := h.registration.McpServerID.UUID
	provider, err := remotesessionsrepo.New(conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(h.project.ID), OrganizationID: conv.ToPGText(h.principal.OrganizationID),
		Slug: "consent-sign-in-" + uuid.NewString()[:8], Issuer: "https://provider.example.test",
		ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
		TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
	})
	require.NoError(t, err)
	admin := NewConsentSignInConnector(conn, h.registrations, orgAdminAuthorizer{err: nil})

	stamped, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: conv.ToNullUUID(provider.ID), ID: mcpServerID, ProjectID: h.project.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, stamped)
	require.NoError(t, admin.ConnectPendingSignIn(ctx, h.principal.UserID, h.principal.OrganizationID, h.project.ID, mcpServerID))
	require.Len(t, h.attachment.attached(), 1, "the column alone is not a connected sign-in provider")

	associateMemberMCPClient(t, ctx, conn, h.principal.OrganizationID, h.project.ID, mcpServerID, provider.ID)
	require.NoError(t, admin.ConnectPendingSignIn(ctx, h.principal.UserID, h.principal.OrganizationID, h.project.ID, mcpServerID))
	require.Len(t, h.attachment.attached(), 1, "an associated provider is already connected")
}
