package admin

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// readTrial reads the trials row straight from the database. Going through the
// API instead would only ever show ends_at, and this slice has to prove that
// four other columns did not move.
func readTrial(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) trialsRepo.Trial {
	t.Helper()

	row, err := trialsRepo.New(conn).GetTrial(ctx, orgID)
	require.NoError(t, err)
	return row
}

func TestExtendTrial_MovesEndsAtByExactlyTheGivenDays(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// The seeded end date is deliberately not a multiple of the extension, and
	// is far from both now() and the fixture's created_at (30 days back). An
	// implementation that added the interval to the wrong base would land on a
	// different date rather than accidentally on the right one:
	//
	//   correct              now + 10d + 3d = now + 13d
	//   added to now()       now + 3d
	//   added to created_at  now - 27d
	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext", name: "Ext Co", slug: "ext-co", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext", endsAt: seededEndsAt})
	before := readTrial(t, ctx, conn, "org_ext")

	res, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext", Days: 3})
	require.NoError(t, err)
	require.Equal(t, "org_ext", res.ID, "the response must describe the organization that was written")

	after := readTrial(t, ctx, conn, "org_ext")
	require.Equal(t, 3*24*time.Hour, after.EndsAt.Time.Sub(before.EndsAt.Time),
		"ends_at must move by exactly the days asked for, from its previous value: was %s, now %s", before.EndsAt.Time, after.EndsAt.Time)

	// Everything else on the row is either the record of how the trial began or
	// the record of how it ended, and an extension is neither.
	require.Equal(t, before.Tier, after.Tier)
	require.Equal(t, before.CreatedAt.Time, after.CreatedAt.Time, "extending must not restamp created_at")
	require.False(t, after.ConvertedAt.Valid, "extending must not convert the trial")
	require.False(t, after.DemotedAt.Valid, "extending must not demote the trial")
	require.True(t, after.UpdatedAt.Time.After(before.UpdatedAt.Time),
		"extending must move updated_at: was %s, now %s", before.UpdatedAt.Time, after.UpdatedAt.Time)

	// The new date reaches the operator through all three surfaces.
	require.NotNil(t, res.TrialEndsAt)
	fromResponse, err := time.Parse(time.RFC3339, *res.TrialEndsAt)
	require.NoError(t, err)
	require.WithinDuration(t, after.EndsAt.Time, fromResponse, time.Second, "the response must carry the new end date")

	detail, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_ext"})
	require.NoError(t, err)
	require.NotNil(t, detail.TrialEndsAt)
	require.Equal(t, *res.TrialEndsAt, *detail.TrialEndsAt, "the detail endpoint must agree with the write")
	require.Equal(t, "running", *detail.TrialState)

	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, list.Organizations, 1)
	require.NotNil(t, list.Organizations[0].TrialEndsAt)
	require.Equal(t, *res.TrialEndsAt, *list.Organizations[0].TrialEndsAt, "the list endpoint must agree with the write")
}

// TestExtendTrial_OnlyARunningTrialCanBeExtended walks the whole state space of
// the guard rather than a sample of it. converted_at and demoted_at are each
// null or not-null, and ends_at is each side of now, which is eight rows; only
// the one where all three are satisfied may move. Seeding one polarity of a
// nullable column makes half of its mutations unreachable, exactly as seeding
// one polarity of a boolean does.
func TestExtendTrial_OnlyARunningTrialCanBeExtended(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	convertedAt := now.Add(-96 * time.Hour)
	demotedAt := now.Add(-72 * time.Hour)

	cases := []struct {
		name      string
		orgID     string
		expired   bool
		converted bool
		demoted   bool
		wantMove  bool
	}{
		{name: "running", orgID: "org_ext_running", wantMove: true},

		// The row the sweeper has not reached yet. It still reads as armed:
		// both stamps are null and only the date says otherwise.
		{name: "expired not yet demoted", orgID: "org_ext_expired", expired: true},

		{name: "converted", orgID: "org_ext_converted", converted: true},
		{name: "converted and expired", orgID: "org_ext_converted_expired", converted: true, expired: true},
		{name: "demoted", orgID: "org_ext_demoted", demoted: true},
		{name: "demoted and expired", orgID: "org_ext_demoted_expired", demoted: true, expired: true},
		{name: "converted after demotion", orgID: "org_ext_both", converted: true, demoted: true},
		{name: "converted after demotion and expired", orgID: "org_ext_both_expired", converted: true, demoted: true, expired: true},
	}

	for _, tc := range cases {
		endsAt := now.Add(10 * 24 * time.Hour)
		if tc.expired {
			endsAt = now.Add(-10 * 24 * time.Hour)
		}
		f := trialFixture{orgID: tc.orgID, endsAt: endsAt}
		if tc.converted {
			f.convertedAt = &convertedAt
		}
		if tc.demoted {
			f.demotedAt = &demotedAt
		}

		seedOrg(t, ctx, conn, orgFixture{id: tc.orgID, name: tc.orgID, slug: tc.orgID, accountType: "enterprise", whitelisted: true})
		seedTrial(t, ctx, conn, f)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Safe to run alongside the other cases: each owns its own
			// organization, and the assertions read that row alone.
			t.Parallel()

			before := readTrial(t, ctx, conn, tc.orgID)

			_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: tc.orgID, Days: 3})

			after := readTrial(t, ctx, conn, tc.orgID)
			if tc.wantMove {
				require.NoError(t, err)
				require.Equal(t, 3*24*time.Hour, after.EndsAt.Time.Sub(before.EndsAt.Time))
				return
			}

			requireOopsCode(t, err, oops.CodeConflict)
			require.Equal(t, before.EndsAt.Time, after.EndsAt.Time,
				"a trial that is not running must not move: was %s, now %s", before.EndsAt.Time, after.EndsAt.Time)
			require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time, "a rejected extension must not touch updated_at")
		})
	}
}

func TestExtendTrial_OrganizationWithNoTrialRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_no_trial", name: "No Trial", slug: "no-trial", whitelisted: true})

	_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_no_trial", Days: 3})
	requireOopsCode(t, err, oops.CodeConflict)

	// Extend moves an end date; it must never be a way to arm a trial that was
	// never granted, which is the auth flow's job.
	_, err = trialsRepo.New(conn).GetTrial(ctx, "org_ext_no_trial")
	require.Error(t, err, "a rejected extension must not create a trial row")
}

func TestExtendTrial_UnknownAndMalformedOrganizationIDs(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// A running trial that must survive every rejected call below.
	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_bystander", name: "Bystander", slug: "ext-bystander", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_bystander", endsAt: seededEndsAt})
	before := readTrial(t, ctx, conn, "org_ext_bystander")

	// The empty id is rejected as a 400 at the HTTP boundary by MinLength(1),
	// which a direct service call does not run; the service-level contract for
	// an id that matches nothing is the same conflict as any other.
	for _, id := range []string{"org_ext_does_not_exist", "", "ext-bystander", "not a valid id"} {
		_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: id, Days: 3})
		requireOopsCode(t, err, oops.CodeConflict)
	}

	after := readTrial(t, ctx, conn, "org_ext_bystander")
	require.Equal(t, before.EndsAt.Time, after.EndsAt.Time, "an unmatched id must not extend anything")
	require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
}

func TestExtendTrial_DayCountBounds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	cases := []struct {
		name    string
		days    int
		wantErr bool
	}{
		// A negative count would shorten a trial through an endpoint named
		// extend, and zero would report success having moved nothing but
		// updated_at.
		{name: "large negative", days: -365, wantErr: true},
		{name: "minus one", days: -1, wantErr: true},
		{name: "zero", days: 0, wantErr: true},

		{name: "minimum", days: constants.MinTrialExtensionDays},
		{name: "maximum", days: constants.MaxTrialExtensionDays},

		{name: "one past the maximum", days: constants.MaxTrialExtensionDays + 1, wantErr: true},
		{name: "far past the maximum", days: 100000, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			orgID := "org_ext_bound_" + tc.name
			seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
			seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, whitelisted: true})
			seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: seededEndsAt})
			before := readTrial(t, ctx, conn, orgID)

			_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: orgID, Days: tc.days})

			after := readTrial(t, ctx, conn, orgID)
			if tc.wantErr {
				requireOopsCode(t, err, oops.CodeInvalid)
				require.Equal(t, before.EndsAt.Time, after.EndsAt.Time,
					"a rejected day count must not move ends_at: was %s, now %s", before.EndsAt.Time, after.EndsAt.Time)
				require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
				return
			}

			require.NoError(t, err)
			require.Equal(t, time.Duration(tc.days)*24*time.Hour, after.EndsAt.Time.Sub(before.EndsAt.Time))
		})
	}
}

// TestExtendTrial_LeavesTheOrganizationTierAlone is the regression the scope of
// this slice turns on. Demotion drops gram_account_type to free and clears
// whitelisted; extending must do neither, in either polarity, or an operator
// buying a customer more time would take their access away instead.
func TestExtendTrial_LeavesTheOrganizationTierAlone(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	cases := []struct {
		accountType string
		whitelisted bool
	}{
		{accountType: "enterprise", whitelisted: true},
		{accountType: "enterprise", whitelisted: false},
		{accountType: "free", whitelisted: true},
		{accountType: "pro", whitelisted: false},
	}

	for _, tc := range cases {
		orgID := "org_ext_tier_" + tc.accountType
		if tc.whitelisted {
			orgID += "_wl"
		}

		seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
		seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: tc.accountType, whitelisted: tc.whitelisted})
		seedTrial(t, ctx, conn, trialFixture{orgID: orgID, endsAt: seededEndsAt})

		_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: orgID, Days: 3})
		require.NoError(t, err)

		// The extension must really have landed, or the assertions below hold
		// for the uninteresting reason that nothing happened at all.
		after := readTrial(t, ctx, conn, orgID)
		require.WithinDuration(t, seededEndsAt.Add(3*24*time.Hour), after.EndsAt.Time, time.Second)

		state := readOrgState(t, ctx, conn, orgID)
		require.Equal(t, tc.accountType, state.GramAccountType, "extending must leave gram_account_type at %s on %s", tc.accountType, orgID)
		require.Equal(t, tc.whitelisted, state.Whitelisted, "extending must leave whitelisted at %v on %s", tc.whitelisted, orgID)
		require.False(t, state.DisabledAt.Valid, "extending must not disable the organization")
	}
}

func TestExtendTrial_TouchesOnlyTheTargetRow(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// The neighbour's end date differs from the target's, so a write that
	// spilled onto it would be visible as a move rather than hidden by the two
	// rows happening to agree.
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_target", name: "Target", slug: "ext-target", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_target", endsAt: time.Now().UTC().Add(10 * 24 * time.Hour)})
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_neighbour", name: "Neighbour", slug: "ext-neighbour", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_neighbour", endsAt: time.Now().UTC().Add(21 * 24 * time.Hour)})

	neighbourBefore := readTrial(t, ctx, conn, "org_ext_neighbour")

	_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_target", Days: 3})
	require.NoError(t, err)

	neighbourAfter := readTrial(t, ctx, conn, "org_ext_neighbour")
	require.Equal(t, neighbourBefore.EndsAt.Time, neighbourAfter.EndsAt.Time, "extending must not spill onto other trials")
	require.Equal(t, neighbourBefore.UpdatedAt.Time, neighbourAfter.UpdatedAt.Time)
}

func TestExtendTrial_ExtensionsAccumulate(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// Two extensions of different sizes: adding to the current end date rather
	// than to now() is what makes the second one land on the sum. The sizes
	// differ so that a mutation which used the first count twice, or the second
	// twice, would miss.
	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_twice", name: "Twice", slug: "ext-twice", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_twice", endsAt: seededEndsAt})

	_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_twice", Days: 3})
	require.NoError(t, err)
	_, err = svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_twice", Days: 5})
	require.NoError(t, err)

	after := readTrial(t, ctx, conn, "org_ext_twice")
	require.WithinDuration(t, seededEndsAt.Add(8*24*time.Hour), after.EndsAt.Time, time.Second,
		"a second extension must build on the first")
}
