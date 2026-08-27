package openrouterdisablecauses

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
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
	_, err := f.pool.Exec(t.Context(), `INSERT INTO organization_metadata (id, name, slug, gram_account_type) VALUES ($1, 'test', $1, $2)`, orgID, accountType)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `INSERT INTO openrouter_api_keys (organization_id, key_type, key_hash, disabled, disable_causes) VALUES ($1, $2, 'test-hash', $3, NULL)`, orgID, keyType, disabled)
	require.NoError(t, err)
	return orgID
}

func (f *runnerFixture) causes(t *testing.T, orgID, keyType string) []string {
	t.Helper()
	var causes []string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT disable_causes FROM openrouter_api_keys WHERE organization_id=$1 AND key_type=$2`, orgID, keyType).Scan(&causes))
	return causes
}

func (f *runnerFixture) seedTrial(t *testing.T, orgID string, demotedAt, convertedAt *time.Time, endsAt time.Time) {
	t.Helper()
	_, err := f.pool.Exec(t.Context(), `INSERT INTO trials (organization_id, tier, ends_at, demoted_at, converted_at) VALUES ($1, 'enterprise', $2, $3, $4)`, orgID, endsAt, demotedAt, convertedAt)
	require.NoError(t, err)
}

func (f *runnerFixture) seedBilling(t *testing.T, orgID string, subscription *string) {
	t.Helper()
	_, err := f.pool.Exec(t.Context(), `INSERT INTO billing_metadata (organization_id, stripe_subscription_id) VALUES ($1, $2)`, orgID, subscription)
	require.NoError(t, err)
}

func (f *runnerFixture) seedAdminAudit(t *testing.T, orgID, keyType, action string, malformed bool) {
	t.Helper()
	metadata := fmt.Sprintf(`{"key_type":%q}`, keyType)
	after := fmt.Sprintf(`{"disabled":true,"disable_causes":[%q]}`, CauseAdminLock)
	if action == "openrouter-key:enable" {
		after = `{"disabled":false,"disable_causes":[]}`
	}
	if malformed {
		after = `{"disabled":true,"disable_causes":"not-an-array"}`
	}
	_, err := f.pool.Exec(t.Context(), `INSERT INTO audit_logs (organization_id, actor_id, actor_type, action, subject_id, subject_type, before_snapshot, after_snapshot, metadata) VALUES ($1, 'system:test', 'system', $2, $3, 'openrouter_api_key', '{"disabled":false,"disable_causes":[]}', $4, $5)`, orgID, action, "openrouter_api_key:"+orgID+"/"+keyType, after, metadata)
	require.NoError(t, err)
}

func newTestRunner(pool *pgxpool.Pool, batchSize int) *Runner {
	return NewRunner(pool, slog.New(slog.DiscardHandler), Options{BatchSize: batchSize, LockTimeout: time.Second, StatementTimeout: 5 * time.Second, MaxLockRetries: 2})
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

	summary, err := newTestRunner(f.pool, 100).Run(t.Context(), ModeApply)
	require.ErrorIs(t, err, ErrAmbiguousRows)
	require.Equal(t, []string{CauseTrialDemotion}, f.causes(t, converted, "internal"))
	require.Equal(t, []string{CauseBillingInactive}, f.causes(t, billing, "chat"))
	require.Equal(t, []string{CauseAdminLock, CauseTrialDemotion, CauseBillingInactive}, f.causes(t, multiple, "chat"))
	require.Equal(t, []string{CauseTrialDemotion}, f.causes(t, latestEnable, "internal"))
	require.Nil(t, f.causes(t, badBilling, "chat"))
	require.Nil(t, f.causes(t, badAudit, "internal"))
	require.Equal(t, int64(1), summary.Ambiguous[AmbiguousBillingProjection])
	require.Equal(t, int64(1), summary.Ambiguous[AmbiguousAdminAudit])
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

	var remaining int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM openrouter_api_keys WHERE disable_causes IS NULL AND deleted IS FALSE`).Scan(&remaining))
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
	_, err := f.pool.Exec(t.Context(), `UPDATE openrouter_api_keys SET disable_causes=ARRAY['admin_lock'], disabled=FALSE WHERE organization_id=$1`, mirror)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE openrouter_api_keys SET disable_causes=ARRAY['future_cause'] WHERE organization_id=$1`, unknown)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE openrouter_api_keys SET disable_causes=ARRAY['admin_lock','admin_lock'] WHERE organization_id=$1`, duplicate)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE openrouter_api_keys SET disable_causes=ARRAY['admin_lock'] WHERE organization_id=$1`, reclassified)
	require.NoError(t, err)

	summary, err := newTestRunner(f.pool, 2).Run(t.Context(), ModeValidate)
	require.ErrorIs(t, err, ErrValidationFailed)
	require.Equal(t, int64(2), summary.Validation[ValidationNull])
	require.Equal(t, int64(1), summary.Validation[ValidationMirrorMismatch])
	require.Equal(t, int64(1), summary.Validation[ValidationUnknownCause])
	require.Equal(t, int64(1), summary.Validation[ValidationDuplicateCause])
	require.GreaterOrEqual(t, summary.Validation[ValidationAmbiguous], int64(1))
	require.GreaterOrEqual(t, summary.Validation[ValidationReclassificationMismatch], int64(1))
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
	require.NoError(t, err)
	require.Empty(t, validation.Validation)

	_, err = runner.ApplyManualOverride(t.Context(), ManualOverride{OrganizationID: orgID, KeyType: "internal", Causes: []string{CauseTrialDemotion}})
	require.ErrorIs(t, err, ErrManualOverrideConflict)
}

func TestRunnerBoundsStatementTimeoutRetries(t *testing.T) {
	t.Parallel()
	f := newRunnerFixture(t)
	f.seedKey(t, "chat", false, "free")

	blocker, err := f.pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(t.Context()) }()
	_, err = blocker.Exec(t.Context(), `LOCK TABLE audit_logs IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)

	runner := NewRunner(f.pool, slog.New(slog.DiscardHandler), Options{BatchSize: 1, LockTimeout: 25 * time.Millisecond, StatementTimeout: 50 * time.Millisecond, MaxLockRetries: 1})
	started := time.Now()
	summary, err := runner.Run(t.Context(), ModeApply)
	require.Error(t, err)
	require.Equal(t, int64(1), summary.LockRetries)
	require.Less(t, time.Since(started), time.Second)
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

	var remaining int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM openrouter_api_keys WHERE disable_causes IS NULL`).Scan(&remaining))
	require.Equal(t, 3, remaining)

	summary, err := newTestRunner(f.pool, 2).Run(t.Context(), ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(3), summary.Updated)
}
