package assistant_platform_mcp_adapter

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/platformmcptest"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true, Temporal: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = env

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type allowAuthorizer struct{}

func (allowAuthorizer) Authenticate(context.Context, string) (platformmcp.Principal, error) {
	return platformmcp.Principal{}, nil
}

func (allowAuthorizer) Enabled(context.Context, string) (bool, error) { return true, nil }

func (allowAuthorizer) PrepareExternalContext(ctx context.Context, _ platformmcp.Principal) (context.Context, error) {
	return ctx, nil
}

func (allowAuthorizer) AuthorizeExternalCall(context.Context, platformmcp.Principal, platformmcp.ExternalAuthorization) error {
	return nil
}

func (allowAuthorizer) RequireLiveMembership(context.Context, platformmcp.Principal) error {
	return nil
}

func (allowAuthorizer) RequireLiveOrgAdmin(context.Context, platformmcp.Principal) error { return nil }

// newTestRuntime composes the Platform MCP runtime over real services.
func newTestRuntime(t *testing.T) *platformmcp.Runtime {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "assistant_platform_mcp_adapter")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	siteURL := testenv.DefaultSiteURL(t)
	temporalEnv, _ := infra.NewTemporalEnv(t)
	services := platformmcptest.NewServices(t, platformmcptest.Deps{
		Logger:          logger,
		TracerProvider:  tracerProvider,
		MeterProvider:   testenv.NewMeterProvider(t),
		DB:              conn,
		Redis:           redisClient,
		ClickHouse:      chConn,
		Encryption:      testenv.NewEncryptionClient(t),
		Sessions:        testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("assistant-platform-mcp-adapter"), billing.NewStubClient(logger, tracerProvider)),
		Authz:           engine,
		Flags:           &feature.InMemory{},
		ProductFeatures: productfeatures.NewClient(logger, tracerProvider, conn, redisClient),
		GuardianPolicy:  policy,
		ServerURL:       siteURL,
		DashboardURL:    siteURL,
		KeyMaterial:     "assistant-platform-mcp-adapter-key",
		TemporalEnv:     temporalEnv,
	})
	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(conn, engine)
	riskPolicyCatalog, err := policycatalog.Build()
	require.NoError(t, err)
	return platformmcp.NewRuntime(logger, allowAuthorizer{}, allowAuthorizer{}, authorizer, "", "assistant-platform-mcp-adapter-key", riskPolicyCatalog, platformmcp.NewPostgresReadinessRecorder(conn), services)
}
