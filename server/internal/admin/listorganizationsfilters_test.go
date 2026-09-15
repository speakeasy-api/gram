package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

const (
	fltFreeNone      = "org_flt_free_none"
	fltFreeRunning   = "org_flt_free_running"
	fltFreeConverted = "org_flt_free_converted"
	fltProEnding     = "org_flt_pro_ending"
	fltProExpired    = "org_flt_pro_expired"
	fltEntDemoted    = "org_flt_ent_demoted"
	fltEntConverted  = "org_flt_ent_converted"
	fltFreeWindowIn  = "org_flt_free_window_in"
	fltFreeWindowOut = "org_flt_free_window_out"
	fltFreeOff       = "org_flt_free_off"
	fltProOffRunning = "org_flt_pro_off_running"
)

type filterFixture struct {
	id          string
	accountType string
	// trial is nil for an organization that never trialled, which the ladder
	// reports as "none".
	trial      *trialFixture
	trialState string
	disabled   bool
}

// filterCorpus spreads account type, trial state and disabled state across
// organizations so that no two of the three filters select the same rows. Every
// trial state appears, converted and demoted appear on more than one row each so
// that a count taken over the wrong state cannot land on the right number, and
// two rows straddle the ending_soon boundary by an hour.
func filterCorpus(now time.Time) []filterFixture {
	demotedAt := now.Add(-72 * time.Hour)
	convertedAt := now.Add(-96 * time.Hour)

	return []filterFixture{
		{id: fltFreeNone, accountType: "free", trialState: "none"},
		{id: fltFreeRunning, accountType: "free", trialState: "running", trial: &trialFixture{endsAt: now.Add(30 * 24 * time.Hour)}},
		{id: fltFreeConverted, accountType: "free", trialState: "converted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), convertedAt: &convertedAt}},
		{id: fltProEnding, accountType: "pro", trialState: "ending_soon", trial: &trialFixture{endsAt: now.Add(24 * time.Hour)}},
		{id: fltProExpired, accountType: "pro", trialState: "expired", trial: &trialFixture{endsAt: now.Add(-24 * time.Hour)}},
		{id: fltEntDemoted, accountType: "enterprise", trialState: "demoted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), demotedAt: &demotedAt}},
		{id: fltEntConverted, accountType: "enterprise", trialState: "converted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), convertedAt: &convertedAt}},
		{id: fltFreeWindowIn, accountType: "free", trialState: "ending_soon", trial: &trialFixture{endsAt: now.Add(endingSoonWindow - time.Hour)}},
		{id: fltFreeWindowOut, accountType: "free", trialState: "running", trial: &trialFixture{endsAt: now.Add(endingSoonWindow + time.Hour)}},
		{id: fltFreeOff, accountType: "free", trialState: "none", disabled: true},
		{id: fltProOffRunning, accountType: "pro", trialState: "running", trial: &trialFixture{endsAt: now.Add(30 * 24 * time.Hour)}, disabled: true},
	}
}

func seedFilterCorpus(t *testing.T, ctx context.Context, conn *pgxpool.Pool, corpus []filterFixture, disabledAt time.Time) {
	t.Helper()

	for _, f := range corpus {
		org := orgFixture{id: f.id, name: "Org " + f.id, slug: f.id, accountType: f.accountType, whitelisted: true}
		if f.disabled {
			org.disabledAt = &disabledAt
		}
		seedOrg(t, ctx, conn, org)

		if f.trial == nil {
			continue
		}

		trial := *f.trial
		trial.orgID = f.id
		seedTrial(t, ctx, conn, trial)
	}
}

func filterIDs(corpus []filterFixture, keep func(filterFixture) bool) []string {
	out := make([]string, 0, len(corpus))
	for _, f := range corpus {
		if keep(f) {
			out = append(out, f.id)
		}
	}
	return out
}

// requireFilterMatches asserts both halves of the response. The total comes from
// a second query with its own copy of the filter arms, so asserting the rows
// alone would leave that copy untested.
func requireFilterMatches(t *testing.T, ctx context.Context, svc *Service, payload *gen.ListOrganizationsPayload, wantIDs []string, label string) {
	t.Helper()

	res, err := svc.ListOrganizations(ctx, payload)
	require.NoError(t, err, "listing organizations for %s", label)

	got := make([]string, 0, len(res.Organizations))
	for _, o := range res.Organizations {
		got = append(got, o.ID)
	}
	require.ElementsMatch(t, wantIDs, got, "rows matched by %s", label)
	require.Equal(t, int64(len(wantIDs)), res.Total, "total for %s", label)
}

func TestListOrganizations_SetFilters(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	corpus := filterCorpus(now)
	seedFilterCorpus(t, ctx, conn, corpus, now.Add(-24*time.Hour))

	active := filterIDs(corpus, func(f filterFixture) bool { return !f.disabled })
	all := filterIDs(corpus, func(filterFixture) bool { return true })

	// Pin the corpus before filtering on it. A fixture whose trial dates do not
	// produce the state its row claims would make every expectation below wrong
	// in the same direction, and the filter tests would still pass.
	t.Run("the corpus reports the trial states it claims", func(t *testing.T) {
		t.Parallel()

		res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{IncludeDisabled: ptrTo(true)})
		require.NoError(t, err)
		require.Len(t, res.Organizations, len(corpus))

		byID := map[string]*gen.AdminOrganization{}
		for _, o := range res.Organizations {
			byID[o.ID] = o
		}
		for _, f := range corpus {
			org := byID[f.id]
			require.NotNil(t, org, "organization %s missing from the list", f.id)
			require.NotNil(t, org.TrialState, "organization %s has no trial state", f.id)
			require.Equal(t, f.trialState, *org.TrialState, "trial state for %s", f.id)
		}
	})

	cases := []struct {
		name    string
		payload *gen.ListOrganizationsPayload
		want    []string
	}{
		{
			name:    "no filters keeps every active organization",
			payload: &gen.ListOrganizationsPayload{},
			want:    active,
		},
		{
			name:    "account_types matches every listed type",
			payload: &gen.ListOrganizationsPayload{AccountTypes: []string{"enterprise", "pro"}},
			want:    []string{fltEntDemoted, fltEntConverted, fltProEnding, fltProExpired},
		},
		{
			name:    "account_types ignores the order of the list",
			payload: &gen.ListOrganizationsPayload{AccountTypes: []string{"pro", "enterprise"}},
			want:    []string{fltEntDemoted, fltEntConverted, fltProEnding, fltProExpired},
		},
		{
			name:    "an unknown account type alongside a known one keeps the known one",
			payload: &gen.ListOrganizationsPayload{AccountTypes: []string{"platinum", "pro"}},
			want:    []string{fltProEnding, fltProExpired},
		},
		{
			name:    "the account_type scalar still filters on its own",
			payload: &gen.ListOrganizationsPayload{AccountType: ptrTo("pro")},
			want:    []string{fltProEnding, fltProExpired},
		},
		{
			name:    "the account_type scalar joins the set rather than replacing it",
			payload: &gen.ListOrganizationsPayload{AccountType: ptrTo("free"), AccountTypes: []string{"enterprise"}},
			want:    []string{fltFreeNone, fltFreeRunning, fltFreeConverted, fltFreeWindowIn, fltFreeWindowOut, fltEntDemoted, fltEntConverted},
		},
		{
			name:    "trial_states running spans the far side of the ending_soon boundary",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"running"}},
			want:    []string{fltFreeRunning, fltFreeWindowOut},
		},
		{
			name:    "trial_states ending_soon spans the near side of the boundary",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"ending_soon"}},
			want:    []string{fltProEnding, fltFreeWindowIn},
		},
		{
			name:    "trial_states matches every listed state",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"running", "ending_soon"}},
			want:    []string{fltFreeRunning, fltFreeWindowOut, fltProEnding, fltFreeWindowIn},
		},
		{
			name:    "trial_states none matches the organizations that never trialled",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"none"}},
			want:    []string{fltFreeNone},
		},
		{
			name:    "trial_states converted takes precedence over the trial end date",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"converted"}},
			want:    []string{fltFreeConverted, fltEntConverted},
		},
		{
			name:    "trial_states demoted takes precedence over the trial end date",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"demoted"}},
			want:    []string{fltEntDemoted},
		},
		{
			name:    "trial_states expired excludes trials that ended after converting or demoting",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"expired"}},
			want:    []string{fltProExpired},
		},
		{
			name:    "disabled_states disabled drops the active organizations",
			payload: &gen.ListOrganizationsPayload{DisabledStates: []string{"disabled"}},
			want:    []string{fltFreeOff, fltProOffRunning},
		},
		{
			name:    "disabled_states with both members keeps everything",
			payload: &gen.ListOrganizationsPayload{DisabledStates: []string{"active", "disabled"}},
			want:    all,
		},
		{
			name:    "include_disabled still widens on its own",
			payload: &gen.ListOrganizationsPayload{IncludeDisabled: ptrTo(true)},
			want:    all,
		},
		{
			name:    "disabled_states overrides include_disabled",
			payload: &gen.ListOrganizationsPayload{IncludeDisabled: ptrTo(true), DisabledStates: []string{"active"}},
			want:    active,
		},
		{
			name: "the three filters compose",
			payload: &gen.ListOrganizationsPayload{
				AccountTypes:   []string{"pro"},
				TrialStates:    []string{"running"},
				DisabledStates: []string{"disabled"},
			},
			want: []string{fltProOffRunning},
		},
		{
			name:    "an unknown account type matches nothing without failing",
			payload: &gen.ListOrganizationsPayload{AccountTypes: []string{"platinum"}},
			want:    nil,
		},
		{
			name:    "an unknown trial state matches nothing without failing",
			payload: &gen.ListOrganizationsPayload{TrialStates: []string{"paused"}},
			want:    nil,
		},
		{
			name:    "an unknown disabled state matches nothing without failing",
			payload: &gen.ListOrganizationsPayload{DisabledStates: []string{"archived"}},
			want:    nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			requireFilterMatches(t, ctx, svc, c.payload, c.want, c.name)
		})
	}
}

// The filters have to survive the paging modes: the page query applies
// trial_states outside the CTE that the cursor predicate sits in, and the count
// query has no cursor predicate at all.
func TestListOrganizations_SetFiltersSurvivePaging(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	corpus := filterCorpus(now)
	seedFilterCorpus(t, ctx, conn, corpus, now.Add(-24*time.Hour))

	filtered := &gen.ListOrganizationsPayload{
		AccountTypes: []string{"free"},
		TrialStates:  []string{"running", "ending_soon", "none"},
		Limit:        ptrTo(2),
	}

	// Cursor mode. Walk the whole filtered set two rows at a time.
	var walked []string
	var cursor *string
	for range len(corpus) {
		payload := *filtered
		payload.Cursor = cursor

		res, err := svc.ListOrganizations(ctx, &payload)
		require.NoError(t, err)
		require.Equal(t, int64(4), res.Total, "the total counts the filtered set, not the page")

		for _, o := range res.Organizations {
			walked = append(walked, o.ID)
		}
		if res.NextCursor == nil {
			break
		}
		cursor = res.NextCursor
	}
	require.ElementsMatch(t, []string{fltFreeNone, fltFreeRunning, fltFreeWindowIn, fltFreeWindowOut}, walked, "the cursor walk over a filtered set")

	// Offset mode. The second page holds the remainder of the same set.
	page2 := *filtered
	page2.Sort = ptrTo("name")
	page2.Page = ptrTo(2)

	res, err := svc.ListOrganizations(ctx, &page2)
	require.NoError(t, err)
	require.Equal(t, int64(4), res.Total)
	require.Len(t, res.Organizations, 2)
}
