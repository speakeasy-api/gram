package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// createGlobalIssuer builds a CreateGlobalIssuer payload for the given slug. The
// caller passes it to CreateGlobalIssuer under an admin context.
func createGlobalIssuer(t *testing.T, slug string) *adminrsgen.CreateGlobalIssuerPayload {
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

	issuer, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "hubspot"))
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

	_, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "hubspot"))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestAdminRemoteSessions_CreateGlobalIssuer_SlugConflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	_, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "dupe"))
	require.NoError(t, err)

	_, err = ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "dupe"))
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestAdminRemoteSessions_ListAndGetGlobalIssuers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	created, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "google-workspace"))
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

	created, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "rename-me"))
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

func TestAdminRemoteSessions_IssuerFieldsStoredTrimmed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	// Create persists the trimmed slug/issuer, not the padded input.
	payload := createGlobalIssuer(t, "padded")
	payload.Slug = "  padded  "
	payload.Issuer = "\thttps://padded.example.com \n"
	created, err := ti.service.CreateGlobalIssuer(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "padded", created.Slug)
	require.Equal(t, "https://padded.example.com", created.Issuer)

	// Update persists trimmed values too.
	newSlug := "  renamed-padded  "
	newIssuer := " https://renamed.example.com "
	updated, err := ti.service.UpdateGlobalIssuer(ctx, &adminrsgen.UpdateGlobalIssuerPayload{
		SessionToken:                      nil,
		ID:                                created.ID,
		Slug:                              &newSlug,
		Issuer:                            &newIssuer,
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
	require.Equal(t, "renamed-padded", updated.Slug)
	require.Equal(t, "https://renamed.example.com", updated.Issuer)

	// A padded variant of an existing slug now collides instead of slipping
	// past the unique index as a visually identical duplicate.
	dupe := createGlobalIssuer(t, "renamed-padded")
	dupe.Slug = " renamed-padded "
	_, err = ti.service.CreateGlobalIssuer(ctx, dupe)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestAdminRemoteSessions_GlobalClientLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	issuer, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "client-host"))
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

func TestAdminRemoteSessions_GetGlobalClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	issuer, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "get-client-host"))
	require.NoError(t, err)

	created, err := ti.service.CreateGlobalClient(ctx, &adminrsgen.CreateGlobalClientPayload{
		SessionToken:            nil,
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "client-get",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	require.NoError(t, err)

	got, err := ti.service.GetGlobalClient(ctx, &adminrsgen.GetGlobalClientPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "client-get", got.ClientID)

	_, err = ti.service.GetGlobalClient(ctx, &adminrsgen.GetGlobalClientPayload{ID: "00000000-0000-0000-0000-000000000000", SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAdminRemoteSessions_UpdateGlobalClient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	issuer, err := ti.service.CreateGlobalIssuer(ctx, createGlobalIssuer(t, "update-client-host"))
	require.NoError(t, err)

	created, err := ti.service.CreateGlobalClient(ctx, &adminrsgen.CreateGlobalClientPayload{
		SessionToken:            nil,
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "client-update",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	require.NoError(t, err)

	newSecret := "rotated-s3cr3t"
	authMethod := "client_secret_post"
	audience := "https://api.example.com"
	updated, err := ti.service.UpdateGlobalClient(ctx, &adminrsgen.UpdateGlobalClientPayload{
		SessionToken:            nil,
		ID:                      created.ID,
		ClientSecret:            &newSecret,
		TokenEndpointAuthMethod: &authMethod,
		Scope:                   []string{"read:things"},
		Audience:                &audience,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, []string{"read:things"}, updated.Scope)
	require.Equal(t, "client_secret_post", conv.PtrValOrEmpty(updated.TokenEndpointAuthMethod, ""))
	require.Equal(t, "https://api.example.com", conv.PtrValOrEmpty(updated.Audience, ""))

	// A blank rotated secret is rejected rather than silently encrypted.
	blank := "   "
	_, err = ti.service.UpdateGlobalClient(ctx, &adminrsgen.UpdateGlobalClientPayload{
		SessionToken:            nil,
		ID:                      created.ID,
		ClientSecret:            &blank,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestAdminRemoteSessions_UpdateGlobalClient_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	secret := "s3cr3t"
	_, err := ti.service.UpdateGlobalClient(ctx, &adminrsgen.UpdateGlobalClientPayload{
		SessionToken:            nil,
		ID:                      "00000000-0000-0000-0000-000000000000",
		ClientSecret:            &secret,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAdminRemoteSessions_ClientMethods_RequireAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	// Default (non-admin) context.

	someID := "00000000-0000-0000-0000-000000000001"

	_, err := ti.service.CreateGlobalClient(ctx, &adminrsgen.CreateGlobalClientPayload{
		SessionToken:            nil,
		RemoteSessionIssuerID:   someID,
		ClientID:                "client-forbidden",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.ListGlobalClients(ctx, &adminrsgen.ListGlobalClientsPayload{
		RemoteSessionIssuerID: someID,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.GetGlobalClient(ctx, &adminrsgen.GetGlobalClientPayload{ID: someID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.UpdateGlobalClient(ctx, &adminrsgen.UpdateGlobalClientPayload{
		SessionToken:            nil,
		ID:                      someID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: nil,
		Scope:                   nil,
		Audience:                nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	err = ti.service.DeleteGlobalClient(ctx, &adminrsgen.DeleteGlobalClientPayload{ID: someID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
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

// TestDeleteGlobalIssuer_BlockedByTenantClient proves the platform-admin delete
// is blocked once a tenant attaches a client, and that the blocking client does
// not appear in the global client listing — the invisible-blocker the enriched
// 409 message calls out.
func TestDeleteGlobalIssuer_BlockedByTenantClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	adminCtx := withAdmin(t, ctx)

	globalIssuer, err := ti.service.CreateGlobalIssuer(adminCtx, createGlobalIssuer(t, "delete-guard-global"))
	require.NoError(t, err)
	issuerUUID := uuid.MustParse(globalIssuer.ID)

	// A tenant (the caller's org) attaches a client to the platform issuer.
	seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, issuerUUID, "delete-guard-tenant-client")

	// The platform admin sees zero global clients...
	list, err := ti.service.ListGlobalClients(adminCtx, &adminrsgen.ListGlobalClientsPayload{
		RemoteSessionIssuerID: globalIssuer.ID,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
	})
	require.NoError(t, err)
	require.Empty(t, list.Items, "tenant clients never appear in the global client listing")

	// ...yet the delete is correctly blocked by the tenant-held client.
	err = ti.service.DeleteGlobalIssuer(adminCtx, &adminrsgen.DeleteGlobalIssuerPayload{
		ID:           globalIssuer.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}
