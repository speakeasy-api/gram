package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/supportmatrix"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

// The seed only mirrors the axes, once, so rows that reference them by id
// have a target; the matrix itself is served from code.
func TestSupportMatrixSeedMirrorsTheAxes(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	matrix, err := supportmatrix.Current()
	require.NoError(t, err)

	require.NoError(t, SeedSupportMatrix(ctx, db))
	// A second start finds everything in place and changes nothing.
	require.NoError(t, SeedSupportMatrix(ctx, db))
	rows, err := repo.New(db).ListSupportMatrixSlugs(ctx)
	require.NoError(t, err)
	counts := map[string]int{}
	slugs := map[string]bool{}
	for _, row := range rows {
		counts[row.Kind]++
		slugs[row.Kind+":"+row.Slug] = true
	}
	require.Equal(t, map[string]int{"platform": len(matrix.Platforms), "plan": len(matrix.Plans), "method": len(matrix.Methods), "capability": len(matrix.Capabilities)}, counts)
	require.True(t, slugs["platform:"+matrix.Platforms[0].ID])
	require.True(t, slugs["method:"+matrix.Methods[0].ID])
	require.True(t, slugs["capability:"+matrix.Capabilities[0].ID])

	served, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, matrix.Revision, served.Revision)
	require.Len(t, served.Methods, len(matrix.Methods))
	require.Len(t, served.Platforms, len(matrix.Platforms))
	require.Len(t, served.Capabilities, len(matrix.Capabilities))
	first := served.Methods[0]
	require.Equal(t, matrix.Methods[0].ID, first.ID)
	require.Len(t, first.Claims, len(matrix.Capabilities))
	require.Len(t, first.Platforms, len(matrix.Platforms))
	for _, support := range first.Platforms {
		if support.Applicability == "applicable" {
			require.Len(t, support.Cells, len(matrix.Capabilities), support.Platform)
		} else {
			require.Empty(t, support.Cells, support.Platform)
		}
		require.NotNil(t, support.Accounts)
	}
}

func TestSupportMatrixHTTP(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/supportMatrix.get", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	sessionID, err := svc.sessions.Store(ctx, StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/admin/supportMatrix.get", nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var snapshot gen.SupportMatrix
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &snapshot))
	require.NotEmpty(t, snapshot.Methods)
	require.Len(t, snapshot.Revision, 64)

	// The matrix is code: there is nothing to write to any more.
	req = httptest.NewRequest(http.MethodPost, "/admin/supportMatrix.update", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
