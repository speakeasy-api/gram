package admin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

type orgFixture struct {
	id                string
	name              string
	slug              string
	accountType       string
	workosID          *string
	workosLastEventID *string
	whitelisted       bool
	disabledAt        *time.Time
	createdAt         *time.Time
}

// The database handle is an interface rather than a pool so a seeder can share
// one transaction with the query it is seeding for, which is what lets a
// boundary fixture meet the same now() the predicate reads.
func seedOrg(t *testing.T, ctx context.Context, conn testrepo.DBTX, f orgFixture) {
	t.Helper()

	if f.accountType == "" {
		f.accountType = "free"
	}

	params := testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 f.id,
		Name:               f.name,
		Slug:               f.slug,
		GramAccountType:    f.accountType,
		WorkosID:           conv.PtrToPGText(f.workosID),
		Whitelisted:        f.whitelisted,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now().UTC()),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(time.Now().UTC().Add(14 * 24 * time.Hour)),
		DisabledAt:         conv.PtrToPGTimestamptz(f.disabledAt),
		CreatedAt:          conv.PtrToPGTimestamptz(f.createdAt),
	}

	err := testrepo.New(conn).CreateOrganizationMetadataFixture(ctx, params)
	require.NoError(t, err)

	// The webhook cursor is seeded separately so it stays out of the INSERT.
	if f.workosLastEventID != nil {
		err := testrepo.New(conn).SetWorkosLastEventIDFixture(ctx, testrepo.SetWorkosLastEventIDFixtureParams{
			ID:                f.id,
			WorkosLastEventID: conv.PtrToPGText(f.workosLastEventID),
		})
		require.NoError(t, err)
	}
}

func seedMembership(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, userID string) {
	t.Helper()

	err := testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         pgtype.Text{String: userID, Valid: true},
	})
	require.NoError(t, err)
}

func TestGetOrganization_ByID(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_get_id", name: "Get Co", slug: "get-co", accountType: "pro", whitelisted: true})
	seedMembership(t, ctx, conn, "org_get_id", "user_x")

	res, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_get_id"})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "org_get_id", res.ID)
	require.Equal(t, "Get Co", res.Name)
	require.Equal(t, "pro", res.AccountType)
	require.Equal(t, 1, res.MemberCount)
}

func TestGetOrganization_BySlug(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_get_slug", name: "Slug Co", slug: "slug-co", whitelisted: true})

	res, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "slug-co"})
	require.NoError(t, err)
	require.Equal(t, "org_get_slug", res.ID)
	require.Equal(t, "slug-co", res.Slug)
}

func TestUpdateOrganization_AccountTypeAndWhitelisted(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_upd",
		name:        "Upd Co",
		slug:        "upd-co",
		accountType: "free",
		whitelisted: true,
	})
	newType := "pro"
	notWhitelisted := false
	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
		ID:          "org_upd",
		AccountType: &newType,
		Whitelisted: &notWhitelisted,
	})
	require.NoError(t, err)
	require.Equal(t, "pro", res.AccountType)
	require.False(t, res.Whitelisted)
}

func TestUpdateOrganization_AccountTypeOnly(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_upd_partial",
		name:        "Upd Partial",
		slug:        "upd-partial",
		accountType: "free",
		whitelisted: true,
	})
	newType := "payg"
	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
		ID:          "org_upd_partial",
		AccountType: &newType,
	})
	require.NoError(t, err)
	require.Equal(t, "payg", res.AccountType)
	require.True(t, res.Whitelisted, "whitelisted should be untouched")

}

func TestUpdateOrganization_NoFieldsRejected(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	// Must exist, or a missing guard is masked by the handler's not-found.
	seedOrg(t, ctx, conn, orgFixture{id: "org_x", name: "X Co", slug: "x-co", accountType: "free"})

	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_x"})
	require.Error(t, err)
	require.ErrorContains(t, err, "at least one of")
}

func postUpdateOrg(t *testing.T, ctx context.Context, svc *Service, body string) (*gen.AdminOrganization, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/admin/organization.update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	payload, err := srv.DecodeUpdateOrganizationRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)(req)
	if err != nil {
		return nil, err
	}

	return svc.UpdateOrganization(ctx, payload)
}

// The two paths accepting different sets is the defect this slice prevents, so
// dropping the allow-list from either one has to turn a test red.
func TestUpdateOrganization_AccountTypeAllowList(t *testing.T) {
	t.Parallel()

	t.Run("refuses an out-of-list value and writes nothing", func(t *testing.T) {
		t.Parallel()

		ctx, svc, conn := newTestAdminService(t)
		seedOrg(t, ctx, conn, orgFixture{id: "org_single_bad", name: "Single Bad", slug: "single-bad", accountType: "free", whitelisted: true})
		before := readOrgState(t, ctx, conn, "org_single_bad")

		for _, bad := range []string{"gold", "Free", "FREE", ""} {
			_, err := postUpdateOrg(t, ctx, svc, `{"id":"org_single_bad","account_type":"`+bad+`"}`)
			require.Error(t, err, "the single path must refuse %q just as the bulk path does", bad)
			if bad != "" {
				require.ErrorContains(t, err, bad, "the refusal must name the offending value")
			}
		}

		after := readOrgState(t, ctx, conn, "org_single_bad")
		require.Equal(t, "free", after.GramAccountType)
		require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
	})

	t.Run("accepts every allowed value", func(t *testing.T) {
		t.Parallel()

		ctx, svc, conn := newTestAdminService(t)
		seedOrg(t, ctx, conn, orgFixture{id: "org_single_ok", name: "Single Ok", slug: "single-ok", accountType: "free"})

		for _, good := range []string{"free", "pro", "enterprise"} {
			res, err := postUpdateOrg(t, ctx, svc, `{"id":"org_single_ok","account_type":"`+good+`"}`)
			require.NoError(t, err)
			require.Equal(t, good, res.AccountType)
		}
	})

	t.Run("whitelisted alone still works", func(t *testing.T) {
		t.Parallel()

		ctx, svc, conn := newTestAdminService(t)
		seedOrg(t, ctx, conn, orgFixture{id: "org_single_wl", name: "Single WL", slug: "single-wl", accountType: "pro", whitelisted: false})

		res, err := postUpdateOrg(t, ctx, svc, `{"id":"org_single_wl","whitelisted":true}`)
		require.NoError(t, err)
		require.True(t, res.Whitelisted)
		require.Equal(t, "pro", res.AccountType, "a whitelist-only write must leave the account type alone")

		_, err = postUpdateOrg(t, ctx, svc, `{"id":"org_single_wl"}`)
		require.Error(t, err, "a body with neither account_type nor whitelisted must still be rejected")
	})

	t.Run("the service refuses what the decoder would", func(t *testing.T) {
		t.Parallel()

		ctx, svc, conn := newTestAdminService(t)
		seedOrg(t, ctx, conn, orgFixture{id: "org_single_svc", name: "Single Svc", slug: "single-svc", accountType: "free"})
		before := readOrgState(t, ctx, conn, "org_single_svc")

		for _, bad := range []string{"gold", "Free", "FREE", ""} {
			_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
				ID:          "org_single_svc",
				AccountType: &bad,
			})
			require.Error(t, err, "the service must refuse %q even with no decoder in front of it", bad)
			if bad != "" {
				require.ErrorContains(t, err, bad, "the refusal must name the offending value")
			}
		}

		after := readOrgState(t, ctx, conn, "org_single_svc")
		require.Equal(t, "free", after.GramAccountType)
		require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
	})
}

func TestUpdateOrganization_AccountTypeMarksTrialInactive(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier

	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_trial_convert",
		name:        "Trial Convert",
		slug:        "trial-convert",
		accountType: "enterprise",
		whitelisted: true,
	})
	newType := "enterprise"
	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
		ID:          "org_trial_convert",
		AccountType: &newType,
	})
	require.NoError(t, err)
	require.Equal(t, "enterprise", res.AccountType)
	require.Equal(t, []string{"org_trial_convert"}, notifier.inactive)
}

func TestUpdateOrganization_WhitelistedOnlyDoesNotMarkTrialInactive(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	notifier := &fakeTrialNotifier{}
	svc.trial = notifier

	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_trial_whitelist",
		name:        "Trial Whitelist",
		slug:        "trial-whitelist",
		accountType: "enterprise",
		whitelisted: true,
	})
	notWhitelisted := false
	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
		ID:          "org_trial_whitelist",
		Whitelisted: &notWhitelisted,
	})
	require.NoError(t, err)
	require.False(t, res.Whitelisted)
	require.Empty(t, notifier.inactive)
}

func TestUpdateOrganization_TrialInactiveFailureDoesNotFailUpdate(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	notifier := &fakeTrialNotifier{inactiveErr: errors.New("loops unavailable")}
	svc.trial = notifier

	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_trial_notify_fail",
		name:        "Trial Notify Fail",
		slug:        "trial-notify-fail",
		accountType: "enterprise",
		whitelisted: true,
	})
	newType := "pro"
	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{
		ID:          "org_trial_notify_fail",
		AccountType: &newType,
	})
	require.NoError(t, err)
	require.Equal(t, "pro", res.AccountType)
	require.Equal(t, []string{"org_trial_notify_fail"}, notifier.inactive)
}

func TestGetOrganization_NotFound(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "does-not-exist"})
	require.Error(t, err)
}

func TestListOrganizations_Empty(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Organizations)
	require.Nil(t, res.NextCursor)
	require.Equal(t, int64(0), res.Total)
}

func TestListOrganizations_DefaultExcludesDisabled(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: "org_active", name: "Active Co", slug: "active-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_disabled", name: "Disabled Co", slug: "disabled-co", whitelisted: true, disabledAt: &now})

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 1)
	require.Equal(t, "org_active", res.Organizations[0].ID)
	require.Nil(t, res.Organizations[0].DisabledAt)
}

func TestListOrganizations_IncludeDisabled(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: "org_a", name: "Alpha", slug: "alpha", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_b", name: "Bravo", slug: "bravo", whitelisted: true, disabledAt: &now})

	include := true
	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{IncludeDisabled: &include})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 2)

	byID := map[string]*gen.AdminOrganization{}
	for _, o := range res.Organizations {
		byID[o.ID] = o
	}
	require.NotNil(t, byID["org_b"].DisabledAt)
}

func TestListOrganizations_SearchByNameOrSlug(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_match_name", name: "Acme Industries", slug: "acme-ind", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_match_slug", name: "Globex", slug: "acme-rivals", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_nope", name: "Unrelated", slug: "no-match", whitelisted: true})

	q := "acme"
	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Q: &q})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 2)

	ids := []string{res.Organizations[0].ID, res.Organizations[1].ID}
	require.Contains(t, ids, "org_match_name")
	require.Contains(t, ids, "org_match_slug")
}

func TestListOrganizations_FilterByAccountType(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_pro", name: "Pro Co", slug: "pro-co", accountType: "pro", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_free", name: "Free Co", slug: "free-co", accountType: "free", whitelisted: true})

	at := "pro"
	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{AccountType: &at})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 1)
	require.Equal(t, "org_pro", res.Organizations[0].ID)
	require.Equal(t, "pro", res.Organizations[0].AccountType)
}

func TestListOrganizations_MemberCount(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_members", name: "Members Co", slug: "members-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_solo", name: "Solo Co", slug: "solo-co", whitelisted: true})

	seedMembership(t, ctx, conn, "org_members", "user_1")
	seedMembership(t, ctx, conn, "org_members", "user_2")
	seedMembership(t, ctx, conn, "org_members", "user_3")

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 2)

	byID := map[string]*gen.AdminOrganization{}
	for _, o := range res.Organizations {
		byID[o.ID] = o
	}
	require.Equal(t, 3, byID["org_members"].MemberCount)
	require.Equal(t, 0, byID["org_solo"].MemberCount)
}

func TestListOrganizations_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	for _, id := range []string{"org_a", "org_b", "org_c", "org_d"} {
		seedOrg(t, ctx, conn, orgFixture{id: id, name: "Org " + id, slug: id, whitelisted: true})
	}

	limit := 2

	page1, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Limit: &limit})
	require.NoError(t, err)
	require.Len(t, page1.Organizations, 2)
	require.NotNil(t, page1.NextCursor)
	require.Equal(t, "org_a", page1.Organizations[0].ID)
	require.Equal(t, "org_b", page1.Organizations[1].ID)
	require.Equal(t, "org_b", *page1.NextCursor)
	require.Equal(t, int64(4), page1.Total, "the cursor walk reports the full match count too")

	page2, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Limit: &limit, Cursor: page1.NextCursor})
	require.NoError(t, err)
	require.Len(t, page2.Organizations, 2)
	require.Equal(t, "org_c", page2.Organizations[0].ID)
	require.Equal(t, "org_d", page2.Organizations[1].ID)
	// The last page is full and exhausts the table, so it ends the walk.
	require.Nil(t, page2.NextCursor)
}

func TestListOrganizations_FullPageWithFilterEndsTheWalk(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_a", name: "Alpha", slug: "alpha", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_b", name: "Bravo", slug: "bravo", whitelisted: true})
	// Sorts after the page but the filter drops it, so it is not a next page.
	seedOrg(t, ctx, conn, orgFixture{id: "org_c", name: "Charlie", slug: "charlie", accountType: "pro", whitelisted: true})

	limit := 2
	at := "free"

	page, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Limit: &limit, AccountType: &at})
	require.NoError(t, err)
	require.Len(t, page.Organizations, 2)
	require.Nil(t, page.NextCursor)
}

type trialFixture struct {
	orgID string
	// tier defaults to enterprise, the only tier the application writes today.
	tier        string
	endsAt      time.Time
	convertedAt *time.Time
	demotedAt   *time.Time
}

func seedTrial(t *testing.T, ctx context.Context, conn trialsRepo.DBTX, f trialFixture) {
	t.Helper()

	if f.tier == "" {
		f.tier = "enterprise"
	}

	err := trialsRepo.New(conn).InsertTrialFixture(ctx, trialsRepo.InsertTrialFixtureParams{
		OrganizationID: f.orgID,
		Tier:           f.tier,
		CreatedAt:      conv.ToPGTimestamptz(time.Now().UTC().Add(-30 * 24 * time.Hour)),
		EndsAt:         conv.ToPGTimestamptz(f.endsAt),
		ConvertedAt:    conv.PtrToPGTimestamptz(f.convertedAt),
		DemotedAt:      conv.PtrToPGTimestamptz(f.demotedAt),
	})
	require.NoError(t, err)
}

// endingSoonWindow mirrors the INTERVAL in the trial_state CASE; the fixtures below straddle it by an hour to pin it.
const endingSoonWindow = 7 * 24 * time.Hour

type trialStateCase struct {
	orgID string
	want  string
	trial *trialFixture
}

func TestGetOrganization_TrialDetails(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	endsAt := time.Date(2035, time.March, 4, 5, 6, 7, 0, time.UTC)
	convertedAt := time.Date(2025, time.April, 5, 6, 7, 8, 0, time.UTC)
	demotedAt := time.Date(2025, time.May, 6, 7, 8, 9, 0, time.UTC)
	tier := "enterprise"
	endsISO := endsAt.In(time.Local).Format(time.RFC3339)
	convertedISO := convertedAt.In(time.Local).Format(time.RFC3339)
	demotedISO := demotedAt.In(time.Local).Format(time.RFC3339)

	cases := []struct {
		name        string
		orgID       string
		trial       *trialFixture
		wantTier    *string
		wantEnds    *string
		wantConvert *string
		wantDemote  *string
	}{
		{
			name:     "live",
			orgID:    "org_trial_detail_live",
			trial:    &trialFixture{tier: "enterprise", endsAt: endsAt},
			wantTier: &tier,
			wantEnds: &endsISO,
		},
		{
			name:        "converted",
			orgID:       "org_trial_detail_converted",
			trial:       &trialFixture{tier: "enterprise", endsAt: endsAt, convertedAt: &convertedAt},
			wantTier:    &tier,
			wantEnds:    &endsISO,
			wantConvert: &convertedISO,
		},
		{
			name:       "demoted",
			orgID:      "org_trial_detail_demoted",
			trial:      &trialFixture{tier: "enterprise", endsAt: endsAt, demotedAt: &demotedAt},
			wantTier:   &tier,
			wantEnds:   &endsISO,
			wantDemote: &demotedISO,
		},
		{name: "no trial", orgID: "org_trial_detail_none"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			seedOrg(t, ctx, conn, orgFixture{id: c.orgID, name: c.name, slug: c.orgID, whitelisted: true})
			if c.trial != nil {
				f := *c.trial
				f.orgID = c.orgID
				seedTrial(t, ctx, conn, f)
			}

			got, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: c.orgID})
			require.NoError(t, err)
			fields := []struct {
				name string
				want *string
				got  *string
			}{
				{name: "trial tier", want: c.wantTier, got: got.TrialTier},
				{name: "trial end", want: c.wantEnds, got: got.TrialEndsAt},
				{name: "trial conversion", want: c.wantConvert, got: got.TrialConvertedAt},
				{name: "trial demotion", want: c.wantDemote, got: got.TrialDemotedAt},
			}
			for _, field := range fields {
				if field.want == nil {
					require.Nil(t, field.got, field.name)
					continue
				}
				require.NotNil(t, field.got, field.name)
				if field.name == "trial tier" {
					require.Equal(t, *field.want, *field.got, field.name)
					continue
				}
				wantTime, err := time.Parse(time.RFC3339, *field.want)
				require.NoError(t, err, field.name)
				gotTime, err := time.Parse(time.RFC3339, *field.got)
				require.NoError(t, err, field.name)
				require.True(t, wantTime.Equal(gotTime), field.name)
			}
		})
	}
}

func TestAdminListOrganizations_TrialState(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	now := time.Now().UTC()
	demotedAt := now.Add(-72 * time.Hour)
	convertedAt := now.Add(-96 * time.Hour)

	cases := []trialStateCase{
		{orgID: "org_trial_none", want: "none"},
		{orgID: "org_trial_running", want: "running", trial: &trialFixture{endsAt: now.Add(30 * 24 * time.Hour)}},
		{orgID: "org_trial_ending_soon", want: "ending_soon", trial: &trialFixture{endsAt: now.Add(24 * time.Hour)}},
		{orgID: "org_trial_expired", want: "expired", trial: &trialFixture{endsAt: now.Add(-24 * time.Hour)}},
		{orgID: "org_trial_demoted", want: "demoted", trial: &trialFixture{endsAt: now.Add(12 * 24 * time.Hour), demotedAt: &demotedAt}},
		{orgID: "org_trial_converted", want: "converted", trial: &trialFixture{endsAt: now.Add(18 * 24 * time.Hour), convertedAt: &convertedAt}},
		{orgID: "org_trial_demoted_past", want: "demoted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), demotedAt: &demotedAt}},
		{orgID: "org_trial_converted_past", want: "converted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), convertedAt: &convertedAt}},

		// MarkTrialConverted guards on converted_at alone, so a demoted trial can later convert. Paying beats demoted.
		{orgID: "org_trial_converted_after_demotion", want: "converted", trial: &trialFixture{endsAt: now.Add(-10 * 24 * time.Hour), demotedAt: &demotedAt, convertedAt: &convertedAt}},

		{orgID: "org_trial_window_inside", want: "ending_soon", trial: &trialFixture{endsAt: now.Add(endingSoonWindow - time.Hour)}},
		{orgID: "org_trial_window_outside", want: "running", trial: &trialFixture{endsAt: now.Add(endingSoonWindow + time.Hour)}},
	}

	for _, c := range cases {
		seedOrg(t, ctx, conn, orgFixture{id: c.orgID, name: "Org " + c.orgID, slug: c.orgID, whitelisted: true})
		if c.trial != nil {
			f := *c.trial
			f.orgID = c.orgID
			seedTrial(t, ctx, conn, f)
		}
	}

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.Len(t, res.Organizations, len(cases))

	byID := map[string]*gen.AdminOrganization{}
	for _, o := range res.Organizations {
		byID[o.ID] = o
	}

	for _, c := range cases {
		org := byID[c.orgID]
		require.NotNil(t, org, "organization %s missing from the list", c.orgID)
		require.NotNil(t, org.TrialState, "organization %s has no trial state", c.orgID)
		require.Equal(t, c.want, *org.TrialState, "list trial state for %s", c.orgID)

		detail, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: c.orgID})
		require.NoError(t, err)
		require.NotNil(t, detail.TrialState)
		require.Equal(t, c.want, *detail.TrialState, "detail trial state for %s", c.orgID)
		require.Equal(t, org.TrialEndsAt, detail.TrialEndsAt, "trial end date for %s", c.orgID)

		if c.trial == nil {
			require.Nil(t, org.TrialEndsAt, "organization %s never trialled, so it has no trial end date", c.orgID)
			continue
		}

		require.NotNil(t, org.TrialEndsAt, "organization %s should report its trial end date", c.orgID)
		got, err := time.Parse(time.RFC3339, *org.TrialEndsAt)
		require.NoError(t, err, "parsing trial end date for %s", c.orgID)
		require.WithinDuration(t, c.trial.endsAt, got, time.Second, "trial end date for %s", c.orgID)
	}

	// Expand only: the old free trial fields stay on the API.
	require.NotNil(t, byID["org_trial_none"].FreeTrialStartedAt)
	require.NotNil(t, byID["org_trial_none"].FreeTrialEndsAt)
}

type searchByIDCase struct {
	name    string
	q       string
	wantIDs []string
}

// requireSearchMatches asserts the exact set of organizations a payload returns, ignoring order.
func requireSearchMatches(t *testing.T, ctx context.Context, svc *Service, payload *gen.ListOrganizationsPayload, wantIDs []string, label string) *gen.AdminListOrganizationsResult {
	t.Helper()

	res, err := svc.ListOrganizations(ctx, payload)
	require.NoError(t, err, "listing organizations for %s", label)

	got := make([]string, len(res.Organizations))
	for i, o := range res.Organizations {
		got[i] = o.ID
	}
	require.ElementsMatch(t, wantIDs, got, "organizations matched by %s", label)

	return res
}

// requireSearchMatchesWithTotal adds the pager total to requireSearchMatches.
// The count query carries its own copy of the filter arms, so a page that agrees
// with the fixtures while the total disagrees is a real failure this catches and
// the rows alone cannot. Only for payloads with no cursor: the count query has no
// cursor predicate, so its total counts past one deliberately.
func requireSearchMatchesWithTotal(t *testing.T, ctx context.Context, svc *Service, payload *gen.ListOrganizationsPayload, wantIDs []string, label string) {
	t.Helper()

	require.Nil(t, payload.Cursor, "total assertion is meaningless with a cursor, for %s", label)

	res := requireSearchMatches(t, ctx, svc, payload, wantIDs, label)
	require.Equal(t, int64(len(wantIDs)), res.Total, "total matched by %s", label)
}

func TestListOrganizations_SearchByID(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	const (
		alphaID   = "org_search_id_alpha"
		bravoID   = "org_search_id_bravo"
		charlieID = "org_search_id_charlie"

		bravoWorkosID = "org_workos_placeholder_bravo"
	)

	workosID := bravoWorkosID
	seedOrg(t, ctx, conn, orgFixture{id: alphaID, name: "Alpha Holdings", slug: "alpha-holdings", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: bravoID, name: "Bravo Holdings", slug: "bravo-holdings", workosID: &workosID, whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: charlieID, name: "Charlie Networks", slug: "charlie-networks", whitelisted: true})

	cases := []searchByIDCase{
		{name: "full organization id", q: alphaID, wantIDs: []string{alphaID}},
		{name: "full workos id", q: bravoWorkosID, wantIDs: []string{bravoID}},
		{name: "id no organization holds", q: "org_search_id_delta", wantIDs: nil},
		// The id arms are exact, so a fragment of a real id matches nothing.
		{name: "fragment of an organization id", q: "search_id_alpha", wantIDs: nil},
		{name: "fragment of a workos id", q: "placeholder_bravo", wantIDs: nil},
		{name: "name, unchanged by the id arms", q: "charlie", wantIDs: []string{charlieID}},
	}

	for _, c := range cases {
		requireSearchMatches(t, ctx, svc, &gen.ListOrganizationsPayload{Q: conv.PtrEmpty(c.q)}, c.wantIDs, c.name)
	}
}

func TestListOrganizations_SearchByIDIgnoresCase(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// A real WorkOS id embeds an uppercase ULID, and a log pipeline hands the operator a lowercased copy of it.
	const (
		mixedID       = "org_search_id_MixedCase"
		mixedWorkosID = "org_workos_placeholder_MixedCase"
	)

	workosID := mixedWorkosID
	seedOrg(t, ctx, conn, orgFixture{id: mixedID, name: "Mixed Case Holdings", slug: "mixed-case-holdings", workosID: &workosID, whitelisted: true})

	// Lowering only the column matches the folded form and misses the other two;
	// lowering only the term matches nothing but the folded form either.
	cases := []searchByIDCase{
		{name: "organization id at its own casing", q: mixedID, wantIDs: []string{mixedID}},
		{name: "organization id case folded", q: strings.ToLower(mixedID), wantIDs: []string{mixedID}},
		{name: "organization id upper cased", q: strings.ToUpper(mixedID), wantIDs: []string{mixedID}},
		{name: "workos id at its own casing", q: mixedWorkosID, wantIDs: []string{mixedID}},
		{name: "workos id case folded", q: strings.ToLower(mixedWorkosID), wantIDs: []string{mixedID}},
		{name: "workos id upper cased", q: strings.ToUpper(mixedWorkosID), wantIDs: []string{mixedID}},

		// Case folding is all the id arms gained: they still compare whole and exactly.
		{name: "an id differing by more than its casing", q: "org_search_id_MixedCases", wantIDs: nil},
		{name: "fragment of an organization id, case folded", q: "search_id_mixedcase", wantIDs: nil},
		{name: "fragment of a workos id, case folded", q: "placeholder_mixedcase", wantIDs: nil},

		{name: "name case folded", q: "mixed case holdings", wantIDs: []string{mixedID}},
		{name: "slug upper cased", q: "MIXED-CASE-HOLDINGS", wantIDs: []string{mixedID}},
	}

	for _, c := range cases {
		requireSearchMatchesWithTotal(t, ctx, svc, &gen.ListOrganizationsPayload{Q: conv.PtrEmpty(c.q)}, c.wantIDs, c.name)
	}
}

type searchFilterCase struct {
	name        string
	q           string
	accountType string
	trialStates []string
	cursor      string
	wantIDs     []string
}

// An id match escapes the disabled filter and no other: it is still one arm of
// the q group as far as every remaining filter is concerned. TestListOrganizations_SearchByIDFindsADisabledOrganization
// covers the one filter it does escape.
func TestListOrganizations_SearchByIDRespectsFilters(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	const (
		proID    = "org_search_id_pro"
		cursorID = "org_search_id_cursor"
		trialID  = "org_search_id_trial"
	)

	seedOrg(t, ctx, conn, orgFixture{id: proID, name: "Pro Holdings", slug: "pro-holdings", accountType: "pro", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: cursorID, name: "Cursor Holdings", slug: "cursor-holdings", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: trialID, name: "Trial Holdings", slug: "trial-holdings", whitelisted: true})
	seedTrial(t, ctx, conn, trialFixture{orgID: trialID, endsAt: time.Now().UTC().Add(30 * 24 * time.Hour)})

	cases := []searchFilterCase{
		{name: "pro organization by id, filtered to free", q: proID, accountType: "free", wantIDs: nil},
		{name: "pro organization by id, filtered to pro", q: proID, accountType: "pro", wantIDs: []string{proID}},

		{name: "running trial by id, filtered to expired", q: trialID, trialStates: []string{"expired"}, wantIDs: nil},
		{name: "running trial by id, filtered to running", q: trialID, trialStates: []string{"running"}, wantIDs: []string{trialID}},

		{name: "organization by id, at or before the cursor", q: cursorID, cursor: cursorID, wantIDs: nil},
		{name: "organization by id, after the cursor", q: cursorID, cursor: "org_search_id_a", wantIDs: []string{cursorID}},
	}

	for _, c := range cases {
		payload := &gen.ListOrganizationsPayload{
			Q:           conv.PtrEmpty(c.q),
			AccountType: conv.PtrEmpty(c.accountType),
			TrialStates: c.trialStates,
			Cursor:      conv.PtrEmpty(c.cursor),
		}

		// The count query has no cursor predicate, so only the cursor-free cases
		// can hold it to a total. The rest must, or the account type and trial
		// state arms are checked in the page query alone.
		if c.cursor != "" {
			requireSearchMatches(t, ctx, svc, payload, c.wantIDs, c.name)
			continue
		}

		requireSearchMatchesWithTotal(t, ctx, svc, payload, c.wantIDs, c.name)
	}
}

type searchDisabledCase struct {
	name            string
	q               string
	includeDisabled bool
	disabledStates  []string
	wantIDs         []string
}

// Pasting the id of a suspended organization is a leading reason to paste an id
// at all, so an exact id match reaches one whatever the disabled filter says.
// Only the id arms escape it; the name and slug arms do not.
func TestListOrganizations_SearchByIDFindsADisabledOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// Both ids are mixed case: the bypass carries its own copy of the id arms, so
	// it needs its own proof that each folds case rather than borrowing the one
	// TestListOrganizations_SearchByIDIgnoresCase gives an active organization.
	const (
		disabledID = "org_search_id_Disabled"
		activeID   = "org_search_id_active"

		disabledWorkosID = "org_workos_placeholder_Disabled"
	)

	disabledAt := time.Now().UTC().Add(-24 * time.Hour)
	workosID := disabledWorkosID
	seedOrg(t, ctx, conn, orgFixture{id: disabledID, name: "Disabled Holdings", slug: "disabled-holdings", workosID: &workosID, whitelisted: true, disabledAt: &disabledAt})
	seedOrg(t, ctx, conn, orgFixture{id: activeID, name: "Active Holdings", slug: "active-holdings", whitelisted: true})

	cases := []searchDisabledCase{
		{name: "disabled organization by id, disabled excluded", q: disabledID, wantIDs: []string{disabledID}},
		{name: "disabled organization by id, disabled included", q: disabledID, includeDisabled: true, wantIDs: []string{disabledID}},
		{name: "disabled organization by id, disabled_states active", q: disabledID, disabledStates: []string{"active"}, wantIDs: []string{disabledID}},
		{name: "disabled organization by lowercased id, disabled excluded", q: strings.ToLower(disabledID), wantIDs: []string{disabledID}},
		{name: "disabled organization by workos id, disabled excluded", q: disabledWorkosID, wantIDs: []string{disabledID}},
		{name: "disabled organization by lowercased workos id, disabled excluded", q: strings.ToLower(disabledWorkosID), wantIDs: []string{disabledID}},

		// The bypass is the two id arms, not the whole q group.
		{name: "disabled organization by name, disabled excluded", q: "Disabled Holdings", wantIDs: nil},
		{name: "disabled organization by slug, disabled excluded", q: "disabled-holdings", wantIDs: nil},
		{name: "disabled organization by name, disabled included", q: "Disabled Holdings", includeDisabled: true, wantIDs: []string{disabledID}},

		// Symmetric: the id arms escape the filter rather than widening it to disabled rows.
		{name: "active organization by id, disabled_states disabled", q: activeID, disabledStates: []string{"disabled"}, wantIDs: []string{activeID}},
		{name: "active organization by name, disabled_states disabled", q: "Active Holdings", disabledStates: []string{"disabled"}, wantIDs: nil},
	}

	for _, c := range cases {
		payload := &gen.ListOrganizationsPayload{
			Q:               conv.PtrEmpty(c.q),
			IncludeDisabled: conv.PtrEmpty(c.includeDisabled),
			DisabledStates:  c.disabledStates,
		}
		requireSearchMatchesWithTotal(t, ctx, svc, payload, c.wantIDs, c.name)
	}
}

// Every id in both id spaces contains underscores, and _ is a single-character
// LIKE wildcard, so an unescaped pasted id draws incidental matches out of the
// name and slug arms.
func TestListOrganizations_SearchEscapesLikeMetacharacters(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	const (
		pastedID     = "org_01_alpha"
		incidentalID = "org_escape_incidental"
		percentID    = "org_escape_percent"
		nameDecoyID  = "org_escape_name_decoy"
		slugDecoyID  = "org_escape_slug_decoy"
		backslashID  = "org_escape_backslash"
	)

	seedOrg(t, ctx, conn, orgFixture{id: pastedID, name: "Alpha Systems", slug: "alpha-systems", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: incidentalID, name: "Acme org 01 alpha", slug: "acme-org-01-alpha", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: percentID, name: "Percent% Holdings", slug: "under_score-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: nameDecoyID, name: "PercentX Holdings", slug: "name-decoy-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: slugDecoyID, name: "Slug Decoy", slug: "underXscore-co", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: backslashID, name: `Back\slash_Co`, slug: "back-slash-co", whitelisted: true})

	cases := []searchByIDCase{
		// The motivating case: unescaped, this id also matches the other organization's name and slug.
		{name: "a pasted id draws no incidental match", q: pastedID, wantIDs: []string{pastedID}},

		// One decoy per arm, so escaping the name arm alone or the slug arm alone still fails.
		{name: "percent in the name arm is literal", q: "Percent% Holdings", wantIDs: []string{percentID}},
		{name: "underscore in the slug arm is literal", q: "under_score-co", wantIDs: []string{percentID}},

		// Backslash is the escape character itself, so it has to be escaped before the two wildcards.
		{name: "backslash in the term is literal", q: `Back\slash_Co`, wantIDs: []string{backslashID}},

		{name: "a substring still matches", q: "Holdings", wantIDs: []string{percentID, nameDecoyID}},
	}

	for _, c := range cases {
		requireSearchMatchesWithTotal(t, ctx, svc, &gen.ListOrganizationsPayload{Q: conv.PtrEmpty(c.q)}, c.wantIDs, c.name)
	}
}

// A pasted id commonly arrives with the newline that ended the line it was
// copied from. The dashboard trims before it asks, but a direct API caller does not.
func TestListOrganizations_SearchTermIsTrimmed(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	const (
		trimID = "org_search_trim_alpha"
		// Matches none of the terms below, so only a request carrying no term at
		// all returns it alongside the first. Without it the whitespace-only case
		// cannot tell a dropped term from a term that still happened to match.
		bystanderID = "org_search_trim_bravo"
	)

	workosID := "org_workos_placeholder_trim"
	seedOrg(t, ctx, conn, orgFixture{id: trimID, name: "Trim Holdings", slug: "trim-holdings", workosID: &workosID, whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: bystanderID, name: "Bystander Systems", slug: "bystander-systems", whitelisted: true})

	cases := []searchByIDCase{
		{name: "leading space", q: " " + trimID, wantIDs: []string{trimID}},
		{name: "trailing newline", q: trimID + "\n", wantIDs: []string{trimID}},
		{name: "trailing space", q: trimID + " ", wantIDs: []string{trimID}},
		{name: "surrounded by mixed whitespace", q: "\t " + trimID + " \r\n", wantIDs: []string{trimID}},
		{name: "workos id with a trailing newline", q: workosID + "\n", wantIDs: []string{trimID}},
		{name: "name with a trailing newline", q: "Trim Holdings\n", wantIDs: []string{trimID}},
		// Whitespace alone is no search term at all rather than a term nothing holds.
		{name: "whitespace only", q: " \n\t ", wantIDs: []string{trimID, bystanderID}},
	}

	for _, c := range cases {
		requireSearchMatchesWithTotal(t, ctx, svc, &gen.ListOrganizationsPayload{Q: conv.PtrEmpty(c.q)}, c.wantIDs, c.name)
	}
}

//go:fix inline
func ptrTo[T any](v T) *T {
	return new(v)
}

func reversed(ids []string) []string {
	out := slices.Clone(ids)
	slices.Reverse(out)
	return out
}

// requireOrder asserts the exact ordered ids a payload returns.
func requireOrder(t *testing.T, ctx context.Context, svc *Service, payload *gen.ListOrganizationsPayload, wantIDs []string, label string) *gen.AdminListOrganizationsResult {
	t.Helper()

	res, err := svc.ListOrganizations(ctx, payload)
	require.NoError(t, err, "listing organizations for %s", label)

	got := make([]string, 0, len(res.Organizations))
	for _, o := range res.Organizations {
		got = append(got, o.ID)
	}
	require.Equal(t, wantIDs, got, "organization order for %s", label)

	return res
}

// The sort fixtures. Every sortable column ranks these four differently, so a
// ladder arm wired to the wrong column shows up as a wrong order. No column may
// rank them in id order either way round, or the tiebreaker alone reproduces the
// expected order and a missing ladder arm reads as correct.
const (
	sortOrgA = "org_sort_a"
	sortOrgB = "org_sort_b"
	sortOrgC = "org_sort_c"
	sortOrgD = "org_sort_d"
)

type sortFixture struct {
	org         orgFixture
	memberCount int
	trialEndsAt time.Time
}

func seedSortFixtures(t *testing.T, ctx context.Context, conn *pgxpool.Pool) {
	t.Helper()

	base := time.Now().UTC()
	fixtures := []sortFixture{
		{
			org:         orgFixture{id: sortOrgA, name: "Cedar Systems", slug: "bravo-co", accountType: "enterprise", whitelisted: true, createdAt: new(base.Add(-10 * time.Hour)), disabledAt: new(base.Add(-8 * time.Hour))},
			memberCount: 1,
			trialEndsAt: base.Add(24 * time.Hour),
		},
		{
			org:         orgFixture{id: sortOrgB, name: "Alder Group", slug: "delta-co", accountType: "pro", whitelisted: true, createdAt: new(base.Add(-30 * time.Hour)), disabledAt: new(base.Add(-9 * time.Hour))},
			memberCount: 3,
			trialEndsAt: base.Add(72 * time.Hour),
		},
		{
			org:         orgFixture{id: sortOrgC, name: "Dogwood Ltd", slug: "alpha-co", accountType: "free", whitelisted: true, createdAt: new(base.Add(-40 * time.Hour)), disabledAt: new(base.Add(-7 * time.Hour))},
			memberCount: 2,
			trialEndsAt: base.Add(96 * time.Hour),
		},
		{
			org:         orgFixture{id: sortOrgD, name: "Birch Works", slug: "charlie-co", accountType: "starter", whitelisted: true, createdAt: new(base.Add(-20 * time.Hour)), disabledAt: new(base.Add(-6 * time.Hour))},
			memberCount: 0,
			trialEndsAt: base.Add(48 * time.Hour),
		},
	}

	for _, f := range fixtures {
		seedOrg(t, ctx, conn, f.org)
		for i := range f.memberCount {
			seedMembership(t, ctx, conn, f.org.id, fmt.Sprintf("user_%s_%d", f.org.id, i))
		}
		seedTrial(t, ctx, conn, trialFixture{orgID: f.org.id, endsAt: f.trialEndsAt})
	}
}

// sortPayload builds an offset-mode request over the sort fixtures, which are all disabled.
func sortPayload(sort, direction string) *gen.ListOrganizationsPayload {
	return &gen.ListOrganizationsPayload{
		Sort:            new(sort),
		Direction:       new(direction),
		IncludeDisabled: new(true),
	}
}

func TestListOrganizations_SortsEveryWhitelistedColumn(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedSortFixtures(t, ctx, conn)

	cases := []struct {
		sort    string
		wantAsc []string
	}{
		{sort: "name", wantAsc: []string{sortOrgB, sortOrgD, sortOrgA, sortOrgC}},
		{sort: "slug", wantAsc: []string{sortOrgC, sortOrgA, sortOrgD, sortOrgB}},
		{sort: "account_type", wantAsc: []string{sortOrgA, sortOrgC, sortOrgB, sortOrgD}},
		{sort: "member_count", wantAsc: []string{sortOrgD, sortOrgA, sortOrgC, sortOrgB}},
		{sort: "created_at", wantAsc: []string{sortOrgC, sortOrgB, sortOrgD, sortOrgA}},
		{sort: "disabled_at", wantAsc: []string{sortOrgB, sortOrgA, sortOrgC, sortOrgD}},
		{sort: "trial_ends_at", wantAsc: []string{sortOrgA, sortOrgD, sortOrgB, sortOrgC}},
	}

	for _, c := range cases {
		requireOrder(t, ctx, svc, sortPayload(c.sort, "asc"), c.wantAsc, c.sort+" ascending")
		requireOrder(t, ctx, svc, sortPayload(c.sort, "desc"), reversed(c.wantAsc), c.sort+" descending")
	}
}

// A sort value arrives from a URL operators paste to each other, so a value the
// API does not know falls back to the default order rather than failing the page.
func TestListOrganizations_UnknownSortAndDirectionFallBack(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedSortFixtures(t, ctx, conn)

	byID := []string{sortOrgA, sortOrgB, sortOrgC, sortOrgD}
	nameAsc := []string{sortOrgB, sortOrgD, sortOrgA, sortOrgC}

	cases := []struct {
		name      string
		sort      string
		direction string
		want      []string
	}{
		{name: "a column that does not exist", sort: "not_a_column", direction: "asc", want: byID},
		{name: "a real column left out of the whitelist", sort: "workos_id", direction: "asc", want: byID},
		{name: "the default column cannot be named", sort: "id", direction: "asc", want: byID},
		{name: "an empty sort", sort: "", direction: "asc", want: byID},
		{name: "a SQL fragment", sort: "name; DROP TABLE organization_metadata", direction: "asc", want: byID},
		{name: "an unknown direction", sort: "name", direction: "sideways", want: nameAsc},
		{name: "an empty direction", sort: "name", direction: "", want: nameAsc},
		{name: "a direction spelled backwards", sort: "name", direction: "descending", want: nameAsc},

		// Both values are matched case-insensitively: a hand-edited URL should not silently lose the sort.
		{name: "a mixed case column and direction", sort: "Name", direction: "DESC", want: reversed(nameAsc)},
	}

	for _, c := range cases {
		requireOrder(t, ctx, svc, sortPayload(c.sort, c.direction), c.want, c.name)
	}
}

// Sorting by an empty date must not bury the rows an operator came to see, so
// nulls stay at the bottom whichever way the direction points.
func TestListOrganizations_NullDatesSortLast(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	const (
		datedEarly = "org_null_dated_1"
		datedLate  = "org_null_dated_2"
		bareFirst  = "org_null_zbare_1"
		bareSecond = "org_null_zbare_2"
	)

	base := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: datedEarly, name: "Dated Early", slug: "dated-early", whitelisted: true, disabledAt: new(base.Add(-2 * time.Hour))})
	seedOrg(t, ctx, conn, orgFixture{id: datedLate, name: "Dated Late", slug: "dated-late", whitelisted: true, disabledAt: new(base.Add(-1 * time.Hour))})
	seedOrg(t, ctx, conn, orgFixture{id: bareFirst, name: "Bare One", slug: "bare-one", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: bareSecond, name: "Bare Two", slug: "bare-two", whitelisted: true})

	seedTrial(t, ctx, conn, trialFixture{orgID: datedEarly, endsAt: base.Add(24 * time.Hour)})
	seedTrial(t, ctx, conn, trialFixture{orgID: datedLate, endsAt: base.Add(48 * time.Hour)})

	// The bare ids sort after the dated ones, so the tiebreaker orders the null tail.
	cases := []struct {
		sort      string
		direction string
		want      []string
	}{
		{sort: "disabled_at", direction: "asc", want: []string{datedEarly, datedLate, bareFirst, bareSecond}},
		{sort: "disabled_at", direction: "desc", want: []string{datedLate, datedEarly, bareFirst, bareSecond}},
		{sort: "trial_ends_at", direction: "asc", want: []string{datedEarly, datedLate, bareFirst, bareSecond}},
		{sort: "trial_ends_at", direction: "desc", want: []string{datedLate, datedEarly, bareFirst, bareSecond}},
	}

	for _, c := range cases {
		requireOrder(t, ctx, svc, sortPayload(c.sort, c.direction), c.want, c.sort+" "+c.direction)
	}
}

// Rows that tie on the sort key fall back to the id, in one direction only.
// Without that a page boundary can drop or repeat a row between two requests.
func TestListOrganizations_TiesBreakOnIDInBothDirections(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	// Seeded back to front, so an order that merely reflects the physical rows fails.
	inserted := []string{"org_tie_d", "org_tie_c", "org_tie_b", "org_tie_a"}
	for _, id := range inserted {
		seedOrg(t, ctx, conn, orgFixture{id: id, name: "Same Name", slug: id, accountType: "free", whitelisted: true})
	}

	byID := []string{"org_tie_a", "org_tie_b", "org_tie_c", "org_tie_d"}

	for _, direction := range []string{"asc", "desc"} {
		for _, sort := range []string{"name", "account_type"} {
			label := sort + " " + direction
			requireOrder(t, ctx, svc, sortPayload(sort, direction), byID, label)
			// Repeating the call pins the order down: an unordered tie group can come back either way.
			requireOrder(t, ctx, svc, sortPayload(sort, direction), byID, label+", repeated")
		}
	}

	// The same ties across a page boundary, which is where an unstable order loses rows.
	page1 := sortPayload("name", "asc")
	page1.Limit, page1.Page = new(2), new(1)
	requireOrder(t, ctx, svc, page1, byID[:2], "tied page 1")

	page2 := sortPayload("name", "asc")
	page2.Limit, page2.Page = new(2), new(2)
	requireOrder(t, ctx, svc, page2, byID[2:], "tied page 2")
}

// The page fixtures. The names run backwards against the ids, so a page that
// quietly fell back to the default order reads as a wrong page.
var pageOrgsByName = []string{"org_page_5", "org_page_4", "org_page_3", "org_page_2", "org_page_1"}

func seedPageFixtures(t *testing.T, ctx context.Context, conn *pgxpool.Pool) {
	t.Helper()

	names := []string{"Echo Co", "Delta Co", "Charlie Co", "Bravo Co", "Alpha Co"}
	for i, name := range names {
		id := fmt.Sprintf("org_page_%d", i+1)
		seedOrg(t, ctx, conn, orgFixture{id: id, name: name, slug: id, whitelisted: true})
	}
}

func TestListOrganizations_OffsetPaging(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedPageFixtures(t, ctx, conn)

	cases := []struct {
		name string
		page *int
		want []string
	}{
		{name: "no page is the first page", page: nil, want: pageOrgsByName[0:2]},
		{name: "page 1", page: new(1), want: pageOrgsByName[0:2]},
		{name: "page 0 clamps to the first page", page: new(0), want: pageOrgsByName[0:2]},
		{name: "a negative page clamps to the first page", page: new(-3), want: pageOrgsByName[0:2]},
		{name: "page 2", page: new(2), want: pageOrgsByName[2:4]},
		{name: "the last page is short", page: new(3), want: pageOrgsByName[4:5]},
		{name: "a page past the end is empty", page: new(4), want: []string{}},
		{name: "an absurd page is empty, not an error", page: new(1 << 40), want: []string{}},

		// The query binds the offset as a bigint. The largest page an operator can
		// type must still clamp, because the multiply that turns it into an offset
		// would otherwise wrap past the end of int64 and back into a negative.
		{name: "the largest page there is", page: ptrTo(math.MaxInt64), want: []string{}},
	}

	for _, c := range cases {
		payload := sortPayload("name", "asc")
		payload.IncludeDisabled = nil
		payload.Limit, payload.Page = new(2), c.page
		requireOrder(t, ctx, svc, payload, c.want, c.name)
	}

	// Walking the pages visits every organization exactly once.
	var walked []string
	for page := 1; page <= 3; page++ {
		payload := sortPayload("name", "asc")
		payload.IncludeDisabled = nil
		payload.Limit, payload.Page = new(2), new(page)

		res, err := svc.ListOrganizations(ctx, payload)
		require.NoError(t, err)
		for _, o := range res.Organizations {
			walked = append(walked, o.ID)
		}
	}
	require.Equal(t, pageOrgsByName, walked, "walking every page drops and repeats nothing")

	// Limit 1 is its own boundary. The page ceiling divides by the limit, so a
	// limit of 1 is the value an arithmetic slip in that ceiling breaks first,
	// and every case above uses limit 2.
	for page := 1; page <= len(pageOrgsByName); page++ {
		payload := sortPayload("name", "asc")
		payload.IncludeDisabled = nil
		payload.Limit, payload.Page = new(1), new(page)
		requireOrder(t, ctx, svc, payload, pageOrgsByName[page-1:page], fmt.Sprintf("limit 1, page %d", page))
	}
}

// A page number alone selects offset paging, with the default order.
func TestListOrganizations_PageWithoutSortUsesTheDefaultOrder(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedPageFixtures(t, ctx, conn)

	byID := []string{"org_page_1", "org_page_2", "org_page_3", "org_page_4", "org_page_5"}

	page1 := requireOrder(t, ctx, svc, &gen.ListOrganizationsPayload{Limit: new(2), Page: new(1)}, byID[0:2], "page 1 without a sort")
	require.Nil(t, page1.NextCursor)

	requireOrder(t, ctx, svc, &gen.ListOrganizationsPayload{Limit: new(2), Page: new(2)}, byID[2:4], "page 2 without a sort")
}

// total reports the rows the filters matched, not the rows this page holds.
func TestListOrganizations_TotalCountsMatchesNotPageLength(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	for i := range 5 {
		id := fmt.Sprintf("org_total_holdings_%d", i)
		seedOrg(t, ctx, conn, orgFixture{id: id, name: fmt.Sprintf("Holdings %d", i), slug: id, whitelisted: true})
	}
	for i := range 2 {
		id := fmt.Sprintf("org_total_networks_%d", i)
		seedOrg(t, ctx, conn, orgFixture{id: id, name: fmt.Sprintf("Networks %d", i), slug: id, accountType: "pro", whitelisted: true})
	}
	disabledAt := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: "org_total_disabled", name: "Holdings Disabled", slug: "org_total_disabled", whitelisted: true, disabledAt: &disabledAt})

	cases := []struct {
		name      string
		payload   *gen.ListOrganizationsPayload
		wantTotal int64
		wantLen   int
	}{
		{name: "no filter, first page", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Limit: new(2), Page: new(1)}, wantTotal: 7, wantLen: 2},
		{name: "no filter, last page", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Limit: new(2), Page: new(4)}, wantTotal: 7, wantLen: 1},
		{name: "search filter", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Limit: new(2), Q: new("holdings")}, wantTotal: 5, wantLen: 2},
		{name: "account type filter", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Limit: new(2), AccountType: new("pro")}, wantTotal: 2, wantLen: 2},
		{name: "disabled included", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Limit: new(2), IncludeDisabled: new(true)}, wantTotal: 8, wantLen: 2},
		{name: "a filter matching nothing", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Q: new("no such organization")}, wantTotal: 0, wantLen: 0},

		// A page past the end still reports what the filters matched. A client that
		// lands there, from a bookmark or from a filter typed while sitting on a
		// later page, needs the count to find its way back to a page that exists.
		{name: "past the end", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Limit: new(2), Page: new(9)}, wantTotal: 7, wantLen: 0},
		{name: "past the end under a filter", payload: &gen.ListOrganizationsPayload{Sort: new("name"), Limit: new(2), Q: new("holdings"), Page: new(4)}, wantTotal: 5, wantLen: 0},

		{name: "cursor mode", payload: &gen.ListOrganizationsPayload{Limit: new(2)}, wantTotal: 7, wantLen: 2},
		{name: "cursor mode under a filter", payload: &gen.ListOrganizationsPayload{Limit: new(2), Q: new("holdings")}, wantTotal: 5, wantLen: 2},
	}

	for _, c := range cases {
		res, err := svc.ListOrganizations(ctx, c.payload)
		require.NoError(t, err, "listing organizations for %s", c.name)
		require.Equal(t, c.wantTotal, res.Total, "total for %s", c.name)
		require.Len(t, res.Organizations, c.wantLen, "page length for %s", c.name)
	}

	// The count must not fall as the cursor advances. It counts the rows the
	// filters matched, not the rows still ahead of the cursor, and the cursor walk
	// is what the deployed dashboard runs.
	var cursor *string
	for page := 1; ; page++ {
		res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Limit: new(2), Cursor: cursor})
		require.NoError(t, err, "cursor page %d", page)
		require.Equal(t, int64(7), res.Total, "total on cursor page %d", page)

		if res.NextCursor == nil {
			require.Equal(t, 4, page, "the walk covers seven rows in four pages of two")
			break
		}
		cursor = res.NextCursor
	}
}

// Offset mode drops the cursor: reporting one would invite a client to mix the
// two walks, and the cursor cannot express a sorted position.
func TestListOrganizations_OffsetModeIgnoresAndOmitsTheCursor(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedPageFixtures(t, ctx, conn)

	byID := []string{"org_page_1", "org_page_2", "org_page_3", "org_page_4", "org_page_5"}

	// The cursor walk is untouched, including when only a direction is supplied.
	for _, c := range []struct {
		name    string
		payload *gen.ListOrganizationsPayload
	}{
		{name: "no paging arguments", payload: &gen.ListOrganizationsPayload{Limit: new(2)}},
		{name: "a direction alone", payload: &gen.ListOrganizationsPayload{Limit: new(2), Direction: new("desc")}},
	} {
		res := requireOrder(t, ctx, svc, c.payload, byID[0:2], c.name)
		require.NotNil(t, res.NextCursor, "cursor mode still pages by cursor for %s", c.name)
		require.Equal(t, "org_page_2", *res.NextCursor)
	}

	// Every offset-mode request omits it, including the ones that fall back to the default order.
	for _, c := range []struct {
		name    string
		payload *gen.ListOrganizationsPayload
		want    []string
	}{
		{name: "a sort", payload: &gen.ListOrganizationsPayload{Limit: new(2), Sort: new("name")}, want: pageOrgsByName[0:2]},
		{name: "an unknown sort", payload: &gen.ListOrganizationsPayload{Limit: new(2), Sort: new("not_a_column")}, want: byID[0:2]},
		{name: "a page alone", payload: &gen.ListOrganizationsPayload{Limit: new(2), Page: new(1)}, want: byID[0:2]},
		{name: "a page past the end", payload: &gen.ListOrganizationsPayload{Limit: new(2), Page: new(9)}, want: []string{}},

		// A cursor left over from the other walk is ignored, not applied on top of the offset.
		{name: "a sort beside a stale cursor", payload: &gen.ListOrganizationsPayload{Limit: new(2), Sort: new("name"), Cursor: new("org_page_5")}, want: pageOrgsByName[0:2]},
	} {
		res := requireOrder(t, ctx, svc, c.payload, c.want, c.name)
		require.Nil(t, res.NextCursor, "offset mode reports no cursor for %s", c.name)
	}
}

// A page number selects offset paging on its own, so a cursor left in the URL
// beside it is ignored the same way a cursor beside a sort is.
func TestListOrganizations_PageIgnoresAStaleCursor(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedPageFixtures(t, ctx, conn)

	byID := []string{"org_page_1", "org_page_2", "org_page_3", "org_page_4", "org_page_5"}

	page1 := &gen.ListOrganizationsPayload{Limit: new(2), Page: new(1), Cursor: new("org_page_3")}
	res := requireOrder(t, ctx, svc, page1, byID[0:2], "page 1 beside a stale cursor")
	require.Nil(t, res.NextCursor)
	require.Equal(t, int64(5), res.Total, "the stale cursor does not narrow the count either")

	page2 := &gen.ListOrganizationsPayload{Limit: new(2), Page: new(2), Cursor: new("org_page_4")}
	requireOrder(t, ctx, svc, page2, byID[2:4], "page 2 beside a stale cursor")
}

// The page size an operator can ask for is bounded at both ends. The design
// declares no minimum, so limit=0 reaches the handler, and the offset is built by
// dividing by the limit: without the fallback that division is a panic, not a
// wrong page.
func TestListOrganizations_LimitIsClampedToItsBounds(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	const seeded = 101
	for i := range seeded {
		id := fmt.Sprintf("org_limit_%03d", i)
		seedOrg(t, ctx, conn, orgFixture{id: id, name: id, slug: id, whitelisted: true})
	}

	cases := []struct {
		name  string
		limit *int
		want  int
	}{
		{name: "no limit takes the default", limit: nil, want: 50},
		{name: "a limit inside the bounds is kept", limit: new(7), want: 7},
		{name: "the maximum itself is kept", limit: new(100), want: 100},
		{name: "a limit above the maximum clamps to it", limit: new(1000), want: 100},
		{name: "zero falls back to the default", limit: new(0), want: 50},
		{name: "a negative limit falls back to the default", limit: new(-5), want: 50},
	}

	for _, c := range cases {
		res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Sort: new("name"), Limit: c.limit, Page: new(1)})
		require.NoError(t, err, "listing organizations for %s", c.name)
		require.Len(t, res.Organizations, c.want, "page length for %s", c.name)
		require.Equal(t, int64(seeded), res.Total, "total for %s", c.name)
	}
}

// member_count is a sortable column, so the rows it counts have to be the live
// memberships. A removed member must not keep ranking the organization.
func TestListOrganizations_MemberCountSkipsRemovedMembers(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_gone_a", name: "Gone A", slug: "gone-a", whitelisted: true})
	seedOrg(t, ctx, conn, orgFixture{id: "org_gone_b", name: "Gone B", slug: "gone-b", whitelisted: true})

	seedMembership(t, ctx, conn, "org_gone_a", "user_gone_1")
	seedMembership(t, ctx, conn, "org_gone_a", "user_gone_2")
	seedMembership(t, ctx, conn, "org_gone_b", "user_gone_3")

	require.NoError(t, testrepo.New(conn).ForceSoftDeleteOrganizationUserRelationshipsFixture(ctx, "org_gone_a"))

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Sort: new("member_count"), Direction: new("desc")})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 2)

	require.Equal(t, "org_gone_b", res.Organizations[0].ID, "the organization with a live member ranks first")
	require.Equal(t, 1, res.Organizations[0].MemberCount)
	require.Equal(t, 0, res.Organizations[1].MemberCount, "removed members are not counted")
}
