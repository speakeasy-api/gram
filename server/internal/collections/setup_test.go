package collections_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
	tgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/collections"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *collections.Service
	toolsets       *toolsets.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
}

func newTestCollectionsService(t *testing.T) (context.Context, *testInstance) {
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	auditLogger := audit.NewLogger()

	svc := collections.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, auditLogger, testenv.DefaultSiteURL(t))
	toolsetsSvc := toolsets.NewService(logger, tracerProvider, conn, sessionManager, nil, authzEngine, auditLogger)

	return ctx, &testInstance{
		service:        svc,
		toolsets:       toolsetsSvc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

func createMCPEnabledToolset(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	name string,
	registrySpecifier string,
) *types.Toolset {
	t.Helper()

	var origin *types.ToolsetOrigin
	if registrySpecifier != "" {
		origin = &types.ToolsetOrigin{
			RegistrySpecifier: registrySpecifier,
		}
	}

	created, err := ti.toolsets.CreateToolset(ctx, &tgen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   name,
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		Origin:                 origin,
	})
	require.NoError(t, err)

	mcpEnabled := true
	updated, err := ti.toolsets.UpdateToolset(ctx, &tgen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             &mcpEnabled,
		CustomDomainID:         nil,
	})
	require.NoError(t, err)
	require.True(t, *updated.McpEnabled)

	return updated
}

// mcpServerFixture is a toolset-backed mcp_server with a single endpoint,
// created directly via the repos. The collections publishing path only reads
// the mcp_server's id/name/slug/visibility and its endpoints, so a
// toolset-backed server stands in for a Remote MCP-backed one without the
// remote_mcp_server / user_session_issuer fixture weight.
type mcpServerFixture struct {
	id           uuid.UUID
	idStr        string
	slug         string
	endpointSlug string
}

func createMCPServerWithEndpoint(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	name string,
	slug string,
	visibility string,
	customDomainID uuid.NullUUID,
) mcpServerFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	backing := createMCPEnabledToolset(t, ctx, ti, name+" Backing", "")
	toolsetID, err := uuid.Parse(backing.ID)
	require.NoError(t, err)

	serverID := uuid.New()
	_, err = mcpserversRepo.New(ti.conn).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           *authCtx.ProjectID,
		Name:                pgtype.Text{String: name, Valid: true},
		Slug:                pgtype.Text{String: slug, Valid: true},
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{},
		RemoteMcpServerID:   uuid.NullUUID{},
		ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility:          visibility,
	})
	require.NoError(t, err)

	endpointSlug := slug + "-endpoint"
	_, err = mcpendpointsRepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: customDomainID,
		McpServerID:    serverID,
		Slug:           endpointSlug,
	})
	require.NoError(t, err)

	return mcpServerFixture{id: serverID, idStr: serverID.String(), slug: slug, endpointSlug: endpointSlug}
}

func createMCPServerWithoutEndpoint(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	name string,
	slug string,
	visibility string,
) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	backing := createMCPEnabledToolset(t, ctx, ti, name+" Backing", "")
	toolsetID, err := uuid.Parse(backing.ID)
	require.NoError(t, err)

	serverID := uuid.New()
	_, err = mcpserversRepo.New(ti.conn).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           *authCtx.ProjectID,
		Name:                pgtype.Text{String: name, Valid: true},
		Slug:                pgtype.Text{String: slug, Valid: true},
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{},
		RemoteMcpServerID:   uuid.NullUUID{},
		ToolsetID:           uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility:          visibility,
	})
	require.NoError(t, err)

	return serverID
}

func createCustomDomain(t *testing.T, ctx context.Context, ti *testInstance, domain string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := customdomainsRepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsRepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          domain,
		IngressName:     pgtype.Text{Valid: false},
		CertSecretName:  pgtype.Text{Valid: false},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	return created.ID
}

func createCollection(t *testing.T, ctx context.Context, ti *testInstance, name, slug, namespace string) *types.MCPCollection {
	t.Helper()

	result, err := ti.service.Create(ctx, &gen.CreatePayload{
		Name:                 name,
		Slug:                 slug,
		Description:          nil,
		McpRegistryNamespace: namespace,
		Visibility:           "private",
		ToolsetIds:           []string{},
		SessionToken:         nil,
		ApikeyToken:          nil,
	})
	require.NoError(t, err)

	return result
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
