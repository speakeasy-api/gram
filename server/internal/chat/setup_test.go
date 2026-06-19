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

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres:   true,
		Redis:      true,
		ClickHouse: true,
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
}

// newTestChatService builds a chat service with RBAC enforcement enabled for
// the org. Use authztest.WithExactGrants to grant org:admin in tests that need
// project-wide visibility.
func newTestChatService(t *testing.T) *chatTestInstance {
	t.Helper()
	return newTestChatServiceWithRBAC(t, authztest.RBACAlwaysEnabled)
}

// newTestChatServiceRBACDisabled builds a chat service whose org has the RBAC
// feature flag off, so ShouldEnforce returns false even for enterprise callers.
func newTestChatServiceRBACDisabled(t *testing.T) *chatTestInstance {
	t.Helper()
	return newTestChatServiceWithRBAC(t, authztest.RBACAlwaysDisabled)
}

func newTestChatServiceWithRBAC(t *testing.T, isRBACEnabled authz.IsRBACEnabled) *chatTestInstance {
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

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tp)
	// Use a unique suffix per test to isolate Redis cache entries when tests
	// run in parallel and all use the same mockidp.MockUserID.
	suffix := cache.Suffix("gram-local-" + uuid.NewString()[:8])
	mgr := testenv.NewTestManager(t, logger, tp, conn, redisClient, suffix, billingClient)

	authzEngine := authz.NewEngine(logger, conn, chConn, isRBACEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	svc := chat.NewService(logger, tp, conn, mgr, nil, nil, nil, nil, nil, nil, nil, authzEngine, nil, billingClient)

	return &chatTestInstance{
		service:   svc,
		sessions:  mgr,
		conn:      conn,
		projectID: project.ID,
		orgID:     orgID,
	}
}
