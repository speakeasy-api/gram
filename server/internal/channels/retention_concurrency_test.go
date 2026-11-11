package channels

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Retention must wait for the posting transaction's settings lock before it
// locks conversation counts; otherwise the two transactions form a lock cycle.
func TestRetentionLocksSettingsBeforeConversation(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_retention_lock_order")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Writer")
	thread := f.post(t, "alice", "channels_create_thread", map[string]any{
		"instanceId": alice["instanceId"], "title": "Aging",
	})
	f.post(t, "alice", "channels_post", map[string]any{
		"instanceId": alice["instanceId"], "threadId": thread["id"],
		"text": "old", "idempotencyKey": "old",
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	queries := repo.New(f.db)
	require.NoError(t, queries.FixtureAgeMessages(ctx, repo.FixtureAgeMessagesParams{
		OrganizationID: testOrg, ProjectID: retentionTestProject(t, f, testOrg), AgeDays: 200, ThroughSeq: math.MaxInt64,
	}))

	posting := testenv.BeginTx(t, ctx, f.db)
	postingQueries := queries.WithTx(posting)
	seq, err := postingQueries.AllocateMessageSeq(ctx, repo.AllocateMessageSeqParams{OrganizationID: testOrg, ProjectID: retentionTestProject(t, f, testOrg)})
	require.NoError(t, err)

	type outcome struct {
		result RetentionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := f.handler.RunRetention(ctx, time.Now())
		done <- outcome{result: result, err: err}
	}()
	waitForRetentionBlocker(t, f, posting)

	// This must succeed while retention is waiting. Before the fix, retention
	// already held this conversation lock while waiting for settings.
	touchCtx, touchCancel := context.WithTimeout(ctx, 2*time.Second)
	defer touchCancel()
	require.NoError(t, postingQueries.TouchConversationActivity(touchCtx, repo.TouchConversationActivityParams{
		OrganizationID: testOrg, ProjectID: retentionTestProject(t, f, testOrg), ID: uuid.MustParse(stringOf(t, thread["id"])), LastMessageSeq: seq,
	}))
	require.NoError(t, posting.Commit(ctx))
	swept := <-done
	require.NoError(t, swept.err)
	require.Equal(t, 1, swept.result.Messages)
}

// A sweep's statement snapshot can still see the old expiration while renewal
// holds the row lock. After waiting, deletion must recheck the renewed row.
func TestRetentionRechecksConcurrentInstanceRenewal(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "channels_retention_renewal")
	f.enable(t, "alice")
	alice := f.connect(t, "alice", "Writer")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	queries := repo.New(f.db)
	require.NoError(t, queries.FixtureAgeInstances(ctx, repo.FixtureAgeInstancesParams{
		OrganizationID: testOrg, ProjectID: retentionTestProject(t, f, testOrg), AgeDays: 200,
	}))

	renewing := testenv.BeginTx(t, ctx, f.db)
	instanceID := uuid.MustParse(stringOf(t, alice["instanceId"]))
	_, err := queries.WithTx(renewing).RenewInstance(ctx, repo.RenewInstanceParams{
		OrganizationID: testOrg, ProjectID: retentionTestProject(t, f, testOrg), ID: instanceID, SubjectID: "alice",
		ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
	})
	require.NoError(t, err)

	type outcome struct {
		ids []uuid.UUID
		err error
	}
	projectID := retentionTestProject(t, f, testOrg)
	done := make(chan outcome, 1)
	go func() {
		ids, err := queries.SoftDeleteStaleInstances(ctx, repo.SoftDeleteStaleInstancesParams{
			OrganizationID: testOrg, ProjectID: projectID,
			Cutoff: conv.ToPGTimestamptz(time.Now().AddDate(0, 0, -30)), ResultLimit: 10,
		})
		done <- outcome{ids: ids, err: err}
	}()
	waitForRetentionBlocker(t, f, renewing)
	require.NoError(t, renewing.Commit(ctx))
	swept := <-done
	require.NoError(t, swept.err)
	require.Empty(t, swept.ids, "retention deleted a successfully renewed instance")
	live, err := queries.ListLiveInstances(ctx, repo.ListLiveInstancesParams{OrganizationID: testOrg, ProjectID: retentionTestProject(t, f, testOrg), ResultLimit: 10})
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, instanceID, live[0].ID)
}

// Observe the actual lock wait rather than relying on a sleep to arrange the
// interleaving. Each fixture has its own database and only one competing write.
func waitForRetentionBlocker(t *testing.T, f *fixture, blocker pgx.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	testenv.WaitForBackendsBlockedBy(t, ctx, f.db, testenv.BackendPID(blocker), 1)
}
