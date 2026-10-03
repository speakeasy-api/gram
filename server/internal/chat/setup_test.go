package chat_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres:   true,
		Redis:      true,
		ClickHouse: true,
		Temporal:   true,
	})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type chatTestInstance struct {
	service   *chat.Service
	sessions  *sessions.Manager
	conn      *pgxpool.Pool
	projectID uuid.UUID
	orgID     string
	assets    assets.BlobStore
	temporal  *temporal.Environment
}

// newTestChatService builds a chat service with RBAC enforcement.
func newTestChatService(t *testing.T) *chatTestInstance {
	t.Helper()
	return newTestChatServiceWithOptions(t, &mockCompletionClient{})
}

// newTestChatServiceWithCompletion builds a chat service with a custom
// OpenRouter completion client (e.g. a mock for summarize tests).
func newTestChatServiceWithCompletion(t *testing.T, completionClient openrouter.CompletionClient) *chatTestInstance {
	t.Helper()
	return newTestChatServiceWithOptions(t, completionClient)
}

func newTestChatServiceWithOptions(t *testing.T, completionClient openrouter.CompletionClient) *chatTestInstance {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "chattest")
	require.NoError(t, err)

	orgID := fmt.Sprintf("org-%s", uuid.NewString()[:8])

	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           fmt.Sprintf("chat-%s", uuid.NewString()[:8]),
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tp)
	mgr := testenv.NewTestManager(t, logger, tp, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	assetStorage := assetstest.NewTestBlobStore(t)
	chatSessionsManager := chatsessions.NewManager(logger, redisClient, "test-jwt-secret")
	posthogClient := posthog.New(ctx, logger, "", "", "")
	temporalEnv, _ := infra.NewTemporalEnv(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tp, []string{})
	require.NoError(t, err)
	contextWindow := openrouter.NewContextWindowResolver(logger, guardianPolicy, cache.NewRedisCacheAdapter(redisClient))

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	featureEnabled := func(context.Context, string) (bool, error) { return true, nil }
	telemetryService := telemetry.NewService(logger, tp, conn, chConn, mgr, chatSessionsManager, featureEnabled, featureEnabled, posthogClient, authzEngine, &feature.InMemory{})

	svc := chat.NewService(
		logger,
		tp,
		conn,
		mgr,
		chatSessionsManager,
		openrouter.NewDevelopment(""),
		completionClient,
		contextWindow,
		posthogClient,
		telemetryService,
		assetStorage,
		authzEngine,
		assistanttokens.New("test-jwt-secret", conn, authzEngine),
		billingClient,
		audit.NewLogger(),
		chat.NewTurnStream(redisClient),
		&background.TemporalChatTitleGenerator{TemporalEnv: temporalEnv},
	)

	return &chatTestInstance{
		service:   svc,
		sessions:  mgr,
		conn:      conn,
		projectID: project.ID,
		orgID:     orgID,
		assets:    assetStorage,
		temporal:  temporalEnv,
	}
}
