package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
)

// The normal binding entrypoints must wait for the complete writer scope
// before locking an issuer/client, not acquire that scope during late resync.
func TestClientBindingWriterScopeBeforeIssuerLocks(t *testing.T) {
	for _, operation := range []string{"create", "cimd", "attach", "detach"} {
		for _, gate := range []string{"project", "source", "server"} {
			t.Run(operation+"/"+gate, func(t *testing.T) {
				ctx, ti := newTestService(t)
				ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				auth, _ := contextvalues.GetAuthContext(ctx)
				projectID := *auth.ProjectID
				issuerID := createCIMDIssuer(t, ctx, ti, "binding-lock-provider", "https://idp.example.test/authorize", "https://idp.example.test/token")
				userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "binding-lock-issuer")
				serverID, sourceID := sourceBackedTarget(t, ctx, ti, "binding-lock-server", userIssuerID)
				var clientID string
				if operation == "attach" || operation == "detach" {
					payload := &clientsgen.CreateRemoteSessionClientPayload{RemoteSessionIssuerID: issuerID.String(), ClientID: "binding-lock-client"}
					if operation == "detach" {
						payload.UserSessionIssuerIds = []string{userIssuerID.String()}
					}
					created, err := ti.service.CreateRemoteSessionClient(ctx, payload)
					require.NoError(t, err)
					clientID = created.ID
				}
				blocker, err := ti.conn.Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = blocker.Rollback(context.Background()) }()
				var blockerPID int32
				require.NoError(t, blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID))
				switch gate {
				case "project":
					require.NoError(t, authclientbackfill.New(blocker).LockEMAProjectScope(ctx, authclientbackfill.LockEMAProjectScopeParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: projectID}))
				case "source":
					require.NoError(t, authclientbackfill.LockRemoteSource(ctx, blocker, auth.ActiveOrganizationID, sourceID))
				case "server":
					_, err = blocker.Exec(ctx, "SELECT id FROM mcp_servers WHERE project_id=$1 AND id=$2 FOR UPDATE NOWAIT", projectID, serverID)
					require.NoError(t, err)
				}
				done := make(chan error, 1)
				go func() {
					var err error
					switch operation {
					case "create":
						_, err = ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{RemoteSessionIssuerID: issuerID.String(), ClientID: "binding-lock-new", UserSessionIssuerIds: []string{userIssuerID.String()}})
					case "cimd":
						_, err = ti.service.CreateCimd(ctx, &clientsgen.CreateCimdPayload{RemoteSessionIssuerID: issuerID.String(), UserSessionIssuerIds: []string{userIssuerID.String()}})
					case "attach":
						_, err = ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: clientID, UserSessionIssuerID: userIssuerID.String()})
					case "detach":
						_, err = ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{ID: clientID, UserSessionIssuerID: userIssuerID.String()})
					}
					done <- err
				}()
				var blockedPID int32
				for blockedPID == 0 {
					select {
					case err := <-done:
						require.NoError(t, err)
						t.Fatal("binding writer returned before acquiring its writer scope")
					default:
					}
					err := ti.conn.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) LIMIT 1`, blockerPID).Scan(&blockedPID)
					if err != nil && err != pgx.ErrNoRows {
						require.NoError(t, err)
					}
				}
				// An old post-commit resync also waits here, but only after it
				// has published the binding. Prove no mutation escaped early.
				var bindings int
				require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_session_client_user_session_issuers AS binding
                 JOIN remote_session_clients AS client ON client.id=binding.remote_session_client_id
                 WHERE client.project_id=$1 AND binding.user_session_issuer_id=$2`, projectID, userIssuerID).Scan(&bindings))
				expectedBindings := 0
				if operation == "detach" {
					expectedBindings = 1
				}
				require.Equal(t, expectedBindings, bindings, "binding mutation must follow writer scope acquisition")
				_, err = blocker.Exec(ctx, "SELECT id FROM user_session_issuers WHERE id=$1 FOR UPDATE NOWAIT", userIssuerID)
				require.NoError(t, err, "issuer lock must follow the complete writer scope")
				if clientID != "" {
					_, err = blocker.Exec(ctx, "SELECT id FROM remote_session_clients WHERE project_id=$1 AND id=$2 FOR UPDATE NOWAIT", projectID, uuid.MustParse(clientID))
					require.NoError(t, err, "client lock must follow the complete writer scope")
				}
				require.NoError(t, blocker.Commit(ctx))
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			})
		}
	}
}
