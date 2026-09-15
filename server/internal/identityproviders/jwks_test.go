package identityproviders_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/identityproviders"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestHandleJSONWebKeySetServesPublicKeyWithCaching(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connection := createConnection(t, ctx, ti, "https://acme.okta.com")

	recorder := serveJSONWebKeySet(t, ctx, ti, connection.ID, "", false)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=3600", recorder.Header().Get("Cache-Control"))
	etag := recorder.Header().Get("ETag")
	require.NotEmpty(t, etag)

	var document struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &document))
	require.Len(t, document.Keys, 1)
	require.Equal(t, connection.SigningKeyKid, document.Keys[0]["kid"])
	require.Equal(t, "RSA", document.Keys[0]["kty"])
	require.Equal(t, "RS256", document.Keys[0]["alg"])
	require.Equal(t, "sig", document.Keys[0]["use"])
	require.Contains(t, document.Keys[0], "n")
	require.Contains(t, document.Keys[0], "e")
	for _, privateField := range []string{"d", "p", "q", "dp", "dq", "qi", "oth"} {
		require.NotContains(t, document.Keys[0], privateField)
	}

	conditional := serveJSONWebKeySet(t, ctx, ti, connection.ID, etag, false)
	require.Equal(t, http.StatusNotModified, conditional.Code)
	require.Equal(t, etag, conditional.Header().Get("ETag"))
	require.Equal(t, "public, max-age=3600", conditional.Header().Get("Cache-Control"))
	require.Empty(t, conditional.Header().Get("Content-Type"))
	require.Empty(t, conditional.Body.String())
}

func TestHandleJSONWebKeySetExcludesDeletedKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connection := createConnection(t, ctx, ti, "https://acme.okta.com")
	connectionID := mustUUID(t, connection.ID)
	require.NoError(t, repo.New(ti.conn).SoftDeleteIdentityProviderSigningKeys(ctx, repo.SoftDeleteIdentityProviderSigningKeysParams{
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: connectionID,
	}))

	recorder := serveJSONWebKeySet(t, ctx, ti, connection.ID, "", false)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"keys":[]}`, recorder.Body.String())
}

func TestHandleJSONWebKeySetReturnsNotFoundForInvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	recorder := serveJSONWebKeySet(t, ctx, ti, "not-a-uuid", "", false)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestHandleJSONWebKeySetReturnsNotFoundForUnknownID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	recorder := serveJSONWebKeySet(t, ctx, ti, uuid.NewString(), "", false)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestHandleJSONWebKeySetReturnsNotFoundOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connection := createConnection(t, ctx, ti, "https://acme.okta.com")
	recorder := serveJSONWebKeySet(t, ctx, ti, connection.ID, "", true)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestHandleJSONWebKeySetAllowsConfiguredRegisteredDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithURLs(t, "", "https://identity-public.example.test")
	connection := createConnection(t, ctx, ti, "https://example.okta.com")
	recorder := serveJSONWebKeySet(t, ctx, ti, connection.ID, "", true, "identity-public.example.test")
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHandleJSONWebKeySetReturnsNotFoundAfterConnectionDeletion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connection := createConnection(t, ctx, ti, "https://acme.okta.com")
	require.NoError(t, ti.service.Delete(ctx, &gen.DeletePayload{ID: connection.ID, SessionToken: nil, ApikeyToken: nil}))

	recorder := serveJSONWebKeySet(t, ctx, ti, connection.ID, "", false)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestIdentityProviderJSONWebKeySetURLTrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	require.Equal(
		t,
		"https://api.example.test/.well-known/identity-provider/"+id.String()+"/jwks.json",
		identityproviders.IdentityProviderJSONWebKeySetURL(mustURL(t, "https://api.example.test/"), id),
	)
}

func serveJSONWebKeySet(t *testing.T, ctx context.Context, ti *testInstance, connectionID, etag string, customDomain bool, hosts ...string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/.well-known/identity-provider/"+connectionID+"/jwks.json", nil).WithContext(ctx)
	if len(hosts) > 0 {
		request.Host = hosts[0]
	}
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("connection_id", connectionID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	if etag != "" {
		request.Header.Set("If-None-Match", etag)
	}
	if customDomain {
		request = request.WithContext(customdomains.WithContext(request.Context(), &customdomains.Context{
			OrganizationID: ti.orgID,
			Domain:         "mcp.customer.example.com",
			DomainID:       uuid.New(),
		}))
	}

	recorder := httptest.NewRecorder()
	oops.ErrHandle(testenv.NewLogger(t), ti.service.HandleJSONWebKeySet).ServeHTTP(recorder, request)
	return recorder
}
