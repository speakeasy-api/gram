package remotesessions_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
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

func TestPreparationScope_DetachRevalidatesOwnershipAfterLockWait(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"client", "user issuer"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti, in := preparationFixture(t)
			auth, _ := contextvalues.GetAuthContext(ctx)
			sibling := createProject(t, ctx, ti.conn, "detach-race-sibling")
			q := repo.New(ti.conn)
			require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: in.ClientID, UserSessionIssuerID: in.UserSessionIssuerID}))
			tx := testenv.BeginTx(t, ctx, ti.conn)
			var err error
			tq := repo.New(tx)
			pattern := "%LockEMAClient :one%"
			if kind == "client" {
				_, err = tq.LockRemoteSessionClientForSessionWrite(ctx, in.ClientID)
			} else {
				pattern = "%LockProjectUserIssuerForDetach :one%"
				_, err = tq.LockProjectUserIssuerForDetach(ctx, repo.LockProjectUserIssuerForDetachParams{ID: in.UserSessionIssuerID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
			}
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				_, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: in.UserSessionIssuerID.String()})
				done <- err
			}()
			require.Eventually(t, func() bool {
				blocked, err := testrepo.New(ti.conn).IsQueryBlockedOnLockFixture(ctx, pattern)
				return err == nil && blocked
			}, 5*time.Second, 10*time.Millisecond, "detach must reach the lifecycle lock before ownership changes")
			if kind == "client" {
				_, err = testrepo.New(tx).MovePreparationFixtureClientProject(ctx, testrepo.MovePreparationFixtureClientProjectParams{ID: in.ClientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), TargetProjectID: conv.ToNullUUID(sibling)})
			} else {
				_, err = testrepo.New(tx).MovePreparationFixtureUserIssuerProject(ctx, testrepo.MovePreparationFixtureUserIssuerProjectParams{ID: in.UserSessionIssuerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), TargetProjectID: conv.ToNullUUID(sibling)})
			}
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				requireOopsCode(t, err, oops.CodeNotFound)
			case <-time.After(5 * time.Second):
				t.Fatal("detach did not finish after ownership changed")
			}
			require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, in.ClientID, in.UserSessionIssuerID), "stale authorization must not delete the join")
		})
	}
}

func TestPreparationScope_DetachInheritedClientIgnoresSiblingEMABinding(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := repo.New(ti.conn)
	sibling := createProject(t, ctx, ti.conn, "inherited-ema-sibling")
	user := createUserSessionIssuerInProject(t, ctx, ti.conn, sibling, "sibling-human")
	issuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "inherited-ema")
	client := seedOrgLevelRemoteClient(t, ctx, ti.conn, auth.ActiveOrganizationID, issuer, "inherited-ema-client", in.UserSessionIssuerID)
	require.NoError(t, q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams{ProjectID: sibling, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, Resource: in.Resource}))
	binding, err := q.GetEMABinding(ctx, repo.GetEMABindingParams{ProjectID: sibling, OrganizationID: auth.ActiveOrganizationID, UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, Resource: in.Resource})
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, repo.SetEMABindingParams{ID: binding.ID, ProjectID: sibling, OrganizationID: auth.ActiveOrganizationID, Generation: binding.Generation + 1, ExpectedGeneration: binding.Generation, State: conv.ToPGText("unknown_grants"), GrantSource: conv.ToPGText("unknown"), RemoteSessionClientID: conv.ToNullUUID(client), RequestedScopes: []string{}})
	require.NoError(t, err)
	_, err = ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{ID: client.String(), UserSessionIssuerID: in.UserSessionIssuerID.String()})
	require.NoError(t, err, "project-local join mutation must not be blocked by another project's inherited EMA binding")
	count, err := q.CountActiveEMABindingsForClient(ctx, repo.CountActiveEMABindingsForClientParams{ClientID: conv.ToNullUUID(client), ProjectID: sibling, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	// The inherited client is valid, but it must not pass an exact project-owned
	// lifecycle lock intended for provider-wide configuration changes.
	_, err = q.LockEMAClientForLifecycle(ctx, repo.LockEMAClientForLifecycleParams{ID: client, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.LockEMAClientForLifecycle(ctx, repo.LockEMAClientForLifecycleParams{ID: client, ProjectID: uuid.Nil, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
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
			require.Eventually(t, func() bool {
				blocked, err := testrepo.New(ti.conn).IsQueryBlockedOnLockFixture(ctx, "%LockEMA% :one%")
				return err == nil && blocked
			}, 5*time.Second, 10*time.Millisecond)
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
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(ti.conn).IsQueryBlockedOnLockFixture(ctx, "%LockEMAProject :one%")
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
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
