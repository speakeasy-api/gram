package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestMarkEnterpriseTrialConvertedRequestBody_RequiresOrganizationIDOnly(t *testing.T) {
	t.Parallel()

	id := "org_convert_validate"
	require.NoError(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{ID: &id}))
	require.Error(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{}))
	require.Error(t, srv.ValidateMarkEnterpriseTrialConvertedRequestBody(&srv.MarkEnterpriseTrialConvertedRequestBody{ID: new(string)}))
}

func TestMarkEnterpriseTrialConverted_AcceptsEveryEligibleStateAndPreservesHistory(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond).Add(time.Nanosecond)
	demotedAt := now.Add(-9 * 24 * time.Hour)
	disabledAt := now.Add(-8 * 24 * time.Hour)
	cases := []struct {
		name       string
		endsAt     time.Time
		demotedAt  *time.Time
		disabledAt *time.Time
	}{
		{name: "running", endsAt: now.Add(14 * 24 * time.Hour)},
		{name: "ending soon", endsAt: now.Add(2 * 24 * time.Hour)},
		{name: "expired", endsAt: now.Add(-2 * 24 * time.Hour)},
		{name: "demoted and disabled", endsAt: now.Add(-10 * 24 * time.Hour), demotedAt: &demotedAt, disabledAt: &disabledAt},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn, _ := newRearmService(t)
			orgID := "org_convert_" + tc.name
			accountType, whitelisted := "enterprise", true
			if tc.demotedAt != nil {
				accountType, whitelisted = "free", false
			}
			seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID + " Name", slug: orgID + "-slug", accountType: accountType, whitelisted: whitelisted, disabledAt: tc.disabledAt})
			seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: tc.endsAt, demotedAt: tc.demotedAt})

			before := readTrial(t, ctx, conn, orgID)
			trialQueries := trialsRepo.New(conn)
			started, err := trialQueries.GetTrialClockFixture(ctx)
			require.NoError(t, err)
			res, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			require.NoError(t, err)
			finished, err := trialQueries.GetTrialClockFixture(ctx)
			require.NoError(t, err)
			require.Equal(t, orgID, res.ID)

			after := readTrial(t, ctx, conn, orgID)
			require.True(t, after.ConvertedAt.Valid)
			require.False(t, after.ConvertedAt.Time.Before(started.Time))
			require.False(t, after.ConvertedAt.Time.After(finished.Time))
			require.Equal(t, before.EndsAt.Time, after.EndsAt.Time)
			require.Equal(t, before.DemotedAt, after.DemotedAt)

			state := readOrgState(t, ctx, conn, orgID)
			require.Equal(t, "enterprise", state.GramAccountType)
			require.True(t, state.Whitelisted)
			if tc.disabledAt != nil {
				require.True(t, state.DisabledAt.Valid)
				require.WithinDuration(t, *tc.disabledAt, state.DisabledAt.Time, time.Microsecond)
			}
		})
	}
}

func TestMarkEnterpriseTrialConverted_RestoresDemotedFeaturesAndKeysWithoutClearingOtherCauses(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_demoted_resources"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	seedDisabledTrialRuntimeFeatures(t, ctx, svc, conn, orgID)

	// Add independent causes to the trial cause seeded by seedDemotedTrial.
	for _, keyType := range openrouter.AllKeyTypes {
		for _, cause := range []openrouter.DisableCause{openrouter.DisableCauseAdminLock, openrouter.DisableCauseBillingInactive} {
			_, err := orrepo.New(conn).AddOpenRouterAPIKeyDisableCause(ctx, orrepo.AddOpenRouterAPIKeyDisableCauseParams{
				OrganizationID: orgID,
				KeyType:        string(keyType),
				DisableCause:   string(cause),
			})
			require.NoError(t, err)
		}
	}

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)

	for _, keyType := range openrouter.AllKeyTypes {
		key := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.EqualValues(t, 100, key.MonthlyCredits)
		require.NotContains(t, key.DisableCauses, string(openrouter.DisableCauseTrialDemotion))
		require.Contains(t, key.DisableCauses, string(openrouter.DisableCauseAdminLock))
		require.Contains(t, key.DisableCauses, string(openrouter.DisableCauseBillingInactive))
	}
	require.Len(t, provisioner.revivals, len(openrouter.AllKeyTypes))
	for _, call := range provisioner.revivals {
		require.NotNil(t, call.limit)
		require.Equal(t, 100, *call.limit, "conversion must send the explicit enterprise ceiling upstream before commit")
		require.Equal(t, "free", call.accountTypeSeen, "upstream work must precede admission commit")
		require.True(t, call.demotedSeen, "upstream work must precede trial conversion commit")
	}

	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, err := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, err)
		require.True(t, enabled, "%s must be restored for a converted demoted trial", feature)
	}
}

func TestMarkEnterpriseTrialConverted_IsIdempotentAndAlwaysNotifiesInactive(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_convert_idempotent"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Idempotent Co", slug: "idempotent-co", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(10 * 24 * time.Hour)})
	notifier := &fakeTrialNotifier{inactiveErr: assertiveNotifierError{}}
	svc.trial = notifier
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "operator-placeholder", Name: "Test Operator", Email: "operator@example.test"})

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	first := readTrial(t, ctx, conn, orgID).ConvertedAt.Time
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	require.Equal(t, first, readTrial(t, ctx, conn, orgID).ConvertedAt.Time)
	require.Equal(t, []string{orgID, orgID}, notifier.inactive)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	var metadata map[string]string
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, "admin", metadata["conversion_source"])
	require.Equal(t, "Test Operator", *entry.ActorDisplayName)
}

func TestMarkEnterpriseTrialConverted_RejectsMissingNoTrialAndPayg(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_convert_no_trial", name: "No Trial", slug: "no-trial", accountType: "enterprise", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_convert_payg", name: "PAYG", slug: "payg", accountType: "payg", whitelisted: true})
	convertedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond).Add(time.Nanosecond)
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_convert_payg", endsAt: time.Now().UTC().Add(10 * 24 * time.Hour), convertedAt: &convertedAt})

	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: "org_convert_missing"})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: "org_convert_no_trial"})
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: "org_convert_payg"})
	requireOopsCode(t, err, oops.CodeConflict)
	require.WithinDuration(t, convertedAt, readTrial(t, ctx, conn, "org_convert_payg").ConvertedAt.Time, time.Microsecond)
}

type assertiveNotifierError struct{}

func (assertiveNotifierError) Error() string { return "notifier unavailable" }

type concurrentTrialNotifier struct {
	mu       sync.Mutex
	inactive []string
}

func (*concurrentTrialNotifier) TrialStarted(context.Context, string) error       { return nil }
func (*concurrentTrialNotifier) AdminAdded(context.Context, string, string) error { return nil }

func (n *concurrentTrialNotifier) TrialInactive(_ context.Context, organizationID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.inactive = append(n.inactive, organizationID)
	return nil
}

func (n *concurrentTrialNotifier) inactiveCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.inactive)
}

func waitForBlockedBackendCount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, want int64) {
	t.Helper()

	require.Eventually(t, func() bool {
		var blocked int64
		//nolint:glint // notestingrawsql: PostgreSQL system view is absent from SQLc's schema snapshot.
		err := conn.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
		`).Scan(&blocked)
		return err == nil && blocked >= want
	}, 5*time.Second, 10*time.Millisecond, "expected at least %d blocked backends", want)
}

func TestMarkEnterpriseTrialConverted_AuditFailureRollsBackConversionAndRestoration(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	orgID := "org_convert_audit_atomic_" + uuid.NewString()
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	seedDisabledTrialRuntimeFeatures(t, ctx, svc, conn, orgID)
	before := readTrial(t, ctx, conn, orgID)
	outboxBefore, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(events.OrganizationEnterpriseTrialV1.EventType()))
	require.NoError(t, err)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted))

	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeUnexpected)

	after := readTrial(t, ctx, conn, orgID)
	require.False(t, after.ConvertedAt.Valid)
	require.Equal(t, before.DemotedAt, after.DemotedAt)
	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "free", state.GramAccountType)
	require.False(t, state.Whitelisted)
	outboxAfter, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(events.OrganizationEnterpriseTrialV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, outboxBefore, outboxAfter)
	require.Empty(t, notifier.inactive)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, featureErr := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, featureErr)
		require.False(t, enabled, "%s restoration must roll back with the rejected audit", feature)
	}
	for _, keyType := range openrouter.AllKeyTypes {
		key := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.EqualValues(t, 100, key.MonthlyCredits, "pre-commit upstream limit work is intentionally durable")
		require.NotContains(t, key.DisableCauses, string(openrouter.DisableCauseTrialDemotion), "pre-commit key-cause work is intentionally durable")
		require.False(t, key.Disabled)
	}
}

func TestMarkEnterpriseTrialConverted_ConcurrentAndReplayedCallsCommitOneConversion(t *testing.T) { //nolint:paralleltest // Coordinates writers against one trial row.
	baseCtx, svc, conn, _ := newRearmService(t)
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()
	const orgID = "org_convert_concurrent"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Concurrent Conversion", slug: "concurrent-conversion", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(24 * time.Hour)})
	notifier := &concurrentTrialNotifier{}
	svc.trial = notifier

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	first := make(chan error, 1)
	second := make(chan error, 1)

	err = keybillinglock.WithAcquireTimeout(ctx, testenv.NewLogger(t), conn, orgID, openrouter.KeyTypeChat, time.Second, func(_ *pgxpool.Conn) error {
		go func() {
			_, callErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			first <- callErr
		}()
		waitForBlockedBackendCount(t, ctx, conn, 1) // first owns the trial row and waits for chat

		go func() {
			_, callErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			second <- callErr
		}()
		waitForBlockedBackendCount(t, ctx, conn, 2) // second waits for the first writer's trial row
		return nil
	})
	require.NoError(t, err)

	for index, result := range []<-chan error{first, second} {
		select {
		case callErr := <-result:
			require.NoError(t, callErr, "conversion call %d", index+1)
		case <-time.After(5 * time.Second):
			t.Fatalf("conversion call %d did not complete", index+1)
		}
	}
	trial := readTrial(t, ctx, conn, orgID)
	require.True(t, trial.ConvertedAt.Valid)
	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	require.Equal(t, 2, notifier.inactiveCount(), "every successful conversion replay must enqueue TrialInactive")
}

func TestMarkEnterpriseTrialConverted_UpstreamFailureRollsBackAndRetryConverges(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, provisioner := newRearmService(t)
	orgID := "org_convert_upstream_retry_" + uuid.NewString()
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	seedDisabledTrialRuntimeFeatures(t, ctx, svc, conn, orgID)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier
	provisioner.failOn = openrouter.KeyTypeInternal
	provisioner.failWith = errors.New("upstream unavailable")

	auditBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	outboxBefore, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(events.OrganizationEnterpriseTrialV1.EventType()))
	require.NoError(t, err)

	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	requireOopsCode(t, err, oops.CodeGatewayError)
	trial := readTrial(t, ctx, conn, orgID)
	require.False(t, trial.ConvertedAt.Valid)
	require.True(t, trial.DemotedAt.Valid)
	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "free", state.GramAccountType)
	require.False(t, state.Whitelisted)
	require.Empty(t, notifier.inactive)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, featureErr := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, featureErr)
		require.False(t, enabled, "%s admission must remain disabled after upstream failure", feature)
	}
	chatAfterFailure := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	require.EqualValues(t, 100, chatAfterFailure.MonthlyCredits)
	require.NotContains(t, chatAfterFailure.DisableCauses, string(openrouter.DisableCauseTrialDemotion))
	require.False(t, chatAfterFailure.Disabled)
	internalAfterFailure := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeInternal)
	require.EqualValues(t, 37, internalAfterFailure.MonthlyCredits)
	require.Contains(t, internalAfterFailure.DisableCauses, string(openrouter.DisableCauseTrialDemotion))
	require.True(t, internalAfterFailure.Disabled)

	auditAfterFailure, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Equal(t, auditBefore, auditAfterFailure)
	outboxAfterFailure, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(events.OrganizationEnterpriseTrialV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, outboxBefore, outboxAfterFailure)

	provisioner.failWith = nil
	_, err = svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	require.True(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid)
	state = readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)
	require.Equal(t, []string{orgID}, notifier.inactive)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, featureErr := svc.productFeatures.IsFeatureEnabled(ctx, orgID, feature)
		require.NoError(t, featureErr)
		require.True(t, enabled, "%s admission must converge after retry", feature)
	}
	for _, keyType := range openrouter.AllKeyTypes {
		key := readOpenRouterKey(t, ctx, conn, orgID, keyType)
		require.EqualValues(t, 100, key.MonthlyCredits)
		require.NotContains(t, key.DisableCauses, string(openrouter.DisableCauseTrialDemotion))
		require.False(t, key.Disabled)
	}

	auditAfterRetry, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfterRetry)
	outboxAfterRetry, err := testrepo.New(conn).CountOutboxEntriesByEventType(ctx, string(events.OrganizationEnterpriseTrialV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, outboxBefore+1, outboxAfterRetry)
}

func TestMarkEnterpriseTrialConverted_TrialRowPrecedesChatAndInternalKeyLocks(t *testing.T) { //nolint:paralleltest // Deliberately coordinates lock owners.
	baseCtx, svc, conn, _ := newRearmService(t)
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()
	const orgID = "org_convert_lock_order"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Lock Order", slug: "lock-order", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(24 * time.Hour)})

	trialOwner := testenv.BeginTx(t, ctx, conn)
	defer func() { _ = trialOwner.Rollback(baseCtx) }()
	lockedID, err := repo.New(trialOwner).LockTrialForUpdate(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, orgID, lockedID)

	converted := make(chan error, 1)
	logger := testenv.NewLogger(t)
	err = keybillinglock.WithAcquireTimeout(ctx, logger, conn, orgID, openrouter.KeyTypeInternal, time.Second, func(_ *pgxpool.Conn) error {
		go func() {
			_, conversionErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
			converted <- conversionErr
		}()
		waitForBlockedBackendCount(t, ctx, conn, 1)

		// While conversion waits for the trial row, neither key lock may be held.
		chatErr := keybillinglock.WithAcquireTimeout(ctx, logger, conn, orgID, openrouter.KeyTypeChat, 250*time.Millisecond, func(_ *pgxpool.Conn) error { return nil })
		require.NoError(t, chatErr)
		require.NoError(t, trialOwner.Commit(ctx))

		// The internal lock remains ours. A correct conversion therefore takes chat
		// and waits here; an internal-before-chat implementation leaves chat free.
		require.Eventually(t, func() bool {
			chatErr = keybillinglock.WithAcquireTimeout(ctx, logger, conn, orgID, openrouter.KeyTypeChat, 100*time.Millisecond, func(_ *pgxpool.Conn) error { return nil })
			return errors.Is(chatErr, keybillinglock.ErrAcquireTimeout)
		}, 3*time.Second, 10*time.Millisecond, "conversion must hold chat before waiting for internal")
		return nil
	})
	require.NoError(t, err)

	select {
	case err = <-converted:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("conversion deadlocked while acquiring chat then internal billing locks")
	}
}

func TestMarkEnterpriseTrialConverted_SerializesBehindTrialDemotion(t *testing.T) { //nolint:paralleltest // Coordinates two lifecycle writers.
	baseCtx, svc, conn, _ := newRearmService(t)
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()
	const orgID = "org_convert_after_demotion"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Demotion Race", slug: "demotion-race", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(-24 * time.Hour)})

	demotionTx := testenv.BeginTx(t, ctx, conn)
	defer func() { _ = demotionTx.Rollback(baseCtx) }()
	demotionQueries := trialsRepo.New(demotionTx)
	_, err := demotionQueries.MarkTrialDemoted(ctx, orgID)
	require.NoError(t, err)
	_, err = demotionQueries.DemoteOrganizationToFree(ctx, orgID)
	require.NoError(t, err)

	converted := make(chan error, 1)
	go func() {
		_, conversionErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		converted <- conversionErr
	}()
	waitForBlockedBackendCount(t, ctx, conn, 1)
	require.NoError(t, demotionTx.Commit(ctx))

	select {
	case err = <-converted:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("conversion deadlocked behind trial demotion")
	}
	trial := readTrial(t, ctx, conn, orgID)
	require.True(t, trial.ConvertedAt.Valid)
	require.True(t, trial.DemotedAt.Valid, "conversion preserves demotion history")
	state := readOrgState(t, ctx, conn, orgID)
	require.Equal(t, "enterprise", state.GramAccountType)
	require.True(t, state.Whitelisted)
}

func TestMarkEnterpriseTrialConverted_SerializesBehindTrialRearm(t *testing.T) { //nolint:paralleltest // Coordinates two lifecycle writers.
	baseCtx, svc, conn, _ := newRearmService(t)
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()
	const orgID = "org_convert_after_rearm"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")

	rearmTx := testenv.BeginTx(t, ctx, conn)
	defer func() { _ = rearmTx.Rollback(baseCtx) }()
	rearmQueries := trialsRepo.New(rearmTx)
	_, err := rearmQueries.RearmTrial(ctx, trialsRepo.RearmTrialParams{OrganizationID: orgID, RearmForDays: 14})
	require.NoError(t, err)
	_, err = rearmQueries.RestoreOrganizationFromTrial(ctx, trialsRepo.RestoreOrganizationFromTrialParams{OrganizationID: orgID, AccountType: "enterprise"})
	require.NoError(t, err)

	converted := make(chan error, 1)
	go func() {
		_, conversionErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		converted <- conversionErr
	}()
	waitForBlockedBackendCount(t, ctx, conn, 1)
	require.NoError(t, rearmTx.Commit(ctx))

	select {
	case err = <-converted:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("conversion deadlocked behind trial re-arm")
	}
	require.True(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid)
	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestMarkEnterpriseTrialConverted_SerializesWithBillingCauseMutation(t *testing.T) { //nolint:paralleltest // Coordinates the chat-key lock holder and conversion.
	baseCtx, svc, conn, _ := newRearmService(t)
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()
	const orgID = "org_convert_cause_race"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")

	mutationStarted := make(chan struct{})
	releaseMutation := make(chan struct{})
	mutationDone := make(chan error, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseMutation) }) }
	defer release()
	go func() {
		mutationDone <- keybillinglock.WithAcquireTimeout(ctx, testenv.NewLogger(t), conn, orgID, openrouter.KeyTypeChat, time.Second, func(keyConn *pgxpool.Conn) error {
			for _, cause := range []openrouter.DisableCause{openrouter.DisableCauseAdminLock, openrouter.DisableCauseBillingInactive} {
				_, mutationErr := orrepo.New(keyConn).AddOpenRouterAPIKeyDisableCause(ctx, orrepo.AddOpenRouterAPIKeyDisableCauseParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), DisableCause: string(cause)})
				if mutationErr != nil {
					return fmt.Errorf("add %s disable cause: %w", cause, mutationErr)
				}
			}
			close(mutationStarted)
			select {
			case <-releaseMutation:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-mutationStarted:
	case mutationErr := <-mutationDone:
		require.NoError(t, mutationErr)
		t.Fatal("billing mutation exited before acquiring the chat lock")
	case <-time.After(3 * time.Second):
		t.Fatal("billing mutation did not acquire the chat lock")
	}

	converted := make(chan error, 1)
	go func() {
		_, conversionErr := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
		converted <- conversionErr
	}()
	waitForBlockedBackendCount(t, ctx, conn, 1)
	release()
	select {
	case mutationErr := <-mutationDone:
		require.NoError(t, mutationErr)
	case <-time.After(3 * time.Second):
		t.Fatal("billing mutation did not release the chat lock")
	}

	select {
	case err := <-converted:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("conversion deadlocked behind billing cause mutation")
	}
	key := readOpenRouterKey(t, ctx, conn, orgID, openrouter.KeyTypeChat)
	require.NotContains(t, key.DisableCauses, string(openrouter.DisableCauseTrialDemotion))
	require.Contains(t, key.DisableCauses, string(openrouter.DisableCauseAdminLock))
	require.Contains(t, key.DisableCauses, string(openrouter.DisableCauseBillingInactive))
	require.True(t, key.Disabled, "remaining causes must keep effective access disabled")
}

func TestMarkEnterpriseTrialConverted_CannotBeDemotedOrRearmed(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, _ := newRearmService(t)
	const orgID = "org_convert_terminal"
	seedDemotedTrial(t, ctx, conn, orgID, "enterprise")
	_, err := svc.MarkEnterpriseTrialConverted(ctx, &gen.MarkEnterpriseTrialConvertedPayload{ID: orgID})
	require.NoError(t, err)
	converted := readTrial(t, ctx, conn, orgID)

	_, err = trialsRepo.New(conn).MarkTrialDemoted(ctx, orgID)
	require.Error(t, err)
	_, err = svc.RearmTrial(ctx, &gen.RearmTrialPayload{ID: orgID, Days: 14})
	requireOopsCode(t, err, oops.CodeConflict)
	after := readTrial(t, ctx, conn, orgID)
	require.Equal(t, converted.ConvertedAt, after.ConvertedAt)
	require.Equal(t, converted.EndsAt, after.EndsAt)
}

func TestUpdateOrganization_DoesNotInferEnterpriseTrialConversion(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_update_not_conversion"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Update Only", slug: "update-only", accountType: "free", whitelisted: false})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: time.Now().UTC().Add(10 * 24 * time.Hour)})
	enterprise := "enterprise"

	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: orgID, AccountType: &enterprise})
	require.NoError(t, err)
	require.False(t, readTrial(t, ctx, conn, orgID).ConvertedAt.Valid)
}
