package background

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
)

func TestSessionHandoffCleanupWorkflow(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		fail := fail
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			calls := 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				calls++
				if fail {
					return errors.New("storage unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "CleanupSessionHandoffStorage"})

			env.ExecuteWorkflow(SessionHandoffCleanupWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if fail {
				require.Error(t, env.GetWorkflowError())
				require.Equal(t, sessionHandoffCleanupMaxAttempts, calls)
			} else {
				require.NoError(t, env.GetWorkflowError())
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestSessionHandoffCleanupScheduleOptions(t *testing.T) {
	t.Parallel()
	options := sessionHandoffCleanupScheduleOptions("shared")
	require.Equal(t, sessionHandoffCleanupScheduleID, options.ID)
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, options.Overlap)
	require.Equal(t, sessionHandoffCleanupScheduleInterval-time.Second, options.CatchupWindow)
	require.Len(t, options.Spec.Intervals, 1)
	require.Equal(t, sessionHandoffCleanupScheduleInterval, options.Spec.Intervals[0].Every)
	action, ok := options.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	require.Equal(t, sessionHandoffCleanupWorkflowID, action.ID)
	require.Equal(t, "shared", action.TaskQueue)
	require.Equal(t, sessionHandoffCleanupWorkflowRunTimeout, action.WorkflowRunTimeout)
}

type cleanupTraversalRepository struct {
	pages       [][]sessionhandoff.ExpiredUnread
	calls       int
	deletedRows []uuid.UUID
	cursors     []*sessionhandoff.ExpiredUnreadCursor
}

func (r *cleanupTraversalRepository) ListExpiredUnread(_ context.Context, cursor *sessionhandoff.ExpiredUnreadCursor, _ int32) ([]sessionhandoff.ExpiredUnread, error) {
	r.cursors = append(r.cursors, cursor)
	if r.calls >= len(r.pages) {
		return nil, nil
	}
	page := r.pages[r.calls]
	r.calls++
	return page, nil
}

func (r *cleanupTraversalRepository) ClaimExpiredUnread(_ context.Context, row sessionhandoff.ExpiredUnread) (sessionhandoff.ExpiredUnread, bool, error) {
	return row, true, nil
}
func (r *cleanupTraversalRepository) FinalizeExpiredUnread(_ context.Context, row sessionhandoff.ExpiredUnread) (bool, error) {
	r.deletedRows = append(r.deletedRows, row.ID)
	return true, nil
}
func (r *cleanupTraversalRepository) QuarantineExpiredUnread(_ context.Context, row sessionhandoff.ExpiredUnread, _ string) error {
	return nil
}

type cleanupTraversalBlobs struct{}

func (cleanupTraversalBlobs) Delete(_ context.Context, _ *url.URL) error { return nil }

func TestCleanupExpiredUnreadSessionHandoffsAdvancesPastFailedLeadingRow(t *testing.T) {
	t.Parallel()
	expiresAt := time.Now().Add(-time.Hour)
	firstPage := make([]sessionhandoff.ExpiredUnread, 100)
	for i := range firstPage {
		firstPage[i] = sessionhandoff.ExpiredUnread{ID: uuid.New(), ProjectID: uuid.New(), BlobURL: "s3://handoffs/ok.md", ExpiresAt: expiresAt}
	}
	failedID := firstPage[0].ID
	firstPage[0].BlobURL = "://malformed"
	laterID := uuid.New()
	repository := &cleanupTraversalRepository{pages: [][]sessionhandoff.ExpiredUnread{
		firstPage,
		{{ID: laterID, ProjectID: uuid.New(), BlobURL: "s3://handoffs/later.md", ExpiresAt: expiresAt.Add(time.Second)}},
	}}
	activities := &Activities{sessionHandoffCleaner: sessionhandoff.NewExpiredUnreadCleaner(repository, cleanupTraversalBlobs{})}

	err := activities.CleanupExpiredUnreadSessionHandoffs(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, repository.calls)
	require.Nil(t, repository.cursors[0])
	require.NotNil(t, repository.cursors[1])
	require.Equal(t, firstPage[len(firstPage)-1].ID, repository.cursors[1].ID)
	require.Contains(t, repository.deletedRows, laterID)
	require.NotContains(t, repository.deletedRows, failedID)
}
