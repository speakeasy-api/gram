package migration_test

import (
	"context"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	migrator       *migration.Migrator
	conn           *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
	deploymentID   uuid.UUID
}

var testActor = migration.Actor{UserID: "staff-oidc-subject", Email: new("staff@example.com")}

func newTestMigrator(t *testing.T) (context.Context, *testInstance) {
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policy := guardian.NewDefaultPolicy(tracerProvider, guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		},
	})))
	migrator := migration.NewMigrator(logger, tracerProvider, conn, audit.NewLogger(), policy, testenv.NewEncryptionClient(t), plugins.PublicationRequests{Enabled: false}, nil)

	deploymentID, err := repo.New(conn).SeedCompletedDeploymentFixture(ctx, repo.SeedCompletedDeploymentFixtureParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)

	return ctx, &testInstance{
		migrator:       migrator,
		conn:           conn,
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      *authCtx.ProjectID,
		deploymentID:   deploymentID,
	}
}

// fixtureServer is a hosted toolset proxying one external MCP attachment.
type fixtureServer struct {
	toolset  toolsetsrepo.Toolset
	issuerID uuid.UUID
	clientID uuid.UUID
}

type serverOptions struct {
	slug          string
	remoteURL     string
	headers       []byte
	requiresOAuth bool
	noIssuer      bool
	noClient      bool
	noWrapper     bool
	// otherTool adds an HTTP tool beside the external MCP proxy tool.
	otherTool bool
	// perTool seeds a per-tool (direct) definition instead of the proxy tool.
	perTool bool
	// mcpOff seeds the toolset's server disabled.
	mcpOff bool
	// sharedIssuer signs the server in through another server's issuer
	// instead of creating its own.
	sharedIssuer *fixtureServer
}

func seedServer(t *testing.T, ctx context.Context, ti *testInstance, opts serverOptions) fixtureServer {
	t.Helper()

	toolURN, toolType := urn.NewTool(urn.ToolKindExternalMCP, opts.slug, "proxy"), "proxy"
	if opts.perTool {
		toolURN, toolType = urn.NewTool(urn.ToolKindExternalMCP, opts.slug, "search"), "direct"
	}
	require.NoError(t, repo.New(ti.conn).SeedExternalMCPProxyFixture(ctx, repo.SeedExternalMCPProxyFixtureParams{
		DeploymentID: ti.deploymentID, Slug: opts.slug, ToolUrn: toolURN.String(), RemoteUrl: opts.remoteURL, Type: toolType, RequiresOauth: opts.requiresOAuth, HeaderDefinitions: opts.headers,
	}))

	toolsets := toolsetsrepo.New(ti.conn)
	toolset, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID,
		Name: opts.slug, Slug: opts.slug, Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{},
	})
	require.NoError(t, err)
	mcpSlug := opts.slug + "-mcp"
	toolURNs := []urn.Tool{toolURN}
	if opts.otherTool {
		toolURNs = append(toolURNs, urn.NewTool(urn.ToolKindHTTP, opts.slug, "extra"))
	}
	_, err = toolsets.CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: 1, ToolUrns: toolURNs, ResourceUrns: []urn.Resource{}, PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	server := fixtureServer{toolset: toolset, issuerID: uuid.Nil, clientID: uuid.Nil}
	switch {
	case opts.sharedIssuer != nil:
		server.issuerID, server.clientID = opts.sharedIssuer.issuerID, opts.sharedIssuer.clientID
	case !opts.noIssuer:
		issuer, err := usersessionsrepo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
			ProjectID: ti.projectID, OrganizationID: conv.ToPGText(ti.organizationID), Slug: opts.slug + "-issuer", AuthnChallengeMode: "interactive",
			SessionDuration: pgtype.Interval{Microseconds: int64(24 * time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
		})
		require.NoError(t, err)
		server.issuerID = issuer.ID
		if opts.noClient {
			break
		}
		clientID, err := repo.New(ti.conn).SeedRemoteSessionClientFixture(ctx, repo.SeedRemoteSessionClientFixtureParams{
			ProjectID: conv.ToNullUUID(ti.projectID), OrganizationID: conv.ToPGText(ti.organizationID),
			Slug: opts.slug + "-upstream", IssuerUrl: "https://auth.example.com/" + opts.slug, UserSessionIssuerID: issuer.ID,
		})
		require.NoError(t, err)
		server.clientID = clientID
	}

	if !opts.noWrapper {
		visibility := "private"
		if opts.mcpOff {
			visibility = "disabled"
		}
		issuerID := uuid.NullUUID{UUID: server.issuerID, Valid: server.issuerID != uuid.Nil}
		_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID: toolset.ID, ProjectID: ti.projectID, Name: conv.ToPGText(toolset.Name), Slug: conv.ToPGText(mcpSlug),
			EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: issuerID, RemoteMcpServerID: uuid.NullUUID{}, TunneledMcpServerID: uuid.NullUUID{},
			ToolsetID: conv.ToNullUUID(toolset.ID), UnproxiedMcpServerID: uuid.NullUUID{}, ToolVariationsGroupID: uuid.NullUUID{},
			Visibility: visibility, NetworkAccessMode: pgtype.Text{},
		})
		require.NoError(t, err)
		testenv.SetHostedMCP(t, ctx, ti.conn, ti.projectID, toolset.ID, testenv.HostedMCP{Slug: mcpSlug})
	}
	server.toolset, err = toolsets.GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolset.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
	return server
}

func countIssuersAndClients(t *testing.T, ctx context.Context, ti *testInstance) repo.CountIssuersAndClientsFixtureRow {
	t.Helper()
	counts, err := repo.New(ti.conn).CountIssuersAndClientsFixture(ctx, ti.organizationID)
	require.NoError(t, err)
	return counts
}
