package demoseed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	return fixtures
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
	// Serialize concurrent invocations; all fixture writes commit atomically.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(890001)"); err != nil {
		return err
	}
	for i, fixture := range adminSeedFixtures(now) {
		var disabledAt *time.Time
		if fixture.Disabled {
			disabledAt = &now
		}
		_, err = tx.Exec(ctx, `INSERT INTO public.organization_metadata
   (id,name,slug,created_at,disabled_at) VALUES ($1,$2,$3,$4,$5)
   ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,slug=EXCLUDED.slug,
   created_at=EXCLUDED.created_at,disabled_at=EXCLUDED.disabled_at,updated_at=$6`,
			fixture.ID, fixture.Name, fixture.Slug, fixture.CreatedAt, disabledAt, now)
		if err != nil {
			return fmt.Errorf("seed fictional organization: %w", err)
		}
		for member := 1; member <= fixture.Members; member++ {
			userID := fmt.Sprintf("user_local_admin_fixture_%02d_%03d", i+1, member)
			_, err = tx.Exec(ctx, `INSERT INTO public.users (id,email,display_name)
    VALUES ($1,$2,$3) ON CONFLICT (id) DO UPDATE SET
    email=EXCLUDED.email,display_name=EXCLUDED.display_name,deleted_at=NULL,updated_at=$4`,
				userID, userID+"@admin-seed.invalid", "Fictional Admin Member", now)
			if err != nil {
				return fmt.Errorf("seed fictional member: %w", err)
			}
			// A reserved negative ID range avoids the normal identity sequence and
			// preserves membership IDs across reruns without delete/reinsert churn.
			_, err = tx.Exec(ctx, `INSERT INTO public.organization_user_relationships
    (id,organization_id,user_id) VALUES ($1,$2,$3)
    ON CONFLICT (organization_id,user_id) DO UPDATE SET deleted_at=NULL,updated_at=$4`,
				int64(-8900000-(i+1)*1000-member), fixture.ID, userID, now)
			if err != nil {
				return fmt.Errorf("seed fictional membership: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit admin seed: %w", err)
	}
	return nil
}
