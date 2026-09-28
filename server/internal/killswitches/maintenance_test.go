package killswitches

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

// insertVersionRow fabricates one prescription header and version directly so
// eligibility timestamps can be controlled exactly. Offsets are applied to a
// single database clock reading, which is authoritative for expiry; a nil
// offset leaves the column NULL.
func insertVersionRow(t *testing.T, conn *pgxpool.Pool, orgID, state string, expiresIn, supersededIn *time.Duration) PrescriptionID {
	t.Helper()
	queries := repo.New(conn)
	clock, err := queries.GetKillswitchDatabaseTime(t.Context())
	require.NoError(t, err)
	at := func(offset *time.Duration) pgtype.Timestamptz {
		if offset == nil {
			return pgtype.Timestamptz{}
		}
		return conv.ToPGTimestamptz(clock.Time.Add(*offset))
	}
	id, err := queries.CreateKillswitchPrescriptionHeader(t.Context(), repo.CreateKillswitchPrescriptionHeaderParams{
		OrganizationID: orgID, DefinitionKey: "block-tools", PrincipalKind: "user", PrincipalKey: "user:fabricated", ResourceKind: "tool",
	})
	require.NoError(t, err)
	inserted, err := queries.CreateKillswitchPrescriptionVersion(t.Context(), repo.CreateKillswitchPrescriptionVersionParams{
		OrganizationID: orgID, PrescriptionID: id, Version: 1, State: state, ResourceScope: "all",
		StartsAt: at(new(-3 * time.Hour)), ExpiresAt: at(expiresIn), ActivatedAt: at(new(-3 * time.Hour)),
		InternalNote: sentinelInternalNote, ExternalNote: "Access paused.",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)
	if supersededIn != nil {
		superseded, err := queries.SupersedeKillswitchPrescriptionVersion(t.Context(), repo.SupersedeKillswitchPrescriptionVersionParams{
			SupersededAt: at(supersededIn), OrganizationID: orgID, PrescriptionID: id, Version: 1,
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), superseded)
	}
	return PrescriptionID(id.String())
}

func countExpiryMarkers(t *testing.T, conn *pgxpool.Pool, orgID string) int {
	t.Helper()
	var count int
	//nolint:glint // notestingrawsql: reads private maintenance state that no production query exposes
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_expiry_events WHERE organization_id = $1`, orgID).Scan(&count))
	return count
}

func markerExists(t *testing.T, conn *pgxpool.Pool, prescriptionID PrescriptionID) bool {
	t.Helper()
	var count int
	//nolint:glint // notestingrawsql: reads private maintenance state that no production query exposes
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_expiry_events WHERE prescription_id = $1`, string(prescriptionID)).Scan(&count))
	return count > 0
}

func TestExpirySweepRecordsOnlyGenuinelyExpiredVersions(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_matrix")
	maintenance := NewMaintenanceService(conn, audit.NewLogger())

	dueCurrent := insertVersionRow(t, conn, orgID, "active", new(-time.Hour), nil)
	expiredThenSuperseded := insertVersionRow(t, conn, orgID, "active", new(-2*time.Hour), new(-time.Hour))
	supersededBeforeExpiry := insertVersionRow(t, conn, orgID, "active", new(-time.Hour), new(-2*time.Hour))
	// Both offsets apply to one database clock reading, so superseded_at equals
	// expires_at exactly.
	expiryEqualsSupersession := insertVersionRow(t, conn, orgID, "active", new(-time.Hour), new(-time.Hour))
	notYetExpired := insertVersionRow(t, conn, orgID, "active", new(time.Hour), nil)
	inactive := insertVersionRow(t, conn, orgID, "inactive", new(-time.Hour), nil)
	unbounded := insertVersionRow(t, conn, orgID, "active", nil, nil)

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
		require.Equal(t, "System", row.ActorDisplayName)
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
	prescription := insertVersionRow(t, conn, orgID, "active", new(-time.Hour), nil)

	result, err := maintenance.RecordDueExpiries(t.Context(), 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Recorded)

	queries := repo.New(conn)
	prescriptionID := uuid.MustParse(string(prescription))
	identity, err := queries.GetKillswitchPrescriptionIdentity(t.Context(), repo.GetKillswitchPrescriptionIdentityParams{OrganizationID: orgID, PrescriptionID: prescriptionID})
	require.NoError(t, err)
	version, err := queries.GetKillswitchPrescriptionVersion(t.Context(), repo.GetKillswitchPrescriptionVersionParams{OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1})
	require.NoError(t, err)
	require.Equal(t, "active", version.State, "expiry recording never rewrites version state; enforcement expires at query time")
	require.False(t, version.SupersededAt.Valid)
	require.Equal(t, int64(1), identity.CurrentVersion)
}

func TestExpiryConcurrentSweepsRecordExactlyOnce(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_concurrency")
	prescription := insertVersionRow(t, conn, orgID, "active", new(-time.Hour), nil)

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

func TestExpirySweepSerializesWithLifecycleHeaderLock(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_lifecycle_lock")
	prescription := insertVersionRow(t, conn, orgID, "active", new(-time.Hour), nil)
	prescriptionID := uuid.MustParse(string(prescription))
	maintenance := NewMaintenanceService(conn, audit.NewLogger())

	//nolint:glint // notestingrawsql: transaction holds the prescription header lock the expiry sweep must wait on
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	txQueries := repo.New(tx)
	_, err = txQueries.LockKillswitchPrescriptionCurrent(t.Context(), repo.LockKillswitchPrescriptionCurrentParams{OrganizationID: orgID, PrescriptionID: prescriptionID})
	require.NoError(t, err)

	type expiryResult struct {
		recorded int64
		err      error
	}
	resultCh := make(chan expiryResult, 1)
	go func() {
		recorded, recordErr := maintenance.recordExpiry(context.Background(), repo.ListDueKillswitchExpiriesRow{OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1})
		resultCh <- expiryResult{recorded: recorded, err: recordErr}
	}()

	select {
	case result := <-resultCh:
		require.FailNow(t, "expiry recording bypassed the lifecycle header lock", "result: %+v", result)
	case <-time.After(150 * time.Millisecond):
	}

	// Supersede exactly at expiry so the version is no longer eligible.
	version, err := txQueries.GetKillswitchPrescriptionVersion(t.Context(), repo.GetKillswitchPrescriptionVersionParams{OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1})
	require.NoError(t, err)
	superseded, err := txQueries.SupersedeKillswitchPrescriptionVersion(t.Context(), repo.SupersedeKillswitchPrescriptionVersionParams{
		SupersededAt: version.ExpiresAt, OrganizationID: orgID, PrescriptionID: prescriptionID, Version: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), superseded)
	require.NoError(t, tx.Commit(t.Context()))

	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		require.Zero(t, result.recorded, "expiry eligibility is rechecked after the lifecycle lock releases")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "expiry recording did not resume after the lifecycle transaction committed")
	}
	require.Zero(t, countExpiryMarkers(t, conn, orgID))
	require.Empty(t, listAuditRows(t, conn, orgID))
	require.Empty(t, listOutboxMessages(t, conn, orgID))
}

func TestExpiryRetryRecovery(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_expiry_retry")
	insertVersionRow(t, conn, orgID, "active", new(-time.Hour), nil)

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
	//nolint:glint // notestingrawsql: fabricates private killswitch rows with SQL-computed timestamps that production lifecycle writes cannot produce
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
	//nolint:glint // notestingrawsql: reads private maintenance state that no production query exposes
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
	require.Zero(t, deleted, "receipts strictly after their expiry boundary are retained")

	require.Equal(t, 1, countOperations(t, conn, orgID))
	require.Zero(t, countOperations(t, conn, otherOrgID))
	var remaining uuid.UUID
	//nolint:glint // notestingrawsql: reads private maintenance state that no production query exposes
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
