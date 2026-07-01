package productfeatures_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
	service        *productfeatures.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	// u1 is the primary test user (auth context owner), u2 is a seeded secondary user.
	u1 string
	u2 string
}

func newTestProductFeaturesService(t *testing.T) (context.Context, *testInstance) {
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

	// The mock IDP returns the same organization for every test, so parallel
	// subtests would otherwise share the cache key feature:<orgID>:<feature>
	// in the shared redis db. One test enabling and another disabling the
	// same feature races on that key and produces flaky failures (e.g.
	// "returns true for enabled feature" reading a `false` written by
	// "returns false after feature is disabled"). Override the org ID with
	// a fresh UUID per test so cache keys are unique. organization_features
	// has no FK on organization_id, and ShouldEnforce skips RBAC for the
	// non-enterprise account type used in tests, so this needs no extra setup.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context not found")
	authCtx.ActiveOrganizationID = uuid.NewString()
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	// The outbox and audit_logs tables have a FK on organization_id → organization_metadata(id).
	// Insert the randomized org so audit writes don't fail on FK violation.
	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          authCtx.ActiveOrganizationID,
		Name:        "Test Org " + authCtx.ActiveOrganizationID[:8],
		Slug:        "test-org-" + authCtx.ActiveOrganizationID[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	svc := productfeatures.NewService(logger, tracerProvider, conn, sessionManager, redisClient, authzEngine, audit.NewLogger())

	// u1 is the primary authenticated user created by InitAuthContext.
	// authCtx was already retrieved above; re-get from updated ctx after SetAuthContext.
	authCtx2, ok2 := contextvalues.GetAuthContext(ctx)
	require.True(t, ok2, "auth context not found after init")
	u1 := authCtx2.UserID

	// Seed a second real user to satisfy the session_capture_exclusions FK.
	u2ID := uuid.NewString()
	usersQueries := userRepo.New(conn)
	_, err = usersQueries.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          u2ID,
		Email:       u2ID + "@example.com",
		DisplayName: "Test User 2",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	// Attach both users to the org. SetSessionCaptureExclusions rejects user
	// IDs that are not active members, so the roster must include u1 and u2.
	for _, uid := range []string{u1, u2ID} {
		_, err = orgRepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			UserID:         conv.ToPGText(uid),
		})
		require.NoError(t, err)
	}

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		u1:             u1,
		u2:             u2ID,
	}
}
