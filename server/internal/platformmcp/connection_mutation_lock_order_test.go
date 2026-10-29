package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSetMCPNetworkAccessPreflightsBeforeIngress(t *testing.T) {
	t.Parallel()
	testConnectionMutationPreflightsBeforeIngress(t, false)
}

func TestSetMCPAddressPreflightsBeforeIngress(t *testing.T) {
	t.Parallel()
	testConnectionMutationPreflightsBeforeIngress(t, true)
}

func testConnectionMutationPreflightsBeforeIngress(t *testing.T, address bool) {
	t.Helper()
	for _, confirmed := range []bool{false, true} {
		name := "preview"
		if confirmed {
			name = "confirmed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			kind := "network"
			if address {
				kind = "address"
			}
			ctx, f := seedConnectionMutationFixture(t, "platform_mcp_"+kind+"_lock_order_"+name, allowConnectionNetworkAccess{})
			before := f.settings(t, ctx, MCPConnectionSettingsMCPServer, f.hostedID)
			holder := testenv.BeginTx(t, ctx, f.conn)
			require.NoError(t, authclientbackfill.LockEMAWriterScope(ctx, holder, f.principal.OrganizationID, []uuid.UUID{f.project.ID}))
			holderPID := holder.Conn().PgConn().PID()

			mutationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				if address {
					_, err = f.setAddress(mutationCtx, MCPConnectionSettingsMCPServer, f.hostedID, before.Version, f.hostedEndpoint.String(), f.orgSlug+"-renamed", "", confirmed)
				} else {
					_, err = f.mutations.SetNetworkAccess(mutationCtx, f.principal, SetMCPNetworkAccessInput{
						ProjectID: f.project.ID.String(), TargetKind: MCPConnectionSettingsMCPServer, TargetID: f.hostedID.String(),
						Mode: "public_only", ExpectedVersion: before.Version, IdempotencyKey: uuid.NewString(), Confirmed: confirmed,
					})
				}
				done <- err
			}()

			// Observe a real lock wait, not a scheduling delay. Before the fix,
			// the mutation holds ingress while waiting for the server row; after
			// the fix it waits at preflight without holding ingress.
			require.Eventually(t, func() bool {
				var waiting bool
				//nolint:glint // notestingrawsql: PostgreSQL lock instrumentation observes the live blocking backend.
				err := f.conn.QueryRow(ctx, `SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity
					WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))
				)`, holderPID).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)

			probeCtx, stopProbe := context.WithTimeout(ctx, time.Second)
			err := networkingressrepo.New(holder).AcquireNetworkIngressOrganizationLock(probeCtx, f.principal.OrganizationID)
			stopProbe()
			require.NoError(t, err, "a mutation waiting for EMA scope must not hold ingress")
			require.NoError(t, holder.Commit(ctx))
			select {
			case err := <-done:
				if confirmed {
					require.NoError(t, err)
				} else {
					var preview *connectionMutationPreviewError
					require.ErrorAs(t, err, &preview)
				}
			case <-mutationCtx.Done():
				t.Fatal("connection mutation did not finish after releasing writer scope")
			}
		})
	}
}
