package autopublish_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish"
	"github.com/speakeasy-api/gram/server/internal/corpus/autopublish/repo"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres: true,
		Redis:    true,
	})
	if err != nil {
		log.Fatalf("launch test infra: %v", err)
		os.Exit(1)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infra: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	svc       *autopublish.Service
	draftsSvc *drafts.Service
	conn      *pgxpool.Pool
	repo      *repo.Queries
	projectID uuid.UUID
	orgID     string
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "corpus_autopublish")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx := testenv.InitAuthContext(t, t.Context(), conn, sessionManager)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authctx.ProjectID)

	svc := autopublish.NewService(conn)
	draftsSvc := drafts.NewService(conn, &stubGitRepo{}, &stubWriteLock{})

	return ctx, &testInstance{
		svc:       svc,
		draftsSvc: draftsSvc,
		conn:      conn,
		repo:      repo.New(conn),
		projectID: *authctx.ProjectID,
		orgID:     authctx.ActiveOrganizationID,
	}
}

// stubGitRepo satisfies drafts.GitRepo for creating test drafts.
type stubGitRepo struct{}

func (s *stubGitRepo) CommitFiles(_ map[string][]byte, _ string) (string, error) {
	return "deadbeef", nil
}

func (s *stubGitRepo) ReadFiles(_ string) (map[string][]byte, error) {
	return map[string][]byte{}, nil
}

func (s *stubGitRepo) ReadBlob(_ string, _ string) ([]byte, error) {
	return nil, nil
}

// stubWriteLock satisfies drafts.WriteLock for tests.
type stubWriteLock struct{}

func (s *stubWriteLock) Lock(_ context.Context, _ string) error   { return nil }
func (s *stubWriteLock) Unlock(_ context.Context, _ string) error { return nil }
