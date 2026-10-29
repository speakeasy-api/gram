package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	backfill "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// A deferred trigger pauses the real transaction at COMMIT, after its reach
// decisions and all synchronization writes. No production testing hooks or
// scheduling sleeps are needed. Each fixture has its own isolated database.
func pauseEMACommit(t *testing.T, ctx context.Context, ti *testInstance, table string) (pgx.Tx, int32) {
	t.Helper()
	require.Contains(t, []string{"remote_session_ema_bindings", "organization_features"}, table)
	_, err := ti.conn.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION pause_ema_test_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(79182351::bigint); RETURN NEW; END $$;
 CREATE CONSTRAINT TRIGGER pause_ema_test_commit AFTER INSERT OR UPDATE ON %s DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pause_ema_test_commit()`, table))
	require.NoError(t, err)
	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(79182351::bigint)`)
	require.NoError(t, err)
	var pid int32
	require.NoError(t, tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	return tx, pid
}

func seedPreparationGatewayIssuer(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()
	auth, _ := contextvalues.GetAuthContext(ctx)
	id := uuid.New()
	require.NoError(t, backfill.New(ti.conn).SeedOrganizationUserSessionIssuerFixture(ctx, backfill.SeedOrganizationUserSessionIssuerFixtureParams{ID: id, OrganizationID: pgtype.Text{String: auth.ActiveOrganizationID, Valid: true}, Slug: "gateway-" + id.String()}))
	return id
}

func TestPreparationGateway_FirstBindingAndOptInSerializeBeforeDiscovery(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	auth, _ := contextvalues.GetAuthContext(ctx)
	server := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "first-gateway-member", in.Resource)
	_, err := ti.conn.Exec(ctx, `INSERT INTO remote_mcp_server_headers(remote_mcp_server_id,name,is_secret,value) SELECT remote_mcp_server_id,'X-Gateway-Test',true,'encrypted-test-value' FROM mcp_servers WHERE id=$1`, server)
	require.NoError(t, err)
	in.UserSessionIssuerID = seedPreparationGatewayIssuer(t, ctx, ti)
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	blocker, blockerPID := pauseEMACommit(t, ctx, ti, "remote_session_ema_bindings")
	prepared := make(chan error, 1)
	go func() { _, err := ti.service.PrepareIdentityChaining(ctx, in); prepared <- err }()
	prepPID := waitForBlockedPreparationPID(t, ctx, ti, blockerPID)
	// This first binding is uncommitted and has already decided there is no
	// configured gateway reach. A project-only toggle lock would discover nothing.
	var count int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_ema_bindings WHERE organization_id=$1`, auth.ActiveOrganizationID).Scan(&count))
	require.Zero(t, count)
	toggled := make(chan error, 1)
	go func() {
		toggled <- ti.features.SetFeatureEnabled(ctx, auth.ActiveOrganizationID, productfeatures.FeatureOrganizationGatewayDisabled, false)
	}()
	_ = waitForBlockedPreparationPID(t, ctx, ti, prepPID)
	require.NoError(t, blocker.Commit(ctx))
	for _, done := range []chan error{prepared, toggled} {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	typed := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
	links, err := backfill.New(ti.conn).ListBackfillLinkedServersFixture(ctx, typed)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{server}, links)
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_auth_client_headers WHERE auth_client_id=$1 AND organization_id=$2 AND NOT deleted`, typed, auth.ActiveOrganizationID).Scan(&count))
	require.Equal(t, 1, count, "committed gateway reach must carry its source credential copy")
	var disabled bool
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT NOT deleted FROM organization_features WHERE organization_id=$1 AND feature_name='organization_gateway_disabled'`, auth.ActiveOrganizationID).Scan(&disabled))
	require.False(t, disabled)
}

func TestPreparationGateway_OptInAndActualSourceCreationSerialize(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	auth, _ := contextvalues.GetAuthContext(ctx)
	directIssuer := in.UserSessionIssuerID
	in.UserSessionIssuerID = seedPreparationGatewayIssuer(t, ctx, ti)
	in.Resource = "https://127.0.0.1/gateway-source"
	in.ConfirmGrants = []string{oauthwire.GrantTypeJWTBearer}
	_, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	blocker, blockerPID := pauseEMACommit(t, ctx, ti, "organization_features")
	toggled := make(chan error, 1)
	go func() {
		toggled <- ti.features.SetFeatureEnabled(ctx, auth.ActiveOrganizationID, productfeatures.FeatureOrganizationGatewayDisabled, false)
	}()
	togglePID := waitForBlockedPreparationPID(t, ctx, ti, blockerPID)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	provisioner := remotemcp.NewRemoteMCPProvisioningService(ti.conn, policy, audit.NewLogger(), nil)
	type outcome struct {
		result remotemcp.RemoteMCPProvisioningResult
		err    error
	}
	created := make(chan outcome, 1)
	go func() {
		result, err := provisioner.ProvisionDashboardRemoteMCP(ctx, auth, remotemcp.DashboardRemoteMCPProvisioningInput{URL: in.Resource, TransportType: "sse", UserSessionIssuerID: conv.PtrEmpty(directIssuer.String())})
		created <- outcome{result, err}
	}()
	_ = waitForBlockedPreparationPID(t, ctx, ti, togglePID)
	require.NoError(t, blocker.Commit(ctx))
	select {
	case err := <-toggled:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case out := <-created:
		require.NoError(t, out.err)
		typed := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
		links, err := backfill.New(ti.conn).ListBackfillLinkedServersFixture(ctx, typed)
		require.NoError(t, err)
		require.Equal(t, []uuid.UUID{out.result.MCPServer.ID}, links)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
