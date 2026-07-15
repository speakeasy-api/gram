package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func upsertTumAlertMetadata(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, limit int64, alertEmail string) {
	t.Helper()

	_, err := usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID:         orgID,
		TumMonthlyTokenLimit:   pgtype.Int8{Int64: limit, Valid: limit > 0},
		AlertEmail:             pgtype.Text{String: alertEmail, Valid: alertEmail != ""},
		BillingCycleAnchorDay:  1,
		TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)
}

// waitForActiveCycleTokens runs the activity until the active cycle's
// snapshot reports the expected token total, absorbing ClickHouse's
// asynchronous materialization of freshly inserted telemetry.
func waitForActiveCycleTokens(t *testing.T, ctx context.Context, act interface {
	Do(ctx context.Context, orgIDs []string) error
}, conn *pgxpool.Pool, orgID string, want int64) {
	t.Helper()

	queries := usagerepo.New(conn)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		if !assert.NoError(c, act.Do(ctx, []string{orgID})) {
			return
		}
		rows, err := queries.ListBillingCycleUsage(ctx, orgID)
		if !assert.NoError(c, err) || !assert.NotEmpty(c, rows) {
			return
		}
		assert.Equal(c, want, rows[len(rows)-1].TumTokens)
	}, 15*time.Second, 500*time.Millisecond)
}

func TestSnapshotBillingCycleUsage_SendsHighestCrossedThresholdAlert(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID, captured := setupSnapshotBillingCycleUsageTestWithEmail(t, "snapshot_billing_alert_threshold")

	upsertTumAlertMetadata(t, ctx, conn, orgID, 1000, "billing@example.com")

	// 950/1000 crosses 50, 75, and 90 at once; only the highest fires.
	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 950)
	waitForActiveCycleTokens(t, ctx, act, conn, orgID, 950)

	sent := captured.Sent()
	require.Len(t, sent, 1, "one alert for the highest crossed threshold")
	require.Equal(t, "billing@example.com", sent[0].Email)
	require.Equal(t, "90", sent[0].DataVariables["threshold_percent"])
	require.Equal(t, "950", sent[0].DataVariables["usage_tokens"])
	require.Equal(t, "1,000", sent[0].DataVariables["token_limit"])
	require.Equal(t, "Test Org", sent[0].DataVariables["organization_name"])
	require.NotContains(t, sent[0].DataVariables, "overage_tokens",
		"below 100%% the approach template is used, not the overage one")

	// Re-running the activity must not re-alert the same threshold.
	require.NoError(t, act.Do(ctx, []string{orgID}))
	require.NoError(t, act.Do(ctx, []string{orgID}))
	require.Len(t, captured.Sent(), 1, "threshold alerts fire once per cycle")
}

func TestSnapshotBillingCycleUsage_EscalatesToOverageAlert(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID, captured := setupSnapshotBillingCycleUsageTestWithEmail(t, "snapshot_billing_alert_overage")

	upsertTumAlertMetadata(t, ctx, conn, orgID, 1000, "billing@example.com")

	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 600)
	waitForActiveCycleTokens(t, ctx, act, conn, orgID, 600)
	require.Len(t, captured.Sent(), 1)
	require.Equal(t, "50", captured.Sent()[0].DataVariables["threshold_percent"])

	// Usage jumping to 150% of the limit sends the overage email for the new
	// highest threshold.
	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 900)
	waitForActiveCycleTokens(t, ctx, act, conn, orgID, 1500)

	sent := captured.Sent()
	require.Len(t, sent, 2, "escalation past 100%% sends the overage alert")
	require.Equal(t, "150", sent[1].DataVariables["threshold_percent"])
	require.Equal(t, "1,500", sent[1].DataVariables["usage_tokens"])
	require.Equal(t, "500", sent[1].DataVariables["overage_tokens"])
}

func TestSnapshotBillingCycleUsage_ReAlertsAfterLimitChange(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID, captured := setupSnapshotBillingCycleUsageTestWithEmail(t, "snapshot_billing_alert_limit_change")

	upsertTumAlertMetadata(t, ctx, conn, orgID, 1000, "billing@example.com")

	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 600)
	waitForActiveCycleTokens(t, ctx, act, conn, orgID, 600)
	require.Len(t, captured.Sent(), 1, "60%% of the original limit alerts at 50")

	// Raising the limit re-arms the ladder: 600/1100 still sits past 50%, so
	// the alert for the new limit fires on the next run.
	upsertTumAlertMetadata(t, ctx, conn, orgID, 1100, "billing@example.com")
	require.NoError(t, act.Do(ctx, []string{orgID}))

	sent := captured.Sent()
	require.Len(t, sent, 2, "changed limit must re-alert already-crossed thresholds")
	require.Equal(t, "50", sent[1].DataVariables["threshold_percent"])
	require.Equal(t, "1,100", sent[1].DataVariables["token_limit"])

	require.NoError(t, act.Do(ctx, []string{orgID}))
	require.Len(t, captured.Sent(), 2, "the re-armed threshold still dedups afterwards")
}

func TestSnapshotBillingCycleUsage_SkipsAlertWithoutAlertEmail(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID, captured := setupSnapshotBillingCycleUsageTestWithEmail(t, "snapshot_billing_alert_no_email")

	upsertTumAlertMetadata(t, ctx, conn, orgID, 1000, "")

	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 950)
	waitForActiveCycleTokens(t, ctx, act, conn, orgID, 950)

	require.Empty(t, captured.Sent(), "no alert contact configured means no email")
}

func TestSnapshotBillingCycleUsage_SkipsAlertWithoutLimit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID, captured := setupSnapshotBillingCycleUsageTestWithEmail(t, "snapshot_billing_alert_no_limit")

	upsertTumAlertMetadata(t, ctx, conn, orgID, 0, "billing@example.com")

	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 950)
	waitForActiveCycleTokens(t, ctx, act, conn, orgID, 950)

	require.Empty(t, captured.Sent(), "no contracted limit means no thresholds to cross")
}

func TestSnapshotBillingCycleUsage_RetriesAlertAfterSendFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID, captured := setupSnapshotBillingCycleUsageTestWithEmail(t, "snapshot_billing_alert_retry")

	// Let the usage materialize with no alert config so no send is attempted
	// while ClickHouse catches up.
	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 600)
	waitForActiveCycleTokens(t, ctx, act, conn, orgID, 600)
	require.Empty(t, captured.Sent())

	upsertTumAlertMetadata(t, ctx, conn, orgID, 1000, "billing@example.com")

	// The first send attempt fails; the reservation must be released so the
	// next run retries instead of losing the alert.
	captured.FailNext(1)
	require.NoError(t, act.Do(ctx, []string{orgID}))
	require.Empty(t, captured.Sent(), "failed send records nothing")

	require.NoError(t, act.Do(ctx, []string{orgID}))
	sent := captured.Sent()
	require.Len(t, sent, 1, "the alert is retried on the next run")
	require.Equal(t, "50", sent[0].DataVariables["threshold_percent"])
}
