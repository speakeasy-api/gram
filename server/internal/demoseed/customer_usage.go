package demoseed

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// customerUsageSeedLockID serializes concurrent customer usage seeds.
const customerUsageSeedLockID = 890002

// customerUsageSeedProject is the fictional project every seeded reading
// belongs to. The customer usage page reads per organization, so one is enough.
const customerUsageSeedProject = "00000000-0000-4000-a000-00000000c001"

// customerUsageSeedFixture is one fictional organization on the admin Customer
// usage page. Usage is one reading per product per day for UsageDays days,
// ending today, scaled by Growth^(-daysAgo/30) so Growth above 1 rises month
// over month and below 1 falls.
type customerUsageSeedFixture struct {
	ID, Name, Slug, AccountType string

	// CreatedDaysAgo dates the organization, which bounds how many billing
	// cycles it has and whether it has a previous cycle to compare against.
	CreatedDaysAgo int

	// AnchorDay is the billing-cycle anchor day; zero leaves no billing row,
	// which means calendar-month cycles.
	AnchorDay int

	// Trial is "", "running", "expired" or "converted".
	Trial string

	UsageDays int
	Growth    float64

	// Daily base volumes per product, before growth and noise.
	StorageSTokens, RiskSTokens, EgressBytes int64
}

func customerUsageSeedFixtures() []customerUsageSeedFixture {
	return []customerUsageSeedFixture{
		{ID: "org_local_customer_usage_growing", Name: "Fictional Growing Enterprise", Slug: "fictional-growing-enterprise", AccountType: "enterprise",
			CreatedDaysAgo: 300, AnchorDay: 25, Trial: "", UsageDays: 186, Growth: 1.35,
			StorageSTokens: 12_000_000, RiskSTokens: 4_000_000, EgressBytes: 200_000_000},
		{ID: "org_local_customer_usage_shrinking", Name: "Fictional Shrinking Enterprise", Slug: "fictional-shrinking-enterprise", AccountType: "enterprise",
			CreatedDaysAgo: 300, AnchorDay: 1, Trial: "converted", UsageDays: 186, Growth: 0.8,
			StorageSTokens: 20_000_000, RiskSTokens: 6_000_000, EgressBytes: 150_000_000},
		{ID: "org_local_customer_usage_steady", Name: "Fictional Steady PAYG", Slug: "fictional-steady-payg", AccountType: "payg",
			CreatedDaysAgo: 300, AnchorDay: 10, Trial: "", UsageDays: 186, Growth: 1,
			StorageSTokens: 3_000_000, RiskSTokens: 500_000, EgressBytes: 600_000_000},
		// Usage starts with the current cycle, so the change reads "New".
		{ID: "org_local_customer_usage_started", Name: "Fictional Newly Active Enterprise", Slug: "fictional-newly-active-enterprise", AccountType: "enterprise",
			CreatedDaysAgo: 120, AnchorDay: 15, Trial: "expired", UsageDays: -1, Growth: 1,
			StorageSTokens: 6_000_000, RiskSTokens: 2_000_000, EgressBytes: 80_000_000},
		// Created this week, so there is no previous cycle to compare.
		{ID: "org_local_customer_usage_new", Name: "Fictional New Pro", Slug: "fictional-new-pro", AccountType: "pro",
			CreatedDaysAgo: 3, AnchorDay: 0, Trial: "", UsageDays: 3, Growth: 1,
			StorageSTokens: 2_000_000, RiskSTokens: 300_000, EgressBytes: 30_000_000},
		// No usage: lands in "No usage this cycle".
		{ID: "org_local_customer_usage_idle", Name: "Fictional Idle PAYG", Slug: "fictional-idle-payg", AccountType: "payg",
			CreatedDaysAgo: 200, AnchorDay: 0, Trial: "", UsageDays: 0, Growth: 1,
			StorageSTokens: 0, RiskSTokens: 0, EgressBytes: 0},
		// Excluded from the page: a running enterprise trial, a pro organization
		// that trialled, and a free organization.
		{ID: "org_local_customer_usage_trialing", Name: "Fictional Trialing Enterprise", Slug: "fictional-trialing-enterprise", AccountType: "enterprise",
			CreatedDaysAgo: 20, AnchorDay: 0, Trial: "running", UsageDays: 20, Growth: 1,
			StorageSTokens: 5_000_000, RiskSTokens: 1_000_000, EgressBytes: 50_000_000},
		{ID: "org_local_customer_usage_trialled_pro", Name: "Fictional Trialled Pro", Slug: "fictional-trialled-pro", AccountType: "pro",
			CreatedDaysAgo: 90, AnchorDay: 0, Trial: "expired", UsageDays: 0, Growth: 1,
			StorageSTokens: 0, RiskSTokens: 0, EgressBytes: 0},
		{ID: "org_local_customer_usage_free", Name: "Fictional Free", Slug: "fictional-free", AccountType: "free",
			CreatedDaysAgo: 60, AnchorDay: 0, Trial: "", UsageDays: 0, Growth: 1,
			StorageSTokens: 0, RiskSTokens: 0, EgressBytes: 0},
	}
}

func customerUsageSeedIDs() []string {
	fixtures := customerUsageSeedFixtures()
	ids := make([]string, len(fixtures))
	for i, fixture := range fixtures {
		ids[i] = fixture.ID
	}
	return ids
}

// validateLoopbackHost fails closed on anything but a loopback ClickHouse host,
// so the seed cannot write readings into a shared or production cluster.
func validateLoopbackHost(host string) error {
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("customer usage seed requires a loopback ClickHouse host")
	}
	return nil
}

// RunCustomerUsageSeed seeds fictional paying organizations with about six
// months of metered usage for the admin Customer usage page. It is local-only,
// like RunAdminSeed, and safe to rerun: it replaces its own rows. Usage is
// dated relative to now, so a rerun brings it up to date.
func RunCustomerUsageSeed(ctx context.Context, environment, databaseURL, clickhouseHost string, openClickhouse func(context.Context) (clickhouse.Conn, error)) error {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid customer usage seed database configuration")
	}
	if err := validateAdminSeedTarget(environment, config.ConnConfig); err != nil {
		return err
	}
	if err := validateLoopbackHost(clickhouseHost); err != nil {
		return err
	}
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect customer usage seed database: %w", err)
	}
	defer db.Close()
	conn, err := openClickhouse(ctx)
	if err != nil {
		return fmt.Errorf("connect customer usage seed clickhouse: %w", err)
	}

	// A session-level lock on one dedicated connection holds across both the
	// Postgres transaction and the ClickHouse replacement, so two concurrent
	// seeds cannot both clear the old readings and then both insert.
	lockConn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire customer usage seed lock connection: %w", err)
	}
	defer lockConn.Release()
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock($1)", customerUsageSeedLockID); err != nil {
		return fmt.Errorf("serialize customer usage seed: %w", err)
	}
	defer func() {
		_, _ = lockConn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", customerUsageSeedLockID)
	}()

	now := time.Now().UTC()
	if err := seedCustomerUsageOrganizations(ctx, lockConn, now); err != nil {
		return err
	}
	return seedCustomerUsageReadings(ctx, conn)
}

func seedCustomerUsageOrganizations(ctx context.Context, conn *pgxpool.Conn, now time.Time) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin customer usage seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fixtures := customerUsageSeedFixtures()
	ids := customerUsageSeedIDs()
	// An existing row with a reserved id but another slug or a WorkOS link is
	// not ours. Refuse rather than overwrite it.
	for _, fixture := range fixtures {
		var collision bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM public.organization_metadata WHERE id=$1
 AND (slug<>$2 OR workos_id IS NOT NULL))`, fixture.ID, fixture.Slug).Scan(&collision); err != nil {
			return fmt.Errorf("validate fictional organization identity: %w", err)
		}
		if collision {
			return errors.New("customer usage seed organization identity collision")
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM public.trials WHERE organization_id=ANY($1::text[])`, ids); err != nil {
		return fmt.Errorf("clear fictional trials: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM public.billing_metadata WHERE organization_id=ANY($1::text[])`, ids); err != nil {
		return fmt.Errorf("clear fictional billing metadata: %w", err)
	}

	for _, fixture := range fixtures {
		createdAt := now.AddDate(0, 0, -fixture.CreatedDaysAgo)
		if _, err := tx.Exec(ctx, `INSERT INTO public.organization_metadata
   (id,name,slug,gram_account_type,created_at,disabled_at) VALUES ($1,$2,$3,$4,$5,NULL)
   ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,slug=EXCLUDED.slug,
   gram_account_type=EXCLUDED.gram_account_type,created_at=EXCLUDED.created_at,
   disabled_at=NULL,updated_at=$6`,
			fixture.ID, fixture.Name, fixture.Slug, fixture.AccountType, createdAt, now); err != nil {
			return fmt.Errorf("seed fictional organization: %w", err)
		}
		if fixture.AnchorDay != 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO public.billing_metadata (organization_id,billing_cycle_anchor_day)
   VALUES ($1,$2)`, fixture.ID, fixture.AnchorDay); err != nil {
				return fmt.Errorf("seed fictional billing anchor: %w", err)
			}
		}
		if fixture.Trial == "" {
			continue
		}
		startedAt := createdAt.AddDate(0, 0, 1)
		endsAt := startedAt.AddDate(0, 0, 30)
		var convertedAt *time.Time
		switch fixture.Trial {
		case "running":
			endsAt = now.AddDate(0, 0, 20)
		case "converted":
			convertedAt = new(endsAt.AddDate(0, 0, -2))
		}
		if _, err := tx.Exec(ctx, `INSERT INTO public.trials (organization_id,tier,created_at,ends_at,converted_at,demoted_at)
   VALUES ($1,'enterprise',$2,$3,$4,NULL)`, fixture.ID, startedAt, endsAt, convertedAt); err != nil {
			return fmt.Errorf("seed fictional trial: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit customer usage seed: %w", err)
	}
	return nil
}

// customerUsageReadingsQuery writes one reading per product per day for the
// last N days. Bound parameters: organization id, storage, risk and egress
// base volumes, growth, and N.
const customerUsageReadingsQuery = `
INSERT INTO billing_meter_readings_by_time
  (id, organization_id, project_id, meter_id, operation_id, unit,
   measurement_method, value, occurred_at, produced_at, corrects_reading_id, attributes)
SELECT
  generateUUIDv4(),
  org,
  toUUID('` + customerUsageSeedProject + `'),
  meter,
  concat('local-customer-usage-', org, '-', toString(number), '-', meter),
  if(meter = 'gram.mcp.bandwidth.egress', 'bytes', 'stokens'),
  if(meter = 'gram.mcp.bandwidth.egress', 'http_body_bytes', 'tiktoken_o200k_base'),
  toInt64(multiIf(meter = 'gram.agent_session.storage', storage, meter = 'gram.risk.scan.gitleaks', risk, egress)
    * pow(growth, -number / 30.0) * (0.7 + (cityHash64(org, number, meter) % 60) / 100.0)),
  least(toDateTime64(toStartOfDay(now('UTC')), 9, 'UTC') - toIntervalDay(number) + toIntervalHour(10),
        now64(9, 'UTC') - toIntervalMinute(30)),
  now64(9, 'UTC'),
  NULL,
  map()
FROM (SELECT ? AS org, toInt64(?) AS storage, toInt64(?) AS risk, toInt64(?) AS egress, toFloat64(?) AS growth) AS params
CROSS JOIN numbers(toUInt64(?)) AS days
ARRAY JOIN ['gram.agent_session.storage', 'gram.risk.scan.gitleaks', 'gram.mcp.bandwidth.egress'] AS meter`

func seedCustomerUsageReadings(ctx context.Context, conn clickhouse.Conn) error {
	ids := customerUsageSeedIDs()
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{"async_insert": 0}))
	for _, table := range []string{"billing_meter_daily_summaries", "billing_meter_readings_by_time"} {
		if err := conn.Exec(ctx, "DELETE FROM "+table+" WHERE organization_id IN (?)", ids); err != nil {
			return fmt.Errorf("clear fictional %s: %w", table, err)
		}
	}

	now := time.Now().UTC()
	for _, fixture := range customerUsageSeedFixtures() {
		days := fixture.UsageDays
		if days < 0 {
			// Start exactly at the current cycle, so the previous cycle has none.
			cycleStart := time.Date(now.Year(), now.Month(), fixture.AnchorDay, 0, 0, 0, 0, time.UTC)
			if cycleStart.After(now) {
				cycleStart = cycleStart.AddDate(0, -1, 0)
			}
			days = int(now.Sub(cycleStart)/(24*time.Hour)) + 1
		}
		if days == 0 {
			continue
		}
		if err := conn.Exec(ctx, customerUsageReadingsQuery,
			fixture.ID, fixture.StorageSTokens, fixture.RiskSTokens, fixture.EgressBytes, fixture.Growth, days); err != nil {
			return fmt.Errorf("seed fictional readings: %w", err)
		}
	}
	return nil
}
