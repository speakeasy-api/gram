package audit_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
)

func TestMarkKitRuntimeWorkloadAdmissionsSecurity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	q := repo.New(conn)

	orgID := uuid.NewString()
	insertCategory := func(actorType, actorID string, action audit.Action, subjectID string, category audit.Category) {
		t.Helper()
		_, err := q.InsertAuditLog(ctx, repo.InsertAuditLogParams{
			OrganizationID: orgID,
			ActorID:        actorID,
			ActorType:      actorType,
			Action:         string(action),
			SubjectID:      subjectID,
			SubjectType:    "workload_admission",
			Category:       string(category),
		})
		require.NoError(t, err)
	}
	insert := func(actorType, actorID string, action audit.Action, subjectID string) {
		t.Helper()
		insertCategory(actorType, actorID, action, subjectID, "")
	}

	// Three runtime-created admissions: two admitted and withdrawn by the
	// runtime, one withdrawn by a person who withdrew the issuer.
	runtimeSubjects := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, subject := range runtimeSubjects {
		insert("system", audit.LegacyKitRuntimeActorID, audit.ActionWorkloadAdmissionAdmit, subject)
	}
	insert("system", audit.LegacyKitRuntimeActorID, audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[0])
	insert("system", audit.LegacyKitRuntimeActorID, audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[1])
	insert("user", "user-1", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[2])

	// An admission recorded as a security entry by the agent: its admit entry
	// is already in the right category, and the person-attributed withdrawal
	// moves.
	agentSubject := uuid.NewString()
	insertCategory("agent", uuid.NewString(), audit.ActionWorkloadAdmissionAdmit, agentSubject, audit.CategorySecurity)
	insert("user", "user-1", audit.ActionWorkloadAdmissionWithdraw, agentSubject)

	// A person's own admission stays a configuration entry, as does the same subject
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

	pending, err := q.CountConfigurationKitRuntimeWorkloadAdmissions(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 7, pending)

	n, err := q.MarkKitRuntimeWorkloadAdmissionsSecurity(ctx, 4)
	require.NoError(t, err)
	require.EqualValues(t, 4, n, "one statement changes at most batch_size rows")
	n, err = q.MarkKitRuntimeWorkloadAdmissionsSecurity(ctx, 4)
	require.NoError(t, err)
	require.EqualValues(t, 3, n)
	n, err = q.MarkKitRuntimeWorkloadAdmissionsSecurity(ctx, 4)
	require.NoError(t, err)
	require.EqualValues(t, 0, n, "a finished backfill changes nothing on rerun")

	admitted, err := audit.SecurityAdmittedSubjects(ctx, conn, orgID, []uuid.UUID{uuid.MustParse(runtimeSubjects[2]), uuid.MustParse(agentSubject), uuid.MustParse(human)})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{uuid.MustParse(runtimeSubjects[2]): true, uuid.MustParse(agentSubject): true}, admitted)

	security, err := audittest.AuditLogCountByCategory(ctx, conn, audit.CategorySecurity)
	require.NoError(t, err)
	require.EqualValues(t, 8, security)
	configuration, err := audittest.AuditLogCountByCategory(ctx, conn, audit.CategoryConfiguration)
	require.NoError(t, err)
	require.EqualValues(t, 3, configuration)
}
