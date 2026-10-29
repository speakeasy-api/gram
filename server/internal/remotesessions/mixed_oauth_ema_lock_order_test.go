package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/stretchr/testify/require"
)

// Use actual PostgreSQL wait edges as barriers. In particular, observing an
// OAuth writer waiting for a source is not enough: a late preflight can already
// own the server or registration that the source writer needs next.
func TestMixedOAuthEMA_OpposingWriterScope(t *testing.T) {
	for _, operation := range []string{"identity_begin", "attach", "resync", "reconcile_inherited_tx"} {
		for _, gate := range []string{"project", "source", "server"} {
			t.Run(operation+"/"+gate, func(t *testing.T) {
				ctx, ti, in := preparationFixture(t)
				ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				auth, _ := contextvalues.GetAuthContext(ctx)
				projectID := *auth.ProjectID
				serverID := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "mixed-writer", in.Resource)
				_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: in.UserSessionIssuerID.String()})
				require.NoError(t, err)
				in.ConfirmGrants = []string{"authorization_code", "refresh_token", oauthwire.GrantTypeJWTBearer}
				prepared, err := ti.service.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				require.Equal(t, remotesessions.PreparationStateReady, prepared.State)
				emaID := assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource)
				var oauthID, sourceID uuid.UUID
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT id FROM mcp_auth_clients WHERE organization_id=$1 AND remote_session_client_id=$2 AND client_type='remote_session' AND NOT deleted`, auth.ActiveOrganizationID, in.ClientID).Scan(&oauthID))
				require.NotEqual(t, oauthID, emaID)
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT remote_mcp_server_id FROM mcp_servers WHERE project_id=$1 AND id=$2`, projectID, serverID).Scan(&sourceID))
				var liveChoices int
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_server_auth_clients WHERE project_id=$1 AND mcp_server_id=$2 AND auth_client_id=ANY($3::uuid[]) AND NOT deleted`, projectID, serverID, []uuid.UUID{oauthID, emaID}).Scan(&liveChoices))
				require.Equal(t, 2, liveChoices, "OAuth and EMA must share the same registration and server reach")

				// Preserve an administrator's removal on a second server, while the first
				// server still offers both choices and participates in the opposing write.
				detachedServer := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "mixed-detached", in.Resource)
				_, err = ti.service.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				var tombstone uuid.UUID
				require.NoError(t, ti.conn.QueryRow(ctx, `UPDATE mcp_server_auth_clients SET source='admin', deleted_at=clock_timestamp() WHERE project_id=$1 AND mcp_server_id=$2 AND auth_client_id=$3 AND NOT deleted RETURNING id`, projectID, detachedServer, emaID).Scan(&tombstone))
				_, err = ti.conn.Exec(ctx, `INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors) VALUES ($1,'user:mixed-writer-test','mcp:connect',jsonb_build_object('resource_kind','mcp_server','resource_id',$2::text))`, auth.ActiveOrganizationID, serverID.String())
				require.NoError(t, err)
				snapshot := func() string {
					var result string
					require.NoError(t, ti.conn.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY id),'[]'::jsonb)::text FROM principal_grants g WHERE organization_id=$1`, auth.ActiveOrganizationID).Scan(&result))
					return result
				}
				grantsBefore := snapshot()
				// Populate real OAuth and EMA header copies before contention, rather
				// than testing an empty header-copy lock set.
				setup, err := ti.conn.Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = setup.Rollback(context.Background()) }()
				_, err = setup.Exec(ctx, `INSERT INTO remote_mcp_server_headers (remote_mcp_server_id,name,is_secret,value) VALUES ($1,'X-Mixed-Lock',true,'test-encrypted-value')`, sourceID)
				require.NoError(t, err)
				require.NoError(t, authclientbackfill.SyncRemoteSource(ctx, setup, auth.ActiveOrganizationID, sourceID))
				require.NoError(t, setup.Commit(ctx))
				var copies int
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_auth_client_headers WHERE organization_id=$1 AND auth_client_id=ANY($2::uuid[]) AND name='X-Mixed-Lock' AND NOT deleted`, auth.ActiveOrganizationID, []uuid.UUID{oauthID, emaID}).Scan(&copies))
				require.Equal(t, 2, copies)
				if operation == "reconcile_inherited_tx" {
					// Restore sole-consumer evidence and remove only the legacy association.
					// Reconcile must really capture a missing row, not take its no-op path.
					_, err = ti.conn.Exec(ctx, `UPDATE mcp_servers SET user_session_issuer_id=NULL WHERE project_id=$1 AND id=$2`, projectID, detachedServer)
					require.NoError(t, err)
					deleted, err := ti.conn.Exec(ctx, `DELETE FROM mcp_server_remote_session_clients WHERE project_id=$1 AND mcp_server_id=$2 AND remote_session_client_id=$3`, projectID, serverID, in.ClientID)
					require.NoError(t, err)
					require.EqualValues(t, 1, deleted.RowsAffected())
				}
				// Make resync's denormalized UPDATE observable; it must not happen before
				// the project/source/server preflight, even when called with a pool.
				_, err = ti.conn.Exec(ctx, `UPDATE mcp_servers SET remote_session_issuer_id=NULL WHERE project_id=$1 AND id=$2`, projectID, serverID)
				require.NoError(t, err)
				writer, err := ti.conn.Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = writer.Rollback(context.Background()) }()
				var writerPID int32
				require.NoError(t, writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
				switch gate {
				case "project":
					require.NoError(t, authclientbackfill.New(writer).LockEMAProjectScope(ctx, authclientbackfill.LockEMAProjectScopeParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: projectID}))
				case "source":
					require.NoError(t, authclientbackfill.LockRemoteSource(ctx, writer, auth.ActiveOrganizationID, sourceID))
				case "server":
					_, err = writer.Exec(ctx, `SELECT id FROM mcp_servers WHERE project_id=$1 AND id=$2 FOR UPDATE NOWAIT`, projectID, serverID)
					require.NoError(t, err)
				}
				commit := identityCommitter(t, ti).Prepare(linkPlan(t, ctx, in.UserSessionIssuerID, in.RemoteSessionIssuerID, in.ClientID))
				var inherited pgx.Tx
				if operation == "reconcile_inherited_tx" {
					inherited, err = ti.conn.Begin(ctx)
					require.NoError(t, err)
					defer func() { _ = inherited.Rollback(context.Background()) }()
				}
				done := make(chan error, 1)
				go func() {
					if operation == "identity_begin" {
						tx, err := commit.Begin(ctx)
						if err == nil {
							err = tx.Rollback(ctx)
						}
						done <- err
					} else if operation == "attach" {
						_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: in.UserSessionIssuerID.String()})
						done <- err
					} else if operation == "reconcile_inherited_tx" {
						err := upstreamclients.Reconcile(ctx, inherited, auth.ActiveOrganizationID, projectID, []uuid.UUID{in.UserSessionIssuerID})
						if err == nil {
							err = inherited.Commit(ctx)
						}
						done <- err
					} else {
						done <- remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, auth.ActiveOrganizationID, projectID, []uuid.UUID{in.UserSessionIssuerID})
					}
				}()
				// No timing sleep decides when to advance. The query itself witnesses the
				// wait edge; an early return is a contract failure, not a timeout success.
				var blockedPID int32
				for blockedPID == 0 {
					select {
					case err := <-done:
						require.NoError(t, err)
						t.Fatal("OAuth entrypoint returned before acquiring the opposing writer's scope")
					default:
					}
					err := ti.conn.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) LIMIT 1`, writerPID).Scan(&blockedPID)
					if err != nil && err != pgx.ErrNoRows {
						require.NoError(t, err)
					}
				}
				var issuerIsNull bool
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT remote_session_issuer_id IS NULL FROM mcp_servers WHERE project_id=$1 AND id=$2`, projectID, serverID).Scan(&issuerIsNull))
				require.True(t, issuerIsNull, "denormalized OAuth write preceded writer scope preflight")
				// Probe downstream row availability while the real OAuth entrypoint is
				// blocked. NOWAIT makes this an early-lock assertion, not an artificial
				// deadlock made by the test acquiring rows in an inverse order.
				_, err = writer.Exec(ctx, `SELECT id FROM remote_session_clients WHERE project_id=$1 AND id=$2 FOR UPDATE NOWAIT`, projectID, in.ClientID)
				require.NoError(t, err)
				_, err = writer.Exec(ctx, `SELECT id FROM mcp_servers WHERE project_id=$1 AND id=$2 FOR UPDATE NOWAIT`, projectID, serverID)
				require.NoError(t, err)
				_, err = writer.Exec(ctx, `SELECT id FROM mcp_auth_clients WHERE organization_id=$1 AND id=ANY($2::uuid[]) FOR UPDATE NOWAIT`, auth.ActiveOrganizationID, []uuid.UUID{oauthID, emaID})
				require.NoError(t, err)
				_, err = writer.Exec(ctx, `SELECT id FROM mcp_auth_client_headers WHERE organization_id=$1 AND auth_client_id=ANY($2::uuid[]) FOR UPDATE NOWAIT`, auth.ActiveOrganizationID, []uuid.UUID{oauthID, emaID})
				require.NoError(t, err, "OAuth header copies must remain unlocked until the full scope is held")
				if gate == "source" {
					_, err = writer.Exec(ctx, `UPDATE remote_mcp_server_headers SET value='updated-test-encrypted-value' WHERE remote_mcp_server_id=$1 AND name='X-Mixed-Lock'`, sourceID)
					require.NoError(t, err)
					require.NoError(t, authclientbackfill.SyncRemoteSource(ctx, writer, auth.ActiveOrganizationID, sourceID))
				}
				require.NoError(t, writer.Commit(ctx))
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(fmt.Errorf("OAuth writer did not finish: %w", ctx.Err()))
				}
				if operation == "reconcile_inherited_tx" {
					var captured int
					require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_server_remote_session_clients WHERE project_id=$1 AND mcp_server_id=$2 AND remote_session_client_id=$3 AND NOT deleted`, projectID, serverID, in.ClientID).Scan(&captured))
					require.Equal(t, 1, captured, "direct reconciliation must capture after acquiring its own preflight scope")
				}
				require.Equal(t, grantsBefore, snapshot(), "configuration synchronization must not change permissions")
				require.Equal(t, emaID, assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource))
				var deleted bool
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT deleted FROM mcp_server_auth_clients WHERE project_id=$1 AND id=$2 AND source='admin'`, projectID, tombstone).Scan(&deleted))
				require.True(t, deleted)
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_server_auth_clients WHERE project_id=$1 AND mcp_server_id=$2 AND auth_client_id=$3 AND NOT deleted`, projectID, detachedServer, emaID).Scan(&liveChoices))
				require.Zero(t, liveChoices, "must not replace an administrator's tombstone")
			})
		}
	}
}
