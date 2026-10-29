package securityevent_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/audit/securityevent"
	"github.com/speakeasy-api/gram/server/internal/audit/securityevent/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()
	orgID := uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        "test-org-" + orgID[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	return orgID
}

func latestPayload(t *testing.T, conn *pgxpool.Pool, orgID string) events.AuditLogCreatedPayloadV1 {
	t.Helper()
	envelope, err := audittestrepo.New(conn).GetLatestOutboxPayloadByOrg(t.Context(), audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: orgID,
		EventType:      string(events.WorkloadAdmissionV1.EventType()),
	})
	require.NoError(t, err)
	var event webhooksv1.Event
	require.NoError(t, proto.Unmarshal(envelope, &event))
	var payload events.AuditLogCreatedPayloadV1
	require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
	return payload
}

// A security event lands in security_audit_logs only and publishes the same
// webhook event as a configuration change, with its category.
func TestLogWorkloadAdmissionAdmit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := newOrganization(t, conn)
	agentID := uuid.NewString()
	admission := uuid.New()

	err = securityevent.LogWorkloadAdmissionAdmit(ctx, conn, audit.LogWorkloadAdmissionAdmitEvent{
		OrganizationID:         orgID,
		ProjectID:              uuid.NullUUID{},
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeAgent, agentID),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		AdmissionURN:           urn.NewWorkloadAdmission(admission),
		AdmissionDisplayName:   "system:serviceaccount:ns:kit-test",
		AdmissionSnapshotAfter: &audit.WorkloadAdmissionSnapshot{Subject: "system:serviceaccount:ns:kit-test", AssignedAgentID: agentID},
	})
	require.NoError(t, err)

	record, err := audittest.LatestSecurityAuditLogByAction(ctx, conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Equal(t, admission.String(), record.SubjectID)
	require.Equal(t, "workload_admission", record.SubjectType)
	require.Equal(t, agentID, record.ActorID)
	require.Equal(t, "agent", record.ActorType)
	require.NotNil(t, record.ActingSurface, "the acting surface is stamped as on the configuration log")
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, agentID, after["assigned_agent_id"])

	configuration, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Zero(t, configuration, "nothing is written to the configuration log")

	payload := latestPayload(t, conn, orgID)
	require.Equal(t, string(audit.CategorySecurity), payload.Category)
	require.Equal(t, admission.String(), payload.SubjectID)
}

// Configuration changes keep publishing their category, so a subscriber can
// tell the two logs apart on the same event type.
func TestConfigurationWebhookCarriesCategory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := newOrganization(t, conn)

	err = audit.NewLogger().LogWorkloadAdmissionAdmit(ctx, conn, audit.LogWorkloadAdmissionAdmitEvent{
		OrganizationID:         orgID,
		ProjectID:              uuid.NullUUID{},
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"),
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		AdmissionURN:           urn.NewWorkloadAdmission(uuid.New()),
		AdmissionDisplayName:   "repo:acme/app:ref:refs/heads/main",
		AdmissionSnapshotAfter: nil,
	})
	require.NoError(t, err)

	require.Equal(t, string(audit.CategoryConfiguration), latestPayload(t, conn, orgID).Category)
	security, err := audittest.SecurityAuditLogCountByAction(ctx, conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.Zero(t, security)
}

func TestMoveKitRuntimeAuditLogs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	configuration := auditrepo.New(conn)
	q := repo.New(conn)

	orgID := uuid.NewString()
	insertConfiguration := func(organizationID, actorType, actorID string, action audit.Action, subjectID string) {
		t.Helper()
		_, err := configuration.InsertAuditLog(ctx, auditrepo.InsertAuditLogParams{
			OrganizationID: organizationID,
			ActorID:        actorID,
			ActorType:      actorType,
			Action:         string(action),
			SubjectID:      subjectID,
			SubjectType:    "workload_admission",
		})
		require.NoError(t, err)
	}

	// Three runtime-created admissions recorded before the security log
	// existed: two admitted and withdrawn by the runtime, one withdrawn by a
	// person who withdrew the issuer.
	runtimeSubjects := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for _, subject := range runtimeSubjects {
		insertConfiguration(orgID, "system", "kit-runtime", audit.ActionWorkloadAdmissionAdmit, subject)
	}
	insertConfiguration(orgID, "system", "kit-runtime", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[0])
	insertConfiguration(orgID, "system", "kit-runtime", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[1])
	insertConfiguration(orgID, "user", "user-1", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[2])

	// An admission whose admit is already in the security log, with a
	// person-attributed withdrawal left in the configuration log: the
	// withdrawal moves.
	agentSubject := uuid.NewString()
	_, err = q.InsertSecurityAuditLog(ctx, repo.InsertSecurityAuditLogParams{
		OrganizationID: orgID,
		ActorID:        uuid.NewString(),
		ActorType:      "agent",
		Action:         string(audit.ActionWorkloadAdmissionAdmit),
		SubjectID:      agentSubject,
		SubjectType:    "workload_admission",
	})
	require.NoError(t, err)
	insertConfiguration(orgID, "user", "user-1", audit.ActionWorkloadAdmissionWithdraw, agentSubject)

	// A person's own admission stays, as does the same subject id in another
	// organization.
	human := uuid.NewString()
	insertConfiguration(orgID, "user", "user-1", audit.ActionWorkloadAdmissionAdmit, human)
	insertConfiguration(orgID, "user", "user-1", audit.ActionWorkloadAdmissionWithdraw, human)
	insertConfiguration(uuid.NewString(), "user", "user-1", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[0])

	admitted, err := securityevent.RuntimeAdmittedSubjects(ctx, conn, orgID, []uuid.UUID{uuid.MustParse(runtimeSubjects[2]), uuid.MustParse(agentSubject), uuid.MustParse(human)})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{uuid.MustParse(runtimeSubjects[2]): true, uuid.MustParse(agentSubject): true}, admitted,
		"runtime admissions are recognized in either log before the backfill runs")

	pending, err := q.CountKitRuntimeAuditLogs(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 7, pending)

	// A batch of three moves the three admits first, leaving withdrawals
	// whose admit is now only in the security log.
	n, err := q.MoveKitRuntimeAuditLogs(ctx, 3)
	require.NoError(t, err)
	require.EqualValues(t, 3, n, "one statement moves at most batch_size rows")
	n, err = q.MoveKitRuntimeAuditLogs(ctx, 3)
	require.NoError(t, err)
	require.EqualValues(t, 3, n)
	n, err = q.MoveKitRuntimeAuditLogs(ctx, 3)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	n, err = q.MoveKitRuntimeAuditLogs(ctx, 3)
	require.NoError(t, err)
	require.EqualValues(t, 0, n, "a finished backfill moves nothing on rerun")

	admitted, err = securityevent.RuntimeAdmittedSubjects(ctx, conn, orgID, []uuid.UUID{uuid.MustParse(runtimeSubjects[2]), uuid.MustParse(human)})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{uuid.MustParse(runtimeSubjects[2]): true}, admitted)

	moved, err := audittest.SecurityAuditLogCountByAction(ctx, conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.EqualValues(t, 4, moved)
	left, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.EqualValues(t, 2, left, "the person's own withdrawal and the other organization's stay")
	leftAdmits, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionWorkloadAdmissionAdmit)
	require.NoError(t, err)
	require.EqualValues(t, 1, leftAdmits)
}

// The security log reads back through the configuration log's types, so the
// audit log API serves both with one reader.
func TestListReadsSecurityLog(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := newOrganization(t, conn)

	for range 2 {
		err = securityevent.LogWorkloadAdmissionWithdraw(ctx, conn, audit.LogWorkloadAdmissionWithdrawEvent{
			OrganizationID:          orgID,
			ProjectID:               uuid.NullUUID{},
			Actor:                   urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()),
			ActorDisplayName:        nil,
			ActorSlug:               nil,
			AdmissionURN:            urn.NewWorkloadAdmission(uuid.New()),
			AdmissionDisplayName:    "subject",
			AdmissionSnapshotBefore: nil,
		})
		require.NoError(t, err)
	}

	rows, err := securityevent.List(ctx, conn, auditrepo.ListAuditLogsParams{OrganizationID: orgID})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Greater(t, rows[0].Seq, rows[1].Seq, "newest first")

	actions, err := securityevent.ActionFacets(ctx, conn, auditrepo.ListAuditActionFacetsParams{OrganizationID: orgID})
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, string(audit.ActionWorkloadAdmissionWithdraw), actions[0].Value)
	require.EqualValues(t, 2, actions[0].Count)
}
