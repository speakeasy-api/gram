package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

// Drives raw JSON through the generated decoder first, because the allow-list
// lives there and a test that calls the handler directly never touches it.
func postBulk(t *testing.T, ctx context.Context, svc *Service, body string) (*gen.AdminBulkUpdateAccountTypeResult, error) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/admin/organizations.bulkUpdateAccountType", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	payload, err := srv.DecodeBulkUpdateAccountTypeRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)(req)
	if err != nil {
		return nil, err
	}

	return svc.BulkUpdateAccountType(ctx, payload)
}

func TestBulkUpdateAccountType_RejectsAnyEnterpriseTrialBatchAtomically(t *testing.T) {
	t.Parallel()
	convertedAt := time.Now().UTC().Add(-time.Hour)
	for _, tc := range []struct {
		name        string
		convertedAt *time.Time
	}{
		{name: "unconverted enterprise trial"},
		{name: "converted enterprise trial", convertedAt: &convertedAt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, svc, conn := newTestAdminService(t)
			suffix := strings.ReplaceAll(tc.name, " ", "_")
			trialID, ordinaryID, bystanderID := "org_bulk_trial_"+suffix, "org_bulk_ordinary_"+suffix, "org_bulk_bystander_"+suffix
			seedOrg(t, ctx, conn, orgFixture{id: trialID, name: trialID, slug: trialID, accountType: "free"})
			seedTrial(t, ctx, conn, trialFixture{orgID: trialID, tier: "enterprise", endsAt: time.Now().UTC().Add(time.Hour), convertedAt: tc.convertedAt})
			seedOrg(t, ctx, conn, orgFixture{id: ordinaryID, name: ordinaryID, slug: ordinaryID, accountType: "free"})
			seedOrg(t, ctx, conn, orgFixture{id: bystanderID, name: bystanderID, slug: bystanderID, accountType: "pro"})
			trialBefore, ordinaryBefore, bystanderBefore := readOrgState(t, ctx, conn, trialID), readOrgState(t, ctx, conn, ordinaryID), readOrgState(t, ctx, conn, bystanderID)

			result, err := svc.BulkUpdateAccountType(ctx, &gen.BulkUpdateAccountTypePayload{Ids: []string{ordinaryID, trialID}, AccountType: "enterprise"})
			require.Nil(t, result)
			require.ErrorContains(t, err, "enterprise trial")
			require.Equal(t, trialBefore, readOrgState(t, ctx, conn, trialID))
			require.Equal(t, ordinaryBefore, readOrgState(t, ctx, conn, ordinaryID), "batch must not partially update before conflict")
			require.Equal(t, bystanderBefore, readOrgState(t, ctx, conn, bystanderID), "organization outside the batch must remain untouched")
		})
	}
}

func TestBulkUpdateAccountType_AllowsNonEnterpriseTargetForEnterpriseTrial(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	orgID := "org_bulk_trial_nonenterprise_target"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: orgID, slug: orgID, accountType: "free"})
	seedTrial(t, ctx, conn, trialFixture{orgID: orgID, tier: "enterprise", endsAt: time.Now().UTC().Add(time.Hour)})

	result, err := svc.BulkUpdateAccountType(ctx, &gen.BulkUpdateAccountTypePayload{Ids: []string{orgID}, AccountType: "pro"})
	require.NoError(t, err)
	require.Equal(t, []string{orgID}, result.UpdatedIds)
	require.Equal(t, "pro", readOrgState(t, ctx, conn, orgID).GramAccountType)
}

func TestBulkUpdateAccountType_WritesOnlyTheListedIDs(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_a", name: "Bulk A", slug: "bulk-a", accountType: "free"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_b", name: "Bulk B", slug: "bulk-b", accountType: "pro"})

	// Without an organization outside ids, an UPDATE that lost its WHERE clause
	// and wrote the whole table would still look correct.
	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_bystander", name: "Bystander", slug: "bystander", accountType: "free"})
	bystanderBefore := readOrgState(t, ctx, conn, "org_bulk_bystander")

	res, err := postBulk(t, ctx, svc, `{"ids":["org_bulk_a","org_bulk_b"],"account_type":"enterprise"}`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"org_bulk_a", "org_bulk_b"}, res.UpdatedIds)
	require.Empty(t, res.MissingIds)

	require.Equal(t, "enterprise", readOrgState(t, ctx, conn, "org_bulk_a").GramAccountType)
	require.Equal(t, "enterprise", readOrgState(t, ctx, conn, "org_bulk_b").GramAccountType)

	bystanderAfter := readOrgState(t, ctx, conn, "org_bulk_bystander")
	require.Equal(t, "free", bystanderAfter.GramAccountType, "an organization outside ids must not be written")
	require.Equal(t, bystanderBefore.UpdatedAt.Time, bystanderAfter.UpdatedAt.Time, "an organization outside ids must not even be touched")
}

func TestBulkUpdateAccountType_LeavesOtherColumnsAlone(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	disabled := time.Now().UTC().Add(-time.Hour)
	seedOrg(t, ctx, conn, orgFixture{
		id:          "org_bulk_cols",
		name:        "Cols Co",
		slug:        "cols-co",
		accountType: "free",
		whitelisted: true,
		disabledAt:  &disabled,
	})
	before := readOrgState(t, ctx, conn, "org_bulk_cols")

	_, err := postBulk(t, ctx, svc, `{"ids":["org_bulk_cols"],"account_type":"pro"}`)
	require.NoError(t, err)

	after := readOrgState(t, ctx, conn, "org_bulk_cols")
	require.Equal(t, "pro", after.GramAccountType)
	require.True(t, after.Whitelisted, "the bulk write must not reset whitelisted")
	require.Equal(t, before.DisabledAt.Time, after.DisabledAt.Time, "a disabled organization must stay disabled")
	require.True(t, after.UpdatedAt.Time.After(before.UpdatedAt.Time), "the write must move updated_at")
}

func TestBulkUpdateAccountType_ReportsMissingIDsAndWritesTheRest(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_real", name: "Real Co", slug: "real-co", accountType: "free"})

	res, err := postBulk(t, ctx, svc,
		`{"ids":["org_bulk_ghost","org_bulk_real","org_bulk_ghost","org_bulk_real"],"account_type":"pro"}`)
	require.NoError(t, err, "a stale id must cost the operator that row, not the batch")

	require.Equal(t, []string{"org_bulk_real"}, res.UpdatedIds, "a repeated id that was written must appear exactly once")
	require.Equal(t, []string{"org_bulk_ghost"}, res.MissingIds, "a repeated id that was not written must appear exactly once")
	require.Equal(t, "pro", readOrgState(t, ctx, conn, "org_bulk_real").GramAccountType)
}

func TestBulkUpdateAccountType_AllIDsMissing(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_only", name: "Only Co", slug: "only-co", accountType: "free"})

	// Unsorted on purpose, so a mutation that sorts is visible.
	res, err := postBulk(t, ctx, svc, `{"ids":["ghost_zulu","ghost_alpha"],"account_type":"enterprise"}`)
	require.NoError(t, err)
	require.NotNil(t, res.UpdatedIds, "updated_ids is required, so it must serialise as [] rather than null")
	require.NotNil(t, res.MissingIds, "missing_ids is required too, so it carries the same contract")
	require.Empty(t, res.UpdatedIds)
	require.Equal(t, []string{"ghost_zulu", "ghost_alpha"}, res.MissingIds)
	require.Equal(t, "free", readOrgState(t, ctx, conn, "org_bulk_only").GramAccountType)
}

func TestBulkUpdateAccountType_RejectedRequestsWriteNothing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		body  string
		names string
	}{
		{name: "unknown value", body: `{"ids":["org_bulk_bad"],"account_type":"gold"}`, names: "gold"},
		{name: "capitalised", body: `{"ids":["org_bulk_bad"],"account_type":"Free"}`, names: "Free"},
		{name: "upper case", body: `{"ids":["org_bulk_bad"],"account_type":"FREE"}`, names: "FREE"},
		{name: "padded", body: `{"ids":["org_bulk_bad"],"account_type":" free"}`, names: " free"},
		{name: "empty value", body: `{"ids":["org_bulk_bad"],"account_type":""}`},
		{name: "empty ids array", body: `{"ids":[],"account_type":"pro"}`},
		{name: "empty id element", body: `{"ids":["org_bulk_bad",""],"account_type":"pro"}`},
		{name: "missing ids", body: `{"account_type":"pro"}`},
		{name: "missing account_type", body: `{"ids":["org_bulk_bad"]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn := newTestAdminService(t)
			seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_bad", name: "Bad Co", slug: "bad-co", accountType: "free"})
			before := readOrgState(t, ctx, conn, "org_bulk_bad")

			_, err := postBulk(t, ctx, svc, tc.body)
			require.Error(t, err, "the request decoder must refuse this before the handler runs")
			if tc.names != "" {
				require.ErrorContains(t, err, "account_type")
				require.ErrorContains(t, err, tc.names, "the refusal must name the offending value")
			}

			after := readOrgState(t, ctx, conn, "org_bulk_bad")
			require.Equal(t, "free", after.GramAccountType, "a refused request must write nothing at all")
			require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
		})
	}
}

func TestBulkUpdateAccountType_AcceptsEveryAllowedValue(t *testing.T) {
	t.Parallel()

	cases := []struct{ seed, target string }{
		{seed: "pro", target: "free"},
		{seed: "free", target: "pro"},
		{seed: "free", target: "enterprise"},
	}

	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn := newTestAdminService(t)
			seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_ok", name: "Ok Co", slug: "ok-co", accountType: tc.seed})

			res, err := postBulk(t, ctx, svc, `{"ids":["org_bulk_ok"],"account_type":"`+tc.target+`"}`)
			require.NoError(t, err)
			require.Equal(t, []string{"org_bulk_ok"}, res.UpdatedIds, "an organization that was written must be reported as written")
			require.Empty(t, res.MissingIds, "an organization that exists must never be reported missing")
			require.Equal(t, tc.target, readOrgState(t, ctx, conn, "org_bulk_ok").GramAccountType)
		})
	}
}

// Every other test here is a real transition, so this is the only one that sees
// a "skip rows already correct" optimisation reporting a real organization missing.
func TestBulkUpdateAccountType_AlreadyOnTargetIsNotMissing(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_noop", name: "Noop Co", slug: "noop-co", accountType: "pro"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_move", name: "Move Co", slug: "move-co", accountType: "free"})

	res, err := postBulk(t, ctx, svc, `{"ids":["org_bulk_noop","org_bulk_move"],"account_type":"pro"}`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"org_bulk_noop", "org_bulk_move"}, res.UpdatedIds,
		"an organization already on the target type exists, so it must be reported as written")
	require.Empty(t, res.MissingIds, "an organization that exists must never be reported missing")

	require.Equal(t, "pro", readOrgState(t, ctx, conn, "org_bulk_noop").GramAccountType)
	require.Equal(t, "pro", readOrgState(t, ctx, conn, "org_bulk_move").GramAccountType)
}

func TestBulkUpdateAccountType_RejectsMoreIDsThanTheCap(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_cap", name: "Cap Co", slug: "cap-co", accountType: "free"})
	before := readOrgState(t, ctx, conn, "org_bulk_cap")

	atCap := make([]string, constants.MaxBulkAccountTypeIDs)
	for i := range atCap {
		atCap[i] = fmt.Sprintf(`"org_bulk_cap_%d"`, i)
	}
	overCap := append(append([]string{}, atCap...), `"org_bulk_cap"`)

	_, err := postBulk(t, ctx, svc, `{"ids":[`+strings.Join(atCap, ",")+`],"account_type":"pro"}`)
	require.NoError(t, err, "a request exactly at the cap must be accepted")

	_, err = postBulk(t, ctx, svc, `{"ids":[`+strings.Join(overCap, ",")+`],"account_type":"pro"}`)
	require.Error(t, err, "one id over the cap must be refused")

	after := readOrgState(t, ctx, conn, "org_bulk_cap")
	require.Equal(t, "free", after.GramAccountType, "the refused request must write nothing")
	require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
}

// Straight at the service on purpose: this is the check a future non-HTTP caller hits.
func TestBulkUpdateAccountType_ServiceRefusesWhatTheDecoderWould(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_svc", name: "Svc Co", slug: "svc-co", accountType: "free"})
	before := readOrgState(t, ctx, conn, "org_bulk_svc")

	for _, bad := range []string{"gold", "Free", "FREE", ""} {
		_, err := svc.BulkUpdateAccountType(ctx, &gen.BulkUpdateAccountTypePayload{
			Ids:         []string{"org_bulk_svc"},
			AccountType: bad,
		})
		require.Error(t, err, "the service must refuse %q even with no decoder in front of it", bad)
		if bad != "" {
			require.ErrorContains(t, err, bad, "the refusal must name the offending value")
		}
	}

	after := readOrgState(t, ctx, conn, "org_bulk_svc")
	require.Equal(t, "free", after.GramAccountType)
	require.Equal(t, before.UpdatedAt.Time, after.UpdatedAt.Time)
}
