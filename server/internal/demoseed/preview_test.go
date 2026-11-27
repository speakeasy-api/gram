//go:build demoseed_safety

package demoseed

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// A preview reset is the seed run with no ClickHouse connection, followed by
// the preview fixtures. Run twice beside another tenant, it leaves one
// non-staff runner whose only membership is the preview's organization, and
// the other tenant as it was.
func TestPreviewFixturesAdoptOnlyTheRunner(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec, err := PreviewSpec(123)
	require.NoError(t, err)
	runner := spec.PreviewRunner()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	// The neighbour: a developer's organization, whose fixtures also create the
	// built-in roles a preview's database has from its environment.
	neighbour := LocalSpec()
	seedLocalPostgres(ctx, t, db, neighbour)
	require.NoError(t, RunLocalFixtures(ctx, logger, db, assetstest.NewTestBlobStore(t), nil, LocalFixturesOptions{DeveloperEmail: testDeveloperEmail, Environment: "local"}))
	neighbourBefore, err := demoseedtest.ReadDeveloperState(ctx, db, neighbour.OrgID, testDeveloperEmail)
	require.NoError(t, err)
	require.Equal(t, 1, neighbourBefore.Memberships)

	for range 2 {
		// No ClickHouse connection: the seed must not reach for one.
		require.NoError(t, Run(ctx, logger, db, nil, assetstest.NewTestBlobStore(t), testenv.NewEncryptionClient(t), testAssistantIssuerURL, testServerURL, spec))
		require.NoError(t, RunPreviewFixtures(ctx, logger, db, nil, spec))
	}

	state, err := demoseedtest.ReadDeveloperState(ctx, db, spec.OrgID, runner.Email)
	require.NoError(t, err)
	require.Equal(t, demoseedtest.DeveloperState{
		Users:           1,
		UserID:          runner.ID,
		WorkOSID:        "workos_" + runner.ID,
		Whitelisted:     state.Whitelisted,
		OrgWorkOSID:     "", // no identity provider organization stands behind it
		Memberships:     1,
		RoleAssignments: 1,
	}, state)

	user, err := userrepo.New(db).GetUserByEmail(ctx, runner.Email)
	require.NoError(t, err)
	require.False(t, user.Admin, "the runner is never platform staff")
	organizations, err := orgrepo.New(db).ListOrganizationsForUser(ctx, conv.ToPGText(runner.ID))
	require.NoError(t, err)
	require.Len(t, organizations, 1)
	require.Equal(t, spec.OrgID, organizations[0].ID)

	neighbourAfter, err := demoseedtest.ReadDeveloperState(ctx, db, neighbour.OrgID, testDeveloperEmail)
	require.NoError(t, err)
	require.Equal(t, neighbourBefore, neighbourAfter)
}

// The seed writes a tool block on the global Admin role before the fixtures
// run, so the fixtures must still give Admin its default grants: otherwise
// the runner, an Admin, cannot even read the organization's project.
func TestPreviewFixturesLetTheRunnerReadTheProject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec, err := PreviewSpec(127)
	require.NoError(t, err)
	runner := spec.PreviewRunner()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	// The built-in roles exist before the first reset, as in a preview's
	// database cloned from its environment.
	seedLocalPostgres(ctx, t, db, LocalSpec())
	require.NoError(t, RunLocalFixtures(ctx, logger, db, assetstest.NewTestBlobStore(t), nil, LocalFixturesOptions{DeveloperEmail: testDeveloperEmail, Environment: "local"}))

	require.NoError(t, Run(ctx, logger, db, nil, assetstest.NewTestBlobStore(t), testenv.NewEncryptionClient(t), testAssistantIssuerURL, testServerURL, spec))
	require.NoError(t, RunPreviewFixtures(ctx, logger, db, nil, spec))

	principals, err := authz.ResolveUserPrincipals(ctx, db, spec.OrgID, runner.ID)
	require.NoError(t, err)
	grants, err := authz.LoadGrants(ctx, db, spec.OrgID, principals)
	require.NoError(t, err)
	projectID := uuid.MustParse(spec.ProjectID())
	require.True(t, authz.GrantsSatisfy(grants, authz.Project(authz.ProjectIDOf(projectID)).Read()), "the runner can read the project")
	require.True(t, authz.GrantsSatisfy(grants, authz.Project(authz.ProjectIDOf(projectID)).Write()), "the runner, an Admin, can change the project")
}

// Creating a built-in role would reach every organization in the database, so
// a database without them is refused, and nobody can sign in to the half-made
// organization: the runner is written last.
func TestPreviewFixturesRefuseADatabaseWithoutBuiltInRoles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	spec, err := PreviewSpec(124)
	require.NoError(t, err)

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	seedLocalPostgres(ctx, t, db, spec)

	require.ErrorContains(t, RunPreviewFixtures(ctx, testenv.NewLogger(t), db, nil, spec), "built-in role")
	_, err = userrepo.New(db).GetUserByEmail(ctx, spec.PreviewRunner().Email)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// The runner's id already belonging to someone is a refusal too: their row is
// not rewritten into the runner.
func TestPreviewFixturesDoNotRewriteAnotherUsersRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec, err := PreviewSpec(126)
	require.NoError(t, err)
	runner := spec.PreviewRunner()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	neighbour := LocalSpec()
	seedLocalPostgres(ctx, t, db, neighbour)
	require.NoError(t, RunLocalFixtures(ctx, logger, db, assetstest.NewTestBlobStore(t), nil, LocalFixturesOptions{DeveloperEmail: testDeveloperEmail, Environment: "local"}))
	require.NoError(t, demoseedtest.PlantLoginArtifacts(ctx, db, demoseedtest.LoginArtifacts{
		OrgID:        "org-someone-else",
		OrgWorkOSID:  "workos-org-someone-else",
		OrgName:      "Someone else",
		OrgSlug:      "someone-else",
		UserID:       runner.ID,
		Email:        "someone-else@example.test",
		WorkOSID:     "workos-user-someone-else",
		MembershipID: "membership-someone-else",
	}))
	seedLocalPostgres(ctx, t, db, spec)

	require.ErrorContains(t, RunPreviewFixtures(ctx, logger, db, nil, spec), "belongs to another user")

	user, err := userrepo.New(db).GetUser(ctx, runner.ID)
	require.NoError(t, err)
	require.Equal(t, "someone-else@example.test", user.Email)
	organizations, err := orgrepo.New(db).ListOrganizationsForUser(ctx, conv.ToPGText(runner.ID))
	require.NoError(t, err)
	require.Len(t, organizations, 1)
	require.Equal(t, "org-someone-else", organizations[0].ID)
}

// The runner's address already belonging to someone is a refusal, not an
// adoption: that user keeps their id and gains no membership.
func TestPreviewFixturesDoNotAdoptAnotherUsersAddress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec, err := PreviewSpec(125)
	require.NoError(t, err)
	runner := spec.PreviewRunner()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	neighbour := LocalSpec()
	seedLocalPostgres(ctx, t, db, neighbour)
	require.NoError(t, RunLocalFixtures(ctx, logger, db, assetstest.NewTestBlobStore(t), nil, LocalFixturesOptions{DeveloperEmail: runner.Email, Environment: "local"}))
	seedLocalPostgres(ctx, t, db, spec)

	require.Error(t, RunPreviewFixtures(ctx, logger, db, nil, spec))

	user, err := userrepo.New(db).GetUserByEmail(ctx, runner.Email)
	require.NoError(t, err)
	require.NotEqual(t, runner.ID, user.ID)
	organizations, err := orgrepo.New(db).ListOrganizationsForUser(ctx, conv.ToPGText(user.ID))
	require.NoError(t, err)
	require.Len(t, organizations, 1)
	require.Equal(t, neighbour.OrgID, organizations[0].ID)
}
