package authclients_test

import (
	"context"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestAttachAuthClientLocksServerBeforeClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Ordered attachment")
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	holder := testrepo.New(tx)
	_, err := holder.LockMCPServerForUpdateFixture(ctx, server.id)
	require.NoError(t, err)
	pid, err := holder.BackendPIDFixture(ctx)
	require.NoError(t, err)
	attached := make(chan error, 1)
	go func() {
		_, err := ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: clientID.String(), McpServerID: server.id.String()})
		attached <- err
	}()
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(ti.conn).CountBackendsBlockedByFixture(ctx, pid)
		return err == nil && blocked > 0
	}, 10*time.Second, 20*time.Millisecond)
	// An enable writer can take the client while attach waits on its server.
	lockCtx, cancelLock := context.WithTimeout(ctx, 2*time.Second)
	defer cancelLock()
	_, err = repo.New(tx).LockAuthClient(lockCtx, repo.LockAuthClientParams{ID: clientID, OrganizationID: ti.organizationID})
	require.NoError(t, err, "attach must not hold the client while waiting for its server")
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-attached)
}
