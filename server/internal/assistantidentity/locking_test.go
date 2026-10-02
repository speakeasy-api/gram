package assistantidentity_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestProvisionRejectsConcurrentRootMutationWithoutPartialAuthority(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"pause", "retarget", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			tx := testenv.BeginTx(t, t.Context(), f.db)
			q := repo.New(tx)
			_, err := q.LockTrigger(t.Context(), repo.LockTriggerParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger})
			require.NoError(t, err)
			before, err := repo.New(f.db).FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			err = inTx(t, f.db, func(provision pgx.Tx) error {
				_, err := testIdentityService.Provision(t.Context(), provision, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
				if err != nil {
					return fmt.Errorf("provision contended root: %w", err)
				}
				return nil
			})
			var locked *pgconn.PgError
			require.ErrorAs(t, err, &locked)
			require.Equal(t, "55P03", locked.Code)
			after, err := repo.New(f.db).FixtureAuthorityCounts(t.Context(), f.org)
			require.NoError(t, err)
			require.Equal(t, before, after)
			switch mutation {
			case "pause":
				err = q.FixtureSetTriggerStatus(t.Context(), repo.FixtureSetTriggerStatusParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger, Status: "paused"})
			case "retarget":
				err = q.FixtureRetargetTrigger(t.Context(), repo.FixtureRetargetTriggerParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger, TargetRef: "invalid-target"})
			case "delete":
				err = q.FixtureDeleteTrigger(t.Context(), repo.FixtureDeleteTriggerParams{OrganizationID: f.org, ProjectID: f.project, TriggerID: f.trigger})
			}
			require.NoError(t, err)
			require.NoError(t, tx.Commit(t.Context()))
			// The next attempt observes the committed mutation, not the old root snapshot.
			err = inTx(t, f.db, func(bind pgx.Tx) error {
				return testIdentityService.BindRootTrigger(t.Context(), bind, f.org, f.project, f.trigger)
			})
			require.Error(t, err)
		})
	}
}

func TestProjectLivenessLockIsSharedAndRejectsDeletion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := testenv.BeginTx(t, t.Context(), f.db)
	second := testenv.BeginTx(t, t.Context(), f.db)
	require.NoError(t, assistantidentity.LockLiveProject(t.Context(), first, f.org, f.project))
	require.NoError(t, assistantidentity.LockLiveProject(t.Context(), second, f.org, f.project), "independent provisioners must share the liveness lock")
	require.NoError(t, first.Rollback(t.Context()))
	require.NoError(t, second.Rollback(t.Context()))
	deleting := testenv.BeginTx(t, t.Context(), f.db)
	_, err := projectsrepo.New(deleting).LockProjectForEMADeletion(t.Context(), projectsrepo.LockProjectForEMADeletionParams{OrganizationID: f.org, ProjectID: f.project})
	require.NoError(t, err)
	err = inTx(t, f.db, func(tx pgx.Tx) error { return assistantidentity.LockLiveProject(t.Context(), tx, f.org, f.project) })
	var locked *pgconn.PgError
	require.ErrorAs(t, err, &locked)
	require.Equal(t, "55P03", locked.Code)
	_, err = projectsrepo.New(deleting).DeleteProject(t.Context(), f.project)
	require.NoError(t, err)
	require.NoError(t, deleting.Commit(t.Context()))
	err = inTx(t, f.db, func(tx pgx.Tx) error {
		_, err := testIdentityService.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: f.org, ProjectID: f.project, AssistantID: f.assistant, ActorUserID: f.actor})
		if err != nil {
			return fmt.Errorf("provision deleted project: %w", err)
		}
		return nil
	})
	require.ErrorIs(t, err, assistantidentity.ErrNotFound)
	counts, err := repo.New(f.db).FixtureAuthorityCounts(t.Context(), f.org)
	require.NoError(t, err)
	require.Zero(t, counts.Assistants)
}

func TestProvisionRejectsInvalidProjectIdentityBeforeQuery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		org     string
		project uuid.UUID
	}{
		{name: "empty organization", project: uuid.New()},
		{name: "nil project", org: "test-org"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A nil transaction ensures invalid input never reaches the database.
			_, err := testIdentityService.Provision(t.Context(), nil, assistantidentity.ProvisionParams{OrganizationID: tc.org, ProjectID: tc.project, AssistantID: uuid.New(), ActorUserID: "test-actor"})
			require.ErrorIs(t, err, assistantidentity.ErrInvalidIdentity)
		})
	}
}
