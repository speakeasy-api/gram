package remotesessions_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func TestPreparationScope_ProjectLocksRequireOwningOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	for _, org := range []string{"foreign-organization", auth.ActiveOrganizationID} {
		_, clientErr := q.LockEMAClient(ctx, repo.LockEMAClientParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(org)})
		_, issuerErr := q.LockEMAIssuer(ctx, repo.LockEMAIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(org)})
		_, userErr := q.LockEMAUserIssuer(ctx, repo.LockEMAUserIssuerParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(org)})
		for _, err := range []error{clientErr, issuerErr, userErr} {
			if org == auth.ActiveOrganizationID {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
		}
	}
	client, err := q.LockEMAClient(ctx, repo.LockEMAClientParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID)})
	require.NoError(t, err)
	require.False(t, client.OrganizationID.Valid, "legacy project client must remain supported via its project's organization")
}

func TestPreparationScope_LocksRevalidateProjectAfterWait(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"client", "remote issuer", "user issuer"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			sibling := createProject(t, ctx, ti.conn, "lock-race-sibling")
			lock := func(q *repo.Queries) error {
				var err error
				switch kind {
				case "client":
					_, err = q.LockEMAClient(ctx, repo.LockEMAClientParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID)})
				case "remote issuer":
					_, err = q.LockEMAIssuer(ctx, repo.LockEMAIssuerParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID)})
				case "user issuer":
					_, err = q.LockEMAUserIssuer(ctx, repo.LockEMAUserIssuerParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID)})
				}
				if err != nil {
					return fmt.Errorf("lock EMA fixture: %w", err)
				}
				return nil
			}
			tx := testenv.BeginTx(t, ctx, ti.conn)
			var err error
			tq := repo.New(tx)
			require.NoError(t, lock(tq))
			done := make(chan error, 1)
			go func() { done <- lock(repo.New(ti.conn)) }()
			testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), "%LockEMA% :one%")
			switch kind {
			case "client":
				_, err = testrepo.New(tx).MovePreparationFixtureClientProject(ctx, testrepo.MovePreparationFixtureClientProjectParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), TargetProjectID: conv.ToNullUUID(sibling)})
			case "remote issuer":
				_, err = testrepo.New(tx).MovePreparationFixtureRemoteIssuerProject(ctx, testrepo.MovePreparationFixtureRemoteIssuerProjectParams{ID: in.RemoteSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), TargetProjectID: conv.ToNullUUID(sibling)})
			case "user issuer":
				_, err = testrepo.New(tx).MovePreparationFixtureUserIssuerProject(ctx, testrepo.MovePreparationFixtureUserIssuerProjectParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), TargetProjectID: conv.ToNullUUID(sibling)})
			}
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				require.ErrorIs(t, err, pgx.ErrNoRows)
			case <-time.After(5 * time.Second):
				t.Fatal("scoped lock did not finish")
			}
		})
	}
}

// Even an entirely inherited configuration must serialize with its consuming
// project's lifecycle; none of its parent queries otherwise needs that row.
func TestPreparationScope_FirstBindingWaitsForProjectDeletion(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	in.RemoteSessionIssuerID = seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "project-race-issuer")
	in.UserSessionIssuerID = createTrustedOrganizationTierUserSessionIssuerForOrganization(t, ctx, ti.conn, auth.ActiveOrganizationID, "project-race-user", in.RemoteSessionIssuerID)
	in.ClientID = seedOrgLevelRemoteClient(t, ctx, ti.conn, auth.ActiveOrganizationID, in.RemoteSessionIssuerID, "project-race-client")
	tx := testenv.BeginTx(t, ctx, ti.conn)
	q := projectsrepo.New(tx)
	_, err := q.LockProjectForEMADeletion(ctx, projectsrepo.LockProjectForEMADeletionParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := ti.service.PrepareIdentityChaining(ctx, in); done <- err }()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), "%LockEMAProject :one%")
	_, err = q.DeleteProject(ctx, *auth.ProjectID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	select {
	case err := <-done:
		requireOopsCode(t, err, oops.CodeNotFound)
	case <-time.After(5 * time.Second):
		t.Fatal("preparation did not revalidate deleted project")
	}
	count, err := testrepo.New(ti.conn).CountPreparationFixtureBindings(ctx, *auth.ProjectID)
	require.NoError(t, err)
	require.Zero(t, count)
}
