package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// client_setup_documentation_url is operator-supplied (not RFC 8414) and is
// rendered as a link in the New Client sheet, so every write surface validates
// it. These tests cover the three-state patch semantics — omit keeps, an
// explicit empty string clears to NULL, any other value must be an absolute
// http(s) URL — across the project-scoped, org-admin, and platform-admin
// (global) issuer services.

const docURL = "https://docs.example.com/oauth/apps"

// plainHTTPDocURL locks in that a plain http:// URL is accepted, not just
// https://. Validation goes through urls.IsAbsoluteHTTP, which allows both; a
// regression that narrowed it to https-only would otherwise pass every test
// that uses the https docURL constant.
const plainHTTPDocURL = "http://docs.example.com/oauth/apps"

// --- project-scoped remoteSessionIssuers ---

func TestCreateRemoteSessionIssuer_ClientSetupDocumentationURLStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := docURL
	payload := newIssuerPayload("idp-doc-url-stored")
	payload.ClientSetupDocumentationURL = &url

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *result.ClientSetupDocumentationURL)
}

// A plain http:// URL is accepted and stored, not just https://.
func TestCreateRemoteSessionIssuer_ClientSetupDocumentationURLAcceptsPlainHTTP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := plainHTTPDocURL
	payload := newIssuerPayload("idp-doc-url-http")
	payload.ClientSetupDocumentationURL = &url

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.ClientSetupDocumentationURL)
	require.Equal(t, plainHTTPDocURL, *result.ClientSetupDocumentationURL)
}

// An empty string on create stores NULL rather than an empty column value.
func TestCreateRemoteSessionIssuer_ClientSetupDocumentationURLEmptyTreatedAsNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	empty := ""
	payload := newIssuerPayload("idp-doc-url-empty")
	payload.ClientSetupDocumentationURL = &empty

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Nil(t, result.ClientSetupDocumentationURL)
}

// A javascript: URL parses cleanly under url.Parse and react-router forwards it
// verbatim into an <a href>, so rejecting it here is what prevents script
// execution on click.
func TestCreateRemoteSessionIssuer_ClientSetupDocumentationURLRejectsJavascriptScheme(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := "javascript:alert(1)"
	payload := newIssuerPayload("idp-doc-url-js")
	payload.ClientSetupDocumentationURL = &url

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateRemoteSessionIssuer_ClientSetupDocumentationURLRejectsMissingHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := "https://"
	payload := newIssuerPayload("idp-doc-url-nohost")
	payload.ClientSetupDocumentationURL = &url

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateRemoteSessionIssuer_SetsClientSetupDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-doc-url-set"))
	require.NoError(t, err)
	require.Nil(t, created.ClientSetupDocumentationURL)

	url := docURL
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                          created.ID,
		ClientSetupDocumentationURL: &url,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *updated.ClientSetupDocumentationURL)
}

// An explicit empty string clears the column to NULL.
func TestUpdateRemoteSessionIssuer_ClearsClientSetupDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := docURL
	createPayload := newIssuerPayload("idp-doc-url-clear")
	createPayload.ClientSetupDocumentationURL = &url
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)
	require.NotNil(t, created.ClientSetupDocumentationURL)

	empty := ""
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                          created.ID,
		ClientSetupDocumentationURL: &empty,
	})
	require.NoError(t, err)
	require.Nil(t, updated.ClientSetupDocumentationURL)
}

// An omitted value (nil) leaves the existing URL untouched.
func TestUpdateRemoteSessionIssuer_OmittedClientSetupDocumentationURLKeepsExisting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := docURL
	createPayload := newIssuerPayload("idp-doc-url-keep")
	createPayload.ClientSetupDocumentationURL = &url
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)

	newSlug := "idp-doc-url-keep-renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                          created.ID,
		Slug:                        &newSlug,
		ClientSetupDocumentationURL: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *updated.ClientSetupDocumentationURL)
}

func TestUpdateRemoteSessionIssuer_RejectsInvalidClientSetupDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-doc-url-invalid"))
	require.NoError(t, err)

	url := "javascript:alert(1)"
	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                          created.ID,
		ClientSetupDocumentationURL: &url,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// --- org-admin organizationRemoteSessionIssuers ---

// newUpdateIssuerClientSetupDocURLPayload patches only the client setup
// documentation URL on an org-admin issuer.
func newUpdateIssuerClientSetupDocURLPayload(id string, url *string) *orgissuersgen.UpdateIssuerPayload {
	return &orgissuersgen.UpdateIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ID:                                id,
		Slug:                              nil,
		Issuer:                            nil,
		Name:                              nil,
		LogoAssetID:                       nil,
		ClientSetupDocumentationURL:       url,
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
	}
}

func TestCreateIssuer_ClientSetupDocumentationURLStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := docURL
	payload := newCreateIssuerPayload("admin-doc-url-stored", nil)
	payload.ClientSetupDocumentationURL = &url

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *created.ClientSetupDocumentationURL)
}

// A plain http:// URL is accepted and stored, not just https://.
func TestCreateIssuer_ClientSetupDocumentationURLAcceptsPlainHTTP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := plainHTTPDocURL
	payload := newCreateIssuerPayload("admin-doc-url-http", nil)
	payload.ClientSetupDocumentationURL = &url

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ClientSetupDocumentationURL)
	require.Equal(t, plainHTTPDocURL, *created.ClientSetupDocumentationURL)
}

func TestCreateIssuer_ClientSetupDocumentationURLRejectsJavascriptScheme(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	url := "javascript:alert(1)"
	payload := newCreateIssuerPayload("admin-doc-url-js", nil)
	payload.ClientSetupDocumentationURL = &url

	_, err := ti.service.CreateIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Walks the three-state patch semantics: set, omit (keep), then clear.
func TestUpdateIssuer_ClientSetupDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-doc-url-update", nil))
	require.NoError(t, err)
	require.Nil(t, created.ClientSetupDocumentationURL)

	url := docURL
	updated, err := ti.service.UpdateIssuer(ctx, newUpdateIssuerClientSetupDocURLPayload(created.ID, &url))
	require.NoError(t, err)
	require.NotNil(t, updated.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *updated.ClientSetupDocumentationURL)

	kept, err := ti.service.UpdateIssuer(ctx, newUpdateIssuerClientSetupDocURLPayload(created.ID, nil))
	require.NoError(t, err)
	require.NotNil(t, kept.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *kept.ClientSetupDocumentationURL)

	empty := ""
	cleared, err := ti.service.UpdateIssuer(ctx, newUpdateIssuerClientSetupDocURLPayload(created.ID, &empty))
	require.NoError(t, err)
	require.Nil(t, cleared.ClientSetupDocumentationURL)
}

func TestUpdateIssuer_RejectsInvalidClientSetupDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("admin-doc-url-bad", nil))
	require.NoError(t, err)

	url := "https://"
	_, err = ti.service.UpdateIssuer(ctx, newUpdateIssuerClientSetupDocURLPayload(created.ID, &url))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// --- platform-admin adminRemoteSessions (global issuers) ---

// updateGlobalIssuerClientSetupDocURL patches only the client setup
// documentation URL on a global issuer.
func updateGlobalIssuerClientSetupDocURL(id string, url *string) *adminrsgen.UpdateGlobalIssuerPayload {
	return &adminrsgen.UpdateGlobalIssuerPayload{
		SessionToken:                      nil,
		ID:                                id,
		Slug:                              nil,
		Issuer:                            nil,
		Name:                              nil,
		LogoAssetID:                       nil,
		ClientSetupDocumentationURL:       url,
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
		ClientIDMetadataDocumentSupported: nil,
	}
}

func TestAdminRemoteSessions_CreateGlobalIssuer_ClientSetupDocumentationURLStored(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	url := docURL
	payload := createGlobalIssuer(t, "global-doc-url-stored")
	payload.ClientSetupDocumentationURL = &url

	created, err := ti.service.CreateGlobalIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *created.ClientSetupDocumentationURL)
}

// A plain http:// URL is accepted and stored, not just https://.
func TestAdminRemoteSessions_CreateGlobalIssuer_ClientSetupDocumentationURLAcceptsPlainHTTP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	url := plainHTTPDocURL
	payload := createGlobalIssuer(t, "global-doc-url-http")
	payload.ClientSetupDocumentationURL = &url

	created, err := ti.service.CreateGlobalIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ClientSetupDocumentationURL)
	require.Equal(t, plainHTTPDocURL, *created.ClientSetupDocumentationURL)
}

func TestAdminRemoteSessions_CreateGlobalIssuer_ClientSetupDocumentationURLRejectsJavascriptScheme(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	url := "javascript:alert(1)"
	payload := createGlobalIssuer(t, "global-doc-url-js")
	payload.ClientSetupDocumentationURL = &url

	_, err := ti.service.CreateGlobalIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Walks the three-state patch semantics on the global surface.
func TestAdminRemoteSessions_UpdateGlobalIssuer_ClientSetupDocumentationURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	created, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "global-doc-url-update"))
	require.NoError(t, err)
	require.Nil(t, created.ClientSetupDocumentationURL)

	url := docURL
	updated, err := ti.service.UpdateGlobalIssuer(ctx, updateGlobalIssuerClientSetupDocURL(created.ID, &url))
	require.NoError(t, err)
	require.NotNil(t, updated.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *updated.ClientSetupDocumentationURL)

	kept, err := ti.service.UpdateGlobalIssuer(ctx, updateGlobalIssuerClientSetupDocURL(created.ID, nil))
	require.NoError(t, err)
	require.NotNil(t, kept.ClientSetupDocumentationURL)
	require.Equal(t, docURL, *kept.ClientSetupDocumentationURL)

	empty := ""
	cleared, err := ti.service.UpdateGlobalIssuer(ctx, updateGlobalIssuerClientSetupDocURL(created.ID, &empty))
	require.NoError(t, err)
	require.Nil(t, cleared.ClientSetupDocumentationURL)
}

func TestAdminRemoteSessions_UpdateGlobalIssuer_RejectsInvalidClientSetupDocumentationURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	created, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "global-doc-url-bad"))
	require.NoError(t, err)

	url := "ftp://files.example.com/readme"
	_, err = ti.service.UpdateGlobalIssuer(ctx, updateGlobalIssuerClientSetupDocURL(created.ID, &url))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}
