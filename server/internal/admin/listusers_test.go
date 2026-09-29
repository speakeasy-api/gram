package admin

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestListUsers(t *testing.T) {
	ctx, svc, db := newTestAdminService(t)
	_, err := db.Exec(ctx, `INSERT INTO users(id,email,display_name,deleted_at,workos_deleted_at) VALUES
 ('user_zero','zero@example.invalid','',NULL,NULL),
 ('user_a','TIE@example.invalid','100%_Literal',NULL,NULL),
 ('user_b','tie@example.invalid','100XXLiteral',NULL,NULL),
 ('user_c','c@example.invalid','Same Name',NULL,NULL),
 ('user_d','d@example.invalid','Same Name',NULL,NULL),
 ('user_deleted','deleted@example.invalid','Deleted',now(),NULL),
 ('user_workos','workos@example.invalid','Deleted',NULL,now())`)
	require.NoError(t, err)
	for i, name := range []string{"Studio", "North", "Duplicate", "Duplicate", "Removed"} {
		id := fmt.Sprintf("org_users_%d", i)
		seedOrg(t, ctx, db, orgFixture{id: id, name: name, slug: fmt.Sprintf("users-%d", i)})
		_, err = db.Exec(ctx, `INSERT INTO organization_user_relationships(organization_id,user_id) VALUES($1,'user_a')`, id)
		require.NoError(t, err)
	}
	_, err = db.Exec(ctx, `UPDATE organization_metadata SET disabled_at=now() WHERE id='org_users_0'; UPDATE organization_user_relationships SET deleted_at=now() WHERE organization_id='org_users_4'`)
	require.NoError(t, err)
	for _, tt := range []struct {
		q     string
		count int64
	}{
		{"email:example.invalid", 5}, {"org:studio org:north", 0}, {"org:studio north", 1},
		{`name:"100%_Literal"`, 1}, {`name:"Same Name"`, 2}, {"org:removed", 0}, {"org:duplicate org:users", 1},
	} {
		t.Run(tt.q, func(t *testing.T) {
			r, e := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &tt.q})
			require.NoError(t, e)
			require.Equal(t, tt.count, r.Total)
		})
	}
	all, e := svc.ListUsers(ctx, &gen.ListUsersPayload{})
	require.NoError(t, e)
	require.Equal(t, 1, all.Page)
	require.Equal(t, 50, all.Limit)
	require.EqualValues(t, 5, all.Total)
	q := "email:TIE@example.invalid"
	one := 1
	two := 2
	three := 3
	a, e := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &q, Limit: &one})
	require.NoError(t, e)
	require.Len(t, a.Users, 1)
	require.Equal(t, "user_a", a.Users[0].ID)
	require.Len(t, a.Users[0].Organizations, 3)
	require.EqualValues(t, 4, a.Users[0].OrganizationCount)
	b, e := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &q, Limit: &one, Page: &two})
	require.NoError(t, e)
	require.Equal(t, "user_b", b.Users[0].ID)
	c, e := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &q, Limit: &one, Page: &three})
	require.NoError(t, e)
	require.Empty(t, c.Users)
	require.EqualValues(t, 2, c.Total)
	q = "email:zero@example.invalid"
	z, e := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &q})
	require.NoError(t, e)
	require.Empty(t, z.Users[0].Organizations)
	require.Nil(t, z.Users[0].LastLogin)
	require.Empty(t, z.Users[0].DisplayName)
	// Every organization is reachable, including disabled and duplicate-name targets.
	seen := map[string]bool{}
	for page := 1; page <= 5; page++ {
		r, e := svc.ListUserOrganizations(ctx, &gen.ListUserOrganizationsPayload{UserID: "user_a", Page: &page, Limit: &one})
		require.NoError(t, e)
		require.EqualValues(t, 4, r.Total)
		for _, o := range r.Organizations {
			require.False(t, seen[o.ID])
			seen[o.ID] = true
			if o.ID == "org_users_0" {
				require.NotNil(t, o.DisabledAt)
			}
		}
	}
	require.Len(t, seen, 4)
}

func TestListUserOrganizations(t *testing.T) {
	ctx, svc, db := newTestAdminService(t)
	_, err := db.Exec(ctx, `INSERT INTO users(id,email,display_name,deleted_at,workos_deleted_at) VALUES ('eligible','eligible@example.invalid','',NULL,NULL),('deleted','deleted@example.invalid','',now(),NULL),('provider','provider@example.invalid','',NULL,now())`)
	require.NoError(t, err)
	for _, id := range []string{"missing", "deleted", "provider"} {
		_, err := svc.ListUserOrganizations(ctx, &gen.ListUserOrganizationsPayload{UserID: id})
		assertOopsCode(t, err, oops.CodeNotFound)
	}
	_, err = db.Exec(ctx, `UPDATE users SET deleted_at=now() WHERE id='provider'; UPDATE users SET deleted_at=NULL WHERE id='provider'`)
	require.NoError(t, err)
	_, err = svc.ListUserOrganizations(ctx, &gen.ListUserOrganizationsPayload{UserID: "provider"})
	assertOopsCode(t, err, oops.CodeNotFound)
	_, err = db.Exec(ctx, `UPDATE users SET workos_deleted_at=NULL, last_login='2026-01-01T12:00:00Z' WHERE id='provider'`)
	require.NoError(t, err)
	restored, err := svc.ListUserOrganizations(ctx, &gen.ListUserOrganizationsPayload{UserID: "provider"})
	require.NoError(t, err)
	require.Zero(t, restored.Total)
	restoredQuery := "email:provider"
	restoredUsers, err := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &restoredQuery})
	require.NoError(t, err)
	require.Len(t, restoredUsers.Users, 1)
	require.NotNil(t, restoredUsers.Users[0].LastLogin)
	login, err := time.Parse(time.RFC3339, *restoredUsers.Users[0].LastLogin)
	require.NoError(t, err)
	require.Equal(t, "2026-01-01T12:00:00Z", login.UTC().Format(time.RFC3339))
	r, err := svc.ListUserOrganizations(ctx, &gen.ListUserOrganizationsPayload{UserID: "eligible"})
	require.NoError(t, err)
	require.Empty(t, r.Organizations)
	require.Zero(t, r.Total)
	for _, tt := range []struct{ page, limit int }{{0, 50}, {-1, 50}, {1, 0}, {1, 101}, {math.MaxInt32, 100}, {math.MaxInt, 100}} {
		_, err := svc.ListUsers(ctx, &gen.ListUsersPayload{Page: &tt.page, Limit: &tt.limit})
		assertOopsCode(t, err, oops.CodeInvalid)
		_, err = svc.ListUserOrganizations(ctx, &gen.ListUserOrganizationsPayload{UserID: "eligible", Page: &tt.page, Limit: &tt.limit})
		assertOopsCode(t, err, oops.CodeInvalid)
	}
	for _, q := range []string{strings.Repeat("a", 2049), strings.Repeat("a ", 21), "name:" + strings.Repeat("a", 257), `name:"unfinished`} {
		_, err := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &q})
		assertOopsCode(t, err, oops.CodeInvalid)
	}
	q := "unknown:query"
	_, err = svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &q})
	assertOopsCode(t, err, oops.CodeInvalid)
}

func TestListUsers_HTTPAuthentication(t *testing.T) {
	ctx, svc, _ := newTestAdminService(t)
	svc.tracer = testenv.NewTracerProvider(t).Tracer("admin_users_test")
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	for _, path := range []string{"/admin/users.list", "/admin/users.organizations.list?user_id=missing"} {
		for _, kind := range []string{"none", "header", "tenant", "invalid-admin"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			switch kind {
			case "header":
				req.Header.Set("Authorization", "Bearer placeholder")
			case "tenant":
				req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: "placeholder"})
			case "invalid-admin":
				req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: "placeholder"})
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, kind)
		}
		session := makeAdminFeatureSession(t, ctx, svc, "operator@example.test")
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if path == "/admin/users.list" {
			require.Equal(t, http.StatusOK, rec.Code)
		} else {
			require.Equal(t, http.StatusNotFound, rec.Code)
		}
	}
}

func TestListUsers_LiteralBackslashAndIndependentTerms(t *testing.T) {
	ctx, svc, db := newTestAdminService(t)
	_, err := db.Exec(ctx, `INSERT INTO users(id,email,display_name) VALUES ('literal','literal@example.invalid',$1),('other','other@example.invalid','pathX100XX')`, `path\100%_`)
	require.NoError(t, err)
	for _, q := range []string{`name:"path\100%_"`, `email:literal name:100`, `PATH email:literal`} {
		r, err := svc.ListUsers(ctx, &gen.ListUsersPayload{Q: &q})
		require.NoError(t, err)
		require.Len(t, r.Users, 1)
		require.Equal(t, "literal", r.Users[0].ID)
	}
}

func TestListUsers_HTTPNonstaffAndBounds(t *testing.T) {
	ctx, svc, _ := newTestAdminService(t)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	session := makeAdminFeatureSession(t, ctx, svc, "operator@example.test")
	for _, path := range []string{"/admin/users.list?page=0", "/admin/users.list?limit=101", "/admin/users.list?q=unknown%3Avalue", "/admin/users.organizations.list", "/admin/users.organizations.list?user_id=missing&page=0", "/admin/users.organizations.list?user_id=missing&limit=101"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		expected := http.StatusBadRequest
		if strings.Contains(path, "unknown") {
			expected = http.StatusUnprocessableEntity
		}
		require.Equal(t, expected, rec.Code, path)
	}
	// A valid cached session is insufficient when live OIDC no longer admits staff.
	svc.verifier.oidc = newTestOIDCClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": "sub-admin", "email": "nonstaff@example.invalid", "email_verified": true, "hd": "untrusted.invalid"})
	})
	for _, path := range []string{"/admin/users.list", "/admin/users.organizations.list?user_id=missing"} {
		session := makeAdminFeatureSession(t, ctx, svc, "nonstaff@example.invalid")
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, path)
	}
}
