//go:build demoseed_safety

package demoseed

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/require"
)

func TestAdminSeedSafetyAndIdempotency(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO organization_metadata (id,name,slug) VALUES ('org_admin_seed_sentinel','Unrelated fictional org','admin-seed-sentinel');
 INSERT INTO users (id,email,display_name) VALUES ('user_admin_seed_sentinel','sentinel@example.invalid','Unrelated fictional user');
 INSERT INTO organization_user_relationships (organization_id,user_id) VALUES ('org_admin_seed_sentinel','user_admin_seed_sentinel')`)
	require.NoError(t, err)
	snapshot := func() string {
		t.Helper()
		var result string
		require.NoError(t, db.QueryRow(ctx, `SELECT jsonb_build_array(
   (SELECT to_jsonb(o) FROM organization_metadata o WHERE id='org_admin_seed_sentinel'),
   (SELECT to_jsonb(u) FROM users u WHERE id='user_admin_seed_sentinel'),
   (SELECT to_jsonb(m) FROM organization_user_relationships m WHERE user_id='user_admin_seed_sentinel'))::text`).Scan(&result))
		return result
	}
	sentinel := snapshot()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	var initialIDs string
	for iteration, at := range []time.Time{now, now, now.Add(24 * time.Hour)} {
		if iteration > 0 {
			_, err := db.Exec(ctx, `UPDATE users SET deleted_at=now() WHERE id='user_local_admin_fixture_02_001';
    UPDATE organization_user_relationships SET deleted_at=now() WHERE user_id='user_local_admin_fixture_02_001'`)
			require.NoError(t, err)
		}
		require.NoError(t, runAdminSeed(ctx, db, at))
		var userRestored, membershipRestored, membershipDeleted bool
		require.NoError(t, db.QueryRow(ctx, `SELECT u.deleted_at IS NULL, m.deleted_at IS NULL, m.deleted
   FROM users u JOIN organization_user_relationships m ON m.user_id=u.id
   WHERE u.id='user_local_admin_fixture_02_001'`).Scan(&userRestored, &membershipRestored, &membershipDeleted))
		require.True(t, userRestored)
		require.True(t, membershipRestored)
		require.False(t, membershipDeleted)
		require.Equal(t, sentinel, snapshot())
		var orgs, users, members int
		require.NoError(t, db.QueryRow(ctx, `SELECT (SELECT count(*) FROM organization_metadata), (SELECT count(*) FROM users), (SELECT count(*) FROM organization_user_relationships)`).Scan(&orgs, &users, &members))
		require.Equal(t, AdminSeedOrganizationCount+1, orgs)
		expectedMembers := 1
		for _, fixture := range adminSeedFixtures(at) {
			expectedMembers += fixture.Members
			var created time.Time
			var disabled bool
			var count int
			require.NoError(t, db.QueryRow(ctx, `SELECT created_at,disabled_at IS NOT NULL,
    (SELECT count(*) FROM organization_user_relationships m WHERE m.organization_id=o.id AND m.deleted_at IS NULL)
    FROM organization_metadata o WHERE id=$1`, fixture.ID).Scan(&created, &disabled, &count))
			require.True(t, created.Equal(fixture.CreatedAt))
			require.Equal(t, fixture.Disabled, disabled)
			require.Equal(t, fixture.Members, count)
		}
		require.Equal(t, expectedMembers, users)
		require.Equal(t, expectedMembers, members)
		var ids string
		require.NoError(t, db.QueryRow(ctx, `SELECT string_agg(id::text,',' ORDER BY id) FROM organization_user_relationships`).Scan(&ids))
		if iteration == 0 {
			initialIDs = ids
		} else {
			require.Equal(t, initialIDs, ids)
		}
		// Exercise the same query used by auth.identity for the normal org picker.
		visible, err := orgrepo.New(db).ListOrganizationsForUser(ctx, pgtype.Text{String: "user_admin_seed_sentinel", Valid: true})
		require.NoError(t, err)
		require.Len(t, visible, 1)
		require.Equal(t, "org_admin_seed_sentinel", visible[0].ID)
		var external int
		require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM organization_metadata WHERE id <> 'org_admin_seed_sentinel' AND workos_id IS NOT NULL`).Scan(&external))
		require.Zero(t, external)
	}
}

func TestAdminSeedRollsBackOnCollision(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO users (id,email,display_name) VALUES ('unrelated_fixture_user','user_local_admin_fixture_02_001@admin-seed.invalid','Fictional collision sentinel')`)
	require.NoError(t, err)
	require.Error(t, runAdminSeed(ctx, db, time.Now()))
	var orgs, users int
	require.NoError(t, db.QueryRow(ctx, `SELECT (SELECT count(*) FROM organization_metadata),(SELECT count(*) FROM users)`).Scan(&orgs, &users))
	require.Zero(t, orgs)
	require.Equal(t, 1, users)
}
