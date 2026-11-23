package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
)

// idJAGTrustStub answers every trust lookup with err.
type idJAGTrustStub struct {
	err error
}

func (s idJAGTrustStub) TrustedIssuer(context.Context, string, uuid.UUID) (idjag.TrustedIssuer, error) {
	return idjag.TrustedIssuer{}, s.err
}

func (s idJAGTrustStub) ResolveUser(context.Context, string, string) (string, error) {
	return "", s.err
}

var errIDJAGTrustLookup = errors.New("database unavailable")

// metadataCheck asserts on a metadata handler's response and returned error.
type metadataCheck func(t *testing.T, w *httptest.ResponseRecorder, err error)

func serveEndpointMetadataWithIDJAGTrust(t *testing.T, trustErr error, check metadataCheck) {
	t.Helper()
	ctx, ti, _, _ := newIDJAGTestService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, sharedTestUpstreamIssuer, "")
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	toolset = setToolsetIssuer(t, ctx, ti.conn, *authCtx.ProjectID, toolset, issuer.ID)
	ti.service.SetIDJAGTrust(idJAGTrustStub{err: trustErr})

	slug := toolset.McpSlug.String
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+slug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	check(t, w, ti.service.HandleGetAuthorizationServer(w, req))
}

func serveSharedMetadataWithIDJAGTrust(t *testing.T, trustErr error, check metadataCheck) {
	t.Helper()
	ctx, ti, _, _ := newIDJAGTestService(t)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, requireProjectAuthContext(t, ctx).ActiveOrganizationID, sharedTestUpstreamIssuer, "")
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")
	ti.service.SetIDJAGTrust(idJAGTrustStub{err: trustErr})

	issuerURL := ti.serverURL.String() + "/oauth/usi/" + issuer.ID.String()
	w := httptest.NewRecorder()
	check(t, w, ti.service.HandleSharedAuthorizationServerMetadata(w, sharedRequest(ctx, http.MethodGet, issuerURL+"/.well-known/oauth-authorization-server", issuer.ID.String(), nil)))
}

func requireMetadataFailedClosed(t *testing.T, _ *httptest.ResponseRecorder, err error) {
	t.Helper()
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func requireMetadataOmitsIDJAGProfile(t *testing.T, w *httptest.ResponseRecorder, err error) {
	t.Helper()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var metadata struct {
		Profiles []string `json:"authorization_grant_profiles_supported"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.NotContains(t, metadata.Profiles, oauthwire.GrantProfileIDJAG)
}

func TestHandleGetAuthorizationServer_OmitsIDJAGWhenTrustLinkMissing(t *testing.T) {
	t.Parallel()
	serveEndpointMetadataWithIDJAGTrust(t, idjag.ErrNoTrustedIssuer, requireMetadataOmitsIDJAGProfile)
}

// Clients cache metadata, so a transient trust lookup error must not serve a document without the grant.
func TestHandleGetAuthorizationServer_FailsClosedOnIDJAGTrustLookupError(t *testing.T) {
	t.Parallel()
	serveEndpointMetadataWithIDJAGTrust(t, errIDJAGTrustLookup, requireMetadataFailedClosed)
}

func TestSharedAuthorizationServer_MetadataOmitsIDJAGWhenTrustLinkMissing(t *testing.T) {
	t.Parallel()
	serveSharedMetadataWithIDJAGTrust(t, idjag.ErrNoTrustedIssuer, requireMetadataOmitsIDJAGProfile)
}

func TestSharedAuthorizationServer_MetadataFailsClosedOnIDJAGTrustLookupError(t *testing.T) {
	t.Parallel()
	serveSharedMetadataWithIDJAGTrust(t, errIDJAGTrustLookup, requireMetadataFailedClosed)
}
