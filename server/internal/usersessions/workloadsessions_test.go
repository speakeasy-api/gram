package usersessions_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// A GitHub Actions subject: colons inside the external subject are what make a
// fixed-segment parse of the session subject wrong.
const workloadTestSubject = "repo:acme/payments-api:ref:refs/heads/main"

// seedWorkloadIssuer registers a workload issuer directly. There is no create
// query yet: writes belong to the workload identity management API.
func seedWorkloadIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.NullUUID, name string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuer := "https://token.actions.example.com/" + uuid.NewString()
	var id uuid.UUID
	err := conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `
		INSERT INTO workload_issuers (organization_id, project_id, name, issuer, jwks_uri)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, authCtx.ActiveOrganizationID, projectID, name, issuer, issuer+"/.well-known/jwks.json").Scan(&id)
	require.NoError(t, err)

	return id
}

// seedWorkloadAgent creates an agent in the caller's organization, with the
// owning membership agents require.
func seedWorkloadAgent(t *testing.T, ctx context.Context, conn *pgxpool.Pool, name string) agentsrepo.Agent {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	userID := "user-" + uuid.NewString()[:8]
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: userID,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	agent, err := agentsrepo.New(conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    userID,
		Name:           name,
	})
	require.NoError(t, err)

	return agent
}

// seedWorkloadAssignment assigns an agent to a workload principal directly.
// There is no create query yet: writes belong to the workload identity
// management API.
func seedWorkloadAssignment(t *testing.T, ctx context.Context, conn *pgxpool.Pool, issuerID uuid.UUID, subject string, agentID uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := conn.Exec( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `
		INSERT INTO workload_agent_assignments (organization_id, workload_issuer_id, subject, agent_id)
		VALUES ($1, $2, $3, $4)
	`, authCtx.ActiveOrganizationID, issuerID, subject, agentID)
	require.NoError(t, err)
}

// seedWorkloadAdmission admits a workload directly, since writes belong to the
// workload identity management API. A null projectID is the organization tier.
func seedWorkloadAdmission(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.NullUUID, issuerID uuid.UUID, subject string, name string) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	var id uuid.UUID
	err := conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `
		INSERT INTO workload_identity_admissions (organization_id, project_id, workload_issuer_id, subject, name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, authCtx.ActiveOrganizationID, projectID, issuerID, subject, name).Scan(&id)
	require.NoError(t, err)

	return id
}

func listAllSessions(t *testing.T, ctx context.Context, ti *testInstance, status *string) []*types.UserSession {
	t.Helper()

	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              status,
		ClientID:            nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	return got.Items
}

func sessionByID(t *testing.T, items []*types.UserSession, id uuid.UUID) *types.UserSession {
	t.Helper()

	for _, item := range items {
		if item.ID == id.String() {
			return item
		}
	}
	require.FailNow(t, fmt.Sprintf("session %s not listed", id))
	return nil
}

func TestListUserSessions_LabelsWorkloadSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedIssuer(t, ctx, ti, "workload-labels")
	workloadIssuerID := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, "GitHub Actions")
	agent := seedWorkloadAgent(t, ctx, ti.conn, "Deploy bot")
	seedWorkloadAssignment(t, ctx, ti.conn, workloadIssuerID, workloadTestSubject, agent.ID)

	workloadSession, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(workloadIssuerID, workloadTestSubject))
	require.NoError(t, err)
	userSession, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewUserSubject("workload-labels-user"))
	require.NoError(t, err)

	items := listAllSessions(t, ctx, ti, nil)

	got := sessionByID(t, items, workloadSession.ID)
	require.Equal(t, "workload", got.SubjectType)
	require.NotNil(t, got.SubjectDisplayName)
	require.Equal(t, workloadTestSubject, *got.SubjectDisplayName)
	require.NotNil(t, got.Workload)
	require.Equal(t, workloadIssuerID.String(), got.Workload.WorkloadIssuerID)
	require.Equal(t, workloadTestSubject, got.Workload.ExternalSubject)
	require.NotNil(t, got.Workload.WorkloadIssuerName)
	require.Equal(t, "GitHub Actions", *got.Workload.WorkloadIssuerName)
	require.NotNil(t, got.Workload.WorkloadIssuerURL)
	require.Contains(t, *got.Workload.WorkloadIssuerURL, "https://token.actions.example.com/")
	require.NotNil(t, got.Workload.AgentID)
	require.Equal(t, agent.ID.String(), *got.Workload.AgentID)
	require.NotNil(t, got.Workload.AgentName)
	require.Equal(t, "Deploy bot", *got.Workload.AgentName)
	require.NotNil(t, got.Workload.AgentStatus)
	require.Equal(t, "active", *got.Workload.AgentStatus)

	require.Nil(t, sessionByID(t, items, userSession.ID).Workload, "a human session carries no workload label")
}

func TestListUserSessions_WorkloadAgentIsKeyedOnIssuerAndSubject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := seedIssuer(t, ctx, ti, "workload-keying")
	assigned := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "Assigned issuer")
	unassigned := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "Unassigned issuer")
	agent := seedWorkloadAgent(t, ctx, ti.conn, "Keyed bot")
	seedWorkloadAssignment(t, ctx, ti.conn, assigned, workloadTestSubject, agent.ID)

	withAgent, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(assigned, workloadTestSubject))
	require.NoError(t, err)
	sameSubjectOtherIssuer, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(unassigned, workloadTestSubject))
	require.NoError(t, err)

	items := listAllSessions(t, ctx, ti, nil)

	require.NotNil(t, sessionByID(t, items, withAgent.ID).Workload.AgentID)

	other := sessionByID(t, items, sameSubjectOtherIssuer.ID).Workload
	require.NotNil(t, other)
	require.NotNil(t, other.WorkloadIssuerName, "an organization-tier issuer is named in every project")
	require.Equal(t, "Unassigned issuer", *other.WorkloadIssuerName)
	require.Nil(t, other.AgentID, "the same subject under another issuer is a different workload")
	require.Nil(t, other.AgentStatus)
}

func TestListUserSessions_WorkloadIssuerOfSiblingProjectIsUnnamed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := seedIssuer(t, ctx, ti, "workload-sibling")
	siblingProjectID := createSiblingProject(t, ctx, ti.conn, "workload-sibling-project")
	siblingIssuer := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: siblingProjectID, Valid: true}, "Sibling issuer")

	session, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(siblingIssuer, workloadTestSubject))
	require.NoError(t, err)

	got := sessionByID(t, listAllSessions(t, ctx, ti, nil), session.ID).Workload
	require.NotNil(t, got, "the identity parsed from the subject is always reported")
	require.Equal(t, siblingIssuer.String(), got.WorkloadIssuerID)
	require.Equal(t, workloadTestSubject, got.ExternalSubject)
	require.Nil(t, got.WorkloadIssuerName)
	require.Nil(t, got.WorkloadIssuerURL)
}

func TestListUserSessions_ReportsSuspendedWorkloadAgent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := seedIssuer(t, ctx, ti, "workload-suspended")
	workloadIssuerID := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "Suspended agent issuer")
	agent := seedWorkloadAgent(t, ctx, ti.conn, "Suspended bot")
	seedWorkloadAssignment(t, ctx, ti.conn, workloadIssuerID, workloadTestSubject, agent.ID)
	_, err := agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ID:             agent.ID,
	})
	require.NoError(t, err)

	session, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(workloadIssuerID, workloadTestSubject))
	require.NoError(t, err)

	got := sessionByID(t, listAllSessions(t, ctx, ti, nil), session.ID).Workload
	require.NotNil(t, got.AgentStatus)
	require.Equal(t, "suspended", *got.AgentStatus)
}

func TestListUserSessions_ListsEveryAdmissionLettingTheWorkloadIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project := uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
	organization := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	issuerID := seedIssuer(t, ctx, ti, "workload-admissions")
	workloadIssuerID := seedWorkloadIssuer(t, ctx, ti.conn, organization, "Admitting issuer")
	siblingProjectID := createSiblingProject(t, ctx, ti.conn, "workload-admissions-sibling")

	withdrawn := seedWorkloadAdmission(t, ctx, ti.conn, project, workloadIssuerID, workloadTestSubject, "Withdrawn admission")
	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `UPDATE workload_identity_admissions SET deleted_at = clock_timestamp() WHERE id = $1`, withdrawn)
	require.NoError(t, err)

	projectAdmission := seedWorkloadAdmission(t, ctx, ti.conn, project, workloadIssuerID, workloadTestSubject, "Project admission")
	orgAdmission := seedWorkloadAdmission(t, ctx, ti.conn, organization, workloadIssuerID, workloadTestSubject, "Organization admission")
	seedWorkloadAdmission(t, ctx, ti.conn, uuid.NullUUID{UUID: siblingProjectID, Valid: true}, workloadIssuerID, workloadTestSubject, "Sibling admission")
	seedWorkloadAdmission(t, ctx, ti.conn, project, workloadIssuerID, "some-other-subject", "Other subject")

	session, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(workloadIssuerID, workloadTestSubject))
	require.NoError(t, err)

	got := sessionByID(t, listAllSessions(t, ctx, ti, nil), session.ID).Workload
	require.NotNil(t, got)
	require.Len(t, got.Admissions, 2, "only live admissions for this workload at this project or the organization")
	require.Equal(t, projectAdmission.String(), got.Admissions[0].ID)
	require.Equal(t, "project", got.Admissions[0].Tier)
	require.Equal(t, "Project admission", *got.Admissions[0].Name)
	require.Equal(t, orgAdmission.String(), got.Admissions[1].ID)
	require.Equal(t, "organization", got.Admissions[1].Tier)
}

func TestListUserSessions_AdmissionsUnderDeletedIssuerAreNotListed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := seedIssuer(t, ctx, ti, "workload-deleted-issuer")
	workloadIssuerID := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "Deleted issuer")
	seedWorkloadAdmission(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, workloadIssuerID, workloadTestSubject, "Orphaned admission")
	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `UPDATE workload_issuers SET deleted_at = clock_timestamp() WHERE id = $1`, workloadIssuerID)
	require.NoError(t, err)

	session, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(workloadIssuerID, workloadTestSubject))
	require.NoError(t, err)

	got := sessionByID(t, listAllSessions(t, ctx, ti, nil), session.ID).Workload
	require.NotNil(t, got)
	require.Empty(t, got.Admissions, "a deleted issuer admits nothing")
}

// Deleting an issuer withdraws the authority of every workload it vouched for,
// so the row must not keep advertising an agent the workload can no longer act
// through.
func TestListUserSessions_DeletedIssuerDropsTheAssignedAgent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := seedIssuer(t, ctx, ti, "workload-deleted-issuer-agent")
	workloadIssuerID := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "Doomed issuer")
	agent := seedWorkloadAgent(t, ctx, ti.conn, "Orphaned bot")
	seedWorkloadAssignment(t, ctx, ti.conn, workloadIssuerID, workloadTestSubject, agent.ID)

	session, err := seedUserSession(t, ctx, ti.conn, issuerID, urn.NewWorkloadSubject(workloadIssuerID, workloadTestSubject))
	require.NoError(t, err)

	live := sessionByID(t, listAllSessions(t, ctx, ti, nil), session.ID).Workload
	require.NotNil(t, live.AgentID, "the assignment resolves while the issuer is live")

	_, err = ti.conn.Exec( //nolint:glint // notestingrawsql: no delete query exists yet; writes belong to the management API milestone
		ctx, `UPDATE workload_issuers SET deleted_at = clock_timestamp() WHERE id = $1`, workloadIssuerID)
	require.NoError(t, err)

	got := sessionByID(t, listAllSessions(t, ctx, ti, nil), session.ID).Workload
	require.NotNil(t, got)
	require.Nil(t, got.AgentID, "a deleted issuer withdraws the workload's authority")
	require.Nil(t, got.AgentName)
	require.Nil(t, got.AgentStatus)
}

func TestRevokeUserSession_WorkloadSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := seedIssuer(t, ctx, ti, "workload-revoke")
	workloadIssuerID := seedWorkloadIssuer(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "Revoke issuer")
	subject := urn.NewWorkloadSubject(workloadIssuerID, workloadTestSubject)

	target, err := seedUserSession(t, ctx, ti.conn, issuerID, subject)
	require.NoError(t, err)
	sibling, err := seedUserSession(t, ctx, ti.conn, issuerID, subject)
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)

	err = ti.service.RevokeUserSession(ctx, &gen.RevokeUserSessionPayload{
		ID:               target.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)
	require.Equal(t, target.ID.String(), record.SubjectID)
	require.Equal(t, subject.String(), record.SubjectDisplay, "the audit entry names the workload by issuer and subject")

	require.True(t, jtiRevoked(t, ctx, ti.redis, target.Jti))
	require.False(t, jtiRevoked(t, ctx, ti.redis, sibling.Jti), "revocation is per session, not per workload")

	status := "all"
	items := listAllSessions(t, ctx, ti, &status)
	revoked := sessionByID(t, items, target.ID)
	require.NotNil(t, revoked.RevokedAt)
	require.NotNil(t, revoked.Workload)
	require.Nil(t, sessionByID(t, items, sibling.ID).RevokedAt)

	err = ti.service.RevokeUserSession(ctx, &gen.RevokeUserSessionPayload{
		ID:               target.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
