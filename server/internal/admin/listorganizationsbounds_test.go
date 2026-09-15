package admin

import (
	"context"

	"fmt"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestListOrganizationsBoundsHTTPDecoder(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		"min_members=-1", "max_members=-1", "min_members=1.5", "max_members=0.5",
		"min_members=9223372036854775808", "max_members=9223372036854775808",
		"disabled_only=invalid",
	} {
		t.Run(query, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/organizations.list?"+query, nil)
			_, err := srv.DecodeListOrganizationsRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)(req)
			require.Error(t, err)
		})
	}
}

func TestListOrganizationsBoundsHTTPValid(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest("GET", "/admin/organizations.list?min_members=0&max_members=9223372036854775807&disabled_only=false&created_from=2024-02-29&created_to=2024-02-29", nil)
	decoded, err := srv.DecodeListOrganizationsRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)(req)
	require.NoError(t, err)
	p := decoded
	require.Equal(t, new(int64(0)), p.MinMembers)
	require.Equal(t, new(int64(math.MaxInt64)), p.MaxMembers)
	require.Equal(t, new(false), p.DisabledOnly)
	require.Equal(t, new("2024-02-29"), p.CreatedFrom)
	require.Equal(t, new("2024-02-29"), p.CreatedTo)
	req = httptest.NewRequest("GET", "/admin/organizations.list", nil)
	decoded, err = srv.DecodeListOrganizationsRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)(req)
	require.NoError(t, err)
	p = decoded
	require.Nil(t, p.MinMembers)
	require.Nil(t, p.MaxMembers)
	require.Nil(t, p.DisabledOnly)
	require.Nil(t, p.CreatedFrom)
	require.Nil(t, p.CreatedTo)
}

func TestListOrganizationsBoundsDirectValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		payload gen.ListOrganizationsPayload
		field   string
	}{
		{gen.ListOrganizationsPayload{MinMembers: new(int64(-1))}, "min_members"},
		{gen.ListOrganizationsPayload{MaxMembers: new(int64(-1))}, "max_members"},
		{gen.ListOrganizationsPayload{MinMembers: new(int64(2)), MaxMembers: new(int64(1))}, "min_members"},
		{gen.ListOrganizationsPayload{CreatedFrom: new("2025-02-29")}, "created_from"},
		{gen.ListOrganizationsPayload{CreatedTo: new("2024-02-30")}, "created_to"},
		{gen.ListOrganizationsPayload{CreatedFrom: new("2025-1-01")}, "created_from"},
		{gen.ListOrganizationsPayload{CreatedTo: new("2025-01-01T00:00:00Z")}, "created_to"},
		{gen.ListOrganizationsPayload{CreatedFrom: new(" 2025-01-01")}, "created_from"},
		{gen.ListOrganizationsPayload{CreatedTo: new("")}, "created_to"},
		{gen.ListOrganizationsPayload{CreatedFrom: new("2025-01-02"), CreatedTo: new("2025-01-01")}, "created_from"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			result, err := (&Service{}).ListOrganizations(context.Background(), &tc.payload)
			require.Nil(t, result)
			require.ErrorContains(t, err, tc.field)
		})
	}
}

func TestListOrganizationsBoundsCalendar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ day, next string }{
		{"2024-02-29", "2024-03-01T00:00:00Z"},
		{"2025-02-28", "2025-03-01T00:00:00Z"},
		{"2025-04-30", "2025-05-01T00:00:00Z"},
		{"2025-12-31", "2026-01-01T00:00:00Z"},
		{"9999-12-31", "10000-01-01T00:00:00Z"},
	} {
		t.Run(tc.day, func(t *testing.T) {
			bounds, err := listOrganizationsBounds(&gen.ListOrganizationsPayload{CreatedFrom: &tc.day, CreatedTo: &tc.day})
			require.NoError(t, err)
			require.True(t, bounds.createdAtGte.Valid)
			require.True(t, bounds.createdAtLt.Valid)
			require.Equal(t, tc.day+"T00:00:00Z", bounds.createdAtGte.Time.Format(time.RFC3339))
			require.Equal(t, tc.next, bounds.createdAtLt.Time.Format(time.RFC3339))
			require.Equal(t, time.UTC, bounds.createdAtLt.Time.Location())
		})
	}
}

func TestListOrganizationsBoundsService(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	start := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	for _, f := range []struct {
		id       string
		members  int
		created  time.Time
		disabled bool
		account  string
	}{
		{"a", 0, start.Add(-time.Microsecond), false, "free"},
		{"b", 1, start, false, "payg"}, {"c", 2, start.Add(time.Microsecond), false, "payg"},
		{"d", 0, end.Add(-time.Microsecond), true, "free"},
		{"e", 2, end, true, "payg"}, {"f", 3, end.Add(time.Microsecond), false, "free"},
	} {
		id := "org_api_bounds_" + f.id
		var disabled *time.Time
		if f.disabled {
			disabled = &start
		}
		seedOrg(t, ctx, conn, orgFixture{id: id, name: id, slug: id, accountType: f.account, createdAt: &f.created, disabledAt: disabled, workosID: new("workos_" + id)})
		for i := 0; i < f.members; i++ {
			seedMembership(t, ctx, conn, id, fmt.Sprintf("member_%d", i))
		}
	}
	for _, tc := range []struct {
		name string
		p    gen.ListOrganizationsPayload
		ids  []string
	}{
		{"omitted", gen.ListOrganizationsPayload{}, []string{"a", "b", "c", "f"}},
		{"false overrides disabled", gen.ListOrganizationsPayload{DisabledOnly: new(false), DisabledStates: []string{"disabled"}}, []string{"a", "b", "c", "d", "e", "f"}},
		{"true overrides active", gen.ListOrganizationsPayload{DisabledOnly: new(true), DisabledStates: []string{"active"}, IncludeDisabled: new(false)}, []string{"d", "e"}},
		{"true overrides include", gen.ListOrganizationsPayload{DisabledOnly: new(true), IncludeDisabled: new(true)}, []string{"d", "e"}},
		{"false overrides include", gen.ListOrganizationsPayload{DisabledOnly: new(false), IncludeDisabled: new(false)}, []string{"a", "b", "c", "d", "e", "f"}},
		{"legacy ID", gen.ListOrganizationsPayload{Q: new("org_api_bounds_d")}, []string{"d"}},
		{"strict ID", gen.ListOrganizationsPayload{Q: new("org_api_bounds_a"), DisabledOnly: new(true)}, []string{}},
		{"strict workos ID", gen.ListOrganizationsPayload{Q: new("workos_org_api_bounds_a"), DisabledOnly: new(true)}, []string{}},
		{"zero equality", gen.ListOrganizationsPayload{MinMembers: new(int64(0)), MaxMembers: new(int64(0)), DisabledOnly: new(false)}, []string{"a", "d"}},
		{"min", gen.ListOrganizationsPayload{MinMembers: new(int64(2)), DisabledOnly: new(false)}, []string{"c", "e", "f"}},
		{"max", gen.ListOrganizationsPayload{MaxMembers: new(int64(1)), DisabledOnly: new(false)}, []string{"a", "b", "d"}},
		{"max int64", gen.ListOrganizationsPayload{MaxMembers: new(int64(math.MaxInt64)), DisabledOnly: new(false)}, []string{"a", "b", "c", "d", "e", "f"}},
		{"day inclusive", gen.ListOrganizationsPayload{CreatedFrom: new("2024-02-29"), CreatedTo: new("2024-02-29"), DisabledOnly: new(false)}, []string{"b", "c", "d"}},
		{"from", gen.ListOrganizationsPayload{CreatedFrom: new("2024-02-29"), DisabledOnly: new(false)}, []string{"b", "c", "d", "e", "f"}},
		{"to", gen.ListOrganizationsPayload{CreatedTo: new("2024-02-29"), DisabledOnly: new(false)}, []string{"a", "b", "c", "d"}},
		{"all combined", gen.ListOrganizationsPayload{Q: new("org_api_bounds"), AccountTypes: []string{"free"}, TrialStates: []string{"none"}, MinMembers: new(int64(0)), MaxMembers: new(int64(0)), CreatedFrom: new("2024-02-29"), CreatedTo: new("2024-02-29"), DisabledOnly: new(true)}, []string{"d"}},
		{"ID still AND members", gen.ListOrganizationsPayload{Q: new("org_api_bounds_d"), MinMembers: new(int64(1))}, []string{}},
		{"ID still AND dates", gen.ListOrganizationsPayload{Q: new("org_api_bounds_d"), CreatedTo: new("2024-02-28")}, []string{}},
		{"empty", gen.ListOrganizationsPayload{MinMembers: new(int64(4))}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.p
			if p.Q == nil {
				p.Q = new("org_api_bounds")
			}
			p.Sort = new("name")
			result, err := svc.ListOrganizations(ctx, &p)
			require.NoError(t, err)
			ids := make([]string, 0, len(result.Organizations))
			for _, org := range result.Organizations {
				ids = append(ids, org.ID[len("org_api_bounds_"):])
			}
			require.Equal(t, tc.ids, ids)
			require.Equal(t, int64(len(tc.ids)), result.Total)
			p.Page = new(99)
			result, err = svc.ListOrganizations(ctx, &p)
			require.NoError(t, err)
			require.Empty(t, result.Organizations)
			require.Equal(t, int64(len(tc.ids)), result.Total)
			p.Sort = nil
			p.Page = nil
			p.Limit = new(1)
			result, err = svc.ListOrganizations(ctx, &p)
			require.NoError(t, err)
			require.Equal(t, int64(len(tc.ids)), result.Total)
			if result.NextCursor != nil {
				p.Cursor = result.NextCursor
				result, err = svc.ListOrganizations(ctx, &p)
				require.NoError(t, err)
				require.Equal(t, int64(len(tc.ids)), result.Total)
			}
		})
	}
}

func TestListOrganizationsBoundsHTTPStatus(t *testing.T) {
	t.Parallel()
	_, svc, _ := newTestAdminService(t)
	sessionID, err := svc.sessions.Store(t.Context(), StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	for _, q := range []string{"min_members=-1", "max_members=0.5", "min_members=9223372036854775808", "min_members=2&max_members=1", "created_from=2025-02-29", "created_to=2025-1-01", "created_from=2025-01-02&created_to=2025-01-01", "disabled_only=invalid"} {
		t.Run(q, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/organizations.list?"+q, nil)
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Contains(t, []int{400, 422}, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), q[:strings.Index(q, "=")])
		})
	}
}
