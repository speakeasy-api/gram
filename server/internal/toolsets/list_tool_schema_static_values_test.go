package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestToolsetsService_ListToolSchemaStaticValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	deployment := createTodoDeploymentWithDocs(t, ctx, ti, "static-values-deployment", "static-values-doc")

	tools, err := testrepo.New(ti.conn).ListDeploymentHTTPTools(ctx, uuid.MustParse(deployment.Deployment.ID))
	require.NoError(t, err)
	require.NotEmpty(t, tools)

	toolURNs := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolURNs = append(toolURNs, tool.ToolUrn.String())
	}

	toolset, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   "Static Values",
		Description:            nil,
		ToolUrns:               toolURNs,
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListToolSchemaStaticValues(ctx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Tools)

	var defaults map[string]string
	for _, tool := range result.Tools {
		if tool.ToolName != "static_values_doc_get_todos" {
			continue
		}
		defaults = map[string]string{}
		for _, value := range tool.Values {
			if value.Keyword == "default" {
				defaults[value.SchemaPath] = value.ValueJSON
			}
		}
	}

	require.Equal(t, map[string]string{
		"/properties/queryParameters/properties/limit":  "20",
		"/properties/queryParameters/properties/offset": "0",
	}, defaults)
}

func TestToolsetsService_ListToolSchemaStaticValues_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "No Static Values")

	result, err := ti.service.ListToolSchemaStaticValues(ctx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Tools)
	require.NotNil(t, result.Tools)
}

func TestToolsetsService_ListToolSchemaStaticValues_RejectsProxyTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Proxy Schema Review",
		Slug:           "proxy-schema-review",
	})
	require.NoError(t, err)

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	registryID, err := externalmcp_repo.New(ti.conn).CreateMCPRegistry(ctx, externalmcp_repo.CreateMCPRegistryParams{
		Name: "proxy-schema-review",
		Url:  "https://example.com/mcp",
	})
	require.NoError(t, err)
	attachment, err := externalmcp_repo.New(ti.conn).CreateExternalMCPAttachment(ctx, externalmcp_repo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              uuid.NullUUID{UUID: registryID, Valid: true},
		Name:                    "Proxy Schema Review",
		Slug:                    "proxy-schema-review",
		RegistryServerSpecifier: "proxy-schema-review",
	})
	require.NoError(t, err)

	toolURN := "tools:externalmcp:proxy-schema-review:proxy"
	_, err = externalmcp_repo.New(ti.conn).CreateExternalMCPToolDefinition(ctx, externalmcp_repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachment.ID,
		ToolUrn:                    toolURN,
		Type:                       "proxy",
		RemoteUrl:                  "https://example.com/mcp",
		TransportType:              externalmcp_types.TransportTypeStreamableHTTP,
		RequiresOauth:              false,
		OauthVersion:               "none",
		OauthAuthorizationEndpoint: pgtype.Text{},
		OauthTokenEndpoint:         pgtype.Text{},
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
	})
	require.NoError(t, err)
	parsedURN, err := urn.ParseTool(toolURN)
	require.NoError(t, err)
	_, err = toolsets_repo.New(ti.conn).CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:    toolset.ID,
		Version:      1,
		ToolUrns:     []urn.Tool{parsedURN},
		ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)

	_, err = ti.service.ListToolSchemaStaticValues(ctx, &gen.ListToolSchemaStaticValuesPayload{Slug: "proxy-schema-review"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
	require.ErrorContains(t, err, "live external MCP tool schemas")
}

func TestToolsetsService_ListToolSchemaStaticValues_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	_, err := ti.service.ListToolSchemaStaticValues(ctx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             "missing-toolset",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestToolsetsService_ListToolSchemaStaticValues_RBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "Static Values RBAC")

	deniedCtx := authztest.WithExactGrants(t, ctx)
	_, err := ti.service.ListToolSchemaStaticValues(deniedCtx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	grantedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPRead,
		Selector: authz.NewSelector(authz.ScopeMCPRead, toolset.ID),
	})
	result, err := ti.service.ListToolSchemaStaticValues(grantedCtx, &gen.ListToolSchemaStaticValuesPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Tools)
}
