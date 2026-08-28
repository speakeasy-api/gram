//nolint:glint // This lock-race test needs concurrent transactions against owner-domain rows.
package mcptoolexecution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

func TestCustomerLifecycleValidatorLocksLivenessUpdatesUntilCommit(t *testing.T) {
	t.Parallel()
	db, orgID := newTestDatabase(t, "ks_customer_lock")
	userID := "user_" + uuid.NewString()
	insertUser(t, db, userID, nil)
	insertMembership(t, db, orgID, userID, nil)
	projectID := insertProject(t, db, orgID, "project-lock", nil)
	serverID := insertMCPServer(t, db, orgID, projectID, nil)

	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	err = NewCustomerLifecycleValidator().ValidateCurrent(t.Context(), tx, killswitches.CurrentReferenceBatch{
		OrganizationID: killswitches.OrganizationID(orgID),
		Principal:      &killswitches.CurrentPrincipalReference{Kind: PrincipalKindUser, Key: killswitches.PrincipalKey(userID)},
		Resources:      &killswitches.CurrentResourceReferences{Kind: ResourceKindMCPServer, Keys: []killswitches.ResourceKey{killswitches.ResourceKey(serverID.String())}},
	})
	require.NoError(t, err)

	userUpdate := make(chan error, 1)
	serverUpdate := make(chan error, 1)
	go func() {
		_, updateErr := db.Exec(t.Context(), `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
		userUpdate <- updateErr
	}()
	go func() {
		_, updateErr := db.Exec(t.Context(), `UPDATE mcp_servers SET deleted_at = clock_timestamp() WHERE id = $1`, serverID)
		serverUpdate <- updateErr
	}()

	for name, result := range map[string]<-chan error{"user": userUpdate, "server": serverUpdate} {
		select {
		case updateErr := <-result:
			require.Failf(t, "liveness update did not wait", "%s update completed before lifecycle commit: %v", name, updateErr)
		case <-time.After(150 * time.Millisecond):
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
