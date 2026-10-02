package mcptoolexecution

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestCustomerLifecycleValidatorLocksLivenessUpdatesUntilCommit(t *testing.T) {
	t.Parallel()
	db, orgID := newTestDatabase(t, "ks_customer_lock")
	userID := "user_" + uuid.NewString()
	insertUser(t, db, userID, false)
	insertMembership(t, db, orgID, userID, false)
	projectID := insertProject(t, db, orgID, "project-lock", false)
	serverID := insertMCPServer(t, db, orgID, projectID, false)

	tx := testenv.BeginTx(t, t.Context(), db)
	err := NewCustomerLifecycleValidator().ValidateCurrent(t.Context(), tx, killswitches.CurrentReferenceBatch{
		OrganizationID: killswitches.OrganizationID(orgID),
		Principal:      &killswitches.CurrentPrincipalReference{Kind: PrincipalKindUser, Key: killswitches.PrincipalKey(userID)},
		Resources:      &killswitches.CurrentResourceReferences{Kind: ResourceKindMCPServer, Keys: []killswitches.ResourceKey{killswitches.ResourceKey(serverID.String())}},
	})
	require.NoError(t, err)

	// probeTimeout bounds each exact row-lock probe.
	const probeTimeout = 100 * time.Millisecond
	userProbe := testenv.BeginTx(t, t.Context(), db)
	testenv.SetLockTimeout(t, t.Context(), userProbe, probeTimeout)
	err = testrepo.New(userProbe).ForceSoftDeleteOrganizationUserRelationship(t.Context(), testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: pgtype.Text{String: userID, Valid: true}})
	testenv.RequireLockNotAvailable(t, err)
	require.NoError(t, userProbe.Rollback(t.Context()))
	serverProbe := testenv.BeginTx(t, t.Context(), db)
	testenv.SetLockTimeout(t, t.Context(), serverProbe, probeTimeout)
	_, err = mcpserversrepo.New(serverProbe).DeleteMCPServer(t.Context(), mcpserversrepo.DeleteMCPServerParams{ID: serverID, ProjectID: projectID})
	testenv.RequireLockNotAvailable(t, err)
	require.NoError(t, serverProbe.Rollback(t.Context()))
	require.NoError(t, tx.Commit(t.Context()))
	require.NoError(t, testrepo.New(db).ForceSoftDeleteOrganizationUserRelationship(t.Context(), testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: pgtype.Text{String: userID, Valid: true}}))
	_, err = mcpserversrepo.New(db).DeleteMCPServer(t.Context(), mcpserversrepo.DeleteMCPServerParams{ID: serverID, ProjectID: projectID})
	require.NoError(t, err)
}
