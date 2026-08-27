//nolint:glint // Integration tests fabricate and inspect private rows directly.
package killswitches

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
)

// insertVersionRow fabricates one prescription header and version directly so
// eligibility timestamps can be controlled exactly. Interval expressions are
// evaluated by the database clock, which is authoritative for expiry.
func insertVersionRow(t *testing.T, conn *pgxpool.Pool, orgID, state, expiresAtSQL, supersededAtSQL string) PrescriptionID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `
		INSERT INTO killswitch_prescriptions (organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version)
		VALUES ($1, 'block-tools', 'user', 'user:fabricated', 'tool', 1)
		RETURNING id
	`, orgID).Scan(&id))
	_, err := conn.Exec(t.Context(), fmt.Sprintf(`
		INSERT INTO killswitch_prescription_versions (organization_id, prescription_id, version, state, resource_scope, starts_at, expires_at, activated_at, superseded_at, internal_note, external_note)
		VALUES ($1, $2, 1, $3, 'all', clock_timestamp() - interval '3 hours', %s, clock_timestamp() - interval '3 hours', %s, $4, 'Access paused.')
	`, expiresAtSQL, supersededAtSQL), orgID, id, state, sentinelInternalNote)
	require.NoError(t, err)
	return PrescriptionID(id.String())
}

func countExpiryMarkers(t *testing.T, conn *pgxpool.Pool, orgID string) int {
	t.Helper()
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_expiry_events WHERE organization_id = $1`, orgID).Scan(&count))
	return count
}

func markerExists(t *testing.T, conn *pgxpool.Pool, prescriptionID PrescriptionID) bool {
	t.Helper()
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_expiry_events WHERE prescription_id = $1`, string(prescriptionID)).Scan(&count))
	return count > 0
}

func TestExpirySweepRecordsOnlyGenuinelyExpiredVersions(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_matrix")
	maintenance := NewMaintenanceService(conn, audit.NewLogger())

	dueCurrent := insertVersionRow(t, conn, orgID, "active", "clock_timestamp() - interval '1 hour'", "NULL")
	expiredThenSuperseded := insertVersionRow(t, conn, orgID, "active", "clock_timestamp() - interval '2 hours'", "clock_timestamp() - interval '1 hour'")
	supersededBeforeExpiry := insertVersionRow(t, conn, orgID, "active", "clock_timestamp() - interval '1 hour'", "clock_timestamp() - interval '2 hours'")
	expiryEqualsSupersession := insertVersionRow(t, conn, orgID, "active", "clock_timestamp() - interval '1 hour'", "clock_timestamp() - interval '1 hour'")
	notYetExpired := insertVersionRow(t, conn, orgID, "active", "clock_timestamp() + interval '1 hour'", "NULL")
	inactive := insertVersionRow(t, conn, orgID, "inactive", "clock_timestamp() - interval '1 hour'", "NULL")
	unbounded := insertVersionRow(t, conn, orgID, "active", "NULL", "NULL")
	// Force the equality boundary to exact identity: interval arithmetic above
	// evaluates clock_timestamp() per call.
	_, err := conn.Exec(t.Context(), `UPDATE killswitch_prescription_versions SET superseded_at = expires_at WHERE prescription_id = $1`, string(expiryEqualsSupersession))
	require.NoError(t, err)

	result, err := maintenance.RecordDueExpiries(t.Context(), 100)
	require.NoError(t, err)
	require.Equal(t, ExpiryBatchResult{Candidates: 2, Recorded: 2}, result, "only the due current version and the version that expired strictly before supersession are eligible")

	require.True(t, markerExists(t, conn, dueCurrent))
	require.True(t, markerExists(t, conn, expiredThenSuperseded), "a delayed sweep still records a version that expired before being superseded")
	for name, prescription := range map[string]PrescriptionID{
		"superseded before expiry":   supersededBeforeExpiry,
		"expiry equals supersession": expiryEqualsSupersession,
		"not yet expired":            notYetExpired,
		"inactive":                   inactive,
		"unbounded":                  unbounded,
	} {
		require.False(t, markerExists(t, conn, prescription), "%s must not be recorded", name)
	}

	auditRows := listAuditRows(t, conn, orgID)
	require.Len(t, auditRows, 2)
	for _, row := range auditRows {
		require.Equal(t, "killswitch:expire", row.Action)
		require.Equal(t, "system", row.ActorID)
		require.Equal(t, "killswitch_prescription", row.SubjectType)
		require.False(t, expiredAtOf(t, row.Metadata).IsZero())
	}
	require.Len(t, listOutboxMessages(t, conn, orgID), 2)
	requireNoSentinelLeak(t, conn, orgID)

	result, err = maintenance.RecordDueExpiries(t.Context(), 100)
	require.NoError(t, err)
	require.Equal(t, ExpiryBatchResult{Candidates: 0, Recorded: 0}, result, "a repeated sweep is a no-op")
	require.Equal(t, 2, countExpiryMarkers(t, conn, orgID))
	require.Len(t, listAuditRows(t, conn, orgID), 2)
	require.Len(t, listOutboxMessages(t, conn, orgID), 2)

	_, err = maintenance.RecordDueExpiries(t.Context(), 0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = maintenance.RecordDueExpiries(t.Context(), maxCleanupBatchSize+1)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestExpirySweepDoesNotAlterEffectiveState(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_state")
	maintenance := NewMaintenanceService(conn, audit.NewLogger())
	prescription := insertVersionRow(t, conn, orgID, "active", "clock_timestamp() - interval '1 hour'", "NULL")

	result, err := maintenance.RecordDueExpiries(t.Context(), 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Recorded)

	var state string
	var supersededAt *time.Time
	var currentVersion int64
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT version.state, version.superseded_at, prescription.current_version
		FROM killswitch_prescription_versions AS version
		JOIN killswitch_prescriptions AS prescription ON prescription.id = version.prescription_id
		WHERE version.prescription_id = $1
	`, string(prescription)).Scan(&state, &supersededAt, &currentVersion))
	require.Equal(t, "active", state, "expiry recording never rewrites version state; enforcement expires at query time")
	require.Nil(t, supersededAt)
	require.Equal(t, int64(1), currentVersion)
}

func TestExpiryConcurrentSweepsRecordExactlyOnce(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_concurrency")
	prescription := insertVersionRow(t, conn, orgID, "active", "clock_timestamp() - interval '1 hour'", "NULL")

	const sweeps = 4
	totals := make([]ExpiryBatchResult, sweeps)
	sweepErrs := make([]error, sweeps)
	var wg sync.WaitGroup
	for i := range sweeps {
		wg.Go(func() {
			maintenance := NewMaintenanceService(conn, audit.NewLogger())
			totals[i], sweepErrs[i] = maintenance.RecordDueExpiries(context.Background(), 100)
		})
	}
	wg.Wait()

	var total int64
	for i, result := range totals {
		require.NoError(t, sweepErrs[i])
		total += result.Recorded
	}
	require.Equal(t, int64(1), total, "concurrent sweeps must record one expiry exactly once")
	require.True(t, markerExists(t, conn, prescription))
	require.Equal(t, 1, countExpiryMarkers(t, conn, orgID))
	require.Len(t, listAuditRows(t, conn, orgID), 1)
	require.Len(t, listOutboxMessages(t, conn, orgID), 1)
}

func TestExpiryRetryRecovery(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_retry")
	insertVersionRow(t, conn, orgID, "active", "clock_timestamp() - interval '1 hour'", "NULL")

	maintenance := NewMaintenanceService(conn, audit.NewLogger())
	maintenance.beforeExpiryCommit = func(context.Context) error { return errors.New("forced pre-commit failure") }

	_, err := maintenance.RecordDueExpiries(t.Context(), 100)
	require.ErrorContains(t, err, "forced pre-commit failure")
	require.Zero(t, countExpiryMarkers(t, conn, orgID), "a failed attempt rolls back its marker")
	require.Empty(t, listAuditRows(t, conn, orgID), "a failed attempt rolls back its audit row")
	require.Empty(t, listOutboxMessages(t, conn, orgID), "a failed attempt rolls back its outbox row")

	maintenance.beforeExpiryCommit = nil
	result, err := maintenance.RecordDueExpiries(t.Context(), 100)
	require.NoError(t, err)
	require.Equal(t, ExpiryBatchResult{Candidates: 1, Recorded: 1}, result, "a retry after rollback records the expiry once")

	result, err = maintenance.RecordDueExpiries(t.Context(), 100)
	require.NoError(t, err)
	require.Equal(t, ExpiryBatchResult{Candidates: 0, Recorded: 0}, result, "a retry after commit observes the marker and is a no-op")
	require.Equal(t, 1, countExpiryMarkers(t, conn, orgID))
	require.Len(t, listAuditRows(t, conn, orgID), 1)
	require.Len(t, listOutboxMessages(t, conn, orgID), 1)
}

func insertOperationReceipt(t *testing.T, conn *pgxpool.Pool, orgID, expiresAtSQL string) uuid.UUID {
	t.Helper()
	operationID := uuid.New()
	_, err := conn.Exec(t.Context(), fmt.Sprintf(`
		INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, expires_at)
		VALUES ($1, $2, 'user:test', 'activate', 'hash', %s)
	`, expiresAtSQL), orgID, operationID)
	require.NoError(t, err)
	return operationID
}

func countOperations(t *testing.T, conn *pgxpool.Pool, orgID string) int {
	t.Helper()
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_operations WHERE organization_id = $1`, orgID).Scan(&count))
	return count
}

func TestOperationCleanupBoundaryAndBatching(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_cleanup_boundary")
	otherOrgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrgID)
	maintenance := NewMaintenanceService(conn, audit.NewLogger())

	insertOperationReceipt(t, conn, orgID, "clock_timestamp() - interval '1 hour'")
	insertOperationReceipt(t, conn, orgID, "clock_timestamp()")
	retained := insertOperationReceipt(t, conn, orgID, "clock_timestamp() + interval '1 hour'")
	insertOperationReceipt(t, conn, otherOrgID, "clock_timestamp() - interval '1 minute'")

	deleted, err := maintenance.CleanupExpiredOperationsGlobal(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted, "a full batch deletes exactly the batch size")
	deleted, err = maintenance.CleanupExpiredOperationsGlobal(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "the privileged sweep continues across organizations")
	deleted, err = maintenance.CleanupExpiredOperationsGlobal(t.Context(), 2)
	require.NoError(t, err)
	require.Zero(t, deleted, "receipts strictly before their boundary are retained")

	require.Equal(t, 1, countOperations(t, conn, orgID))
	require.Zero(t, countOperations(t, conn, otherOrgID))
	var remaining uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT operation_id FROM killswitch_operations WHERE organization_id = $1`, orgID).Scan(&remaining))
	require.Equal(t, retained, remaining)

	_, err = maintenance.CleanupExpiredOperationsGlobal(t.Context(), 0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = maintenance.CleanupExpiredOperationsGlobal(t.Context(), maxCleanupBatchSize+1)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestOperationCleanupConcurrentCleanersDoNotDoubleCount(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_cleanup_concurrency")
	const expired = 24
	for range expired {
		insertOperationReceipt(t, conn, orgID, "clock_timestamp() - interval '1 hour'")
	}

	const cleaners = 3
	totals := make([]int64, cleaners)
	cleanupErrs := make([]error, cleaners)
	var wg sync.WaitGroup
	for i := range cleaners {
		wg.Go(func() {
			maintenance := NewMaintenanceService(conn, audit.NewLogger())
			for {
				deleted, err := maintenance.CleanupExpiredOperationsGlobal(context.Background(), 5)
				if err != nil {
					cleanupErrs[i] = err
					return
				}
				if deleted == 0 {
					return
				}
				totals[i] += deleted
			}
		})
	}
	wg.Wait()

	var total int64
	for i, deleted := range totals {
		require.NoError(t, cleanupErrs[i])
		total += deleted
	}
	require.Equal(t, int64(expired), total, "concurrent cleaners must not double-count deletions")
	require.Zero(t, countOperations(t, conn, orgID))
}
