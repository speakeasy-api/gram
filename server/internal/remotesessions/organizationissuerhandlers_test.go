package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// newOrgIssuerPayload returns a CreateOrganizationRemoteSessionIssuerPayload
// with a fresh slug.
func newOrgIssuerPayload(slug string) *orggen.CreateOrganizationRemoteSessionIssuerPayload {
	authEP := "https://idp.example.com/authorize"
	tokenEP := "https://idp.example.com/token"
	oidc := false
	passthrough := false
	return &orggen.CreateOrganizationRemoteSessionIssuerPayload{
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             &authEP,
		TokenEndpoint:                     &tokenEP,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   []string{"openid", "profile"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              &oidc,
		Passthrough:                       &passthrough,
		SessionToken:                      nil,
		ApikeyToken:                       nil,
	}
}

func TestCreateOrganizationRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-create"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, "org-idp-create", result.Slug)

	// Organization-level issuers carry no project id and are scoped to the org.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Empty(t, result.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, result.OrganizationID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestCreateOrganizationRemoteSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// org:read is insufficient for a create — org:admin is required.
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-rbac"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateOrganizationRemoteSessionIssuer_BadRequestEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload(""))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetOrganizationRemoteSessionIssuer_ByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-get"))
	require.NoError(t, err)

	fetched, err := ti.service.GetOrganizationRemoteSessionIssuer(ctx, &orggen.GetOrganizationRemoteSessionIssuerPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Empty(t, fetched.ProjectID)
}

func TestGetOrganizationRemoteSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// A project-level issuer must not be reachable via the org-level getter.
	projectIssuer, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("org-idp-notfound"))
	require.NoError(t, err)

	_, err = ti.service.GetOrganizationRemoteSessionIssuer(ctx, &orggen.GetOrganizationRemoteSessionIssuerPayload{
		ID:           projectIssuer.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListOrganizationRemoteSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	orgIssuer, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-list-1"))
	require.NoError(t, err)

	// A project-level issuer must not leak into the org-level list.
	projectIssuer, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("org-idp-list-project"))
	require.NoError(t, err)

	result, err := ti.service.ListOrganizationRemoteSessionIssuers(ctx, &orggen.ListOrganizationRemoteSessionIssuersPayload{
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	ids := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		ids[item.ID] = true
	}
	require.True(t, ids[orgIssuer.ID], "org-level issuer should be listed")
	require.False(t, ids[projectIssuer.ID], "project-level issuer should not be listed")
}

func TestUpdateOrganizationRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-update"))
	require.NoError(t, err)

	newIssuer := "https://renamed.example.com"
	updated, err := ti.service.UpdateOrganizationRemoteSessionIssuer(ctx, &orggen.UpdateOrganizationRemoteSessionIssuerPayload{
		ID:                                created.ID,
		Issuer:                            &newIssuer,
		Slug:                              nil,
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
		SessionToken:                      nil,
		ApikeyToken:                       nil,
	})
	require.NoError(t, err)
	require.Equal(t, newIssuer, updated.Issuer)
	require.Equal(t, created.Slug, updated.Slug)
	require.Empty(t, updated.ProjectID)
}

func TestDeleteOrganizationRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-delete"))
	require.NoError(t, err)

	err = ti.service.DeleteOrganizationRemoteSessionIssuer(ctx, &orggen.DeleteOrganizationRemoteSessionIssuerPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	_, err = ti.service.GetOrganizationRemoteSessionIssuer(ctx, &orggen.GetOrganizationRemoteSessionIssuerPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestProjectReadsInheritOrgLevelIssuers asserts that project-scoped list/get
// surface organization-level issuers belonging to the project's org.
func TestProjectReadsInheritOrgLevelIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	orgIssuer, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-inherit"))
	require.NoError(t, err)

	projectIssuer, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("project-idp-inherit"))
	require.NoError(t, err)

	// Project list returns both the project's own issuer and the inherited
	// org-level issuer.
	listed, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	ids := make(map[string]bool, len(listed.Items))
	for _, item := range listed.Items {
		ids[item.ID] = true
	}
	require.True(t, ids[projectIssuer.ID], "project-level issuer should be listed")
	require.True(t, ids[orgIssuer.ID], "inherited org-level issuer should be listed")

	// Project get-by-id resolves the inherited org-level issuer.
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &orgIssuer.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, orgIssuer.ID, fetched.ID)
	require.Empty(t, fetched.ProjectID)
}

// TestProjectGetBySlugIsProjectScoped asserts that get-by-slug resolves only
// project-level issuers: a project-level slug is found, while an org-level
// issuer is not slug-addressable (it is reachable by id instead). This keeps
// slug lookups deterministic via the per-project unique index.
func TestProjectGetBySlugIsProjectScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	orgIssuer, err := ti.service.CreateOrganizationRemoteSessionIssuer(ctx, newOrgIssuerPayload("org-idp-byslug"))
	require.NoError(t, err)

	projectIssuer, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("project-idp-byslug"))
	require.NoError(t, err)

	// The project-level slug resolves.
	projectSlug := projectIssuer.Slug
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               nil,
		Slug:             &projectSlug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, projectIssuer.ID, fetched.ID)
	require.NotEmpty(t, fetched.ProjectID)

	// The org-level issuer's slug is not resolvable via the project get-by-slug.
	orgSlug := orgIssuer.Slug
	_, err = ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               nil,
		Slug:             &orgSlug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	// But it is reachable by id (inherited into the project read path).
	byID, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &orgIssuer.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, orgIssuer.ID, byID.ID)
	require.Empty(t, byID.ProjectID)
}
