package adminmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func newTrialWriter(f writeFixture) *trialWriter {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationExtendOrganizationTrial: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	return &trialWriter{runner: f.runner, audit: audit.NewLogger(), writes: writes}
}

// seedTrial stores an enterprise trial ending at endsAt, truncated to the
// microsecond precision Postgres keeps.
func seedTrial(t *testing.T, f writeFixture, organizationID string, endsAt time.Time, converted bool) time.Time {
	t.Helper()
	endsAt = endsAt.UTC().Truncate(time.Microsecond)
	convertedAt := pgtype.Timestamptz{}
	if converted {
		convertedAt = conv.ToPGTimestamptz(time.Now())
	}
	require.NoError(t, trialsrepo.New(f.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: organizationID, Tier: "enterprise", CreatedAt: conv.ToPGTimestamptz(time.Now().Add(-24 * time.Hour)), EndsAt: conv.ToPGTimestamptz(endsAt), ConvertedAt: convertedAt, DemotedAt: pgtype.Timestamptz{},
	}))
	return endsAt
}

func trialEndsAt(t *testing.T, f writeFixture, organizationID string) time.Time {
	t.Helper()
	trial, err := trialsrepo.New(f.db).GetTrial(t.Context(), organizationID)
	require.NoError(t, err)
	return trial.EndsAt.Time.UTC()
}

func TestTrialExtensionApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_write")
	writer := newTrialWriter(f)
	ctx := writeContext(t, f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	neighbourEndsAt := seedTrial(t, f, f.orgB, time.Now().Add(10*24*time.Hour), false)
	expected := trialTimestamp(endsAt)

	for name, bad := range map[string]TrialExtensionInput{
		"slug":         {OrganizationID: "synthetic-a", Days: 14, ExpectedEndsAt: expected},
		"zero":         {OrganizationID: f.orgA, Days: 0, ExpectedEndsAt: expected},
		"negative":     {OrganizationID: f.orgA, Days: -7, ExpectedEndsAt: expected},
		"too many":     {OrganizationID: f.orgA, Days: 366, ExpectedEndsAt: expected},
		"wraps to one": {OrganizationID: f.orgA, Days: 1<<32 + 1, ExpectedEndsAt: expected},
		"no end date":  {OrganizationID: f.orgA, Days: 14},
		"bad end date": {OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: "next week"},
	} {
		_, err := writer.apply(ctx, bad)
		require.Error(t, err, name)
	}
	require.Equal(t, endsAt, trialEndsAt(t, f, f.orgA))

	receipt, err := writer.apply(ctx, TrialExtensionInput{OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: expected})
	require.NoError(t, err)
	require.Equal(t, trialReceipt{OrganizationID: f.orgA, Name: "Synthetic A", Slug: receipt.Slug, Days: 14, PreviousEndsAt: expected, EndsAt: trialTimestamp(endsAt.AddDate(0, 0, 14)), SideEffects: trialSideEffects}, receipt)
	require.Equal(t, endsAt.AddDate(0, 0, 14), trialEndsAt(t, f, f.orgA))
	require.Equal(t, neighbourEndsAt, trialEndsAt(t, f, f.orgB), "a write cannot affect a second tenant")
	trial, err := trialsrepo.New(f.db).GetTrial(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, trial.ConvertedAt.Valid)
	require.False(t, trial.DemotedAt.Valid)

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface, "customer audit feeds and the outbox mask admin_mcp actors")
	require.NotNil(t, entry.ActingClientID)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationExtendOrganizationTrial, f.orgA, "executed"))
}

// Extension is relative, so expected_ends_at is all that stops a retry
// extending twice.
func TestTrialExtensionRepeatedCallExtendsOnce(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_repeat")
	writer := newTrialWriter(f)
	ctx := writeContext(t, f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	input := TrialExtensionInput{OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: trialTimestamp(endsAt)}

	_, err := writer.apply(ctx, input)
	require.NoError(t, err)
	_, err = writer.apply(ctx, input)
	require.ErrorIs(t, err, ErrStaleState)
	require.Equal(t, endsAt.AddDate(0, 0, 14), trialEndsAt(t, f, f.orgA))
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationExtendOrganizationTrial, f.orgA, "executed"))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationExtendOrganizationTrial, f.orgA, "failed"))
}

// get_organization_trial reports whole seconds; the stored end date does not.
func TestTrialExtensionAcceptsReadToolPrecision(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_precision")
	writer := newTrialWriter(f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour).Truncate(time.Second).Add(123456*time.Microsecond), false)
	_, err := writer.apply(writeContext(t, f), TrialExtensionInput{OrganizationID: f.orgA, Days: 1, ExpectedEndsAt: endsAt.Format(time.RFC3339)})
	require.NoError(t, err)
	require.Equal(t, endsAt.AddDate(0, 0, 1), trialEndsAt(t, f, f.orgA))
}

func TestTrialExtensionRefusesDriftedEndDate(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_stale")
	writer := newTrialWriter(f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	_, err := writer.apply(writeContext(t, f), TrialExtensionInput{OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: trialTimestamp(endsAt.Add(time.Hour))})
	require.ErrorIs(t, err, ErrStaleState)
	require.Equal(t, endsAt, trialEndsAt(t, f, f.orgA))
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))
}

func TestTrialExtensionRefusesDemotedOrConverted(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_ended")
	writer := newTrialWriter(f)
	ctx := writeContext(t, f)
	demotedEndsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	convertedEndsAt := seedTrial(t, f, f.orgB, time.Now().Add(10*24*time.Hour), false)
	rows, err := trialsrepo.New(f.db).DemoteTrialFixture(t.Context(), f.orgA)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	rows, err = trialsrepo.New(f.db).MarkTrialConverted(t.Context(), f.orgB)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	_, err = writer.apply(ctx, TrialExtensionInput{OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: trialTimestamp(demotedEndsAt)})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning, "a demoted trial is not extended")
	_, err = writer.apply(ctx, TrialExtensionInput{OrganizationID: f.orgB, Days: 14, ExpectedEndsAt: trialTimestamp(convertedEndsAt)})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning, "a converted trial is not extended")
	require.Equal(t, demotedEndsAt, trialEndsAt(t, f, f.orgA))
	require.Equal(t, convertedEndsAt, trialEndsAt(t, f, f.orgB))
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))
}

func TestTrialExtensionRefusesTrialsThatAreNotRunning(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_not_running")
	writer := newTrialWriter(f)
	ctx := writeContext(t, f)
	expected := trialTimestamp(time.Now())

	_, err := writer.apply(ctx, TrialExtensionInput{OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: expected})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning)
	seedTrial(t, f, f.orgA, time.Now().Add(-time.Hour), false)
	_, err = writer.apply(ctx, TrialExtensionInput{OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: expected})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning)
	seedTrial(t, f, f.orgB, time.Now().Add(10*24*time.Hour), true)
	_, err = writer.apply(ctx, TrialExtensionInput{OrganizationID: f.orgB, Days: 14, ExpectedEndsAt: expected})
	require.ErrorIs(t, err, admin.ErrTrialNotRunning)
	_, err = writer.apply(ctx, TrialExtensionInput{OrganizationID: "org_missing_" + uuid.NewString(), Days: 14, ExpectedEndsAt: expected})
	require.Error(t, err)
}

// Concurrent identical calls extend the trial once.
func TestTrialExtensionConcurrentCallsWriteOnce(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_concurrent")
	writer := newTrialWriter(f)
	ctx := writeContext(t, f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	input := TrialExtensionInput{OrganizationID: f.orgA, Days: 30, ExpectedEndsAt: trialTimestamp(endsAt)}

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := writer.apply(ctx, input)
			results <- err
		}()
	}
	succeeded := 0
	for range 2 {
		err := <-results
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, ErrStaleState)
	}
	require.Equal(t, 1, succeeded, "exactly one call writes")
	require.Equal(t, endsAt.AddDate(0, 0, 30), trialEndsAt(t, f, f.orgA))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationExtendOrganizationTrial, f.orgA, "executed"))
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnterpriseTrialExtended))
}

func TestTrialExtensionAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_trial_rollback")
	writer := newTrialWriter(f)
	endsAt := seedTrial(t, f, f.orgA, time.Now().Add(10*24*time.Hour), false)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationEnterpriseTrialExtended))
	_, err := writer.apply(writeContext(t, f), TrialExtensionInput{OrganizationID: f.orgA, Days: 14, ExpectedEndsAt: trialTimestamp(endsAt)})
	require.Error(t, err)
	require.Equal(t, endsAt, trialEndsAt(t, f, f.orgA), "the extension rolls back with the failed audit")
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationExtendOrganizationTrial, f.orgA, "failed"))
	require.Zero(t, countWriteEvents(t, f.db, OperationExtendOrganizationTrial, f.orgA, "executed"))
}
