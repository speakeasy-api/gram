package remotesessions_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// A successful binding write must leave usable typed configuration without a
// subsequent binding change, manual reconcile, or migration backfill. A failed
// capture must reject and roll back the binding, permitting a caller retry.
func TestAuthStackE2E_PostCommitBindingResync(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"create", "attach"} {
		for _, injectFailure := range []bool{false, true} {
			name := "normal"
			if injectFailure {
				name = "reconcile_failure"
			}
			t.Run(operation+"/"+name, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestService(t)
				auth, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				require.NotNil(t, auth.ProjectID)
				projectID := *auth.ProjectID
				issuerID := createRemoteIssuer(t, ctx, ti, "resync-upstream", "")
				userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "resync-signin")
				attachRemoteMcpServerToIssuer(t, ctx, ti.conn, projectID, userIssuerID, "resync-server", "https://resync.example.com/mcp")
				var serverID uuid.UUID
				fixtureValue1, err := repo.New(ti.conn).LockingFixtureFindIssuerServer(ctx, repo.LockingFixtureFindIssuerServerParams{ProjectID: projectID, UserIssuerID: userIssuerID})
				require.NoError(t, err)
				serverID = fixtureValue1

				// The external client ID is unique and lets the trigger target create
				// before the service allocates the registration's database ID.
				externalID := "resync-" + uuid.NewString()
				payload := &clientsgen.CreateRemoteSessionClientPayload{
					RemoteSessionIssuerID: issuerID,
					ClientID:              externalID,
				}
				var clientID string
				if operation == "attach" {
					created, err := ti.service.CreateRemoteSessionClient(ctx, payload)
					require.NoError(t, err)
					require.NotNil(t, created)
					clientID = created.ID
				}

				var failureObserved func() bool
				var removeFailure func()
				if injectFailure {
					failureObserved, removeFailure = rejectAuthStackAssociation(t, ctx, ti.conn, projectID, auth.ActiveOrganizationID, serverID, externalID)
				}
				write := func() (*types.RemoteSessionClient, error) {
					if operation == "create" {
						payload.UserSessionIssuerIds = []string{userIssuerID.String()}
						return ti.service.CreateRemoteSessionClient(ctx, payload)
					}
					return ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
						ID: clientID, UserSessionIssuerID: userIssuerID.String(),
					})
				}
				if injectFailure {
					result, err := write()
					require.Error(t, err, "capture failure must not acknowledge a binding commit")
					require.Nil(t, result)
					require.True(t, failureObserved(), "the targeted capture fault must execute")
					var registrations, bindings, associations, typed int
					fixtureValue2, err := repo.New(ti.conn).LockingFixtureCountRegistrations(ctx, repo.LockingFixtureCountRegistrationsParams{ProjectID: projectID, ExternalID: externalID})
					require.NoError(t, err)
					registrations = int(fixtureValue2)
					wantRegistrations := 0
					if operation == "attach" {
						wantRegistrations = 1
					}
					require.Equal(t, wantRegistrations, registrations, "failed create must not persist a registration")
					fixtureValue3, err := repo.New(ti.conn).LockingFixtureCountExternalBindings(ctx, repo.LockingFixtureCountExternalBindingsParams{ProjectID: projectID, ExternalID: externalID, UserIssuerID: userIssuerID})
					require.NoError(t, err)
					bindings = int(fixtureValue3)
					require.Zero(t, bindings, "failed capture must roll back the binding")
					fixtureValue4, err := repo.New(ti.conn).LockingFixtureCountServerLegacyChoices(ctx, repo.LockingFixtureCountServerLegacyChoicesParams{ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID, ServerID: serverID})
					require.NoError(t, err)
					associations = int(fixtureValue4)
					require.Zero(t, associations)
					fixtureValue5, err := repo.New(ti.conn).LockingFixtureCountServerTypedChoices(ctx, repo.LockingFixtureCountServerTypedChoicesParams{ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID, ServerID: serverID})
					require.NoError(t, err)
					typed = int(fixtureValue5)
					require.Zero(t, typed)
					removeFailure()
				}
				result, err := write()
				require.NoError(t, err)
				require.NotNil(t, result)
				clientID = result.ID
				require.Contains(t, result.UserSessionIssuerIds, userIssuerID.String())
				registrationID := uuid.MustParse(clientID)
				require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, registrationID, userIssuerID), "successful publication must persist exactly one binding")
				if injectFailure {
					require.True(t, failureObserved(), "must exercise the scoped database rejection, not an unrelated setup failure")
				}

				var legacy, typed int
				fixtureValue6, err := repo.New(ti.conn).LockingFixtureCountRegistrationLegacyChoices(ctx, repo.LockingFixtureCountRegistrationLegacyChoicesParams{ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID, ServerID: serverID, ClientID: registrationID})
				require.NoError(t, err)
				legacy = int(fixtureValue6)
				fixtureValue7, err := repo.New(ti.conn).LockingFixtureCountEnabledOAuthChoices(ctx, repo.LockingFixtureCountEnabledOAuthChoicesParams{ProjectID: projectID, OrganizationID: auth.ActiveOrganizationID, ServerID: serverID, ClientID: registrationID})
				require.NoError(t, err)
				typed = int(fixtureValue7)
				t.Logf("successful %s: binding=1 legacy_associations=%d typed_associations=%d injected_failure=%t", operation, legacy, typed, injectFailure)
				// This is configuration readiness only. The caller retries the failed
				// database operation, never a separate reconciliation or migration.
				require.Equal(t, 1, typed, "successful binding must leave one enabled typed OAuth association without manual reconciliation")
				require.Equal(t, 1, legacy)
			})
		}
	}
}

// The trigger exists only in this test's cloned database and rejects exactly
// one tenant/project/server/client combination. Sequence increments survive the
// rejected statement's rollback, proving that the intended fault actually ran.
func rejectAuthStackAssociation(t *testing.T, ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, organizationID string, serverID uuid.UUID, externalID string) (observed func() bool, remove func()) {
	t.Helper()
	suffix := uuid.New().String()[:8]
	function := pgx.Identifier{"auth_stack_reject_" + suffix}.Sanitize()
	trigger := pgx.Identifier{"auth_stack_reject_" + suffix}.Sanitize()
	sequence := pgx.Identifier{"auth_stack_fault_" + suffix}.Sanitize()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		//nolint:glint // notestingrawsql: scoped trigger fault injection requires dynamic identifiers.
		_, err := db.Exec(cleanupCtx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON mcp_server_remote_session_clients;
			DROP FUNCTION IF EXISTS %s(); DROP SEQUENCE IF EXISTS %s;`, trigger, function, sequence))
		require.NoError(t, err)
	})
	//nolint:glint // notestingrawsql: scoped trigger fault injection requires dynamic identifiers.
	_, err := db.Exec(ctx, fmt.Sprintf(`CREATE SEQUENCE %s;
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.project_id::text = TG_ARGV[0] AND NEW.organization_id = TG_ARGV[1]
			   AND NEW.mcp_server_id::text = TG_ARGV[2] AND EXISTS (
				SELECT 1 FROM remote_session_clients AS client
				WHERE client.id=NEW.remote_session_client_id
				AND client.project_id=NEW.project_id AND client.client_id=TG_ARGV[3]
			   ) THEN
				PERFORM nextval(TG_ARGV[4]::regclass);
				RAISE EXCEPTION 'injected auth-stack association failure' USING ERRCODE='P0001';
			END IF;
			RETURN NEW;
		END $$;`, sequence, function))
	require.NoError(t, err)
	// Quote literals with PostgreSQL itself rather than interpolating tenant data.
	var args string
	//nolint:glint // notestingrawsql: scoped trigger fault injection requires dynamic identifiers.
	require.NoError(t, db.QueryRow(ctx, `SELECT format('%L, %L, %L, %L, %L', $1::text, $2::text, $3::text, $4::text, $5::text)`, projectID.String(), organizationID, serverID.String(), externalID, sequence).Scan(&args))
	//nolint:glint // notestingrawsql: scoped trigger fault injection requires dynamic identifiers.
	_, err = db.Exec(ctx, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT OR UPDATE ON mcp_server_remote_session_clients
		FOR EACH ROW EXECUTE FUNCTION %s(%s)`, trigger, function, args))
	require.NoError(t, err)
	return func() bool {
			var called bool
			//nolint:glint // notestingrawsql: scoped trigger fault injection requires dynamic identifiers.
			require.NoError(t, db.QueryRow(ctx, "SELECT is_called FROM "+sequence).Scan(&called))
			return called
		}, func() {
			//nolint:glint // notestingrawsql: scoped trigger fault injection requires dynamic identifiers.
			_, err := db.Exec(ctx, fmt.Sprintf("DROP TRIGGER %s ON mcp_server_remote_session_clients", trigger))
			require.NoError(t, err)
		}
}
