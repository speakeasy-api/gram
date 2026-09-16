package workloadidentity_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// seedAgent creates an agent with an owner, since agents pin owner_user_id to a
// live membership in the same organization.
func seedAgent(t *testing.T, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	ctx := t.Context()
	userID := fmt.Sprintf("user-%s", uuid.NewString()[:8])

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: userID,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	agent, err := agentsrepo.New(conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: organizationID,
		OwnerUserID:    userID,
		Name:           fmt.Sprintf("Agent %s", uuid.NewString()[:8]),
	})
	require.NoError(t, err)

	return agent.ID
}

// seedAssignment assigns an agent to a workload principal directly. Raw SQL for
// the same reason seedIssuer uses it: writes are the management API's job, and
// this package is read-only by design.
func seedAssignment(t *testing.T, conn *pgxpool.Pool, organizationID string, issuerID uuid.UUID, subject string, agentID uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		t.Context(), `
		INSERT INTO workload_agent_assignments
		  (organization_id, workload_issuer_id, subject, agent_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, organizationID, issuerID, subject, agentID).Scan(&id)
	require.NoError(t, err)

	return id
}

func unassign(t *testing.T, conn *pgxpool.Pool, id uuid.UUID) {
	t.Helper()

	_, err := conn.Exec( //nolint:glint // notestingrawsql: see seedAssignment
		t.Context(), `UPDATE workload_agent_assignments SET deleted_at = clock_timestamp() WHERE id = $1`, id)
	require.NoError(t, err)
}

// assignmentFixture is one tenant with one issuer and one agent.
type assignmentFixture struct {
	tenant   tenant
	issuerID uuid.UUID
	agentID  uuid.UUID
}

func newAssignmentFixture(t *testing.T, conn *pgxpool.Pool) assignmentFixture {
	t.Helper()

	tenant := newTenant(t, conn)

	return assignmentFixture{
		tenant:   tenant,
		issuerID: seedIssuer(t, conn, tenant.organizationID, organizationTier(), "gh-actions", testIssuerURL, epoch),
		agentID:  seedAgent(t, conn, tenant.organizationID),
	}
}

func (f assignmentFixture) params() workloadidentity.AssignmentParams {
	return workloadidentity.AssignmentParams{
		OrganizationID:   f.tenant.organizationID,
		WorkloadIssuerID: f.issuerID,
		Subject:          testSubject,
	}
}

func TestResolveAssignedAgent_AnAssignedAgentResolves(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAssignmentFixture(t, conn)
	seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, testSubject, fixture.agentID)

	agentID, found, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, fixture.agentID, agentID)
}

// No assigned agent is no authority, which is a decision rather than an error.
func TestResolveAssignedAgent_AWorkloadWithNoAssignmentResolvesNothing(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAssignmentFixture(t, conn)

	agentID, found, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, uuid.Nil, agentID)
}

// Changing any single component of the key must not resolve.
func TestResolveAssignedAgent_EveryComponentOfTheKeyMustMatch(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAssignmentFixture(t, conn)
	seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, testSubject, fixture.agentID)

	other := newTenant(t, conn)
	otherIssuer := seedIssuer(t, conn, fixture.tenant.organizationID, organizationTier(), "staging", "https://staging.example.test", epoch)

	for name, mutate := range map[string]func(workloadidentity.AssignmentParams) workloadidentity.AssignmentParams{
		"another organization": func(p workloadidentity.AssignmentParams) workloadidentity.AssignmentParams {
			p.OrganizationID = other.organizationID
			return p
		},
		"another issuer": func(p workloadidentity.AssignmentParams) workloadidentity.AssignmentParams {
			p.WorkloadIssuerID = otherIssuer
			return p
		},
		"another subject": func(p workloadidentity.AssignmentParams) workloadidentity.AssignmentParams {
			p.Subject = testSubject + ":other"
			return p
		},
		"an empty organization": func(p workloadidentity.AssignmentParams) workloadidentity.AssignmentParams {
			p.OrganizationID = ""
			return p
		},
		"an empty subject": func(p workloadidentity.AssignmentParams) workloadidentity.AssignmentParams {
			p.Subject = ""
			return p
		},
		"no issuer": func(p workloadidentity.AssignmentParams) workloadidentity.AssignmentParams {
			p.WorkloadIssuerID = uuid.Nil
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			agentID, found, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, mutate(fixture.params()))

			require.NoError(t, err)
			require.False(t, found, "%s must not resolve against the assigned row", name)
			require.Equal(t, uuid.Nil, agentID)
		})
	}
}

// Unassigning is a soft delete; ignoring it would keep the workload acting with
// authority an administrator believes they have removed.
func TestResolveAssignedAgent_AnUnassignedAgentDoesNotResolve(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAssignmentFixture(t, conn)
	id := seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, testSubject, fixture.agentID)

	unassign(t, conn, id)

	_, found, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.False(t, found)
}

// Reassigning after unassigning resolves the new agent, which is what makes the
// lookup safe to define as :one.
func TestResolveAssignedAgent_ReassignmentResolvesTheNewAgent(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAssignmentFixture(t, conn)
	id := seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, testSubject, fixture.agentID)
	unassign(t, conn, id)

	replacement := seedAgent(t, conn, fixture.tenant.organizationID)
	seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, testSubject, replacement)

	agentID, found, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, fixture.params())

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, replacement, agentID)
}

// One live agent per workload, enforced by the schema rather than by the caller.
// This is what lets the lookup return a single row.
func TestResolveAssignedAgent_AWorkloadCannotHoldTwoLiveAgents(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAssignmentFixture(t, conn)
	seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, testSubject, fixture.agentID)

	second := seedAgent(t, conn, fixture.tenant.organizationID)
	_, err = conn.Exec( //nolint:glint // notestingrawsql: see seedAssignment
		t.Context(), `
		INSERT INTO workload_agent_assignments
		  (organization_id, workload_issuer_id, subject, agent_id)
		VALUES ($1, $2, $3, $4)
	`, fixture.tenant.organizationID, fixture.issuerID, testSubject, second)

	require.Error(t, err, "a second live assignment must be rejected by workload_agent_assignments_workload_key")
}

// An agent serves several workloads, which is the direction the relationship
// does allow.
func TestResolveAssignedAgent_OneAgentServesSeveralWorkloads(t *testing.T) {
	t.Parallel()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixture := newAssignmentFixture(t, conn)
	const otherSubject = "repo:acme/payments-api:ref:refs/heads/release"

	seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, testSubject, fixture.agentID)
	seedAssignment(t, conn, fixture.tenant.organizationID, fixture.issuerID, otherSubject, fixture.agentID)

	first, found, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, fixture.params())
	require.NoError(t, err)
	require.True(t, found)

	secondParams := fixture.params()
	secondParams.Subject = otherSubject
	second, found, err := workloadidentity.ResolveAssignedAgent(t.Context(), conn, secondParams)
	require.NoError(t, err)
	require.True(t, found)

	require.Equal(t, fixture.agentID, first)
	require.Equal(t, fixture.agentID, second)
}
