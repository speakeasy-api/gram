package mcptoolexecution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
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

	//nolint:glint // notestingrawsql: transaction runs only the validator's SQLc queries and holds the row locks the concurrent soft deletes must wait on
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	err = NewCustomerLifecycleValidator().ValidateCurrent(t.Context(), tx, killswitches.CurrentReferenceBatch{
		OrganizationID: killswitches.OrganizationID(orgID),
		Principal:      &killswitches.CurrentPrincipalReference{Kind: PrincipalKindUser, Key: killswitches.PrincipalKey(userID)},
		Resources:      &killswitches.CurrentResourceReferences{Kind: ResourceKindMCPServer, Keys: []killswitches.ResourceKey{killswitches.ResourceKey(serverID.String())}},
	})
	require.NoError(t, err)

	userConn, err := db.Acquire(t.Context())
	require.NoError(t, err)
	defer userConn.Release()
	serverConn, err := db.Acquire(t.Context())
	require.NoError(t, err)
	defer serverConn.Release()

	started := make(chan string, 2)
	userUpdate := make(chan error, 1)
	serverUpdate := make(chan error, 1)
	go func() {
		started <- "user"
		updateErr := testrepo.New(userConn).ForceSoftDeleteOrganizationUserRelationship(t.Context(), testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{
			OrganizationID: orgID, UserID: pgtype.Text{String: userID, Valid: true},
		})
		userUpdate <- updateErr
	}()
	go func() {
		started <- "server"
		_, updateErr := mcpserversrepo.New(serverConn).DeleteMCPServer(t.Context(), mcpserversrepo.DeleteMCPServerParams{ID: serverID, ProjectID: projectID})
		serverUpdate <- updateErr
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("soft-delete goroutine did not start")
		}
	}
	// Each test runs in its own cloned database, so the sqlc query name
	// identifies the one soft delete issued on each dedicated connection.
	probe := testrepo.New(db)
	for name, pattern := range map[string]string{"user": "%name: ForceSoftDeleteOrganizationUserRelationship :exec%", "server": "%name: DeleteMCPServer :one%"} {
		require.Eventually(t, func() bool {
			waiting, err := probe.IsQueryBlockedOnLockFixture(t.Context(), pattern)
			return err == nil && waiting
		}, 2*time.Second, 10*time.Millisecond, "%s soft delete never reached the row lock", name)
	}
	for name, result := range map[string]<-chan error{"user": userUpdate, "server": serverUpdate} {
		select {
		case updateErr := <-result:
			require.Failf(t, "liveness update did not wait", "%s update completed before lifecycle commit: %v", name, updateErr)
		default:
		}
	}
	require.NoError(t, tx.Commit(t.Context()))

	select {
	case updateErr := <-userUpdate:
		require.NoError(t, updateErr)
	case <-time.After(2 * time.Second):
		t.Fatal("user soft delete remained blocked after lifecycle commit")
	}
	select {
	case updateErr := <-serverUpdate:
		require.NoError(t, updateErr)
	case <-time.After(2 * time.Second):
		t.Fatal("server soft delete remained blocked after lifecycle commit")
	}
}
