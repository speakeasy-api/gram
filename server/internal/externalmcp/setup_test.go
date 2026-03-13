package externalmcp_test

import (
	"context"
	"fmt"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres: true,
		Redis:    true,
	})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *externalmcp.Service
	conn           *pgxpool.Pool
	repo           *repo.Queries
	sessionManager *sessions.Manager
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-test"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	registryClient := testenv.NewMCPRegistryClient(t, logger, tracerProvider)

	testServerURL, _ := url.Parse("http://localhost:8080")
	svc := externalmcp.NewService(logger, tracerProvider, conn, sessionManager, registryClient, testServerURL)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		repo:           repo.New(conn),
		sessionManager: sessionManager,
	}
}

// newNonAdminTestService creates a test service with a non-admin user.
func newNonAdminTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)

	// Create a non-admin mock IDP
	cfg := mockidp.NewConfig()
	cfg.User.Admin = false
	srv := httptest.NewServer(mockidp.Handler(cfg))
	t.Cleanup(srv.Close)

	fakePylon, err := pylon.NewPylon(logger, "")
	require.NoError(t, err)

	fakePosthog := posthog.New(ctx, logger, "test-posthog-key", "test-posthog-host", "")

	sessionManager := sessions.NewManager(
		logger, conn, redisClient, cache.Suffix("gram-test"),
		srv.URL, mockidp.MockSecretKey,
		fakePylon, fakePosthog, billingClient, nil,
	)

	ctx = initAuthContext(t, ctx, conn, sessionManager)

	registryClient := testenv.NewMCPRegistryClient(t, logger, tracerProvider)
	testServerURL, _ := url.Parse("http://localhost:8080")
	svc := externalmcp.NewService(logger, tracerProvider, conn, sessionManager, registryClient, testServerURL)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		repo:           repo.New(conn),
		sessionManager: sessionManager,
	}
}

// initAuthContext is a local copy of testenv.InitAuthContext so we can use
// a custom session manager (e.g. non-admin).
func initAuthContext(t *testing.T, ctx context.Context, conn *pgxpool.Pool, sm *sessions.Manager) context.Context {
	t.Helper()

	idToken, err := sm.ExchangeTokenFromSpeakeasy(ctx, "test-code")
	require.NoError(t, err)

	userInfo, err := sm.GetUserInfoFromSpeakeasy(ctx, idToken)
	require.NoError(t, err)
	require.NotEmpty(t, userInfo.Organizations)

	activeOrg := userInfo.Organizations[0]

	orgQueries := orgRepo.New(conn)
	_, err = orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              activeOrg.ID,
		Name:            activeOrg.Name,
		Slug:            activeOrg.Slug,
		SsoConnectionID: conv.PtrToPGText(activeOrg.SsoConnectionID),
	})
	require.NoError(t, err)

	session := sessions.Session{
		SessionID:            idToken,
		UserID:               userInfo.UserID,
		ActiveOrganizationID: activeOrg.ID,
	}
	err = sm.StoreSession(ctx, session)
	require.NoError(t, err)

	ctx, err = sm.Authenticate(ctx, idToken)
	require.NoError(t, err)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectSlug := fmt.Sprintf("test-%s", uuid.New().String()[:8])
	p, err := projectsRepo.New(conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: authctx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	authctx.ProjectID = &p.ID
	authctx.ProjectSlug = &p.Slug

	return ctx
}
