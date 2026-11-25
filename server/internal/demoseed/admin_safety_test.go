//go:build demoseed_safety

package demoseed

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/demoseed/demoseedtest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// Decode the existing schema-agnostic safety snapshot rather than querying raw
// SQL in tests. Full snapshots below also catch changes to unmodeled columns.
type adminSeedSnapshotRow struct {
	Name            string     `json:"name"`
	DisplayName     string     `json:"display_name"`
	ID              any        `json:"id"`
	CreatedAt       time.Time  `json:"created_at"`
	DisabledAt      *time.Time `json:"disabled_at"`
	DeletedAt       *time.Time `json:"deleted_at"`
	LastLogin       *time.Time `json:"last_login"`
	WorkosDeletedAt *time.Time `json:"workos_deleted_at"`
	Deleted         bool       `json:"deleted"`
	WorkosID        *string    `json:"workos_id"`
	UserID          string     `json:"user_id"`
	OrganizationID  string     `json:"organization_id"`
}

func adminSeedSnapshotRows(t *testing.T, snapshot demoseedtest.PostgresSnapshot, table string) map[string]adminSeedSnapshotRow {
	t.Helper()
	rows := make(map[string]adminSeedSnapshotRow)
	for data := range snapshot[table] {
		var row adminSeedSnapshotRow
		require.NoError(t, json.Unmarshal([]byte(data), &row))
		rows[fmt.Sprint(row.ID)] = row
	}
	return rows
}

func TestAdminSeedSafetyAndIdempotency(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	fixtures := testrepo.New(db)
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID: "org_admin_seed_sentinel", Name: "Unrelated fictional org", Slug: "admin-seed-sentinel",
	}))
	require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID: "user_admin_seed_sentinel", Email: "sentinel@example.invalid", DisplayName: "Unrelated fictional user",
	}))
	require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: "org_admin_seed_sentinel", UserID: pgtype.Text{String: "user_admin_seed_sentinel", Valid: true},
	}))
	sentinel, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	// Names are refreshed presentation, not ownership. Stable slug/email identify
	// legitimate preexisting fixtures even before their membership is created.
	require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: "org_local_admin_fixture_02", Name: "Stale fixture name", Slug: "fictional-admin-lab-02",
		FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
		DisabledAt:         pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}))
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID: "org_local_admin_fixture_61", Name: "Stale fixture name", Slug: "fictional-admin-lab-61",
	}))
	require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID: "user_local_admin_fixture_02_001", Email: "user_local_admin_fixture_02_001@admin-seed.invalid", DisplayName: "Stale fixture name",
	}))
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	var initialIDs []string
	for iteration, at := range []time.Time{now, now, now.Add(24 * time.Hour)} {
		require.NoError(t, fixtures.ForceSoftDeleteUser(ctx, "user_local_admin_fixture_02_001"))
		require.NoError(t, fixtures.ForceSoftDeleteOrganizationUserRelationship(ctx, testrepo.ForceSoftDeleteOrganizationUserRelationshipParams{
			OrganizationID: "org_local_admin_fixture_02", UserID: pgtype.Text{String: "user_local_admin_fixture_02_001", Valid: true},
		}))
		require.NoError(t, runAdminSeed(ctx, db, at))
		snapshot, err := demoseedtest.SnapshotPostgres(ctx, db)
		require.NoError(t, err)
		for table, rows := range sentinel {
			for row, count := range rows {
				require.Equal(t, count, snapshot[table][row], "unrelated row changed in %s", table)
			}
		}
		orgs := adminSeedSnapshotRows(t, snapshot, "organization_metadata")
		users := adminSeedSnapshotRows(t, snapshot, "users")
		members := adminSeedSnapshotRows(t, snapshot, "organization_user_relationships")
		require.Len(t, orgs, AdminSeedOrganizationCount+1)
		expectedMembers := 1
		counts := map[string]int{}
		ids := make([]string, 0, len(members))
		for id, member := range members {
			ids = append(ids, id)
			if strings.HasPrefix(member.UserID, "user_local_admin_edge_") {
				continue
			}
			require.Nil(t, member.DeletedAt)
			require.False(t, member.Deleted)
			counts[member.OrganizationID]++
		}
		for _, user := range users {
			if strings.HasPrefix(fmt.Sprint(user.ID), "user_local_admin_edge_") {
				continue
			}
			require.Nil(t, user.DeletedAt)
			if user.ID != "user_admin_seed_sentinel" {
				require.Equal(t, "Fictional Admin Member", user.DisplayName)
			}
		}
		for _, fixture := range adminSeedFixtures(at) {
			expectedMembers += fixture.Members
			org := orgs[fixture.ID]
			require.Equal(t, fixture.Name, org.Name)
			require.True(t, org.CreatedAt.Equal(fixture.CreatedAt))
			require.Equal(t, fixture.Disabled, org.DisabledAt != nil)
			require.Equal(t, fixture.Members, counts[fixture.ID])
			require.Nil(t, org.WorkosID)
		}
		require.Greater(t, expectedMembers, 50)
		extraMembers := 0
		for _, fixture := range adminSeedUserFixtures() {
			user := users[fixture.ID]
			require.Equal(t, fixture.Name, user.DisplayName)
			require.Equal(t, fixture.Deleted, user.DeletedAt != nil)
			require.Equal(t, fixture.WorkosDeleted, user.WorkosDeletedAt != nil)
			require.Equal(t, fixture.HasLogin, user.LastLogin != nil)
			actual := []string{}
			for _, member := range members {
				if member.UserID == fixture.ID {
					actual = append(actual, member.OrganizationID)
					require.Equal(t, fixture.DeletedMembership, member.Deleted)
				}
			}
			require.ElementsMatch(t, fixture.Organizations, actual)
			extraMembers += len(actual)
		}
		require.Equal(t, orgs["org_local_admin_fixture_01"].Name, orgs["org_local_admin_fixture_61"].Name)
		require.NotNil(t, orgs["org_local_admin_fixture_61"].DisabledAt)
		require.Len(t, users, expectedMembers+6)
		require.Len(t, members, expectedMembers+extraMembers)
		// Exercise the actual Task 2 reads against lifecycle and overflow fixtures.
		queries := adminrepo.New(db)
		params := adminrepo.AdminListUsersParams{
			NamePatterns: []string{}, EmailPatterns: []string{`%user\_local\_admin\_edge\_%`}, OrgPatterns: []string{}, AnyPatterns: []string{}, PageLimit: 50,
		}
		page, err := queries.AdminListUsers(ctx, params)
		require.NoError(t, err)
		actualIDs := []string{}
		for _, user := range page {
			actualIDs = append(actualIDs, user.ID)
		}
		require.Equal(t, []string{"user_local_admin_edge_login", "user_local_admin_edge_membership_deleted", "user_local_admin_edge_multi", "user_local_admin_edge_zero"}, actualIDs)
		total, err := queries.AdminCountUsers(ctx, adminrepo.AdminCountUsersParams{NamePatterns: params.NamePatterns, EmailPatterns: params.EmailPatterns, OrgPatterns: params.OrgPatterns, AnyPatterns: params.AnyPatterns})
		require.NoError(t, err)
		require.EqualValues(t, 4, total)
		previews, err := queries.AdminListUsersOrganizationPreviews(ctx, actualIDs)
		require.NoError(t, err)
		require.Len(t, previews, 3)
		for _, preview := range previews {
			require.Equal(t, "user_local_admin_edge_multi", preview.UserID.String)
			require.EqualValues(t, 5, preview.OrganizationCount)
		}
		overflow, err := queries.AdminListUserOrganizations(ctx, adminrepo.AdminListUserOrganizationsParams{UserID: "user_local_admin_edge_multi", PageOffset: 3, PageLimit: 50})
		require.NoError(t, err)
		require.Len(t, overflow, 2)
		orgTotal, err := queries.AdminCountUserOrganizations(ctx, "user_local_admin_edge_multi")
		require.NoError(t, err)
		require.EqualValues(t, 5, orgTotal)
		slices.Sort(ids)
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
	}
}

func TestAdminSeedRollsBackOnCollision(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, testrepo.New(db).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID: "unrelated_fixture_user", Email: "user_local_admin_fixture_02_001@admin-seed.invalid", DisplayName: "Fictional collision sentinel",
	}))
	before, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	require.Error(t, runAdminSeed(ctx, db, time.Now()))
	after, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAdminSeedRejectsReservedIdentityCollision(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"organization slug", "organization external identity", "user email", "user external identity", "user unrelated membership", "edge user email", "edge user external identity", "edge user unrelated membership"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)
			fixtures := testrepo.New(db)
			userID := "user_local_admin_fixture_02_001"
			if strings.HasPrefix(kind, "edge ") {
				userID = "user_local_admin_edge_zero"
				kind = strings.TrimPrefix(kind, "edge ")
			}
			switch kind {
			case "organization slug", "organization external identity":
				slug := "unrelated-fictional-org"
				var workosID pgtype.Text
				if kind == "organization external identity" {
					slug = "fictional-admin-lab-01"
					workosID = pgtype.Text{String: "org_unrelated_fictional", Valid: true}
				}
				require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
					FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, FreeTrialEndsAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
					ID: "org_local_admin_fixture_01", Name: "Unrelated fictional org", Slug: slug, WorkosID: workosID,
				}))
			case "user email", "user external identity", "user unrelated membership":
				email := "unrelated@example.invalid"
				if kind != "user email" {
					email = userID + "@admin-seed.invalid"
				}
				require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
					ID: userID, Email: email, DisplayName: "Unrelated fictional user",
				}))
				if kind == "user external identity" {
					require.NoError(t, userrepo.New(db).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{
						ID: userID, WorkosID: pgtype.Text{String: "user_unrelated_fictional", Valid: true},
					}))
				}
				if kind == "user unrelated membership" {
					require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
						FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, FreeTrialEndsAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
						ID: "org_unrelated_fictional", Name: "Unrelated fictional org", Slug: "unrelated-fictional-org",
					}))
					require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
						OrganizationID: "org_unrelated_fictional", UserID: pgtype.Text{String: userID, Valid: true},
					}))
				}
			}
			before, err := demoseedtest.SnapshotPostgres(ctx, db)
			require.NoError(t, err)
			seedErr := runAdminSeed(ctx, db, time.Now())
			after, err := demoseedtest.SnapshotPostgres(ctx, db)
			require.NoError(t, err)
			require.Error(t, seedErr, "must reject reserved IDs owned by unrelated identities")
			require.Equal(t, before, after, "collision must not overwrite any existing data")
		})
	}
}

func TestAdminSeedConcurrentReruns(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	var group errgroup.Group
	for range 2 {
		group.Go(func() error { return runAdminSeed(ctx, db, now) })
	}
	require.NoError(t, group.Wait())
	snapshot, err := demoseedtest.SnapshotPostgres(ctx, db)
	require.NoError(t, err)
	require.Len(t, snapshot["organization_metadata"], AdminSeedOrganizationCount)
	expectedMembers := 0
	for _, fixture := range adminSeedFixtures(now) {
		expectedMembers += fixture.Members
	}
	require.Len(t, snapshot["users"], expectedMembers+len(adminSeedUserFixtures()))
	for _, fixture := range adminSeedUserFixtures() {
		expectedMembers += len(fixture.Organizations)
	}
	require.Len(t, snapshot["organization_user_relationships"], expectedMembers)
}
