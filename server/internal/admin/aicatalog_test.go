package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	catalogrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func asStaff(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{Email: "operator@example.test"})
}

func seedAIProduct(t *testing.T, db catalogrepo.DBTX, id, name string) {
	t.Helper()

	n, err := catalogrepo.New(db).InsertProduct(t.Context(), catalogrepo.InsertProductParams{
		ID:              id,
		VendorKey:       "vendor-" + id,
		DisplayName:     name,
		Category:        conv.ToPGText("harness"),
		BundleIds:       []string{"com.example." + id},
		Binaries:        []string{id},
		ConfigDirs:      []string{"~/." + id},
		ProcessNames:    []string{id + "-proc"},
		VersionPlistKey: pgtype.Text{String: "", Valid: false},
		ClientInfoNames: []string{name + " Client"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

func softDeleteAIProduct(t *testing.T, db testrepo.DBTX, id, name string) {
	t.Helper()

	require.NoError(t, testrepo.New(db).EditProductFixture(t.Context(), testrepo.EditProductFixtureParams{
		ID: id, DisplayName: name, DeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite},
	}))
}

func seedAIDocument(t *testing.T, db testrepo.DBTX, productID, url string, enabled, pattern bool) uuid.UUID {
	t.Helper()

	id := uuid.New()
	require.NoError(t, testrepo.New(db).InsertDocumentFixture(t.Context(), testrepo.InsertDocumentFixtureParams{
		ID: id, AiProductID: productID, Url: url, DisplayName: "Doc " + url, Enabled: enabled, IsPattern: pattern,
	}))
	return id
}

func TestListAIProducts_RequiresStaff(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.ListAIProducts(ctx, &gen.ListAIProductsPayload{Query: nil, IncludeDeleted: false})
	requireOopsCode(t, err, oops.CodeUnauthorized)

	_, err = svc.GetAIProduct(ctx, &gen.GetAIProductPayload{ID: "anything"})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestAIProductRoutes_RequireStaffSession(t *testing.T) {
	t.Parallel()
	_, svc, db := newTestAdminService(t)
	seedAIProduct(t, db, "zz-route-product", "Route Product")
	staffSession := makeAdminFeatureSession(t, t.Context(), svc, "operator@example.com")

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)

	nonStaff := newTestSessionService(t, newTestOIDCClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"sub-admin","email":"person@external.test","email_verified":true,"hd":"external.test"}`))
	}))
	nonStaffSession := makeAdminFeatureSession(t, t.Context(), nonStaff, "person@external.test")
	nonStaffMux := goahttp.NewMuxer()
	Attach(nonStaffMux, nonStaff)
	nonStaffHandler := SessionMiddleware(nonStaffMux)

	for _, route := range []string{"/admin/aiProducts.list", "/admin/aiProducts.get?id=zz-route-product"} {
		for name, tc := range map[string]struct {
			handler http.Handler
			cookie  string
			value   string
		}{
			"no cookie":        {handler: handler, cookie: "", value: ""},
			"customer session": {handler: handler, cookie: constants.SessionCookie, value: staffSession},
			"unknown session":  {handler: handler, cookie: constants.AdminSessionCookie, value: "invalid"},
			"non-staff":        {handler: nonStaffHandler, cookie: constants.AdminSessionCookie, value: nonStaffSession},
		} {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			req.Header.Set("Authorization", "Bearer customer-key")
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: tc.cookie, Value: tc.value})
			}
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", route, name)
		}

		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: staffSession})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "%s %s", route, rec.Body.String())
	}
}

func TestListAIProducts_SearchAndDeletedFilter(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = asStaff(ctx)

	seedAIProduct(t, db, "zz-alpha-cli", "Zzlisted Alpha")
	seedAIProduct(t, db, "zz-beta-ide", "Zzlisted Beta")
	seedAIProduct(t, db, "zz-gone", "Zzlisted Gone")
	seedAIDocument(t, db, "zz-alpha-cli", "https://alpha.example.test/a.json", true, false)
	seedAIDocument(t, db, "zz-alpha-cli", "https://alpha.example.test/*/b.json", false, true)
	removed := seedAIDocument(t, db, "zz-alpha-cli", "https://alpha.example.test/old.json", true, false)
	require.NoError(t, testrepo.New(db).EditDocumentFixture(ctx, testrepo.EditDocumentFixtureParams{
		ID: removed, Url: "https://alpha.example.test/old.json", DisplayName: "Old", Enabled: true,
		DeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite},
	}))
	softDeleteAIProduct(t, db, "zz-gone", "Zzlisted Gone")

	byName, err := svc.ListAIProducts(ctx, &gen.ListAIProductsPayload{Query: new("zzLISTED"), IncludeDeleted: false})
	require.NoError(t, err)
	require.False(t, byName.Truncated)
	require.Len(t, byName.Products, 2, "deleted products are hidden by default")
	alpha, beta := byName.Products[0], byName.Products[1]
	require.Equal(t, "zz-alpha-cli", alpha.ID)
	require.Equal(t, "Zzlisted Alpha", alpha.DisplayName)
	require.Equal(t, "vendor-zz-alpha-cli", alpha.VendorKey)
	require.Equal(t, "harness", *alpha.Category)
	require.Equal(t, 2, alpha.DocumentCount, "disabled documents count, deleted ones do not")
	require.False(t, alpha.Deleted)
	require.NotEmpty(t, alpha.UpdatedAt)
	require.Equal(t, "zz-beta-ide", beta.ID)
	require.Equal(t, 0, beta.DocumentCount)

	byID, err := svc.ListAIProducts(ctx, &gen.ListAIProductsPayload{Query: new(" BETA-ide "), IncludeDeleted: false})
	require.NoError(t, err)
	require.Len(t, byID.Products, 1)
	require.Equal(t, "zz-beta-ide", byID.Products[0].ID)

	withDeleted, err := svc.ListAIProducts(ctx, &gen.ListAIProductsPayload{Query: new("zzlisted"), IncludeDeleted: true})
	require.NoError(t, err)
	require.Len(t, withDeleted.Products, 3)
	require.Equal(t, "zz-gone", withDeleted.Products[2].ID, "deleted products sort after live ones")
	require.True(t, withDeleted.Products[2].Deleted)

	none, err := svc.ListAIProducts(ctx, &gen.ListAIProductsPayload{Query: new("no-such-product-%"), IncludeDeleted: true})
	require.NoError(t, err)
	require.Empty(t, none.Products)
}

func TestGetAIProduct_DetailDocumentsAndBlockCount(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = asStaff(ctx)

	const productID = "zz-detail"
	seedAIProduct(t, db, productID, "Detail Product")
	seedAIProduct(t, db, "zz-other", "Other Product")
	seedAIDocument(t, db, productID, "https://detail.example.test/client.json", true, false)
	seedAIDocument(t, db, productID, "https://detail.example.test/*/client.json", false, true)
	removed := seedAIDocument(t, db, productID, "https://detail.example.test/removed.json", true, false)
	require.NoError(t, testrepo.New(db).EditDocumentFixture(ctx, testrepo.EditDocumentFixtureParams{
		ID: removed, Url: "https://detail.example.test/removed.json", DisplayName: "Removed", Enabled: true,
		DeletedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite},
	}))
	seedAIDocument(t, db, "zz-other", "https://other.example.test/client.json", true, false)

	agent := agentrepo.New(db)
	for _, org := range []struct{ id, status string }{
		{"org_aicat_block_a", "blocked"},
		{"org_aicat_block_b", "blocked"},
		{"org_aicat_allow", "approved"},
	} {
		seedOrg(t, ctx, db, orgFixture{id: org.id, name: org.id, slug: org.id})
		_, err := agent.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
			OrganizationID: org.id, ID: productID, AiProductID: conv.ToPGText(productID), Status: org.status, Rationale: pgtype.Text{String: "", Valid: false},
		})
		require.NoError(t, err)
	}
	// A second blocked row in the same organization must not count it twice.
	_, err := agent.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: "org_aicat_block_a", ID: "zz-detail-alias", AiProductID: conv.ToPGText(productID), Status: "blocked", Rationale: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = agent.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: "org_aicat_allow", ID: "zz-other", AiProductID: conv.ToPGText("zz-other"), Status: "blocked", Rationale: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	product, err := svc.GetAIProduct(ctx, &gen.GetAIProductPayload{ID: productID})
	require.NoError(t, err)
	require.Equal(t, productID, product.ID)
	require.Equal(t, "Detail Product", product.DisplayName)
	require.Equal(t, "vendor-zz-detail", product.VendorKey)
	require.Equal(t, "harness", *product.Category)
	require.Equal(t, []string{"com.example.zz-detail"}, product.BundleIds)
	require.Equal(t, []string{"zz-detail"}, product.Binaries)
	require.Equal(t, []string{"~/.zz-detail"}, product.ConfigDirs)
	require.Equal(t, []string{"zz-detail-proc"}, product.ProcessNames)
	require.Nil(t, product.VersionPlistKey)
	require.Equal(t, []string{"Detail Product Client"}, product.ClientInfoNames)
	require.Nil(t, product.CreatedBy, "seeded rows have no author")
	require.False(t, product.Deleted)
	require.Equal(t, 2, product.BlockedOrganizationCount, "counts organizations, not rows")

	require.Len(t, product.Documents, 3, "deleted documents are listed")
	pattern, exact, gone := product.Documents[0], product.Documents[1], product.Documents[2]
	require.Equal(t, "https://detail.example.test/*/client.json", pattern.URL)
	require.True(t, pattern.IsPattern)
	require.False(t, pattern.Enabled)
	require.False(t, pattern.Deleted)
	require.Equal(t, "https://detail.example.test/client.json", exact.URL)
	require.True(t, exact.Enabled)
	require.False(t, exact.IsPattern)
	require.Nil(t, exact.VerifiedAt)
	require.Nil(t, exact.VerificationError)
	require.Equal(t, removed.String(), gone.ID)
	require.True(t, gone.Deleted, "deleted documents sort last")

	softDeleteAIProduct(t, db, productID, "Detail Product")
	deleted, err := svc.GetAIProduct(ctx, &gen.GetAIProductPayload{ID: productID})
	require.NoError(t, err)
	require.True(t, deleted.Deleted, "deleted products stay readable")
	require.Equal(t, 2, deleted.BlockedOrganizationCount)
}

func TestGetAIProduct_NotFound(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.GetAIProduct(asStaff(ctx), &gen.GetAIProductPayload{ID: "zz-missing"})
	requireOopsCode(t, err, oops.CodeNotFound)
}
