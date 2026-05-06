package collections_test

import (
	"context"
	"log"
	"os"
	"testing"

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
	"github.com/speakeasy-api/gram/server/internal/guardian"
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
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, guardianPolicy, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	auditLogger := audit.NewLogger()

	svc := collections.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, testenv.DefaultSiteURL(t))
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
