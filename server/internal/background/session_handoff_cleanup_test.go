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
	for _, tc := range []struct {
		name        string
		failExpired bool
		failIntents bool
	}{
		{name: "success"},
		{name: "expired failure", failExpired: true},
		{name: "intent failure", failIntents: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.SetWorkerOptions(worker.Options{DeadlockDetectionTimeout: 5 * time.Second})
			expiredCalls, intentCalls := 0, 0
			env.RegisterActivityWithOptions(func(context.Context) error {
				expiredCalls++
				if tc.failExpired {
					return errors.New("expired storage unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "CleanupExpiredUnreadSessionHandoffs"})
			env.RegisterActivityWithOptions(func(context.Context) error {
				intentCalls++
				if tc.failIntents {
					return errors.New("intent storage unavailable")
				}
				return nil
			}, activity.RegisterOptions{Name: "CleanupStaleSessionHandoffUploadIntents"})

			env.ExecuteWorkflow(SessionHandoffCleanupWorkflow)
			require.True(t, env.IsWorkflowCompleted())
			if tc.failExpired || tc.failIntents {
				require.Error(t, env.GetWorkflowError())
			} else {
				require.NoError(t, env.GetWorkflowError())
			}
			require.Equal(t, map[bool]int{false: 1, true: sessionHandoffCleanupMaxAttempts}[tc.failExpired], expiredCalls)
			require.Equal(t, map[bool]int{false: 1, true: sessionHandoffCleanupMaxAttempts}[tc.failIntents], intentCalls)
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
	projectID       uuid.UUID
	listedProjects  []uuid.UUID
	pages           [][]sessionhandoff.ExpiredUnread
	calls           int
	deletedRows     []uuid.UUID
	quarantinedRows []uuid.UUID
	cursors         []*sessionhandoff.ExpiredUnreadCursor
}

func (r *cleanupTraversalRepository) ListExpiredUnreadProjects(_ context.Context, cursor *uuid.UUID, _ int32) ([]uuid.UUID, error) {
	if cursor != nil {
		return nil, nil
	}
	return []uuid.UUID{r.projectID}, nil
}

func (r *cleanupTraversalRepository) ListExpiredUnread(_ context.Context, projectID uuid.UUID, cursor *sessionhandoff.ExpiredUnreadCursor, _ int32) ([]sessionhandoff.ExpiredUnread, error) {
	r.listedProjects = append(r.listedProjects, projectID)
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
	r.quarantinedRows = append(r.quarantinedRows, row.ID)
	return nil
}

type cleanupTraversalBlobs struct{}

func (cleanupTraversalBlobs) Delete(_ context.Context, _ *url.URL) error { return nil }

func TestCleanupExpiredUnreadSessionHandoffsAdvancesPastMalformedLeadingRow(t *testing.T) {
	t.Parallel()
	expiresAt := time.Now().Add(-time.Hour)
	projectID := uuid.New()
	firstPage := make([]sessionhandoff.ExpiredUnread, 100)
	for i := range firstPage {
		firstPage[i] = sessionhandoff.ExpiredUnread{ID: uuid.New(), ProjectID: projectID, BlobURL: "s3://handoffs/ok.md", ExpiresAt: expiresAt}
	}
	failedID := firstPage[0].ID
	firstPage[0].BlobURL = "://malformed"
	laterID := uuid.New()
	repository := &cleanupTraversalRepository{projectID: projectID, pages: [][]sessionhandoff.ExpiredUnread{
		firstPage,
		{{ID: laterID, ProjectID: projectID, BlobURL: "s3://handoffs/later.md", ExpiresAt: expiresAt.Add(time.Second)}},
	}}
	activities := &Activities{sessionHandoffCleaner: sessionhandoff.NewExpiredUnreadCleaner(repository, cleanupTraversalBlobs{})}

	err := activities.CleanupExpiredUnreadSessionHandoffs(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, repository.calls)
	require.Equal(t, []uuid.UUID{projectID, projectID}, repository.listedProjects)
	require.Nil(t, repository.cursors[0])
	require.NotNil(t, repository.cursors[1])
	require.Equal(t, firstPage[len(firstPage)-1].ID, repository.cursors[1].ID)
	require.Contains(t, repository.deletedRows, laterID)
	require.NotContains(t, repository.deletedRows, failedID)
	require.Equal(t, []uuid.UUID{failedID}, repository.quarantinedRows)
}
