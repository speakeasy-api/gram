package remotesessions_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// createGlobalIssuer seeds a global remote_session_issuer through the admin
// surface and returns its view. The caller supplies an admin context.
func createGlobalIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug string) *adminrsgen.CreateGlobalIssuerPayload {
	t.Helper()
	payload := &adminrsgen.CreateGlobalIssuerPayload{
		SessionToken:                      nil,
		Slug:                              slug,
		Issuer:                            "https://" + slug + ".example.com",
		Name:                              nil,
		LogoAssetID:                       nil,
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
	return payload
}

func TestAdminRemoteSessions_CreateGlobalIssuer_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	issuer, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, ctx, ti, "hubspot"))
	require.NoError(t, err)
	require.NotEmpty(t, issuer.ID)
	require.Equal(t, "hubspot", issuer.Slug)
	// Global rows serialize project_id / organization_id as empty.
	require.Empty(t, issuer.ProjectID)
	require.Empty(t, issuer.OrganizationID)
}

func TestAdminRemoteSessions_CreateGlobalIssuer_RequiresAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	// Default (non-admin) context.

	_, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, ctx, ti, "hubspot"))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestAdminRemoteSessions_CreateGlobalIssuer_SlugConflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	_, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, ctx, ti, "dupe"))
	require.NoError(t, err)

	_, err = ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, ctx, ti, "dupe"))
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestAdminRemoteSessions_ListAndGetGlobalIssuers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	created, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, ctx, ti, "google-workspace"))
	require.NoError(t, err)

	list, err := ti.service.ListGlobalIssuers(ctx, &adminrsgen.ListGlobalIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, created.ID, list.Items[0].ID)

	got, err := ti.service.GetGlobalIssuer(ctx, &adminrsgen.GetGlobalIssuerPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
}

func TestAdminRemoteSessions_GetGlobalIssuer_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	_, err := ti.service.GetGlobalIssuer(ctx, &adminrsgen.GetGlobalIssuerPayload{ID: "00000000-0000-0000-0000-000000000000", SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAdminRemoteSessions_UpdateGlobalIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	created, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, ctx, ti, "rename-me"))
	require.NoError(t, err)

	newSlug := "renamed"
	updated, err := ti.service.UpdateGlobalIssuer(ctx, &adminrsgen.UpdateGlobalIssuerPayload{
		SessionToken:                      nil,
		ID:                                created.ID,
		Slug:                              &newSlug,
		Issuer:                            nil,
		Name:                              nil,
		LogoAssetID:                       nil,
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
	})
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.Slug)
}

func TestAdminRemoteSessions_GlobalClientLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	issuer, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, ctx, ti, "client-host"))
	require.NoError(t, err)

	secret := "s3cr3t"
	client, err := ti.service.CreateGlobalClient(ctx, &adminrsgen.CreateGlobalClientPayload{
		SessionToken:            nil,
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "client-abc",
		ClientSecret:            &secret,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	require.NoError(t, err)
	require.Equal(t, "client-abc", client.ClientID)
	require.Equal(t, issuer.ID, client.RemoteSessionIssuerID)
	// Global clients have no project and no user_session_issuer attachments.
	require.Empty(t, client.ProjectID)
	require.Empty(t, client.UserSessionIssuerIds)

	list, err := ti.service.ListGlobalClients(ctx, &adminrsgen.ListGlobalClientsPayload{
		RemoteSessionIssuerID: issuer.ID,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
	})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, client.ID, list.Items[0].ID)

	// Issuer delete is blocked while a live client references it.
	err = ti.service.DeleteGlobalIssuer(ctx, &adminrsgen.DeleteGlobalIssuerPayload{ID: issuer.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	// Delete the client, then the issuer succeeds.
	err = ti.service.DeleteGlobalClient(ctx, &adminrsgen.DeleteGlobalClientPayload{ID: client.ID, SessionToken: nil})
	require.NoError(t, err)

	err = ti.service.DeleteGlobalIssuer(ctx, &adminrsgen.DeleteGlobalIssuerPayload{ID: issuer.ID, SessionToken: nil})
	require.NoError(t, err)
}

func TestAdminRemoteSessions_CreateGlobalClient_RejectsNonGlobalIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	// A project-scoped issuer is not global, so the admin create must reject it.
	projectIssuer := createRemoteIssuer(t, ctx, ti, "proj-issuer", "https://idp.example.com/register")

	_, err := ti.service.CreateGlobalClient(adminCtx, &adminrsgen.CreateGlobalClientPayload{
		SessionToken:            nil,
		RemoteSessionIssuerID:   projectIssuer,
		ClientID:                "client-xyz",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
