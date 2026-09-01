//nolint:glint // This lock-race test needs concurrent transactions against owner-domain rows.
package mcptoolexecution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

func TestCustomerLifecycleValidatorAcceptsOnlyRegisteredHookActivities(t *testing.T) {
	t.Parallel()
	db, orgID := newTestDatabase(t, "ks_customer_hook_activity")
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	bindings := delegation.ApprovedBindings()
	keys := make([]killswitches.ResourceKey, len(bindings))
	for i, binding := range bindings {
		keys[i] = killswitches.ResourceKey(binding.ResourceKey)
	}
	validator := NewCustomerLifecycleValidator()
	require.NoError(t, validator.ValidateCurrent(t.Context(), tx, killswitches.CurrentReferenceBatch{
		OrganizationID: killswitches.OrganizationID(orgID),
		Resources:      &killswitches.CurrentResourceReferences{Kind: ResourceKindHookActivity, Keys: keys},
	}))

	err = validator.ValidateCurrent(t.Context(), tx, killswitches.CurrentReferenceBatch{
		OrganizationID: killswitches.OrganizationID(orgID),
		Resources:      &killswitches.CurrentResourceReferences{Kind: ResourceKindHookActivity, Keys: []killswitches.ResourceKey{"claude:PermissionRequest"}},
	})
	require.ErrorIs(t, err, killswitches.ErrInvalidReference)

	err = validator.ValidateCurrent(t.Context(), tx, killswitches.CurrentReferenceBatch{
		OrganizationID: killswitches.OrganizationID(orgID),
		Resources:      &killswitches.CurrentResourceReferences{Kind: ResourceKindHookActivity, Keys: nil},
	})
	require.ErrorIs(t, err, killswitches.ErrInvalidReference)
}

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
		_, updateErr := userConn.Exec(t.Context(), `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
		userUpdate <- updateErr
	}()
	go func() {
		started <- "server"
		_, updateErr := serverConn.Exec(t.Context(), `UPDATE mcp_servers SET deleted_at = clock_timestamp() WHERE id = $1`, serverID)
		serverUpdate <- updateErr
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("soft-delete goroutine did not start")
		}
	}
	for name, pid := range map[string]uint32{"user": userConn.Conn().PgConn().PID(), "server": serverConn.Conn().PgConn().PID()} {
		require.Eventually(t, func() bool {
			var waiting bool
			err := db.QueryRow(t.Context(), `SELECT COALESCE(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&waiting)
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
