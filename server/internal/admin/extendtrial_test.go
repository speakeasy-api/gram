package admin

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

// TestExtendTrial_OrganizationWithNoTrialRow is one half of the pair that pins
// the two causes of a zero-row update apart. This organization exists, so the
// answer is a conflict; TestExtendTrial_UnknownAndMalformedOrganizationIDs
// covers the other half, where it does not and the answer is not-found.
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

// TestExtendTrial_UnknownAndMalformedOrganizationIDs is the other half of that
// pair. Every id here names an organization that does not exist, so every one
// is not-found rather than conflict: an operator who pastes one bad id must get
// the same answer from extend that disable and enable already give them, or the
// second story sends them to inspect a trial when the fault is their clipboard.
func TestExtendTrial_UnknownAndMalformedOrganizationIDs(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// A running trial that must survive every rejected call below. Its slug is
	// one of the ids tried, which pins that the lookup resolves ids only: a
	// slug is not a way in here, and matching one must not turn the answer into
	// a conflict about that organization's perfectly healthy trial.
	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_bystander", name: "Bystander", slug: "ext-bystander", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_bystander", endsAt: seededEndsAt})
	before := readTrial(t, ctx, conn, "org_ext_bystander")

	// The empty id is rejected as a 400 at the HTTP boundary by MinLength(1),
	// which a direct service call does not run; the service-level contract for
	// it is the same not-found as any other id that matches no organization.
	for _, id := range []string{"org_ext_does_not_exist", "", "ext-bystander", "not a valid id"} {
		_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: id, Days: 3})
		requireOopsCode(t, err, oops.CodeNotFound)
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

		// These two pin that the bounds check runs on the wide payload.Days and
		// that the int32 narrowing stays below it. Only a non-HTTP caller can
		// reach the service with a day count this large, which is the caller the
		// handler's own check exists for.
		//
		// The second is the one that matters. 1<<32 + 1 narrows to exactly 1, a
		// value inside the bounds, so a handler that narrowed before checking
		// would accept it and quietly extend by a day. The first narrows to
		// MinInt32 and is rejected either way; it is here so the pair reads as
		// the boundary it is.
		{name: "int32 overflow to a negative", days: math.MaxInt32 + 1, wantErr: true},
		{name: "int32 overflow to a valid day count", days: math.MaxUint32 + 2, wantErr: true},
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

// TestExtendTrialRequestBody_DesignBoundsAreEnforced pins the other copy of the
// bounds. Every other test here calls svc.ExtendTrial directly, which skips the
// generated request decoder, so for the only caller production actually has —
// an HTTP request — none of them touch the validation that runs first. Deleting
// Minimum, Maximum and MinLength(1) from the design would leave the rest of this
// file green.
//
// It needs no database, because the generated validator is a pure function.
func TestExtendTrialRequestBody_DesignBoundsAreEnforced(t *testing.T) {
	t.Parallel()

	id := "org_ext_validate"
	minDays := constants.MinTrialExtensionDays
	maxDays := constants.MaxTrialExtensionDays

	cases := []struct {
		name    string
		id      *string
		days    *int
		wantErr bool
	}{
		// Every field is a pointer in the generated body, so the table has to
		// distinguish "absent" from "present and invalid".
		{name: "at the minimum", id: &id, days: &minDays},
		{name: "at the maximum", id: &id, days: &maxDays},

		{name: "below the minimum", id: &id, days: new(minDays - 1), wantErr: true},
		{name: "above the maximum", id: &id, days: new(maxDays + 1), wantErr: true},
		{name: "negative", id: &id, days: new(-1), wantErr: true},

		{name: "empty id", id: new(""), days: &minDays, wantErr: true},
		{name: "missing id", id: nil, days: &minDays, wantErr: true},
		{name: "missing days", id: &id, days: nil, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := srv.ValidateExtendTrialRequestBody(&srv.ExtendTrialRequestBody{ID: tc.id, Days: tc.days})
			if tc.wantErr {
				require.Error(t, err, "the request decoder must reject this before the handler runs")
				return
			}
			require.NoError(t, err)
		})
	}
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

func TestExtendTrial_WritesAnAuditEntry(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// id, name and slug all differ, so an assertion on one cannot pass on
	// another. The seeded end date is ten days out and the extension is three,
	// so the two dates in the metadata are far apart and neither is now().
	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_audit", name: "org_ext_audit Name", slug: "org_ext_audit-slug", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_audit", endsAt: seededEndsAt})
	before := readTrial(t, ctx, conn, "org_ext_audit")

	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-ext-audit",
		Email:       "operator@example.test",
		OIDCSubject: "oidc-subject-ext-audit",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	countBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)

	_, err = svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_audit", Days: 3})
	require.NoError(t, err)

	countAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)
	require.Equal(t, countBefore+1, countAfter, "an extension must leave a trace in the organization's feed")

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)
	require.Equal(t, "organization", entry.SubjectType)
	require.Equal(t, "org_ext_audit", entry.SubjectID)
	require.Equal(t, "org_ext_audit Name", entry.SubjectDisplay, "the customer's feed must name the organization, not only its id")
	require.Equal(t, "org_ext_audit-slug", entry.SubjectSlug)

	// A trial belongs to the organization, not to one of its projects. There is
	// no foreign key here, so a project id that is set but names nothing would
	// insert and then hide the entry behind the feed's project filter.
	require.False(t, entry.ProjectID.Valid, "a trial extension must not be scoped to a project")

	require.NotNil(t, entry.ActorDisplayName, "the entry must name who extended the trial")
	require.Equal(t, audit.SpeakeasyTeamActorLabel, *entry.ActorDisplayName)

	var metadata struct {
		ExtendedByDays      int       `json:"extended_by_days"`
		PreviousTrialEndsAt time.Time `json:"previous_trial_ends_at"`
		TrialEndsAt         time.Time `json:"trial_ends_at"`
	}
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.Equal(t, 3, metadata.ExtendedByDays)

	// The two dates the database wrote, in the order they happened. Reading them
	// back to front would describe a trial that was cut short.
	after := readTrial(t, ctx, conn, "org_ext_audit")
	require.WithinDuration(t, before.EndsAt.Time, metadata.PreviousTrialEndsAt, 0, "the entry must carry the end date the row held before the extension")
	require.WithinDuration(t, after.EndsAt.Time, metadata.TrialEndsAt, 0, "the entry must carry the end date the row holds now")
	require.True(t, metadata.TrialEndsAt.After(metadata.PreviousTrialEndsAt), "an extension must read forwards")

	// The trial lifecycle event; any other would deliver this to nobody.
	_, err = audittestrepo.New(conn).GetLatestOutboxPayloadByOrg(ctx, audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: "org_ext_audit",
		EventType:      string(events.OrganizationEnterpriseTrialV1.EventType()),
	})
	require.NoError(t, err, "an extension must enqueue an outbox entry on the enterprise trial event")
}

func TestExtendTrial_AuditEntryNamesTheTeamAndNotTheOperator(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_actor", name: "Actor Co", slug: "ext-actor", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_actor", endsAt: time.Now().UTC().Add(10 * 24 * time.Hour)})

	const operatorEmail = "operator@example.test"
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "session-ext-actor",
		Email:       operatorEmail,
		OIDCSubject: "oidc-subject-ext-actor",
		Name:        "Test Operator",
		HD:          "example.test",
	})

	_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_actor", Days: 3})
	require.NoError(t, err)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)

	// The customer reads this feed, so a Speakeasy action carries the collective
	// label. The read-side mask cannot reach this entry: it matches an actor id
	// against a Gram user, and an admin session has an OIDC subject instead.
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, audit.SpeakeasyTeamActorLabel, *entry.ActorDisplayName)

	// Without this, an entry naming nobody at all would satisfy the one above.
	require.Equal(t, "oidc-subject-ext-actor", entry.ActorID,
		"the entry must still record which operator acted, in the field the customer's feed does not render")
	require.Equal(t, "user", entry.ActorType)

	for name, field := range map[string]string{
		"actor display name": conv.PtrValOr(entry.ActorDisplayName, ""),
		"actor slug":         entry.ActorSlug,
		"actor id":           entry.ActorID,
		"subject display":    entry.SubjectDisplay,
		"subject slug":       entry.SubjectSlug,
		"metadata":           string(entry.Metadata),
		"before snapshot":    string(entry.BeforeSnapshot),
		"after snapshot":     string(entry.AfterSnapshot),
	} {
		require.NotContains(t, field, operatorEmail, "the operator's email must not reach the customer's audit feed through the %s", name)
	}
}

// TestExtendTrial_AFailedAuditEntryRollsBackTheExtension pins that the write and
// its entry commit together. An extension that outlived a failed entry would
// leave the feed silent, which is the whole point of this slice.
func TestExtendTrial_AFailedAuditEntryRollsBackTheExtension(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_atomic", name: "Atomic Co", slug: "ext-atomic", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_atomic", endsAt: seededEndsAt})
	before := readTrial(t, ctx, conn, "org_ext_atomic")

	// Failing the audit insert deterministically, from outside the handler. The
	// test owns its database, so the constraint reaches no other test.
	require.NoError(t, audittest.RejectAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialExtended))

	_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_atomic", Days: 3})
	requireOopsCode(t, err, oops.CodeUnexpected)

	after := readTrial(t, ctx, conn, "org_ext_atomic")
	require.Equal(t, before.EndsAt.Time, after.EndsAt.Time,
		"an extension whose audit entry failed must not survive: was %s, now %s", before.EndsAt.Time, after.EndsAt.Time)
	require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)

	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)
	require.Zero(t, count)
}

// TestExtendTrial_ASecondExtensionThatUnblocksOntoAnExtendedTrialSucceeds is the
// concurrency case the CTE created. Two operators extend the same trial in its
// last moments: the first holds a row lock, the second blocks behind it, and the
// seeded end date passes while it waits. By the time the second one unblocks the
// trial has two more weeks on it, so the only correct answer is another
// extension. A conflict here would tell an operator there is no running trial to
// extend, about a trial that was just extended in front of them.
//
// The predicates therefore have to be evaluated by the locking read, which
// re-evaluates against the newest row version once it unblocks. Left on the
// outer UPDATE they run against the statement's own snapshot, which under READ
// COMMITTED still shows the pre-extension row, and a row skipped by the qual
// never reaches the recheck.
func TestExtendTrial_ASecondExtensionThatUnblocksOntoAnExtendedTrialSucceeds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// Close enough that it expires while the second call is blocked, far enough
	// that the first call still finds a running trial.
	seededEndsAt := time.Now().UTC().Add(2 * time.Second)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_race", name: "Race Co", slug: "ext-race", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_race", endsAt: seededEndsAt})

	// The first operator extends by a fortnight and holds the transaction open.
	first := testenv.BeginTx(t, ctx, conn)
	extended, err := trialsRepo.New(first).ExtendTrial(ctx, trialsRepo.ExtendTrialParams{
		OrganizationID: "org_ext_race",
		ExtendByDays:   14,
	})
	require.NoError(t, err)

	second := make(chan error, 1)
	go func() {
		_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_race", Days: 3})
		second <- err
	}()

	testenv.WaitForBlockedBackend(t, ctx, conn)

	// The database's own clock, not the test process's: the seeded date has to
	// have passed where the predicate is evaluated.
	require.Eventually(t, func() bool {
		clock, err := testrepo.New(conn).GetTransactionClockFixture(ctx)
		return err == nil && clock.TransactionNow.Time.After(seededEndsAt)
	}, 30*time.Second, 50*time.Millisecond, "expected the seeded end date to pass while the second extension was blocked")

	select {
	case err := <-second:
		t.Fatalf("the second extension must still be blocked on the first, got %v", err)
	default:
	}

	require.NoError(t, first.Commit(ctx))

	require.NoError(t, <-second,
		"an extension that unblocks onto a trial another operator just extended must not be reported as a conflict")

	after := readTrial(t, ctx, conn, "org_ext_race")
	require.WithinDuration(t, extended.EndsAt.Time.Add(3*24*time.Hour), after.EndsAt.Time, time.Second,
		"the second extension must build on the first: first landed on %s, row now holds %s", extended.EndsAt.Time, after.EndsAt.Time)

	// This is what the lock buys. FOR UPDATE returns the row version it waited
	// for; a plain read returns the one its own snapshot shows, and the entry
	// would claim the trial had been running only to the seeded date.
	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOrganizationEnterpriseTrialExtended)
	require.NoError(t, err)

	var metadata struct {
		PreviousTrialEndsAt time.Time `json:"previous_trial_ends_at"`
	}
	require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
	require.WithinDuration(t, extended.EndsAt.Time, metadata.PreviousTrialEndsAt, 0,
		"the entry must carry the date the first extension left the row on")
}

// TestExtendTrial_ADatabaseFailureIsNotReportedAsAConflict pins the branch that
// tells pgx.ErrNoRows apart from every other error the extend query can answer.
// Collapsing the two would tell an operator whose connection dropped, or whose
// statement deadlocked, that the organization has no running trial to extend,
// and would log nothing about the real fault.
func TestExtendTrial_ADatabaseFailureIsNotReportedAsAConflict(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// A trial that is running in every respect, so a conflict here could only
	// come from the handler collapsing the error, never from the guard.
	seededEndsAt := time.Now().UTC().Add(10 * 24 * time.Hour)
	seedOrg(t, ctx, conn, orgFixture{id: "org_ext_dberr", name: "DB Err Co", slug: "ext-dberr", accountType: "enterprise", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: "org_ext_dberr", endsAt: seededEndsAt})
	before := readTrial(t, ctx, conn, "org_ext_dberr")

	// Failing the write deterministically, from outside the handler, with an
	// error that is emphatically not a missing row.
	testenv.RejectWritesTo(t, ctx, conn, "trials")

	_, err := svc.ExtendTrial(ctx, &gen.ExtendTrialPayload{ID: "org_ext_dberr", Days: 3})
	requireOopsCode(t, err, oops.CodeUnexpected)

	after := readTrial(t, ctx, conn, "org_ext_dberr")
	require.Equal(t, before.EndsAt.Time, after.EndsAt.Time, "a failed extension must not move ends_at")
}
