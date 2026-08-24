package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// statsFixture is one organization and, optionally, the trial hanging off it.
type statsFixture struct {
	id      string
	created time.Duration // age at seed time, always negative
	// disabled is the age of disabled_at at seed time; zero leaves the
	// organization active.
	disabled time.Duration
	// accountType is the organization's gram_account_type; empty seeds free.
	accountType string
	trial       *trialFixture
}

func seedStatsCorpus(t *testing.T, ctx context.Context, conn *pgxpool.Pool, corpus []statsFixture) {
	t.Helper()

	now := time.Now().UTC()
	for _, f := range corpus {
		createdAt := now.Add(f.created)

		var disabledAt *time.Time
		if f.disabled != 0 {
			at := now.Add(f.disabled)
			disabledAt = &at
		}

		seedOrg(t, ctx, conn, orgFixture{
			id:          f.id,
			name:        "Org " + f.id,
			slug:        f.id,
			accountType: f.accountType,
			whitelisted: true,
			disabledAt:  disabledAt,
			createdAt:   &createdAt,
		})

		if f.trial != nil {
			trial := *f.trial
			trial.orgID = f.id
			seedTrial(t, ctx, conn, trial)
		}
	}
}

// TestGetOrganizationStats_Counts pins all seven figures against one corpus in a
// single assertion. No two of the seven are equal, so a handler that deals two
// fields out in the wrong order cannot pass by matching a neighbour's value.
func TestGetOrganizationStats_Counts(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-10 * 24 * time.Hour)
	demotedAt := now.Add(-10 * 24 * time.Hour)

	corpus := []statsFixture{
		// Active, never trialled. Two old, three inside the created window.
		// Account types are spread over the corpus: a customer count that
		// only looked at one of payg or enterprise, or that dropped disabled
		// customers, reads short of the eight below.
		{id: "org_stats_old_a", created: -60 * 24 * time.Hour, accountType: "enterprise"},
		{id: "org_stats_old_b", created: -90 * 24 * time.Hour, accountType: "payg"},
		{id: "org_stats_new_a", created: -2 * 24 * time.Hour, accountType: "payg"},
		{id: "org_stats_new_b", created: -5 * 24 * time.Hour, accountType: "pro"},
		{id: "org_stats_new_c", created: -6 * 24 * time.Hour, accountType: "payg"},

		// Disabled. The second is also inside the created window, so a
		// created count that excludes disabled organizations reads one short.
		{id: "org_stats_disabled_recent_a", created: -60 * 24 * time.Hour, disabled: -24 * time.Hour, accountType: "enterprise"},
		{id: "org_stats_disabled_recent_b", created: -3 * 24 * time.Hour, disabled: -2 * 24 * time.Hour, accountType: "payg"},
		// Disabled long ago but created long ago too, so counting
		// disabled_last_7_days off created_at cannot accidentally agree.
		{id: "org_stats_disabled_old", created: -200 * 24 * time.Hour, disabled: -30 * 24 * time.Hour, accountType: "pro"},

		// Trials that are ending soon.
		{id: "org_stats_trial_soon_a", created: -40 * 24 * time.Hour, accountType: "enterprise", trial: &trialFixture{endsAt: now.Add(2 * 24 * time.Hour)}},
		{id: "org_stats_trial_soon_b", created: -4 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(6 * 24 * time.Hour)}},
		{id: "org_stats_trial_soon_c", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(5 * 24 * time.Hour)}},
		{id: "org_stats_trial_soon_d", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(24 * time.Hour)}},

		// Trials that end inside the window but are not ending soon, because an
		// earlier arm of the ladder claims them first.
		{id: "org_stats_trial_converted", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(3 * 24 * time.Hour), convertedAt: &convertedAt}},
		{id: "org_stats_trial_demoted", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(3 * 24 * time.Hour), demotedAt: &demotedAt}},
		{id: "org_stats_trial_expired", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(-24 * time.Hour)}},
		{id: "org_stats_trial_running", created: -40 * 24 * time.Hour, accountType: "payg", trial: &trialFixture{endsAt: now.Add(30 * 24 * time.Hour)}},
	}

	seedStatsCorpus(t, ctx, conn, corpus)

	res, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)

	require.Equal(t, &gen.AdminOrganizationStats{
		Total:                     16,
		CreatedLast7Days:          5,
		Customers:                 8,
		CustomersCreatedLast7Days: 3,
		TrialsEndingSoon:          4,
		Disabled:                  3,
		DisabledLast7Days:         2,
	}, res)
}

// TestGetOrganizationStats_AgreesWithTheListItNavigatesTo checks the middle cell
// against the rows an operator lands on after clicking it. The figure and the
// filtered list must come from one definition of ending_soon.
func TestGetOrganizationStats_AgreesWithTheListItNavigatesTo(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-10 * 24 * time.Hour)
	demotedAt := now.Add(-10 * 24 * time.Hour)

	seedStatsCorpus(t, ctx, conn, []statsFixture{
		{id: "org_agree_soon_a", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(2 * 24 * time.Hour)}},
		{id: "org_agree_soon_b", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(6 * 24 * time.Hour)}},
		{id: "org_agree_soon_disabled", created: -40 * 24 * time.Hour, disabled: -24 * time.Hour, trial: &trialFixture{endsAt: now.Add(4 * 24 * time.Hour)}},
		{id: "org_agree_converted", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(3 * 24 * time.Hour), convertedAt: &convertedAt}},
		{id: "org_agree_demoted", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(3 * 24 * time.Hour), demotedAt: &demotedAt}},
		{id: "org_agree_expired", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(-24 * time.Hour)}},
		{id: "org_agree_running", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(30 * 24 * time.Hour)}},
	})

	stats, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)

	// The strip counts the platform, so the list it navigates to has to ask for
	// both statuses to see the same rows.
	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{
		TrialStates:    []string{"ending_soon"},
		DisabledStates: []string{"active", "disabled"},
	})
	require.NoError(t, err)

	require.Equal(t, int64(3), stats.TrialsEndingSoon)
	require.Equal(t, stats.TrialsEndingSoon, list.Total, "the strip and the list it navigates to disagree")
	require.Len(t, list.Organizations, 3)
}

// TestGetOrganizationStats_IgnoresFilters pins the endpoint's one structural
// promise: it takes no filters, so the figures are the same however the list
// below them is narrowed.
func TestGetOrganizationStats_IgnoresFilters(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	seedStatsCorpus(t, ctx, conn, []statsFixture{
		{id: "org_filterproof_active", created: -2 * 24 * time.Hour, accountType: "payg"},
		{id: "org_filterproof_disabled", created: -60 * 24 * time.Hour, disabled: -24 * time.Hour},
		{id: "org_filterproof_soon", created: -60 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(2 * 24 * time.Hour)}},
	})

	before, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)

	_, err = svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{
		AccountTypes:   []string{"free"},
		TrialStates:    []string{"expired"},
		DisabledStates: []string{"disabled"},
	})
	require.NoError(t, err)

	after, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)

	require.Equal(t, before, after)
	require.Equal(t, &gen.AdminOrganizationStats{
		Total:                     3,
		CreatedLast7Days:          1,
		Customers:                 1,
		CustomersCreatedLast7Days: 1,
		TrialsEndingSoon:          1,
		Disabled:                  1,
		DisabledLast7Days:         1,
	}, after)
}

// TestGetOrganizationStats_Boundaries pins which side of every edge in the query
// counts, by seeding each fixture exactly on an edge.
//
// The whole test runs inside one transaction, and that is the mechanism rather
// than tidiness. now() is the transaction timestamp, so a fixture offset from a
// value read inside the transaction meets that same value again in the count.
// Reading now() on a separate statement is not enough: the count then opens a
// later transaction, its now() is strictly later, and every exact fixture drifts
// off its edge before the comparison happens, leaving both sides of each
// operator agreeing on the answer.
//
// The cost is that this addresses the repository rather than the handler, which
// holds a pool and cannot be pointed at a transaction. Only the handler's field
// mapping goes uncovered here, and the three tests above pin that.
func TestGetOrganizationStats_Boundaries(t *testing.T) {
	t.Parallel()

	ctx, _, conn := newTestAdminService(t)

	tx := testenv.BeginTx(t, ctx, conn)

	// Postgres computes the edges, so the fixtures cannot disagree with the
	// predicates over what seven days of INTERVAL arithmetic comes to.
	clock, err := testrepo.New(tx).GetTransactionClockFixture(ctx)
	require.NoError(t, err)
	txNow, windowEdge, trialEdge := clock.TransactionNow.Time, clock.SevenDaysAgo.Time, clock.InSevenDays.Time

	// Old enough that no fixture lands in the created window by accident.
	old := windowEdge.Add(-30 * 24 * time.Hour)

	seedBoundaryOrg := func(id string, createdAt time.Time, disabledAt *time.Time) {
		seedOrg(t, ctx, tx, orgFixture{
			id:          id,
			name:        "Org " + id,
			slug:        id,
			whitelisted: true,
			disabledAt:  disabledAt,
			createdAt:   &createdAt,
		})
	}
	seedBoundaryCustomer := func(id string, createdAt time.Time) {
		seedOrg(t, ctx, tx, orgFixture{
			id:          id,
			name:        "Org " + id,
			slug:        id,
			accountType: "enterprise",
			whitelisted: true,
			createdAt:   &createdAt,
		})
	}
	at := func(ts time.Time) *time.Time { return &ts }

	// created_at > now() - INTERVAL '7 days': exactly seven days old is out.
	seedBoundaryOrg("org_bound_created_inside", windowEdge.Add(time.Hour), nil)
	seedBoundaryOrg("org_bound_created_exact", windowEdge, nil)
	seedBoundaryOrg("org_bound_created_outside", windowEdge.Add(-time.Hour), nil)

	// The same edge again for customers_created_last_7_days, which has its own
	// predicate and so its own chance to include the boundary.
	seedBoundaryCustomer("org_bound_customer_inside", windowEdge.Add(time.Hour))
	seedBoundaryCustomer("org_bound_customer_exact", windowEdge)
	seedBoundaryCustomer("org_bound_customer_outside", windowEdge.Add(-time.Hour))

	// disabled_at > now() - INTERVAL '7 days': the same edge, the other column.
	seedBoundaryOrg("org_bound_disabled_inside", old, at(windowEdge.Add(time.Hour)))
	seedBoundaryOrg("org_bound_disabled_exact", old, at(windowEdge))
	seedBoundaryOrg("org_bound_disabled_outside", old, at(windowEdge.Add(-time.Hour)))

	// The ladder's two date arms. ends_at <= now() + INTERVAL '7 days' takes the
	// exact row, and ends_at <= now() claims a trial ending on the instant for
	// expired before ending_soon can have it.
	for _, trial := range []trialFixture{
		{orgID: "org_bound_trial_exact", endsAt: trialEdge},
		{orgID: "org_bound_trial_outside", endsAt: trialEdge.Add(time.Hour)},
		{orgID: "org_bound_trial_expiry_exact", endsAt: txNow},
		{orgID: "org_bound_trial_expiry_inside", endsAt: txNow.Add(time.Hour)},
	} {
		seedBoundaryOrg(trial.orgID, old, nil)
		seedTrial(t, ctx, tx, trial)
	}

	row, err := repo.New(tx).AdminGetOrganizationStats(ctx)
	require.NoError(t, err)

	require.Equal(t, repo.AdminGetOrganizationStatsRow{
		Total:                     13,
		CreatedLast7Days:          2,
		Customers:                 3,
		CustomersCreatedLast7Days: 1,
		TrialsEndingSoon:          2,
		Disabled:                  3,
		DisabledLast7Days:         1,
	}, row)
}

// TestGetOrganizationStats_EmptyPlatform pins zeros rather than an error: the
// aggregate always produces a row, and the strip has to render on a fresh
// platform.
func TestGetOrganizationStats_EmptyPlatform(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	res, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminOrganizationStats{}, res)
}
