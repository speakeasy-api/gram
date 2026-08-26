//go:build demoseed_safety

package demoseed

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/users"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestLocalFixturesDeveloperIdentityIsOrderIndependent(t *testing.T) { //nolint:paralleltest // Full seed runs share ClickHouse tables with TestDemoSeedSafety.
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	db, err := infra.CloneTestDatabase(t, "local_developer_identity")
	require.NoError(t, err)
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	const loginFirstEmail = "login-first@example.test"
	emulateDeveloperLogin(t, ctx, db, loginFirstEmail)
	require.NoError(t, Run(ctx, logger, db, ch, blob, LocalSpec()))
	require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, LocalFixturesOptions{
		DeveloperEmail: loginFirstEmail,
		Environment:    "local",
		ObservedEnv:    nil,
	}))
	requireCanonicalDeveloper(t, ctx, db, loginFirstEmail)

	require.NoError(t, Run(ctx, logger, db, ch, blob, LocalSpec()))
	require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, LocalFixturesOptions{
		DeveloperEmail: loginFirstEmail,
		Environment:    "local",
		ObservedEnv:    nil,
	}))
	requireCanonicalDeveloper(t, ctx, db, loginFirstEmail)

	const seedFirstEmail = "seed-first@example.test"
	require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, LocalFixturesOptions{
		DeveloperEmail: seedFirstEmail,
		Environment:    "local",
		ObservedEnv:    nil,
	}))
	requireCanonicalDeveloper(t, ctx, db, seedFirstEmail)

	emulateDeveloperLogin(t, ctx, db, seedFirstEmail)
	requireCanonicalDeveloper(t, ctx, db, seedFirstEmail)

	require.NoError(t, RunLocalFixtures(ctx, logger, db, blob, nil, LocalFixturesOptions{
		DeveloperEmail: seedFirstEmail,
		Environment:    "local",
		ObservedEnv:    nil,
	}))
	requireCanonicalDeveloper(t, ctx, db, seedFirstEmail)
}

func emulateDeveloperLogin(t *testing.T, ctx context.Context, db *pgxpool.Pool, email string) {
	t.Helper()

	workosID := devidentity.WorkOSUserID(devidentity.DeterministicUserID(email))
	gramUserID := users.UserIDFromWorkOSID(workosID)

	queries := usersrepo.New(db)
	admin := false
	existing, err := queries.GetUserByEmail(ctx, email)
	if err == nil {
		admin = existing.Admin
	} else {
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}

	_, err = queries.UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          gramUserID,
		Email:       email,
		DisplayName: email,
		PhotoUrl:    conv.ToPGText(""),
		Admin:       admin,
	})
	require.NoError(t, err)
	require.NoError(t, queries.OverwriteUserWorkosID(ctx, usersrepo.OverwriteUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosID),
		ID:       gramUserID,
	}))
}

func requireCanonicalDeveloper(t *testing.T, ctx context.Context, db *pgxpool.Pool, email string) {
	t.Helper()

	workosID := devidentity.WorkOSUserID(devidentity.DeterministicUserID(email))
	gramUserID := users.UserIDFromWorkOSID(workosID)

	userQueries := usersrepo.New(db)
	user, err := userQueries.GetUserByEmail(ctx, email)
	require.NoError(t, err)
	require.Equal(t, gramUserID, user.ID)
	require.Equal(t, workosID, user.WorkosID.String)

	userID, err := userQueries.GetUserIDByWorkosID(ctx, conv.ToPGText(workosID))
	require.NoError(t, err)
	require.Equal(t, gramUserID, userID)

	organizationQueries := organizationsrepo.New(db)
	membership, err := organizationQueries.GetOrganizationUserRelationship(ctx, organizationsrepo.GetOrganizationUserRelationshipParams{
		OrganizationID: LocalSpec().OrgID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)
	require.Equal(t, gramUserID, membership.UserID.String)
	require.Equal(t, workosID, membership.WorkosUserID.String)
	require.Equal(t, "devidp_mem_"+gramUserID, membership.WorkosMembershipID.String)
}
