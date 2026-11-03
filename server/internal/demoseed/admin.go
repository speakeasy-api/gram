package demoseed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// Each status set exceeds the admin list's 50-row page size.
const (
	AdminSeedOrganizationsPerStatus = 60
	AdminSeedOrganizationCount      = 2 * AdminSeedOrganizationsPerStatus
	AdminSeedSearchName             = "Fictional Admin Lab"
)

// validateAdminSeedTarget is checked before opening a connection. Match the
// repository's local environment gate, plus fail closed on non-development DBs
// and every non-loopback primary/fallback target. Never include the DSN in errors.
func validateAdminSeedTarget(environment string, config *pgx.ConnConfig) error {
	if environment != "local" || config.Database != "gram" || config.User != "gram" {
		return errors.New("admin seed requires environment=local and the gram development database/user")
	}
	hosts := []string{config.Host}
	for _, fallback := range config.Fallbacks {
		hosts = append(hosts, fallback.Host)
	}
	for _, host := range hosts {
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("admin seed requires loopback-only PostgreSQL targets (including fallbacks)")
		}
	}
	return nil
}

type adminSeedFixture struct {
	ID, Name, Slug string
	Members        int
	Disabled       bool
	CreatedAt      time.Time
}

func adminSeedFixtures(now time.Time) []adminSeedFixture {
	now = now.UTC()
	midnight := now.Truncate(24 * time.Hour)
	dates := []time.Time{now, now.Add(-time.Hour)}
	// Calendar presets include today: an N-day window starts at UTC midnight
	// N-1 days ago. Also bracket today's start and a custom range's exclusive
	// end (midnight after an end date three days ago).
	boundaries := []time.Time{midnight, midnight.AddDate(0, 0, -2)}
	for _, days := range []int{7, 14, 30} {
		boundaries = append(boundaries, midnight.AddDate(0, 0, -(days-1)), now.AddDate(0, 0, -days))
	}
	for _, boundary := range boundaries {
		for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
			candidate := boundary.Add(offset)
			// At midnight, the after-today-start fixture would otherwise be future.
			if !candidate.After(now) {
				dates = append(dates, candidate)
			}
		}
	}
	for _, days := range []int{60, 90, 180, 365} {
		dates = append(dates, midnight.AddDate(0, 0, -days))
	}
	counts := []int{0, 1, 5, 10, 25, 100}
	fixtures := make([]adminSeedFixture, AdminSeedOrganizationCount)
	for i := range fixtures {
		fixtures[i] = adminSeedFixture{
			ID:      fmt.Sprintf("org_local_admin_fixture_%02d", i+1),
			Name:    fmt.Sprintf("%s %02d", AdminSeedSearchName, i+1),
			Slug:    fmt.Sprintf("fictional-admin-lab-%02d", i+1),
			Members: counts[i%len(counts)], Disabled: i >= AdminSeedOrganizationsPerStatus, CreatedAt: dates[(i%AdminSeedOrganizationsPerStatus)%len(dates)],
		}
	}
	fixtures[60].Name = fixtures[0].Name // Duplicate display names, distinct canonical targets.
	return fixtures
}

// Dedicated fictional users cover lifecycle and overflow without changing the
// organization-list fixtures' original membership distribution.
type adminSeedUserFixture struct {
	ID, Name                                            string
	Organizations                                       []string
	Deleted, WorkosDeleted, DeletedMembership, HasLogin bool
}

func adminSeedUserFixtures() []adminSeedUserFixture {
	return []adminSeedUserFixture{
		{ID: "user_local_admin_edge_zero", Name: "", Organizations: nil, Deleted: false, WorkosDeleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_multi", Name: "Fictional 100%_Literal Member", Organizations: []string{"org_local_admin_fixture_01", "org_local_admin_fixture_02", "org_local_admin_fixture_03", "org_local_admin_fixture_04", "org_local_admin_fixture_61"}, Deleted: false, WorkosDeleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_deleted", Name: "Fictional Deleted Member", Deleted: true, Organizations: nil, WorkosDeleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_workos_deleted", Name: "Fictional Externally Deleted Member", WorkosDeleted: true, Organizations: nil, Deleted: false, DeletedMembership: false, HasLogin: false},
		{ID: "user_local_admin_edge_membership_deleted", Name: "Fictional Former Member", Organizations: []string{"org_local_admin_fixture_01"}, DeletedMembership: true, Deleted: false, WorkosDeleted: false, HasLogin: false},
		{ID: "user_local_admin_edge_login", Name: "Fictional Returning Member", HasLogin: true, Organizations: nil, Deleted: false, WorkosDeleted: false, DeletedMembership: false},
	}
}

// RunAdminSeed is deliberately independent of Run and RunLocalFixtures. It
// creates no projects, developer memberships, credentials or external accounts.
func RunAdminSeed(ctx context.Context, environment, databaseURL string) error {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid admin seed database configuration")
	}
	if err := validateAdminSeedTarget(environment, config.ConnConfig); err != nil {
		return err
	}
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect admin seed database: %w", err)
	}
	defer db.Close()
	return runAdminSeed(ctx, db, time.Now())
}

func runAdminSeed(ctx context.Context, db *pgxpool.Pool, now time.Time) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin admin seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := repo.New(tx)
	// Serialize concurrent invocations; all fixture writes commit atomically.
	if err = q.AdminSeedLock(ctx); err != nil {
		return fmt.Errorf("serialize admin seed: %w", err)
	}
	// The advisory lock only serializes seeders. Lock the local-only fixture
	// tables against other writers too, including inserts into currently absent
	// reserved IDs, so ownership cannot change between validation and upsert.
	if err := q.LockAdminSeedIdentities(ctx); err != nil {
		return fmt.Errorf("lock admin seed identities: %w", err)
	}
	fixtures := adminSeedFixtures(now)
	// Validate every existing identity before the first write. Names, dates,
	// disabled/deleted states are refreshed deliberately; stable fictional slugs
	// and emails, no external identity, and no unrelated memberships mark ownership.
	for i, fixture := range fixtures {
		collision, err := q.AdminSeedOrganizationCollision(ctx, repo.AdminSeedOrganizationCollisionParams{OrganizationID: fixture.ID, Slug: fixture.Slug})
		if err != nil {
			return fmt.Errorf("validate fictional organization identity: %w", err)
		}
		if collision {
			return errors.New("admin seed organization identity collision")
		}
		userIDs := make([]string, fixture.Members)
		for member := range userIDs {
			userIDs[member] = fmt.Sprintf("user_local_admin_fixture_%02d_%03d", i+1, member+1)
		}
		collision, err = q.AdminSeedMemberCollision(ctx, repo.AdminSeedMemberCollisionParams{UserIds: userIDs, OrganizationID: fixture.ID})
		if err != nil {
			return fmt.Errorf("validate fictional member identity: %w", err)
		}
		if collision {
			return errors.New("admin seed member identity collision")
		}
	}
	updatedAt := conv.ToPGTimestamptz(now)
	for i, fixture := range fixtures {
		var disabledAt *time.Time
		if fixture.Disabled {
			disabledAt = &now
		}
		err = q.AdminSeedUpsertOrganization(ctx, repo.AdminSeedUpsertOrganizationParams{
			ID:         fixture.ID,
			Name:       fixture.Name,
			Slug:       fixture.Slug,
			CreatedAt:  conv.ToPGTimestamptz(fixture.CreatedAt),
			DisabledAt: conv.PtrToPGTimestamptz(disabledAt),
			UpdatedAt:  updatedAt,
		})
		if err != nil {
			return fmt.Errorf("seed fictional organization: %w", err)
		}
		for member := 1; member <= fixture.Members; member++ {
			userID := fmt.Sprintf("user_local_admin_fixture_%02d_%03d", i+1, member)
			err = q.AdminSeedUpsertMember(ctx, repo.AdminSeedUpsertMemberParams{
				ID:          userID,
				Email:       userID + "@admin-seed.invalid",
				DisplayName: "Fictional Admin Member",
				UpdatedAt:   updatedAt,
			})
			if err != nil {
				return fmt.Errorf("seed fictional member: %w", err)
			}
			err = q.AdminSeedUpsertMembership(ctx, repo.AdminSeedUpsertMembershipParams{
				ID:             int64(-8900000 - (i+1)*1000 - member),
				OrganizationID: fixture.ID,
				UserID:         userID,
				UpdatedAt:      updatedAt,
			})
			if err != nil {
				return fmt.Errorf("seed fictional membership: %w", err)
			}
		}
	}
	for i, fixture := range adminSeedUserFixtures() {
		orgIDs := fixture.Organizations
		if orgIDs == nil {
			orgIDs = []string{}
		}
		collision, err := q.AdminSeedEdgeUserCollision(ctx, repo.AdminSeedEdgeUserCollisionParams{UserID: fixture.ID, OrganizationIds: orgIDs})
		if err != nil {
			return fmt.Errorf("validate fictional edge user: %w", err)
		}
		if collision {
			return errors.New("admin seed edge user identity collision")
		}
		var deletedAt, workosDeletedAt, lastLogin *time.Time
		if fixture.Deleted {
			deletedAt = &now
		}
		if fixture.WorkosDeleted {
			workosDeletedAt = &now
		}
		if fixture.HasLogin {
			lastLogin = &now
		}
		if err := q.AdminSeedUpsertEdgeUser(ctx, repo.AdminSeedUpsertEdgeUserParams{
			ID:              fixture.ID,
			Email:           fixture.ID + "@admin-seed.invalid",
			DisplayName:     fixture.Name,
			DeletedAt:       conv.PtrToPGTimestamptz(deletedAt),
			WorkosDeletedAt: conv.PtrToPGTimestamptz(workosDeletedAt),
			LastLogin:       conv.PtrToPGTimestamptz(lastLogin),
			UpdatedAt:       updatedAt,
		}); err != nil {
			return fmt.Errorf("seed fictional edge user: %w", err)
		}
		for j, orgID := range orgIDs {
			var membershipDeletedAt *time.Time
			if fixture.DeletedMembership {
				membershipDeletedAt = &now
			}
			if err := q.AdminSeedUpsertEdgeMembership(ctx, repo.AdminSeedUpsertEdgeMembershipParams{
				ID:             int64(-9200000 - i*100 - j),
				OrganizationID: orgID,
				UserID:         fixture.ID,
				DeletedAt:      conv.PtrToPGTimestamptz(membershipDeletedAt),
				UpdatedAt:      updatedAt,
			}); err != nil {
				return fmt.Errorf("seed fictional edge membership: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin seed: %w", err)
	}
	return nil
}
