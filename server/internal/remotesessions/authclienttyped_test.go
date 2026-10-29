package remotesessions_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// An organization whose upstream OAuth migration already ran keeps setting up
// servers through the ordinary writers. Each association they add has to get
// its typed auth client and server link in the same transaction: the typed
// resolver serves only those, and nothing else records them until another
// backfill run.

// migrateOrganization runs the auth client backfill for the test's
// organization, the state a migrated organization is in.
func migrateOrganization(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	_, err := authclientbackfill.Run(ctx, ti.conn, authclientbackfill.Options{OrganizationID: activeOrganizationID(t, ctx), Apply: true, PageSize: 0})
	require.NoError(t, err)
}

// requireTypedClient checks the registration's backfilled personal client
// exists and the server offers it, returning its id.
func requireTypedClient(t *testing.T, ctx context.Context, ti *testInstance, registration, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	orgID := activeOrganizationID(t, ctx)
	want := authclientbackfill.OAuthClientID(orgID, registration, authclientbackfill.TypeRemoteSession)
	state, err := authclientbackfill.New(ti.conn).GetAuthClientFixture(ctx, want)
	require.NoError(t, err, "the registration has no typed client")
	require.Equal(t, authclientbackfill.TypeRemoteSession, state.ClientType)
	require.False(t, state.Deleted)
	links, err := authclientbackfill.New(ti.conn).ListServerAuthClientsFixture(ctx, serverID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{want}, links, "the server does not offer the typed client")
	return want
}

// requireReady checks a dry run of the backfill finds nothing left to record.
func requireReady(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	dry, err := authclientbackfill.Run(ctx, ti.conn, authclientbackfill.Options{OrganizationID: activeOrganizationID(t, ctx), Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.Empty(t, dry.PhaseErrors)
	require.Zero(t, dry.PendingClients, "a dry run still plans a client")
	require.Zero(t, dry.PendingLinks, "a dry run still plans a link")
	require.Zero(t, dry.PendingHeaders, "a dry run still plans a header copy")
	require.Zero(t, dry.Gaps)
	require.Zero(t, dry.PendingSelectionMappings, "a choice still needs its typed client named")
}

// recordSelectionFixture exercises the selection repository query with a synthetic
// credential ID. It does not mint a credential or exercise request enforcement.
func recordSelectionFixture(t *testing.T, ctx context.Context, ti *testInstance, userIssuerID, serverID uuid.UUID) uuid.NullUUID {
	t.Helper()
	credentialID := uuid.New()
	_, err := usersessionsrepo.New(ti.conn).RecordUpstreamClientSelections(ctx, usersessionsrepo.RecordUpstreamClientSelectionsParams{
		UserSessionIssuerID: userIssuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        credentialID,
		Source:              upstreamclients.SelectionSourceIssuance,
		PickedMcpServerIds:  []uuid.UUID{},
		PickedClientIds:     []uuid.UUID{},
		PickedAuthClientIds: nil,
	})
	require.NoError(t, err)
	choice, err := authclientbackfill.New(ti.conn).GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{
		CredentialID: credentialID,
		McpServerID:  serverID,
	})
	require.NoError(t, err, "the selection query recorded no choice for the server")
	return choice.AuthClientID
}

func TestCommitServerIdentityConfigurationRecordsTheTypedClientAfterMigration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	migrateOrganization(t, ctx, ti)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "typed-commit-issuer")
	target, source := sourceBackedTarget(t, ctx, ti, "typed-commit-target", userIssuerID)
	seedAuxiliaryHeader(t, ctx, ti, source, "X-Tenant-Key")
	providerID := createServerIdentityProvider(t, ctx, ti, "typed-commit-provider", "", false, []string{"none"})

	// The ordinary setup: a new personal registration for the server.
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(target, providerID, "typed-commit-client"))
	require.NoError(t, err)
	registration, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	authClientID := requireTypedClient(t, ctx, ti, registration, target)
	require.Equal(t, []string{"X-Tenant-Key"}, copiedHeaderNames(t, ctx, ti, authClientID),
		"the source's credential header is not carried by the typed client")

	require.Equal(t, uuid.NullUUID{UUID: authClientID, Valid: true}, recordSelectionFixture(t, ctx, ti, userIssuerID, target),
		"the selection query records the choice without its typed client")
	requireReady(t, ctx, ti)

	// Configuring the same client again is idempotent.
	_, err = ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(target, providerID, registration))
	require.NoError(t, err)
	require.Equal(t, authClientID, requireTypedClient(t, ctx, ti, registration, target))
	requireReady(t, ctx, ti)
}

// Binding a client to a server's user session issuer associates it with the
// server when the server is the issuer's only consumer; that capture records
// the typed client too, from the post-commit resync's own transaction.
func TestResyncCaptureRecordsTheTypedClientAfterMigration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	migrateOrganization(t, ctx, ti)
	projectID := projectIDFromContext(t, ctx)
	orgID := activeOrganizationID(t, ctx)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "typed-capture-issuer")
	target, _ := sourceBackedTarget(t, ctx, ti, "typed-capture-target", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "typed-capture-provider", "", false, []string{"none"})
	registration := createServerIdentityClient(t, ctx, ti, "typed-capture-client", providerID, false)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: registration,
		UserSessionIssuerID:   userIssuerID,
	}))

	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{userIssuerID}))

	require.Equal(t, []uuid.UUID{registration}, associatedClients(t, ctx, ti.conn, projectID, target))
	authClientID := requireTypedClient(t, ctx, ti, registration, target)
	require.Equal(t, uuid.NullUUID{UUID: authClientID, Valid: true}, recordSelectionFixture(t, ctx, ti, userIssuerID, target))
	requireReady(t, ctx, ti)
}

// Another organization's servers are never named by a write in this one.
func TestCommitServerIdentityConfigurationRecordsNothingForAnotherServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	migrateOrganization(t, ctx, ti)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "typed-scope-issuer")
	target, _ := sourceBackedTarget(t, ctx, ti, "typed-scope-target", userIssuerID)
	bystander, _ := sourceBackedTarget(t, ctx, ti, "typed-scope-bystander", createUserSessionIssuer(t, ctx, ti.conn, "typed-scope-other"))
	providerID := createServerIdentityProvider(t, ctx, ti, "typed-scope-provider", "", false, []string{"none"})

	_, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(target, providerID, "typed-scope-client"))
	require.NoError(t, err)

	links, err := authclientbackfill.New(ti.conn).ListServerAuthClientsFixture(ctx, bystander)
	require.NoError(t, err)
	require.Empty(t, links, "a server the write never touched offers the new client")
}

// Intercept only the conditional link write; all capture, client creation,
// savepoints and transaction completion still execute against PostgreSQL.
type failedAssociationPool struct {
	*pgxpool.Pool
	linkErr  error
	attempts int
}

func (p *failedAssociationPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failedAssociationTx{Tx: tx, pool: p}, nil
}

type failedAssociationTx struct {
	pgx.Tx
	pool *failedAssociationPool
}

func (tx *failedAssociationTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(sql, "-- name: LinkAuthClientToServer ") {
		tx.pool.attempts++
		return pgconn.NewCommandTag("INSERT 0 0"), tx.pool.linkErr
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func TestReconcileCaptureRollsBackWhenTypedStateFails(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"exhausted race", "write failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			migrateOrganization(t, ctx, ti)
			projectID := projectIDFromContext(t, ctx)
			orgID := activeOrganizationID(t, ctx)
			issuer := createUserSessionIssuer(t, ctx, ti.conn, "rollback-issuer")
			target, _ := sourceBackedTarget(t, ctx, ti, "rollback-target", issuer)
			provider := createServerIdentityProvider(t, ctx, ti, "rollback-provider", "", false, []string{"none"})
			registration := createServerIdentityClient(t, ctx, ti, "rollback-client", provider, false)
			require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
				RemoteSessionClientID: registration, UserSessionIssuerID: issuer,
			}))
			pool := &failedAssociationPool{Pool: ti.conn}
			if name == "write failure" {
				pool.linkErr = errors.New("injected link failure")
			}
			err := upstreamclients.Reconcile(ctx, pool, orgID, projectID, []uuid.UUID{issuer})
			require.Error(t, err)
			if pool.linkErr != nil {
				require.ErrorIs(t, err, pool.linkErr)
				require.Equal(t, 1, pool.attempts)
			} else {
				var retryable *oops.ShareableError
				require.ErrorAs(t, err, &retryable)
				require.True(t, retryable.IsTemporary(ctx))
				requireOopsCode(t, err, oops.CodeUnavailable)
				require.Equal(t, 2, pool.attempts, "both bounded link attempts must race")
			}
			require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, target), "capture must roll back")
			clientID := authclientbackfill.OAuthClientID(orgID, registration, authclientbackfill.TypeRemoteSession)
			_, err = authclientbackfill.New(ti.conn).GetAuthClientFixture(ctx, clientID)
			require.ErrorIs(t, err, pgx.ErrNoRows, "typed client inserted before the failed link must roll back")
			// Capture uses ON CONFLICT DO NOTHING RETURNING. If the failed capture
			// had committed, this retry would not synchronize that association again.
			require.NoError(t, upstreamclients.Reconcile(ctx, ti.conn, orgID, projectID, []uuid.UUID{issuer}))
			require.Equal(t, []uuid.UUID{registration}, associatedClients(t, ctx, ti.conn, projectID, target))
			require.Equal(t, clientID, requireTypedClient(t, ctx, ti, registration, target))
			requireReady(t, ctx, ti)
		})
	}
}
