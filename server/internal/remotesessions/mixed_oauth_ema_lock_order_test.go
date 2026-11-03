package remotesessions_test

import (
	"context"
	"errors"
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
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// Use actual PostgreSQL wait edges as barriers. In particular, observing an
// OAuth writer waiting for a source is not enough: a late preflight can already
// own the server or registration that the source writer needs next.
func TestMixedOAuthEMA_OpposingWriterScope(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"identity_begin", "attach", "resync", "reconcile_inherited_tx"} {
		for _, gate := range []string{"project", "source", "server"} {
			t.Run(operation+"/"+gate, func(t *testing.T) {
				t.Parallel()
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
				fixtureValue1, err := repo.New(ti.conn).LockingFixtureFindOAuthClient(ctx, repo.LockingFixtureFindOAuthClientParams{OrganizationID: auth.ActiveOrganizationID, ClientID: in.ClientID})
				require.NoError(t, err)
				oauthID = fixtureValue1
				require.NotEqual(t, oauthID, emaID)
				fixtureValue2, err := repo.New(ti.conn).LockingFixtureFindRemoteSource(ctx, repo.LockingFixtureFindRemoteSourceParams{ProjectID: projectID, ServerID: serverID})
				require.NoError(t, err)
				sourceID = fixtureValue2.UUID
				var liveChoices int
				fixtureValue3, err := repo.New(ti.conn).LockingFixtureCountLiveChoices(ctx, repo.LockingFixtureCountLiveChoicesParams{ProjectID: projectID, ServerID: serverID, AuthClientIds: []uuid.UUID{oauthID, emaID}})
				require.NoError(t, err)
				liveChoices = int(fixtureValue3)
				require.Equal(t, 2, liveChoices, "OAuth and EMA must share the same registration and server reach")

				// Preserve an administrator's removal on a second server, while the first
				// server still offers both choices and participates in the opposing write.
				detachedServer := seedRemoteMCPServerForIssuer(t, ctx, ti, in.UserSessionIssuerID, "mixed-detached", in.Resource)
				_, err = ti.service.PrepareIdentityChaining(ctx, in)
				require.NoError(t, err)
				var tombstone uuid.UUID
				fixtureValue4, err := repo.New(ti.conn).LockingFixtureTombstoneChoice(ctx, repo.LockingFixtureTombstoneChoiceParams{ProjectID: projectID, ServerID: detachedServer, AuthClientID: emaID})
				require.NoError(t, err)
				tombstone = fixtureValue4
				err = repo.New(ti.conn).LockingFixtureInsertGrant(ctx, repo.LockingFixtureInsertGrantParams{OrganizationID: auth.ActiveOrganizationID, ServerID: serverID.String(), ProjectID: projectID})
				require.NoError(t, err)
				snapshot := func() string {
					var result string
					fixtureValue5, err := repo.New(ti.conn).LockingFixtureSnapshotGrants(ctx, auth.ActiveOrganizationID)
					require.NoError(t, err)
					result = fixtureValue5
					return result
				}
				grantsBefore := snapshot()
				// Populate real OAuth and EMA header copies before contention, rather
				// than testing an empty header-copy lock set.
				setup := testenv.BeginTx(t, ctx, ti.conn)
				err = repo.New(setup).LockingFixtureInsertSourceHeader(ctx, repo.LockingFixtureInsertSourceHeaderParams{SourceID: sourceID, ProjectID: projectID})
				require.NoError(t, err)
				require.NoError(t, authclientbackfill.SyncRemoteSource(ctx, setup, auth.ActiveOrganizationID, sourceID))
				require.NoError(t, setup.Commit(ctx))
				var copies int
				fixtureValue6, err := repo.New(ti.conn).LockingFixtureCountHeaderCopies(ctx, repo.LockingFixtureCountHeaderCopiesParams{OrganizationID: auth.ActiveOrganizationID, AuthClientIds: []uuid.UUID{oauthID, emaID}})
				require.NoError(t, err)
				copies = int(fixtureValue6)
				require.Equal(t, 2, copies)
				if operation == "reconcile_inherited_tx" {
					// Restore sole-consumer evidence and remove only the legacy association.
					// Reconcile must really capture a missing row, not take its no-op path.
					err = repo.New(ti.conn).LockingFixtureClearUserIssuer(ctx, repo.LockingFixtureClearUserIssuerParams{ProjectID: projectID, ServerID: detachedServer})
					require.NoError(t, err)
					deleted, err := repo.New(ti.conn).LockingFixtureDeleteLegacyChoice(ctx, repo.LockingFixtureDeleteLegacyChoiceParams{ProjectID: projectID, ServerID: serverID, ClientID: in.ClientID})
					require.NoError(t, err)
					require.EqualValues(t, 1, deleted)
				}
				// Make resync's denormalized UPDATE observable; it must not happen before
				// the project/source/server preflight, even when called with a pool.
				err = repo.New(ti.conn).LockingFixtureClearRemoteIssuer(ctx, repo.LockingFixtureClearRemoteIssuerParams{ProjectID: projectID, ServerID: serverID})
				require.NoError(t, err)
				writer := testenv.BeginTx(t, ctx, ti.conn)
				writerPID := int32(writer.Conn().PgConn().PID())
				switch gate {
				case "project":
					require.NoError(t, authclientbackfill.New(writer).LockEMAProjectScope(ctx, authclientbackfill.LockEMAProjectScopeParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: projectID}))
				case "source":
					require.NoError(t, authclientbackfill.LockRemoteSource(ctx, writer, auth.ActiveOrganizationID, sourceID))
				case "server":
					err = repo.New(writer).LockingFixtureLockServer(ctx, repo.LockingFixtureLockServerParams{ProjectID: projectID, ServerID: serverID})
					require.NoError(t, err)
				}
				commit := identityCommitter(t, ti).Prepare(linkPlan(t, ctx, in.UserSessionIssuerID, in.RemoteSessionIssuerID, in.ClientID))
				var inherited pgx.Tx
				if operation == "reconcile_inherited_tx" {
					inherited = testenv.BeginTx(t, ctx, ti.conn)
				}
				done := make(chan error, 1)
				go func() {
					switch operation {
					case "identity_begin":
						tx, err := commit.Begin(ctx)
						if err == nil {
							err = tx.Rollback(ctx)
						}
						done <- err
					case "attach":
						_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: in.UserSessionIssuerID.String()})
						done <- err
					case "reconcile_inherited_tx":
						err := upstreamclients.Reconcile(ctx, inherited, auth.ActiveOrganizationID, projectID, []uuid.UUID{in.UserSessionIssuerID})
						if err == nil {
							err = inherited.Commit(ctx)
						}
						done <- err
					default:
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
					//nolint:glint // notestingrawsql: observe the live PostgreSQL backend wait edge.
					err := ti.conn.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) LIMIT 1`, writerPID).Scan(&blockedPID)
					if err != nil && !errors.Is(err, pgx.ErrNoRows) {
						require.NoError(t, err)
					}
				}
				var issuerIsNull bool
				fixtureValue7, err := repo.New(ti.conn).LockingFixtureIsRemoteIssuerNull(ctx, repo.LockingFixtureIsRemoteIssuerNullParams{ProjectID: projectID, ServerID: serverID})
				require.NoError(t, err)
				issuerIsNull = fixtureValue7
				require.True(t, issuerIsNull, "denormalized OAuth write preceded writer scope preflight")
				// Probe downstream row availability while the real OAuth entrypoint is
				// blocked. NOWAIT makes this an early-lock assertion, not an artificial
				// deadlock made by the test acquiring rows in an inverse order.
				err = repo.New(writer).LockingFixtureLockRegistration(ctx, repo.LockingFixtureLockRegistrationParams{ProjectID: projectID, ClientID: in.ClientID})
				require.NoError(t, err)
				err = repo.New(writer).LockingFixtureLockServer(ctx, repo.LockingFixtureLockServerParams{ProjectID: projectID, ServerID: serverID})
				require.NoError(t, err)
				err = repo.New(writer).LockingFixtureLockAuthClients(ctx, repo.LockingFixtureLockAuthClientsParams{OrganizationID: auth.ActiveOrganizationID, AuthClientIds: []uuid.UUID{oauthID, emaID}})
				require.NoError(t, err)
				err = repo.New(writer).LockingFixtureLockAuthHeaders(ctx, repo.LockingFixtureLockAuthHeadersParams{OrganizationID: auth.ActiveOrganizationID, AuthClientIds: []uuid.UUID{oauthID, emaID}})
				require.NoError(t, err, "OAuth header copies must remain unlocked until the full scope is held")
				if gate == "source" {
					err = repo.New(writer).LockingFixtureUpdateSourceHeader(ctx, repo.LockingFixtureUpdateSourceHeaderParams{SourceID: sourceID, ProjectID: projectID})
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
					fixtureValue8, err := repo.New(ti.conn).LockingFixtureCountLegacyChoice(ctx, repo.LockingFixtureCountLegacyChoiceParams{ProjectID: projectID, ServerID: serverID, ClientID: in.ClientID})
					require.NoError(t, err)
					captured = int(fixtureValue8)
					require.Equal(t, 1, captured, "direct reconciliation must capture after acquiring its own preflight scope")
				}
				require.Equal(t, grantsBefore, snapshot(), "configuration synchronization must not change permissions")
				require.Equal(t, emaID, assertPreparationTypedClient(t, ctx, ti, in.ClientID, in.Resource))
				var deleted bool
				fixtureValue9, err := repo.New(ti.conn).LockingFixtureReadTombstone(ctx, repo.LockingFixtureReadTombstoneParams{ProjectID: projectID, ID: tombstone})
				require.NoError(t, err)
				deleted = fixtureValue9
				require.True(t, deleted)
				fixtureValue10, err := repo.New(ti.conn).LockingFixtureCountLiveChoice(ctx, repo.LockingFixtureCountLiveChoiceParams{ProjectID: projectID, ServerID: detachedServer, AuthClientID: emaID})
				require.NoError(t, err)
				liveChoices = int(fixtureValue10)
				require.Zero(t, liveChoices, "must not replace an administrator's tombstone")
			})
		}
	}
}
