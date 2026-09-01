//nolint:glint // Integration tests inspect audit and outbox rows directly.
package killswitches

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/platform_killswitches"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const sentinelInternalNote = "SENTINEL-INTERNAL-NOTE-d0f1"

type auditRow struct {
	Action           string
	ActorID          string
	ActorDisplayName string
	SubjectID        string
	SubjectType      string
	AfterSnapshot    []byte
	Metadata         []byte
	ActingSurface    string
}

func listAuditRows(t *testing.T, conn *pgxpool.Pool, orgID string) []auditRow {
	t.Helper()
	rows, err := conn.Query(t.Context(), `
		SELECT action, actor_id, coalesce(actor_display_name, ''), subject_id, subject_type, coalesce(after_snapshot, 'null'::jsonb), coalesce(metadata, 'null'::jsonb), coalesce(acting_surface, '')
		FROM audit_logs
		WHERE organization_id = $1
		ORDER BY seq
	`, orgID)
	require.NoError(t, err)
	defer rows.Close()
	var result []auditRow
	for rows.Next() {
		var row auditRow
		require.NoError(t, rows.Scan(&row.Action, &row.ActorID, &row.ActorDisplayName, &row.SubjectID, &row.SubjectType, &row.AfterSnapshot, &row.Metadata, &row.ActingSurface))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func listOutboxMessages(t *testing.T, conn *pgxpool.Pool, orgID string) [][]byte {
	t.Helper()
	rows, err := conn.Query(t.Context(), `SELECT message FROM publish_outbox WHERE organization_id = $1 ORDER BY id`, orgID)
	require.NoError(t, err)
	defer rows.Close()
	var messages [][]byte
	for rows.Next() {
		var message []byte
		require.NoError(t, rows.Scan(&message))
		messages = append(messages, message)
	}
	require.NoError(t, rows.Err())
	return messages
}

func requireNoSentinelLeak(t *testing.T, conn *pgxpool.Pool, orgID string) {
	t.Helper()
	var leaked int
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count(*) FROM audit_logs WHERE organization_id = $1 AND audit_logs::text LIKE '%' || $2 || '%'
	`, orgID, sentinelInternalNote).Scan(&leaked))
	require.Zero(t, leaked, "internal note must not appear in any audit column")
	for _, message := range listOutboxMessages(t, conn, orgID) {
		require.False(t, bytes.Contains(message, []byte(sentinelInternalNote)), "internal note must not appear in outbox payloads")
	}
}

func TestLifecycleAuditAtomicityAndReplay(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_lifecycle_audit")
	service := newLifecycleServiceForTest(t, conn, nil, nil, NewAuditBeforeCommitHook(audit.NewLogger()))

	activate := testActivateRequest(orgID, uuid.New())
	activate.Desired.InternalNote = sentinelInternalNote
	activated, err := service.ActivatePrescription(t.Context(), activate)
	require.NoError(t, err)

	change := testChangeRequest(orgID, activated.PrescriptionID, 1, uuid.New(), []string{"tool:a", "tool:b"})
	change.Desired.InternalNote = sentinelInternalNote
	changed, err := service.ChangePrescription(t.Context(), change)
	require.NoError(t, err)

	deactivate := DeactivatePrescriptionRequest{MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: activated.PrescriptionID, ExpectedVersion: changed.Version}
	_, err = service.DeactivatePrescription(t.Context(), deactivate)
	require.NoError(t, err)

	reactivate := ReactivatePrescriptionRequest{MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: activated.PrescriptionID, ExpectedVersion: 3, Desired: testDesired([]string{"tool:a"})}
	reactivate.Desired.InternalNote = sentinelInternalNote
	reactivated, err := service.ReactivatePrescription(t.Context(), reactivate)
	require.NoError(t, err)
	require.Equal(t, int64(4), reactivated.Version)

	auditRows := listAuditRows(t, conn, orgID)
	require.Len(t, auditRows, 4, "each successful transition writes exactly one audit row")
	actions := make([]string, len(auditRows))
	for i, row := range auditRows {
		actions[i] = row.Action
		require.Equal(t, string(activated.PrescriptionID), row.SubjectID)
		require.Equal(t, "killswitch_prescription", row.SubjectType)
		require.Equal(t, "user:test", row.ActorID)
		require.Equal(t, "Test User", row.ActorDisplayName)
	}
	require.Equal(t, []string{"killswitch:activate", "killswitch:change", "killswitch:deactivate", "killswitch:activate"}, actions, "reactivation is recorded as an activation")

	var snapshot audit.KillswitchVersionSnapshot
	require.NoError(t, json.Unmarshal(auditRows[3].AfterSnapshot, &snapshot))
	require.Equal(t, audit.KillswitchVersionSnapshot{Version: 4, State: "active"}, snapshot)
	var metadata struct {
		Operation   string    `json:"operation"`
		OperationID uuid.UUID `json:"operation_id"`
	}
	require.NoError(t, json.Unmarshal(auditRows[3].Metadata, &metadata))
	require.Equal(t, "reactivate", metadata.Operation)
	require.Equal(t, reactivate.OperationID, metadata.OperationID)

	messages := listOutboxMessages(t, conn, orgID)
	require.Len(t, messages, 4, "each successful transition enqueues exactly one outbox row")
	for _, message := range messages {
		require.True(t, bytes.Contains(message, []byte("audit_log.killswitch_event_v1")), "outbox rows carry the cataloged killswitch event type")
		require.Contains(t, string(message), `"actor_display_name":"Test User"`)
	}
	requireNoSentinelLeak(t, conn, orgID)

	replayed, err := service.ChangePrescription(t.Context(), change)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)

	conflicting := testChangeRequest(orgID, activated.PrescriptionID, 1, change.OperationID, []string{"tool:c"})
	_, err = service.ChangePrescription(t.Context(), conflicting)
	require.ErrorIs(t, err, ErrOperationConflict)

	require.Len(t, listAuditRows(t, conn, orgID), 4, "replay and conflicting reuse must not duplicate audit history")
	require.Len(t, listOutboxMessages(t, conn, orgID), 4, "replay and conflicting reuse must not duplicate outbox rows")
}

func TestPlatformBreakGlassDelegationWritesOneAtomicAuditWithClosedSurface(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_platform_break_glass_audit")
	lifecycle := newLifecycleServiceForTest(t, conn, nil, nil, NewAuditBeforeCommitHook(audit.NewLogger()))
	facade, err := NewFacade(lifecycle)
	require.NoError(t, err)
	service := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{results: []bool{true}}, generic: facade}

	result, err := service.ActivatePrescription(
		validatedCustomerContext(t, "", "user:platform-admin", "platform-admin@example.com"),
		&gen.ActivatePrescriptionPayload{
			OrganizationID: orgID, OperationID: uuid.NewString(), Definition: new("block-tools"),
			PrincipalKind: new("user"), PrincipalInput: new("User:Alpha"), ResourceKind: new("tool"),
			ResourceScope: "selected", SelectedResourceInputs: []string{"tool:a"}, StartMode: "now",
			InternalNote: sentinelInternalNote, ExternalNote: "Access paused.",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, result)

	rows := listAuditRows(t, conn, orgID)
	require.Len(t, rows, 1, "the adapter must not write a second audit row")
	require.Equal(t, "user:platform-admin", rows[0].ActorID)
	require.Equal(t, "platform-admin@example.com", rows[0].ActorDisplayName)
	require.Equal(t, string(audit.SurfacePlatformBreakGlass), rows[0].ActingSurface)
	require.Len(t, listOutboxMessages(t, conn, orgID), 1)
	requireNoSentinelLeak(t, conn, orgID)
}

func TestManagementAuditAndBreakGlassRemainAvailableDuringEvaluatorIncident(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_management_evaluator_incident")
	lifecycle := newLifecycleServiceForTest(t, conn, nil, nil, NewAuditBeforeCommitHook(audit.NewLogger()))
	facade, err := NewFacade(lifecycle)
	require.NoError(t, err)
	customer, err := NewAuthorizedService(facade, &authorizerStub{})
	require.NoError(t, err)
	customerCtx := validatedCustomerContext(t, orgID, "user:customer-admin", "customer-admin@example.com")

	activated, err := customer.ActivatePrescription(customerCtx, AuthorizedActivatePrescriptionRequest{
		OperationID: uuid.New(), Definition: "block-tools", PrincipalKind: "user", PrincipalInput: "User:Alpha", ResourceKind: "tool",
		Desired: DesiredVersionInput{ResourceScope: ResourceScopeSelected, SelectedResourceInputs: []string{"tool:a"}, StartMode: StartModeNow, InternalNote: sentinelInternalNote, ExternalNote: "Access paused."},
	})
	require.NoError(t, err)

	evaluator, err := newEvaluator(evaluationQueryFunc(func(context.Context, repo.EvaluateCurrentPrescriptionsParams) (repo.EvaluateCurrentPrescriptionsRow, error) {
		return repo.EvaluateCurrentPrescriptionsRow{}, errors.New("evaluator database unavailable")
	}), lifecycle.registry, time.Second, nil)
	require.NoError(t, err)
	evaluation := evaluator.Evaluate(t.Context(), evaluationRequest("block-tools"))
	require.Equal(t, EvaluationResultInfrastructureFailure, evaluation.Kind())

	current, err := customer.GetPrescription(customerCtx, AuthorizedGetPrescriptionRequest{PrescriptionID: activated.PrescriptionID})
	require.NoError(t, err)
	require.Equal(t, PrescriptionStateActive, current.State)

	platform := &PlatformService{logger: testenv.NewLogger(t), sessions: &platformAdminReaderStub{results: []bool{true}}, generic: facade}
	deactivated, err := platform.DeactivatePrescription(
		validatedCustomerContext(t, "", "user:platform-admin", "platform-admin@example.com"),
		&gen.DeactivatePrescriptionPayload{OrganizationID: orgID, OperationID: uuid.NewString(), PrescriptionID: string(activated.PrescriptionID), ExpectedVersion: activated.Version},
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), deactivated.Version)

	rows := listAuditRows(t, conn, orgID)
	require.Len(t, rows, 2)
	require.Equal(t, "user:customer-admin", rows[0].ActorID)
	require.Equal(t, string(audit.SurfaceDashboard), rows[0].ActingSurface)
	require.Equal(t, "user:platform-admin", rows[1].ActorID)
	require.Equal(t, "platform-admin@example.com", rows[1].ActorDisplayName)
	require.Equal(t, string(audit.SurfacePlatformBreakGlass), rows[1].ActingSurface)
	require.Len(t, listOutboxMessages(t, conn, orgID), 2)
}

func TestLifecycleAuditRollback(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_lifecycle_audit_rollback")
	auditHook := NewAuditBeforeCommitHook(audit.NewLogger())
	failingService := newLifecycleServiceForTest(t, conn, nil, nil, func(ctx context.Context, queries LifecycleTransactionQueries, event MutationEvent) error {
		if err := auditHook(ctx, queries, event); err != nil {
			return err
		}
		return errors.New("forced post-audit failure")
	})

	activate := testActivateRequest(orgID, uuid.New())
	_, err := failingService.ActivatePrescription(t.Context(), activate)
	require.ErrorContains(t, err, "forced post-audit failure")

	for _, table := range []string{"killswitch_prescriptions", "killswitch_prescription_versions", "killswitch_operations", "audit_logs", "publish_outbox"} {
		var count int
		require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM `+table+` WHERE organization_id = $1`, orgID).Scan(&count))
		require.Zero(t, count, "a failed lifecycle transaction must roll back %s together with the mutation", table)
	}

	service := newLifecycleServiceForTest(t, conn, nil, nil, auditHook)
	activated, err := service.ActivatePrescription(t.Context(), activate)
	require.NoError(t, err, "a rolled-back attempt must not consume its operation ID")
	require.False(t, activated.Replayed)
	require.Len(t, listAuditRows(t, conn, orgID), 1)
	require.Len(t, listOutboxMessages(t, conn, orgID), 1)
}

func TestLifecycleAuditActionMapping(t *testing.T) {
	t.Parallel()

	for operation, want := range map[MutationOperation]audit.Action{
		MutationOperationActivate:   audit.ActionKillswitchActivate,
		MutationOperationReactivate: audit.ActionKillswitchActivate,
		MutationOperationChange:     audit.ActionKillswitchChange,
		MutationOperationDeactivate: audit.ActionKillswitchDeactivate,
	} {
		got, err := lifecycleAuditAction(operation)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := lifecycleAuditAction(MutationOperation("unknown"))
	require.Error(t, err)
}

// expiredAtOf reads the recorded expiry deadline back out of an audit metadata payload.
func expiredAtOf(t *testing.T, metadata []byte) time.Time {
	t.Helper()
	var decoded struct {
		Version   int64     `json:"version"`
		ExpiredAt time.Time `json:"expired_at"`
	}
	require.NoError(t, json.Unmarshal(metadata, &decoded))
	return decoded.ExpiredAt
}
