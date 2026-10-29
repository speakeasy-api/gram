package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	authclientsgen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

func holdPreparationRegistration(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) (pgx.Tx, int32) {
	t.Helper()
	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var pid int32
	require.NoError(t, tx.QueryRow(ctx, `SELECT pg_backend_pid() FROM remote_session_clients WHERE id=$1 FOR UPDATE`, id).Scan(&pid))
	return tx, pid
}

func waitForBlockedPreparationPID(t *testing.T, ctx context.Context, ti *testInstance, blocker int32) int32 {
	t.Helper()
	var pid int32
	require.Eventually(t, func() bool {
		return ti.conn.QueryRow(ctx, `SELECT a.pid FROM pg_stat_activity a WHERE a.datname=current_database() AND $1=ANY(pg_blocking_pids(a.pid)) LIMIT 1`, blocker).Scan(&pid) == nil
	}, 5*time.Second, 10*time.Millisecond)
	return pid
}

func TestPrepareEMA_ProjectGateBlocksActualSourceCreation(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	in.Resource = "https://127.0.0.1/mcp"
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	auth, _ := contextvalues.GetAuthContext(ctx)
	blocker, blockerPID := holdPreparationRegistration(t, ctx, ti, in.ClientID)
	prepared := make(chan error, 1)
	go func() { _, err := ti.service.PrepareIdentityChaining(ctx, in); prepared <- err }()
	prepPID := waitForBlockedPreparationPID(t, ctx, ti, blockerPID)
	// Preparation now owns its empty-project gate, with no source rows to lock.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	provisioner := remotemcp.NewRemoteMCPProvisioningService(ti.conn, policy, audit.NewLogger(), nil)
	created := make(chan error, 1)
	go func() {
		_, err := provisioner.ProvisionDashboardRemoteMCP(ctx, auth, remotemcp.DashboardRemoteMCPProvisioningInput{URL: in.Resource, TransportType: "sse", UserSessionIssuerID: conv.PtrEmpty(in.UserSessionIssuerID.String())})
		created <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := ti.conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE a.datname=current_database() AND a.wait_event='advisory' AND $1=ANY(pg_blocking_pids(a.pid)))`, prepPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "actual source creation did not wait on preparation's project gate")
	var count int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_mcp_servers WHERE project_id=$1`, *auth.ProjectID).Scan(&count))
	require.Zero(t, count, "source inserted before acquiring the scope gate")
	require.NoError(t, blocker.Commit(ctx))
	for _, done := range []chan error{prepared, created} {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_mcp_servers WHERE project_id=$1`, *auth.ProjectID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestPrepareEMA_ActualAdminEnableCompletesUnderContention(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "admin-lock-order", in.Resource)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	_, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	typed := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, err = ti.conn.Exec(ctx, `UPDATE mcp_auth_clients SET enabled=false WHERE organization_id=$1 AND id=$2`, auth.ActiveOrganizationID, typed)
	require.NoError(t, err)
	blocker, blockerPID := holdPreparationRegistration(t, ctx, ti, in.ClientID)
	prepared := make(chan error, 1)
	go func() { _, err := ti.service.PrepareIdentityChaining(ctx, in); prepared <- err }()
	prepPID := waitForBlockedPreparationPID(t, ctx, ti, blockerPID)
	logger := testenv.NewLogger(t)
	admin := authclients.NewService(logger, testenv.NewTracerProvider(t), ti.conn, ti.sessionManager, authz.NewEngine(logger, ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), audit.NewLogger(), testenv.NewEncryptionClient(t))
	updated := make(chan error, 1)
	go func() {
		_, err := admin.UpdateAuthClient(ctx, &authclientsgen.UpdateAuthClientPayload{ID: typed.String(), Enabled: new(true)})
		updated <- err
	}()
	// The actual admin API reaches its server lock while preparation owns that
	// server. Releasing registration now must let preparation proceed; the admin
	// must not have taken the typed client first and formed the inverse cycle.
	_ = waitForBlockedPreparationPID(t, ctx, ti, prepPID)
	require.NoError(t, blocker.Commit(ctx))
	for _, done := range []chan error{prepared, updated} {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var enabled bool
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT enabled FROM mcp_auth_clients WHERE organization_id=$1 AND id=$2`, auth.ActiveOrganizationID, typed).Scan(&enabled))
	require.True(t, enabled)
	require.Equal(t, typed, assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource))
}
