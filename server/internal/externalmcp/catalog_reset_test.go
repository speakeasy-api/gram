package externalmcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type catalogResetInstallationSource struct{ bundle mcpregistry.CatalogBundle }

func (s *catalogResetInstallationSource) Fetch(context.Context) (mcpregistry.CatalogBundle, error) {
	return s.bundle, nil
}

func TestCatalogResetRetainsPersistedNativeInstallation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestExternalMCPService(t)
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	registry := mcpregistry.New(ti.conn, validator)
	const name = "io.example/reset-installed"
	const installedURL = "https://example.com/installed"
	const authorizationURL = "https://example.com/authorize"
	const tokenURL = "https://example.com/token"
	const toolURN = "tools:externalmcp:reset-installed:proxy"
	source := &catalogResetInstallationSource{bundle: mcpregistry.CatalogBundle{
		Version: mcpregistry.CatalogBundleVersion,
		Entries: []json.RawMessage{json.RawMessage(`{"server":{"name":"io.example/reset-installed","description":"Persisted reset installation","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.com/default"},{"type":"streamable-http","url":"https://example.com/installed"}]}}`)},
	}}
	_, err = mcpregistry.NewCatalogRefresher(registry, "local", "configured-production", source, nil).Import(ctx, "staff-owner")
	require.NoError(t, err)
	original, err := registry.GetByName(ctx, name)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	deployments := deploymentsrepo.New(ti.conn)
	deploymentID, err := deployments.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		UserID: authCtx.UserID, IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, deployments.CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{DeploymentID: deploymentID, Status: "completed"}))
	external := externalmcprepo.New(ti.conn)
	attachment, err := external.CreateExternalMCPAttachment(ctx, externalmcprepo.CreateExternalMCPAttachmentParams{
		DeploymentID: deploymentID, RegistryID: conv.ToNullUUID(externalmcp.NativeCatalogRegistryID),
		Name: "Reset installed", Slug: "reset-installed", RegistryServerSpecifier: name,
	})
	require.NoError(t, err)
	// The fixture insertion API does not expose selected_remotes. Persist a
	// non-default selection, then reload it rather than passing a literal to the reader.
	err = external.SetAttachmentSelectedRemotesFixture(ctx, externalmcprepo.SetAttachmentSelectedRemotesFixtureParams{ID: attachment.ID, SelectedRemotes: []string{installedURL}})
	require.NoError(t, err)
	loadAttachment := func() externalmcprepo.ExternalMcpAttachment {
		t.Helper()
		row, err := external.GetAttachmentFixture(ctx, attachment.ID)
		require.NoError(t, err)
		return row
	}
	beforeAttachment := loadAttachment()
	require.Equal(t, []string{installedURL}, beforeAttachment.SelectedRemotes)
	_, err = external.CreateExternalMCPToolDefinition(ctx, externalmcprepo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID: attachment.ID, ToolUrn: toolURN, Type: "proxy",
		RemoteUrl: installedURL, TransportType: externalmcptypes.TransportTypeStreamableHTTP,
		RequiresOauth: true, OauthVersion: "2.0", OauthAuthorizationEndpoint: conv.ToPGText(authorizationURL),
		OauthTokenEndpoint: conv.ToPGText(tokenURL), OauthScopesSupported: []string{"read"},
	})
	require.NoError(t, err)
	toolLookup := externalmcprepo.GetExternalMCPToolDefinitionByURNParams{ProjectID: *authCtx.ProjectID, ToolUrn: toolURN}
	beforeTool, err := external.GetExternalMCPToolDefinitionByURN(ctx, toolLookup)
	require.NoError(t, err)

	// Remote OAuth registrations are associated with the installed endpoint's
	// authorization server, not with a publication-gated catalog row.
	remote := remotesessionsrepo.New(ti.conn)
	issuer, err := remote.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID), OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug: "reset-installed", Issuer: "https://example.com", AuthorizationEndpoint: conv.ToPGText(authorizationURL),
		TokenEndpoint: conv.ToPGText(tokenURL), ScopesSupported: []string{"read"},
		GrantTypesSupported:                 []string{"authorization_code", "refresh_token"},
		AuthorizationGrantProfilesSupported: []string{},
		ResponseTypesSupported:              []string{"code"},
		TokenEndpointAuthMethodsSupported:   []string{"client_secret_post"},
	})
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	ciphertext, err := enc.Encrypt([]byte("synthetic-reset-client-secret"))
	require.NoError(t, err)
	client, err := remote.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID), OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: issuer.ID, ClientID: "synthetic-reset-client",
		ClientSecretEncrypted: conv.ToPGText(ciphertext), TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
		Scope: []string{"read"}, Audience: conv.ToPGText(installedURL),
	})
	require.NoError(t, err)
	issuerLookup := remotesessionsrepo.GetRemoteSessionIssuerByIDParams{ID: issuer.ID, ProjectID: conv.ToNullUUID(*authCtx.ProjectID)}
	beforeIssuer, err := remote.GetRemoteSessionIssuerByID(ctx, issuerLookup)
	require.NoError(t, err)
	clientLookup := remotesessionsrepo.GetRemoteSessionClientByIDParams{ID: client.ID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID}
	beforeClient, err := remote.GetRemoteSessionClientByID(ctx, clientLookup)
	require.NoError(t, err)
	catalog := externalmcp.NewCatalogService(ti.conn, externalmcp.NewNativeRegistryReader(registry))
	beforeDetails, err := catalog.Details(ctx, beforeAttachment.RegistryID.UUID, beforeAttachment.RegistryServerSpecifier, beforeAttachment.SelectedRemotes)
	require.NoError(t, err)
	require.Equal(t, installedURL, beforeDetails.RemoteURL)
	newInstall := &gen.GetServerDetailsPayload{RegistryID: externalmcp.NativeCatalogRegistryID.String(), ServerSpecifier: name}
	_, err = ti.service.GetServerDetails(ctx, newInstall)
	require.NoError(t, err)

	// The available, empty source makes the installed destination entry a
	// destination-only record. Reset must unpublish it, never delete it.
	source.bundle.Entries = nil
	reset := mcpregistry.NewCatalogResetter(registry, "local", "configured-production", "local-test", source, nil)
	plan, err := reset.Preview(ctx, "staff-owner")
	require.NoError(t, err)
	require.NotEmpty(t, plan.Confirmation)
	_, err = reset.Apply(ctx, "staff-owner", plan.ID, plan.Confirmation)
	require.NoError(t, err)
	retained, err := registry.GetByName(ctx, name)
	require.NoError(t, err)
	require.Equal(t, original.ID, retained.ID)
	require.False(t, retained.Published)
	require.JSONEq(t, string(original.Data), string(retained.Data), "canonical name and remote endpoints must remain available to installed consumers")

	afterAttachment := loadAttachment()
	require.Equal(t, beforeAttachment, afterAttachment, "the complete persisted attachment, including selection and lifecycle, is untouched")
	afterTool, err := external.GetExternalMCPToolDefinitionByURN(ctx, toolLookup)
	require.NoError(t, err)
	require.Equal(t, beforeTool, afterTool, "installed tool consumer retains endpoints, OAuth metadata and attachment linkage")
	afterDetails, err := catalog.Details(ctx, afterAttachment.RegistryID.UUID, afterAttachment.RegistryServerSpecifier, afterAttachment.SelectedRemotes)
	require.NoError(t, err)
	require.Equal(t, beforeDetails, afterDetails)
	afterIssuer, err := remote.GetRemoteSessionIssuerByID(ctx, issuerLookup)
	require.NoError(t, err)
	require.Equal(t, beforeIssuer, afterIssuer)
	afterClient, err := remote.GetRemoteSessionClientByID(ctx, clientLookup)
	require.NoError(t, err)
	require.Equal(t, beforeClient, afterClient, "registration configuration and encrypted credential bytes are untouched")
	secret, err := enc.Decrypt(afterClient.RemoteSessionClient.ClientSecretEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "synthetic-reset-client-secret", secret)
	visible, err := catalog.List(ctx, nil, nil)
	require.NoError(t, err)
	require.Empty(t, visible, "unpublished installations are excluded from new discovery")
	_, err = ti.service.GetServerDetails(ctx, newInstall)
	require.Error(t, err, "an unpublished entry cannot authorize a new installation")
}
