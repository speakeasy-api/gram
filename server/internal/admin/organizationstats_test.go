package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

// statsFixture is one organization and, optionally, the trial hanging off it.
type statsFixture struct {
	id      string
	created time.Duration // age at seed time, always negative
	// disabled is the age of disabled_at at seed time; zero leaves the
	// organization active.
	disabled time.Duration
	trial    *trialFixture
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

// TestGetOrganizationStats_Counts pins all five figures against one corpus in a
// single assertion. No two of the five are equal, so a handler that deals two
// fields out in the wrong order cannot pass by matching a neighbour's value.
func TestGetOrganizationStats_Counts(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-10 * 24 * time.Hour)
	demotedAt := now.Add(-10 * 24 * time.Hour)

	corpus := []statsFixture{
		// Active, never trialled. Two old, three inside the created window.
		{id: "org_stats_old_a", created: -60 * 24 * time.Hour},
		{id: "org_stats_old_b", created: -90 * 24 * time.Hour},
		{id: "org_stats_new_a", created: -2 * 24 * time.Hour},
		{id: "org_stats_new_b", created: -5 * 24 * time.Hour},
		{id: "org_stats_new_c", created: -6 * 24 * time.Hour},

		// Disabled. The second is also inside the created window, so a
		// created count that excludes disabled organizations reads one short.
		{id: "org_stats_disabled_recent_a", created: -60 * 24 * time.Hour, disabled: -24 * time.Hour},
		{id: "org_stats_disabled_recent_b", created: -3 * 24 * time.Hour, disabled: -2 * 24 * time.Hour},
		// Disabled long ago but created long ago too, so counting
		// disabled_last_7_days off created_at cannot accidentally agree.
		{id: "org_stats_disabled_old", created: -200 * 24 * time.Hour, disabled: -30 * 24 * time.Hour},

		// Trials that are ending soon.
		{id: "org_stats_trial_soon_a", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(2 * 24 * time.Hour)}},
		{id: "org_stats_trial_soon_b", created: -4 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(6 * 24 * time.Hour)}},
		{id: "org_stats_trial_soon_c", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(5 * 24 * time.Hour)}},
		{id: "org_stats_trial_soon_d", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(24 * time.Hour)}},

		// Trials that end inside the window but are not ending soon, because an
		// earlier arm of the ladder claims them first.
		{id: "org_stats_trial_converted", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(3 * 24 * time.Hour), convertedAt: &convertedAt}},
		{id: "org_stats_trial_demoted", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(3 * 24 * time.Hour), demotedAt: &demotedAt}},
		{id: "org_stats_trial_expired", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(-24 * time.Hour)}},
		{id: "org_stats_trial_running", created: -40 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(30 * 24 * time.Hour)}},
	}

	seedStatsCorpus(t, ctx, conn, corpus)

	res, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)

	require.Equal(t, &gen.AdminOrganizationStats{
		Total:             16,
		CreatedLast7Days:  5,
		TrialsEndingSoon:  4,
		Disabled:          3,
		DisabledLast7Days: 2,
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
		{id: "org_filterproof_active", created: -2 * 24 * time.Hour},
		{id: "org_filterproof_disabled", created: -60 * 24 * time.Hour, disabled: -24 * time.Hour},
		{id: "org_filterproof_soon", created: -60 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(2 * 24 * time.Hour)}},
	})

	before, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)

	_, err = svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{
		TrialStates:    []string{"expired"},
		DisabledStates: []string{"disabled"},
	})
	require.NoError(t, err)

	after, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)

	require.Equal(t, before, after)
	require.Equal(t, &gen.AdminOrganizationStats{
		Total:             3,
		CreatedLast7Days:  1,
		TrialsEndingSoon:  1,
		Disabled:          1,
		DisabledLast7Days: 1,
	}, after)
}

// TestGetOrganizationStats_Boundaries pins which side of each 7-day edge counts.
//
// The two windows exclude their boundary: an organization created or disabled
// exactly seven days ago is out. now() has already moved on by the time the
// count runs, so a row seeded at exactly seven days is the only exact case a
// test can state without racing the clock.
//
// The trial edge is inclusive, inherited from the ladder's `<=`: a trial ending
// exactly seven days out is ending_soon.
func TestGetOrganizationStats_Boundaries(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	const window = 7 * 24 * time.Hour

	seedStatsCorpus(t, ctx, conn, []statsFixture{
		{id: "org_bound_created_inside", created: -(window - time.Hour)},
		{id: "org_bound_created_exact", created: -window},
		{id: "org_bound_created_outside", created: -(window + time.Hour)},

		{id: "org_bound_disabled_inside", created: -60 * 24 * time.Hour, disabled: -(window - time.Hour)},
		{id: "org_bound_disabled_exact", created: -60 * 24 * time.Hour, disabled: -window},
		{id: "org_bound_disabled_outside", created: -60 * 24 * time.Hour, disabled: -(window + time.Hour)},

		{id: "org_bound_trial_exact", created: -60 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(window)}},
		{id: "org_bound_trial_outside", created: -60 * 24 * time.Hour, trial: &trialFixture{endsAt: now.Add(window + time.Hour)}},
	})

	res, err := svc.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
	require.NoError(t, err)

	require.Equal(t, &gen.AdminOrganizationStats{
		Total:             8,
		CreatedLast7Days:  1,
		TrialsEndingSoon:  1,
		Disabled:          3,
		DisabledLast7Days: 1,
	}, res)
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
