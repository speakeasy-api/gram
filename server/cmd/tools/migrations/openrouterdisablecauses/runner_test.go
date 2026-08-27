package openrouterdisablecauses

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type runnerFixture struct {
	pool *pgxpool.Pool
}

func newRunnerFixture(t *testing.T) *runnerFixture {
	t.Helper()
	pool, err := testInfra.CloneTestDatabase(t, "openrouterdisablecauses")
	require.NoError(t, err)
	return &runnerFixture{pool: pool}
}

func (f *runnerFixture) seedKey(t *testing.T, keyType string, disabled bool, accountType string) string {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	q := New(f.pool)
	require.NoError(t, q.SeedOrganizationFixture(t.Context(), SeedOrganizationFixtureParams{OrganizationID: orgID, AccountType: accountType}))
	require.NoError(t, q.SeedOpenRouterKeyFixture(t.Context(), SeedOpenRouterKeyFixtureParams{OrganizationID: orgID, KeyType: keyType, Disabled: disabled}))
	return orgID
}

func (f *runnerFixture) causes(t *testing.T, orgID, keyType string) []string {
	t.Helper()
	causes, err := New(f.pool).GetOpenRouterDisableCausesFixture(t.Context(), GetOpenRouterDisableCausesFixtureParams{OrganizationID: orgID, KeyType: keyType})
	require.NoError(t, err)
	return causes
}

func (f *runnerFixture) seedTrial(t *testing.T, orgID string, demotedAt, convertedAt *time.Time, endsAt time.Time) {
	t.Helper()
	require.NoError(t, New(f.pool).SeedTrialFixture(t.Context(), SeedTrialFixtureParams{
		OrganizationID: orgID,
		EndsAt:         pgtype.Timestamptz{Time: endsAt, Valid: true},
		DemotedAt:      nullableTimestamptz(demotedAt),
		ConvertedAt:    nullableTimestamptz(convertedAt),
	}))
}

func (f *runnerFixture) seedBilling(t *testing.T, orgID string, subscription *string) {
	t.Helper()
	value := pgtype.Text{}
	if subscription != nil {
		value = pgtype.Text{String: *subscription, Valid: true}
	}
	require.NoError(t, New(f.pool).SeedBillingFixture(t.Context(), SeedBillingFixtureParams{OrganizationID: orgID, StripeSubscriptionID: value}))
}

func nullableTimestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *value, Valid: true}
}

func (f *runnerFixture) seedAdminAudit(t *testing.T, orgID, keyType, action string, malformed bool) {
	t.Helper()
	metadata := fmt.Sprintf(`{"key_type":%q}`, keyType)
	if malformed {
		metadata = `{"key_type":42}`
	}
	require.NoError(t, New(f.pool).SeedAdminAuditFixture(t.Context(), SeedAdminAuditFixtureParams{
		OrganizationID: orgID,
		Action:         action,
		SubjectID:      "openrouter_api_key:" + orgID + "/" + keyType,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       []byte(metadata),
	}))
}

func newTestRunner(pool *pgxpool.Pool, batchSize int) *Runner {
	return NewRunner(pool, slog.New(slog.DiscardHandler), Options{BatchSize: batchSize, LockTimeout: time.Second, StatementTimeout: 5 * time.Second, MaxLockRetries: 2})
}

func TestNewRunnerBoundsBatchSizeForSQLcParameter(t *testing.T) {
	t.Parallel()
	runner := NewRunner(nil, slog.New(slog.DiscardHandler), Options{BatchSize: math.MaxInt})
	require.Equal(t, math.MaxInt32, runner.options.BatchSize)
}

func TestRunnerSafeDryRunSucceedsWithoutWrites(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	orgID := f.seedKey(t, "chat", false, "free")

	summary, err := newTestRunner(f.pool, 10).Run(t.Context(), ModeDryRun)
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.Classified)
	require.Equal(t, int64(1), summary.RemainingNulls)
	require.Nil(t, f.causes(t, orgID, "chat"))
}

func TestRunnerDryRunApplyResumeAndIdempotency(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	now := time.Now().UTC()
	enabled := f.seedKey(t, "chat", false, "free")
	trial := f.seedKey(t, "internal", true, "free")
	f.seedTrial(t, trial, &now, nil, now.Add(-time.Hour))
	ambiguous := f.seedKey(t, "internal", true, "free")

	runner := newTestRunner(f.pool, 1)
	dry, err := runner.Run(t.Context(), ModeDryRun)
	require.ErrorIs(t, err, ErrAmbiguousRows)
	require.Equal(t, int64(3), dry.Scanned)
	require.Equal(t, int64(2), dry.Classified)
	require.Equal(t, int64(1), dry.Ambiguous[AmbiguousNoProvenance])
	require.Nil(t, f.causes(t, enabled, "chat"))
	require.Nil(t, f.causes(t, trial, "internal"))

	applied, err := runner.Run(t.Context(), ModeApply)
	require.ErrorIs(t, err, ErrAmbiguousRows)
	require.Equal(t, int64(2), applied.Updated)
	require.Empty(t, f.causes(t, enabled, "chat"))
	require.Equal(t, []string{CauseTrialDemotion}, f.causes(t, trial, "internal"))
	require.Nil(t, f.causes(t, ambiguous, "internal"))

	rerun, err := runner.Run(t.Context(), ModeApply)
	require.ErrorIs(t, err, ErrAmbiguousRows)
	require.Zero(t, rerun.Updated)
	require.Equal(t, int64(1), rerun.Scanned)
}

func TestRunnerClassifiesDurableProjections(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	now := time.Now().UTC()

	converted := f.seedKey(t, "internal", true, "free")
	demotedAt, convertedAt := now.Add(-2*time.Hour), now.Add(-time.Hour)
	f.seedTrial(t, converted, &demotedAt, &convertedAt, now.Add(-3*time.Hour))

	billing := f.seedKey(t, "chat", true, "base")
	f.seedBilling(t, billing, nil)

	multiple := f.seedKey(t, "chat", true, "base")
	f.seedTrial(t, multiple, &demotedAt, nil, now.Add(-3*time.Hour))
	f.seedBilling(t, multiple, nil)
	f.seedAdminAudit(t, multiple, "chat", "openrouter-key:disable", false)

	latestEnable := f.seedKey(t, "internal", true, "free")
	f.seedTrial(t, latestEnable, &demotedAt, nil, now.Add(-3*time.Hour))
	f.seedAdminAudit(t, latestEnable, "internal", "openrouter-key:disable", false)
	f.seedAdminAudit(t, latestEnable, "internal", "openrouter-key:enable", false)

	badBilling := f.seedKey(t, "chat", true, "base")
	sub := "sub_test"
	f.seedBilling(t, badBilling, &sub)

	badAudit := f.seedKey(t, "internal", true, "free")
	f.seedAdminAudit(t, badAudit, "internal", "openrouter-key:disable", true)

	wrongKeyTypeAudit := f.seedKey(t, "internal", true, "free")
	require.NoError(t, New(f.pool).SeedAdminAuditFixture(t.Context(), SeedAdminAuditFixtureParams{
		OrganizationID: wrongKeyTypeAudit,
		Action:         "openrouter-key:disable",
		SubjectID:      "openrouter_api_key:" + wrongKeyTypeAudit + "/internal",
		Metadata:       []byte(`{"key_type":"chat"}`),
	}))

	summary, err := newTestRunner(f.pool, 100).Run(t.Context(), ModeApply)
	require.ErrorIs(t, err, ErrAmbiguousRows)
	require.Equal(t, []string{CauseTrialDemotion}, f.causes(t, converted, "internal"))
	require.Equal(t, []string{CauseBillingInactive}, f.causes(t, billing, "chat"))
	require.Equal(t, []string{CauseAdminLock, CauseTrialDemotion, CauseBillingInactive}, f.causes(t, multiple, "chat"))
	require.Equal(t, []string{CauseTrialDemotion}, f.causes(t, latestEnable, "internal"))
	require.Nil(t, f.causes(t, badBilling, "chat"))
	require.Nil(t, f.causes(t, badAudit, "internal"))
	require.Nil(t, f.causes(t, wrongKeyTypeAudit, "internal"))
	require.Equal(t, int64(1), summary.Ambiguous[AmbiguousBillingProjection])
	require.Equal(t, int64(2), summary.Ambiguous[AmbiguousAdminAudit])
}

func TestRunnerConcurrentApplyUsesSkipLocked(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	for range 20 {
		f.seedKey(t, "chat", false, "free")
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			<-start
			_, err := newTestRunner(f.pool, 1).Run(context.Background(), ModeApply)
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	remaining, err := New(f.pool).CountLiveNullClassifications(t.Context())
	require.NoError(t, err)
	require.Zero(t, remaining)
}

func TestRunnerValidationFindsEveryUnsafeClass(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)

	nullRow := f.seedKey(t, "chat", false, "free")
	_ = nullRow
	ambiguousNull := f.seedKey(t, "internal", true, "free")
	_ = ambiguousNull
	mirror := f.seedKey(t, "chat", false, "free")
	unknown := f.seedKey(t, "chat", true, "free")
	duplicate := f.seedKey(t, "chat", true, "free")
	reclassified := f.seedKey(t, "chat", true, "base")
	f.seedBilling(t, reclassified, nil)
	q := New(f.pool)
	require.NoError(t, q.SetOpenRouterClassificationFixture(t.Context(), SetOpenRouterClassificationFixtureParams{OrganizationID: mirror, KeyType: "chat", DisableCauses: []string{"admin_lock"}, Disabled: false}))
	require.NoError(t, q.SetOpenRouterClassificationFixture(t.Context(), SetOpenRouterClassificationFixtureParams{OrganizationID: unknown, KeyType: "chat", DisableCauses: []string{"future_cause"}, Disabled: true}))
	require.NoError(t, q.SetOpenRouterClassificationFixture(t.Context(), SetOpenRouterClassificationFixtureParams{OrganizationID: duplicate, KeyType: "chat", DisableCauses: []string{"admin_lock", "admin_lock"}, Disabled: true}))
	require.NoError(t, q.SetOpenRouterClassificationFixture(t.Context(), SetOpenRouterClassificationFixtureParams{OrganizationID: reclassified, KeyType: "chat", DisableCauses: []string{"admin_lock"}, Disabled: true}))

	summary, err := newTestRunner(f.pool, 2).Run(t.Context(), ModeValidate)
	require.ErrorIs(t, err, ErrValidationFailed)
	require.Equal(t, int64(2), summary.Validation[ValidationNull])
	require.Equal(t, int64(1), summary.Validation[ValidationMirrorMismatch])
	require.Equal(t, int64(1), summary.Validation[ValidationUnknownCause])
	require.Equal(t, int64(1), summary.Validation[ValidationDuplicateCause])
	require.GreaterOrEqual(t, summary.Validation[ValidationAmbiguous], int64(1))
	require.GreaterOrEqual(t, summary.Validation[ValidationReclassificationMismatch], int64(1))
}

func TestRunnerValidationRequiresEvidenceForEveryAmbiguousStoredClassification(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	now := time.Now().UTC()

	noProvenance := f.seedKey(t, "internal", true, "free")
	malformedAudit := f.seedKey(t, "internal", true, "free")
	f.seedAdminAudit(t, malformedAudit, "internal", "openrouter-key:disable", true)
	inconsistentBilling := f.seedKey(t, "chat", true, "base")
	subscription := "sub_test"
	f.seedBilling(t, inconsistentBilling, &subscription)
	contradictoryTrial := f.seedKey(t, "internal", true, "free")
	demotedAt := now.Add(-time.Hour)
	f.seedTrial(t, contradictoryTrial, &demotedAt, nil, now.Add(time.Hour))

	q := New(f.pool)
	var overrides []ManualOverride
	for _, target := range []struct{ organizationID, keyType string }{
		{noProvenance, "internal"}, {malformedAudit, "internal"},
		{inconsistentBilling, "chat"}, {contradictoryTrial, "internal"},
	} {
		require.NoError(t, q.SetOpenRouterClassificationFixture(t.Context(), SetOpenRouterClassificationFixtureParams{
			OrganizationID: target.organizationID, KeyType: target.keyType, DisableCauses: []string{CauseAdminLock}, Disabled: true,
		}))
		overrides = append(overrides, ManualOverride{OrganizationID: target.organizationID, KeyType: target.keyType, Causes: []string{CauseAdminLock}})
	}

	summary, err := newTestRunner(f.pool, 2).Run(t.Context(), ModeValidate)
	require.ErrorIs(t, err, ErrValidationFailed)
	require.Equal(t, int64(4), summary.Validation[ValidationAmbiguous])

	summary, err = newTestRunner(f.pool, 2).Validate(t.Context(), overrides)
	require.NoError(t, err)
	require.Empty(t, summary.Validation)
}

func TestRunnerValidationRejectsPopulationChangedDuringPass(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	first := f.seedKey(t, "chat", false, "free")
	second := f.seedKey(t, "chat", false, "free")
	q := New(f.pool)
	for _, orgID := range []string{first, second} {
		require.NoError(t, q.SetOpenRouterClassificationFixture(t.Context(), SetOpenRouterClassificationFixtureParams{
			OrganizationID: orgID, KeyType: "chat", DisableCauses: []string{}, Disabled: false,
		}))
	}

	runner := newTestRunner(f.pool, 1)
	var once sync.Once
	runner.afterValidationBatch = func() {
		once.Do(func() {
			require.NoError(t, q.TouchOpenRouterClassificationFixture(t.Context(), TouchOpenRouterClassificationFixtureParams{
				OrganizationID: first, KeyType: "chat",
			}))
		})
	}

	summary, err := runner.Run(t.Context(), ModeValidate)
	require.ErrorIs(t, err, ErrValidationFailed)
	require.Positive(t, summary.Validation[ValidationUnstable])
}

func TestRunnerValidationDetectsBehindCursorNullReset(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	first := f.seedKey(t, "chat", false, "free")
	second := f.seedKey(t, "chat", false, "free")
	q := New(f.pool)
	for _, orgID := range []string{first, second} {
		require.NoError(t, q.SetOpenRouterClassificationFixture(t.Context(), SetOpenRouterClassificationFixtureParams{
			OrganizationID: orgID, KeyType: "chat", DisableCauses: []string{}, Disabled: false,
		}))
	}

	runner := newTestRunner(f.pool, 1)
	var once sync.Once
	runner.afterValidationBatch = func() {
		once.Do(func() {
			require.NoError(t, q.ResetOpenRouterClassificationFixture(t.Context(), ResetOpenRouterClassificationFixtureParams{
				OrganizationID: first, KeyType: "chat",
			}))
			require.NoError(t, q.ResetOpenRouterClassificationFixture(t.Context(), ResetOpenRouterClassificationFixtureParams{
				OrganizationID: second, KeyType: "chat",
			}))
		})
	}

	summary, err := runner.Run(t.Context(), ModeValidate)
	require.ErrorIs(t, err, ErrValidationFailed)
	require.Positive(t, summary.Validation[ValidationNull])
}

func TestRunnerManualOverrideIsAuthorizedIdempotentAndPreservesAccess(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	orgID := f.seedKey(t, "internal", true, "free")
	runner := newTestRunner(f.pool, 1)

	require.ErrorIs(t, AuthorizeManualOverride("wrong", "protected-token"), ErrManualOverrideUnauthorized)
	require.NoError(t, AuthorizeManualOverride("protected-token", "protected-token"))

	req := ManualOverride{OrganizationID: orgID, KeyType: "internal", Causes: []string{CauseAdminLock}}
	changed, err := runner.ApplyManualOverride(t.Context(), req)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, []string{CauseAdminLock}, f.causes(t, orgID, "internal"))

	changed, err = runner.ApplyManualOverride(t.Context(), req)
	require.NoError(t, err)
	require.False(t, changed)

	validation, err := runner.Run(t.Context(), ModeValidate)
	require.ErrorIs(t, err, ErrValidationFailed)
	require.Equal(t, int64(1), validation.Validation[ValidationAmbiguous])

	validation, err = runner.Validate(t.Context(), []ManualOverride{req})
	require.NoError(t, err)
	require.Empty(t, validation.Validation)

	_, err = runner.ApplyManualOverride(t.Context(), ManualOverride{OrganizationID: orgID, KeyType: "internal", Causes: []string{CauseTrialDemotion}})
	require.ErrorIs(t, err, ErrManualOverrideConflict)
}

func TestRunnerBoundsStatementTimeoutRetries(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	f.seedKey(t, "chat", false, "free")

	blocker := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, New(blocker).LockAuditLogsFixture(t.Context()))

	runner := NewRunner(f.pool, slog.New(slog.DiscardHandler), Options{BatchSize: 1, LockTimeout: 25 * time.Millisecond, StatementTimeout: 50 * time.Millisecond, MaxLockRetries: 1})
	started := time.Now()
	summary, err := runner.Run(t.Context(), ModeApply)
	require.Error(t, err)
	require.Equal(t, int64(1), summary.LockRetries)
	require.Less(t, time.Since(started), time.Second)
}

func TestRunnerBoundsTerminalCountStatementTimeout(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	f.seedKey(t, "chat", false, "free")

	runner := NewRunner(f.pool, slog.New(slog.DiscardHandler), Options{BatchSize: 1, LockTimeout: 25 * time.Millisecond, StatementTimeout: 50 * time.Millisecond})
	blocker := testenv.BeginTx(t, t.Context(), f.pool)
	runner.beforeRemainingCount = func() {
		require.NoError(t, New(blocker).LockOpenRouterKeysFixture(t.Context()))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runner.Run(ctx, ModeApply)
	require.Error(t, err)
	require.Less(t, time.Since(started), 250*time.Millisecond)
}

func TestRunnerValidationQueryUsesStatementTimeout(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	f.seedKey(t, "chat", false, "free")
	blocker := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, New(blocker).LockAuditLogsFixture(t.Context()))

	runner := NewRunner(f.pool, slog.New(slog.DiscardHandler), Options{BatchSize: 1, LockTimeout: 25 * time.Millisecond, StatementTimeout: 50 * time.Millisecond})
	started := time.Now()
	_, err := runner.Run(t.Context(), ModeValidate)
	require.Error(t, err)
	require.Less(t, time.Since(started), 250*time.Millisecond)
}

func TestRunnerCrashRollsBackBatchAndResumes(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	for range 3 {
		f.seedKey(t, "chat", false, "free")
	}

	runner := newTestRunner(f.pool, 2)
	runner.beforeCommit = func() error { return errors.New("simulated crash") }
	failed, err := runner.Run(t.Context(), ModeApply)
	require.ErrorContains(t, err, "simulated crash")
	require.Zero(t, failed.Updated, "rolled-back rows must not be reported as durable updates")

	remaining, err := New(f.pool).CountAllNullClassificationsFixture(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(3), remaining)

	summary, err := newTestRunner(f.pool, 2).Run(t.Context(), ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(3), summary.Updated)
}
