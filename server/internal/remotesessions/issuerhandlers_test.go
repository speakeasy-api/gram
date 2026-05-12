package remotesessions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// newIssuerPayload returns a CreateRemoteSessionIssuerPayload with a fresh
// project-unique slug.
func newIssuerPayload(slug string) *gen.CreateRemoteSessionIssuerPayload {
	authEP := "https://idp.example.com/authorize"
	tokenEP := "https://idp.example.com/token"
	regEP := "https://idp.example.com/register"
	jwksURI := "https://idp.example.com/jwks"
	oidc := false
	passthrough := false
	return &gen.CreateRemoteSessionIssuerPayload{
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             &authEP,
		TokenEndpoint:                     &tokenEP,
		RegistrationEndpoint:              &regEP,
		JwksURI:                           &jwksURI,
		ScopesSupported:                   []string{"openid", "profile"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              &oidc,
		Passthrough:                       &passthrough,
	}
}

func TestCreateRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-create"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, "idp-create", result.Slug)
	require.Equal(t, "https://idp.example.com", result.Issuer)
	require.NotNil(t, result.AuthorizationEndpoint)
	require.Equal(t, "https://idp.example.com/authorize", *result.AuthorizationEndpoint)
	require.False(t, result.Oidc)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateRemoteSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Hand the caller only read scope; create should be denied.
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-rbac"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateRemoteSessionIssuer_BadRequestEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	payload := newIssuerPayload("")
	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetRemoteSessionIssuer_ByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-get-id"))
	require.NoError(t, err)

	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, created.Slug, fetched.Slug)
}

func TestGetRemoteSessionIssuer_BySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-get-slug"))
	require.NoError(t, err)

	slug := created.Slug
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               nil,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
}

func TestGetRemoteSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	_, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetRemoteSessionIssuer_BothIDAndSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := "x"
	_, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &id,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListRemoteSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-list-1"))
	require.NoError(t, err)
	_, err = ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-list-2"))
	require.NoError(t, err)

	result, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Items), 2)
}

func TestListRemoteSessionIssuers_PaginationTraversal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	const total = 5
	wantIDs := make(map[string]bool, total)
	for range total {
		created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload(uuid.NewString()))
		require.NoError(t, err)
		wantIDs[created.ID] = true
	}

	pageSize := 2
	gotIDs := make(map[string]bool, total)
	var cursor *string
	pages := 0
	for {
		pages++
		require.Less(t, pages, 10, "pagination did not terminate")
		result, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
			Cursor:           cursor,
			Limit:            &pageSize,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		for _, item := range result.Items {
			require.False(t, gotIDs[item.ID], "duplicate id across pages: %s", item.ID)
			gotIDs[item.ID] = true
		}
		if result.NextCursor == nil {
			break
		}
		cursor = result.NextCursor
	}
	for id := range wantIDs {
		require.True(t, gotIDs[id], "issuer %s missing from paginated traversal", id)
	}
}

func TestListRemoteSessionIssuers_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// No grants installed for this principal; list should be denied.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-update"))
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	newSlug := "idp-update-renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              &newSlug,
		Issuer:                            nil,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.NoError(t, err)
	require.Equal(t, "idp-update-renamed", updated.Slug)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateRemoteSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := "anything"
	_, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                id,
		Slug:                              &slug,
		Issuer:                            nil,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-delete"))
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Subsequent reads should miss.
	_, err = ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteRemoteSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// fakeIssuerServer returns an httptest.Server that serves an RFC 8414
// well-known document derived from its own URL. Use the `mutate` callback to
// drop fields and exercise the warnings path.
func fakeIssuerServer(t *testing.T, mutate func(doc map[string]any)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-authorization-server") {
			http.NotFound(w, r)
			return
		}
		doc := map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"registration_endpoint":                 server.URL + "/register",
			"jwks_uri":                              server.URL + "/jwks",
			"scopes_supported":                      []string{"openid"},
			"grant_types_supported":                 []string{"authorization_code"},
			"response_types_supported":              []string{"code"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		}
		if mutate != nil {
			mutate(doc)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDiscoverRemoteSessionIssuer_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	server := fakeIssuerServer(t, nil)

	draft, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft)
	require.Equal(t, server.URL, draft.Issuer)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.NotNil(t, draft.JwksURI)
	require.Empty(t, draft.DiscoveryWarnings)
}

func TestDiscoverRemoteSessionIssuer_WithWarnings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Drop jwks_uri and token_endpoint to force warnings.
	server := fakeIssuerServer(t, func(doc map[string]any) {
		delete(doc, "jwks_uri")
		delete(doc, "token_endpoint")
	})

	draft, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, draft.DiscoveryWarnings)
	require.Nil(t, draft.JwksURI)
	require.Nil(t, draft.TokenEndpoint)
}

func TestDiscoverRemoteSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           "https://idp.example.com",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDiscoverRemoteSessionIssuer_BadURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           "ftp://not-http",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}
