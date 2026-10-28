package audit_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
)

func TestMarkKitRuntimeWorkloadAdmissionsInternal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	q := repo.New(conn)

	insert := func(actorType, actorID string, action audit.Action) uuid.UUID {
		t.Helper()
		row, err := q.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: uuid.NewString(),
			ActorID:        actorID,
			ActorType:      actorType,
			Action:         string(action),
			SubjectID:      uuid.NewString(),
			SubjectType:    "workload_admission",
		})
		require.NoError(t, err)
		return row.ID
	}

	for range 3 {
		insert("system", "kit-runtime", audit.ActionWorkloadAdmissionAdmit)
	}
	insert("system", "kit-runtime", audit.ActionWorkloadAdmissionWithdraw)
	// Not the runtime's own admission activity: these stay customer entries.
	insert("system", "kit-runtime", audit.ActionWorkloadIssuerCreate)
	insert("user", "kit-runtime", audit.ActionWorkloadAdmissionAdmit)
	insert("system", "workos-role-sync", audit.ActionWorkloadAdmissionAdmit)

	pending, err := q.CountCustomerKitRuntimeWorkloadAdmissions(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 4, pending)

	n, err := q.MarkKitRuntimeWorkloadAdmissionsInternal(ctx, 3)
	require.NoError(t, err)
	require.EqualValues(t, 3, n, "one statement changes at most batch_size rows")
	n, err = q.MarkKitRuntimeWorkloadAdmissionsInternal(ctx, 3)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = q.MarkKitRuntimeWorkloadAdmissionsInternal(ctx, 3)
	require.NoError(t, err)
	require.EqualValues(t, 0, n, "a finished backfill changes nothing on rerun")

	internal, err := audittest.AuditLogCountByVisibility(ctx, conn, audit.VisibilityInternal)
	require.NoError(t, err)
	require.EqualValues(t, 4, internal)
	customer, err := audittest.AuditLogCountByVisibility(ctx, conn, audit.VisibilityCustomer)
	require.NoError(t, err)
	require.EqualValues(t, 3, customer)
}
