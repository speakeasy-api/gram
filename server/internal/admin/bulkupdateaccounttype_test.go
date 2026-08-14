package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
)

// postBulk drives a raw JSON body through the generated request decoder before
// the handler, because the allow-list lives in the decoder and a test that calls
// the handler directly never touches it.
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

// postUpdateOrg is postBulk's twin for the single-organization write path.
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

func TestBulkUpdateAccountType_WritesOnlyTheListedIDs(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_a", name: "Bulk A", slug: "bulk-a", accountType: "free"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_b", name: "Bulk B", slug: "bulk-b", accountType: "pro"})

	// The bystander is the whole point of this fixture: without a seeded
	// organization outside ids, an UPDATE that dropped its WHERE clause and wrote
	// the entire table would still look correct.
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
		`{"ids":["org_bulk_ghost","org_bulk_real","org_bulk_ghost"],"account_type":"pro"}`)
	require.NoError(t, err, "a stale id must cost the operator that row, not the batch")

	require.Equal(t, []string{"org_bulk_real"}, res.UpdatedIds)
	require.Equal(t, []string{"org_bulk_ghost"}, res.MissingIds)
	require.Equal(t, "pro", readOrgState(t, ctx, conn, "org_bulk_real").GramAccountType)
}

func TestBulkUpdateAccountType_AllIDsMissing(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_only", name: "Only Co", slug: "only-co", accountType: "free"})

	res, err := postBulk(t, ctx, svc, `{"ids":["ghost_one","ghost_two"],"account_type":"enterprise"}`)
	require.NoError(t, err)
	require.NotNil(t, res.UpdatedIds, "updated_ids is required, so it must serialise as [] rather than null")
	require.Empty(t, res.UpdatedIds)
	require.Equal(t, []string{"ghost_one", "ghost_two"}, res.MissingIds)
	require.Equal(t, "free", readOrgState(t, ctx, conn, "org_bulk_only").GramAccountType)
}

func TestBulkUpdateAccountType_RejectedRequestsWriteNothing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		// names is the value the refusal must quote back to the operator.
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

	for _, accountType := range []string{"free", "pro", "enterprise"} {
		t.Run(accountType, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn := newTestAdminService(t)
			seedOrg(t, ctx, conn, orgFixture{id: "org_bulk_ok", name: "Ok Co", slug: "ok-co", accountType: "free"})

			_, err := postBulk(t, ctx, svc, `{"ids":["org_bulk_ok"],"account_type":"`+accountType+`"}`)
			require.NoError(t, err)
			require.Equal(t, accountType, readOrgState(t, ctx, conn, "org_bulk_ok").GramAccountType)
		})
	}
}

// The two write paths accepting different sets of values is the defect this
// endpoint exists to prevent, so dropping the allow-list from either one has to
// turn a test red.
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

	// The allow-list narrows account_type; it must not have made it required or
	// disturbed the guard that rejects a body carrying neither field.
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
}
