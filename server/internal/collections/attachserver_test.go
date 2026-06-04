package collections_test

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/collections"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/stretchr/testify/require"
)

func TestCollectionsService_AttachServer_AllowsOriginBackedToolsets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	toolset := createMCPEnabledToolset(
		t,
		ctx,
		ti,
		"Origin Toolset",
		"com.speakeasy.example/server",
	)
	collection := createCollection(
		t,
		ctx,
		ti,
		"Registry",
		"registry",
		"com.speakeasy.registry",
	)

	result, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, collection.ID, result.ID)
}

func TestCollectionsService_ListServers_PreservesOriginLineage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	toolset := createMCPEnabledToolset(
		t,
		ctx,
		ti,
		"Origin Toolset",
		"com.speakeasy.example/server",
	)
	collection := createCollection(
		t,
		ctx,
		ti,
		"Registry",
		"registry",
		"com.speakeasy.registry",
	)

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.NotNil(t, toolset.McpSlug)
	require.Equal(t, "com.speakeasy.registry/"+string(*toolset.McpSlug), result.Servers[0].RegistrySpecifier)
	require.NotNil(t, result.Servers[0].ToolsetID)
	require.Equal(t, toolset.ID, *result.Servers[0].ToolsetID)
	require.Nil(t, result.Servers[0].RegistryID)
	require.NotNil(t, result.Servers[0].OrganizationMcpCollectionRegistryID)
	require.NotNil(t, result.Servers[0].Remotes)
	require.Len(t, result.Servers[0].Remotes, 1)

	headers := result.Servers[0].Remotes[0].Headers
	require.Len(t, headers, 2)
	headersByName := make(map[string]*types.ExternalMCPRemoteHeader, len(headers))
	for _, header := range headers {
		headersByName[header.Name] = header
	}

	environmentHeader := headersByName[toolconfig.ToHTTPHeader("gram_environment")]
	require.NotNil(t, environmentHeader)
	require.NotNil(t, environmentHeader.Placeholder)
	require.Equal(t, "${GRAM_ENVIRONMENT}", *environmentHeader.Placeholder)

	authorizationHeader := headersByName[toolconfig.ToHTTPHeader("authorization")]
	require.NotNil(t, authorizationHeader)
	require.NotNil(t, authorizationHeader.IsSecret)
	require.True(t, *authorizationHeader.IsSecret)
	require.NotNil(t, authorizationHeader.Placeholder)
	require.Equal(t, "${GRAM_KEY}", *authorizationHeader.Placeholder)
}

func TestCollectionsService_ListServers_IncludesUserProvidedEnvironmentHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createMCPEnabledToolset(
		t,
		ctx,
		ti,
		"Example Toolset",
		"com.speakeasy.example/server",
	)
	toolsetID, err := uuid.Parse(toolset.ID)
	require.NoError(t, err)
	err = toolsetsRepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsetsRepo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolsetID,
		ProjectID:   *authCtx.ProjectID,
	})
	require.NoError(t, err)

	mcpRepo := mcpmetarepo.New(ti.conn)
	metadata, err := mcpRepo.UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolsetID, Valid: true},
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{Valid: false},
		ExternalDocumentationText: pgtype.Text{Valid: false},
		LogoID:                    uuid.NullUUID{Valid: false},
		Instructions:              pgtype.Text{Valid: false},
		DefaultEnvironmentID:      uuid.NullUUID{Valid: false},
		InstallationOverrideUrl:   pgtype.Text{Valid: false},
	})
	require.NoError(t, err)

	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "SERVICE_API_KEY",
		HeaderDisplayName: pgtype.Text{String: "External API Key", Valid: true},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "SYSTEM_ONLY",
		HeaderDisplayName: pgtype.Text{String: "X-System-Only", Valid: true},
		ProvidedBy:        "system",
	})
	require.NoError(t, err)
	_, err = mcpRepo.UpsertEnvironmentConfig(ctx, mcpmetarepo.UpsertEnvironmentConfigParams{
		ProjectID:         *authCtx.ProjectID,
		McpMetadataID:     metadata.ID,
		VariableName:      "HEADERLESS_SECRET",
		HeaderDisplayName: pgtype.Text{Valid: false},
		ProvidedBy:        "user",
	})
	require.NoError(t, err)

	collection := createCollection(
		t,
		ctx,
		ti,
		"Registry",
		"registry",
		"com.speakeasy.registry",
	)

	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.Len(t, result.Servers[0].Remotes, 1)

	headers := result.Servers[0].Remotes[0].Headers
	require.Len(t, headers, 2)

	headersByName := make(map[string]*types.ExternalMCPRemoteHeader, len(headers))
	for _, header := range headers {
		headersByName[header.Name] = header
	}

	customDisplayHeader := headersByName[toolconfig.ToHTTPHeader("MCP-SERVICE_API_KEY")]
	require.NotNil(t, customDisplayHeader)
	require.NotNil(t, customDisplayHeader.IsSecret)
	require.True(t, *customDisplayHeader.IsSecret)
	require.NotNil(t, customDisplayHeader.IsRequired)
	require.True(t, *customDisplayHeader.IsRequired)
	require.NotNil(t, customDisplayHeader.Placeholder)
	require.Equal(t, "${EXTERNAL_API_KEY}", *customDisplayHeader.Placeholder)

	headerlessHeader := headersByName[toolconfig.ToHTTPHeader("MCP-HEADERLESS_SECRET")]
	require.NotNil(t, headerlessHeader)
	require.NotNil(t, headerlessHeader.Placeholder)
	require.Equal(t, "${MCP_HEADERLESS_SECRET}", *headerlessHeader.Placeholder)
	require.Nil(t, result.Servers[0].Remotes[0].Variables)
}

func TestCollectionsService_AttachServer_McpServerBacked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	server := createMCPServerWithEndpoint(t, ctx, ti, "Remote Server", "remote-server", mcpservers.VisibilityPrivate, uuid.NullUUID{})
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	result, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, collection.ID, result.ID)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Servers, 1)

	got := listed.Servers[0]
	require.NotNil(t, got.McpServerID)
	require.Equal(t, server.idStr, *got.McpServerID)
	require.Nil(t, got.ToolsetID)
	require.Equal(t, "com.speakeasy.registry/"+server.endpointSlug, got.RegistrySpecifier)
	require.NotNil(t, got.OrganizationMcpCollectionRegistryID)

	require.Len(t, got.Remotes, 1)
	expectedURL := testenv.DefaultSiteURL(t).JoinPath("mcp", server.endpointSlug).String()
	require.Equal(t, expectedURL, got.Remotes[0].URL)
	// mcp_server-backed remotes authenticate via OAuth, so no static headers.
	require.Empty(t, got.Remotes[0].Headers)
}

func TestCollectionsService_AttachServer_CustomDomainEndpointURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	domainID := createCustomDomain(t, ctx, ti, "mcp.example.com")
	server := createMCPServerWithEndpoint(t, ctx, ti, "Domain Server", "domain-server", mcpservers.VisibilityPrivate, uuid.NullUUID{UUID: domainID, Valid: true})
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Servers, 1)

	expectedURL := (&url.URL{Scheme: "https", Host: "mcp.example.com"}).JoinPath("mcp", server.endpointSlug).String()
	require.Equal(t, expectedURL, listed.Servers[0].Remotes[0].URL)
}

func TestCollectionsService_ListServers_SkipsDanglingCustomDomainEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	domainID := createCustomDomain(t, ctx, ti, "going-away.example.com")
	// Endpoint A is custom-domain backed and created first, so the selection's
	// (custom-domain-first, oldest-created) preference would pick it.
	server := createMCPServerWithEndpoint(t, ctx, ti, "Dangling Domain", "dangling-domain", mcpservers.VisibilityPrivate, uuid.NullUUID{UUID: domainID, Valid: true})
	// Endpoint B is platform-hosted, added afterwards.
	platformSlug := "dangling-domain-platform"
	_, err := mcpendpointsRepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: uuid.NullUUID{},
		McpServerID:    server.id,
		Slug:           platformSlug,
	})
	require.NoError(t, err)

	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")
	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	// Soft-delete the custom domain so endpoint A's host no longer resolves.
	require.NoError(t, customdomainsRepo.New(ti.conn).DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Servers, 1)

	// The dangling custom-domain endpoint is skipped in favor of the platform
	// endpoint, so the URL is the platform host with the platform slug — never
	// a platform URL built from the custom-domain endpoint's slug.
	expectedURL := testenv.DefaultSiteURL(t).JoinPath("mcp", platformSlug).String()
	require.Equal(t, expectedURL, listed.Servers[0].Remotes[0].URL)
}

func TestCollectionsService_AttachServer_RequiresExactlyOneBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	toolset := createMCPEnabledToolset(t, ctx, ti, "Toolset", "")
	server := createMCPServerWithEndpoint(t, ctx, ti, "Server", "server", mcpservers.VisibilityPrivate, uuid.NullUUID{})
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	_, neitherErr := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, neitherErr, oops.CodeBadRequest)

	_, bothErr := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, bothErr, oops.CodeBadRequest)
}

func TestCollectionsService_AttachServer_DetachRequiresExactlyOneBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	neitherErr := ti.service.DetachServer(ctx, &gen.DetachServerPayload{
		CollectionID: collection.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, neitherErr, oops.CodeBadRequest)
}

func TestCollectionsService_AttachServer_RejectsDisabledMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	server := createMCPServerWithEndpoint(t, ctx, ti, "Disabled", "disabled-server", mcpservers.VisibilityDisabled, uuid.NullUUID{})
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCollectionsService_AttachServer_RejectsMcpServerWithoutEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	serverID := createMCPServerWithoutEndpoint(t, ctx, ti, "No Endpoint", "no-endpoint-server", mcpservers.VisibilityPrivate)
	serverIDStr := serverID.String()
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &serverIDStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestCollectionsService_DetachServer_McpServerBacked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	server := createMCPServerWithEndpoint(t, ctx, ti, "Remote Server", "remote-server", mcpservers.VisibilityPrivate, uuid.NullUUID{})
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	err = ti.service.DetachServer(ctx, &gen.DetachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, listed.Servers)
}

func TestCollectionsService_ListServers_MergesBackendsByPublishedAt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	toolset := createMCPEnabledToolset(t, ctx, ti, "Toolset Server", "com.speakeasy.example/toolset")
	server := createMCPServerWithEndpoint(t, ctx, ti, "Remote Server", "remote-server", mcpservers.VisibilityPrivate, uuid.NullUUID{})
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")

	// Attach the toolset first, then the mcp_server, so the mcp_server has the
	// newer published_at and must sort ahead under global published_at DESC.
	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Servers, 2)

	require.NotNil(t, listed.Servers[0].McpServerID)
	require.Equal(t, server.idStr, *listed.Servers[0].McpServerID)
	require.Nil(t, listed.Servers[0].ToolsetID)

	require.NotNil(t, listed.Servers[1].ToolsetID)
	require.Equal(t, toolset.ID, *listed.Servers[1].ToolsetID)
	require.Nil(t, listed.Servers[1].McpServerID)
}

func TestCollectionsService_Audit_AttachAndDetachMcpServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	server := createMCPServerWithEndpoint(t, ctx, ti, "Audited Server", "audited-server", mcpservers.VisibilityPrivate, uuid.NullUUID{})
	collection := createCollection(t, ctx, ti, "Attach Detach", "attach-detach", "com.example.attach-detach")

	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	attachRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpCollectionAttachServer)
	require.NoError(t, err)
	attachMetadata, err := audittest.DecodeAuditData(attachRecord.Metadata)
	require.NoError(t, err)
	require.Equal(t, server.idStr, attachMetadata["mcp_server_id"])
	require.NotContains(t, attachMetadata, "toolset_id")

	err = ti.service.DetachServer(ctx, &gen.DetachServerPayload{
		CollectionID: collection.ID,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	detachRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMcpCollectionDetachServer)
	require.NoError(t, err)
	detachMetadata, err := audittest.DecodeAuditData(detachRecord.Metadata)
	require.NoError(t, err)
	require.Equal(t, server.idStr, detachMetadata["mcp_server_id"])
	require.NotContains(t, detachMetadata, "toolset_id")
}
