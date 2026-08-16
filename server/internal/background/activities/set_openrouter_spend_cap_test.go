package activities_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type failFirstSpendCapAuditLogger struct {
	delegate *audit.Logger
	calls    atomic.Int32
}

type spendCapHeartbeatFixture struct {
	BeforeMonthlyCredits int64
	ObservedKeyUpdatedAt time.Time
	AppliedKeyUpdatedAt  time.Time
	Applied              bool
}

func (l *failFirstSpendCapAuditLogger) LogOpenRouterAPIKeySetSpendCap(
	ctx context.Context,
	dbtx auditrepo.DBTX,
	event audit.LogOpenRouterAPIKeySetSpendCapEvent,
) error {
	if l.calls.Add(1) == 1 {
		return errors.New("audit unavailable")
	}
	if err := l.delegate.LogOpenRouterAPIKeySetSpendCap(ctx, dbtx, event); err != nil {
		return errors.Join(errors.New("delegate spend-cap audit log"), err)
	}
	return nil
}

func createSpendCapActivityKey(t *testing.T, db openrouterrepo.DBTX, organizationID string, monthlyCredits int64) {
	t.Helper()
	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
		Key:            pgtype.Text{String: "key_placeholder", Valid: true},
		KeyEncrypted:   pgtype.Text{},
		KeyHash:        "hash_placeholder",
		MonthlyCredits: monthlyCredits,
	})
	require.NoError(t, err)
}

func spendCapActivityArgs(operationID, organizationID string, limit int) activities.SetOpenRouterSpendCapArgs {
	return activities.SetOpenRouterSpendCapArgs{
		OperationID:      operationID,
		OrganizationID:   organizationID,
		Limit:            limit,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "user_placeholder"),
		ActorDisplayName: nil,
	}
}

func TestSetOpenRouterSpendCapRetryPreservesOriginalAuditSnapshot(t *testing.T) {
	t.Parallel()

	_, provisioner, db, organizationID := setupPaygChatKeyReconciler(
		t,
		"payg",
		pgtype.Text{String: "subscription_placeholder", Valid: true},
	)
	createSpendCapActivityKey(t, db, organizationID, 250)

	provisioner.On(
		"RefreshAPIKeyLimit",
		mock.Anything,
		organizationID,
		openrouter.KeyTypeChat,
		mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 600 }),
	).Run(func(args mock.Arguments) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)
		require.NoError(t, openrouterrepo.New(db).UpdateOpenRouterKeyMonthlyCredits(ctx, openrouterrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
			MonthlyCredits: 600,
			OrganizationID: organizationID,
			KeyType:        string(openrouter.KeyTypeChat),
		}))
	}).Return(600, nil).Once()

	auditLogger := &failFirstSpendCapAuditLogger{delegate: audit.NewLogger()}
	setter := activities.NewSetOpenRouterSpendCap(testenv.NewLogger(t), db, provisioner, auditLogger)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(setter.Do, activity.RegisterOptions{Name: "SetOpenRouterSpendCap"})
	env.ExecuteWorkflow(background.OpenRouterSpendCapWorkflow, background.OpenRouterSpendCapParams{
		OperationID:      "operation_retry_placeholder",
		OrganizationID:   organizationID,
		Limit:            600,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, "user_placeholder"),
		ActorDisplayName: nil,
	})
	require.NoError(t, env.GetWorkflowError())
	require.EqualValues(t, 2, auditLogger.calls.Load())
	provisioner.AssertExpectations(t)

	entry, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionOpenRouterAPIKeySetSpendCap)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.EqualValues(t, 250, before["monthly_credits"])
	require.EqualValues(t, 600, after["monthly_credits"])
}

func TestSetOpenRouterSpendCapRetryReappliesWhenOriginalValueEqualsRequestedLimit(t *testing.T) {
	t.Parallel()

	_, provisioner, db, organizationID := setupPaygChatKeyReconciler(
		t,
		"payg",
		pgtype.Text{String: "subscription_placeholder", Valid: true},
	)
	createSpendCapActivityKey(t, db, organizationID, 600)
	observed, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)

	provisioner.On(
		"RefreshAPIKeyLimit",
		mock.Anything,
		organizationID,
		openrouter.KeyTypeChat,
		mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 600 }),
	).Run(func(args mock.Arguments) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)
		require.NoError(t, openrouterrepo.New(db).UpdateOpenRouterKeyMonthlyCredits(ctx, openrouterrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
			MonthlyCredits: 600,
			OrganizationID: organizationID,
			KeyType:        string(openrouter.KeyTypeChat),
		}))
	}).Return(600, nil).Once()

	setter := activities.NewSetOpenRouterSpendCap(testenv.NewLogger(t), db, provisioner, audit.NewLogger())
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(setter.Do)
	env.SetHeartbeatDetails(spendCapHeartbeatFixture{
		BeforeMonthlyCredits: 600,
		ObservedKeyUpdatedAt: observed.UpdatedAt.Time,
		AppliedKeyUpdatedAt:  time.Time{},
		Applied:              false,
	})
	_, err = env.ExecuteActivity(setter.Do, spendCapActivityArgs("operation_equal_placeholder", organizationID, 600))
	require.NoError(t, err)
	provisioner.AssertExpectations(t)
}

func TestSetOpenRouterSpendCapRetryAuditsWhenAppliedHeartbeatWasLost(t *testing.T) {
	t.Parallel()

	_, provisioner, db, organizationID := setupPaygChatKeyReconciler(
		t,
		"payg",
		pgtype.Text{String: "subscription_placeholder", Valid: true},
	)
	createSpendCapActivityKey(t, db, organizationID, 100)
	queries := openrouterrepo.New(db)
	observed, err := queries.GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.NoError(t, queries.UpdateOpenRouterKeyMonthlyCredits(t.Context(), openrouterrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
		MonthlyCredits: 600,
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	}))

	setter := activities.NewSetOpenRouterSpendCap(testenv.NewLogger(t), db, provisioner, audit.NewLogger())
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(setter.Do)
	env.SetHeartbeatDetails(spendCapHeartbeatFixture{
		BeforeMonthlyCredits: 100,
		ObservedKeyUpdatedAt: observed.UpdatedAt.Time,
		AppliedKeyUpdatedAt:  time.Time{},
		Applied:              false,
	})
	_, err = env.ExecuteActivity(setter.Do, spendCapActivityArgs("operation_lost_heartbeat_placeholder", organizationID, 600))
	require.NoError(t, err)
	provisioner.AssertNotCalled(t, "RefreshAPIKeyLimit", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOpenRouterAPIKeySetSpendCap)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestSetOpenRouterSpendCapRetryDoesNotOverwriteNewerUnauditedOperation(t *testing.T) {
	t.Parallel()

	_, provisioner, db, organizationID := setupPaygChatKeyReconciler(
		t,
		"payg",
		pgtype.Text{String: "subscription_placeholder", Valid: true},
	)
	createSpendCapActivityKey(t, db, organizationID, 100)

	updateLimit := func(args mock.Arguments, limit int64) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)
		require.NoError(t, openrouterrepo.New(db).UpdateOpenRouterKeyMonthlyCredits(ctx, openrouterrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
			MonthlyCredits: limit,
			OrganizationID: organizationID,
			KeyType:        string(openrouter.KeyTypeChat),
		}))
	}
	provisioner.On(
		"RefreshAPIKeyLimit",
		mock.Anything,
		organizationID,
		openrouter.KeyTypeChat,
		mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 600 }),
	).Run(func(args mock.Arguments) { updateLimit(args, 600) }).Return(600, nil).Once()
	provisioner.On(
		"RefreshAPIKeyLimit",
		mock.Anything,
		organizationID,
		openrouter.KeyTypeChat,
		mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 700 }),
	).Run(func(args mock.Arguments) { updateLimit(args, 700) }).Return(700, nil).Once()

	initialKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)

	firstAuditLogger := &failFirstSpendCapAuditLogger{delegate: audit.NewLogger()}
	firstSetter := activities.NewSetOpenRouterSpendCap(testenv.NewLogger(t), db, provisioner, firstAuditLogger)
	var suite testsuite.WorkflowTestSuite
	firstEnv := suite.NewTestActivityEnvironment()
	firstEnv.RegisterActivity(firstSetter.Do)
	_, err = firstEnv.ExecuteActivity(firstSetter.Do, spendCapActivityArgs("operation_first_placeholder", organizationID, 600))
	require.ErrorContains(t, err, "audit unavailable")
	firstAppliedKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)

	secondAuditLogger := &failFirstSpendCapAuditLogger{delegate: audit.NewLogger()}
	secondSetter := activities.NewSetOpenRouterSpendCap(testenv.NewLogger(t), db, provisioner, secondAuditLogger)
	secondEnv := suite.NewTestActivityEnvironment()
	secondEnv.RegisterActivity(secondSetter.Do)
	_, err = secondEnv.ExecuteActivity(secondSetter.Do, spendCapActivityArgs("operation_second_placeholder", organizationID, 700))
	require.ErrorContains(t, err, "audit unavailable")
	secondAppliedKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)

	firstRetryEnv := suite.NewTestActivityEnvironment()
	firstRetryEnv.RegisterActivity(firstSetter.Do)
	firstRetryEnv.SetHeartbeatDetails(spendCapHeartbeatFixture{
		BeforeMonthlyCredits: 100,
		ObservedKeyUpdatedAt: initialKey.UpdatedAt.Time,
		AppliedKeyUpdatedAt:  firstAppliedKey.UpdatedAt.Time,
		Applied:              true,
	})
	_, err = firstRetryEnv.ExecuteActivity(firstSetter.Do, spendCapActivityArgs("operation_first_placeholder", organizationID, 600))
	require.NoError(t, err)

	secondRetryEnv := suite.NewTestActivityEnvironment()
	secondRetryEnv.RegisterActivity(secondSetter.Do)
	secondRetryEnv.SetHeartbeatDetails(spendCapHeartbeatFixture{
		BeforeMonthlyCredits: 600,
		ObservedKeyUpdatedAt: firstAppliedKey.UpdatedAt.Time,
		AppliedKeyUpdatedAt:  secondAppliedKey.UpdatedAt.Time,
		Applied:              true,
	})
	_, err = secondRetryEnv.ExecuteActivity(secondSetter.Do, spendCapActivityArgs("operation_second_placeholder", organizationID, 700))
	require.NoError(t, err)
	provisioner.AssertExpectations(t)
	require.EqualValues(t, 1, firstAuditLogger.calls.Load(), "superseded retry must not audit")
	require.EqualValues(t, 2, secondAuditLogger.calls.Load(), "newest retry must audit")

	key, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.EqualValues(t, 700, key.MonthlyCredits)
	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOpenRouterAPIKeySetSpendCap)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	entry, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionOpenRouterAPIKeySetSpendCap)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.EqualValues(t, 600, before["monthly_credits"])
	require.EqualValues(t, 700, after["monthly_credits"])
}

func TestSetOpenRouterSpendCapSerializesConcurrentOperations(t *testing.T) {
	t.Parallel()

	_, provisioner, db, organizationID := setupPaygChatKeyReconciler(
		t,
		"payg",
		pgtype.Text{String: "subscription_placeholder", Valid: true},
	)
	createSpendCapActivityKey(t, db, organizationID, 100)

	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFirst) }) })

	updateLimit := func(args mock.Arguments, limit int64) {
		ctx, ok := args.Get(0).(context.Context)
		require.True(t, ok)
		require.NoError(t, openrouterrepo.New(db).UpdateOpenRouterKeyMonthlyCredits(ctx, openrouterrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
			MonthlyCredits: limit,
			OrganizationID: organizationID,
			KeyType:        string(openrouter.KeyTypeChat),
		}))
	}
	provisioner.On(
		"RefreshAPIKeyLimit",
		mock.Anything,
		organizationID,
		openrouter.KeyTypeChat,
		mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 600 }),
	).Run(func(args mock.Arguments) {
		close(firstStarted)
		<-releaseFirst
		updateLimit(args, 600)
	}).Return(600, nil).Once()
	provisioner.On(
		"RefreshAPIKeyLimit",
		mock.Anything,
		organizationID,
		openrouter.KeyTypeChat,
		mock.MatchedBy(func(limit *int) bool { return limit != nil && *limit == 700 }),
	).Run(func(args mock.Arguments) {
		close(secondStarted)
		updateLimit(args, 700)
	}).Return(700, nil).Once()

	setter := activities.NewSetOpenRouterSpendCap(testenv.NewLogger(t), db, provisioner, audit.NewLogger())
	run := func(args activities.SetOpenRouterSpendCapArgs) error {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestActivityEnvironment()
		env.SetTestTimeout(10 * time.Second)
		env.RegisterActivity(setter.Do)
		_, err := env.ExecuteActivity(setter.Do, args)
		if err != nil {
			return errors.Join(errors.New("execute spend-cap activity"), err)
		}
		return nil
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- run(spendCapActivityArgs("operation_first_placeholder", organizationID, 600)) }()
	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "first spend-cap operation did not reach OpenRouter")
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- run(spendCapActivityArgs("operation_second_placeholder", organizationID, 700)) }()
	select {
	case <-secondStarted:
		require.FailNow(t, "second spend-cap operation bypassed the billing lock")
	case <-time.After(150 * time.Millisecond):
	}

	releaseOnce.Do(func() { close(releaseFirst) })
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	provisioner.AssertExpectations(t)

	key, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.EqualValues(t, 700, key.MonthlyCredits)

	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOpenRouterAPIKeySetSpendCap)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}
