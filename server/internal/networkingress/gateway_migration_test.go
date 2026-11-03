package networkingress_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	gatewayrepo "github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/tailscaleapi"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// operatorIngress creates an ingress and moves it back onto the operator, the
// state every ingress created before the gateway is in.
func operatorIngress(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()
	id := uuid.MustParse(ti.create(t, ctx).ID)
	_, err := gatewayrepo.New(ti.conn).MarkNetworkIngressRuntime(ctx, gatewayrepo.MarkNetworkIngressRuntimeParams{
		Runtime: pgtype.Text{String: "", Valid: false}, ID: id, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	return id
}

func newMigrator(t *testing.T, ti *testInstance, api tailscaleapi.Client) *networkingress.GatewayMigrator {
	t.Helper()
	migrator, err := networkingress.NewGatewayMigrator(testenv.NewLogger(t), ti.conn, testenv.NewEncryptionClient(t), api, "tag:speakeasy",
		networkingress.NewOutboxRequester("test-network-ingress"), audit.NewLogger())
	require.NoError(t, err)
	return migrator
}

func moveAuditRows(t *testing.T, ctx context.Context, ti *testInstance) []auditrepo.ListAuditLogsRow {
	t.Helper()
	rows, err := auditrepo.New(ti.conn).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{
		OrganizationID: ti.orgID, Action: pgtype.Text{String: string(audit.ActionNetworkIngressMoveRuntime), Valid: true},
	})
	require.NoError(t, err)
	return rows
}

var staffActor = urn.NewPrincipal(urn.PrincipalTypeUser, "staff-subject")

func TestGatewayMigratorRefusesCredentialsThatCannotRegisterGatewayNodes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id := operatorIngress(t, ctx, ti)
	api := tailscaleapi.NewFake()
	api.Reject = tailscaleapi.ErrUnauthorized
	migrator := newMigrator(t, ti, api)

	_, err := migrator.MoveToGateway(ctx, ti.orgID, staffActor, new("Staff"))
	require.ErrorIs(t, err, networkingress.ErrGatewayCredentialsRejected)

	row, err := repo.New(ti.conn).GetNetworkIngressByID(ctx, repo.GetNetworkIngressByIDParams{ID: id, OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.False(t, row.Runtime.Valid, "a failed check leaves the operator hosting the ingress")
	require.Empty(t, moveAuditRows(t, ctx, ti))
}

func TestGatewayMigratorMovesOneOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	id := operatorIngress(t, ctx, ti)
	before, err := repo.New(ti.conn).ListNetworkIngressReconcileRequests(ctx, ti.orgID)
	require.NoError(t, err)
	api := tailscaleapi.NewFake()
	migrator := newMigrator(t, ti, api)

	state, err := migrator.State(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, networkingress.RuntimeOperator, state.Runtime)

	state, err = migrator.MoveToGateway(ctx, ti.orgID, staffActor, new("Staff"))
	require.NoError(t, err)
	require.Equal(t, networkingress.RuntimeGateway, state.Runtime)
	require.Equal(t, "pending", state.Ingress.Status)

	keys := api.Keys()
	require.Len(t, keys, 1, "the credential check mints exactly one key")
	require.Equal(t, []string{keys[0].ID}, api.RevokedKeys(), "and revokes it straight away")

	row, err := repo.New(ti.conn).GetNetworkIngressByID(ctx, repo.GetNetworkIngressByIDParams{ID: id, OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.Equal(t, networkingress.RuntimeGateway, row.Runtime.String)
	after, err := repo.New(ti.conn).ListNetworkIngressReconcileRequests(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, after, len(before)+1, "the move queues a reconcile")
	audits := moveAuditRows(t, ctx, ti)
	require.Len(t, audits, 1)
	require.Equal(t, id.String(), audits[0].SubjectID)

	// Moving again is a no-op.
	state, err = migrator.MoveToGateway(ctx, ti.orgID, staffActor, new("Staff"))
	require.NoError(t, err)
	require.Equal(t, networkingress.RuntimeGateway, state.Runtime)
	require.Len(t, api.Keys(), 1)
	require.Len(t, moveAuditRows(t, ctx, ti), 1)
}

func TestGatewayMigratorRequiresAnIngress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	migrator := newMigrator(t, ti, tailscaleapi.NewFake())
	_, err := migrator.State(ctx, ti.orgID)
	require.ErrorIs(t, err, networkingress.ErrGatewayIngressNotFound)
	_, err = migrator.MoveToGateway(ctx, ti.orgID, staffActor, nil)
	require.ErrorIs(t, err, networkingress.ErrGatewayIngressNotFound)
}
