package hooks

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
	service        *Service
	conn           *pgxpool.Pool
	redisClient    *redis.Client
	accessStore    accesscontrol.Store
	sessionManager *sessions.Manager
}

func newTestHooksService(t *testing.T) (context.Context, *testInstance) {
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

	cacheAdapter := cache.NewRedisCacheAdapter(redisClient)

	// Pass nil for telemetry logger, temporalEnv, productFeatures, and chatTitleGenerator in tests
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, conn, nil)
	t.Cleanup(func() { _ = chatWriterShutdown(t.Context()) })
	accessStore := accesscontrol.NewRedisStore(cacheAdapter, accesscontrol.AlphaTTL)
	shadowMCPClient := shadowmcp.NewClient(logger, conn, cacheAdapter, accessStore)
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	cursorEvents := newTestCursorAgentEventSource(t)
	svc := NewService(logger, conn, tracerProvider, nil, sessionManager, cacheAdapter, nil, nil, authzEngine, nil, nil, nil, shadowMCPClient, chatWriter, cursorEvents, siteURL, "test-jwt-secret")

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		redisClient:    redisClient,
		accessStore:    accessStore,
		sessionManager: sessionManager,
	}
}

func createHookAccessRule(t *testing.T, ctx context.Context, ti *testInstance, projectID string, accessScope string, disposition string, matchKind string, matchValue string, displayName string) accesscontrol.AccessRule {
	t.Helper()

	now := time.Now().UTC()
	rule, err := ti.accessStore.CreateRule(ctx, accesscontrol.AccessRule{
		ID:             uuid.NewString(),
		OrganizationID: authOrganizationID(t, ctx),
		ProjectID:      projectID,
		AccessScope:    accessScope,
		ResourceType:   accesscontrol.ResourceTypeShadowMCP,
		Disposition:    disposition,
		MatchKind:      matchKind,
		MatchValue:     matchValue,
		DisplayName:    displayName,
		ObservedSummary: accesscontrol.ObservedSummary{
			Name:           nil,
			FullURL:        nil,
			URLHost:        nil,
			ServerIdentity: nil,
			ToolName:       nil,
			ToolCall:       nil,
			BlockReason:    nil,
			RiskPolicyID:   nil,
			RiskResultID:   nil,
		},
		SourceRequestID: "",
		CreatedBy:       "",
		UpdatedBy:       "",
		Reason:          "",
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	require.NoError(t, err)
	return rule
}

func authOrganizationID(t *testing.T, ctx context.Context) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authCtx.ActiveOrganizationID
}

func seedHookUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, userID string, email string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: email,
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)

	err = organizationsrepo.New(conn).AttachWorkOSUserToOrg(ctx, organizationsrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: pgtype.Text{},
	})
	require.NoError(t, err)
}
