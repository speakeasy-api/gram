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

	orgID := uuid.NewString()
	insert := func(actorType, actorID string, action audit.Action, subjectID string) {
		t.Helper()
		_, err := q.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: orgID,
			ActorID:        actorID,
			ActorType:      actorType,
			Action:         string(action),
			SubjectID:      subjectID,
			SubjectType:    "workload_admission",
		})
		require.NoError(t, err)
	}

	// Three runtime-created admissions: two admitted and withdrawn by the
	// runtime, one withdrawn by a person who withdrew the issuer.
	runtimeSubjects := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, subject := range runtimeSubjects {
		insert("system", audit.KitRuntimeActorID, audit.ActionWorkloadAdmissionAdmit, subject)
	}
	insert("system", audit.KitRuntimeActorID, audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[0])
	insert("system", audit.KitRuntimeActorID, audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[1])
	insert("user", "user-1", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[2])

	// A person's own admission stays a customer entry, as does the same subject
	// id in another organization.
	human := uuid.NewString()
	insert("user", "user-1", audit.ActionWorkloadAdmissionAdmit, human)
	insert("user", "user-1", audit.ActionWorkloadAdmissionWithdraw, human)
	_, err = q.InsertAuditLog(ctx, repo.InsertAuditLogParams{
		OrganizationID: uuid.NewString(),
		ActorID:        "user-1",
		ActorType:      "user",
		Action:         string(audit.ActionWorkloadAdmissionWithdraw),
		SubjectID:      runtimeSubjects[0],
		SubjectType:    "workload_admission",
	})
	require.NoError(t, err)

	pending, err := q.CountCustomerKitRuntimeWorkloadAdmissions(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 6, pending)

	n, err := q.MarkKitRuntimeWorkloadAdmissionsInternal(ctx, 4)
	require.NoError(t, err)
	require.EqualValues(t, 4, n, "one statement changes at most batch_size rows")
	n, err = q.MarkKitRuntimeWorkloadAdmissionsInternal(ctx, 4)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
	n, err = q.MarkKitRuntimeWorkloadAdmissionsInternal(ctx, 4)
	require.NoError(t, err)
	require.EqualValues(t, 0, n, "a finished backfill changes nothing on rerun")

	admitted, err := audit.KitRuntimeAdmittedSubjects(ctx, conn, orgID, []uuid.UUID{uuid.MustParse(runtimeSubjects[2]), uuid.MustParse(human)})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{uuid.MustParse(runtimeSubjects[2]): true}, admitted)

	internal, err := audittest.AuditLogCountByVisibility(ctx, conn, audit.VisibilityInternal)
	require.NoError(t, err)
	require.EqualValues(t, 6, internal)
	customer, err := audittest.AuditLogCountByVisibility(ctx, conn, audit.VisibilityCustomer)
	require.NoError(t, err)
	require.EqualValues(t, 3, customer)
}
