//go:build demoseed_safety

package demoseed

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/users"
)

const testDeveloperEmail = "dev@example.com"

// seedLocalPostgres applies the tenant seed the way demoseed.Run does, minus
// ClickHouse: the local fixtures are a Postgres-only concern, and the shared
// ClickHouse container would make these tests trample the row counts
// TestDemoSeedSafety holds fixed.
func seedLocalPostgres(ctx context.Context, t *testing.T, db *pgxpool.Pool, spec Spec) {
	t.Helper()

	require.NoError(t, demoseedtest.ExecPostgresScript(ctx, db, spec.Rewrite(postgresSQL)))
}

// TestLocalFixturesAfterLogin covers the database a developer actually has
// when they run the seed for the first time: one where they have already
// logged in. The auth callback creates the org (un-whitelisted) and a user row
// whose id it derives from the dev-idp subject — neither of which the seed can
// predict — so a seed that keys on its own derived ids collides on
// users_email_key and leaves the org behind the BookDemo gate.
func TestLocalFixturesAfterLogin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec := LocalSpec()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	subject := devidentity.WorkOSUserID(devidentity.DeterministicUserID(testDeveloperEmail))
	loginUserID := users.UserIDFromWorkOSID(subject)
	require.NotEqual(t, loginUserID, devidentity.DeterministicUserID(testDeveloperEmail).String(),
		"the two id derivations coincide; this test would be vacuous")

	require.NoError(t, demoseedtest.PlantLoginArtifacts(ctx, db, demoseedtest.LoginArtifacts{
		OrgID:        spec.OrgID,
		OrgWorkOSID:  spec.WorkOSOrgID,
		OrgName:      spec.OrgName,
		OrgSlug:      spec.OrgSlug,
		UserID:       loginUserID,
		Email:        testDeveloperEmail,
		WorkOSID:     subject,
		MembershipID: "devidp_mem_from_login",
	}))

	seedLocalPostgres(ctx, t, db, spec)
	require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, LocalFixturesOptions{
		DeveloperEmail: testDeveloperEmail,
		Environment:    "local",
	}))

	state, err := demoseedtest.ReadDeveloperState(ctx, db, spec.OrgID, testDeveloperEmail)
	require.NoError(t, err)
	require.Equal(t, demoseedtest.DeveloperState{
		Users:           1,
		UserID:          loginUserID,
		WorkOSID:        subject,
		Whitelisted:     true,
		OrgWorkOSID:     spec.WorkOSOrgID,
		Memberships:     1,
		RoleAssignments: 1,
	}, state)
}

// TestLocalFixturesReseed is the ordinary case — seed first, then seed again —
// which must stay idempotent now that the developer row is keyed on email.
func TestLocalFixturesReseed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	spec := LocalSpec()

	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	opts := LocalFixturesOptions{DeveloperEmail: testDeveloperEmail, Environment: "local"}
	for range 2 {
		seedLocalPostgres(ctx, t, db, spec)
		require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, opts))
	}

	seedUserID := devidentity.DeterministicUserID(testDeveloperEmail)
	state, err := demoseedtest.ReadDeveloperState(ctx, db, spec.OrgID, testDeveloperEmail)
	require.NoError(t, err)
	require.Equal(t, demoseedtest.DeveloperState{
		Users:           1,
		UserID:          seedUserID.String(),
		WorkOSID:        devidentity.WorkOSUserID(seedUserID),
		Whitelisted:     true,
		OrgWorkOSID:     spec.WorkOSOrgID,
		Memberships:     1,
		RoleAssignments: 1,
	}, state)
}
