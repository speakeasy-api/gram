package agentownership_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agentownership"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var cloneTestDatabase testenv.PostgresDBCloneFunc

func TestMain(m *testing.M) {
	ctx := context.Background()
	container, cloneFunc, err := testenv.NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("launch test postgres: %v", err)
	}
	cloneTestDatabase = cloneFunc
	code := m.Run()
	if err := container.Terminate(ctx); err != nil {
		log.Fatalf("terminate test postgres: %v", err)
	}
	os.Exit(code)
}

func newTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	conn, err := cloneTestDatabase(t, "agent_ownership")
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn
}

func seedOrganization(t *testing.T, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Test Organization", Slug: organizationID, WorkosID: conv.PtrToPGText(nil),
	})
	require.NoError(t, err)
}

func seedOrganizationUser(t *testing.T, conn *pgxpool.Pool, organizationID, userID string) {
	t.Helper()
	_, err := usersrepo.New(conn).UpsertUser(t.Context(), usersrepo.UpsertUserParams{
		ID: userID, Email: userID + "@example.com", DisplayName: userID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func TestOwnerLossLatchRejectsUnknownReason(t *testing.T) {
	t.Parallel()

	err := agentownership.LatchOwnerLossByUser(t.Context(), nil, "owner", agentownership.OwnerReassignmentReason("unknown"), agentownership.SystemActor, nil)
	require.ErrorContains(t, err, "invalid owner reassignment reason")
}

func TestOwnerLossLatchIsDurableScopedAndIdempotent(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org-one")
	seedOrganization(t, conn, "org-two")
	seedOrganizationUser(t, conn, "org-one", "owner")
	seedOrganizationUser(t, conn, "org-two", "owner")
	one, err := agentrepo.New(conn).CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: "org-one", OwnerUserID: "owner", Name: "One"})
	require.NoError(t, err)
	two, err := agentrepo.New(conn).CreateAgent(t.Context(), agentrepo.CreateAgentParams{OrganizationID: "org-two", OwnerUserID: "owner", Name: "Two"})
	require.NoError(t, err)

	require.NoError(t, agentownership.LatchOwnerLossByMembership(t.Context(), conn, "org-one", "owner", agentownership.OwnerReassignmentReasonMembershipLost, agentownership.SystemActor, nil))
	latchedOne, err := agentrepo.New(conn).GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-one", ID: one.ID})
	require.NoError(t, err)
	require.True(t, latchedOne.OwnerReassignmentRequiredAt.Valid)
	require.Equal(t, string(agentownership.OwnerReassignmentReasonMembershipLost), latchedOne.OwnerReassignmentReason.String)
	unlatchedTwo, err := agentrepo.New(conn).GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-two", ID: two.ID})
	require.NoError(t, err)
	require.False(t, unlatchedTwo.OwnerReassignmentRequiredAt.Valid)

	firstLatch := latchedOne.OwnerReassignmentRequiredAt.Time
	require.NoError(t, agentownership.LatchOwnerLossByMembership(t.Context(), conn, "org-one", "owner", agentownership.OwnerReassignmentReasonOwnerInactive, agentownership.SystemActor, nil))
	latchedOne, err = agentrepo.New(conn).GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-one", ID: one.ID})
	require.NoError(t, err)
	require.Equal(t, firstLatch, latchedOne.OwnerReassignmentRequiredAt.Time)
	require.Equal(t, string(agentownership.OwnerReassignmentReasonMembershipLost), latchedOne.OwnerReassignmentReason.String)

	require.NoError(t, agentownership.LatchOwnerLossByUser(t.Context(), conn, "owner", agentownership.OwnerReassignmentReasonOwnerDeleted, agentownership.SystemActor, nil))
	latchedTwo, err := agentrepo.New(conn).GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-two", ID: two.ID})
	require.NoError(t, err)
	require.True(t, latchedTwo.OwnerReassignmentRequiredAt.Valid)
	require.Equal(t, string(agentownership.OwnerReassignmentReasonOwnerDeleted), latchedTwo.OwnerReassignmentReason.String)

	var ownerLossEvents int
	err = conn.QueryRow(t.Context(), `SELECT count(*) FROM audit_logs WHERE action = 'agent:owner_loss'`).Scan(&ownerLossEvents) //nolint:glint // notestingrawsql: idempotent audit emission is the assertion
	require.NoError(t, err)
	require.Equal(t, 2, ownerLossEvents)
}
