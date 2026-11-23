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
func TestLogWorkloadAdmissionWithdraw(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := newOrganization(t, conn)
	agentID := uuid.NewString()
	admission := uuid.New()

	err = securityevent.LogWorkloadAdmissionWithdraw(ctx, conn, audit.LogWorkloadAdmissionWithdrawEvent{
		OrganizationID:          orgID,
		ProjectID:               uuid.NullUUID{},
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"),
		ActorDisplayName:        nil,
		ActorSlug:               nil,
		AdmissionURN:            urn.NewWorkloadAdmission(admission),
		AdmissionDisplayName:    "system:serviceaccount:ns:kit-test",
		AdmissionSnapshotBefore: &audit.WorkloadAdmissionSnapshot{Subject: "system:serviceaccount:ns:kit-test", AssignedAgentID: agentID},
	})
	require.NoError(t, err)

	record, err := audittest.LatestSecurityAuditLogByAction(ctx, conn, audit.ActionWorkloadAdmissionWithdraw)
	require.NoError(t, err)
	require.Equal(t, admission.String(), record.SubjectID)
	require.Equal(t, "workload_admission", record.SubjectType)
	require.Equal(t, "user-1", record.ActorID)
	require.Equal(t, "user", record.ActorType)
	require.NotNil(t, record.ActingSurface, "the acting surface is stamped as on the configuration log")
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, agentID, before["assigned_agent_id"])

	configuration, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionWorkloadAdmissionWithdraw)
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
	insertConfiguration := func(organizationID, actorType, actorID string, action audit.Action, subjectID string, internal bool) uuid.UUID {
		t.Helper()
		row, err := configuration.InsertAuditLog(ctx, auditrepo.InsertAuditLogParams{
			OrganizationID: organizationID,
			IsInternal:     internal,
			ActorID:        actorID,
			ActorType:      actorType,
			Action:         string(action),
			SubjectID:      subjectID,
			SubjectType:    "workload_admission",
		})
		require.NoError(t, err)
		return row.ID
	}

	// An entry the security log already holds, written after deploy.
	nativeSubject := uuid.New()
	native, err := q.InsertSecurityAuditLog(ctx, repo.InsertSecurityAuditLogParams{
		IsInternal:     false,
		OrganizationID: orgID,
		ActorID:        uuid.NewString(),
		ActorType:      "agent",
		Action:         string(audit.ActionWorkloadAdmissionAdmit),
		SubjectID:      nativeSubject.String(),
		SubjectType:    "workload_admission",
	})
	require.NoError(t, err)

	// Three runtime-created admissions recorded before the security log
	// existed: two admitted and withdrawn by the runtime, one withdrawn by a
	// person.
	runtimeSubjects := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	var moving []uuid.UUID
	for _, subject := range runtimeSubjects {
		moving = append(moving, insertConfiguration(orgID, "system", "kit-runtime", audit.ActionWorkloadAdmissionAdmit, subject, false))
	}
	moving = append(moving, insertConfiguration(orgID, "system", "kit-runtime", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[0], false))
	moving = append(moving, insertConfiguration(orgID, "system", "kit-runtime", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[1], false))

	// A person's withdrawal of a runtime admission stays: older entries cannot
	// tell an issuer cascade from a direct withdrawal. So does a person's own
	// admission, and the same subject in another organization.
	insertConfiguration(orgID, "user", "user-1", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[2], false)
	human := uuid.NewString()
	insertConfiguration(orgID, "user", "user-1", audit.ActionWorkloadAdmissionAdmit, human, false)
	insertConfiguration(orgID, "user", "user-1", audit.ActionWorkloadAdmissionWithdraw, human, false)
	insertConfiguration(uuid.NewString(), "user", "user-1", audit.ActionWorkloadAdmissionWithdraw, runtimeSubjects[0], false)

	// Another organization's deliberately classified fixture verifies that
	// moving rows preserves an existing flag; admission itself is not internal.
	otherOrgID := uuid.NewString()
	otherRuntime := insertConfiguration(otherOrgID, "system", "kit-runtime", audit.ActionWorkloadAdmissionAdmit, uuid.NewString(), true)

	before, err := configuration.ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{IncludeInternalEvents: true, OrganizationID: orgID})
	require.NoError(t, err)
	seqByID := make(map[uuid.UUID]int64, len(before))
	for _, row := range before {
		seqByID[row.ID] = row.Seq
	}

	admitted, err := securityevent.RuntimeAdmittedSubjects(ctx, conn, orgID, []uuid.UUID{uuid.MustParse(runtimeSubjects[2]), nativeSubject, uuid.MustParse(human)})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{uuid.MustParse(runtimeSubjects[2]): true, nativeSubject: true}, admitted,
		"runtime admissions are recognized in either log before the backfill moves them")

	pending, err := q.CountKitRuntimeAuditLogs(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 6, pending)

	organizations, err := q.ListKitRuntimeAuditOrganizations(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{orgID, otherOrgID}, organizations,
		"only organizations holding runtime entries are listed")

	move := func(organizationID string) int64 {
		t.Helper()
		n, err := q.MoveKitRuntimeAuditLogs(ctx, repo.MoveKitRuntimeAuditLogsParams{OrganizationID: organizationID, BatchSize: 3})
		require.NoError(t, err)
		return n
	}
	require.EqualValues(t, 3, move(orgID), "one statement moves at most batch_size rows")
	require.EqualValues(t, 2, move(orgID))
	require.EqualValues(t, 0, move(orgID), "a finished organization moves nothing on rerun")

	otherLeft, err := configuration.ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{IncludeInternalEvents: true, OrganizationID: otherOrgID})
	require.NoError(t, err)
	require.Len(t, otherLeft, 1, "a batch moves only its own organization's entries")
	require.Equal(t, otherRuntime, otherLeft[0].ID)
	require.EqualValues(t, 1, move(otherOrgID))
	classified, err := audittestrepo.New(conn).GetSecurityAuditLogInternal(ctx, otherRuntime)
	require.NoError(t, err)
	require.True(t, classified)

	admitted, err = securityevent.RuntimeAdmittedSubjects(ctx, conn, orgID, []uuid.UUID{uuid.MustParse(runtimeSubjects[2]), nativeSubject, uuid.MustParse(human)})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{uuid.MustParse(runtimeSubjects[2]): true, nativeSubject: true}, admitted, "and after")

	// Moved entries keep their seq, below every entry written to the security
	// log directly, so the feed lists them after it, newest first.
	security, err := securityevent.List(ctx, conn, auditrepo.ListAuditLogsParams{IncludeInternalEvents: true, OrganizationID: orgID})
	require.NoError(t, err)
	require.Len(t, security, len(moving)+1)
	require.Equal(t, native.ID, security[0].ID)
	for i, id := range moving {
		row := security[len(security)-1-i]
		require.Equal(t, id, row.ID)
		require.Equal(t, seqByID[id], row.Seq)
		require.False(t, row.IsInternal)
		require.Less(t, row.Seq, security[0].Seq)
	}

	left, err := configuration.ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{IncludeInternalEvents: true, OrganizationID: orgID})
	require.NoError(t, err)
	require.Len(t, left, 3, "the person-made entries stay in the configuration log")
	for _, row := range left {
		require.Equal(t, "user", row.ActorType)
		require.False(t, row.IsInternal)
	}
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

func TestPublishWebhook_InternalSecurityEvent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	input := repo.InsertSecurityAuditLogParams{OrganizationID: uuid.NewString(), ActorID: "audit-test", ActorType: "system", Action: string(audit.ActionWorkloadAdmissionAdmit), SubjectID: uuid.NewString(), SubjectType: "workload_admission", IsInternal: true}
	row, err := repo.New(conn).InsertSecurityAuditLog(ctx, input)
	require.NoError(t, err)
	internal, err := audittestrepo.New(conn).GetSecurityAuditLogInternal(ctx, row.ID)
	require.NoError(t, err)
	require.True(t, internal)
	require.NoError(t, audit.PublishWebhook(ctx, conn, auditrepo.InsertAuditLogParams(input), auditrepo.InsertAuditLogRow(row), audit.CategorySecurity, events.WorkloadAdmissionV1))
	count, err := audittestrepo.New(conn).CountPublishOutboxByOrg(ctx, input.OrganizationID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestListSecurityAuditLogs_InternalClassification(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Test Organization", Slug: "test-" + orgID[:8]})
	require.NoError(t, err)
	q := repo.New(conn)
	for _, subject := range []string{"catalog_promotion", "mcp_server"} {
		for _, internal := range []bool{false, true} {
			actor, action, surface := "visible", "visible:create", "dashboard"
			if internal {
				actor, action, surface = "hidden", "hidden:create", "platform_mcp"
			}
			_, err = q.InsertSecurityAuditLog(ctx, repo.InsertSecurityAuditLogParams{OrganizationID: orgID, ActorID: actor, ActorType: "system", Action: action, SubjectID: subject, SubjectType: subject, IsInternal: internal, ActingSurface: pgtype.Text{String: surface, Valid: true}})
			require.NoError(t, err)
		}
	}
	base := repo.ListSecurityAuditLogsParams{OrganizationID: orgID}
	for _, filter := range []string{"none", "action", "subject_type", "subject_id", "subject_ids"} {
		p := base
		switch filter {
		case "action":
			p.Action = pgtype.Text{String: "hidden:create", Valid: true}
		case "subject_type":
			p.SubjectType = pgtype.Text{String: "mcp_server", Valid: true}
		case "subject_id":
			p.SubjectID = pgtype.Text{String: "mcp_server", Valid: true}
		case "subject_ids":
			p.SubjectIds = []string{"mcp_server"}
		}
		rows, err := q.ListSecurityAuditLogs(ctx, p)
		require.NoError(t, err)
		for _, row := range rows {
			require.False(t, row.IsInternal, filter)
		}
		if filter == "none" {
			require.Len(t, rows, 2)
		}
		if filter == "action" {
			require.Empty(t, rows)
		}
	}
	actors, err := q.ListSecurityActorFacets(ctx, repo.ListSecurityActorFacetsParams{OrganizationID: orgID})
	require.NoError(t, err)
	require.Len(t, actors, 1)
	actions, err := q.ListSecurityActionFacets(ctx, repo.ListSecurityActionFacetsParams{OrganizationID: orgID})
	require.NoError(t, err)
	require.Len(t, actions, 1)
	surfaces, err := q.ListSecuritySurfaceFacets(ctx, repo.ListSecuritySurfaceFacetsParams{OrganizationID: orgID})
	require.NoError(t, err)
	require.Len(t, surfaces, 1)
	base.IncludeInternalEvents = true
	rows, err := q.ListSecurityAuditLogs(ctx, base)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	for i, row := range rows {
		p := base
		p.CursorSeq = pgtype.Int8{Int64: row.Seq, Valid: true}
		next, err := q.ListSecurityAuditLogs(ctx, p)
		require.NoError(t, err)
		require.Len(t, next, len(rows)-i-1)
		for j := range next {
			require.Equal(t, rows[i+1+j], next[j])
		}
	}
}
