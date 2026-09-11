package remotesessions_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func clientJSONWebKeySetRequest(t *testing.T, id string, customDomain bool) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-client/"+id+"/jwks.json", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if customDomain {
		ctx = customdomains.WithContext(ctx, &customdomains.Context{
			OrganizationID: "org-jwks",
			Domain:         "mcp.customer.example.com",
			DomainID:       uuid.New(),
		})
	}

	return req.WithContext(ctx)
}

func attachJsonWebKeySet(t *testing.T, ctx context.Context, ti *testInstance, clientID string, setID uuid.UUID) {
	t.Helper()

	_, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		ID:              clientID,
		JSONWebKeySetID: setID.String(),
	})
	require.NoError(t, err)
}

func TestClientJSONWebKeySetURL(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	want := "https://app.getgram.ai/.well-known/oauth-client/" + id.String() + "/jwks.json"
	require.Equal(t, want, remotesessions.ClientJSONWebKeySetURL(mustURL(t, "https://app.getgram.ai"), id))
	require.Equal(t, want, remotesessions.ClientJSONWebKeySetURL(mustURL(t, "https://app.getgram.ai/"), id))
}

func TestHandleClientJSONWebKeySet_ServesLiveKeysForManualClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)

	issuerID := createRemoteIssuer(t, ctx, ti, "jwks-document-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "jwks-document-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "manual-client-id")
	setID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "jwks-document-set")

	createJsonWebKey(t, ctx, ti.conn, organizationID, setID, "pending", "pending-key")
	createJsonWebKey(t, ctx, ti.conn, organizationID, setID, "active", "active-key")
	createJsonWebKey(t, ctx, ti.conn, organizationID, setID, "retired", "retired-key")
	revokedID := createJsonWebKey(t, ctx, ti.conn, organizationID, setID, "pending", "revoked-key")
	revokeJsonWebKey(t, ctx, ti.conn, organizationID, revokedID)
	attachJsonWebKeySet(t, ctx, ti, clientID, setID)

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	rec := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientJSONWebKeySet(rec, clientJSONWebKeySetRequest(t, clientID, false)))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/jwk-set+json", rec.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	var document struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &document))
	kids := make([]string, 0, len(document.Keys))
	for _, key := range document.Keys {
		kid, ok := key["kid"].(string)
		require.True(t, ok)
		kids = append(kids, kid)

		modulus, ok := key["n"].(string)
		require.True(t, ok)
		decodedModulus, err := base64.RawURLEncoding.DecodeString(modulus)
		require.NoError(t, err)
		require.Len(t, decodedModulus, 256, "fixture keys must carry a 2048-bit RSA modulus")
		require.Equal(t, "AQAB", key["e"])
	}
	require.ElementsMatch(t, []string{"pending-key", "active-key", "retired-key"}, kids)
	require.NotContains(t, kids, "revoked-key")

	conditionalReq := clientJSONWebKeySetRequest(t, clientID, false)
	conditionalReq.Header.Set("If-None-Match", etag)
	conditionalRec := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientJSONWebKeySet(conditionalRec, conditionalReq))
	require.Equal(t, http.StatusNotModified, conditionalRec.Code)
	require.Empty(t, conditionalRec.Body.String())
}

func TestHandleClientJSONWebKeySet_ServesEmptyAttachedSet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)

	issuerID := createRemoteIssuer(t, ctx, ti, "jwks-empty-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "jwks-empty-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "empty-set-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "jwks-empty-set")
	attachJsonWebKeySet(t, ctx, ti, clientID, setID)

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	rec := httptest.NewRecorder()
	require.NoError(t, mgr.HandleClientJSONWebKeySet(rec, clientJSONWebKeySetRequest(t, clientID, false)))
	require.JSONEq(t, `{"keys":[]}`, rec.Body.String())
}

func TestHandleClientJSONWebKeySet_NotFoundWithoutAttachedSet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createRemoteIssuer(t, ctx, ti, "jwks-no-set-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "jwks-no-set-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "no-set-client")

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	err := mgr.HandleClientJSONWebKeySet(httptest.NewRecorder(), clientJSONWebKeySetRequest(t, clientID, false))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestHandleClientJSONWebKeySet_NotFoundUnknownID(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)

	err := mgr.HandleClientJSONWebKeySet(httptest.NewRecorder(), clientJSONWebKeySetRequest(t, uuid.NewString(), false))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestHandleClientJSONWebKeySet_NotFoundInvalidID(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)
	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)

	err := mgr.HandleClientJSONWebKeySet(httptest.NewRecorder(), clientJSONWebKeySetRequest(t, "not-a-uuid", false))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestHandleClientJSONWebKeySet_NotFoundOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	organizationID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, organizationID)

	issuerID := createRemoteIssuer(t, ctx, ti, "jwks-domain-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "jwks-domain-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "domain-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, organizationID, "jwks-domain-set")
	attachJsonWebKeySet(t, ctx, ti, clientID, setID)

	mgr := newCIMDChallengeManager(t, ti, cimdServerURL)
	err := mgr.HandleClientJSONWebKeySet(httptest.NewRecorder(), clientJSONWebKeySetRequest(t, clientID, true))
	requireOopsCode(t, err, oops.CodeNotFound)
}
