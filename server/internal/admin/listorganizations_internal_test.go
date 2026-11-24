package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListOrganizations_InternalStatusFilter(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	now := time.Now().UTC()
	seedOrg(t, ctx, conn, orgFixture{id: "org_is_cust_a", name: "Customer A", slug: "is-cust-a"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_is_cust_b", name: "Customer B", slug: "is-cust-b", disabledAt: &now})
	seedOrg(t, ctx, conn, orgFixture{id: "org_is_int_a", name: "Internal A", slug: "is-int-a"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_is_int_b", name: "Internal B", slug: "is-int-b", disabledAt: &now})
	for _, id := range []string{"org_is_int_a", "org_is_int_b"} {
		_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: id, Internal: new(true)})
		require.NoError(t, err)
	}

	for _, tc := range []struct {
		name    string
		payload gen.ListOrganizationsPayload
		want    []string
	}{
		{"omitted is all", gen.ListOrganizationsPayload{}, []string{"org_is_cust_a", "org_is_cust_b", "org_is_int_a", "org_is_int_b"}},
		{"explicit all", gen.ListOrganizationsPayload{InternalStatus: new("all")}, []string{"org_is_cust_a", "org_is_cust_b", "org_is_int_a", "org_is_int_b"}},
		{"internal includes disabled", gen.ListOrganizationsPayload{InternalStatus: new("internal")}, []string{"org_is_int_a", "org_is_int_b"}},
		{"external", gen.ListOrganizationsPayload{InternalStatus: new("external")}, []string{"org_is_cust_a", "org_is_cust_b"}},
		{"internal and active", gen.ListOrganizationsPayload{InternalStatus: new("internal"), DisabledStatus: new("active")}, []string{"org_is_int_a"}},
		{"exact id still filtered", gen.ListOrganizationsPayload{Q: new("org_is_cust_a"), InternalStatus: new("internal")}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := svc.ListOrganizations(ctx, &tc.payload)
			require.NoError(t, err)
			got := make([]string, 0, len(res.Organizations))
			for _, org := range res.Organizations {
				got = append(got, org.ID)
			}
			require.ElementsMatch(t, tc.want, got)
			require.Equal(t, int64(len(tc.want)), res.Total, "the total must count the same rows the filter returns")
		})
	}
}

func TestListOrganizations_InternalStatusRejectsUnknown(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	for _, value := range []string{"yes", ""} {
		_, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{InternalStatus: new(value)})
		requireOopsCode(t, err, oops.CodeInvalid)
		require.ErrorContains(t, err, "internal_status must be all, internal, or external")
	}
}

func TestListOrganizations_InternalStatusRejectsEmptyQueryValue(t *testing.T) {
	t.Parallel()

	_, svc, _ := newTestAdminService(t)
	sessionID, err := svc.sessions.Store(t.Context(), StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)

	for _, query := range []string{"internal_status=", "internal_status", "internal_status=invalid"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/admin/organizations.list?"+query, nil)
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Contains(t, []int{400, 422}, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "internal_status")
		})
	}
}
