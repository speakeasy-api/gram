package corpus_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/corpus"
	"github.com/speakeasy-api/gram/server/internal/corpus/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type reconcilerTestInstance struct {
	conn      *pgxpool.Pool
	queries   *repo.Queries
	projectID uuid.UUID
	orgID     string
}

func newReconcilerTest(t *testing.T) (context.Context, *reconcilerTestInstance) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "corpus_reconciler")
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

	return ctx, &reconcilerTestInstance{
		conn:      conn,
		queries:   repo.New(conn),
		projectID: *authctx.ProjectID,
		orgID:     authctx.ActiveOrganizationID,
	}
}

// mockWorkflowStarter captures StartCorpusIndexWorkflow calls for assertion.
type mockWorkflowStarter struct {
	started []corpus.StartCorpusIndexParams
}

func (m *mockWorkflowStarter) StartCorpusIndexWorkflow(_ context.Context, params corpus.StartCorpusIndexParams) error {
	m.started = append(m.started, params)
	return nil
}

func TestReconciler_PicksPendingOutboxRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newReconcilerTest(t)

	// Insert a pending publish event.
	event, err := ti.queries.InsertPublishEvent(ctx, repo.InsertPublishEventParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		CommitSha:      "abc123",
	})
	require.NoError(t, err)
	require.Equal(t, "pending", event.Status)

	logger := testenv.NewLogger(t)
	starter := &mockWorkflowStarter{}

	reconciler := corpus.NewReconciler(ti.conn, starter, "test-queue", logger)
	err = reconciler.ReconcileOnce(ctx)
	require.NoError(t, err)

	// Verify the event was transitioned to indexing.
	updated, err := ti.queries.GetPublishEvent(ctx, repo.GetPublishEventParams{
		ID:        event.ID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, "indexing", updated.Status)

	// Verify a workflow was enqueued.
	require.Len(t, starter.started, 1)
	require.Equal(t, "abc123", starter.started[0].CommitSHA)
}

func TestReconciler_SkipsIndexingRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newReconcilerTest(t)

	// Insert a publish event and transition to indexing.
	event, err := ti.queries.InsertPublishEvent(ctx, repo.InsertPublishEventParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		CommitSha:      "def456",
	})
	require.NoError(t, err)

	_, err = ti.queries.UpdatePublishEventStatus(ctx, repo.UpdatePublishEventStatusParams{
		ID:        event.ID,
		ProjectID: ti.projectID,
		Status:    "indexing",
	})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	starter := &mockWorkflowStarter{}

	reconciler := corpus.NewReconciler(ti.conn, starter, "test-queue", logger)
	err = reconciler.ReconcileOnce(ctx)
	require.NoError(t, err)

	// No workflows should be enqueued because the event is already indexing.
	require.Empty(t, starter.started)
}
