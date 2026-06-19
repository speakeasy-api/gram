package remotesessions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
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

	// The project's organization id is populated from the auth context.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, result.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, result.OrganizationID)

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

func TestCreateRemoteSessionIssuer_NameStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "My IdP"
	payload := newIssuerPayload("idp-name-stored")
	payload.Name = &name

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.Name)
	require.Equal(t, "My IdP", *result.Name)

	// The audit subject display name reflects the name when set.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, "My IdP", record.SubjectDisplay)
}

func TestCreateRemoteSessionIssuer_NameTrimmed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "  Trimmed Name  "
	payload := newIssuerPayload("idp-name-trimmed")
	payload.Name = &name

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.Name)
	require.Equal(t, "Trimmed Name", *result.Name)
}

func TestCreateRemoteSessionIssuer_NameEmptyTreatedAsNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "   "
	payload := newIssuerPayload("idp-name-empty")
	payload.Name = &name

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Nil(t, result.Name)

	// With no name, the audit subject display name falls back to the issuer URL.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, "https://idp.example.com", record.SubjectDisplay)
}

func TestCreateRemoteSessionIssuer_InvalidLogoAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	badID := "not-a-uuid"
	payload := newIssuerPayload("idp-bad-logo")
	payload.LogoAssetID = &badID

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

func TestUpdateRemoteSessionIssuer_SetsName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-update-name"))
	require.NoError(t, err)
	require.Nil(t, created.Name)

	name := "Renamed IdP"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Name: &name,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "Renamed IdP", *updated.Name)
}

// An explicit empty string clears the name to NULL, mirroring the nullable
// endpoint columns.
func TestUpdateRemoteSessionIssuer_ClearsName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "Initial Name"
	createPayload := newIssuerPayload("idp-clear-name")
	createPayload.Name = &name
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)
	require.NotNil(t, created.Name)

	empty := ""
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Name: &empty,
	})
	require.NoError(t, err)
	require.Nil(t, updated.Name)
}

// An omitted name (nil) leaves the existing value untouched.
func TestUpdateRemoteSessionIssuer_OmittedNameKeepsExisting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "Keep Me"
	createPayload := newIssuerPayload("idp-keep-name")
	createPayload.Name = &name
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)

	newSlug := "idp-keep-name-renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Slug: &newSlug,
		Name: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "Keep Me", *updated.Name)
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

// An explicit empty string on any of the four nullable endpoint fields
// clears the column to NULL. registration_endpoint clearing is the
// operator-facing path for disabling DCR on a saved issuer.
func TestUpdateRemoteSessionIssuer_ClearsNullableEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-clear"))
	require.NoError(t, err)
	require.NotNil(t, created.AuthorizationEndpoint)
	require.NotNil(t, created.TokenEndpoint)
	require.NotNil(t, created.RegistrationEndpoint)
	require.NotNil(t, created.JwksURI)

	empty := ""
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              nil,
		Issuer:                            nil,
		AuthorizationEndpoint:             &empty,
		TokenEndpoint:                     &empty,
		RegistrationEndpoint:              &empty,
		JwksURI:                           &empty,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.NoError(t, err)
	require.Nil(t, updated.AuthorizationEndpoint)
	require.Nil(t, updated.TokenEndpoint)
	require.Nil(t, updated.RegistrationEndpoint)
	require.Nil(t, updated.JwksURI)
}

// Omitting a nullable endpoint field keeps the existing value rather than
// clearing it. Guards against future regressions in the three-state
// COALESCE/CASE shape of UpdateRemoteSessionIssuer.
func TestUpdateRemoteSessionIssuer_OmittedKeepsExisting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-keep"))
	require.NoError(t, err)
	require.NotNil(t, created.RegistrationEndpoint)

	newSlug := "idp-keep-renamed"
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
	require.NotNil(t, updated.RegistrationEndpoint)
	require.Equal(t, *created.RegistrationEndpoint, *updated.RegistrationEndpoint)
}

func TestUpdateRemoteSessionIssuer_BadRequestEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-empty-slug"))
	require.NoError(t, err)

	empty := ""
	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              &empty,
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
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateRemoteSessionIssuer_BadRequestEmptyIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-empty-issuer"))
	require.NoError(t, err)

	empty := ""
	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              nil,
		Issuer:                            &empty,
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
	requireOopsCode(t, err, oops.CodeBadRequest)
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

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err, "delete is idempotent: missing issuer returns success")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "no audit entry when there was nothing to delete")
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

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestService(t)

	draft, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           idp.OAuth21URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft)
	require.Equal(t, idp.OAuth21URL, draft.Issuer)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.NotNil(t, draft.TokenEndpoint)
	require.NotNil(t, draft.JwksURI)
	require.NotNil(t, draft.RegistrationEndpoint)
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

// statusOnlyServer returns an httptest.Server that responds to the well-known
// path with the supplied HTTP status and no body. Use it to exercise the
// discoveryFailure → UserMessage path in DiscoverRemoteSessionIssuer.
func statusOnlyServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-authorization-server") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDiscoverRemoteSessionIssuer_NotFoundSurfacesWellKnownURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := statusOnlyServer(t, http.StatusNotFound)

	_, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Contains(t, err.Error(), "OAuth metadata not found at")
	require.Contains(t, err.Error(), "/.well-known/oauth-authorization-server")
}

func TestDiscoverRemoteSessionIssuer_UnexpectedStatusSurfacesCode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := statusOnlyServer(t, http.StatusServiceUnavailable)

	_, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Contains(t, err.Error(), "Unexpected HTTP 503")
	require.Contains(t, err.Error(), "/.well-known/oauth-authorization-server")
}

func TestDiscoverRemoteSessionIssuer_OpenIDConfigurationFallback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Upstream advertises metadata only under the OpenID Connect Discovery
	// path. Many IdPs (Auth0, Okta, Google) serve no oauth-authorization-server
	// document, so discovery must fall back to openid-configuration.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"jwks_uri":               server.URL + "/jwks",
			"registration_endpoint":  server.URL + "/register",
		})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.NotNil(t, draft.TokenEndpoint)
	require.Equal(t, []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
	}, probedPaths, "oauth-authorization-server first, then openid-configuration")
}

func TestDiscoverRemoteSessionIssuer_OriginStyleFallbackStripsPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Issuer carries a path component but the upstream serves metadata only at
	// the origin-root well-known URL (a common gateway / SPA catch-all shape).
	// The path-aware candidates 404, so discovery must fall back to the
	// path-stripped origin-style location.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/oauth-authorization-server" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"jwks_uri":               server.URL + "/jwks",
			"registration_endpoint":  server.URL + "/register",
		})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL + "/tenant",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.Equal(t, []string{
		"/.well-known/oauth-authorization-server/tenant",
		"/.well-known/openid-configuration/tenant",
		"/tenant/.well-known/openid-configuration",
		"/.well-known/oauth-authorization-server",
	}, probedPaths, "path-aware candidates 404, fall back to origin-style")
}

func TestDiscoverRemoteSessionIssuer_SkipsCatchAll200WithoutEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// A SPA/gateway catch-all answers every path-aware candidate with a 200
	// that parses but carries no usable OAuth endpoints. Discovery must treat
	// those as misses and keep probing until it reaches the origin-style
	// oauth-authorization-server URL that serves the real document.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"jwks_uri":               server.URL + "/jwks",
				"registration_endpoint":  server.URL + "/register",
			})
			return
		}
		// Catch-all: 200 with no authorization_endpoint / token_endpoint.
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL + "/tenant",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.NotNil(t, draft.TokenEndpoint)
	require.Equal(t, []string{
		"/.well-known/oauth-authorization-server/tenant",
		"/.well-known/openid-configuration/tenant",
		"/tenant/.well-known/openid-configuration",
		"/.well-known/oauth-authorization-server",
	}, probedPaths, "incomplete catch-all 200s skipped until the real document")
}

func TestDiscoverRemoteSessionIssuer_IncompleteDocReturnedAsLastResort(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Every candidate answers 200 with a parseable but endpoint-less document.
	// No candidate is usable, so discovery probes them all and surfaces the
	// first incomplete document (with warnings) rather than failing outright.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.DiscoverRemoteSessionIssuer(ctx, &gen.DiscoverRemoteSessionIssuerPayload{
		Issuer:           server.URL + "/tenant",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, draft.AuthorizationEndpoint)
	require.Nil(t, draft.TokenEndpoint)
	require.NotEmpty(t, draft.DiscoveryWarnings)
	require.Len(t, probedPaths, 5, "all candidates probed before falling back to the incomplete document")
}
