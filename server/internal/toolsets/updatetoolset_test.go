package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestToolsetsService_UpdateToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 3, "expected at least 3 tools from petstore")

	// Create a toolset with one tool
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original Toolset",
		Description:            new("Original description"),
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.Len(t, created.Tools, 1, "should start with 1 HTTP tool")

	// Update the toolset with different tools
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   new("Updated Toolset"),
		Description:            new("Updated description"),
		DefaultEnvironmentSlug: nil,
		ToolUrns:               []string{tools[1].ToolUrn.String(), tools[2].ToolUrn.String()},
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Updated Toolset", result.Name)
	require.Equal(t, "Updated description", *result.Description)
	require.Len(t, result.Tools, 2, "should have 2 HTTP tools after update")
	require.Equal(t, string(created.Slug), string(result.Slug)) // Slug should remain the same

	// Verify the tool URNs were updated
	toolUrns := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		baseTool, err := conv.ToBaseTool(tool)
		require.NoError(t, err)
		toolUrns[i] = baseTool.ToolUrn
	}
	require.ElementsMatch(t, []string{tools[1].ToolUrn.String(), tools[2].ToolUrn.String()}, toolUrns)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_PartialUpdate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 1, "expected at least 1 tool from petstore")

	// Create a toolset first with a tool
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original Toolset",
		Description:            new("Original description"),
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update only the name (ToolUrns is nil, so tools should remain unchanged)
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   new("Updated Name Only"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Updated Name Only", result.Name)
	require.Equal(t, "Original description", *result.Description) // Should remain unchanged
	require.Len(t, result.Tools, 1, "should still have 1 tool")   // Should remain unchanged
	baseTool, err := conv.ToBaseTool(result.Tools[0])
	require.NoError(t, err)
	require.Equal(t, tools[0].ToolUrn.String(), baseTool.ToolUrn)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_WithEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create an environment first
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	envRepo := environmentsRepo.New(ti.conn)
	_, err = envRepo.CreateEnvironment(ctx, environmentsRepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Update Test Environment",
		Slug:           "update-test-env",
		Description:    pgtype.Text{String: "Update test environment", Valid: true},
	})
	require.NoError(t, err)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset for Env Update",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update with environment
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: (*types.Slug)(new("update-test-env")),
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "update-test-env", string(*result.DefaultEnvironmentSlug))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   "non-existent-slug",
		Name:                   new("New Name"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolset not found")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_UpdateToolset_InvalidEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset for Invalid Env",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Try to update with non-existent environment
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: (*types.Slug)(new("non-existent-env")),
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "error finding environment")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_UpdateToolset_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   "some-slug",
		Name:                   new("New Name"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_UpdateToolset_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   "some-slug",
		Name:                   new("New Name"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_UpdateToolset_EmptyToolUrns(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Create a toolset with tools
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset with Tools",
		Description:            nil,
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.Len(t, created.Tools, 2, "should start with 2 tools")

	// Update to have empty tool URNs (remove all tools)
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Tools, "should have no tools after clearing")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_McpEnabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "MCP Toolset",
		Description:            new("Toolset for MCP testing"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Update to enable MCP
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             new(true),
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, *result.McpEnabled)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_ResourceUrnsNil_PreservesResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create deployment with functions that include resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get resources from the deployment
	repo := testrepo.New(ti.conn)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create toolset with resources
	resourceUrns := make([]string, len(resources))
	for i, r := range resources {
		resourceUrns[i] = r.ResourceUrn.String()
	}

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset With Resources",
		Description:            new("A toolset with resources"),
		ToolUrns:               []string{},
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.Len(t, created.Resources, 3, "should start with 3 resources")

	// Update the toolset with ResourceUrns as nil (should preserve existing resources)
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   new("Updated Name"),
		Description:            new("Updated description"),
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil, // nil should preserve existing resources
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Updated Name", result.Name)
	require.Equal(t, "Updated description", *result.Description)
	require.Len(t, result.Resources, 3, "resources should be preserved when ResourceUrns is nil")
	require.Len(t, result.ResourceUrns, 3, "resource URNs should be preserved when ResourceUrns is nil")

	// Verify resource names are still present
	resourceNames := make(map[string]bool)
	for _, r := range result.Resources {
		require.NotNil(t, r.FunctionResourceDefinition)
		resourceNames[r.FunctionResourceDefinition.Name] = true
	}
	require.True(t, resourceNames["user_guide"])
	require.True(t, resourceNames["api_reference"])
	require.True(t, resourceNames["data_source"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_ResourceUrnsEmpty_RemovesResources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	// Create deployment with functions that include resources
	dep := createFunctionsDeploymentWithResources(t, ctx, ti)

	// Get resources from the deployment
	repo := testrepo.New(ti.conn)
	resources, err := repo.ListDeploymentFunctionsResources(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment resources")
	require.Len(t, resources, 3, "expected 3 resources from manifest")

	// Create toolset with resources
	resourceUrns := make([]string, len(resources))
	for i, r := range resources {
		resourceUrns[i] = r.ResourceUrn.String()
	}

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset With Resources",
		Description:            new("A toolset with resources to be removed"),
		ToolUrns:               []string{},
		ResourceUrns:           resourceUrns,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.Len(t, created.Resources, 3, "should start with 3 resources")

	// Update the toolset with ResourceUrns as empty array (should remove all resources)
	result, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   new("Updated Without Resources"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           []string{}, // empty array should remove all resources
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Updated Without Resources", result.Name)
	require.Empty(t, result.Resources, "resources should be removed when ResourceUrns is empty array")
	require.Empty(t, result.ResourceUrns, "resource URNs should be removed when ResourceUrns is empty array")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Audit Update Original",
		Description:            new("Before description"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   new("Audit Update Renamed"),
		Description:            new("After description"),
		DefaultEnvironmentSlug: nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetUpdate), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, updated.Name, record.SubjectDisplay)
	require.Equal(t, string(updated.Slug), record.SubjectSlug)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, created.Name, beforeSnapshot["Name"])
	require.Equal(t, updated.Name, afterSnapshot["Name"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_ClearsExternalOAuth_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)
	toolset := createMinimalPublicToolset(t, ctx, ti, "Audit Clear External OAuth Toolset")
	attached, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		Slug:         toolset.Slug,
		ExternalOauthServer: &types.ExternalOAuthServerForm{
			Slug: types.Slug("update-detach-external-oauth"),
			Metadata: map[string]any{
				"issuer":         "https://example.com",
				"token_endpoint": "https://example.com/token",
			},
		},
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, attached)
	require.NotNil(t, attached.ExternalOauthServer)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)

	private := false
	updated, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   toolset.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            &private,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Nil(t, updated.ExternalOauthServer)
	require.NotNil(t, updated.McpIsPublic)
	require.False(t, *updated.McpIsPublic)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetDetachExternalOAuth), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, updated.Name, record.SubjectDisplay)
	require.Equal(t, string(updated.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, attached.ExternalOauthServer.ID, metadata["external_oauth_server_id"])
	require.Equal(t, string(attached.ExternalOauthServer.Slug), metadata["external_oauth_server_slug"])
	require.InDelta(t, updated.ToolsetVersion, metadata["toolset_version_after"], 0)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDetachExternalOAuth)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_UpdateToolset_NotFound_NoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   "missing-toolset",
		Name:                   new("Should Fail"),
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolset not found")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}
